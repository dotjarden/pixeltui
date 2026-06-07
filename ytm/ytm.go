// Package ytm wraps the pure-Go YouTube Music client (raitonoberu/ytmusic),
// adapting its results to engine.Candidate so search, album art, duration, and
// "radio" recommendations all flow through the same type used by the TUI.
//
// It is metadata-only: it returns video ids, titles, durations and thumbnail
// URLs. Turning a video id into a playable audio stream is still yt-dlp's job
// (see the tui playback layer) — but because we already hold the video id, that
// step skips the slow YouTube *search* and only does extraction.
package ytm

import (
	"fmt"
	"strings"

	"github.com/raitonoberu/ytmusic"

	"pixeltui/engine"
)

// Search returns up to limit song results from YouTube Music for the query.
// Results carry VideoID, DurationSec and ArtURL so playback starts fast and
// album art / duration need no extra API calls.
func Search(query string, limit int) ([]engine.Candidate, error) {
	res, err := ytmusic.TrackSearch(query).Next()
	if err != nil {
		return nil, err
	}
	return fromTracks(res.Tracks, limit), nil
}

// Resolve finds the single best YouTube Music match for an artist/track pair.
// Used to enrich a recommender Candidate (which has no video id) just before
// playback. Returns the enriched candidate (VideoID/DurationSec/ArtURL filled).
func Resolve(artist, track string) (engine.Candidate, error) {
	query := strings.TrimSpace(track + " " + artist)
	res, err := ytmusic.TrackSearch(query).Next()
	if err != nil {
		return engine.Candidate{}, err
	}
	if len(res.Tracks) == 0 {
		return engine.Candidate{}, fmt.Errorf("no YouTube Music match for %q", query)
	}
	return fromTrack(res.Tracks[0]), nil
}

// Radio returns YouTube Music's "watch playlist" for a video id — its native
// radio/recommendation feed. Useful to augment the local recommender.
func Radio(videoID string, limit int) ([]engine.Candidate, error) {
	tracks, err := ytmusic.GetWatchPlaylist(videoID)
	if err != nil {
		return nil, err
	}
	return fromTracks(tracks, limit), nil
}

// Lyrics returns the plain-text lyrics for a video id ("" if none found).
func Lyrics(videoID string) (string, error) {
	if videoID == "" {
		return "", fmt.Errorf("no track")
	}
	return ytmusic.GetLyrics(videoID)
}

// ── mapping helpers ─────────────────────────────────────────────────────────

func fromTracks(tracks []*ytmusic.TrackItem, limit int) []engine.Candidate {
	out := make([]engine.Candidate, 0, len(tracks))
	for _, t := range tracks {
		if t == nil || t.VideoID == "" {
			continue
		}
		out = append(out, fromTrack(t))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func fromTrack(t *ytmusic.TrackItem) engine.Candidate {
	return engine.Candidate{
		Track:       t.Title,
		Artist:      joinArtists(t.Artists),
		Path:        "ytmusic",
		VideoID:     t.VideoID,
		DurationSec: t.Duration,
		ArtURL:      bestThumb(t.Thumbnails),
	}
}

func joinArtists(artists []ytmusic.Artist) string {
	names := make([]string, 0, len(artists))
	for _, a := range artists {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	return strings.Join(names, ", ")
}

// bestThumb returns the highest-resolution thumbnail URL.
func bestThumb(thumbs []ytmusic.Thumbnail) string {
	best := ""
	bestArea := 0
	for _, th := range thumbs {
		if area := th.Width * th.Height; area >= bestArea && th.URL != "" {
			bestArea = area
			best = th.URL
		}
	}
	return best
}

// WatchURL returns the canonical music.youtube.com URL for a video id, which
// yt-dlp / mpv resolve to an audio stream.
func WatchURL(videoID string) string {
	return "https://music.youtube.com/watch?v=" + videoID
}
