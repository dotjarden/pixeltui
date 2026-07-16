// Package tunnel abstracts tunnel/discovery providers so `pixeltui serve` can
// publish itself without hardcoding cloudflare/ngrok/tailscale branches.
//
// Each provider is independent and optional: the registry reports which are
// available, and callers pick one by name or accept the first available default.
// Supervised providers (cloudflare, ngrok) are restarted when they die; quick
// tunnels get a fresh URL each restart and report it through OnURL so the
// server can re-advertise itself.
package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Provider is one tunnel/discovery backend.
type Provider interface {
	// Name is the short identifier used by flags and config, e.g. "cloudflare".
	Name() string

	// Label is a human-readable description for menus and status output.
	Label() string

	// Available reports whether the provider can be used right now (binaries
	// installed, service running, etc.).
	Available() bool

	// Start launches the tunnel for the server bound at addr and returns once
	// the public URL is known.
	Start(ctx context.Context, addr string) (*Tunnel, error)
}

// Tunnel is a running (or detected) tunnel advertising a public URL.
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

// Registry holds tunnel providers by name.
type Registry struct {
	byKey map[string]Provider
	order []string
	mu    sync.RWMutex
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{byKey: make(map[string]Provider)}
}

// Register adds a provider. Panics on duplicate name.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	if _, ok := r.byKey[name]; ok {
		panic("tunnel registry: duplicate provider " + name)
	}
	r.byKey[name] = p
	r.order = append(r.order, name)
}

// ByKey returns a provider by name, or nil.
func (r *Registry) ByKey(name string) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byKey[name]
}

// Keys returns registered provider names in registration order.
func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Available returns the names of providers that report Available() == true.
func (r *Registry) Available() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, k := range r.order {
		if r.byKey[k].Available() {
			out = append(out, k)
		}
	}
	return out
}

// Default returns the first available registered provider, or nil if none.
func (r *Registry) Default() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, k := range r.order {
		if p := r.byKey[k]; p.Available() {
			return p
		}
	}
	return nil
}

// Start looks up the named provider and starts it. An empty name uses the
// default available provider.
func (r *Registry) Start(ctx context.Context, name, addr string) (*Tunnel, error) {
	if r == nil {
		return nil, fmt.Errorf("no tunnel registry")
	}
	var p Provider
	if name != "" {
		p = r.ByKey(name)
		if p == nil {
			return nil, fmt.Errorf("unknown tunnel provider %q", name)
		}
	} else {
		p = r.Default()
		if p == nil {
			return nil, fmt.Errorf("no tunnel provider available")
		}
	}
	t, err := p.Start(ctx, addr)
	if err != nil {
		return nil, err
	}
	if t != nil {
		t.Provider = p.Name()
	}
	return t, nil
}

// NewDefaultRegistry returns a registry with the built-in providers
// (cloudflare, ngrok, tailscale).
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(cloudflareProvider{})
	r.Register(ngrokProvider{})
	r.Register(tailscaleProvider{})
	return r
}

// tunnelTimeout caps how long we wait for a provider to report its URL.
const tunnelTimeout = 30 * time.Second

// Supervisor keeps a tunnel alive and feeds URL changes to OnURL.
type Supervisor struct {
	provider string
	addr     string
	onURL    func(string)
	reg      *Registry

	mu     sync.Mutex
	cur    *Tunnel
	closed bool
	done   chan struct{}
}

// StartSupervised starts the named provider and keeps it running until Close.
// onURL fires on every re-establishment whose URL differs from the previous
// one (never for the initial start — read that from URL()).
func (r *Registry) StartSupervised(ctx context.Context, name, addr string, onURL func(string)) (*Supervisor, error) {
	t, err := r.Start(ctx, name, addr)
	if err != nil || t == nil {
		return nil, err
	}
	sup := &Supervisor{provider: t.Provider, addr: addr, onURL: onURL, reg: r, cur: t, done: make(chan struct{})}
	if t.cmd != nil {
		go sup.watch(t)
	}
	return sup, nil
}

// URL is the tunnel's current public URL.
func (s *Supervisor) URL() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil {
		return ""
	}
	return s.cur.URL
}

// Provider names the active provider.
func (s *Supervisor) Provider() string {
	if s == nil {
		return ""
	}
	return s.provider
}

// Close stops supervision and tears the tunnel down.
func (s *Supervisor) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cur := s.cur
	s.mu.Unlock()
	close(s.done)
	cur.Close()
}

