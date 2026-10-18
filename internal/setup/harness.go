package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/waldnzwrld/nvim-mcp/internal/config"
)

// ServerName is the name nvim-mcp is registered under in every harness; tool
// permission entries are prefixed with mcp__<ServerName>__.
const ServerName = "nvim"

// Options are the inputs shared by every harness install.
type Options struct {
	Exe      string   // absolute path of the nvim-mcp binary to register
	Allow    []string // permission entries for the server's tools
	AutoOpen bool     // install the harness's auto-open integration
	DryRun   bool     // report what would change, write nothing
}

// Harness is an MCP client nvim-mcp can install itself into.
type Harness struct {
	ID       string // stable id used by --harness
	Name     string
	Detected bool
	install  func(Options) ([]string, error)
}

// Install registers the server and wires the harness's integrations, returning
// one human-readable line per step.
func (h Harness) Install(o Options) ([]string, error) { return h.install(o) }

// Harnesses returns every supported harness with Detected set when its CLI is
// on PATH or its config directory exists.
func Harnesses() []Harness {
	return []Harness{
		{ID: "claude", Name: "Claude Code", Detected: detect("claude", config.ClaudeDir), install: installClaude},
		{ID: "pi", Name: "pi", Detected: detect("pi", config.PiDir), install: installPi},
	}
}

func detect(cli string, dir func() (string, error)) bool {
	if _, err := exec.LookPath(cli); err == nil {
		return true
	}
	d, err := dir()
	if err != nil {
		return false
	}
	info, err := os.Stat(d)
	return err == nil && info.IsDir()
}

func installClaude(o Options) ([]string, error) {
	var out []string

	// `claude mcp get` exits 0 only when the server is registered in some scope.
	line, err := register("claude", exec.Command("claude", "mcp", "get", ServerName).Run() == nil,
		[]string{"mcp", "add", "--scope", "user", ServerName, "--", o.Exe}, o.DryRun)
	out = append(out, line)
	if err != nil {
		return out, err
	}

	path, err := SettingsPath()
	if err != nil {
		return out, err
	}
	added, err := MergeAllow(path, o.Allow, o.DryRun)
	if err != nil {
		return out, err
	}
	switch {
	case len(added) == 0:
		out = append(out, fmt.Sprintf("tools: all %d already allowed in %s", len(o.Allow), path))
	case o.DryRun:
		out = append(out, fmt.Sprintf("tools: would allow %d in %s", len(added), path))
	default:
		out = append(out, fmt.Sprintf("tools: allowed %d in %s", len(added), path))
	}

	switch {
	case !o.AutoOpen:
		out = append(out, "auto-open: off (vim_surface_hook), skipped")
	case config.SurfaceHookInstalled():
		out = append(out, "auto-open: hook already installed")
	default:
		if err := InstallHook(path, o.Exe+" hook", o.DryRun); err != nil {
			return out, err
		}
		out = append(out, fmt.Sprintf("auto-open: %s PostToolUse hook in %s", verb(o.DryRun, "would add", "added"), path))
	}
	return out, nil
}

func installPi(o Options) ([]string, error) {
	var out []string
	dir, err := config.PiDir()
	if err != nil {
		return out, err
	}

	line, err := register("pi", piRegistered(dir),
		[]string{"mcp", "add", ServerName, "--", o.Exe}, o.DryRun)
	out = append(out, line)
	if err != nil {
		return out, err
	}

	// pi runs tools without per-call approval, so there is no allowlist.
	if !o.AutoOpen {
		return append(out, "auto-open: off (vim_surface_hook), skipped"), nil
	}
	path := filepath.Join(dir, "extensions", "nvim-mcp.ts")
	want := piExtension(o.Exe)
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
		return append(out, "auto-open: extension already installed at "+path), nil
	}
	if !o.DryRun {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return out, err
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return out, err
		}
	}
	return append(out, fmt.Sprintf("auto-open: %s extension %s", verb(o.DryRun, "would write", "wrote"), path)), nil
}

// piRegistered reports whether pi's user-level mcp.json already has ServerName.
func piRegistered(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if err != nil {
		return false
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return false
	}
	_, ok := doc.MCPServers[ServerName]
	return ok
}

// register runs `<cli> <args>` unless the server is already registered. A
// missing CLI is reported with the command to run by hand, not as an error.
func register(cli string, registered bool, args []string, dryRun bool) (string, error) {
	cmd := cli + " " + strings.Join(args, " ")
	switch {
	case registered:
		return "server: already registered", nil
	case dryRun:
		return "server: would run " + cmd, nil
	}
	if _, err := exec.LookPath(cli); err != nil {
		return "server: " + cli + " CLI not on PATH; run " + cmd, nil
	}
	if b, err := exec.Command(cli, args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s: %v: %s", cmd, err, bytes.TrimSpace(b))
	}
	return "server: registered via " + cmd, nil
}

// piExtension is a pi tool_result extension that surfaces files changed by pi's
// edit and write tools by running `<exe> open <abs path>`.
func piExtension(exe string) []byte {
	lit, _ := json.Marshal(exe)
	return []byte(`// Generated by ` + "`nvim-mcp setup`" + `; rewritten on each run.
import { resolve } from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { isEditToolResult, isWriteToolResult } from "@earendil-works/pi-coding-agent";

const NVIM_MCP = ` + string(lit) + `;

export default function (pi: ExtensionAPI) {
  pi.on("tool_result", async (event, ctx) => {
    if (event.isError || !(isEditToolResult(event) || isWriteToolResult(event))) return;
    const path = event.input.path;
    if (typeof path === "string" && path) await pi.exec(NVIM_MCP, ["open", resolve(ctx.cwd, path)]).catch(() => {});
  });
}
`)
}

func verb(dryRun bool, would, did string) string {
	if dryRun {
		return would
	}
	return did
}
