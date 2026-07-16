// Package art resolves album-art URLs for a track through a chain of optional
// fallback providers. Front-ends request art without knowing which source
// supplied the track: each resolver is independent and may be registered or
// omitted.
//
// The source-aware resolver consults the central source registry; future
// resolvers can query Last.fm, embedded files, or a static placeholder.
package art

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"net/http"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ErrNoArt is returned when no resolver can find artwork for a candidate.
var ErrNoArt = errors.New("no artwork found")

// Candidate is the minimal art lookup key. It mirrors engine.Candidate so
// resolvers can avoid importing the engine package if they prefer.
type Candidate interface {
	Artist() string
	Album() string
}

// Resolver turns a track (artist/album plus any source-specific ids) into a
// direct image URL. Resolvers are independent; one failing does not stop the
// chain.
type Resolver interface {
	// Name identifies this resolver for logging and doctor output.
	Name() string

	// ArtURL returns a direct image URL for the candidate, or an empty string
	// with a nil error to indicate "not found but keep trying fallbacks".
	ArtURL(ctx context.Context, c engineCandidate) (string, error)
}

// engineCandidate is a thin wrapper so the public API can accept an
// engine.Candidate without importing engine in every resolver.
type engineCandidate struct {
	Artist string
	Album  string
	Track  string
	ArtURL string
	ID     string
}

// FromEngine builds an art candidate from engine.Candidate fields. Callers in
// the TUI/server use this; resolvers see only the simple engineCandidate.
func FromEngine(c engine.Candidate) engineCandidate {
	ec := engineCandidate{
		Artist: c.Artist,
		Album:  c.Album,
		Track:  c.Track,
		ArtURL: c.ArtURL,
	}
	// Reconstruct the opaque stream id so the source resolver can dispatch.
	switch c.Source {
	case "", "youtube":
		if c.VideoID != "" {
			ec.ID = "yt:" + c.VideoID
		}
	case "local":
		if c.Path != "" {
			ec.ID = "lo:" + base64.URLEncoding.EncodeToString([]byte(c.Path))
		}
	}
	return ec
}

// Registry holds resolvers in priority order (highest first). The zero value is
// empty but safe.
type Registry struct {
	resolvers []Resolver
}

// NewRegistry creates an empty art registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a resolver.
func (r *Registry) Register(res Resolver) {
	r.resolvers = append(r.resolvers, res)
}

// ArtURL walks the registered resolvers and returns the first non-empty URL.
// If every resolver returns empty/nil, it returns ErrNoArt.
func (r *Registry) ArtURL(ctx context.Context, c engineCandidate) (string, error) {
	if r == nil || len(r.resolvers) == 0 {
		return "", ErrNoArt
	}
	var lastErr error
	for _, res := range r.resolvers {
		url, err := res.ArtURL(ctx, c)
		if err != nil {
			lastErr = err
			continue
		}
		if url != "" {
			return url, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", ErrNoArt
}

// Names returns the registered resolver names.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, len(r.resolvers))
	for i, res := range r.resolvers {
		out[i] = res.Name()
	}
	return out
}

// FetchImage downloads and decodes an image URL (jpeg/png). Shared helper so
// the player and TUI don't each keep their own copy.
func FetchImage(ctx context.Context, url string) (image.Image, error) {
	if url == "" {
		return nil, fmt.Errorf("empty art URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	return img, err
}
