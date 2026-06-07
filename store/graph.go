package store

import (
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"os"
	"strings"
	"time"

	"pixeltui/lastfm"
)

// GraphData is the on-disk format for the pre-built artist similarity graph.
// It is gob-encoded and gzip-compressed.
type GraphData struct {
	Version  int
	BuiltAt  time.Time
	Artists  map[string]GraphArtist // key: lowercase artist name
}

// GraphArtist holds pre-fetched similarity data for one artist.
type GraphArtist struct {
	Name           string // original casing
	SimilarArtists []GraphSim
	TopTracks      []GraphTrack
}

// GraphSim is a compact similar-artist reference.
type GraphSim struct {
	Name  string
	Match float32
}

// GraphTrack is a compact top-track reference.
type GraphTrack struct {
	Name      string
	Listeners int32
}

// GraphReader is a loaded, read-only artist graph. It is safe for concurrent reads.
type GraphReader struct {
	data *GraphData
}

// LoadGraph reads and decompresses a graph file from disk.
func LoadGraph(path string) (*GraphReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	defer gz.Close()

	var data GraphData
	if err := gob.NewDecoder(gz).Decode(&data); err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	return &GraphReader{data: &data}, nil
}

// SaveGraph writes graph data to disk (gob + gzip).
func SaveGraph(path string, data *GraphData) error {
	f, err := os.CreateTemp("", "pixeltui-graph-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()

	gz := gzip.NewWriter(f)
	if err := gob.NewEncoder(gz).Encode(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// Atomic replace
	return os.Rename(tmpPath, path)
}

// ArtistCount returns the number of artists in the graph.
func (g *GraphReader) ArtistCount() int { return len(g.data.Artists) }

// BuiltAt returns when the graph was built.
func (g *GraphReader) BuiltAt() time.Time { return g.data.BuiltAt }

// GraphData exposes the underlying data so the builder can resume from it.
func (g *GraphReader) GraphData() *GraphData { return g.data }

// lookup returns the graph node for an artist (case-insensitive), or nil.
func (g *GraphReader) lookup(artist string) *GraphArtist {
	node, ok := g.data.Artists[strings.ToLower(artist)]
	if !ok {
		return nil
	}
	return &node
}

// GetSimilarArtists returns similar artists from the graph, or (nil, false) if not found.
func (g *GraphReader) GetSimilarArtists(artist string, limit int) ([]lastfm.SimilarArtist, bool) {
	node := g.lookup(artist)
	if node == nil {
		return nil, false
	}
	sims := node.SimilarArtists
	if limit > 0 && len(sims) > limit {
		sims = sims[:limit]
	}
	out := make([]lastfm.SimilarArtist, len(sims))
	for i, s := range sims {
		out[i] = lastfm.SimilarArtist{
			Name:  s.Name,
			Match: lastfm.FlexFloat(s.Match),
		}
	}
	return out, true
}

// GetArtistTopTracks returns top tracks from the graph, or (nil, false) if not found.
func (g *GraphReader) GetArtistTopTracks(artist string, limit int) ([]lastfm.TopTrack, bool) {
	node := g.lookup(artist)
	if node == nil {
		return nil, false
	}
	tracks := node.TopTracks
	if limit > 0 && len(tracks) > limit {
		tracks = tracks[:limit]
	}
	out := make([]lastfm.TopTrack, len(tracks))
	for i, t := range tracks {
		out[i] = lastfm.TopTrack{
			Name: t.Name,
			Artist: struct {
				Name string `json:"name"`
			}{Name: node.Name},
			Listeners: lastfm.FlexInt(t.Listeners),
		}
	}
	return out, true
}
