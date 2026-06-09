package ytm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// innerTubeKey is YouTube Music's public WEB_REMIX InnerTube key (same one the
// ytmusic library uses for search) — no account/auth needed.
const innerTubeKey = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"

// Charts returns the *current* YouTube Music top-tracks chart for a country.
// country is an ISO-3166 alpha-2 code (e.g. "US") or "ZZ"/"" for Global.
//
// YouTube's no-auth charts page doesn't embed a song list directly — it links to
// per-country chart *playlists* (e.g. "Top 100 Music Videos — United States").
// So this does two browse calls: discover the chart playlist, then fetch it.
// Rows are title + artist; playback resolves the stream at play time (like recs).
func Charts(country string, limit int) ([]engine.Candidate, error) {
	if country == "" {
		country = "ZZ"
	}
	// The chart country is selected via formData; context `gl` must be a real
	// country code — "ZZ" (Global) isn't, so fall back to "US".
	gl := country
	if len(gl) != 2 || gl == "ZZ" {
		gl = "US"
	}

	page, err := browse(map[string]interface{}{
		"browseId": "FEmusic_charts",
		"formData": map[string]interface{}{"selectedValues": []string{country}},
		"context":  innerContext(gl),
	})
	if err != nil {
		return nil, err
	}
	playlistID := findChartPlaylist(page)
	if playlistID == "" {
		return nil, fmt.Errorf("charts: no chart playlist found")
	}

	pl, err := browse(map[string]interface{}{
		"browseId": playlistID,
		"context":  innerContext(gl),
	})
	if err != nil {
		return nil, err
	}
	out := parseTrackRows(pl, limit)
	if len(out) == 0 {
		return nil, fmt.Errorf("charts: no tracks parsed")
	}
	return out, nil
}

func innerContext(gl string) map[string]interface{} {
	return map[string]interface{}{
		"client": map[string]interface{}{
			"clientName":    "WEB_REMIX",
			"clientVersion": "1.20220715.04.00",
			"hl":            "en",
			"gl":            gl,
		},
	}
}

func browse(payload map[string]interface{}) (interface{}, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST",
		"https://music.youtube.com/youtubei/v1/browse?alt=json&key="+innerTubeKey,
		bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.77 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("charts: HTTP %d", resp.StatusCode)
	}
	var root interface{}
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return nil, err
	}
	return root, nil
}

// findChartPlaylist picks the chart playlist id from the charts page. It prefers
// "Trending" — the genuinely *current* chart — over the "Daily Top"/"Top 100"
// lists, which YouTube ranks by raw view count and are therefore dominated by
// evergreen music videos (Michael Jackson, etc.) rather than what's hot now.
func findChartPlaylist(root interface{}) string {
	type card struct{ title, id string }
	var cards []card
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if tr, ok := t["musicTwoRowItemRenderer"].(map[string]interface{}); ok {
				id := str(dig(tr, "navigationEndpoint", "browseEndpoint", "browseId"))
				title := runText(dig(tr, "title", "runs"))
				if strings.HasPrefix(id, "VL") {
					cards = append(cards, card{strings.ToLower(title), id})
				}
				return
			}
			for _, c := range t {
				walk(c)
			}
		case []interface{}:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(root)

	for _, want := range []string{"trending", "daily top", "top 100"} {
		for _, c := range cards {
			if strings.Contains(c.title, want) {
				return c.id
			}
		}
	}
	if len(cards) > 0 {
		return cards[0].id
	}
	return ""
}

// parseTrackRows pulls title+artist from every track row in a playlist response,
// skipping artist/non-song rows.
func parseTrackRows(root interface{}, limit int) []engine.Candidate {
	var out []engine.Candidate
	seen := map[string]bool{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		if limit > 0 && len(out) >= limit {
			return
		}
		switch t := v.(type) {
		case map[string]interface{}:
			if mr, ok := t["musicResponsiveListItemRenderer"].(map[string]interface{}); ok {
				cols, _ := mr["flexColumns"].([]interface{})
				title := cleanTitle(cleanText(flexText(cols, 0)))
				artist := cleanText(flexText(cols, 1))
				if title != "" && artist != "" && !strings.Contains(strings.ToLower(artist), "subscriber") {
					k := strings.ToLower(title + "|" + artist)
					if !seen[k] {
						seen[k] = true
						out = append(out, engine.Candidate{Track: title, Artist: artist})
					}
				}
				return
			}
			for _, c := range t {
				walk(c)
			}
		case []interface{}:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(root)
	return out
}

// ── tiny JSON helpers ────────────────────────────────────────────────────────

func dig(v interface{}, keys ...string) interface{} {
	for _, k := range keys {
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func str(v interface{}) string { s, _ := v.(string); return s }

// runText returns the text of the first run in a []runs value.
func runText(runs interface{}) string {
	a, ok := runs.([]interface{})
	if !ok || len(a) == 0 {
		return ""
	}
	return str(dig(a[0], "text"))
}

func flexText(cols []interface{}, i int) string {
	if i < 0 || i >= len(cols) {
		return ""
	}
	return runText(dig(cols[i], "musicResponsiveListItemFlexColumnRenderer", "text", "runs"))
}

var (
	// Parenthetical/bracket noise: "(Official Video)", "[Official Audio]",
	// "(Lyric Video)", "(Visualizer)", "(4K)", "(Explicit)" — but NOT "(feat. …)",
	// "(Live)", "(Remix)", "(Acoustic)", which carry real meaning.
	titleNoise   = regexp.MustCompile(`(?i)\s*[\(\[][^()\[\]]*\b(official|lyrics?|visuali[sz]er|audio|video|m/?v|hd|4k|explicit|clean version|colou?rs? show)\b[^()\[\]]*[\)\]]`)
	pipeSuffix   = regexp.MustCompile(`\s*\|.*$`) // "Song | From The Block 🎙"
	colorsSuffix = regexp.MustCompile(`(?i)\s*-\s*a colou?rs show.*$`)
	multiSpace   = regexp.MustCompile(`\s{2,}`)
)

// cleanTitle strips YouTube video-title noise so chart rows read like songs.
func cleanTitle(s string) string {
	s = titleNoise.ReplaceAllString(s, "")
	s = colorsSuffix.ReplaceAllString(s, "")
	s = pipeSuffix.ReplaceAllString(s, "")
	s = multiSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "-–—|"))
}

// cleanText strips control and zero-width/bidi/format runes that YouTube titles
// often contain — they break fixed-width terminal layout and OS media widgets.
func cleanText(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f: // control
		case unicode.Is(unicode.Cf, r): // zero-width / bidi / BOM
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
