// Package settings persists user appearance preferences as JSON under
// os.UserConfigDir()/md-view/settings.json.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the persisted user preferences. Defaults follow
// ARCHITECTURE.md: light theme (white), system font, 16px, ~72ch column.
type Settings struct {
	Theme        string `json:"theme"`        // light | dark | sepia | system
	FontFamily   string `json:"fontFamily"`   // "" = system font stack
	FontSize     int    `json:"fontSize"`     // px
	ContentWidth int    `json:"contentWidth"` // ch

	// PrewarmAsked records that the reader has been offered the background
	// prewarm once. It is deliberately separate from whether prewarm is *on*
	// — that lives in the system's login-item registry, not here — so that
	// turning it off in System Settings does not make MDv ask again. An app
	// that re-offers something you declined is nagware.
	PrewarmAsked bool `json:"prewarmAsked"`
}

// Default returns the out-of-the-box settings.
func Default() Settings {
	return Settings{
		Theme:        "light",
		FontFamily:   "",
		FontSize:     16,
		ContentWidth: 72,
	}
}

var validThemes = map[string]bool{"light": true, "dark": true, "sepia": true, "system": true}

// normalize clamps values into sane ranges and falls back to defaults for
// invalid fields, so a hand-edited settings file cannot break the UI.
func normalize(s Settings) Settings {
	d := Default()
	if !validThemes[s.Theme] {
		s.Theme = d.Theme
	}
	if s.FontSize < 9 || s.FontSize > 40 {
		s.FontSize = d.FontSize
	}
	if s.ContentWidth < 40 || s.ContentWidth > 160 {
		s.ContentWidth = d.ContentWidth
	}
	if len(s.FontFamily) > 200 {
		s.FontFamily = d.FontFamily
	}
	return s
}

// Store loads and saves Settings at a fixed path.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore creates a store at os.UserConfigDir()/md-view/settings.json.
func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate user config dir: %w", err)
	}
	return NewStoreAt(filepath.Join(dir, "md-view", "settings.json")), nil
}

// NewStoreAt creates a store at an explicit path (used by tests).
func NewStoreAt(path string) *Store {
	return &Store{path: path}
}

// Load reads the persisted settings, returning defaults when the file does
// not exist yet. A corrupt file is an error (surfaced to the UI, not ignored).
func (s *Store) Load() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), fmt.Errorf("read settings: %w", err)
	}
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		return Default(), fmt.Errorf("parse settings %s: %w", s.path, err)
	}
	return normalize(out), nil
}

// Save validates, normalizes and atomically writes the settings file.
func (s *Store) Save(in Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := normalize(in)
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}
