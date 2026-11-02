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
	Tools    []string // the server's tool names, unprefixed
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
		{ID: "omp", Name: "omp (oh-my-pi)", Detected: detect("omp", config.OmpDir), install: installOmp},
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
	allow := make([]string, len(o.Tools))
	for i, name := range o.Tools {
		allow[i] = "mcp__" + ServerName + "__" + name
	}
	added, err := MergeAllow(path, allow, o.DryRun)
	if err != nil {
		return out, err
	}
	switch {
	case len(added) == 0:
		out = append(out, fmt.Sprintf("tools: all %d already allowed in %s", len(allow), path))
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

	line, err := register("pi", mcpRegistered(filepath.Join(dir, "mcp.json")),
		[]string{"mcp", "add", ServerName, "--", o.Exe}, o.DryRun)
	out = append(out, line)
	if err != nil {
		return out, err
	}

	// pi runs tools without per-call approval, so there is no allowlist.
	if !o.AutoOpen {
		return append(out, "auto-open: off (vim_surface_hook), skipped"), nil
	}
	line, err = writeExtension(filepath.Join(dir, "extensions", "nvim-mcp.ts"), piExtension(o.Exe), o.DryRun)
	return append(out, line), err
}

func installOmp(o Options) ([]string, error) {
	var out []string
	dir, err := config.OmpDir()
	if err != nil {
		return out, err
	}

	line, err := registerOmp(filepath.Join(dir, "mcp.json"), o.Exe, o.DryRun)
	out = append(out, line)
	if err != nil {
		return out, err
	}

	line, err = allowOmp(o.Tools, o.DryRun)
	out = append(out, line)
	if err != nil {
		return out, err
	}

	if !o.AutoOpen {
		return append(out, "auto-open: off (vim_surface_hook), skipped"), nil
	}
	line, err = writeExtension(filepath.Join(dir, "extensions", "nvim-mcp.ts"), ompExtension(o.Exe), o.DryRun)
	return append(out, line), err
}

// mcpRegistered reports whether the mcp.json at path already has ServerName
// under mcpServers (the shape pi and omp share).
func mcpRegistered(path string) bool {
	b, err := os.ReadFile(path)
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

// ompSchema is the $schema omp writes into the mcp.json files it manages.
const ompSchema = "https://raw.githubusercontent.com/can1357/oh-my-pi/main/packages/coding-agent/src/config/mcp-schema.json"

// ompServer is the stdio server entry; a struct keeps omp's key order.
type ompServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// registerOmp adds ServerName to the mcpServers of omp's user mcp.json at path.
// omp has no CLI for this, so the file is edited directly: only the new entry is
// inserted, and a missing file is created the way omp writes it.
func registerOmp(path, exe string, dryRun bool) (string, error) {
	if mcpRegistered(path) {
		return "server: already registered in " + path, nil
	}
	if dryRun {
		return "server: would register in " + path, nil
	}
	entry := ompServer{Type: "stdio", Command: exe, Args: []string{}}
	b, err := os.ReadFile(path)
	switch {
	case err != nil && !os.IsNotExist(err):
		return "", err
	case len(bytes.TrimSpace(b)) == 0:
		b, _ = json.MarshalIndent(map[string]any{
			"$schema":    ompSchema,
			"mcpServers": map[string]any{ServerName: entry},
		}, "", "\t")
		b = append(b, '\n')
	default:
		if b, err = prependKey(b, []string{"mcpServers"}, ServerName, entry); err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
	}
	if err := atomicWrite(path, b); err != nil {
		return "", err
	}
	return "server: registered in " + path, nil
}

// allowOmp sets tools.approval to allow for every tool omp would otherwise
// prompt for under an asking approval mode, through omp's own config CLI so the
// active profile and config.yml layout are handled by omp. Entries the user
// already set (allow, prompt or deny) are kept.
func allowOmp(tools []string, dryRun bool) (string, error) {
	const key = "tools.approval"
	if _, err := exec.LookPath("omp"); err != nil {
		return "tools: omp CLI not on PATH; allow mcp__" + ServerName + "_<tool> under " + key + " in config.yml", nil
	}
	b, err := exec.Command("omp", "config", "get", key, "--json").Output()
	if err != nil {
		return "", fmt.Errorf("omp config get %s: %v", key, err)
	}
	var cur struct {
		Value map[string]string `json:"value"`
	}
	if err := json.Unmarshal(b, &cur); err != nil {
		return "", fmt.Errorf("omp config get %s: %v", key, err)
	}
	merged := map[string]string{}
	for k, v := range cur.Value {
		merged[k] = v
	}
	added := 0
	for _, name := range tools {
		// omp registers MCP tools as mcp__<server>_<tool>.
		k := "mcp__" + ServerName + "_" + name
		if _, ok := merged[k]; !ok {
			merged[k] = "allow"
			added++
		}
	}
	switch {
	case added == 0:
		return fmt.Sprintf("tools: all %d already set in %s", len(tools), key), nil
	case dryRun:
		return fmt.Sprintf("tools: would allow %d in %s", added, key), nil
	}
	val, _ := json.Marshal(merged)
	if b, err := exec.Command("omp", "config", "set", key, string(val)).CombinedOutput(); err != nil {
		return "", fmt.Errorf("omp config set %s: %v: %s", key, err, bytes.TrimSpace(b))
	}
	return fmt.Sprintf("tools: allowed %d in %s", added, key), nil
}

// writeExtension writes a generated auto-open extension to path unless it
// already has exactly that content.
func writeExtension(path string, want []byte, dryRun bool) (string, error) {
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
		return "auto-open: extension already installed at " + path, nil
	}
	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("auto-open: %s extension %s", verb(dryRun, "would write", "wrote"), path), nil
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

// ompExtension is an omp tool_result extension that surfaces files changed by
// omp's file-mutating tools by running `<exe> open <abs paths>`. omp reports
// changed paths in each tool's result details: write (resolvedPath), edit (path,
// or perFileResults for multi-file edits), ast_edit once applied, and the
// resolve tool that applies a previewed change. Tools mounted as xd:// devices
// arrive as a write whose details.xdev carries the device tool and its details.
func ompExtension(exe string) []byte {
	lit, _ := json.Marshal(exe)
	return []byte(`// Generated by ` + "`nvim-mcp setup`" + `; rewritten on each run.
import { isAbsolute, resolve } from "node:path";
import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";

const NVIM_MCP = ` + string(lit) + `;

type Details = Record<string, any> | undefined;

function changedFiles(tool: string, input: Record<string, any> | undefined, d: Details): string[] {
  if (!d) return [];
  switch (tool) {
    case "write":
      if (d.xdev) return changedFiles(d.xdev.tool, d.xdev.args, d.xdev.inner);
      return [d.resolvedPath ?? input?.path];
    case "edit":
      return Array.isArray(d.perFileResults) ? d.perFileResults.map((r: any) => r?.path) : [d.path];
    case "ast_edit":
      return d.applied && Array.isArray(d.files) ? d.files : [];
    case "resolve":
      return d.action === "apply" ? changedFiles(d.sourceToolName, undefined, d.sourceResultDetails) : [];
  }
  return [];
}

export default function (pi: ExtensionAPI) {
  pi.on("tool_result", async (event: any, ctx: any) => {
    if (event.isError) return;
    const files = changedFiles(event.toolName, event.input, event.details)
      .filter((f): f is string => typeof f === "string" && f !== "" && !f.includes("://"))
      .map(f => (isAbsolute(f) ? f : resolve(ctx.cwd, f)));
    if (files.length) await pi.exec(NVIM_MCP, ["open", ...files]).catch(() => {});
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
