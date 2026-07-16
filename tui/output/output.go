// Package output abstracts audio output sinks so the player can pick the right
// mpv (or future non-mpv) backend without hardcoding --audio-device logic.
//
// Sinks are independent and optional: a zero or empty registry falls back to the
// default mpv output path. Providers register themselves in main.go, and the
// player asks the registry to apply the active sink to mpv's base args.
package output

import (
	"errors"
	"fmt"
)

// ErrNotSupported is returned when the requested sink key isn't registered.
var ErrNotSupported = errors.New("output sink not supported")

// Sink describes one audio output target. It may wrap an --audio-device name,
// a network renderer, or a future non-mpv backend.
type Sink interface {
	// Key is the short registry identifier, e.g. "default", "mpv-device".
	Key() string

	// Label is a human-readable name for menus and status output.
	Label() string

	// Available reports whether the sink can be used right now.
	Available() bool

	// Apply returns a copy of the player args with this sink's options added.
	Apply(args []string) []string
}

// Registry holds registered sinks. The zero value is empty but safe.
type Registry struct {
	byKey map[string]Sink
	order []string
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{byKey: make(map[string]Sink)}
}

// Register adds a sink. Panics if the key collides.
func (r *Registry) Register(s Sink) {
	if r.byKey == nil {
		r.byKey = make(map[string]Sink)
	}
	key := s.Key()
	if _, ok := r.byKey[key]; ok {
		panic("output registry: duplicate key " + key)
	}
	r.byKey[key] = s
	r.order = append(r.order, key)
}

// ByKey returns a sink by key, or nil.
func (r *Registry) ByKey(key string) Sink {
	if r == nil {
		return nil
	}
	return r.byKey[key]
}

// Keys returns registered sink keys in registration order.
func (r *Registry) Keys() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.order...)
}

// Default returns the first available registered sink, or the built-in mpv
// default sink if the registry is empty.
func (r *Registry) Default() Sink {
	for _, k := range r.order {
		if s := r.byKey[k]; s != nil && s.Available() {
			return s
		}
	}
	return Default()
}

// Apply looks up the named sink and applies it to args. An empty key uses the
// default sink. If the named sink is missing it returns ErrNotSupported.
func (r *Registry) Apply(key string, args []string) ([]string, error) {
	if r == nil || key == "" {
		return r.Default().Apply(args), nil
	}
	s := r.ByKey(key)
	if s == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotSupported, key)
	}
	return s.Apply(args), nil
}

// defaultSink is the fallback mpv output with no special args.
type defaultSink struct{}

func (defaultSink) Key() string   { return "default" }
func (defaultSink) Label() string { return "mpv default" }
func (defaultSink) Available() bool { return true }
func (defaultSink) Apply(args []string) []string { return args }

// Default returns the built-in mpv default sink.
func Default() Sink { return defaultSink{} }

// MPVDevice is an --audio-device sink for mpv.
type MPVDevice struct {
	Device string
}

func (s *MPVDevice) Key() string   { return "mpv-device" }
func (s *MPVDevice) Label() string { return s.Device }
func (s *MPVDevice) Available() bool {
	return s != nil && s.Device != ""
}
func (s *MPVDevice) Apply(args []string) []string {
	if !s.Available() {
		return args
	}
	return append(append([]string(nil), args...), "--audio-device="+s.Device)
}
