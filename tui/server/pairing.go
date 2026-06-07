package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// ── device token store ────────────────────────────────────────────────────────

type device struct {
	Token   string    `json:"token"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

// deviceStore persists paired-device tokens at <dataDir>/devices.json (0600).
type deviceStore struct {
	mu   sync.Mutex
	path string
	devs []device
}

func openDeviceStore(dataDir string) *deviceStore {
	ds := &deviceStore{path: filepath.Join(dataDir, "devices.json")}
	if b, err := os.ReadFile(ds.path); err == nil {
		json.Unmarshal(b, &ds.devs) //nolint:errcheck
	}
	return ds
}

func (ds *deviceStore) save() {
	b, err := json.MarshalIndent(ds.devs, "", "  ")
	if err != nil {
		return
	}
	tmp := ds.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, ds.path) //nolint:errcheck
	}
}

// valid reports whether token belongs to a paired device (constant-time).
func (ds *deviceStore) valid(token string) bool {
	if token == "" {
		return false
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for _, d := range ds.devs {
		if subtle.ConstantTimeCompare([]byte(d.Token), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

// add issues and persists a new device token.
func (ds *deviceStore) add(name string) string {
	tok := randHex(32)
	ds.mu.Lock()
	ds.devs = append(ds.devs, device{Token: tok, Name: name, Created: time.Now()})
	ds.save()
	ds.mu.Unlock()
	return tok
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

// randCode is a short, human-typable session pairing code (no ambiguous chars).
func randCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	rand.Read(b) //nolint:errcheck
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func constantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func osHostname() (string, error) { return os.Hostname() }

// printPairing shows the pairing QR + code at startup.
func (s *server) printPairing() {
	base := s.baseURL()
	payload := fmt.Sprintf("pixeltui://pair?url=%s&code=%s", url.QueryEscape(base), s.code)

	fmt.Printf("\n  \033[1mpixeltui server\033[0m — %s\n", s.cfg.Name)
	fmt.Printf("  Listening on %s   (LAN: %s)\n\n", s.cfg.Addr, base)
	fmt.Println("  Pair the app — scan this QR (or enter the URL + code):")
	fmt.Println()
	if q, err := qrcode.New(payload, qrcode.Medium); err == nil {
		fmt.Println(q.ToSmallString(false))
	}
	fmt.Printf("  URL:  %s\n", base)
	fmt.Printf("  Code: %s\n\n", s.code)
	fmt.Println("  From anywhere: put this behind a tunnel (e.g. Tailscale) and pass")
	fmt.Println("  its address with --url.   Ctrl-C to stop.")
	fmt.Println()
}

// ── SSE hub ───────────────────────────────────────────────────────────────────

type sseHub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newSSEHub() *sseHub { return &sseHub{clients: map[chan string]struct{}{}} }

func (h *sseHub) add() chan string {
	ch := make(chan string, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) remove(ch chan string) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}
