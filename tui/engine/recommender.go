package engine

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"

	"github.com/dotjarden/pixeltui/tui/lastfm"
)

// Recommender is the core engine. Create one per request; it holds no state between calls.
type Recommender struct {
	src     DataSource
	Weights Weights
	Dev     bool

	// DeepCuts skips the top 3 most-played tracks per artist expansion and
	// takes the next 5 instead — surfaces album cuts over radio staples.
	DeepCuts bool

	// ExcludeArtists is a set of lowercase artist names to omit entirely
	// from the candidate pool before scoring. Populated from --no-artist.
	ExcludeArtists map[string]bool

	// Affinity boosts candidates whose artist you've liked/played (lowercase
	// artist → 0..1). Populated from the local library so recs lean toward
	// what you already love. Applied as a small additive bonus when scoring.
	Affinity map[string]float64
}

// affinityWeight is how strongly library affinity nudges scores (kept modest so
// it biases, not dominates, the similarity-driven ranking).
const affinityWeight = 0.20

func New(src DataSource, dev bool) *Recommender {
	return &Recommender{
		src:     src,
		Weights: DefaultWeights,
		Dev:     dev,
	}
}

// SeedTags returns the genre/mood tags of the seed track, for display in dev mode.
func (r *Recommender) SeedTags(artist, track string) []string {
	tags, _ := r.src.GetTrackTags(artist, track)
	return tags
}

// Recommend returns the top n recommendations for the given seed track.
// It fans out multiple Last.fm API calls concurrently, scores candidates,
// applies per-artist diversity caps, and returns ranked results.
func (r *Recommender) Recommend(artist, track string, n int) ([]Candidate, error) {
	seed, err := r.fetchSeed(artist, track)
	if err != nil {
		return nil, err
	}

	candidates := r.buildCandidates(seed, artist, track)

	// Drop explicitly excluded artists before scoring
	if len(r.ExcludeArtists) > 0 {
		keep := candidates[:0]
		for _, c := range candidates {
			if !r.ExcludeArtists[strings.ToLower(c.Artist)] {
				keep = append(keep, c)
			}
		}
		candidates = keep
	}

	r.score(candidates, artist) // pass seed artist so novelty can be computed

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return selectTop(candidates, n, artist), nil
}

// Seed identifies one seed track for multi-seed recommendations.
type Seed struct {
	Artist string
	Track  string
}

// maxSeeds caps the per-station seed count: each seed costs ~13 Last.fm calls,
// and beyond 4 the blend stops adding meaningful variety.
const maxSeeds = 4

// RecommendMulti blends recommendations from several seeds (a playlist, your
// liked tracks, …) into one ranked list. Seeds are fetched concurrently;
// tracks surfaced by more than one seed get a confidence boost — that overlap
// is the strongest signal a track fits the blend. Per-artist results are
// capped at 2 for breadth. Partial seed failures are tolerated.
func (r *Recommender) RecommendMulti(seeds []Seed, n int) ([]Candidate, error) {
	if len(seeds) == 0 {
		return nil, fmt.Errorf("no seeds")
	}
	if len(seeds) > maxSeeds {
		seeds = seeds[:maxSeeds]
	}

	// The seeds themselves never come back as recommendations.
	seedKeys := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		seedKeys[trackKey(s.Artist, s.Track)] = true
	}

	per := n
	if per < 10 {
		per = 10
	}
	type result struct {
		cs  []Candidate
		err error
	}
	ch := make(chan result, len(seeds))
	for _, s := range seeds {
		go func(s Seed) {
			cs, err := r.Recommend(s.Artist, s.Track, per)
			ch <- result{cs, err}
		}(s)
	}

	byKey := map[string]*Candidate{}
	var order []string // first-seen order keeps merging deterministic
	var lastErr error
	for range seeds {
		res := <-ch
		if res.err != nil {
			lastErr = res.err
			continue
		}
		for i := range res.cs {
			c := res.cs[i]
			k := trackKey(c.Artist, c.Track)
			if seedKeys[k] {
				continue
			}
			if ex, ok := byKey[k]; ok {
				// Recommended off multiple seeds → boost, keep the higher score's path.
				ex.Score += 0.25 + 0.25*c.Score
				continue
			}
			cc := c
			byKey[k] = &cc
			order = append(order, k)
		}
	}
	if len(order) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no recommendations for these seeds")
	}

	merged := make([]Candidate, 0, len(order))
	for _, k := range order {
		merged = append(merged, *byKey[k])
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })

	const perArtistCap = 2
	count := map[string]int{}
	out := make([]Candidate, 0, n)
	for _, c := range merged {
		lc := strings.ToLower(c.Artist)
		if count[lc] >= perArtistCap {
			continue
		}
		count[lc]++
		out = append(out, c)
		if len(out) >= n {
			break
		}
	}
	return out, nil
}

