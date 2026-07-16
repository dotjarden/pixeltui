// Package source defines the central provider abstraction for pixeltui.
//
// Every music backend (YouTube Music, Subsonic, local files, and future
// Jellyfin/Plex/Navidrome/etc.) implements Provider. The server and TUI
// route all playback, search, artwork, and metadata requests through a
// Registry instead of branching on source-specific fields.
//
// The opaque stream ID grammar is source:ref, e.g. yt:abc123, su:12345,
// lo:<base64 path>. The registry parses the prefix and dispatches to the
// owning provider.
package source

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// ErrNotSupported is returned when a provider does not implement a given
// capability (charts, radio, artist pages, etc.). Callers should fall back
// or omit the feature quietly.
var ErrNotSupported = errors.New("not supported by this source")

// TrackInfo is source-specific metadata surfaced by /api/trackinfo and the
// TUI info overlay.
type TrackInfo struct {
	Title       string
	Artist      string
	Album       string
	DurationSec int
	ArtURL      string

	// Provider can return arbitrary extra metadata; the server normalizes it.
	Raw map[string]any
}

// ArtistRef is a lightweight, provider-specific artist handle used for
// BrowseArtist. The client never interprets the Ref string.
type ArtistRef struct {
	Name string
	Ref  string // provider-specific, e.g. YTM browse id
	Art  string
}

// AlbumRef is a lightweight, provider-specific album handle used for
// BrowseAlbum.
type AlbumRef struct {
	Title  string
	Artist string
	Year   string
	Ref    string // provider-specific
	Art    string
}

// ArtistPage is the normalized result of BrowseArtist.
type ArtistPage struct {
	Name           string
	Art            string
	Description    string
	MonthlyListeners int // best-effort; 0 when unknown
	TopSongs       []engine.Candidate
	Albums         []AlbumRef
	Singles        []AlbumRef
	Similar        []ArtistRef
	Tags           []string
	Bio            string
}

// AlbumPage is the normalized result of BrowseAlbum.
type AlbumPage struct {
	Title       string
	Artist      string
	Year        string
	Art         string
	Description string
	Explicit    bool
	Tracks      []engine.Candidate
}

// Capabilities describes what a provider can do for a specific track.
// This is sent to clients so the UI can adapt without knowing the source.
type Capabilities struct {
	StartStation bool   `json:"start_station"`
	GoToArtist   bool   `json:"go_to_artist"`
	GoToAlbum    bool   `json:"go_to_album"`
	Radio        bool   `json:"radio"`
	Download     bool   `json:"download"`
	Lyrics       bool   `json:"lyrics"`
	ShareURL     string `json:"share_url,omitempty"`
}

// Provider is the common interface every music source implements. Methods
// that are optional should return ErrNotSupported when unavailable.
type Provider interface {
	// Identity
	Key() string   // short id: "youtube", "subsonic", "local"
	Label() string // human label: "YouTube Music", "Subsonic"

	// Discovery / browse (optional)
	Search(ctx context.Context, query string, limit int) ([]engine.Candidate, error)
	SearchArtists(ctx context.Context, query string, limit int) ([]ArtistRef, error)
	SearchAlbums(ctx context.Context, query string, limit int) ([]AlbumRef, error)
	BrowseArtist(ctx context.Context, ref ArtistRef) (*ArtistPage, error)
	BrowseAlbum(ctx context.Context, ref AlbumRef) (*AlbumPage, error)
	Charts(ctx context.Context, country string, limit int) ([]engine.Candidate, error)
	Radio(ctx context.Context, seedID string, limit int, exclude []string) ([]engine.Candidate, error)

	// Playback / media
	StreamURL(ctx context.Context, id string) (string, error)          // direct URL if available
	ResolveStream(ctx context.Context, id string) (string, error)       // resolver path if no direct URL
	ArtURL(ctx context.Context, id string) (string, error)              // direct/proxied art URL
	TrackInfo(ctx context.Context, id string) (TrackInfo, error)        // metadata for /api/trackinfo
	Capabilities(ctx context.Context, id string) (Capabilities, error) // what this track supports

	// Library (optional)
	Liked(ctx context.Context) ([]engine.Candidate, error)
	Playlists(ctx context.Context) ([]string, error)
	PlaylistTracks(ctx context.Context, name string) ([]engine.Candidate, error)
}

// Registry holds all configured providers and dispatches by ID prefix.
type Registry struct {
	byKey   map[string]Provider
	order   []string // stable iteration order (registration order)
	prefixes map[string]string // prefix -> key, e.g. "yt" -> "youtube"
}

// NewRegistry builds an empty registry. Providers are registered separately so
// main.go can decide which sources are enabled.
func NewRegistry() *Registry {
	return &Registry{
		byKey:    make(map[string]Provider),
		prefixes: map[string]string{
			"yt": "youtube",
			"su": "subsonic",
			"lo": "local",
		},
	}
}

// Register adds a provider. Panics if the key or a supported prefix collides.
func (r *Registry) Register(p Provider) {
	key := p.Key()
	if _, ok := r.byKey[key]; ok {
		panic("source registry: duplicate key " + key)
	}
	r.byKey[key] = p
	r.order = append(r.order, key)
}

// ByKey returns a provider by its short key, or nil.
func (r *Registry) ByKey(key string) Provider { return r.byKey[key] }

// All returns providers in registration order.
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.byKey[k])
	}
	return out
}

// Keys returns registered keys in order.
func (r *Registry) Keys() []string { return append([]string(nil), r.order...) }

// SourceFor parses an opaque stream id and returns its owning provider.
// The prefix map is fixed: yt -> youtube, su -> subsonic, lo -> local.
func (r *Registry) SourceFor(id string) (Provider, string, bool) {
	i := strings.IndexByte(id, ':')
	if i <= 0 || i == len(id)-1 {
		return nil, "", false
	}
	prefix := id[:i]
	key, ok := r.prefixes[prefix]
	if !ok {
		return nil, prefix, false
	}
	return r.byKey[key], prefix, true
}

// StreamID is the value part of an id after the prefix.
func StreamID(id string) string {
	i := strings.IndexByte(id, ':')
	if i <= 0 || i == len(id)-1 {
		return ""
	}
	return id[i+1:]
}

// Downloader is an optional capability a Provider may implement for sources
// that can fetch a track to local storage (e.g. YouTube Music via yt-dlp).
type Downloader interface {
	Provider
	Download(ctx context.Context, id, destDir string) (string, error)
}

// Download dispatches to the provider that owns id and, if it implements
// Downloader, saves the track to destDir. Returns ErrNotSupported if the source
// cannot be downloaded.
func (r *Registry) Download(ctx context.Context, id, destDir string) (string, error) {
	p, prefix, ok := r.SourceFor(id)
	if !ok {
		return "", fmt.Errorf("unknown source for %q", id)
	}
	d, ok := p.(Downloader)
	if !ok {
		return "", fmt.Errorf("source %q: %w", prefix, ErrNotSupported)
	}
	return d.Download(ctx, StreamID(id), destDir)
}

// Charts returns the first successful non-empty chart list from a provider that
// supports charts. Providers that return ErrNotSupported are skipped.
func (r *Registry) Charts(ctx context.Context, country string, limit int) ([]engine.Candidate, error) {
	for _, k := range r.order {
		p := r.byKey[k]
		got, err := p.Charts(ctx, country, limit)
		if err != nil {
			if errors.Is(err, ErrNotSupported) {
				continue
			}
			return nil, err
		}
		if len(got) > 0 {
			return got, nil
		}
	}
	return nil, ErrNotSupported
}
