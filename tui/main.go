package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/mattn/go-isatty"

	"github.com/dotjarden/pixeltui/tui/config"
	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/lastfm"
	"github.com/dotjarden/pixeltui/tui/library"
	"github.com/dotjarden/pixeltui/tui/scrobble"
	"github.com/dotjarden/pixeltui/tui/server"
	"github.com/dotjarden/pixeltui/tui/store"
	"github.com/dotjarden/pixeltui/tui/subsonic"
	"github.com/dotjarden/pixeltui/tui/tui"
)

// version is the build version, injected at release time via
// -ldflags "-X main.version=vX.Y.Z". "dev" for local/source builds.
var version = "dev"

// dataDir returns (and creates) ~/.pixeltui/. If the legacy ~/.musicrec exists
// and the new dir doesn't, it's migrated in place so the pre-built artist graph,
// cache, mpv bundle and yt-dlp venv are preserved across the rebrand.
func dataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".pixeltui")
	legacy := filepath.Join(home, ".musicrec")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if _, err := os.Stat(legacy); err == nil {
			if err := os.Rename(legacy, dir); err == nil {
				// A Python venv hardcodes absolute paths and can't be relocated;
				// drop it so we fall back to PATH yt-dlp until it's rebuilt.
				os.RemoveAll(filepath.Join(dir, "ytdlp-venv")) //nolint:errcheck
				fmt.Fprintln(os.Stderr, "migrated ~/.musicrec → ~/.pixeltui")
				fmt.Fprintln(os.Stderr, "note: re-run 'make fast-ytdlp' (venv) and 'make stream-setup' (mpv) — they don't survive the move")
			}
		}
	}
	return dir, os.MkdirAll(dir, 0755)
}

func main() {
	if len(os.Args) > 1 {
		// Accept subcommands with or without leading dashes (doctor / --doctor).
		switch strings.TrimLeft(os.Args[1], "-") {
		case "doctor":
			cmdDoctor(os.Args[2:])
			return
		case "setup":
			cmdSetup(os.Args[2:])
			return
		case "scrobble-auth", "lastfm-auth":
			cmdScrobbleAuth(os.Args[2:])
			return
		case "reset":
			cmdReset(os.Args[2:])
			return
		case "uninstall":
			cmdUninstall(os.Args[2:])
			return
		case "export":
			cmdExport(os.Args[2:])
			return
		case "update", "upgrade", "self-update":
			cmdUpdate(os.Args[2:])
			return
		case "serve":
			cmdServe(os.Args[2:])
			return
		case "devices":
			cmdDevices(os.Args[2:])
			return
		case "build-graph":
			cmdBuildGraph(os.Args[2:])
			return
		case "cache":
			cmdCache(os.Args[2:])
			return
		case "help", "h":
			printUsage()
			return
		case "version", "v":
			fmt.Println("pixeltui " + version)
			return
		}
	}
	cmdRecommend(os.Args[1:])
}

// ── recommend ─────────────────────────────────────────────────────────────────

func cmdRecommend(args []string) {
	fs := flag.NewFlagSet("pixeltui", flag.ExitOnError)
	artistFlag := fs.String("artist", "", "Artist name")
	trackFlag := fs.String("track", "", "Track/song name")
	keyFlag := fs.String("key", "", "Last.fm API key (or LASTFM_API_KEY env var)")
	countFlag := fs.Int("n", 10, "Number of recommendations")
	devFlag := fs.Bool("dev", false, "Show per-signal scoring breakdown")
	offlineFlag := fs.Bool("offline", false, "Never hit the live API (cache/graph only)")
	graphFlag := fs.String("graph", "", "Path to graph file (default: ~/.pixeltui/graph.bin)")
	cacheFlag := fs.String("cache", "", "Path to cache file (default: ~/.pixeltui/cache.db)")
	exploreFlag := fs.Int("explore", 5, "Discovery level: 0 = safe/similar, 10 = wild/genre-crossing")
	deepCutsFlag := fs.Bool("deep-cuts", false, "Skip top hits per artist; surface album cuts instead")
	noArtistFlag := fs.String("no-artist", "", "Comma-separated artists to exclude (e.g. \"Adele,Coldplay\")")
	noTUIFlag := fs.Bool("no-tui", false, "Print plain list without the interactive browser")
	fs.Usage = printUsage
	fs.Parse(args)

	// Resolve data paths
	dir, err := dataDir()
	if err != nil {
		fatalf("could not create data dir: %v", err)
	}

	// Config file (~/.pixeltui/config.json) merged with env overrides.
	cfg, _ := config.Load(dir)

	// First launch with no config + a real terminal + no seed → guided setup,
	// then reload so the rest of this run sees the new settings.
	if !*noTUIFlag && len(fs.Args()) == 0 && *artistFlag == "" && *trackFlag == "" {
		if _, statErr := os.Stat(config.Path(dir)); os.IsNotExist(statErr) &&
			isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd()) {
			maybeOnboard(dir)
			cfg, _ = config.Load(dir)
		}
	}

	// API key precedence: --key  >  env/file (config already merged both).
	apiKey := *keyFlag
	if apiKey == "" {
		apiKey = cfg.LastfmKey
	}
	graphPath := *graphFlag
	if graphPath == "" {
		graphPath = filepath.Join(dir, "graph.bin")
	}
	cachePath := *cacheFlag
	if cachePath == "" {
		cachePath = filepath.Join(dir, "cache.db")
	}

	// Build hybrid data source
	h := &store.Hybrid{Offline: *offlineFlag}

	// Layer 1: static graph
	if gr, err := store.LoadGraph(graphPath); err == nil {
		h.Graph = gr
		if *devFlag {
			fmt.Printf("  [graph]  %d artists  (built %s ago)\n",
				gr.ArtistCount(), fmtAge(gr.BuiltAt()))
		}
	}

	// Layer 2+3: cache + live API
	var cache *store.Cache
	if c, err := store.OpenCache(cachePath); err == nil {
		cache = c
		h.Cache = c
		defer c.Close()
	}
	if apiKey != "" && !*offlineFlag {
		h.Live = lastfm.NewClient(apiKey)
	}

	// Local library (likes/playlists/history/resume) — open best-effort.
	lib, _ := library.Open(dir)

	// Scrobbling (Last.fm / ListenBrainz) — the client is built whenever creds
	// are configured so the in-app settings toggle works without a restart; it
	// only submits when enabled.
	var scrob *scrobble.Scrobbler
	if cfg.ScrobbleReady() && !*offlineFlag {
		var lf *scrobble.Lastfm
		if cfg.LastfmScrobbleReady() {
			lf = scrobble.NewLastfm(cfg.LastfmKey, cfg.Scrobble.LastfmSecret, cfg.Scrobble.LastfmSession)
		}
		var lb *scrobble.ListenBrainz
		if cfg.Scrobble.ListenBrainzToken != "" {
			lb = scrobble.NewListenBrainz(cfg.Scrobble.ListenBrainzToken)
		}
		if scrob = scrobble.New(lf, lb, dir); scrob != nil && cfg.Scrobble.Enabled {
			go scrob.RetrySpool() // deliver any backlog from offline sessions
		}
	}

	// Optional Subsonic/Navidrome source (config file or env).
	var sub *subsonic.Client
	if cfg.HasSubsonic() && !*offlineFlag {
		sub = subsonic.NewClient(cfg.Subsonic.URL, cfg.Subsonic.User, cfg.Subsonic.Pass)
	}

	// Validate we have at least one data source
	if h.Graph == nil && h.Cache == nil && h.Live == nil {
		fatalf("no data source available.\n" +
			"  Set LASTFM_API_KEY for live lookups, or\n" +
			"  run `pixeltui build-graph` to build a local graph first.")
	}
	if h.Live == nil && !*offlineFlag {
		fmt.Fprintln(os.Stderr, "warning: no API key set — using local data only")
	}

	// Resolve track + artist
	artistName, trackName := *artistFlag, *trackFlag
	if artistName == "" && trackName == "" {
		rest := fs.Args()
		if len(rest) >= 2 {
			trackName = rest[0]
			artistName = rest[1]
		}
	}

	// Build recommender (needed in all paths).
	rec := engine.New(h, *devFlag)

	// Learn from your library: bias recommendations toward artists you've liked.
	if lib != nil {
		aff := map[string]float64{}
		for _, c := range lib.Liked() {
			if c.Artist != "" {
				aff[strings.ToLower(c.Artist)] = 1.0
			}
		}
		if len(aff) > 0 {
			rec.Affinity = aff
		}
	}

	// No seed given — open TUI directly in browse/search mode.
	if trackName == "" && artistName == "" {
		tui.Run(tui.Config{
			Header:        "♫  pixeltui",
			Dev:           *devFlag,
			Rec:           rec,
			URLCache:      cache,
			Library:       lib,
			Subsonic:      sub,
			LocalDirs:     cfg.LocalDirs,
			DownloadDir:   cfg.DownloadDir,
			Theme:         cfg.Theme,
			DataDir:       dir,
			ChartsGlobal:  cfg.Charts.Global,
			ChartsCountry: cfg.Charts.Country,
			Explore:       cfg.Explore,
			SeekStep:      cfg.SeekStep,
			Scrobbler:     scrob,
			ScrobbleOn:    cfg.Scrobble.Enabled,
			Lastfm:        h.Live,
		})
		return
	}

	if trackName == "" || artistName == "" {
		scanner := bufio.NewScanner(os.Stdin)
		if trackName == "" {
			fmt.Print("Track name: ")
			scanner.Scan()
			trackName = strings.TrimSpace(scanner.Text())
		}
		if artistName == "" {
			fmt.Print("Artist name: ")
			scanner.Scan()
			artistName = strings.TrimSpace(scanner.Text())
		}
	}
	if trackName == "" || artistName == "" {
		fatalf("track and artist are both required")
	}

	// Explore dial: level 5 = DefaultWeights; deviate only when explicitly set.
	if *exploreFlag != 5 {
		rec.Weights = engine.ExploreWeights(*exploreFlag)
	}

	// Deep cuts: skip the most-played tracks per artist expansion.
	rec.DeepCuts = *deepCutsFlag

	// Negative artist filter: comma-separated list, case-insensitive.
	if *noArtistFlag != "" {
		rec.ExcludeArtists = make(map[string]bool)
		for _, a := range strings.Split(*noArtistFlag, ",") {
			rec.ExcludeArtists[strings.ToLower(strings.TrimSpace(a))] = true
		}
	}

	fmt.Printf("\nSearching for \"%s\" by %s...\n\n", trackName, artistName)

	results, err := rec.Recommend(artistName, trackName, *countFlag)
	if err != nil {
		fatalf("%v", err)
	}
	if len(results) == 0 {
		fmt.Println("No recommendations found.")
		fmt.Println("Try building the graph (`pixeltui build-graph`) or check the spelling.")
		os.Exit(0)
	}

	header := fmt.Sprintf("Recommendations for \"%s\" by %s", trackName, artistName)
	if *exploreFlag != 5 {
		header += fmt.Sprintf("  [explore %d/10]", *exploreFlag)
	}
	if *deepCutsFlag {
		header += "  [deep cuts]"
	}

	// Collect seed tags for dev mode display.
	var seedTags []string
	if *devFlag {
		seedTags = rec.SeedTags(artistName, trackName)
	}

	if *noTUIFlag {
		// Plain output (also used when stdout is piped).
		fmt.Println(header)
		fmt.Println(strings.Repeat("─", len(header)))
		fmt.Println()
		if *devFlag && len(seedTags) > 0 {
			fmt.Printf("  Seed tags: %s\n\n", strings.Join(seedTags, ", "))
		}
		for i, r := range results {
			if *devFlag {
				printDevResult(i+1, r)
			} else {
				fmt.Printf("  %2d.  %s — %s\n", i+1, r.Track, r.Artist)
			}
		}
		fmt.Println()
		return
	}

	// Interactive TUI (falls back to plain list if stdout is not a terminal).
	tui.Run(tui.Config{
		Header:        header,
		SeedTags:      seedTags,
		Results:       results,
		Dev:           *devFlag,
		Rec:           rec,
		URLCache:      cache,
		Library:       lib,
		Subsonic:      sub,
		LocalDirs:     cfg.LocalDirs,
		DownloadDir:   cfg.DownloadDir,
		Theme:         cfg.Theme,
		DataDir:       dir,
		ChartsGlobal:  cfg.Charts.Global,
		ChartsCountry: cfg.Charts.Country,
		Explore:       cfg.Explore,
		SeekStep:      cfg.SeekStep,
		Scrobbler:     scrob,
		ScrobbleOn:    cfg.Scrobble.Enabled,
		Lastfm:        h.Live,
	})
}

