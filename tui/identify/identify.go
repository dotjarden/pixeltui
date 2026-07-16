// Package identify provides modular audio recognition for pixeltui clients.
//
// Identifiers are independent and optional: the registry walks registered
// backends in order and returns the first confident match. Backends can include
// a local fingerprint index (built from the user's library) and AcoustID
// (web lookup). If no identifier is registered, iOS can hide the "Identify"
// option entirely.
package identify

import (
	"context"
	"errors"
	"fmt"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ErrNoMatch is returned when no identifier recognizes the fingerprint.
var ErrNoMatch = errors.New("no match found")

// Result is one recognized track.
type Result struct {
	Candidate engine.Candidate
	Score     float64 // confidence 0..1
	Source    string  // identifier name, e.g. "local" or "acoustid"
}

// Identifier turns an audio fingerprint + duration into a candidate.
type Identifier interface {
	// Name identifies this backend for status/doctor output.
	Name() string

	// Available reports whether this backend can be used right now.
	Available() bool

	// Identify returns the best match for the given fingerprint. A low
	// confidence or no match should return ErrNoMatch.
	Identify(ctx context.Context, fingerprint []int32, durationSec int) (Result, error)
}

// FingerprintIdentifier is an identifier that can accept the full Fingerprint
// value (including the compressed AcoustID form). The registry uses it when
// available, otherwise it falls back to the plain int32 signature.
type FingerprintIdentifier interface {
	Identifier
	IdentifyFingerprint(ctx context.Context, fp Fingerprint) (Result, error)
}

// Registry holds identifiers in priority order (highest first).
type Registry struct {
	identifiers []Identifier
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds an identifier.
func (r *Registry) Register(id Identifier) {
	r.identifiers = append(r.identifiers, id)
}

// Names returns the registered identifier names.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, len(r.identifiers))
	for i, id := range r.identifiers {
		out[i] = id.Name()
	}
	return out
}

// AvailableNames returns names of identifiers that report Available() == true.
func (r *Registry) AvailableNames() []string {
	if r == nil {
		return nil
	}
	var out []string
	for _, id := range r.identifiers {
		if id.Available() {
			out = append(out, id.Name())
		}
	}
	return out
}

// Identify walks identifiers and returns the first confident match. A match
// with Score <= 0 is treated as no match and the walk continues.
func (r *Registry) Identify(ctx context.Context, fingerprint []int32, durationSec int) (Result, error) {
	return r.IdentifyFingerprint(ctx, Fingerprint{DurationSec: durationSec, Data: fingerprint})
}

// IdentifyFingerprint walks identifiers with the full fingerprint value
// (including the compressed AcoustID form) and returns the first confident
// match.
func (r *Registry) IdentifyFingerprint(ctx context.Context, fp Fingerprint) (Result, error) {
	if r == nil || len(r.identifiers) == 0 {
		return Result{}, ErrNoMatch
	}
	var lastErr error
	for _, id := range r.identifiers {
		if !id.Available() {
			continue
		}
		var res Result
		var err error
		if fi, ok := id.(FingerprintIdentifier); ok {
			res, err = fi.IdentifyFingerprint(ctx, fp)
		} else {
			res, err = id.Identify(ctx, fp.Data, fp.DurationSec)
		}
		if err != nil {
			if errors.Is(err, ErrNoMatch) {
				continue
			}
			lastErr = err
			continue
		}
		if res.Score > 0 {
			return res, nil
		}
	}
	if lastErr != nil {
		return Result{}, lastErr
	}
	return Result{}, ErrNoMatch
}

// LocalIndex returns the first registered LocalIndex, if any. Useful for
// sharing the same index between the library store and the identify registry.
func (r *Registry) LocalIndex() *LocalIndex {
	if r == nil {
		return nil
	}
	for _, id := range r.identifiers {
		if li, ok := id.(*LocalIndex); ok {
			return li
		}
	}
	return nil
}

// RegistryFrom creates a ready-to-use registry from individual identifiers,
// skipping nil ones.
func RegistryFrom(ids ...Identifier) *Registry {
	r := NewRegistry()
	for _, id := range ids {
		if id != nil {
			r.Register(id)
		}
	}
	return r
}

// NoMatchErrorf wraps an underlying error as ErrNoMatch for logging.
func NoMatchErrorf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrNoMatch, fmt.Sprintf(format, a...))
}
