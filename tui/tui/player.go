package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/lastfm"
	"github.com/dotjarden/pixeltui/tui/library"
	"github.com/dotjarden/pixeltui/tui/lyrics"
	"github.com/dotjarden/pixeltui/tui/player"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// This file is the TUI's thin glue over the headless player package: each cmd*
// turns a player call into a tea.Msg the model's Update loop consumes. The audio
// engine itself (mpv, IPC, resolution, gapless, cover art) lives in tui/player,
// so the pocket hardware client and the server can reuse it without the TUI.

// cmdPlay starts playback. preloadedURL (if set) is a direct CDN URL for an
// instant start. gen tags the resulting playOKMsg so the model can ignore a
// play that the user has already superseded.
func cmdPlay(c engine.Candidate, old *player.Stream, preloadedURL string, gen int) tea.Cmd {
	return func() tea.Msg {
		old.Stop()
		pb, enriched, err := player.Start(c, preloadedURL)
		if err != nil {
			return playErrMsg{err}
		}
		return playOKMsg{pb: pb, c: enriched, gen: gen}
	}
}

// waitMedia blocks on the stream's media channel, turning an OS / hardware
// transport command into a mediaMsg. Returns nil when there's no media channel
// (e.g. a non-mpv fallback player). Re-issue it after each command to keep
// listening; it reports closed=true when mpv exits.
func waitMedia(pb *player.Stream, gen int) tea.Cmd {
	if pb == nil || pb.Media() == nil {
		return nil
	}
	ch := pb.Media()
	return func() tea.Msg {
		c, ok := <-ch
		return mediaMsg{cmd: c, gen: gen, closed: !ok}
	}
}

// cmdPreload resolves the video id (if needed) and the CDN URL ahead of time so
// the next play starts near-instantly.
func cmdPreload(c engine.Candidate) tea.Cmd {
	return func() tea.Msg {
		// Direct-URL tracks (Subsonic) are already playable — just warm the cover.
		if c.StreamURL != "" {
			player.CoverFor(c.ArtURL)
			return preloadMsg{key: trackKey(c), c: c, url: c.StreamURL}
		}
		c = player.EnsureVideoID(c)
		key := trackKey(c)
		if c.VideoID == "" {
			return preloadMsg{key: key, c: c, err: fmt.Errorf("preload unavailable")}
		}
		url, err := player.Resolve(c.VideoID)
		player.CoverFor(c.ArtURL) // warm the pixelated cover so play time stays instant
		return preloadMsg{key: key, c: c, url: url, err: err}
	}
}

// cmdPoll samples player state every 500 ms (self-scheduling). gen identifies
// which track this poll belongs to so the model can drop polls from a replaced
// track.
func cmdPoll(pb *player.Stream, gen int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)
		if pb == nil || pb.Ended() {
			return pollMsg{ended: true, gen: gen}
		}
		return pollMsg{
			pos:     pb.Position(),
			dur:     pb.Duration(),
			paused:  pb.IsPaused(),
			vol:     pb.Volume(),
			plCurID: pb.CurrentEntryID(),
			gen:     gen,
		}
	}
}

// cmdGaplessSet reconciles mpv's playlist with the queue head: drops a stale
// appended entry (removeID) and/or appends next (already resolved to url) so
// the natural end-of-track boundary plays on inside the running mpv.
func cmdGaplessSet(pb *player.Stream, removeID int, next *engine.Candidate, url string, gen int) tea.Cmd {
	return func() tea.Msg {
		if next == nil {
			pb.Gapless(removeID, "", "", "")
			return gaplessMsg{gen: gen}
		}
		c := *next
		cover := player.CoverFor(c.ArtURL)
		id, err := pb.Gapless(removeID, url, c.Track+" — "+c.Artist, cover)
		return gaplessMsg{id: id, key: trackKey(c), c: c, gen: gen, err: err}
	}
}

