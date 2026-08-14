// Package config persists small user preferences for nvim-mcp across server
// restarts. Each preference is a tri-state: unset (never decided), on, or off,
// modelled as a *bool (nil = unset). The file lives at
// $XDG_CONFIG_HOME/nvim-mcp/config.json (or ~/.config/nvim-mcp/config.json).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// file is the on-disk JSON shape. A nil field means "unset" — the user has not
// been asked — which is distinct from an explicit false.
type file struct {
	HighlightEdits *bool `json:"highlightEdits,omitempty"`
	SurfaceHook    *bool `json:"surfaceHook,omitempty"`
}

// Store is a goroutine-safe accessor over the persisted preferences. Tool
// handlers may run concurrently, so every read/write takes the mutex.
type Store struct {
	mu   sync.Mutex
	path string
	f    file
}

// Load reads the config file if present. A missing or malformed file yields a
// store with every preference unset, never an error — first run is normal.
func Load() *Store {
	s := &Store{path: configPath()}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.f)
	}
	return s
}

// HighlightEdits reports the stored preference and whether a choice was made.
func (s *Store) HighlightEdits() (enabled, decided bool) { return s.get(func() *bool { return s.f.HighlightEdits }) }

// SetHighlightEdits records and persists the edit-highlighting preference.
func (s *Store) SetHighlightEdits(enabled bool) error {
	return s.set(func(f *file) { f.HighlightEdits = &enabled })
}

// SurfaceHook reports the stored preference for the auto-open PostToolUse hook
// and whether a choice was made. Note: this is only the recorded intent; use
// SurfaceHookInstalled to check whether the hook is actually in settings.json.
func (s *Store) SurfaceHook() (enabled, decided bool) { return s.get(func() *bool { return s.f.SurfaceHook }) }

// SetSurfaceHook records and persists the surface-hook preference.
func (s *Store) SetSurfaceHook(enabled bool) error {
	return s.set(func(f *file) { f.SurfaceHook = &enabled })
}

func (s *Store) get(field func() *bool) (enabled, decided bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := field()
	if p == nil {
		return false, false
	}
	return *p, true
}

func (s *Store) set(mut func(*file)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mut(&s.f)
	return s.save()
}

// save writes the current preferences. Caller holds mu.
func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(b, '\n'), 0o644)
}

// configPath resolves the config file location, preferring XDG_CONFIG_HOME and
// falling back to ~/.config so it is predictable on macOS and Linux alike.
func configPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "nvim-mcp", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "nvim-mcp", "config.json")
}
