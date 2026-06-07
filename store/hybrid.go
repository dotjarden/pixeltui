package store

import (
	"pixeltui/lastfm"
)

// Hybrid implements engine.DataSource with a four-layer fallback chain:
//
//  1. Static graph (instant, offline, covers pre-crawled artists)
//  2. Fresh cache (fast, offline, covers recently queried artists)
//  3. Live Last.fm API (online only — result is written back to cache)
//  4. Stale cache (offline last resort — data may be expired but better than nothing)
//
// Any layer may be nil; the resolver skips it and tries the next.
type Hybrid struct {
	Graph   *GraphReader
	Cache   *Cache
	Live    *lastfm.Client
	Offline bool // if true, skip the live layer entirely
}

// ── engine.DataSource implementation ─────────────────────────────────────────

func (h *Hybrid) GetSimilarTracks(artist, track string, limit int) ([]lastfm.SimilarTrack, error) {
	// Graph doesn't store track-level similarity; skip to cache/live.
	if h.Cache != nil {
		if v, ok := h.Cache.GetSimilarTracks(artist, track, false); ok {
			return v, nil
		}
	}
	if h.Live != nil && !h.Offline {
		v, err := h.Live.GetSimilarTracks(artist, track, limit)
		if err == nil && h.Cache != nil {
			h.Cache.PutSimilarTracks(artist, track, v)
		}
		if err == nil {
			return v, nil
		}
	}
	// Stale cache as final fallback
	if h.Cache != nil {
		if v, ok := h.Cache.GetSimilarTracks(artist, track, true); ok {
			return v, nil
		}
	}
	return nil, nil // empty is fine; engine falls back to artist expansion
}

func (h *Hybrid) GetTrackTags(artist, track string) ([]string, error) {
	// Graph doesn't store per-track tags; skip to cache/live.
	if h.Cache != nil {
		if v, ok := h.Cache.GetTrackTags(artist, track, false); ok {
			return v, nil
		}
	}
	if h.Live != nil && !h.Offline {
		v, err := h.Live.GetTrackTags(artist, track)
		if err == nil && h.Cache != nil {
			h.Cache.PutTrackTags(artist, track, v)
		}
		if err == nil {
			return v, nil
		}
	}
	if h.Cache != nil {
		if v, ok := h.Cache.GetTrackTags(artist, track, true); ok {
			return v, nil
		}
	}
	return nil, nil // tags are optional; engine scores without them
}

func (h *Hybrid) GetSimilarArtists(artist string, limit int) ([]lastfm.SimilarArtist, error) {
	// 1. Graph
	if h.Graph != nil {
		if v, ok := h.Graph.GetSimilarArtists(artist, limit); ok {
			return v, nil
		}
	}
	// 2. Fresh cache
	if h.Cache != nil {
		if v, ok := h.Cache.GetSimilarArtists(artist, false); ok {
			return v, nil
		}
	}
	// 3. Live
	if h.Live != nil && !h.Offline {
		v, err := h.Live.GetSimilarArtists(artist, limit)
		if err == nil && h.Cache != nil {
			h.Cache.PutSimilarArtists(artist, v)
		}
		if err == nil {
			return v, nil
		}
	}
	// 4. Stale cache
	if h.Cache != nil {
		if v, ok := h.Cache.GetSimilarArtists(artist, true); ok {
			return v, nil
		}
	}
	return nil, nil
}

func (h *Hybrid) GetArtistTopTracks(artist string, limit int) ([]lastfm.TopTrack, error) {
	// 1. Graph
	if h.Graph != nil {
		if v, ok := h.Graph.GetArtistTopTracks(artist, limit); ok {
			return v, nil
		}
	}
	// 2. Fresh cache
	if h.Cache != nil {
		if v, ok := h.Cache.GetArtistTopTracks(artist, false); ok {
			return v, nil
		}
	}
	// 3. Live
	if h.Live != nil && !h.Offline {
		v, err := h.Live.GetArtistTopTracks(artist, limit)
		if err == nil && h.Cache != nil {
			h.Cache.PutArtistTopTracks(artist, v)
		}
		if err == nil {
			return v, nil
		}
	}
	// 4. Stale cache
	if h.Cache != nil {
		if v, ok := h.Cache.GetArtistTopTracks(artist, true); ok {
			return v, nil
		}
	}
	return nil, nil
}
