// Package surface implements the `nvim-mcp hook` subcommand: a Claude Code
// PostToolUse hook that opens files Claude edits into the live Neovim session,
// so the human sees the agent's work in their editor.
//
// It is bundled into the binary itself (rather than shipped as a separate shell
// script) so a `go install`-ed nvim-mcp is fully self-contained: the same
// executable the user already registered as the MCP server is also the hook.
// That removes the old script's dependencies on a checked-out repo, python3,
// and the `nvim` CLI.
//
// Scoping: a file is surfaced if it is a real file on disk AND does not live in
// a temporary/scratch location. Files anywhere else are surfaced, including
// outside the current project. Excluded are the OS temp dirs (where Claude's
// scratchpad lives) and the machine-managed session subtrees under ~/.claude
// (plans, transcripts, shell snapshots, …). The rest of ~/.claude —
// settings.json, CLAUDE.md, agents/, skills/, memory/, hooks/, … — is
// user-authored config the human edits and wants reloaded, so it is NOT
// excluded. Anything skipped can still be opened deliberately with vim_file_open.
package surface

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

// payload is the subset of the Claude Code PostToolUse hook JSON we read: the
// edited file's path (Edit / Write / MultiEdit all report it as file_path).
type payload struct {
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// luaSurface adds the edited file as a buffer and refreshes it from disk. The
// path arrives as the sole Lua argument and is escaped for the ex-command.
// `set autoread` + `checktime` make an already-open buffer pick up the change.
const luaSurface = `
local path = ...
vim.cmd('badd ' .. vim.fn.fnameescape(path))
vim.o.autoread = true
vim.cmd('checktime')
return 1
`

// Run reads a PostToolUse hook payload from r and surfaces the edited file into
// Neovim, unless it is a temporary/scratch path (see excluded). It is
// intentionally quiet and never reports an error: the hook must never block or
// fail a tool call, so main always exits 0. socketFlag is an optional explicit
// nvim address override (normally empty).
func Run(r io.Reader, socketFlag string) {
	b, err := io.ReadAll(r)
	if err != nil {
		return
	}
	var p payload
	if err := json.Unmarshal(b, &p); err != nil {
		return
	}

	file := p.ToolInput.FilePath
	if file == "" {
		return
	}
	// Only surface something backed by a real file on disk — never an
	// unnamed/nonexistent scratch target.
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		return
	}

	abs, err := filepath.EvalSymlinks(file)
	if err != nil {
		return
	}
	if excluded(abs) {
		return
	}

	c := nvimc.New(socketFlag)
	defer c.Close()
	_ = c.Lua(luaSurface, nil, abs) // best-effort: no live nvim, no surface
}

// claudeTransientDirs are the machine-managed subdirectories of ~/.claude that
// hold session/transcript/scratch state Claude writes as it runs. They are
// skipped so those never flood the buffer list — unlike the user's own config
// and content in ~/.claude (settings.json, CLAUDE.md, agents/, skills/,
// memory/, hooks/, statusline-command.sh, …), which is surfaced normally.
var claudeTransientDirs = []string{
	"plans", "projects", "sessions", "session-env", "shell-snapshots",
	"tasks", "jobs", "todos", "telemetry", "cache", "paste-cache",
	"file-history", "backups", "debug", "downloads", "daemon", "logs", "statsig",
}

// excluded reports whether abs lives in a temporary/scratch location that should
// not be auto-opened: any OS temp dir ($TMPDIR / /tmp / /private/tmp /
// /var/folders — where Claude's scratchpad lives) or one of the transient
// session subtrees under ~/.claude (see claudeTransientDirs). abs is expected
// already symlink-resolved; each excluded root is resolved the same way so the
// comparison holds despite the macOS /tmp → /private/tmp and /var → /private/var
// symlinks.
func excluded(abs string) bool {
	roots := []string{"/tmp", "/private/tmp", "/var/folders", "/private/var/folders"}
	if t := os.TempDir(); t != "" {
		roots = append(roots, t)
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, sub := range claudeTransientDirs {
			roots = append(roots, filepath.Join(home, ".claude", sub))
		}
	}
	for _, r := range roots {
		if resolved, err := filepath.EvalSymlinks(r); err == nil {
			r = resolved
		}
		if abs == r || strings.HasPrefix(abs, r+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
