// Package config manages pixeltui's JSON config file at <dataDir>/config.json.
// It supports a setup wizard (Save) plus env-var overrides at load time.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Subsonic holds credentials for a Subsonic-compatible server.
type Subsonic struct {
	URL  string `json:"url"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

// Charts configures the optional global/country chart surfaces (off by default).
// The auto genre chart (from your listening) needs no config.
type Charts struct {
	Global  bool   `json:"global"`  // show a worldwide Top chart
	Country string `json:"country"` // e.g. "United States" — empty disables the country chart
}

// Config is the persisted application configuration.
type Config struct {
	LastfmKey   string   `json:"lastfm_key"`
	Subsonic    Subsonic `json:"subsonic"`
	LocalDirs   []string `json:"local_dirs"`   // folders of local audio files
	DownloadDir string   `json:"download_dir"` // where downloads are saved
	Theme       string   `json:"theme"`        // accent theme name (default if empty)
	Explore     int      `json:"explore"`      // 0..10, default 5
	Autoplay    bool     `json:"autoplay"`     // default true
	Charts      Charts   `json:"charts"`       // optional global/country charts
}

// Default returns a Config with sensible defaults (Explore=5, Autoplay=true).
func Default() *Config {
	return &Config{Explore: 5, Autoplay: true, Charts: Charts{Global: true}}
}

// Path returns the config file path for the given data directory.
func Path(dataDir string) string {
	return filepath.Join(dataDir, "config.json")
}

// Load reads <dataDir>/config.json and overlays environment variables (env wins).
// A missing or malformed file is non-fatal: it falls back to Default() then
// applies env. It returns an error only for unexpected IO failures.
func Load(dataDir string) (*Config, error) {
	c := Default()

	data, err := os.ReadFile(Path(dataDir))
	switch {
	case err == nil:
		// Parse onto defaults so absent JSON keys keep their default values.
		// A parse error is intentionally ignored: keep the defaults.
		_ = json.Unmarshal(data, c)
	case os.IsNotExist(err):
		// No config yet: defaults + env only.
	default:
		// Unexpected IO error (permissions, etc.).
		return nil, err
	}

	c.applyEnv()
	return c, nil
}

// applyEnv overlays recognized environment variables onto the config.
func (c *Config) applyEnv() {
	if v, ok := os.LookupEnv("LASTFM_API_KEY"); ok {
		c.LastfmKey = v
	}
	if v, ok := os.LookupEnv("PIXELTUI_SUBSONIC_URL"); ok {
		c.Subsonic.URL = v
	}
	if v, ok := os.LookupEnv("PIXELTUI_SUBSONIC_USER"); ok {
		c.Subsonic.User = v
	}
	if v, ok := os.LookupEnv("PIXELTUI_SUBSONIC_PASS"); ok {
		c.Subsonic.Pass = v
	}
	if v, ok := os.LookupEnv("PIXELTUI_LOCAL_DIRS"); ok {
		c.LocalDirs = splitDirs(v)
	}
	if v, ok := os.LookupEnv("PIXELTUI_DOWNLOAD_DIR"); ok {
		c.DownloadDir = v
	}
	if v, ok := os.LookupEnv("PIXELTUI_THEME"); ok {
		c.Theme = v
	}
}

// splitDirs splits a PATH-style list, dropping empty entries.
func splitDirs(v string) []string {
	var out []string
	for _, p := range strings.Split(v, string(os.PathListSeparator)) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Save writes the config as pretty-printed JSON atomically (temp file + rename),
// creating dataDir if needed. The dir is 0700 and the file 0600 since it can
// hold a password.
func (c *Config) Save(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Write to a temp file in the same dir, then atomically rename into place.
	tmp, err := os.CreateTemp(dataDir, "config-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, Path(dataDir))
}

// HasSubsonic reports whether a Subsonic URL is configured.
func (c *Config) HasSubsonic() bool { return c.Subsonic.URL != "" }

// HasLocal reports whether any local directories are configured.
func (c *Config) HasLocal() bool { return len(c.LocalDirs) > 0 }