func printDevResult(rank int, r engine.Candidate) {
	fmt.Printf("  %2d.  %s — %s   [%.4f]\n", rank, r.Track, r.Artist, r.Score)
	fmt.Printf("       path: %s\n", r.Path)
	for _, s := range r.Signals {
		fmt.Printf("       %-14s %5.3f × %.2f = %+.4f   %s\n",
			s.Name+":", s.Raw, s.Weight, s.Score, s.Note)
	}
	fmt.Println()
}

// ── build-graph ───────────────────────────────────────────────────────────────

func cmdBuildGraph(args []string) {
	fs := flag.NewFlagSet("build-graph", flag.ExitOnError)
	keyFlag := fs.String("key", "", "Last.fm API key (or LASTFM_API_KEY env var)")
	maxFlag := fs.Int("max", 5000, "Maximum number of artists to crawl")
	rateFlag := fs.Float64("rate", 4.5, "API requests per second (max 5 for free tier)")
	workersFlag := fs.Int("workers", 10, "Parallel workers (more = better rate utilization)")
	outFlag := fs.String("output", "", "Output path (default: ~/.pixeltui/graph.bin)")
	verboseFlag := fs.Bool("v", false, "Verbose: show skipped artists")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: pixeltui build-graph [flags]")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	apiKey := *keyFlag
	if apiKey == "" {
		apiKey = os.Getenv("LASTFM_API_KEY")
	}
	if apiKey == "" {
		fatalf("Last.fm API key required (--key or LASTFM_API_KEY)")
	}

	outPath := *outFlag
	if outPath == "" {
		dir, err := dataDir()
		if err != nil {
			fatalf("data dir: %v", err)
		}
		outPath = filepath.Join(dir, "graph.bin")
	}

	fmt.Printf("Building graph → %s\n", outPath)
	fmt.Printf("Max artists: %d  |  Rate: %.1f req/sec  |  Workers: %d\n\n",
		*maxFlag, *rateFlag, *workersFlag)
	fmt.Printf("Estimated build time: ~%s\n\n",
		estimateBuildTime(*maxFlag, *rateFlag).Round(time.Minute))
	fmt.Println("Press Ctrl-C to stop early — partial graph will be saved.")

	client := lastfm.NewClient(apiKey)
	cfg := store.BuildConfig{
		MaxArtists: *maxFlag,
		Workers:    *workersFlag,
		ReqPerSec:  *rateFlag,
		OutputPath: outPath,
		Verbose:    *verboseFlag,
	}
	if _, err := store.BuildGraph(client, cfg); err != nil {
		fatalf("build-graph: %v", err)
	}
}

// ── cache subcommands ─────────────────────────────────────────────────────────

func cmdCache(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: pixeltui cache <warm|clear|stats>")
		os.Exit(1)
	}
	switch args[0] {
	case "warm":
		cmdCacheWarm(args[1:])
	case "clear":
		cmdCacheClear(args[1:])
	case "stats":
		cmdCacheStats(args[1:])
	default:
		fatalf("unknown cache command %q — use warm, clear, or stats", args[0])
	}
}

func cmdCacheWarm(args []string) {
	fs := flag.NewFlagSet("cache warm", flag.ExitOnError)
	keyFlag := fs.String("key", "", "Last.fm API key (or LASTFM_API_KEY env var)")
	artistFlag := fs.String("artist", "", "Artist to warm (required)")
	cacheFlag := fs.String("cache", "", "Cache path (default: ~/.pixeltui/cache.db)")
	fs.Parse(args)

	apiKey := *keyFlag
	if apiKey == "" {
		apiKey = os.Getenv("LASTFM_API_KEY")
	}
	if apiKey == "" {
		fatalf("Last.fm API key required")
	}
	artist := *artistFlag
	if artist == "" {
		if rest := fs.Args(); len(rest) > 0 {
			artist = rest[0]
		}
	}
	if artist == "" {
		fatalf("--artist is required")
	}

	cachePath := *cacheFlag
	if cachePath == "" {
		dir, err := dataDir()
		if err != nil {
			fatalf("%v", err)
		}
		cachePath = filepath.Join(dir, "cache.db")
	}

	cache, err := store.OpenCache(cachePath)
	if err != nil {
		fatalf("cache: %v", err)
	}
	defer cache.Close()

	client := lastfm.NewClient(apiKey)
	if err := store.WarmCache(client, cache, artist); err != nil {
		fatalf("%v", err)
	}
	fmt.Println("Done. These artists will now work offline.")
}

func cmdCacheClear(args []string) {
	fs := flag.NewFlagSet("cache clear", flag.ExitOnError)
	cacheFlag := fs.String("cache", "", "Cache path (default: ~/.pixeltui/cache.db)")
	fs.Parse(args)

	cachePath := *cacheFlag
	if cachePath == "" {
		dir, err := dataDir()
		if err != nil {
			fatalf("%v", err)
		}
		cachePath = filepath.Join(dir, "cache.db")
	}

	cache, err := store.OpenCache(cachePath)
	if err != nil {
		fatalf("cache: %v", err)
	}
	defer cache.Close()

	fmt.Print("Clear all cached data? [y/N] ")
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Aborted.")
		return
	}
	if err := cache.Clear(); err != nil {
		fatalf("%v", err)
	}
	fmt.Println("Cache cleared.")
}

