package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWhenMissing(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "settings.json"))
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != Default() {
		t.Errorf("Load on missing file = %+v, want defaults %+v", got, Default())
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "nested", "settings.json"))
	in := Settings{Theme: "dark", FontFamily: "Georgia, serif", FontSize: 18, ContentWidth: 90}
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != in {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

func TestSaveNormalizesInvalidValues(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "settings.json"))
	if err := s.Save(Settings{Theme: "neon", FontSize: 900, ContentWidth: 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := Default()
	if got.Theme != d.Theme || got.FontSize != d.FontSize || got.ContentWidth != d.ContentWidth {
		t.Errorf("invalid values not normalized: %+v", got)
	}
}

func TestLoadCorruptFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStoreAt(path)
	got, err := s.Load()
	if err == nil {
		t.Errorf("Load of corrupt file must error (never silently ignored)")
	}
	if got != Default() {
		t.Errorf("corrupt file should still yield usable defaults, got %+v", got)
	}
}

func TestDefaultMatchesArchitecture(t *testing.T) {
	d := Default()
	if d.Theme != "light" || d.FontSize != 16 || d.ContentWidth != 72 || d.FontFamily != "" {
		t.Errorf("defaults deviate from ARCHITECTURE.md: %+v", d)
	}
}
