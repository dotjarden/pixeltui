// Package share abstracts share-URL resolvers so front-ends can turn a track
// into an outbound link (and later turn an inbound link back into a track)
// without hardcoding YouTube Music URLs.
//
// Resolvers are independent and optional: the registry walks them in order and
// uses the first one that claims it can handle a candidate or URL.
package share

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ErrNotResolved is returned when no resolver can convert a URL to a candidate.
var ErrNotResolved = errors.New("share URL could not be resolved")

// Resolver handles one share-URL format (YouTube Music, Subsonic, Spotify, etc.).
type Resolver interface {
	// Name identifies this resolver.
	Name() string

	// CanShare reports whether this resolver can produce a share URL for c.
	CanShare(c engine.Candidate) bool

	// ShareURL returns an outbound share URL for c. Called only when CanShare
	// is true, so it may assume a valid mapping exists.
	ShareURL(c engine.Candidate) string

	// CanResolve reports whether this resolver can turn url into a candidate.
	CanResolve(rawURL string) bool

	// Resolve turns an inbound share URL into a candidate. Returns ErrNotResolved
	// if the URL shape matches but the lookup fails; the registry will then try
	// the next resolver.
	Resolve(ctx context.Context, rawURL string) (engine.Candidate, error)
}

// Registry holds resolvers in registration order.
type Registry struct {
	resolvers []Resolver
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a resolver.
func (r *Registry) Register(res Resolver) {
	r.resolvers = append(r.resolvers, res)
}

// Names returns registered resolver names.
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

// ShareURL returns the first non-empty share URL from a resolver that CanShare.
func (r *Registry) ShareURL(c engine.Candidate) string {
	if r == nil {
		return ""
	}
	for _, res := range r.resolvers {
		if res.CanShare(c) {
			if u := res.ShareURL(c); u != "" {
				return u
			}
		}
	}
	return ""
}

// Resolve walks resolvers and returns the first successful candidate.
func (r *Registry) Resolve(ctx context.Context, rawURL string) (engine.Candidate, error) {
	if r == nil || len(r.resolvers) == 0 {
		return engine.Candidate{}, ErrNotResolved
	}
	var lastErr error
	for _, res := range r.resolvers {
		if !res.CanResolve(rawURL) {
			continue
		}
		c, err := res.Resolve(ctx, rawURL)
		if err != nil {
			lastErr = err
			continue
		}
		return c, nil
	}
	if lastErr != nil {
		return engine.Candidate{}, lastErr
	}
	return engine.Candidate{}, ErrNotResolved
}

// YouTubeMusicResolver produces music.youtube.com links and resolves them.
type YouTubeMusicResolver struct{}

func (YouTubeMusicResolver) Name() string { return "youtube-music" }

func (YouTubeMusicResolver) CanShare(c engine.Candidate) bool {
	return c.VideoID != "" && (c.Source == "" || c.Source == "youtube")
}

func (YouTubeMusicResolver) ShareURL(c engine.Candidate) string {
	if c.VideoID == "" {
		return ""
	}
	return "https://music.youtube.com/watch?v=" + c.VideoID
}

func (YouTubeMusicResolver) CanResolve(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "music.youtube.com", "www.youtube.com", "youtube.com", "youtu.be":
		return true
	}
	return false
}

func (YouTubeMusicResolver) Resolve(_ context.Context, rawURL string) (engine.Candidate, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return engine.Candidate{}, err
	}
	var id string
	switch u.Hostname() {
	case "music.youtube.com", "www.youtube.com", "youtube.com":
		id = u.Query().Get("v")
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	}
	if id == "" {
		return engine.Candidate{}, ErrNotResolved
	}
	return engine.Candidate{
		Source:  "youtube",
		VideoID: id,
		ArtURL:  "https://img.youtube.com/vi/" + id + "/0.jpg",
	}, nil
}
