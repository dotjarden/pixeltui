package ytm

// Provider wraps the ytm package as a source.Provider. It exposes the YouTube
// Music backend through the common source abstraction so the server and TUI
// can route all music sources uniformly.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	dlpkg "github.com/dotjarden/pixeltui/tui/download"
	"github.com/dotjarden/pixeltui/tui/innertube"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/source"
)

// Provider implements source.Provider for YouTube Music.
type Provider struct {
	streamCache StreamURLCache // optional; nil in TUI, populated in server
	ytdlp       string         // optional yt-dlp binary path for downloads
}

// StreamURLCache is the minimal cache interface the provider uses for resolved
// stream URLs. *store.Cache satisfies this.
type StreamURLCache interface {
	GetStreamURL(key string) (string, bool)
	PutStreamURL(key, url string, expire int64)
}

// NewProvider builds a YouTube Music provider. cache may be nil.
func NewProvider(cache StreamURLCache) *Provider {
	return &Provider{streamCache: cache}
}

// WithDownloader enables local downloads through this provider using the given
// yt-dlp binary path.
func (p *Provider) WithDownloader(ytdlp string) *Provider {
	p.ytdlp = ytdlp
	return p
}

func (p *Provider) Key() string   { return "youtube" }
func (p *Provider) Label() string { return "YouTube Music" }

// Search implements source.Provider.
func (p *Provider) Search(ctx context.Context, query string, limit int) ([]engine.Candidate, error) {
	return Search(query, limit)
}

// SearchArtists implements source.Provider.
func (p *Provider) SearchArtists(ctx context.Context, query string, limit int) ([]source.ArtistRef, error) {
	hits, err := SearchArtists(query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]source.ArtistRef, 0, len(hits))
	for _, h := range hits {
		out = append(out, source.ArtistRef{Name: h.Name, Ref: h.BrowseID, Art: h.ArtURL})
	}
	return out, nil
}

// SearchAlbums implements source.Provider.
func (p *Provider) SearchAlbums(ctx context.Context, query string, limit int) ([]source.AlbumRef, error) {
	albums, err := SearchAlbums(query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]source.AlbumRef, 0, len(albums))
	for _, a := range albums {
		out = append(out, source.AlbumRef{
			Title:  a.Title,
			Artist: a.Artist,
			Year:   a.Year,
			Ref:    a.BrowseID,
			Art:    a.ArtURL,
		})
	}
	return out, nil
}

// BrowseArtist implements source.Provider.
func (p *Provider) BrowseArtist(ctx context.Context, ref source.ArtistRef) (*source.ArtistPage, error) {
	if ref.Ref == "" {
		return nil, fmt.Errorf("ytm BrowseArtist: empty browse id")
	}
	pg, err := BrowseArtist(ref.Ref)
	if err != nil {
		return nil, err
	}
	out := &source.ArtistPage{
		Name:        pg.Name,
		Art:         ref.Art,
		Description: pg.Description,
		TopSongs:    pg.TopSongs,
	}
	for _, a := range pg.Albums {
		out.Albums = append(out.Albums, source.AlbumRef{
			Title:  a.Title,
			Artist: a.Artist,
			Year:   a.Year,
			Ref:    a.BrowseID,
			Art:    a.ArtURL,
		})
	}
	for _, a := range pg.Singles {
		out.Singles = append(out.Singles, source.AlbumRef{
			Title:  a.Title,
			Artist: a.Artist,
			Year:   a.Year,
			Ref:    a.BrowseID,
			Art:    a.ArtURL,
		})
	}
	return out, nil
}

// BrowseAlbum implements source.Provider.
func (p *Provider) BrowseAlbum(ctx context.Context, ref source.AlbumRef) (*source.AlbumPage, error) {
	if ref.Ref == "" {
		return nil, fmt.Errorf("ytm BrowseAlbum: empty browse id")
	}
	album := Album{Title: ref.Title, Artist: ref.Artist, Year: ref.Year, BrowseID: ref.Ref, ArtURL: ref.Art}
	pg, err := BrowseAlbum(album, 0)
	if err != nil {
		return nil, err
	}
	return &source.AlbumPage{
		Title:       pg.Album.Title,
		Artist:      pg.Album.Artist,
		Year:        pg.Album.Year,
		Art:         pg.ArtURL,
		Description: pg.Description,
		Explicit:    pg.IsExplicit,
		Tracks:      pg.Tracks,
	}, nil
}

// Charts implements source.Provider.
func (p *Provider) Charts(ctx context.Context, country string, limit int) ([]engine.Candidate, error) {
	return Charts(country, limit)
}

// Radio implements source.Provider. seedID must be a YouTube video id (the
// value after the "yt:" prefix).
func (p *Provider) Radio(ctx context.Context, seedID string, limit int, exclude []string) ([]engine.Candidate, error) {
	return Radio(seedID, limit)
}

// StreamURL implements source.Provider. YouTube Music has no direct URL; use
// ResolveStream instead.
func (p *Provider) StreamURL(ctx context.Context, id string) (string, error) {
	return "", source.ErrNotSupported
}