// seedData holds the raw Last.fm responses gathered during phase 1.
type seedData struct {
	similarTracks  []lastfm.SimilarTrack
	seedTags       []string
	similarArtists []lastfm.SimilarArtist
}

// fetchSeed fires three API calls concurrently and collects results.
// If all three fail we surface the error; partial failures are tolerated.
func (r *Recommender) fetchSeed(artist, track string) (*seedData, error) {
	type stRes struct {
		v   []lastfm.SimilarTrack
		err error
	}
	type tagsRes struct {
		v   []string
		err error
	}
	type saRes struct {
		v   []lastfm.SimilarArtist
		err error
	}

	stCh := make(chan stRes, 1)
	tagsCh := make(chan tagsRes, 1)
	saCh := make(chan saRes, 1)

	go func() {
		v, err := r.src.GetSimilarTracks(artist, track, 50) // more direct candidates
		stCh <- stRes{v, err}
	}()
	go func() {
		v, err := r.src.GetTrackTags(artist, track)
		tagsCh <- tagsRes{v, err}
	}()
	go func() {
		v, err := r.src.GetSimilarArtists(artist, 15) // fetch more, expand top 10
		saCh <- saRes{v, err}
	}()

	st := <-stCh
	tags := <-tagsCh
	sa := <-saCh

	if st.err != nil && tags.err != nil && sa.err != nil {
		return nil, fmt.Errorf("could not find \"%s\" by %s on Last.fm", track, artist)
	}

	return &seedData{
		similarTracks:  st.v,
		seedTags:       tags.v,
		similarArtists: sa.v,
	}, nil
}

// rawCandidate holds pre-scoring data for a candidate track.
type rawCandidate struct {
	track     string
	artist    string
	match     float64 // Last.fm match/similarity score, 0–1
	listeners int     // -1 = unknown
	path      string
}

// buildCandidates assembles candidates from two sources:
//  1. Direct similar tracks (track.getSimilar on the seed)
//  2. Top tracks from similar artists (artist expansion)
//
// Phase 2 artist expansions fire concurrently. Seen-set deduplication
// ensures each track appears at most once in the pool.
func (r *Recommender) buildCandidates(seed *seedData, seedArtist, seedTrack string) []Candidate {
	seen := make(map[string]bool)
	seen[trackKey(seedArtist, seedTrack)] = true

	var raw []rawCandidate

	// Source 1: direct similar tracks — highest fidelity, no listener count available
	for _, t := range seed.similarTracks {
		k := trackKey(t.Artist.Name, t.Name)
		if seen[k] {
			continue
		}
		seen[k] = true
		raw = append(raw, rawCandidate{
			track:     t.Name,
			artist:    t.Artist.Name,
			match:     float64(t.Match),
			listeners: -1,
			path:      "direct similar",
		})
	}

	// Source 2: artist expansion — top tracks from similar artists,
	// with real listener counts for popularity scoring.
	// Expanding 10 artists concurrently gives a much broader candidate pool
	// with zero extra wall-clock time vs 5 (all calls run in parallel).
	topArtists := seed.similarArtists
	if len(topArtists) > 10 {
		topArtists = topArtists[:10]
	}

	type expansion struct {
		artist lastfm.SimilarArtist
		tracks []lastfm.TopTrack
	}
	ch := make(chan expansion, len(topArtists))
	for _, a := range topArtists {
		go func(a lastfm.SimilarArtist) {
			tracks, _ := r.src.GetArtistTopTracks(a.Name, 8) // 8 tracks each vs 5
			ch <- expansion{a, tracks}
		}(a)
	}
	for range topArtists {
		exp := <-ch

		tracks := exp.tracks
		if r.DeepCuts {
			// Skip the top 3 (the radio hits everyone knows) and take the next 5.
			// This surfaces album cuts and deep catalogue over chart staples.
			if len(tracks) > 3 {
				tracks = tracks[3:]
			} else {
				tracks = nil // artist only has hits — nothing to surface
			}
		}
		if len(tracks) > 5 {
			tracks = tracks[:5]
		}

		for _, t := range tracks {
			k := trackKey(t.Artist.Name, t.Name)
			if seen[k] {
				continue
			}
			seen[k] = true
			// Artist match is artist-level; scale down slightly for track-level confidence
			raw = append(raw, rawCandidate{
				track:     t.Name,
				artist:    t.Artist.Name,
				match:     float64(exp.artist.Match) * 0.72,
				listeners: t.ListenerCount(),
				path:      "via " + exp.artist.Name,
			})
		}
	}

	return toCandidates(raw)
}

