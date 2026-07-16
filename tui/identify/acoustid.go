package identify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dotjarden/pixeltui/tui/engine"
)

// AcoustID looks up fingerprints against the AcoustID web service. It requires
// an AcoustID API key and is optional.
type AcoustID struct {
	APIKey string
	client *http.Client
}

// NewAcoustID creates an AcoustID identifier. Returns nil if apiKey is empty.
func NewAcoustID(apiKey string) *AcoustID {
	if apiKey == "" {
		return nil
	}
	return &AcoustID{
		APIKey: apiKey,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (a *AcoustID) Name() string { return "acoustid" }

func (a *AcoustID) Available() bool {
	return a != nil && a.APIKey != ""
}

func (a *AcoustID) Identify(ctx context.Context, fingerprint []int32, durationSec int) (Result, error) {
	return a.IdentifyFingerprint(ctx, Fingerprint{DurationSec: durationSec, Data: fingerprint})
}

// IdentifyFingerprint looks up a fingerprint using its compressed base64 form.
func (a *AcoustID) IdentifyFingerprint(ctx context.Context, fp Fingerprint) (Result, error) {
	if fp.Compressed == "" {
		return Result{}, fmt.Errorf("missing compressed fingerprint")
	}

	form := url.Values{
		"client":    {a.APIKey},
		"duration":  {strconv.Itoa(fp.DurationSec)},
		"fingerprint": {fp.Compressed},
		"meta":      {"recordings+releasegroups+tracks+compress"},
		"format":    {"json"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.acoustid.org/v2/lookup", strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("acoustid http %d: %s", resp.StatusCode, string(body))
	}

	var parsed acoustidResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Result{}, fmt.Errorf("parse acoustid response: %w", err)
	}
	if parsed.Status != "ok" {
		return Result{}, fmt.Errorf("acoustid error: %s", parsed.Error.Message)
	}
	// Debug: surface what AcoustID actually returned so a failed match can be
	// diagnosed (empty results = unrecognized/garbage fingerprint; results with
	// no recordings = noisy/low-score fingerprint).
	if len(parsed.Results) == 0 {
		fmt.Printf("acoustid: 0 results (fingerprint not recognized)\n")
	} else {
		top := parsed.Results[0]
		fmt.Printf("acoustid: %d result(s); top score=%.2f recordings=%d\n",
			len(parsed.Results), top.Score, len(top.Recordings))
	}
	return a.bestResult(parsed.Results)
}

type acoustidResponse struct {
	Status  string            `json:"status"`
	Error   acoustidError     `json:"error"`
	Results []acoustidResult  `json:"results"`
}

type acoustidError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type acoustidResult struct {
	ID        string              `json:"id"`
	Score     float64             `json:"score"`
	Recordings []acoustidRecording `json:"recordings"`
}

type acoustidRecording struct {
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	Artists []acoustidArtist `json:"artists"`
	Duration int            `json:"duration"`
	ReleaseGroups []acoustidRelease `json:"releasegroups"`
}

type acoustidArtist struct {
	Name string `json:"name"`
}

type acoustidRelease struct {
	Title string `json:"title"`
}

func (a *AcoustID) bestResult(results []acoustidResult) (Result, error) {
	for _, r := range results {
		if len(r.Recordings) == 0 {
			continue
		}
		rec := r.Recordings[0]
		artist := ""
		if len(rec.Artists) > 0 {
			artist = rec.Artists[0].Name
		}
		album := ""
		if len(rec.ReleaseGroups) > 0 {
			album = rec.ReleaseGroups[0].Title
		}
		return Result{
			Source: a.Name(),
			Score:  r.Score,
			Candidate: engine.Candidate{
				Artist:      artist,
				Track:       rec.Title,
				Album:       album,
				DurationSec: rec.Duration,
			},
		}, nil
	}
	return Result{}, ErrNoMatch
}
