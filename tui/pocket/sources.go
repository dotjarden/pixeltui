package pocket

import (
	"github.com/dotjarden/pixeltui/tui/engine"
	"github.com/dotjarden/pixeltui/tui/source"
)

// Sources supplies the browsable library/catalog the menus list. Any field may
// be nil (its menu just shows empty). cmdPocket wires these to the library +
// the central source registry.
type Sources struct {
	Registry *source.Registry // optional: drives charts and source-aware downloads

	Liked      func() []engine.Candidate            // liked tracks
	Playlists  func() []string                      // playlist names
	Playlist   func(name string) []engine.Candidate // a playlist's tracks
	History    func() []engine.Candidate            // recently played
	Charts     func() []engine.Candidate            // top charts (sourced from the registry)
	Downloaded func() []engine.Candidate            // on-device files (downloads + local) — play offline

	// Per-track actions (the hold-Y menu); any may be nil.
	Like     func(engine.Candidate)
	Unlike   func(engine.Candidate)
	IsLiked  func(engine.Candidate) bool
	Download func(engine.Candidate) error
}
