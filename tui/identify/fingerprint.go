package identify

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"os/exec"
	"time"
)

// Fingerprint is a Chromaprint fingerprint plus its duration in seconds.
// It keeps two representations: the compressed base64 string (for AcoustID)
// and the raw int32 subfingerprints (for the local index).
type Fingerprint struct {
	DurationSec int     `json:"duration_sec"`
	Data        []int32 `json:"data"`
	Compressed  string  `json:"compressed"` // base64 compressed fingerprint (AcoustID)
}

// ComputeFingerprint runs fpcalc on a local audio file and returns the
// Chromaprint fingerprint. Requires the chromaprint fpcalc binary to be on
// PATH.
func ComputeFingerprint(ctx context.Context, path string) (Fingerprint, error) {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		return Fingerprint{}, fmt.Errorf("fpcalc not found — install chromaprint")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Run both fpcalc modes in parallel: default output is the compressed
	// base64 string AcoustID expects; -raw gives the unsigned int array we
	// compare locally. Both are fast and independent.
	type result struct {
		out []byte
		err error
	}
	compCh := make(chan result, 1)
	rawCh := make(chan result, 1)
	go func() {
		out, err := exec.CommandContext(ctx, "fpcalc", "-json", path).Output()
		compCh <- result{out, err}
	}()
	go func() {
		out, err := exec.CommandContext(ctx, "fpcalc", "-json", "-raw", path).Output()
		rawCh <- result{out, err}
	}()

	compRes := <-compCh
	rawRes := <-rawCh
	if compRes.err != nil {
		return Fingerprint{}, fmt.Errorf("fpcalc %s: %w", path, compRes.err)
	}
	if rawRes.err != nil {
		return Fingerprint{}, fmt.Errorf("fpcalc -raw %s: %w", path, rawRes.err)
	}

	var comp struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal(compRes.out, &comp); err != nil {
		return Fingerprint{}, fmt.Errorf("parse fpcalc output: %w", err)
	}

	var raw struct {
		Duration    float64  `json:"duration"`
		Fingerprint []uint32 `json:"fingerprint"`
	}
	if err := json.Unmarshal(rawRes.out, &raw); err != nil {
		return Fingerprint{}, fmt.Errorf("parse fpcalc -raw output: %w", err)
	}

	data := make([]int32, len(raw.Fingerprint))
	for i, u := range raw.Fingerprint {
		data[i] = int32(u) // preserve bit pattern for local comparison
	}
	return Fingerprint{
		DurationSec: int(math.Round(comp.Duration)),
		Data:        data,
		Compressed:  comp.Fingerprint,
	}, nil
}

// compareFingerprints returns a similarity score in [0,1] using the bit error
// rate between two fingerprints. It slides the query across the entire
// reference so a clip captured from anywhere in a track — not just its first
// few seconds — can align. Lower bit error → higher score.
func compareFingerprints(query, ref []int32) float64 {
	if len(query) == 0 || len(ref) == 0 {
		return 0
	}
	qlen := len(query)
	if qlen > len(ref) {
		qlen = len(ref)
	}
	// Full scan: the query is a short clip (~25 subfingerprints for 10 s) and
	// the reference is a whole track (a few hundred), so every alignment is
	// cheap and a one-off identification query can afford the exhaustive pass.
	best := 1.0
	for off := 0; off+qlen <= len(ref); off++ {
		err := bitError(query[:qlen], ref[off:off+qlen])
		if err < best {
			best = err
		}
	}
	if best >= 0.5 {
		return 0
	}
	return 1.0 - (best * 2.0) // scale so 0 error = 1.0, 0.5 error = 0
}

// bitError computes the fraction of differing bits between two equal-length
// fingerprints.
func bitError(a, b []int32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1
	}
	var diff int
	for i := range a {
		diff += bits.OnesCount32(uint32(a[i] ^ b[i]))
	}
	return float64(diff) / float64(len(a)*32)
}