// watch waits on the tunnel process and restarts it when it dies.
func (s *Supervisor) watch(t *Tunnel) {
	started := time.Now()
	t.cmd.Wait() //nolint:errcheck

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	prevURL := t.URL
	s.mu.Unlock()

	backoff := time.Second
	if time.Since(started) > time.Minute {
		backoff = time.Second
	}
	ctx := context.Background()
	for {
		fmt.Printf("  ! %s tunnel exited — restarting in %s…\n", s.provider, backoff)
		select {
		case <-s.done:
			return
		case <-time.After(backoff):
		}
		nt, err := s.reg.Start(ctx, s.provider, s.addr)
		if err == nil && nt != nil {
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				nt.Close()
				return
			}
			s.cur = nt
			s.mu.Unlock()
			if nt.URL != prevURL && s.onURL != nil {
				s.onURL(nt.URL)
			}
			if nt.cmd != nil {
				go s.watch(nt)
			}
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

// ── built-in providers ───────────────────────────────────────────────────────

type cloudflareProvider struct{}

func (cloudflareProvider) Name() string  { return "cloudflare" }
func (cloudflareProvider) Label() string { return "Cloudflare quick tunnel" }
func (cloudflareProvider) Available() bool {
	_, err := exec.LookPath("cloudflared")
	return err == nil
}
func (cloudflareProvider) Start(ctx context.Context, addr string) (*Tunnel, error) {
	return startCloudflare(ctx, addrPort(addr))
}

type ngrokProvider struct{}

func (ngrokProvider) Name() string  { return "ngrok" }
func (ngrokProvider) Label() string { return "ngrok" }
func (ngrokProvider) Available() bool {
	_, err := exec.LookPath("ngrok")
	return err == nil
}
func (ngrokProvider) Start(ctx context.Context, addr string) (*Tunnel, error) {
	return startNgrok(ctx, addrPort(addr))
}

type tailscaleProvider struct{}

func (tailscaleProvider) Name() string  { return "tailscale" }
func (tailscaleProvider) Label() string { return "Tailscale tailnet" }
func (tailscaleProvider) Available() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}
func (tailscaleProvider) Start(ctx context.Context, addr string) (*Tunnel, error) {
	return detectTailscale(addrPort(addr))
}

// addrPort extracts the port from a bind address like ":8787" or "0.0.0.0:8787".
func addrPort(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return "8787"
}

// startCloudflare runs a cloudflared quick tunnel and scrapes the assigned
// *.trycloudflare.com URL from its log output. It waits until cloudflared has
// actually registered the tunnel connection before returning, so the server
// doesn't advertise a URL that isn't reachable yet.
func startCloudflare(ctx context.Context, port string) (*Tunnel, error) {
	bin, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, fmt.Errorf("cloudflared not found — install it (brew install cloudflared) or pick another tunnel")
	}
	cmd := exec.CommandContext(ctx, bin, "tunnel", "--url", "http://127.0.0.1:"+port, "--no-autoupdate")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	url, err := scanCloudflareReady(stdout, stderr)
	if err != nil {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
		return nil, fmt.Errorf("cloudflared: %w", err)
	}
	return &Tunnel{Provider: "cloudflare", URL: url, cmd: cmd}, nil
}

// startNgrok runs `ngrok http <port>` with JSON logs and reads the public URL
// from the "started tunnel" log line.
func startNgrok(ctx context.Context, port string) (*Tunnel, error) {
	bin, err := exec.LookPath("ngrok")
	if err != nil {
		return nil, fmt.Errorf("ngrok not found — install it (brew install ngrok) and run `ngrok config add-authtoken …`")
	}
	cmd := exec.CommandContext(ctx, bin, "http", port, "--log", "stdout", "--log-format", "json")
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

// scanForURLMerged scans multiple readers concurrently and returns the first
// extracted URL. Useful for cloudflared, which writes its quick-tunnel URL to
// stderr in current versions but may shift it to stdout in future releases.
func scanForURLMerged(readers []io.Reader, extract func(string) string) (string, error) {
	found := make(chan string, 1)
	var wg sync.WaitGroup
	for _, r := range readers {
		wg.Add(1)
		go func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				if u := extract(sc.Text()); u != "" {
					select {
					case found <- u:
					default:
					}
					// Keep draining so the child never blocks on a full pipe.
					go func() {
						for sc.Scan() {
						}
					}()
					return
				}
			}
		}(r)
	}
	go func() {
		wg.Wait()
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

// scanCloudflareReady reads cloudflared's combined stdout/stderr, extracts the
// quick-tunnel URL, and then waits for the "Registered tunnel connection" line
// that indicates the edge has accepted the tunnel and the hostname will resolve.
// It returns the URL as soon as it's ready, or returns the URL anyway after a
// short grace period so a future cloudflared log change doesn't hard-fail.
func scanCloudflareReady(stdout, stderr io.Reader) (string, error) {
	lines := make(chan string, 32)
	var wg sync.WaitGroup
	for _, r := range []io.Reader{stdout, stderr} {
		wg.Add(1)
		go func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				lines <- sc.Text()
			}
		}(r)
	}
	go func() {
		wg.Wait()
		close(lines)
	}()

	urlRe := regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	readyRe := regexp.MustCompile(`Registered tunnel connection`)

	urlSeen := make(chan string, 1)
	readySeen := make(chan struct{}, 1)
	// Drain + scan concurrently so the child never blocks on a full pipe.
	go func() {
		var url string
		for l := range lines {
			if u := urlRe.FindString(l); u != "" && url == "" {
				url = u
				urlSeen <- u
			}
			if readyRe.MatchString(l) && url != "" {
				select {
				case readySeen <- struct{}{}:
				default:
				}
			}
		}
	}()

	select {
	case u := <-urlSeen:
		// Wait for the ready signal, but don't fail if cloudflared's log
		// format changes and it never appears.
		select {
		case <-readySeen:
			return u, nil
		case <-time.After(10 * time.Second):
			return u, nil
		}
	case <-time.After(tunnelTimeout):
		return "", fmt.Errorf("no public URL after %s", tunnelTimeout)
	}
}
