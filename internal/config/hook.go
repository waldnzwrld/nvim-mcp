package config

import (
	"os"
	"path/filepath"
	"strings"
)

// surfaceHookMarker uniquely identifies the auto-open PostToolUse hook in a
// Claude settings file. The hook is the binary invoking itself as `nvim-mcp
// hook`, so its command string always contains this substring regardless of the
// absolute path it is installed with (e.g. "/Users/me/go/bin/nvim-mcp hook").
const surfaceHookMarker = "nvim-mcp hook"

// SurfaceHookCommand returns the command string to place in a Claude Code
// PostToolUse hook to enable auto-open: the absolute path of this running
// executable followed by the `hook` subcommand. Using the resolved executable
// path (not a bare "nvim-mcp") makes the hook work regardless of the caller's
// PATH. It falls back to a bare "nvim-mcp hook" only if the path can't be
// resolved.
func SurfaceHookCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "nvim-mcp hook"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe + " hook"
}

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