func toCandidates(raw []rawCandidate) []Candidate {
	out := make([]Candidate, len(raw))
	for i, c := range raw {
		out[i] = Candidate{
			Track:  c.track,
			Artist: c.artist,
			Path:   c.path,
			// stash raw values in Signals; score() will replace them with full breakdown
			Signals: []Signal{
				{Name: "_match", Raw: c.match},
				{Name: "_listeners", Raw: float64(c.listeners)},
			},
		}
	}
	return out
}

// score computes the final score for each candidate.
// seedArtist is required so we can compute the ArtistNovelty signal.
func (r *Recommender) score(candidates []Candidate, seedArtist string) {
	for i := range candidates {
		c := &candidates[i]

		var match, listeners float64
		for _, s := range c.Signals {
			switch s.Name {
			case "_match":
				match = s.Raw
			case "_listeners":
				listeners = s.Raw
			}
		}

		popScore := popularityScore(int(listeners))
		novelty := artistNoveltyScore(c.Artist, seedArtist)
		jitter := serendipityJitter(c.Artist + "|" + c.Track)

		simTerm := match * r.Weights.Similarity
		popTerm := popScore * r.Weights.Popularity
		noveltyTerm := novelty * r.Weights.ArtistNovelty
		jitterTerm := jitter * r.Weights.Serendipity

		aff := r.Affinity[strings.ToLower(c.Artist)]
		affTerm := aff * affinityWeight

		c.Score = simTerm + popTerm + noveltyTerm + jitterTerm + affTerm

		if r.Dev {
			listenerNote := "unknown"
			if int(listeners) > 0 {
				listenerNote = formatCount(int(listeners)) + " listeners"
			}
			noveltyNote := "different artist"
			if novelty == 0 {
				noveltyNote = "seed artist / family — penalised"
			}
			c.Signals = []Signal{
				{
					Name:   "similarity",
					Raw:    match,
					Weight: r.Weights.Similarity,
					Score:  simTerm,
					Note:   fmt.Sprintf("Last.fm match %.3f", match),
				},
				{
					Name:   "popularity",
					Raw:    popScore,
					Weight: r.Weights.Popularity,
					Score:  popTerm,
					Note:   listenerNote,
				},
				{
					Name:   "novelty",
					Raw:    novelty,
					Weight: r.Weights.ArtistNovelty,
					Score:  noveltyTerm,
					Note:   noveltyNote,
				},
				{
					Name:   "serendipity",
					Raw:    jitter,
					Weight: r.Weights.Serendipity,
					Score:  jitterTerm,
					Note:   "deterministic hash jitter",
				},
			}
		}
	}
}

