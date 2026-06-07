// Package local indexes local audio files in given folders and exposes them
// as engine.Candidate tracks. Local files play directly: the StreamURL is the
// absolute file path, which the player opens as-is (no yt-dlp resolution).
package local

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/engine"
)

// Source is the routing tag stamped on every candidate from this package.
const Source = "local"

// audioExts is the set of audio file extensions we include (lowercase).
var audioExts = map[string]bool{
	".mp3": true, ".m4a": true, ".aac": true, ".flac": true,
	".ogg": true, ".opus": true, ".wav": true, ".wma": true,
	".aiff": true, ".aif": true, ".alac": true,
}

// IsAudio reports whether path has a recognized audio extension (case-insensitive).
func IsAudio(path string) bool {
	return audioExts[strings.ToLower(filepath.Ext(path))]
}

// Scan walks each dir recursively and returns all audio files as candidates,
// sorted by Artist then Track. Unreadable dirs/files are skipped (not fatal).
func Scan(dirs []string) ([]engine.Candidate, error) {
	var out []engine.Candidate
	for _, dir := range dirs {
		// WalkDir keeps going on errors thanks to the fn below returning nil.
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				// Unreadable dir/file: skip this entry, keep walking siblings.
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !IsAudio(path) {
				return nil
			}
			abs, aerr := filepath.Abs(path)
			if aerr != nil {
				abs = path
			}
			out = append(out, candidateFor(abs))
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Artist != out[j].Artist {
			return out[i].Artist < out[j].Artist
		}
		return out[i].Track < out[j].Track
	})
	return out, nil
}

// candidateFor builds a playable candidate for one absolute audio file path.
func candidateFor(abs string) engine.Candidate {
	artist, title, dur := metadata(abs)
	return engine.Candidate{
		Track:       title,
		Artist:      artist,
		DurationSec: dur,
		Source:      Source,
		StreamURL:   abs, // player opens the file path directly
	}
}

// metadata returns (artist, title, durationSec) for a file. It prefers ffprobe
// and falls back to parsing the filename when ffprobe is unavailable or yields
// nothing useful.
func metadata(path string) (artist, title string, dur int) {
	if a, t, d, ok := probe(path); ok {
		return a, t, d
	}
	a, t := fromFilename(path)
	return a, t, 0
}

// fromFilename derives artist/title from the base name (no extension):
// "Artist - Title" splits on the first " - "; otherwise Artist="" and Track=base.
func fromFilename(path string) (artist, title string) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if i := strings.Index(base, " - "); i >= 0 {
		return strings.TrimSpace(base[:i]), strings.TrimSpace(base[i+3:])
	}
	return "", base
}

// ffprobeFormat mirrors the slice of ffprobe -show_format JSON we care about.
type ffprobeFormat struct {
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// probe runs ffprobe (if on PATH) with a short timeout and parses tags.
// ok is false when ffprobe is missing, errors, or yields no useful metadata,
// signaling the caller to fall back to the filename.
func probe(path string) (artist, title string, dur int, ok bool) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return "", "", 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet", "-print_format", "json", "-show_format", path)
	data, err := cmd.Output()
	if err != nil {
		return "", "", 0, false
	}
	var f ffprobeFormat
	if json.Unmarshal(data, &f) != nil {
		return "", "", 0, false
	}
	title = firstTag(f.Format.Tags, "title", "TITLE")
	artist = firstTag(f.Format.Tags, "artist", "ARTIST", "album_artist", "ALBUM_ARTIST")
	if s := strings.TrimSpace(f.Format.Duration); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			dur = int(v)
		}
	}
	// Useful only if we recovered at least a title; otherwise fall back so the
	// filename can supply a sensible Track (and possibly Artist).
	if strings.TrimSpace(title) == "" {
		return "", "", 0, false
	}
	return artist, title, dur, true
}

// firstTag returns the first non-empty value among the given tag keys.
func firstTag(tags map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(tags[k]); v != "" {
			return v
		}
	}
	return ""
}