// cmdLyrics fetches lyrics for a track from the model's registered lyric sources.
// It tries the registry first; if the registry is absent, it falls back to the
// legacy package-level Fetch (LRCLIB + YTM via SetDefault in main.go).
func (m model) cmdLyrics(c engine.Candidate, key string) tea.Cmd {
	return func() tea.Msg {
		var res lyrics.Result
		var err error
		if m.lyrics != nil {
			res, err = m.lyrics.Fetch(context.Background(), c.Artist, c.Track, c.Album, c.DurationSec)
		} else {
			res, err = lyrics.Fetch(c.Artist, c.Track, c.Album, c.DurationSec)
		}
		if err == nil && !res.Empty() {
			return lyricsMsg{key: key, synced: res.Synced, text: res.Plain}
		}
		// No lyrics from the registry. If this is a YouTube track, try a direct
		// video-id lookup as a last resort (the YTM lyrics provider cannot do this
		// on its own because Fetch doesn't receive the video id).
		if c.VideoID != "" {
			text, yerr := ytm.Lyrics(c.VideoID)
			if yerr == nil && text != "" {
				return lyricsMsg{key: key, text: text}
			}
		}
		return lyricsMsg{key: key, err: err}
	}
}

// cmdTrackInfo gathers every piece of metadata the local client can reach for
// a track: Last.fm stats, local listening history, lyrics availability, and
// source details. It mirrors what the server's /api/trackinfo endpoint returns,
// but composes it directly from the TUI's own connections.
func (m model) cmdTrackInfo(lfm *lastfm.Client, lib *library.Store, c engine.Candidate) tea.Cmd {
	return func() tea.Msg {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("%s — %s\n", c.Track, c.Artist))
		if c.Album != "" {
			b.WriteString(fmt.Sprintf("Album: %s\n", c.Album))
		}
		if c.DurationSec > 0 {
			b.WriteString(fmt.Sprintf("Duration: %d:%02d\n", c.DurationSec/60, c.DurationSec%60))
		}
		src := c.Source
		if src == "" {
			switch {
			case c.VideoID != "":
				src = "youtube"
			case c.Path != "":
				src = "local"
			default:
				src = "unknown"
			}
		}
		b.WriteString(fmt.Sprintf("Source: %s", src))
		if c.VideoID != "" {
			b.WriteString(fmt.Sprintf(" · video %s", c.VideoID))
		}
		b.WriteString("\n\n")

		if lfm != nil {
			if info, err := lfm.GetTrackInfo(c.Artist, c.Track); err == nil && info != nil {
				b.WriteString("Last.fm\n")
				if info.Playcount > 0 {
					b.WriteString(fmt.Sprintf("  Scrobbles: %s\n", fmtCount(info.Playcount)))
				}
				if info.Listeners > 0 {
					b.WriteString(fmt.Sprintf("  Listeners: %s\n", fmtCount(info.Listeners)))
				}
				if len(info.Tags) > 0 {
					b.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(info.Tags, ", ")))
				}
				if info.Album != "" {
					b.WriteString(fmt.Sprintf("  Album: %s\n", info.Album))
				}
				if info.Wiki != "" {
					b.WriteString(fmt.Sprintf("\n%s\n", strings.TrimSpace(info.Wiki)))
				}
				b.WriteString("\n")
			}
		}

		if lib != nil {
			if listens, err := lib.Listens(0); err == nil {
				var plays int
				var first, last time.Time
				for _, l := range listens {
					if !trackMatch(l.Candidate, c) {
						continue
					}
					plays++
					if first.IsZero() || l.At.Before(first) {
						first = l.At
					}
					if l.At.After(last) {
						last = l.At
					}
				}
				if plays > 0 {
					b.WriteString("Your History\n")
					b.WriteString(fmt.Sprintf("  Plays: %d\n", plays))
					if !first.IsZero() {
						b.WriteString(fmt.Sprintf("  First played: %s\n", first.Format("Jan _2 2006")))
					}
					if !last.IsZero() {
						b.WriteString(fmt.Sprintf("  Last played: %s\n", last.Format("Jan _2 2006")))
					}
					b.WriteString("\n")
				}
			}
		}

		var hasLyrics bool
		var lerr error
		var lres lyrics.Result
		if m.lyrics != nil {
			lres, lerr = m.lyrics.Fetch(context.Background(), c.Artist, c.Track, c.Album, c.DurationSec)
		} else {
			lres, lerr = lyrics.Fetch(c.Artist, c.Track, c.Album, c.DurationSec)
		}
		if lerr == nil && !lres.Empty() {
			hasLyrics = true
		} else if c.VideoID != "" {
			if text, yerr := ytm.Lyrics(c.VideoID); yerr == nil && text != "" {
				hasLyrics = true
			}
		}
		if hasLyrics {
			b.WriteString("Lyrics: available\n")
		} else {
			b.WriteString("Lyrics: not found\n")
		}

		return trackInfoMsg{c: c, content: b.String()}
	}
}

