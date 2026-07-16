package art

import (
	"context"
	"errors"

	"github.com/dotjarden/pixeltui/tui/source"
)

// SourceResolver looks up artwork through the central source registry. It
// returns the candidate's existing ArtURL when present, otherwise asks the
// owning provider for a direct/proxied art URL.
type SourceResolver struct {
	Registry *source.Registry
}

func (r *SourceResolver) Name() string { return "source" }

func (r *SourceResolver) ArtURL(ctx context.Context, c engineCandidate) (string, error) {
	if c.ArtURL != "" {
		return c.ArtURL, nil
	}
	if r.Registry == nil || c.ID == "" {
		return "", nil
	}
	p, _, ok := r.Registry.SourceFor(c.ID)
	if !ok {
		return "", nil
	}
	url, err := p.ArtURL(ctx, source.StreamID(c.ID))
	if errors.Is(err, source.ErrNotSupported) {
		return "", nil
	}
	return url, err
}
