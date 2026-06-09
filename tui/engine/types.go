package engine

// Weights controls how each signal contributes to the final score.
// All weights should sum to 1.0 for interpretable scores.
type Weights struct {
	Similarity    float64 // Last.fm graph match score
	Popularity    float64 // anti-popularity: higher for niche artists
	ArtistNovelty float64 // 1.0 for different artist, 0.0 for seed-family tracks
	Serendipity   float64 // deterministic jitter to prevent total determinism
}

// DefaultWeights are tuned to surface discovery over same-artist safety.
//
// The ArtistNovelty signal is the key fix for the "filter niche" problem:
// without it, high-similarity same-artist tracks dominate because nothing
// rewards stepping outside the seed artist's orbit. With it, a track by a
// genuinely different artist at 0.75 similarity beats a same-artist track
// at 1.0 similarity — which is closer to how good human curation feels.
var DefaultWeights = Weights{
	Similarity:    0.40,
	Popularity:    0.20,
	ArtistNovelty: 0.35,
	Serendipity:   0.05,
}

// ExploreWeights returns weights for a given discovery level.
//
//	0  — safe:  similarity dominates, results stay close to the seed
//	5  — default: matches DefaultWeights exactly
//	10 — wild:  novelty and anti-popularity take over, genre-crossing results
//
// All returned weight sets sum to 1.0.
func ExploreWeights(level int) Weights {
	if level < 0 {
		level = 0
	}
	if level > 10 {
		level = 10
	}
	t := float64(level) / 10.0

	// Endpoints chosen so that t=0.5 (level 5) equals DefaultWeights exactly.
	safe := Weights{Similarity: 0.60, Popularity: 0.10, ArtistNovelty: 0.25, Serendipity: 0.05}
	wild := Weights{Similarity: 0.20, Popularity: 0.30, ArtistNovelty: 0.45, Serendipity: 0.05}

	lerp := func(a, b float64) float64 { return a + t*(b-a) }
	return Weights{
		Similarity:    lerp(safe.Similarity, wild.Similarity),
		Popularity:    lerp(safe.Popularity, wild.Popularity),
		ArtistNovelty: lerp(safe.ArtistNovelty, wild.ArtistNovelty),
		Serendipity:   lerp(safe.Serendipity, wild.Serendipity),
	}
}

// Signal is a single scored factor in a recommendation, exposed in dev mode.
type Signal struct {
	Name   string
	Raw    float64 // normalized 0–1 value
	Weight float64
	Score  float64 // Raw × Weight contribution to final score
	Note   string
}

// Candidate is a track under consideration for recommendation.
type Candidate struct {
	Track   string
	Artist  string
	Album   string // album name when known (YTM/Subsonic); blank otherwise
	Score   float64
	Path    string   // discovery path, shown in dev mode
	Signals []Signal // per-signal breakdown, populated when dev mode is on

	// Playback metadata — populated by the ytmusic layer, not the scorer.
	// Empty on candidates straight from the recommender; resolved lazily
	// at play/preload time.
	VideoID     string // YouTube Music video id (skips the yt-dlp search step)
	DurationSec int    // native track length in seconds (0 = unknown)
	ArtURL      string // album-art thumbnail URL (preferred over Last.fm)

	// Source/playback routing.
	Source    string // "youtube" (default/empty), "subsonic", "library"
	StreamURL string // direct, already-playable audio URL — when set, playback
	//                   uses it as-is and SKIPS yt-dlp resolution (e.g. Subsonic)
}