// trackMatch reports whether two candidates are the same track.
func trackMatch(a, b engine.Candidate) bool {
	if a.VideoID != "" && b.VideoID != "" {
		return a.VideoID == b.VideoID
	}
	return strings.EqualFold(a.Track, b.Track) && strings.EqualFold(a.Artist, b.Artist)
}

// cmdAutoQueue asks the recommendation registry for the next batch of tracks
// seeded by the current candidate. Any engine that CanSeed the candidate may
// respond; the registry falls back through Last.fm, YouTube radio, etc.
func (m model) cmdAutoQueue(seed engine.Candidate) tea.Cmd {
	return func() tea.Msg {
		if m.recommend == nil {
			return autoQueueMsg{}
		}
		ctx := context.Background()
		if !m.recommend.CanSeed(ctx, seed) {
			return autoQueueMsg{}
		}
		exclude := m.queueKeys()
		results, err := m.recommend.Recommend(ctx, seed, 12, exclude)
		if err != nil || len(results) == 0 {
			return autoQueueMsg{}
		}
		return autoQueueMsg{results: results}
	}
}

// queueKeys returns the trackKey set of every queued item, used to exclude
// duplicates from auto-queue recommendations.
func (m model) queueKeys() []string {
	items := m.queue.Items()
	out := make([]string, 0, len(items))
	for _, it := range items {
		switch v := it.(type) {
		case trackItem:
			out = append(out, trackKey(v.c))
		case *trackItem:
			out = append(out, trackKey(v.c))
		}
	}
	return out
}

// cmdMultiStation blends recommendations from several seed tracks (a playlist
// station). Results arrive as a normal auto-queue fill.
func (m model) cmdMultiStation(seeds []engine.Seed) tea.Cmd {
	return func() tea.Msg {
		if m.recommend == nil || len(seeds) == 0 {
			return autoQueueMsg{station: true}
		}
		seen := map[string]bool{}
		var all []engine.Candidate
		ctx := context.Background()
		for _, s := range seeds {
			seed := engine.Candidate{Artist: s.Artist, Track: s.Track}
			if !m.recommend.CanSeed(ctx, seed) {
				continue
			}
			res, err := m.recommend.Recommend(ctx, seed, 6, m.queueKeys())
			if err != nil {
				continue
			}
			for _, c := range res {
				k := trackKey(c)
				if seen[k] {
					continue
				}
				seen[k] = true
				all = append(all, c)
			}
		}
		if len(all) == 0 {
			return autoQueueMsg{station: true}
		}
		return autoQueueMsg{results: all, station: true}
	}
}

// cmdDiscoverRecs fetches recommendations to enrich the "For You" landing.
// Best-effort and async: a nil registry, missing engines, or any error just
// yields no recs (the landing keeps its local content).
func (m model) cmdDiscoverRecs(artist, track string) tea.Cmd {
	return func() tea.Msg {
		if m.recommend == nil || (artist == "" && track == "") {
			return discoverRecsMsg{}
		}
		seed := engine.Candidate{Artist: artist, Track: track}
		ctx := context.Background()
		if !m.recommend.CanSeed(ctx, seed) {
			return discoverRecsMsg{}
		}
		results, err := m.recommend.Recommend(ctx, seed, 12, nil)
		if err != nil || len(results) == 0 {
			return discoverRecsMsg{}
		}
		return discoverRecsMsg{recs: results}
	}
}

// cmdRadio is kept as a thin wrapper for callers that still pass a video id.
func cmdRadio(videoID string) tea.Cmd {
	return func() tea.Msg {
		if videoID == "" {
			return autoQueueMsg{}
		}
		results, err := ytm.Radio(videoID, 15)
		if err != nil || len(results) == 0 {
			return autoQueueMsg{}
		}
		return autoQueueMsg{results: results}
	}
}

// searchYTM runs a YouTube Music search (songs, not videos) and returns
// candidates pre-filled with video id, duration and album art.
func searchYTM(query string, limit int) ([]engine.Candidate, error) {
	return ytm.Search(query, limit)
}
