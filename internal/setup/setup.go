// Package setup implements the `nvim-mcp setup` subcommand: it installs the
// server into each selected harness (Claude Code, pi) — registering it, allowing
// its tools where the harness asks per call, and wiring auto-open of edited
// files. Every step is idempotent, and settings edits insert only the new bytes,
// leaving the rest of the file intact.
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/waldnzwrld/nvim-mcp/internal/config"
)

// SettingsPath returns Claude Code's settings.json path under config.ClaudeDir.
func SettingsPath() (string, error) {
	dir, err := config.ClaudeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// MergeAllow ensures every entry in want is present in permissions.allow of the
// settings file at path. Missing entries are inserted as one contiguous block at
// the head of the array; entries already present are left untouched. It returns
// the entries it added (or would add, when dryRun is set).
func MergeAllow(path string, want []string, dryRun bool) (added []string, err error) {
	b, err := readSettings(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil, fmt.Errorf("%s: permissions.allow is not a string array", path)
	}
	have := map[string]bool{}
	for _, e := range doc.Permissions.Allow {
		have[e] = true
	}
	var elems []any
	for _, e := range want {
		if !have[e] {
			added = append(added, e)
			elems = append(elems, e)
			have[e] = true // dedupe within want
		}
	}
	if len(added) == 0 || dryRun {
		return added, nil
	}
	out, err := prependArray(b, []string{"permissions", "allow"}, elems)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return added, atomicWrite(path, out)
}

// hookEntry is one PostToolUse matcher group; a struct keeps the key order
// Claude's docs use.
type hookEntry struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// InstallHook adds a PostToolUse hook running command after Edit, Write and
// MultiEdit to the settings file at path, at the head of hooks.PostToolUse.
// The caller checks whether the hook is already installed.
func InstallHook(path, command string, dryRun bool) error {
	b, err := readSettings(path)
	if err != nil || dryRun {
		return err
	}
	entry := hookEntry{Matcher: "Edit|Write|MultiEdit", Hooks: []hookCommand{{Type: "command", Command: command}}}
	out, err := prependArray(b, []string{"hooks", "PostToolUse"}, []any{entry})
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return atomicWrite(path, out)
}

// readSettings returns the settings file's bytes, "{}" when it is missing or
// empty, or an error when it is not a JSON object.
func readSettings(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return []byte("{}\n"), nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return nil, fmt.Errorf("%s is not a JSON object", path)
	}
	return b, nil
}

// prependArray inserts elems at the head of the array reached from the root
// object by the keys in path, creating whichever trailing objects and the array
// are missing. Only the inserted bytes change; the rest of b is preserved.
func prependArray(b []byte, path []string, elems []any) ([]byte, error) {
	depth, err := existingDepth(b, path)
	if err != nil {
		return nil, err
	}
	delim := byte('{')
	if depth == len(path) {
		delim = '['
	}
	open, close, ok := containerBounds(b, path[:depth], delim)
	if !ok {
		return nil, fmt.Errorf("cannot locate %v", path[:depth])
	}
	inner := b[open:close]
	empty := len(bytes.TrimSpace(inner)) == 0
	closeIndent := lineIndent(b, close)
	indent := closeIndent + "  "
	if !empty {
		lead := inner[:len(inner)-len(bytes.TrimLeft(inner, " \t\r\n"))]
		if i := bytes.LastIndexByte(lead, '\n'); i >= 0 {
			indent = string(lead[i+1:])
		}
	}

	var parts [][]byte
	if depth == len(path) {
		for _, e := range elems {
			parts = append(parts, render(e, indent))
		}
	} else {
		var v any = elems
		for i := len(path) - 1; i > depth; i-- {
			v = map[string]any{path[i]: v}
		}
		parts = append(parts, concat(render(path[depth], indent), []byte(": "), render(v, indent)))
	}

	var block bytes.Buffer
	for i, p := range parts {
		block.WriteString("\n" + indent)
		block.Write(p)
		if !empty || i < len(parts)-1 {
			block.WriteByte(',')
		}
	}
	if empty {
		block.WriteString("\n" + closeIndent)
		return concat(b[:open], block.Bytes(), b[close:]), nil
	}
	return concat(b[:open], block.Bytes(), b[open:]), nil
}

// existingDepth reports how many leading keys of path already exist as objects
// (or, for the last key, as the target array), erroring when one exists with a
// different JSON type.
func existingDepth(b []byte, path []string) (int, error) {
	var node any
	if err := json.Unmarshal(b, &node); err != nil {
		return 0, err
	}
	for i, key := range path {
		obj, ok := node.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("%v is not an object", path[:i])
		}
		child, exists := obj[key]
		if !exists {
			return i, nil
		}
		if i == len(path)-1 {
			if _, ok := child.([]any); !ok {
				return 0, fmt.Errorf("%v is not an array", path)
			}
		}
		node = child
	}
	return len(path), nil
}

// containerBounds walks b's tokens and returns the offset just past the delim
// that opens the container at path (object keys from the root) and the offset
// of its matching close.
func containerBounds(b []byte, path []string, delim byte) (open, close int, ok bool) {
	type frame struct {
		obj, wantKey bool
		key          string
	}
	var stack []frame
	valueDone := func() {
		if n := len(stack); n > 0 && stack[n-1].obj {
			stack[n-1].wantKey = true
		}
	}
	atPath := func() bool {
		if len(stack) != len(path) {
			return false
		}
		for i, f := range stack {
			if !f.obj || f.key != path[i] {
				return false
			}
		}
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, false
		}
		switch t := tok.(type) {
		case json.Delim:
			if t == '{' || t == '[' {
				if byte(t) == delim && atPath() {
					open = int(dec.InputOffset())
					for depth := 1; depth > 0; {
						tk, err := dec.Token()
						if err != nil {
							return 0, 0, false
						}
						if d, isDelim := tk.(json.Delim); isDelim {
							if d == '{' || d == '[' {
								depth++
							} else {
								depth--
							}
						}
					}
					return open, int(dec.InputOffset()) - 1, true
				}
				stack = append(stack, frame{obj: t == '{', wantKey: t == '{'})
				continue
			}
			stack = stack[:len(stack)-1]
			valueDone()
		case string:
			if n := len(stack); n > 0 && stack[n-1].obj && stack[n-1].wantKey {
				stack[n-1].key, stack[n-1].wantKey = t, false
				continue
			}
			valueDone()
		default:
			valueDone()
		}
	}
}

// lineIndent returns the whitespace before pos on its line, or "" when other
// text precedes pos there.
func lineIndent(b []byte, pos int) string {
	seg := b[bytes.LastIndexByte(b[:pos], '\n')+1 : pos]
	if len(bytes.TrimLeft(seg, " \t")) == 0 {
		return string(seg)
	}
	return ""
}

// render marshals v with two-space nesting, continuation lines prefixed by indent.
func render(v any, indent string) []byte {
	out, _ := json.MarshalIndent(v, indent, "  ")
	return out
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
