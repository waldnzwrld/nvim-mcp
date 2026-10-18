package config

import (
	"os"
	"path/filepath"
	"strings"
)

// surfaceHookMarker uniquely identifies the auto-open PostToolUse hook in an
// agent settings file. The hook is the binary invoking itself as `nvim-mcp
// hook`, so its command string always contains this substring regardless of the
// absolute path it is installed with (e.g. "/Users/me/go/bin/nvim-mcp hook").
const surfaceHookMarker = "nvim-mcp hook"

// SurfaceHookCommand returns the command string to place in a PostToolUse hook
// to enable auto-open: the absolute path of this running executable followed by
// the `hook` subcommand. Using the resolved executable path (not a bare
// "nvim-mcp") makes the hook work regardless of the caller's PATH. It falls
// back to a bare "nvim-mcp hook" only if the path can't be resolved.
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
// in any of the agent settings files that could carry it: user-level (Claude
// Code's ~/.claude, Pi's ~/.pi/agent) and project-level (<cwd>/.claude,
// <cwd>/.pi/agent). This is the source of truth for "is the hook actually wired
// in", independent of the recorded preference — so a hook added or removed by
// hand is reflected too.
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
	home, _ := os.UserHomeDir()

	// --- Claude Code paths ---
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		paths = append(paths,
			filepath.Join(dir, "settings.json"),
			filepath.Join(dir, "settings.local.json"),
		)
	} else if home != "" {
		paths = append(paths,
			filepath.Join(home, ".claude", "settings.json"),
			filepath.Join(home, ".claude", "settings.local.json"),
		)
	}

	// --- Pi paths ---
	if dir := os.Getenv("PI_AGENT_DIR"); dir != "" {
		paths = append(paths,
			filepath.Join(dir, "settings.json"),
			filepath.Join(dir, "settings.local.json"),
		)
	} else if home != "" {
		paths = append(paths,
			filepath.Join(home, ".pi", "agent", "settings.json"),
			filepath.Join(home, ".pi", "agent", "settings.local.json"),
		)
	}

	// --- Project-level paths (Claude Code) ---
	if cwd, err := os.Getwd(); err == nil {
		if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
			paths = append(paths,
				filepath.Join(dir, "settings.json"),
				filepath.Join(dir, "settings.local.json"),
			)
		} else {
			paths = append(paths,
				filepath.Join(cwd, ".claude", "settings.json"),
				filepath.Join(cwd, ".claude", "settings.local.json"),
			)
		}
	}

	// --- Project-level paths (Pi) ---
	if cwd, err := os.Getwd(); err == nil {
		if dir := os.Getenv("PI_AGENT_DIR"); dir != "" {
			paths = append(paths,
				filepath.Join(dir, "settings.json"),
				filepath.Join(dir, "settings.local.json"),
			)
		} else {
			paths = append(paths,
				filepath.Join(cwd, ".pi", "agent", "settings.json"),
				filepath.Join(cwd, ".pi", "agent", "settings.local.json"),
			)
		}
	}

	return paths
}
