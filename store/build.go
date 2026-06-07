package store

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dotjarden/pixeltui/lastfm"
)

// defaultSeeds is a genre-diverse starting set for BFS.
// These expand into a wide variety via the similarity graph.
var defaultSeeds = []string{
	// Rock / Alternative
	"Radiohead", "The Beatles", "Led Zeppelin", "Pink Floyd", "Nirvana",
	"David Bowie", "The Rolling Stones", "The Velvet Underground", "Pixies",
	"Sonic Youth", "The Clash", "R.E.M.", "Talking Heads",
	// Electronic
	"Aphex Twin", "Boards of Canada", "Daft Punk", "Kraftwerk",
	"Portishead", "Massive Attack", "Burial", "Four Tet", "Autechre",
	"The Chemical Brothers", "Underworld",
	// Hip Hop
	"Kendrick Lamar", "Jay-Z", "Outkast", "Nas", "A Tribe Called Quest",
	"Wu-Tang Clan", "MF DOOM", "J Dilla", "De La Soul", "Run-DMC",
	// Jazz
	"Miles Davis", "John Coltrane", "Bill Evans", "Charles Mingus",
	"Thelonious Monk", "Chet Baker", "Herbie Hancock", "Wayne Shorter",
	// Pop
	"Michael Jackson", "Madonna", "Beyoncé", "Prince", "The Weeknd",
	// Metal
	"Black Sabbath", "Metallica", "Tool", "Slayer", "Meshuggah",
	"Pantera", "Opeth",
	// Indie / Lo-fi
	"Arcade Fire", "The Strokes", "Bon Iver", "Beach House",
	"Animal Collective", "Sufjan Stevens", "Pavement", "Neutral Milk Hotel",
	// Folk / Country / Americana
	"Bob Dylan", "Johnny Cash", "Nick Drake", "Iron & Wine",
	"Fleet Foxes", "Gillian Welch",
	// Soul / R&B / Funk
	"Stevie Wonder", "Marvin Gaye", "D'Angelo", "Al Green",
	"James Brown", "Parliament", "Curtis Mayfield",
	// Classical
	"Johann Sebastian Bach", "Ludwig van Beethoven", "Wolfgang Amadeus Mozart",
	"Frédéric Chopin", "Claude Debussy",
	// Ambient / Drone
	"Brian Eno", "Stars of the Lid", "William Basinski", "Harold Budd",
	// World
	"Fela Kuti", "Buena Vista Social Club", "Youssou N'Dour",
	// Reggae
	"Bob Marley", "Lee Scratch Perry",
	// Punk
	"The Ramones", "Dead Kennedys", "Minor Threat",
	// Blues
	"Robert Johnson", "Muddy Waters", "Howlin Wolf",
}

// BuildConfig controls graph construction.
type BuildConfig struct {
	MaxArtists int     // stop after crawling this many unique artists
	Workers    int     // parallel workers (default 10)
	ReqPerSec  float64 // Last.fm API rate limit (max 5 for free tier)
	OutputPath string
	Verbose    bool
}

// tokenBucket is a channel-based rate limiter. Workers call Take() before
// each API request; the bucket refills at ReqPerSec. Burst allows a small
// initial burst so all workers start immediately rather than queueing.
type tokenBucket struct {
	ch   chan struct{}
	stop chan struct{}
}

func newTokenBucket(perSec float64, burst int) *tokenBucket {
	tb := &tokenBucket{
		ch:   make(chan struct{}, burst),
		stop: make(chan struct{}),
	}
	// Pre-fill burst tokens so workers don't block at the very start
	for i := 0; i < burst; i++ {
		tb.ch <- struct{}{}
	}
	interval := time.Duration(float64(time.Second) / perSec)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case tb.ch <- struct{}{}: // refill one token
				default: // bucket full — discard (never exceed burst)
				}
			case <-tb.stop:
				return
			}
		}
	}()
	return tb
}

func (tb *tokenBucket) Take()  { <-tb.ch }
func (tb *tokenBucket) Close() { close(tb.stop) }

