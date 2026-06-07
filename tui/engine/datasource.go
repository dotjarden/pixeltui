package engine

import "github.com/dotjarden/pixeltui/tui/lastfm"

// DataSource abstracts where music similarity data comes from.
// *lastfm.Client, *store.Hybrid, and any test double all satisfy this interface.
type DataSource interface {
	GetSimilarTracks(artist, track string, limit int) ([]lastfm.SimilarTrack, error)
	GetTrackTags(artist, track string) ([]string, error)
	GetSimilarArtists(artist string, limit int) ([]lastfm.SimilarArtist, error)
	GetArtistTopTracks(artist string, limit int) ([]lastfm.TopTrack, error)
}