func cmdCacheStats(args []string) {
	fs := flag.NewFlagSet("cache stats", flag.ExitOnError)
	cacheFlag := fs.String("cache", "", "Cache path (default: ~/.pixeltui/cache.db)")
	fs.Parse(args)

	cachePath := *cacheFlag
	if cachePath == "" {
		dir, err := dataDir()
		if err != nil {
			fatalf("%v", err)
		}
		cachePath = filepath.Join(dir, "cache.db")
	}

	cache, err := store.OpenCache(cachePath)
	if err != nil {
		fatalf("cache: %v", err)
	}
	defer cache.Close()

	stats := cache.Stats()
	total := 0
	fmt.Printf("Cache: %s\n\n", cachePath)
	labels := map[string]string{
		"st": "similar_tracks ",
		"tt": "track_tags     ",
		"sa": "similar_artists",
		"at": "artist_tracks  ",
	}
	for _, k := range []string{"st", "tt", "sa", "at"} {
		n := stats[k]
		total += n
		fmt.Printf("  %s  %5d entries\n", labels[k], n)
	}
	fmt.Printf("\n  Total: %d entries\n", total)

	if fi, err := os.Stat(cachePath); err == nil {
		fmt.Printf("  Size:  %s\n", fmtSize(fi.Size()))
	}
}

// ── setup ─────────────────────────────────────────────────────────────────────

// cmdSetup is an interactive form (huh) that writes ~/.pixeltui/config.json.
func cmdSetup(_ []string) {
	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	if err := runSetup(dir); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Setup cancelled — nothing saved.")
			return
		}
		fatalf("setup: %v", err)
	}
}

// runSetup shows the interactive configuration form, saves it, and reports
// connection tests. Shared by `setup` and first-run onboarding.
func runSetup(dir string) error {
	cfg, _ := config.Load(dir)
	if cfg.Theme == "" {
		cfg.Theme = "default"
	}
	localCSV := strings.Join(cfg.LocalDirs, ", ")

	exploreOpts := make([]huh.Option[int], 0, 11)
	for i := 0; i <= 10; i++ {
		label := fmt.Sprintf("%d", i)
		switch i {
		case 0:
			label = "0 — safe / very similar"
		case 5:
			label = "5 — balanced (default)"
		case 10:
			label = "10 — wild / genre-crossing"
		}
		exploreOpts = append(exploreOpts, huh.NewOption(label, i))
	}

	countryOpts := []huh.Option[string]{huh.NewOption("(off)", "")}
	inList := false
	for _, c := range tui.ChartCountries() {
		countryOpts = append(countryOpts, huh.NewOption(c, c))
		if strings.EqualFold(c, cfg.Charts.Country) {
			inList = true
		}
	}
	if cfg.Charts.Country != "" && !inList { // preserve a hand-set value
		countryOpts = append(countryOpts, huh.NewOption(cfg.Charts.Country+" (current)", cfg.Charts.Country))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("pixeltui setup").Description("Tab/↑↓ move · Enter advances · Esc cancels.\n"),
			huh.NewInput().
				Title("Last.fm API key").
				Description("Recommendations (free: last.fm/api/account/create)").
				Placeholder("optional · 32-char key").
				Value(&cfg.LastfmKey).
				Validate(validateLastfmKey),
			huh.NewSelect[string]().Title("Theme").Options(huh.NewOptions(tui.ThemeNames()...)...).Value(&cfg.Theme),
		),
		huh.NewGroup(
			huh.NewNote().Title("Scrobbling").
				Description("Submit your plays to Last.fm and/or ListenBrainz (optional).\n"),
			huh.NewConfirm().Title("Enable scrobbling?").Value(&cfg.Scrobble.Enabled),
			huh.NewInput().
				Title("Last.fm shared secret").
				Description("From the same API account page as your key — needed to scrobble").
				Placeholder("optional · 32-char secret").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Scrobble.LastfmSecret).
				Validate(validateLastfmKey),
			huh.NewInput().
				Title("ListenBrainz token").
				Description("listenbrainz.org/profile (optional)").
				EchoMode(huh.EchoModePassword).
				Value(&cfg.Scrobble.ListenBrainzToken),
		),
		huh.NewGroup(
			huh.NewNote().Title("Playback").Description("How recommendations and autoplay behave.\n"),
			huh.NewSelect[int]().
				Title("Discovery level").
				Description("How far autoplay/recommendations roam from a seed").
				Options(exploreOpts...).
				Value(&cfg.Explore),
			huh.NewConfirm().Title("Autoplay similar tracks when the queue runs out?").Value(&cfg.Autoplay),
		),
		huh.NewGroup(
			huh.NewNote().Title("Subsonic / Navidrome").Description("Optional self-hosted source.\n"),
			huh.NewInput().Title("Server URL").Placeholder("https://music.example.com").Value(&cfg.Subsonic.URL),
			huh.NewInput().Title("Username").Value(&cfg.Subsonic.User),
			huh.NewInput().Title("Password").EchoMode(huh.EchoModePassword).Value(&cfg.Subsonic.Pass),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Local music folders").
				Description("Comma-separated paths (optional)").
				Value(&localCSV).
				Validate(validateLocalDirs),
			huh.NewInput().
				Title("Download folder").
				Description("Artist/Album layout for Navidrome (optional)").
				Value(&cfg.DownloadDir),
		),
		huh.NewGroup(
			huh.NewNote().Title("Charts").Description("Live YouTube Music Top charts (no API key needed).\n"),
			huh.NewConfirm().Title("Show the Global Top chart?").Value(&cfg.Charts.Global),
			huh.NewSelect[string]().Title("Country chart").Options(countryOpts...).Value(&cfg.Charts.Country),
		),
		huh.NewGroup(
			huh.NewNote().Title("Companion server").
				Description("Defaults for `pixeltui serve` — stream your library to the iOS app.\n"),
			huh.NewSelect[string]().
				Title("Remote access").
				Description("How phones reach the server away from home").
				Options(
					huh.NewOption("LAN only (no tunnel)", ""),
					huh.NewOption("Tailscale — private, recommended", "tailscale"),
					huh.NewOption("Cloudflare quick tunnel — public URL, no account", "cloudflare"),
					huh.NewOption("ngrok — public URL, needs authtoken", "ngrok"),
				).
				Value(&cfg.Server.Tunnel),
			huh.NewInput().
				Title("Bind address").
				Description("Port the server listens on").
				Placeholder(":8787").
				Value(&cfg.Server.Addr),
			huh.NewInput().
				Title("Fixed public URL").
				Description("Only for a tunnel you run yourself (overrides the tunnel choice)").
				Placeholder("optional · https://music.example.com").
				Value(&cfg.Server.PublicURL),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	cfg.LocalDirs = nil
	for _, d := range strings.Split(localCSV, ",") {
		if d = strings.TrimSpace(d); d != "" {
			cfg.LocalDirs = append(cfg.LocalDirs, d)
		}
	}
	if err := cfg.Save(dir); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\n  Saved → %s\n", config.Path(dir))

	// Scrobbling enabled with Last.fm creds but no session yet → one-time browser
	// authorization, right here so setup ends fully working.
	if cfg.Scrobble.Enabled && cfg.LastfmKey != "" &&
		cfg.Scrobble.LastfmSecret != "" && cfg.Scrobble.LastfmSession == "" {
		if err := authorizeLastfm(dir, cfg); err != nil {
			fmt.Printf("  Last.fm authorization skipped: %v\n", err)
			fmt.Println("  Finish it anytime with:  pixeltui scrobble-auth")
		}
	}

	checkConnections(cfg)
	fmt.Println("  Next: 'pixeltui doctor' to verify mpv/yt-dlp, or just 'pixeltui' to start.")
	return nil
}

// authorizeLastfm runs the one-time Last.fm desktop auth flow: request a token,
// have the user approve it in the browser, then exchange it for a session key
// (saved to config). cfg must already carry the API key + shared secret.
func authorizeLastfm(dir string, cfg *config.Config) error {
	lf := scrobble.NewLastfm(cfg.LastfmKey, cfg.Scrobble.LastfmSecret, "")
	token, authURL, err := lf.GetToken()
	if err != nil {
		return err
	}
	fmt.Println("\n  Authorize pixeltui with your Last.fm account:")
	fmt.Println("    " + authURL)
	if openBrowser(authURL) {
		fmt.Println("  (opened in your browser)")
	}
	fmt.Print("  Press Enter once you've clicked “Yes, allow access”… ")
	bufio.NewReader(os.Stdin).ReadString('\n') //nolint:errcheck

	key, user, err := lf.GetSession(token)
	if err != nil {
		return err
	}
	cfg.Scrobble.LastfmSession = key
	cfg.Scrobble.LastfmUser = user
	if err := cfg.Save(dir); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("  ✓ Scrobbling authorized as %s\n", user)
	return nil
}