// selectTop picks the top n candidates with two diversity rules:
//
//  1. The seed artist and any "family" artists (e.g. collaborations that
//     contain the seed name like "MJ & Janet Jackson") share a single
//     combined cap of 2.
//
//  2. Every other artist gets a hard cap of 1 — forces true breadth.
//
// Because novelty is already a scored signal, same-artist tracks naturally
// rank lower before we even reach this filter. The caps are a final safety net.
func selectTop(candidates []Candidate, n int, seedArtist string) []Candidate {
	const seedFamilyCap = 2 // total for seed + all detected family acts
	const otherArtistCap = 1

	seedFamilyCount := 0
	otherCount := make(map[string]int) // lowercase artist → count

	result := make([]Candidate, 0, n)
	for _, c := range candidates {
		if len(result) >= n {
			break
		}
		if isSeedFamily(c.Artist, seedArtist) {
			if seedFamilyCount >= seedFamilyCap {
				continue
			}
			result = append(result, c)
			seedFamilyCount++
		} else {
			lc := strings.ToLower(c.Artist)
			if otherCount[lc] >= otherArtistCap {
				continue
			}
			result = append(result, c)
			otherCount[lc]++
		}
	}
	return result
}

// isSeedFamily reports whether candidateArtist is the seed artist or a
// detected family/collaboration act.
//
// Strategy: extract "significant" words from the seed name (length > 3,
// which filters out "The", "And", "feat" etc.) and require ALL of them to
// appear in the candidate name. This catches:
//   - Exact matches               "Michael Jackson" == "Michael Jackson"
//   - Collaboration credits       "Michael Jackson & Paul McCartney"
//   - Features                    "Michael Jackson feat. Eddie Van Halen"
//
// It intentionally does NOT catch loosely related acts like "The Jacksons"
// or "Jackson Browne" — those are genuinely different artists and should
// each get their own cap slot.
func isSeedFamily(candidateArtist, seedArtist string) bool {
	cLower := strings.ToLower(candidateArtist)
	sLower := strings.ToLower(seedArtist)

	if cLower == sLower {
		return true
	}

	// Extract significant words from seed (len > 3 drops "The", "And", "De", etc.)
	var keywords []string
	for _, w := range strings.Fields(sLower) {
		// Strip common punctuation
		w = strings.Trim(w, ".,&-'\"")
		if len(w) > 3 {
			keywords = append(keywords, w)
		}
	}

	if len(keywords) == 0 {
		// Single very-short name (e.g. "Air") — only exact match counts
		return false
	}

	// ALL keywords must appear in the candidate name
	for _, kw := range keywords {
		if !strings.Contains(cLower, kw) {
			return false
		}
	}
	return true
}

// artistNoveltyScore returns 1.0 for a genuinely different artist and 0.0
// for the seed artist or a detected family act.
func artistNoveltyScore(candidateArtist, seedArtist string) float64 {
	if isSeedFamily(candidateArtist, seedArtist) {
		return 0.0
	}
	return 1.0
}

// trackKey produces a stable, case-insensitive dedup key.
func trackKey(artist, track string) string {
	return strings.ToLower(artist) + "§" + strings.ToLower(track)
}

// popularityScore returns 1.0 for niche artists, 0.0 for mega-popular ones.
// Log scale: ~50M listeners → 0.0, ~1K listeners → ~1.0.
// Listeners = -1 (unknown) → 0.5 neutral.
func popularityScore(listeners int) float64 {
	if listeners <= 0 {
		return 0.5
	}
	const maxListeners = 50_000_000.0
	score := 1.0 - math.Log10(float64(listeners))/math.Log10(maxListeners)
	return math.Max(0, math.Min(1, score))
}

// serendipityJitter returns a deterministic 0–1 float derived from the track identity.
// Prevents identical-scoring candidates from always appearing in the same order,
// and adds a tiny unpredictability that makes repeated queries feel fresh.
func serendipityJitter(s string) float64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return float64(h.Sum32()%1000) / 1000.0
}

// formatCount formats listener counts as "1.2M", "345K", etc.
func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