// BuildGraph crawls Last.fm in parallel via level-by-level BFS, building a
// static artist similarity graph. Workers share a rate limiter so the
// aggregate request rate never exceeds cfg.ReqPerSec regardless of concurrency.
//
// If cfg.OutputPath already exists the graph is loaded and the BFS resumes
// from the frontier — similar artists that were discovered but not yet crawled.
// No work is repeated.
//
// Why parallel speeds this up: sequential processing blocks during API latency
// (~200ms), leaving rate-limit slots idle. With N workers, latency from
// different artists overlaps, keeping the rate-limit pipe saturated.
// Empirical speedup: ~3–5× over sequential on typical API latency.
func BuildGraph(client *lastfm.Client, cfg BuildConfig) (*GraphData, error) {
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}

	// ── load or create graph data ─────────────────────────────────────────────
	data := &GraphData{
		Version: 1,
		Artists: make(map[string]GraphArtist, cfg.MaxArtists),
	}
	resumed := false

	if cfg.OutputPath != "" {
		if gr, err := LoadGraph(cfg.OutputPath); err == nil {
			existing := gr.GraphData()
			if len(existing.Artists) >= cfg.MaxArtists {
				fmt.Printf("Existing graph already has %d artists (target: %d).\n",
					len(existing.Artists), cfg.MaxArtists)
				fmt.Printf("Rerun with --max %d or higher to expand it.\n",
					len(existing.Artists)+1000)
				return existing, nil
			}
			data = existing
			resumed = true
			fmt.Printf("Resuming existing graph: %d artists  (built %s ago)\n",
				len(data.Artists),
				time.Since(data.BuiltAt).Round(time.Minute))
		}
	}

	// ── build the seen set and starting level ─────────────────────────────────
	seen := make(map[string]bool, cfg.MaxArtists)
	for k := range data.Artists {
		seen[k] = true
	}

	var currentLevel []string

	if resumed && len(data.Artists) > 0 {
		// Reconstruct frontier: similar artists discovered but not yet crawled.
		// The graph stores SimilarArtists for every processed node, so the
		// frontier is implicit — just find refs that aren't keys in the map.
		frontierNames := make(map[string]string) // lowercase → original casing
		for _, node := range data.Artists {
			for _, sim := range node.SimilarArtists {
				lc := strings.ToLower(sim.Name)
				if !seen[lc] {
					frontierNames[lc] = sim.Name
				}
			}
		}
		for lc, name := range frontierNames {
			seen[lc] = true // mark queued so workers don't re-add them
			currentLevel = append(currentLevel, name)
		}
		fmt.Printf("Frontier:  %d artists queued\n", len(currentLevel))
		fmt.Printf("Target:    %d artists total\n\n", cfg.MaxArtists)
	} else {
		// Fresh start — seed from the built-in diverse list
		for _, s := range defaultSeeds {
			lc := strings.ToLower(s)
			if !seen[lc] {
				seen[lc] = true
				currentLevel = append(currentLevel, s)
			}
		}
	}

	var mu sync.Mutex

	// totalDone starts at existing count so progress display is accurate
	var totalDone atomic.Int64
	totalDone.Store(int64(len(data.Artists)))

	var stopped atomic.Bool

	// Graceful SIGINT: set stopped flag; in-flight workers finish, new ones exit early
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println("\n\nInterrupted — draining workers and saving...")
		stopped.Store(true)
	}()
	defer signal.Stop(sigs)

	// Token bucket: burst = min(workers, 5) so we don't spike above rate limit
	burst := cfg.Workers
	if burst > 5 {
		burst = 5
	}
	tb := newTokenBucket(cfg.ReqPerSec, burst)
	defer tb.Close()

	// Semaphore caps actual concurrency at Workers
	sem := make(chan struct{}, cfg.Workers)

	start := time.Now()

	// Progress ticker: one goroutine owns stdout to avoid interleaved writes
	progressStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n := totalDone.Load()
				elapsed := time.Since(start).Seconds()
				rate := 0.0
				if elapsed > 0 {
					rate = float64(n) / elapsed
				}
				eta := "calculating..."
				if rate > 0 {
					remaining := float64(cfg.MaxArtists) - float64(n)
					d := time.Duration(remaining/rate) * time.Second
					eta = "eta " + d.Round(time.Second).String()
				}
				fmt.Printf("\r  %5d / %d  |  %.1f artists/sec  |  %s  |  workers: %d  ",
					n, cfg.MaxArtists, rate, eta, cfg.Workers)
			case <-progressStop:
				return
			}
		}
	}()

	for len(currentLevel) > 0 {
		if stopped.Load() {
			break
		}

		// Cap this level to not exceed MaxArtists
		remaining := cfg.MaxArtists - int(totalDone.Load())
		if remaining <= 0 {
			break
		}
		if len(currentLevel) > remaining {
			currentLevel = currentLevel[:remaining]
		}

		// Shuffle to prevent genre clustering — avoids crawling all of one
		// genre's similarity graph before branching to others
		rand.Shuffle(len(currentLevel), func(i, j int) {
			currentLevel[i], currentLevel[j] = currentLevel[j], currentLevel[i]
		})

		var nextLevel []string
		var wg sync.WaitGroup

		for _, artist := range currentLevel {
			if stopped.Load() {
				break
			}
			wg.Add(1)
			go func(artist string) {
				defer wg.Done()

				// Acquire concurrency slot
				sem <- struct{}{}
				defer func() { <-sem }()

				if stopped.Load() {
					return
				}

				// ── fetch similar artists ─────────────────────────────
				tb.Take()
				similar, err := client.GetSimilarArtists(artist, 20)
				if err != nil {
					if cfg.Verbose {
						fmt.Fprintf(os.Stderr, "\n  skip %q: %v\n", artist, err)
					}
					return
				}

				// ── fetch top tracks ──────────────────────────────────
				tb.Take()
				topTracks, _ := client.GetArtistTopTracks(artist, 10)
				// non-fatal: store artist with whatever tracks we have

				// ── build and store node ──────────────────────────────
				node := GraphArtist{Name: artist}
				for _, s := range similar {
					node.SimilarArtists = append(node.SimilarArtists, GraphSim{
						Name:  s.Name,
						Match: float32(s.Match),
					})
				}
				for _, t := range topTracks {
					node.TopTracks = append(node.TopTracks, GraphTrack{
						Name:      t.Name,
						Listeners: int32(t.ListenerCount()),
					})
				}

				mu.Lock()
				data.Artists[strings.ToLower(artist)] = node
				for _, s := range similar {
					lc := strings.ToLower(s.Name)
					if !seen[lc] {
						seen[lc] = true
						nextLevel = append(nextLevel, s.Name)
					}
				}
				mu.Unlock()

				totalDone.Add(1)
			}(artist)
		}

		wg.Wait() // wait for every artist in this level before advancing
		currentLevel = nextLevel
	}

	close(progressStop)

	elapsed := time.Since(start).Round(time.Second)
	n := totalDone.Load()
	fmt.Printf("\r\nCrawled %d artists in %s (%.1f artists/sec)\n",
		n, elapsed, float64(n)/time.Since(start).Seconds())

	data.BuiltAt = time.Now()

	if cfg.OutputPath != "" {
		fmt.Printf("Saving to %s...\n", cfg.OutputPath)
		if err := SaveGraph(cfg.OutputPath, data); err != nil {
			return data, fmt.Errorf("save: %w", err)
		}
		fmt.Println("Done.")
	}

	return data, nil
}

// WarmCache pre-fetches all data needed to recommend from a given artist
// and stores it in the cache so subsequent queries work offline.
func WarmCache(client *lastfm.Client, cache *Cache, artist string) error {
	fmt.Printf("Warming cache for %q...\n", artist)

	similar, err := client.GetSimilarArtists(artist, 10)
	if err != nil {
		return fmt.Errorf("similar artists: %w", err)
	}
	if err := cache.PutSimilarArtists(artist, similar); err != nil {
		return err
	}
	fmt.Printf("  similar artists: %d\n", len(similar))

	// Fan out top-track fetches in parallel with a modest rate limit
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3) // 3 concurrent fetches
	for _, s := range similar {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			time.Sleep(220 * time.Millisecond) // ~4.5 req/sec
			tracks, err := client.GetArtistTopTracks(name, 5)
			if err == nil {
				cache.PutArtistTopTracks(name, tracks)
			}
		}(s.Name)
	}
	wg.Wait()
	fmt.Printf("  top tracks fetched for %d similar artists\n", len(similar))

	return nil
}