// openBrowser best-effort opens url in the default browser.
func openBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start() == nil
}

// cmdScrobbleAuth (re)runs the Last.fm authorization flow standalone.
func cmdScrobbleAuth(_ []string) {
	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	cfg, _ := config.Load(dir)
	if cfg.LastfmKey == "" || cfg.Scrobble.LastfmSecret == "" {
		fatalf("Last.fm API key + shared secret required first — run 'pixeltui setup'.\n" +
			"  Both come from the same page: https://www.last.fm/api/account/create")
	}
	if !cfg.Scrobble.Enabled {
		cfg.Scrobble.Enabled = true // explicit auth implies opting in
	}
	if err := authorizeLastfm(dir, cfg); err != nil {
		fatalf("scrobble-auth: %v", err)
	}
}

// validateLastfmKey accepts an empty key or a 32-char hex string.
func validateLastfmKey(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len(s) != 32 {
		return fmt.Errorf("Last.fm keys are 32 characters (got %d)", len(s))
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return fmt.Errorf("Last.fm keys are hex (0-9, a-f)")
		}
	}
	return nil
}

// validateLocalDirs ensures each comma-separated path is an existing folder.
func validateLocalDirs(s string) error {
	for _, d := range strings.Split(s, ",") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if strings.HasPrefix(d, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				d = filepath.Join(home, d[2:])
			}
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			return fmt.Errorf("not a folder: %s", d)
		}
	}
	return nil
}

// checkConnections does a quick live test of the Last.fm key and Subsonic creds.
func checkConnections(cfg *config.Config) {
	if cfg.LastfmKey != "" {
		fmt.Print("  Last.fm key … ")
		if _, err := lastfm.NewClient(cfg.LastfmKey).GetSimilarArtists("Radiohead", 1); err != nil {
			fmt.Printf("✗ %s\n", err)
		} else {
			fmt.Println("✓ ok")
		}
	}
	if cfg.HasSubsonic() {
		fmt.Print("  Subsonic … ")
		if err := subsonic.NewClient(cfg.Subsonic.URL, cfg.Subsonic.User, cfg.Subsonic.Pass).Ping(); err != nil {
			fmt.Printf("✗ %s\n", err)
		} else {
			fmt.Println("✓ ok")
		}
	}
}

// maybeOnboard runs a friendly first-launch setup when there's no config yet.
func maybeOnboard(dir string) {
	yes := true
	welcome := huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title("Welcome to pixeltui ♫").
			Description("A fast terminal music player.\nLet's set it up — Last.fm key (optional), sources, theme.\n"),
		huh.NewConfirm().Title("Configure now?").Affirmative("Set up").Negative("Skip").Value(&yes),
	))
	if err := welcome.Run(); err != nil || !yes {
		return // skipped or aborted — fall through to the player
	}
	_ = runSetup(dir)
}

// ── reset ─────────────────────────────────────────────────────────────────────

// cmdReset wipes pixeltui data. Targets: cache | graph | library | config | all.
// "all" keeps the installed tools (mpv bundle, yt-dlp venv).
func cmdReset(args []string) {
	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	target := "all"
	if len(args) > 0 {
		target = strings.TrimLeft(args[0], "-")
	}

	sets := map[string][]string{
		"cache":   {"cache.db"},
		"graph":   {"graph.bin"},
		"library": {"library"},
		"config":  {"config.json"},
	}
	sets["all"] = []string{"cache.db", "graph.bin", "library", "config.json"}

	names, ok := sets[target]
	if !ok {
		fatalf("unknown reset target %q — use cache | graph | library | config | all", target)
	}

	fmt.Printf("Reset %q will delete from %s:\n", target, dir)
	for _, n := range names {
		fmt.Printf("  - %s\n", n)
	}
	fmt.Print("Proceed? [y/N] ")
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		fmt.Println("Aborted.")
		return
	}
	for _, n := range names {
		if err := os.RemoveAll(filepath.Join(dir, n)); err != nil {
			fmt.Fprintf(os.Stderr, "  failed: %s (%v)\n", n, err)
		}
	}
	fmt.Println("Done. (Installed tools — mpv, yt-dlp venv — were kept.)")
}

// ── uninstall ─────────────────────────────────────────────────────────────────

// uninstallDataTargets returns the names inside the data dir to remove. With
// keepData, the user's library and config are spared; everything else (cache,
// graph, bundled tools) still goes.
func uninstallDataTargets(entries []string, keepData bool) []string {
	if !keepData {
		return entries
	}
	keep := map[string]bool{"library": true, "config.json": true}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !keep[e] {
			out = append(out, e)
		}
	}
	return out
}

// cmdUninstall removes pixeltui: the data dir (~/.pixeltui), the running binary,
// and on Windows the PATH entry the installer added. System packages (e.g. mpv
// from apt) are left in place. --keep-data spares the library + config; -y skips
// the confirmation prompt.
func cmdUninstall(args []string) {
	keepData, yes := false, false
	for _, a := range args {
		switch strings.TrimLeft(a, "-") {
		case "keep-data":
			keepData = true
		case "y", "yes":
			yes = true
		}
	}

	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	exe, _ := os.Executable()
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}

	fmt.Println("\n  pixeltui uninstall")
	fmt.Println("  Will remove:")
	if exe != "" {
		fmt.Printf("    • binary   %s\n", exe)
	}
	if keepData {
		fmt.Printf("    • data     %s  (keeping library/ and config.json)\n", dir)
	} else {
		fmt.Printf("    • data     %s  (everything, incl. library + config)\n", dir)
	}
	if runtime.GOOS == "windows" && exe != "" {
		fmt.Printf("    • PATH entry for %s\n", filepath.Dir(exe))
	}

	if !yes {
		fmt.Print("  Proceed? [y/N] ")
		var c string
		fmt.Scanln(&c) //nolint:errcheck
		if strings.ToLower(strings.TrimSpace(c)) != "y" {
			fmt.Println("  Aborted.")
			return
		}
	}

	// 1. data dir (remove targets, then the now-empty dir on a full uninstall).
	if entries, err := os.ReadDir(dir); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		for _, n := range uninstallDataTargets(names, keepData) {
			os.RemoveAll(filepath.Join(dir, n)) //nolint:errcheck
		}
		if !keepData {
			os.Remove(dir) //nolint:errcheck
		}
	}

	// 2. binary (+ Windows PATH cleanup).
	if exe != "" {
		removeSelf(exe)
	}

	fmt.Println("\n  ✓ pixeltui uninstalled.")
	if runtime.GOOS == "linux" {
		fmt.Println("  Note: mpv installed via your package manager was left in place.")
		fmt.Println("        Remove it with:  sudo apt-get remove mpv   (or dnf/pacman/zypper)")
	}
}

// removeSelf deletes the running binary. On Unix the file can be unlinked while
// running. On Windows a running .exe can't be deleted, so it's renamed aside,
// the installer's PATH entry is stripped, and a detached cleaner finishes up.
func removeSelf(exe string) {
	if runtime.GOOS != "windows" {
		if err := os.Remove(exe); err != nil {
			fmt.Printf("  could not remove %s (%v)\n  finish with:  sudo rm %s\n", exe, err, exe)
		}
		return
	}
	old := exe + ".old"
	os.Remove(old) //nolint:errcheck
	if err := os.Rename(exe, old); err != nil {
		fmt.Printf("  could not remove %s (%v) — delete it manually\n", exe, err)
		return
	}
	binPath := filepath.Dir(exe)
	removeWindowsPathEntry(binPath)
	// Detached: wait for this process to exit, then delete the stub + its dir.
	clean := fmt.Sprintf(`ping 127.0.0.1 -n 2 >nul & del /q "%s" & rmdir "%s"`, old, binPath)
	exec.Command("cmd", "/c", clean).Start() //nolint:errcheck
}

// removeWindowsPathEntry strips dir from the User PATH (reversing what
// install.ps1 added). Best-effort via PowerShell so no registry deps are needed.
func removeWindowsPathEntry(dir string) {
	ps := fmt.Sprintf(
		`$p=([Environment]::GetEnvironmentVariable('PATH','User') -split ';' | Where-Object { $_ -and $_ -ne '%s' }) -join ';'; [Environment]::SetEnvironmentVariable('PATH',$p,'User')`,
		dir,
	)
	exec.Command("powershell", "-NoProfile", "-Command", ps).Run() //nolint:errcheck
}

// ── export ──────────────────────────────────────────────────────────────────────

