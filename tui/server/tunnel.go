package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Tunnel publishing: `pixeltui serve --tunnel <provider>` starts (or detects)
// a tunnel and advertises its public URL in the pairing QR, so remote access
// needs no manual URL plumbing. Providers:
//
//   - cloudflare: `cloudflared` quick tunnel (random *.trycloudflare.com URL,
//     no account needed). HTTPS terminated by Cloudflare.
//   - ngrok: `ngrok http <port>` (needs a configured ngrok agent). HTTPS.
//   - tailscale: no extra process — detects this machine's tailnet DNS name
//     and advertises http://<name>:<port>. Traffic stays inside WireGuard.
//
// The bearer-token auth still applies on every request; the tunnel only
// handles transport.

// Tunnel is a running (or detected) tunnel advertising URL.
type Tunnel struct {
	Provider string
	URL      string
	cmd      *exec.Cmd // nil for detection-only providers (tailscale)
}

// Close stops the tunnel process, if one was started.
func (t *Tunnel) Close() {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return
	}
	t.cmd.Process.Kill() //nolint:errcheck
	t.cmd.Wait()         //nolint:errcheck
}

// tunnelTimeout caps how long we wait for a provider to report its URL.
const tunnelTimeout = 30 * time.Second

// StartTunnel launches the given provider for a server bound at addr and
// returns once the public URL is known.
func StartTunnel(provider, addr string) (*Tunnel, error) {
	port := addrPort(addr)
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "cloudflare", "cloudflared":
		return startCloudflare(port)
	case "ngrok":
		return startNgrok(port)
	case "tailscale", "ts":
		return detectTailscale(port)
	case "", "none", "off":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown tunnel %q (cloudflare, ngrok, tailscale)", provider)
	}
}

// addrPort extracts the port from a bind address like ":8787" or "0.0.0.0:8787".
func addrPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return "8787"
}

// startCloudflare runs a cloudflared quick tunnel and scrapes the assigned
// *.trycloudflare.com URL from its log output.
func startCloudflare(port string) (*Tunnel, error) {
	bin, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, fmt.Errorf("cloudflared not found — install it (brew install cloudflared) or pick another tunnel")
	}
	cmd := exec.Command(bin, "tunnel", "--url", "http://127.0.0.1:"+port, "--no-autoupdate")
	// cloudflared logs the URL to stderr.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	url, err := scanForURL(stderr, func(line string) string { return re.FindString(line) })
	if err != nil {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
		return nil, fmt.Errorf("cloudflared: %w", err)
	}
	return &Tunnel{Provider: "cloudflare", URL: url, cmd: cmd}, nil
}

// startNgrok runs `ngrok http <port>` with JSON logs and reads the public URL
// from the "started tunnel" log line.
func startNgrok(port string) (*Tunnel, error) {
	bin, err := exec.LookPath("ngrok")
	if err != nil {
		return nil, fmt.Errorf("ngrok not found — install it (brew install ngrok) and run `ngrok config add-authtoken …`")
	}
	cmd := exec.Command(bin, "http", port, "--log", "stdout", "--log-format", "json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	url, err := scanForURL(stdout, func(line string) string {
		var entry struct {
			URL string `json:"url"`
			Err string `json:"err"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil {
			return ""
		}
		if strings.HasPrefix(entry.URL, "https://") {
			return entry.URL
		}
		return ""
	})
	if err != nil {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
		return nil, fmt.Errorf("ngrok: %w (is an authtoken configured?)", err)
	}
	return &Tunnel{Provider: "ngrok", URL: url, cmd: cmd}, nil
}

// detectTailscale reads this machine's tailnet DNS name from the local
// tailscale daemon. No process is started — Tailscale is already the tunnel.
func detectTailscale(port string) (*Tunnel, error) {
	bin, err := exec.LookPath("tailscale")
	if err != nil {
		return nil, fmt.Errorf("tailscale not found — install it from tailscale.com/download")
	}
	out, err := exec.Command(bin, "status", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status failed — is Tailscale running and logged in?")
	}
	var status struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}
	if status.BackendState != "Running" || status.Self.DNSName == "" {
		return nil, fmt.Errorf("tailscale is %s — run `tailscale up` first", strings.ToLower(status.BackendState))
	}
	host := strings.TrimSuffix(status.Self.DNSName, ".")
	return &Tunnel{Provider: "tailscale", URL: "http://" + host + ":" + port}, nil
}

// scanForURL reads lines from r until extract returns a URL or the timeout
// elapses.
func scanForURL(r io.Reader, extract func(string) string) (string, error) {
	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			if u := extract(sc.Text()); u != "" {
				found <- u
				// Keep draining so the child never blocks on a full pipe.
				go func() {
					for sc.Scan() {
					}
				}()
				return
			}
		}
		close(found)
	}()
	select {
	case u, ok := <-found:
		if !ok || u == "" {
			return "", fmt.Errorf("exited before reporting a public URL")
		}
		return u, nil
	case <-time.After(tunnelTimeout):
		return "", fmt.Errorf("no public URL after %s", tunnelTimeout)
	}
}
