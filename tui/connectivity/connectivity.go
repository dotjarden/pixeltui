// Package connectivity tracks whether the device currently has working internet,
// so playback and UI can degrade gracefully when offline and resume when it
// returns. It's shared infrastructure: the pocket player gates streaming on it,
// and any client can subscribe to online/offline transitions.
package connectivity

import (
	"context"
	"net"
	"sync"
	"time"
)

// Monitor reports online/offline state, polling a probe and notifying
// subscribers on every change. Safe for concurrent use.
type Monitor struct {
	mu        sync.Mutex
	online    bool
	probe     func() bool
	interval  time.Duration
	tolerance int // consecutive failed probes before declaring offline
	subs      map[chan bool]struct{}
}

// New returns a Monitor. probe reports reachability (nil → a default DNS-port
// dial); interval is the poll period (<=0 → 10s). It assumes online until the
// first probe so playback isn't needlessly gated at startup.
func New(probe func() bool, interval time.Duration) *Monitor {
	if probe == nil {
		probe = defaultProbe
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Monitor{online: true, probe: probe, interval: interval, tolerance: 3, subs: map[chan bool]struct{}{}}
}

// Online reports the last known connectivity state.
func (m *Monitor) Online() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online
}

// Subscribe returns a channel that receives the new state on each change, plus
// an unsubscribe func.
func (m *Monitor) Subscribe() (<-chan bool, func()) {
	ch := make(chan bool, 1)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		if _, ok := m.subs[ch]; ok {
			delete(m.subs, ch)
			close(ch)
		}
		m.mu.Unlock()
	}
}

// set updates state and notifies subscribers only on an actual change.
func (m *Monitor) set(online bool) {
	m.mu.Lock()
	if online == m.online {
		m.mu.Unlock()
		return
	}
	m.online = online
	subs := make([]chan bool, 0, len(m.subs))
	for ch := range m.subs {
		subs = append(subs, ch)
	}
	m.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- online:
		default:
		}
	}
}

// Run probes until ctx is cancelled. Call once in a goroutine. It probes once
// promptly, then every interval. To avoid flapping on a brief blip (the Pi's
// WiFi is weak), it declares offline only after `tolerance` consecutive failed
// probes, but recovers online on the first success.
func (m *Monitor) Run(ctx context.Context) {
	if m.probe() {
		m.set(true) // confirm online fast; don't flip offline on one startup miss
	}
	fails := 0
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if m.probe() {
				fails = 0
				m.set(true)
				continue
			}
			fails++
			if fails >= m.tolerance {
				m.set(false)
			}
		}
	}
}

// defaultProbe reports reachability with a quick TCP dial. It tries a real
// streaming-class host on 443 first (exercising DNS + HTTPS, the path playback
// actually uses) and falls back to a raw DNS-port dial for networks that resolve
// oddly; online if either answers. Multiple targets mean one blocked/rate-limited
// endpoint can't, on its own, read as offline and silence playable audio.
func defaultProbe() bool {
	for _, addr := range []string{"www.google.com:443", "1.1.1.1:53"} {
		c, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
		if err == nil {
			c.Close() //nolint:errcheck
			return true
		}
	}
	return false
}