// cmdExport writes a library playlist as XSPF (standard, portable). Usage:
//
//	pixeltui export "My Mix" [out.xspf]   (no file → stdout)
func cmdExport(args []string) {
	if len(args) == 0 {
		fatalf("usage: pixeltui export <playlist> [out.xspf]")
	}
	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	lib, err := library.Open(dir)
	if err != nil {
		fatalf("%v", err)
	}
	out := os.Stdout
	if len(args) > 1 {
		f, err := os.Create(args[1])
		if err != nil {
			fatalf("%v", err)
		}
		defer f.Close()
		out = f
	}
	if err := lib.ExportXSPF(args[0], out); err != nil {
		fatalf("export: %v", err)
	}
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "Exported %q → %s\n", args[0], args[1])
	}
}

// ── serve ─────────────────────────────────────────────────────────────────────

// flagWasSet reports whether a flag was explicitly passed on the command line.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// cmdServe runs the HTTP server that backs the companion app: browse, search,
// and stream your library (and sources) from anywhere. Remote access comes
// from --tunnel (cloudflare/ngrok/tailscale) or a BYO tunnel via --url; both
// can live in the config's "server" section so plain `pixeltui serve` works.
func cmdServe(args []string) {
	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	cfg, _ := config.Load(dir)

	// Config supplies the defaults; flags override per run.
	defAddr := cfg.Server.Addr
	if defAddr == "" {
		defAddr = ":8787"
	}
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", defAddr, "bind address")
	urlFlag := fs.String("url", cfg.Server.PublicURL, "public base URL advertised in the pairing QR (BYO tunnel)")
	name := fs.String("name", cfg.Server.Name, "server name shown to clients (default: hostname)")
	tunnelFlag := fs.String("tunnel", cfg.Server.Tunnel, "publish via a tunnel: cloudflare, ngrok, or tailscale")
	fs.Parse(args)

	// An explicit --url wins over a tunnel from config (it IS the tunnel).
	if *urlFlag != "" && !flagWasSet(fs, "tunnel") {
		*tunnelFlag = ""
	}
	var tun *server.Tunnel
	if *tunnelFlag != "" {
		fmt.Printf("  Starting %s tunnel…\n", *tunnelFlag)
		tun, err = server.StartTunnel(*tunnelFlag, *addr)
		if err != nil {
			fatalf("tunnel: %v", err)
		}
		if tun != nil {
			*urlFlag = tun.URL
			fmt.Printf("  ✓ %s tunnel up: %s\n\n", tun.Provider, tun.URL)
			defer tun.Close()
			// Make sure Ctrl-C also tears the tunnel process down.
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sig
				tun.Close()
				os.Exit(0)
			}()
		}
	}
	lib, _ := library.Open(dir)
	var sub *subsonic.Client
	if cfg.HasSubsonic() {
		sub = subsonic.NewClient(cfg.Subsonic.URL, cfg.Subsonic.User, cfg.Subsonic.Pass)
	}
	// Stream-URL cache: makes repeat YouTube plays resolve instantly. Optional —
	// if the TUI holds the bbolt lock, serve just runs uncached.
	var streamCache server.StreamURLCache
	if c, err := store.OpenCache(filepath.Join(dir, "cache.db")); err == nil {
		streamCache = c
		defer c.Close()
	}
	var lfm *lastfm.Client
	if cfg.LastfmKey != "" {
		lfm = lastfm.NewClient(cfg.LastfmKey)
	}
	// Scrobbling for client plays (POST /api/played): same services and spool
	// as the TUI, so phone listens reach Last.fm / ListenBrainz too.
	var scrob *scrobble.Scrobbler
	if cfg.ScrobbleReady() && cfg.Scrobble.Enabled {
		var lf *scrobble.Lastfm
		if cfg.LastfmScrobbleReady() {
			lf = scrobble.NewLastfm(cfg.LastfmKey, cfg.Scrobble.LastfmSecret, cfg.Scrobble.LastfmSession)
		}
		var lb *scrobble.ListenBrainz
		if cfg.Scrobble.ListenBrainzToken != "" {
			lb = scrobble.NewListenBrainz(cfg.Scrobble.ListenBrainzToken)
		}
		if scrob = scrobble.New(lf, lb, dir); scrob != nil {
			go scrob.RetrySpool()
		}
	}
	// Recommendation engine for /api/recommend — the TUI's layered data source
	// (static graph → bbolt cache → live Last.fm) with liked-artist affinity,
	// so client recs match the TUI's.
	var rec *engine.Recommender
	if lfm != nil {
		h := &store.Hybrid{Live: lfm}
		if gr, gerr := store.LoadGraph(filepath.Join(dir, "graph.bin")); gerr == nil {
			h.Graph = gr
		}
		if c, ok := streamCache.(*store.Cache); ok {
			h.Cache = c
		}
		rec = engine.New(h, false)
		if lib != nil {
			aff := map[string]float64{}
			for _, c := range lib.Liked() {
				if c.Artist != "" {
					aff[strings.ToLower(c.Artist)] = 1.0
				}
			}
			if len(aff) > 0 {
				rec.Affinity = aff
			}
		}
	}
	err = server.Run(server.Config{
		DataDir:     dir,
		Name:        *name,
		Version:     version,
		Addr:        *addr,
		URL:         *urlFlag,
		Library:     lib,
		Subsonic:    sub,
		LocalDirs:   cfg.LocalDirs,
		StreamCache: streamCache,
		Lastfm:      lfm,
		Rec:         rec,
		Scrobbler:   scrob,
	})
	if err != nil {
		fatalf("serve: %v", err)
	}
}

// ── devices ───────────────────────────────────────────────────────────────────

// pairedDevice mirrors the listing fields of <dataDir>/devices.json (written by
// the serve command's device store). Tokens are stored hashed and never shown.
type pairedDevice struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

// cmdDevices lists companion-app devices paired with `pixeltui serve`, or
// revokes one: `pixeltui devices revoke <id>`.
func cmdDevices(args []string) {
	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	path := filepath.Join(dir, "devices.json")

	if len(args) > 0 && strings.TrimLeft(args[0], "-") == "revoke" {
		if len(args) < 2 {
			fatalf("usage: pixeltui devices revoke <id>")
		}
		revokeDevice(path, args[1])
		return
	}

	var devs []pairedDevice
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &devs) //nolint:errcheck
	}
	if len(devs) == 0 {
		fmt.Println("\n  No paired devices. Pair one with 'pixeltui serve' (scan the QR).")
		fmt.Println()
		return
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(dimStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return s.Bold(true)
			}
			if col >= 2 {
				return s.Foreground(lipgloss.Color("250"))
			}
			return s
		}).
		Headers("ID", "NAME", "PAIRED", "LAST SEEN")
	for _, d := range devs {
		t.Row(d.ID, d.Name, fmtAge(d.Created)+" ago", fmtAge(d.LastSeen)+" ago")
	}
	fmt.Println()
	fmt.Println(t)
	fmt.Println("  Revoke with 'pixeltui devices revoke <id>'.")
	fmt.Println()
}

