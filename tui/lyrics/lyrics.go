// Package lyrics fetches song lyrics through pluggable providers.
//
// A Provider can be any backend: LRCLIB, YouTube Music, a local file, etc.
// A Registry holds registered providers and tries each in priority order.
// Core code asks the registry for lyrics rather than hardcoding LRCLIB + YTM.
//
// The default package-level Fetch function keeps old callers working with a
// default registry that includes LRCLIB and (if available) YouTube Music.
package lyrics

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// ErrNotFound means no registered provider returned lyrics for the track.
var ErrNotFound = errors.New("lyrics not found")

// Line is one synced lyric line at time T (seconds).
type Line struct {
	T    float64
	Text string
}

// Result holds synced (timestamped) lyrics and/or a plain-text version.
type Result struct {
	Synced []Line
	Plain  string
}

// Empty reports whether no lyrics were found.
func (r Result) Empty() bool {
	return len(r.Synced) == 0 && strings.TrimSpace(r.Plain) == ""
}

// Provider is one lyrics source.
type Provider interface {
	// Name returns a short identifier, e.g. "lrclib", "youtube".
	Name() string

	// Priority orders providers within a registry. Higher values run first.
	Priority() int

	// Fetch looks up lyrics for a track. album may be empty; duration is in
	// seconds and may be 0. Implementations should return an error only on a
	// real failure; "track not found" can be returned as an empty Result.
	Fetch(ctx context.Context, artist, track, album string, durationSec int) (Result, error)
}

// Registry holds lyrics providers and queries them in priority order.
// Zero value is usable but empty.
type Registry struct {
	providers []Provider
}

// NewRegistry creates an empty registry. Use Register to add providers.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a provider. Providers are sorted by priority (descending)
// so the highest priority is tried first.
func (r *Registry) Register(p Provider) {
	r.providers = append(r.providers, p)
	sort.Slice(r.providers, func(i, j int) bool {
		return r.providers[i].Priority() > r.providers[j].Priority()
	})
}

// Providers returns the registered providers in priority order.
func (r *Registry) Providers() []Provider {
	return append([]Provider(nil), r.providers...)
}

// Fetch tries each registered provider in priority order and returns the first
// non-empty result. If no provider is registered, or none find lyrics, it
// returns ErrNotFound.
func (r *Registry) Fetch(ctx context.Context, artist, track, album string, durationSec int) (Result, error) {
	if r == nil {
		return Result{}, ErrNotFound
	}
	for _, p := range r.providers {
		res, err := p.Fetch(ctx, artist, track, album, durationSec)
		if err != nil {
			continue // real failure: try next provider
		}
		if !res.Empty() {
			return res, nil
		}
	}
	return Result{}, ErrNotFound
}

// defaultRegistry is the package-level registry used by Fetch(). It is
// initialized lazily so callers can override it in tests or main.go.
var defaultRegistry = NewRegistry()

// SetDefault replaces the package-level default registry. Main programs use
// this once after constructing their real registry.
func SetDefault(reg *Registry) {
	if reg == nil {
		defaultRegistry = NewRegistry()
		return
	}
	defaultRegistry = reg
}

// Fetch uses the package-level default registry. It exists for backwards
// compatibility; new code should create a Registry and call Registry.Fetch.
func Fetch(artist, track, album string, durationSec int) (Result, error) {
	return defaultRegistry.Fetch(context.Background(), artist, track, album, durationSec)
}
