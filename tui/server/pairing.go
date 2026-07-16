package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// ── device token store ────────────────────────────────────────────────────────

// device is one paired client. Only the SHA-256 of its bearer token is kept,
// so a leaked devices.json can't be replayed as credentials.
type device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	TokenSHA string    `json:"token_sha256"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`

	// Token is the legacy plaintext field from pre-hashing versions; it's
	// migrated to TokenSHA (and blanked) the first time the store loads.
	Token string `json:"token,omitempty"`
}

// deviceStore persists paired-device records at <dataDir>/devices.json (0600).
type deviceStore struct {
	mu   sync.Mutex
	path string
	devs []device
}

func openDeviceStore(dataDir string) *deviceStore {
	ds := &deviceStore{path: filepath.Join(dataDir, "devices.json")}
	b, err := os.ReadFile(ds.path)
	if err != nil {
		return ds
	}
	json.Unmarshal(b, &ds.devs) //nolint:errcheck

	// Migrate legacy records: hash plaintext tokens, assign missing ids.
	changed := false
	for i := range ds.devs {
		if ds.devs[i].Token != "" && ds.devs[i].TokenSHA == "" {
			ds.devs[i].TokenSHA = hashToken(ds.devs[i].Token)
			ds.devs[i].Token = ""
			changed = true
		}
		if ds.devs[i].ID == "" {
			ds.devs[i].ID = randHex(4)
			changed = true
		}
	}
	if changed {
		ds.save()
	}
	return ds
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
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

// valid checks a presented token against the stored hashes (constant-time)
// and returns the matching device id. It refreshes the device's last-seen
// stamp at minute granularity.
func (ds *deviceStore) valid(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	h := hashToken(token)
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for i := range ds.devs {
		if subtle.ConstantTimeCompare([]byte(ds.devs[i].TokenSHA), []byte(h)) == 1 {
			if time.Since(ds.devs[i].LastSeen) > time.Minute {
				ds.devs[i].LastSeen = time.Now()
				ds.save()
			}
			return ds.devs[i].ID, true
		}
	}
	return "", false
}

// add issues a new device token. The plaintext token is returned exactly once
// (to the pairing client); only its hash is persisted.
func (ds *deviceStore) add(name string) (token, id string) {
	token = randHex(32)
	id = randHex(4)
	ds.mu.Lock()
	ds.devs = append(ds.devs, device{
		ID:       id,
		Name:     name,
		TokenSHA: hashToken(token),
		Created:  time.Now(),
		LastSeen: time.Now(),
	})
	ds.save()
	ds.mu.Unlock()
	return token, id
}

// revoke removes a paired device; its token stops working immediately.
func (ds *deviceStore) revoke(id string) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for i := range ds.devs {
		if ds.devs[i].ID == id {
			ds.devs = append(ds.devs[:i], ds.devs[i+1:]...)
			ds.save()
			return true
		}
	}
	return false
}

// deviceInfo is the client-safe listing entry (no token material).
type deviceInfo struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
	Current  bool      `json:"current"`
}

// list returns client-safe device records; current marks the caller's own.
func (ds *deviceStore) list(currentID string) []deviceInfo {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	out := make([]deviceInfo, 0, len(ds.devs))
	for _, d := range ds.devs {
		out = append(out, deviceInfo{
			ID: d.ID, Name: d.Name, Created: d.Created,
			LastSeen: d.LastSeen, Current: d.ID == currentID,
		})
	}
	return out
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

// pairingDetails is the single source of truth for terminal and desktop
// pairing surfaces. The payload is intentionally the same deep link the iOS
// app accepts when it scans a server QR.
func (s *server) pairingDetails() (base, code, link string) {
	base = s.baseURL()
	s.pairMu.Lock()
	code = s.code
	s.pairMu.Unlock()
	link = fmt.Sprintf("pixeltui://pair?url=%s&code=%s", url.QueryEscape(base), code)
	return base, code, link
}

// handlePairingInfo lets an already-paired host render the current pairing
// code and address in its own UI. It is authenticated because the code grants
// a new device access to the server.
func (s *server) handlePairingInfo(w http.ResponseWriter, _ *http.Request) {
	base, code, link := s.pairingDetails()
	writeJSON(w, map[string]string{"url": base, "code": code, "link": link})
}

// handlePairingQR renders the current pairing deep link as a PNG. Keeping QR
// generation in the host means desktop stays fully offline-capable and the QR
// always rotates with the server's single-use code.
func (s *server) handlePairingQR(w http.ResponseWriter, _ *http.Request) {
	_, _, link := s.pairingDetails()
	png, err := qrcode.Encode(link, qrcode.Medium, 480)
	if err != nil {
		http.Error(w, "could not encode pairing QR", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, private")
	_, _ = w.Write(png)
}

// printPairing shows the pairing QR + code at startup.
func (s *server) printPairing() {
	base, code, payload := s.pairingDetails()

	fmt.Printf("\n  \033[1mpixeltui server\033[0m — %s\n", s.cfg.Name)
	if s.currentURL() != "" {
		fmt.Printf("  Listening on %s   (public: %s · LAN: %s)\n\n", s.cfg.Addr, base, s.lanURL())
	} else {
		fmt.Printf("  Listening on %s   (LAN: %s)\n\n", s.cfg.Addr, base)
	}
	fmt.Println("  Pair the app — scan this QR (or enter the URL + code):")
	fmt.Println()
	if q, err := qrcode.New(payload, qrcode.Medium); err == nil {
		fmt.Println(q.ToSmallString(false))
	}
	fmt.Printf("  URL:  %s\n", base)
	fmt.Printf("  Code: %s\n\n", code)
	if s.currentURL() == "" {
		fmt.Println("  This address only works on your network. From anywhere:")
		fmt.Println("    pixeltui serve --tunnel tailscale     private mesh (recommended)")
		fmt.Println("    pixeltui serve --tunnel cloudflare    public URL, no account needed")
		fmt.Println("    pixeltui serve --tunnel ngrok         public URL, ngrok account")
		fmt.Println("  Save a default with `pixeltui setup` (Companion server step).")
	}
	fmt.Println("  Ctrl-C to stop.")
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

// broadcast sends msg to every connected client (drops it for slow ones —
// SSE here is a hint to refresh, not a reliable stream).
func (h *sseHub) broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}