// revokeDevice removes one entry from devices.json (atomic rewrite). Records
// are kept as raw JSON so the stored token hashes survive untouched.
func revokeDevice(path, id string) {
	b, err := os.ReadFile(path)
	if err != nil {
		fatalf("no paired devices (%s)", path)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		fatalf("devices.json: %v", err)
	}
	kept := raw[:0]
	found := false
	for _, r := range raw {
		var d pairedDevice
		if json.Unmarshal(r, &d) == nil && d.ID == id {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		fatalf("no device with id %q — run 'pixeltui devices' to list them", id)
	}
	out, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		fatalf("%v", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		fatalf("%v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("  ✓ Revoked device %s.\n", id)
	fmt.Println("  Note: a running 'pixeltui serve' keeps its in-memory copy — restart it to apply.")
}

// ── update ────────────────────────────────────────────────────────────────────

const repoSlug = "dotjarden/pixeltui"

// cmdUpdate replaces the running binary with a GitHub release build for this
// OS/arch (same release URLs the installer uses). With no argument it tracks
// the latest release; `pixeltui update v0.2.4` (or `0.2.4`) installs that tag —
// also the way to roll back.
func cmdUpdate(args []string) {
	exe, err := os.Executable()
	if err != nil {
		fatalf("can't locate the running binary: %v", err)
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}

	asset := fmt.Sprintf("pixeltui-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset = "pixeltui-windows-amd64.exe" // only an amd64 Windows build is published
	}

	var wantTag string
	if len(args) > 0 && args[0] != "" && args[0] != "latest" {
		wantTag = args[0]
		if !strings.HasPrefix(wantTag, "v") {
			wantTag = "v" + wantTag
		}
	}

	base := "https://github.com/" + repoSlug + "/releases/latest/download/"
	if wantTag != "" {
		base = "https://github.com/" + repoSlug + "/releases/download/" + wantTag + "/"
		if wantTag == "v"+version {
			fmt.Printf("Already on %s — reinstalling it anyway.\n", wantTag)
		}
		fmt.Printf("Updating pixeltui → %s …\n", wantTag)
	} else if tag := latestTag(); tag != "" { // best-effort, for the message
		fmt.Printf("Updating pixeltui → %s …\n", tag)
	} else {
		fmt.Println("Updating pixeltui to the latest release …")
	}

	// Download into the same directory so the final rename is atomic.
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".pixeltui-update-*")
	if err != nil {
		fatalf("can't write to %s (try sudo, or re-run the install script): %v", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := download(base+asset, tmp); err != nil {
		tmp.Close()
		if wantTag != "" && strings.Contains(err.Error(), "404") {
			fatalf("no release %s (or it lacks %s)\n  releases: https://github.com/%s/releases",
				wantTag, asset, repoSlug)
		}
		fatalf("download failed: %v", err)
	}
	tmp.Close()

	if err := verifyChecksum(base+"SHA256SUMS", asset, tmpPath); err != nil {
		fatalf("%v", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		fatalf("%v", err)
	}
	if runtime.GOOS == "darwin" {
		exec.Command("xattr", "-d", "com.apple.quarantine", tmpPath).Run() //nolint:errcheck
	}

	// Swap into place. On Windows a running .exe can't be overwritten, so move
	// the old one aside first (it can be deleted on next launch).
	if runtime.GOOS == "windows" {
		_ = os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			fatalf("can't replace %s: %v", exe, err)
		}
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		fatalf("can't install update to %s (try sudo): %v", exe, err)
	}
	fmt.Printf("✓ Updated → %s\n", exe)
	if wantTag != "" {
		fmt.Printf("  now on %s\n", wantTag)
	}
}

// download streams url into w.
func download(url string, w io.Writer) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// verifyChecksum downloads SHA256SUMS and checks the file's hash for asset.
// A missing/!matching sums entry is treated leniently (skip) unless it mismatches.
func verifyChecksum(sumsURL, asset, path string) error {
	resp, err := http.Get(sumsURL) //nolint:gosec
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil // no checksums published — skip
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var want string
	for _, line := range strings.Split(string(body), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == asset {
			want = f[0]
		}
	}
	if want == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch — aborting update")
	}
	return nil
}

// latestTag fetches the latest release tag name (best-effort).
func latestTag() string {
	resp, err := http.Get("https://api.github.com/repos/" + repoSlug + "/releases/latest")
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()
	var v struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return ""
	}
	return v.TagName
}

// ── doctor ──────────────────────────────────────────────────────────────────────

// cmdDoctor checks that everything pixeltui needs is wired up correctly.
// With --fix it self-resolves what it safely can (the fast yt-dlp venv).
func cmdDoctor(args []string) {
	fix := false
	for _, a := range args {
		if strings.TrimLeft(a, "-") == "fix" {
			fix = true
		}
	}

	dim := "\033[2m"
	reset := "\033[0m"
	type drow struct {
		status int // 0 ok, 1 warn, 2 bad
		name   string
		detail string
	}
	var rows []drow
	ok := func(label, detail string) { rows = append(rows, drow{0, label, detail}) }
	warn := func(label, detail string) { rows = append(rows, drow{1, label, detail}) }
	bad := func(label, detail string) { rows = append(rows, drow{2, label, detail}) }

	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	cfg, _ := config.Load(dir)

	fmt.Printf("\n  \033[1mpixeltui doctor\033[0m  (%s/%s)%s\n\n", runtime.GOOS, runtime.GOARCH,
		map[bool]string{true: dim + "  --fix" + reset, false: ""}[fix])

	// yt-dlp (optional) — streaming resolves natively via InnerTube; yt-dlp is
	// the resolution fallback and powers downloads. Self-resolvable.
	yt := preferredYtdlp()
	ver := ""
	if yt != "" {
		ver = toolVersion(yt, "--version")
	}
	if (yt == "" || ver == "?") && fix {
		fmt.Println("  → installing yt-dlp…")
		if fixYtdlp(dir) {
			yt = preferredYtdlp()
			ver = toolVersion(yt, "--version")
		}
	}
	switch {
	case yt == "":
		warn("yt-dlp", "not found — streaming works without it (native resolver); needed for downloads + as a stream fallback. Fix: pixeltui doctor --fix")
	case ver == "?":
		bad("yt-dlp", "found but won't run — Fix: pixeltui doctor --fix  ("+yt+")")
	default:
		kind := "on PATH"
		switch {
		case strings.Contains(yt, "ytdlp-venv"):
			kind = "pip venv"
		case strings.Contains(yt, ".pixeltui"):
			kind = "bundled"
		}
		ok("yt-dlp", fmt.Sprintf("%s  [%s]", ver, kind))
	}

	// players — mpv is self-resolvable.
	if mpvBin() == "" && fix {
		fmt.Println("  → installing mpv…")
		fixMPV(dir)
	}
	switch mb := mpvBin(); {
	case mb != "" && toolVersion(mb, "--version") != "?":
		ok("mpv", "pause/seek/volume + OS Now Playing")
	case mb != "":
		bad("mpv", "found but won't run — Fix: pixeltui doctor --fix  ("+mb+")")
	default:
		warn("mpv", "missing — audio plays via ffplay/afplay but no controls. Fix: pixeltui doctor --fix")
	}
	switch {
	case hasBin("ffplay"):
		ok("ffplay", "fallback player available")
	case hasBin("afplay"):
		ok("afplay", "fallback player available (macOS)")
	default:
		if !hasBin("mpv") {
			bad("player", "no mpv/ffplay/afplay found — nothing can play audio")
		}
	}
	if hasBin("ffprobe") {
		ok("ffprobe", "local-file tags available")
	} else if cfg.HasLocal() {
		warn("ffprobe", "missing — local files fall back to filename for metadata")
	}

	// Last.fm key (recommendations)
	if cfg.LastfmKey != "" {
		ok("Last.fm key", "set — recommendations enabled")
	} else {
		warn("Last.fm key", "unset — run 'pixeltui setup'. Free key: www.last.fm/api/account/create")
	}

	// Scrobbling (Last.fm / ListenBrainz play submission)
	switch {
	case !cfg.Scrobble.Enabled:
		if cfg.ScrobbleReady() {
			warn("scrobbling", "configured but off — enable in settings (,) or 'pixeltui setup'")
		}
	case cfg.LastfmScrobbleReady() && cfg.Scrobble.ListenBrainzToken != "":
		ok("scrobbling", "Last.fm (as "+cfg.Scrobble.LastfmUser+") + ListenBrainz")
	case cfg.LastfmScrobbleReady():
		ok("scrobbling", "Last.fm — as "+cfg.Scrobble.LastfmUser)
	case cfg.Scrobble.ListenBrainzToken != "":
		ok("scrobbling", "ListenBrainz")
	case cfg.LastfmKey != "" && cfg.Scrobble.LastfmSecret != "":
		warn("scrobbling", "not authorized yet — run 'pixeltui scrobble-auth'")
	default:
		warn("scrobbling", "enabled but no service configured — run 'pixeltui setup'")
	}

	// Optional sources
	if cfg.HasSubsonic() {
		c := subsonic.NewClient(cfg.Subsonic.URL, cfg.Subsonic.User, cfg.Subsonic.Pass)
		if err := c.Ping(); err == nil {
			ok("Subsonic", "connected — "+cfg.Subsonic.URL)
		} else {
			bad("Subsonic", "configured but unreachable: "+strings.SplitN(err.Error(), "\n", 2)[0])
		}
	}
	if cfg.HasLocal() {
		n := 0
		for _, d := range cfg.LocalDirs {
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				n++
			} else {
				warn("local dir", "missing: "+d)
			}
		}
		if n > 0 {
			ok("local", fmt.Sprintf("%d folder(s) configured", n))
		}
	}

	// data dir / graph / cache
	ok("data dir", dir)
	if gr, err := store.LoadGraph(filepath.Join(dir, "graph.bin")); err == nil {
		ok("graph", fmt.Sprintf("%d artists, built %s ago", gr.ArtistCount(), fmtAge(gr.BuiltAt())))
	} else {
		warn("graph", "none — offline recs need it. Build once: pixeltui build-graph")
	}
	if fi, err := os.Stat(filepath.Join(dir, "cache.db")); err == nil {
		ok("cache", fmtSize(fi.Size()))
	} else {
		warn("cache", "empty (fills on first use)")
	}

	// Render the collected checks as a table.
	icon := map[int]string{0: "✓", 1: "!", 2: "✗"}
	colors := map[int]lipgloss.Color{0: "10", 1: "11", 2: "9"}
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(dimStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row < 0 || row >= len(rows) {
				return lipgloss.NewStyle().Padding(0, 1)
			}
			s := lipgloss.NewStyle().Padding(0, 1)
			switch col {
			case 0:
				return s.Foreground(colors[rows[row].status]).Bold(true).Align(lipgloss.Center)
			case 2:
				return s.Foreground(lipgloss.Color("250"))
			}
			return s
		})
	for _, r := range rows {
		t.Row(icon[r.status], r.name, r.detail)
	}
	fmt.Println(t)
	if !fix {
		fmt.Printf("  %sRun 'pixeltui doctor --fix' to auto-resolve fixable items.%s\n", dim, reset)
	}
	fmt.Println()
}

