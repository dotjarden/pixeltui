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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dotjarden/pixeltui/tui/config"
	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/lastfm"
	"github.com/dotjarden/pixeltui/tui/library"
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
	cfg, _ := config.Load(dir)
	if cfg.Theme == "" {
		cfg.Theme = "default"
	}
	localCSV := strings.Join(cfg.LocalDirs, ", ")

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("pixeltui setup").
				Description("Tab/↑↓ move · Enter advances · Esc cancels.\n"),
			huh.NewInput().
				Title("Last.fm API key").
				Description("Recommendations (free: last.fm/api/account/create)").
				Placeholder("optional").
				Value(&cfg.LastfmKey),
			huh.NewSelect[string]().
				Title("Theme").
				Options(huh.NewOptions(tui.ThemeNames()...)...).
				Value(&cfg.Theme),
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
				Value(&localCSV),
			huh.NewInput().
				Title("Download folder").
				Description("Artist/Album layout for Navidrome (optional)").
				Value(&cfg.DownloadDir),
		),
		huh.NewGroup(
			huh.NewNote().Title("Charts").Description("Live YouTube Music Top charts (no API key needed).\n"),
			huh.NewConfirm().Title("Show the Global Top chart?").Value(&cfg.Charts.Global),
			huh.NewInput().
				Title("Country chart").
				Description("Country name or 2-letter code, e.g. United States / GB (blank = off)").
				Value(&cfg.Charts.Country),
		),
	)

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("Setup cancelled — nothing saved.")
			return
		}
		fatalf("setup: %v", err)
	}

	cfg.LocalDirs = nil
	for _, d := range strings.Split(localCSV, ",") {
		if d = strings.TrimSpace(d); d != "" {
			cfg.LocalDirs = append(cfg.LocalDirs, d)
		}
	}

	if err := cfg.Save(dir); err != nil {
		fatalf("save config: %v", err)
	}
	fmt.Printf("\n  Saved → %s\n", config.Path(dir))
	fmt.Println("  Next: 'pixeltui doctor' to verify, or just 'pixeltui' to start.")
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

// cmdServe runs the HTTP server that backs the companion app: browse, search,
// and stream your library (and sources) from anywhere via a BYO tunnel.
func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8787", "bind address")
	urlFlag := fs.String("url", "", "public base URL advertised in the pairing QR (for tunnels)")
	name := fs.String("name", "", "server name shown to clients (default: hostname)")
	fs.Parse(args)

	dir, err := dataDir()
	if err != nil {
		fatalf("%v", err)
	}
	cfg, _ := config.Load(dir)
	lib, _ := library.Open(dir)
	var sub *subsonic.Client
	if cfg.HasSubsonic() {
		sub = subsonic.NewClient(cfg.Subsonic.URL, cfg.Subsonic.User, cfg.Subsonic.Pass)
	}
	err = server.Run(server.Config{
		DataDir:   dir,
		Name:      *name,
		Addr:      *addr,
		URL:       *urlFlag,
		Library:   lib,
		Subsonic:  sub,
		LocalDirs: cfg.LocalDirs,
	})
	if err != nil {
		fatalf("serve: %v", err)
	}
}

// ── update ────────────────────────────────────────────────────────────────────

const repoSlug = "dotjarden/pixeltui"

// cmdUpdate replaces the running binary with the latest GitHub release build for
// this OS/arch (same release URL the installer uses).
func cmdUpdate(_ []string) {
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
	base := "https://github.com/" + repoSlug + "/releases/latest/download/"

	tag := latestTag() // best-effort, for the message
	if tag != "" {
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
	if tag != "" {
		fmt.Printf("  now on %s\n", tag)
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

	// yt-dlp (required) — self-resolvable.
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
		bad("yt-dlp", "NOT FOUND — playback won't work. Fix: pixeltui doctor --fix")
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
  pixeltui setup                    interactive config (key, Subsonic, folders)
  pixeltui serve [--addr --url]     run the server for the companion app (pair via QR)
  pixeltui update                   self-update to the latest release
  pixeltui version                  print the build version
  pixeltui doctor [--fix]           check setup; --fix auto-resolves what it can
  pixeltui reset [cache|graph|library|config|all]   wipe data (keeps tools)
  pixeltui uninstall [--keep-data] [-y]             remove pixeltui + data (full clean)
  pixeltui export <playlist> [file] write a playlist as XSPF (portable)
  pixeltui build-graph              build the recommendation graph (run once)
  pixeltui cache warm --artist X    pre-fetch an artist for offline use
  pixeltui cache stats | clear      show / wipe the cache

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
    ← → / h l        seek −10s / +10s
    n                next track
    + / −            volume up / down
  Track    (lower = highlighted · SHIFT = now-playing)
    f / F            like / unlike (♥)
    a                add to queue          (A = play next / front)
    p / P            add to playlist
    d / D            download to your music folder
    x / X            mute artist for this session  (X also skips)
    .                actions menu — all of the above + play-next & station
    o / O            start an endless station (from selection / playing)
  Queue    (Tab switches focus: Discover ⇄ Up Next)
    ↑ / ↓            navigate          j / k  reorder (Up Next focused)
    del              remove            s  shuffle · r  repeat · c  clear
  Modes
    /                search — the prompt shows the source; Tab switches it
                       (YouTube · Subsonic · Local)
    '                filter the current list in place (fuzzy)
    b                browse: Liked · playlists · Local · Subsonic · save queue
                       (in browse: del = delete · p = rename a playlist)
    y                lyrics            z  autoplay        t  sleep timer
    Tab              switch pane       ?  all keys
    q                quit              esc  back / close

PLAYBACK SETUP                    (or just run:  pixeltui doctor)
  yt-dlp   required — resolves the audio stream. Single binary:
             curl -fsSL https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_macos \
               -o /usr/local/bin/yt-dlp && chmod +x /usr/local/bin/yt-dlp
             (Linux: the "yt-dlp" asset · Windows: yt-dlp.exe)
  mpv      recommended — enables pause/seek/volume + OS Now Playing:
             make stream-setup        # standalone bundle, no package manager
             Without mpv, audio still plays (ffplay/afplay) but no controls.

SPEED
  make fast-ytdlp    install pip yt-dlp (~0.6s startup vs ~8s for the
                     standalone) → cold play drops from ~20s to ~3s.
                     Auto-detected at ~/.pixeltui/ytdlp-venv, or set
                     PIXELTUI_YTDLP=/path/to/yt-dlp.
  Preloading + an on-disk stream-URL cache make most plays start in ~0.2s.

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
