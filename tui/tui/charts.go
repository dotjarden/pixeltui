package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/ytm"
)

// chartFetcher returns a current top-tracks chart for a country code
// ("ZZ"/"" = Global). Backed by YouTube Music; faked in tests.
type chartFetcher interface {
	ChartTracks(country string, limit int) ([]engine.Candidate, error)
}

// ytmCharts is the real fetcher (no API key needed).
type ytmCharts struct{}

func (ytmCharts) ChartTracks(country string, limit int) ([]engine.Candidate, error) {
	return ytm.Charts(country, limit)
}

// countryCode resolves a config value to a YouTube Music country code: a 2-letter
// code is used as-is; common country names are mapped; otherwise "" (disabled).
func countryCode(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 2 {
		return strings.ToUpper(s)
	}
	if c, ok := countryNames[strings.ToLower(s)]; ok {
		return c
	}
	return ""
}

var countryNames = map[string]string{
	"united states": "US", "usa": "US", "america": "US",
	"united kingdom": "GB", "uk": "GB", "great britain": "GB", "england": "GB",
	"canada": "CA", "australia": "AU", "ireland": "IE", "new zealand": "NZ",
	"germany": "DE", "france": "FR", "spain": "ES", "italy": "IT",
	"netherlands": "NL", "belgium": "BE", "portugal": "PT", "austria": "AT",
	"switzerland": "CH", "sweden": "SE", "norway": "NO", "denmark": "DK",
	"finland": "FI", "poland": "PL", "russia": "RU", "ukraine": "UA",
	"mexico": "MX", "brazil": "BR", "argentina": "AR", "chile": "CL",
	"colombia": "CO", "peru": "PE", "japan": "JP", "south korea": "KR",
	"korea": "KR", "india": "IN", "indonesia": "ID", "philippines": "PH",
	"thailand": "TH", "vietnam": "VN", "turkey": "TR", "south africa": "ZA",
	"nigeria": "NG", "egypt": "EG", "saudi arabia": "SA",
	"united arab emirates": "AE", "uae": "AE", "israel": "IL",
}

// capPerArtist keeps a chart varied for the compact For You section: at most
// maxPer tracks per artist, returning at most total tracks (order preserved).
func capPerArtist(cs []engine.Candidate, maxPer, total int) []engine.Candidate {
	seen := map[string]int{}
	out := make([]engine.Candidate, 0, total)
	for _, c := range cs {
		k := strings.ToLower(strings.TrimSpace(c.Artist))
		if seen[k] >= maxPer {
			continue
		}
		seen[k]++
		out = append(out, c)
		if len(out) >= total {
			break
		}
	}
	return out
}

// cmdGlobalChart / cmdGeoChart load a full current chart into Discover (Browse).
func cmdGlobalChart(f chartFetcher) tea.Cmd {
	return func() tea.Msg {
		if f == nil {
			return searchMsg{}
		}
		cs, err := f.ChartTracks("ZZ", 50)
		if err != nil {
			return searchMsg{err: err}
		}
		return searchMsg{results: cs}
	}
}

func cmdGeoChart(f chartFetcher, country string) tea.Cmd {
	return func() tea.Msg {
		code := countryCode(country)
		if f == nil || code == "" {
			return searchMsg{}
		}
		cs, err := f.ChartTracks(code, 50)
		if err != nil {
			return searchMsg{err: err}
		}
		return searchMsg{results: cs}
	}
}

// cmdForYouChart fetches the current chart for the For You landing — the
// configured country chart if set, otherwise the global chart.
func cmdForYouChart(f chartFetcher, country string, global bool) tea.Cmd {
	return func() tea.Msg {
		if f == nil {
			return forYouChartMsg{}
		}
		if code := countryCode(country); code != "" {
			cs, err := f.ChartTracks(code, 40)
			if err != nil {
				return forYouChartMsg{}
			}
			return forYouChartMsg{tracks: capPerArtist(cs, 2, 12), label: country + " Top"}
		}
		if global {
			cs, err := f.ChartTracks("ZZ", 40)
			if err != nil {
				return forYouChartMsg{}
			}
			return forYouChartMsg{tracks: capPerArtist(cs, 2, 12), label: "Top Charts"}
		}
		return forYouChartMsg{}
	}
}