// fixYtdlp installs the self-contained standalone yt-dlp binary into
// ~/.pixeltui/bin — no Python, pip, or venv. Existing pip venvs are still
// discovered by preferredYtdlp for back-compat; this is the universal path.
func fixYtdlp(dir string) bool {
	asset := ytdlpAsset(runtime.GOOS, runtime.GOARCH)
	if asset == "" {
		fmt.Println("    no prebuilt yt-dlp for this platform — install yt-dlp manually")
		return false
	}
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name = "yt-dlp.exe"
	}
	dest := filepath.Join(binDir(dir), name)
	url := "https://github.com/yt-dlp/yt-dlp/releases/latest/download/" + asset
	fmt.Println("    downloading yt-dlp (self-contained)…")
	if err := downloadBinary(url, dest); err != nil {
		fmt.Println("    download failed:", err)
		return false
	}
	if toolVersion(dest, "--version") == "?" {
		fmt.Println("    installed but won't run:", dest)
		return false
	}
	fmt.Println("    yt-dlp installed → ~/.pixeltui/bin")
	return true
}

// mpvBin mirrors tui's resolver: $PIXELTUI_MPV → ~/.pixeltui/mpv.app → PATH.
func mpvBin() string {
	if p := os.Getenv("PIXELTUI_MPV"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, cand := range []string{
			filepath.Join(home, ".pixeltui", "mpv.app", "Contents", "MacOS", "mpv"),
			filepath.Join(home, ".pixeltui", "mpv", "mpv.exe"),
		} {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
		}
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" { // winget portable shim
		cand := filepath.Join(la, "Microsoft", "WinGet", "Links", "mpv.exe")
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	if p, err := exec.LookPath("mpv"); err == nil {
		return p
	}
	return ""
}

// fixMPV installs mpv: a self-contained bundle into ~/.pixeltui/mpv.app on macOS
// (no package manager needed), or via the system package manager on Linux.
func fixMPV(dir string) bool {
	switch runtime.GOOS {
	case "linux":
		pms := [][]string{
			{"apt-get", "install", "-y", "mpv"}, {"dnf", "install", "-y", "mpv"},
			{"pacman", "-S", "--noconfirm", "mpv"}, {"zypper", "install", "-y", "mpv"},
		}
		isRoot := os.Geteuid() == 0
		hasSudo := hasBin("sudo")
		for _, pm := range pms {
			if !hasBin(pm[0]) {
				continue
			}
			// apt's package list may be stale in minimal images; refresh first.
			if pm[0] == "apt-get" {
				upd := mpvInstallCmd(isRoot, hasSudo, []string{"apt-get", "update"})
				u := exec.Command(upd[0], upd[1:]...)
				u.Stdin, u.Stdout, u.Stderr = os.Stdin, os.Stdout, os.Stderr
				u.Run() //nolint:errcheck
			}
			argv := mpvInstallCmd(isRoot, hasSudo, pm)
			fmt.Printf("    installing mpv via %s…\n", pm[0])
			c := exec.Command(argv[0], argv[1:]...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if c.Run() == nil && hasBin("mpv") {
				return true
			}
		}
		fmt.Println("    couldn't auto-install mpv — install via your package manager (apt/dnf/pacman/zypper)")
		return false
	case "darwin":
		file := "mpv-latest.tar.gz"
		if runtime.GOARCH == "arm64" {
			file = "mpv-arm64-latest.tar.gz"
		}
		url := "https://laboratory.stolendata.net/~djinn/mpv_osx/" + file
		fmt.Println("    downloading mpv bundle…")
		resp, err := http.Get(url)
		if err != nil {
			fmt.Println("    download failed:", err)
			return false
		}
		defer resp.Body.Close()
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			fmt.Println("    gunzip failed:", err)
			return false
		}
		// Extract into a temp dir on the SAME volume as dir so the final rename is atomic.
		tmp, err := os.MkdirTemp(dir, ".mpv-extract-")
		if err != nil {
			return false
		}
		defer os.RemoveAll(tmp)
		tr := tar.NewReader(gz)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				fmt.Println("    extract failed:", err)
				return false
			}
			target := filepath.Join(tmp, hdr.Name) //nolint:gosec
			switch hdr.Typeflag {
			case tar.TypeDir:
				os.MkdirAll(target, 0755) //nolint:errcheck
			case tar.TypeSymlink:
				os.MkdirAll(filepath.Dir(target), 0755) //nolint:errcheck
				os.Symlink(hdr.Linkname, target)        //nolint:errcheck
			case tar.TypeReg:
				os.MkdirAll(filepath.Dir(target), 0755) //nolint:errcheck
				f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
				if err == nil {
					io.Copy(f, tr) //nolint:errcheck,gosec
					f.Close()
				}
			}
		}
		app := findAppBundle(tmp)
		if app == "" {
			fmt.Println("    couldn't find mpv.app in the archive")
			return false
		}
		dst := filepath.Join(dir, "mpv.app")
		os.RemoveAll(dst)
		if err := os.Rename(app, dst); err != nil {
			fmt.Println("    install failed:", err)
			return false
		}
		if err := exec.Command(filepath.Join(dst, "Contents", "MacOS", "mpv"), "--version").Run(); err != nil {
			fmt.Println("    installed but won't run:", err)
			return false
		}
		fmt.Println("    mpv installed → ~/.pixeltui/mpv.app")
		return true
	case "windows":
		// Primary: self-contained standalone build → ~/.pixeltui/mpv (no admin,
		// no package manager). Windows 10/11 ship `tar` (libarchive) which reads
		// the .7z. Falls back to package managers, then manual guidance.
		if installMpvWindows(dir) {
			return true
		}
		for _, pm := range [][]string{
			{"winget", "install", "--id", "mpv.mpv", "-e", "--source", "winget", "--silent",
				"--accept-package-agreements", "--accept-source-agreements"},
			{"scoop", "install", "mpv"},
			{"choco", "install", "mpv", "-y"},
		} {
			if hasBin(pm[0]) {
				fmt.Printf("    installing mpv via %s…\n", pm[0])
				c := exec.Command(pm[0], pm[1:]...)
				c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
				if c.Run() == nil {
					fmt.Println("    mpv installed — restart your terminal if controls don't appear yet.")
					return true
				}
			}
		}
		fmt.Println("    couldn't auto-install mpv. Options:")
		fmt.Println("      • winget install mpv.mpv   (enable the 'winget' source if missing)")
		fmt.Println("      • scoop install mpv        (no admin needed)")
		fmt.Println("      • https://mpv.io/installation/  (unzip and add to PATH)")
		return false
	default:
		fmt.Println("    auto-install unsupported on this OS — install mpv manually")
		return false
	}
}

// installMpvWindows downloads the standalone shinchiro mpv build and extracts it
// to ~/.pixeltui/mpv using the built-in `tar` (libarchive reads .7z). Returns
// false if tar is unavailable, the asset can't be found, or extraction fails.
func installMpvWindows(dir string) bool {
	if !hasBin("tar") {
		return false // need libarchive tar for .7z
	}
	url := shinchiroMpvURL()
	if url == "" {
		return false
	}
	fmt.Println("    downloading mpv (standalone build)…")
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("    download failed:", err)
		return false
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp(dir, ".mpv-*.7z")
	if err != nil {
		return false
	}
	arch := f.Name()
	defer os.Remove(arch)
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		fmt.Println("    download failed:", err)
		return false
	}
	f.Close()

	dst := filepath.Join(dir, "mpv")
	os.RemoveAll(dst)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false
	}
	fmt.Println("    extracting…")
	c := exec.Command("tar", "-xf", arch, "-C", dst)
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Println("    extract failed (tar can't read .7z?):", err)
		return false
	}
	if _, err := os.Stat(filepath.Join(dst, "mpv.exe")); err != nil {
		fmt.Println("    mpv.exe not found after extraction")
		return false
	}
	fmt.Println("    mpv installed → ~/.pixeltui/mpv")
	return true
}

// shinchiroMpvURL returns the download URL of the latest x86_64 mpv .7z build
// (baseline, not -dev or -v3) from shinchiro/mpv-winbuild-cmake.
func shinchiroMpvURL() string {
	resp, err := http.Get("https://api.github.com/repos/shinchiro/mpv-winbuild-cmake/releases/latest")
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil {
		return ""
	}
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, "mpv-x86_64-") && // baseline x86_64 (excludes mpv-dev-*, mpv-i686-*, mpv-aarch64-*)
			!strings.HasPrefix(a.Name, "mpv-x86_64-v3") && // skip the v3 (newer-CPU) variant
			strings.HasSuffix(a.Name, ".7z") {
			return a.URL
		}
	}
	return ""
}