// ResolveStream implements source.Provider. It returns a pre-signed m4a CDN
// URL for the video id, consulting the optional cache.
func (p *Provider) ResolveStream(ctx context.Context, id string) (string, error) {
	return p.resolveM4A(ctx, id)
}

// ArtURL implements source.Provider. YouTube thumbnails are public URLs.
func (p *Provider) ArtURL(ctx context.Context, id string) (string, error) {
	return "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg", nil
}

// TrackInfo implements source.Provider.
func (p *Provider) TrackInfo(ctx context.Context, id string) (source.TrackInfo, error) {
	c, err := TrackByVideoID(id)
	if err != nil {
		return source.TrackInfo{}, err
	}
	return source.TrackInfo{
		Title:       c.Track,
		Artist:      c.Artist,
		Album:       c.Album,
		DurationSec: c.DurationSec,
		ArtURL:      c.ArtURL,
	}, nil
}

// Capabilities implements source.Provider. All YouTube Music tracks support
// the same set of rich actions.
func (p *Provider) Capabilities(ctx context.Context, id string) (source.Capabilities, error) {
	return source.Capabilities{
		StartStation: true,
		GoToArtist:   true,
		GoToAlbum:    true,
		Radio:        true,
		Download:     true,
		Lyrics:       true,
		ShareURL:     "https://music.youtube.com/watch?v=" + id,
	}, nil
}

// Likked is not supported by the public ytmusic library; the server library
// provides likes.
func (p *Provider) Liked(ctx context.Context) ([]engine.Candidate, error) {
	return nil, source.ErrNotSupported
}

// Playlists is not supported by the public ytmusic library.
func (p *Provider) Playlists(ctx context.Context) ([]string, error) {
	return nil, source.ErrNotSupported
}

// PlaylistTracks is not supported by the public ytmusic library.
func (p *Provider) PlaylistTracks(ctx context.Context, name string) ([]engine.Candidate, error) {
	return nil, source.ErrNotSupported
}

// Download implements source.Downloader for YouTube Music tracks.
func (p *Provider) Download(ctx context.Context, id, destDir string) (string, error) {
	if p.ytdlp == "" {
		return "", fmt.Errorf("yt-dlp not configured")
	}
	return dlpkg.Track(p.ytdlp, WatchURL(id), destDir)
}

// resolveM4A returns a direct AAC/m4a CDN URL for a video id, consulting the
// optional cache. It mirrors the server-side logic but lives in the provider so
// future callers (server, TUI, pocket) can resolve through the same path.
func (p *Provider) resolveM4A(ctx context.Context, videoID string) (string, error) {
	key := innertube.CacheKey(videoID)
	if p.streamCache != nil {
		if u, ok := p.streamCache.GetStreamURL(key); ok {
			return u, nil
		}
	}

	// Fast path: native InnerTube VISIONOS resolution.
	if res, err := innertube.Resolve(ctx, videoID); err == nil && res.URL != "" {
		if p.streamCache != nil {
			p.streamCache.PutStreamURL(key, res.URL, res.Expire)
		}
		return res.URL, nil
	}

	// Fallback: yt-dlp resolution.
	u, err := resolveM4AWithYTDLP(videoID)
	if err != nil {
		return "", err
	}
	if p.streamCache != nil {
		p.streamCache.PutStreamURL(key, u, expireOf(u))
	}
	return u, nil
}

func resolveM4AWithYTDLP(videoID string) (string, error) {
	ydl := ytdlpPath()
	if ydl == "" {
		return "", fmt.Errorf("yt-dlp not found")
	}
	u, err := ytGetURL(ydl, videoID, "visionos")
	if err != nil {
		u, err = ytGetURL(ydl, videoID, "visionos,web")
	}
	return u, err
}

func ytdlpPath() string {
	if p := os.Getenv("PIXELTUI_YTDLP"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		venv := filepath.Join(home, ".pixeltui", "ytdlp-venv")
		cands := []string{filepath.Join(venv, "bin", "yt-dlp")}
		if runtime.GOOS == "windows" {
			cands = []string{filepath.Join(venv, "Scripts", "yt-dlp.exe")}
		}
		for _, c := range cands {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	return ""
}

func ytGetURL(ydl, videoID, clients string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ydl,
		"--extractor-args", "youtube:player_client="+clients,
		"--get-url", "-f", "bestaudio[ext=m4a]/bestaudio[acodec^=mp4a]",
		"--no-playlist", "--quiet",
		"https://music.youtube.com/watch?v="+videoID).Output()
	if err != nil {
		return "", fmt.Errorf("yt-dlp: %w", err)
	}
	u := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if u == "" {
		return "", fmt.Errorf("no m4a stream")
	}
	return u, nil
}

// expireOf reads the googlevideo `expire=` unix timestamp; falls back to +5h.
func expireOf(cdnURL string) int64 {
	i := strings.Index(cdnURL, "expire=")
	if i >= 0 {
		s := cdnURL[i+7:]
		if j := strings.IndexByte(s, '&'); j >= 0 {
			s = s[:j]
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	return time.Now().Add(5 * time.Hour).Unix()
}
