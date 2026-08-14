package config

import (
	"os"
	"path/filepath"
	"strings"
)

// surfaceHookMarker uniquely identifies the surface-in-nvim PostToolUse hook in
// a Claude settings file. Matching the script's basename is schema-agnostic: it
// works whether the entry is written as a bare command or a wrapped invocation.
const surfaceHookMarker = "surface-in-nvim"

// SurfaceHookInstalled reports whether the auto-open PostToolUse hook is present
// in any of the Claude settings files that could carry it: the user-level files
// under ~/.claude, and the project-level files under <cwd>/.claude. This is the
// source of truth for "is the hook actually wired in", independent of the
// recorded preference — so a hook added or removed by hand is reflected too.
func SurfaceHookInstalled() bool {
	for _, p := range settingsCandidates() {
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), surfaceHookMarker) {
			return true
		}
	}
	return false
}

func settingsCandidates() []string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".claude", "settings.json"),
			filepath.Join(home, ".claude", "settings.local.json"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(cwd, ".claude", "settings.json"),
			filepath.Join(cwd, ".claude", "settings.local.json"),
		)
	}
	return paths
}