// findAppBundle walks root for a directory named "mpv.app".
func findAppBundle(root string) string {
	found := ""
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error { //nolint:errcheck
		if err == nil && d.IsDir() && d.Name() == "mpv.app" {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// preferredYtdlp mirrors the resolver's lookup order: $PIXELTUI_YTDLP → venv → PATH.
func preferredYtdlp() string {
	if p := os.Getenv("PIXELTUI_YTDLP"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		venv := filepath.Join(home, ".pixeltui", "ytdlp-venv")
		for _, c := range []string{
			filepath.Join(venv, "bin", "yt-dlp"),
			filepath.Join(venv, "Scripts", "yt-dlp.exe"),
		} {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
		}
		bin := filepath.Join(home, ".pixeltui", "bin", "yt-dlp")
		if runtime.GOOS == "windows" {
			bin = filepath.Join(home, ".pixeltui", "bin", "yt-dlp.exe")
		}
		if fi, err := os.Stat(bin); err == nil && !fi.IsDir() {
			return bin
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	return ""
}

func toolVersion(bin, flag string) string {
	out, err := exec.Command(bin, flag).Output()
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

func hasBin(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
	os.Exit(1)
}

func fmtAge(t time.Time) string {
	d := time.Since(t).Round(time.Hour)
	if d < 24*time.Hour {
		return fmt.Sprintf("%.0fh", d.Hours())
	}
	return fmt.Sprintf("%.0fd", d.Hours()/24)
}

func fmtSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func estimateBuildTime(artists int, ratePerSec float64) time.Duration {
	// 2 API calls per artist
	totalCalls := artists * 2
	seconds := float64(totalCalls) / ratePerSec
	return time.Duration(seconds) * time.Second
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `pixeltui — a fast terminal music player
  YouTube Music for playback · Last.fm for recommendations

USAGE
  pixeltui                          open the player (search-first)
  pixeltui [track] [artist]         start seeded from a track
  pixeltui setup                    interactive config (key, scrobbling, Subsonic, folders)
  pixeltui scrobble-auth            authorize Last.fm scrobbling (one-time)
  pixeltui serve [--tunnel --addr]  run the companion-app server (pair via QR)
  pixeltui devices                  list devices paired with the server
  pixeltui devices revoke <id>      unpair a device (running serve needs a restart)
  pixeltui update [version]         self-update: latest, or a tag like v0.2.4 (rollback)
  pixeltui version                  print the build version
  pixeltui doctor [--fix]           check setup; --fix auto-resolves what it can
  pixeltui reset [cache|graph|library|config|all]   wipe data (keeps tools)
  pixeltui uninstall [--keep-data] [-y]             remove pixeltui + data (full clean)
  pixeltui export <playlist> [file] write a playlist as XSPF (portable)
  pixeltui build-graph              build the recommendation graph (run once)
  pixeltui cache warm --artist X    pre-fetch an artist for offline use
  pixeltui cache stats | clear      show / wipe the cache

SERVER (companion app)
  pixeltui serve                    LAN only — prints a pairing QR + code
  pixeltui serve --tunnel tailscale reachable from anywhere on your tailnet (private)
  pixeltui serve --tunnel cloudflare  public trycloudflare.com URL, no account
  pixeltui serve --tunnel ngrok     public ngrok URL (needs an authtoken)
  pixeltui serve --url https://…    you run the tunnel; pixeltui advertises it
  Defaults (addr/name/tunnel) live in setup → "Companion server", so a plain
  serve remembers your choice. Streams YouTube/Subsonic/local, shares
  likes/playlists/history with the TUI, and scrobbles client plays.

FLAGS
  --key KEY              Last.fm API key (or set LASTFM_API_KEY)
  --explore 0-10         discovery level: 0 safe · 5 default · 10 wild
  --deep-cuts            skip top hits; surface album cuts & deep catalogue
  --no-artist "X,Y"      exclude artists from results (comma-separated)
  --n 10                 number of recommendations
  --no-tui               print a plain numbered list (no interactive UI)
  --offline              use only local graph/cache (no network)
  --dev                  show per-signal scoring breakdown
  --graph PATH           override graph file (default ~/.pixeltui/graph.bin)
  --cache PATH           override cache file (default ~/.pixeltui/cache.db)

CONTROLS                          (press ? in the app for this list anytime)
  One rule for track keys: lowercase acts on the HIGHLIGHTED track,
  SHIFT acts on what's PLAYING.  (e.g. f = like selected · F = like playing)

  Playback   (always the now-playing track)
    ↵                play selected track
    space            pause / resume   (or play, if stopped)
    ← → / h l        seek (step configurable in settings)
    n                next track        ;  jump to the playing track
    + / −            volume up / down
  Track    (lower = highlighted · SHIFT = now-playing)
    f / F            like / unlike (♥)
    a                add to queue          (A = play next / front)
    p / P            add to playlist
    d / D            download to your music folder
    x / X            mute artist for this session  (X also skips)
    .                actions menu — all of the above + go to artist / album
    o / O            start an endless station (from selection / playing)
  Pages
    !a <artist>      artist page: top songs · albums · singles (+ Last.fm stats)
    !al <album>      album pages: ordered tracklist, year, durations
    esc              back through pages (artist → album → results)
  Queue    (Tab switches focus: Discover ⇄ Up Next)
    ↑ / ↓            navigate          j / k  reorder (Up Next focused)
    del              remove            s  shuffle · r  repeat · c  clear
    u                undo the last clear / remove / shuffle
  Modes
    /                search — the prompt shows the source; Tab switches it
                       (YouTube · Subsonic · Local)
    '                filter the current list in place (fuzzy)
    b                browse: Liked · playlists · charts · stats · Local · Subsonic
                       (in browse: del delete · p rename · u restore · o station
                        from a playlist — blends up to 4 random seeds)
    y                lyrics — [ / ] nudge sync · 0 reset
    z  autoplay      t  sleep timer    ,  settings
    Tab              switch pane       ?  all keys
    q                quit              esc  back / close

SCROBBLING  (Last.fm + ListenBrainz — optional)
  pixeltui setup     enter your Last.fm API key + shared secret (same page) and/or
                     a ListenBrainz token, then authorize in the browser once.
  pixeltui scrobble-auth   redo the Last.fm authorization anytime
  Plays submit at 50% / 4 min (the standard rule); offline plays are spooled in
  ~/.pixeltui/library/scrobble-spool.jsonl and retried on the next launch.
  Toggle live in settings (,).

PLAYBACK SETUP                    (or just run:  pixeltui doctor)
  Streams resolve natively (built-in InnerTube resolver, ~0.2s) — no external
  tool needed for playback.
  mpv      recommended — enables pause/seek/volume + OS Now Playing:
             make stream-setup        # standalone bundle, no package manager
             Without mpv, audio still plays (ffplay/afplay) but no controls.
  yt-dlp   optional — powers downloads (d key) and is the stream-resolution
             fallback for rare videos the native resolver can't handle:
             pixeltui doctor --fix    # installs a self-contained binary

SPEED
  Cold play start is ~0.2–0.5s: native stream resolution plus preloading and
  an on-disk stream-URL cache. If the native resolver is ever blocked, the
  yt-dlp fallback kicks in (make fast-ytdlp installs the quick pip build;
  auto-detected at ~/.pixeltui/ytdlp-venv, or set PIXELTUI_YTDLP=/path).

LIBRARY  (open, portable formats under ~/.pixeltui/library/)
  Likes & playlists  →  M3U8  (export to XSPF: pixeltui export <name>)
  Listening history  →  ListenBrainz-style JSONL
  Up Next + session  →  restored on next launch

DOWNLOADS  (set a folder in 'pixeltui setup', then press D in the app)
  Saved as Artist/Album/Title with embedded tags + cover art —
  drop the folder into Navidrome/Subsonic and it just works.

SUBSONIC  (optional 2nd source — your own Navidrome/Subsonic server)
  export PIXELTUI_SUBSONIC_URL=https://music.example.com
  export PIXELTUI_SUBSONIC_USER=you   PIXELTUI_SUBSONIC_PASS=secret
  Then press  b  in the app (browse) and pick it. Subsonic streams play directly.

DATA LAYERS                       (checked in order per query)
  1  ~/.pixeltui/graph.bin        prebuilt artist graph (instant, offline)
  2  ~/.pixeltui/cache.db         cached results + stream URLs (offline)
  3  Last.fm API                  live lookups (online, auto-cached)
  4  stale cache                  expired but usable when offline

Free Last.fm API key:  https://www.last.fm/api/account/create`)
}
