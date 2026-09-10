// Package setup implements the `nvim-mcp setup` subcommand: it merges the
// server's tool names into permissions.allow of Claude Code's settings.json so
// the tools run without a per-call approval prompt. The merge is idempotent and
// edits only the allow array, leaving the rest of the file byte-for-byte intact.
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SettingsPath returns Claude Code's settings.json path, honoring
// CLAUDE_CONFIG_DIR and falling back to ~/.claude.
func SettingsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// MergeAllow ensures every entry in want is present in permissions.allow of the
// settings file at path. Missing entries are appended as one contiguous block
// at the head of the array; entries already present are left untouched. It
// returns the entries it added (or would add, when dryRun is set). When the
// file, its permissions object, or the allow array is absent, it falls back to
// a structural write that creates them.
func MergeAllow(path string, want []string, dryRun bool) (added []string, err error) {
	b, readErr := os.ReadFile(path)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return nil, readErr
		}
		b = nil
	}

	have := map[string]bool{}
	if len(b) > 0 {
		var doc struct {
			Permissions struct {
				Allow []string `json:"allow"`
			} `json:"permissions"`
		}
		if json.Unmarshal(b, &doc) != nil {
			return nil, fmt.Errorf("%s is not valid JSON", path)
		}
		for _, e := range doc.Permissions.Allow {
			have[e] = true
		}
	}

	for _, e := range want {
		if !have[e] {
			added = append(added, e)
			have[e] = true // dedupe within want
		}
	}
	if len(added) == 0 || dryRun {
		return added, nil
	}

	openOff, closePos, ok := allowArrayBounds(b)
	if !ok {
		// No usable allow array — rebuild structurally (order not preserved,
		// acceptable on a first-time / hand-absent file).
		return added, structuralWrite(path, b, want)
	}

	inner := b[openOff:closePos]
	empty := len(bytes.TrimSpace(inner)) == 0

	var out []byte
	if empty {
		indent, closeIndent := emptyIndents(b, openOff, closePos)
		var block bytes.Buffer
		for i, e := range added {
			block.WriteString("\n")
			block.WriteString(indent)
			block.Write(quote(e))
			if i < len(added)-1 {
				block.WriteByte(',')
			}
		}
		block.WriteString("\n")
		block.WriteString(closeIndent)
		out = concat(b[:openOff], block.Bytes(), b[closePos:])
	} else {
		indent := elementIndent(inner)
		var block bytes.Buffer
		for _, e := range added {
			block.WriteString("\n")
			block.WriteString(indent)
			block.Write(quote(e))
			block.WriteByte(',')
		}
		// Prepend the block right after '['; the existing first element (which
		// already carries its own newline+indent) follows, so commas stay valid.
		out = concat(b[:openOff], block.Bytes(), b[openOff:])
	}

	return added, atomicWrite(path, out)
}

// allowArrayBounds scans the JSON and returns the byte offset just past the
// '[' that opens permissions.allow and the offset of its matching ']'. It keys
// off the object key "allow" followed by an array; a string *value* "allow" is
// never followed by '[' in valid JSON, so the simple key tracking is safe.
func allowArrayBounds(b []byte) (openOff, closePos int, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	var lastKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, false
		}
		switch t := tok.(type) {
		case string:
			lastKey = t
		case json.Delim:
			if t == '[' && lastKey == "allow" {
				open := int(dec.InputOffset())
				depth := 1
				for depth > 0 {
					tk, err := dec.Token()
					if err != nil {
						return 0, 0, false
					}
					if d, ok := tk.(json.Delim); ok {
						switch d {
						case '[', '{':
							depth++
						case ']', '}':
							depth--
						}
					}
				}
				return open, int(dec.InputOffset()) - 1, true
			}
			lastKey = ""
		}
	}
}

// elementIndent reads the leading whitespace of the first array element from
// the bytes between '[' and ']', defaulting to six spaces.
func elementIndent(inner []byte) string {
	if i := bytes.IndexByte(inner, '\n'); i >= 0 {
		j := i + 1
		k := j
		for k < len(inner) && (inner[k] == ' ' || inner[k] == '\t') {
			k++
		}
		return string(inner[j:k])
	}
	return "      "
}

// emptyIndents derives element and closing-bracket indentation for an empty
// array from the line the ']' sits on.
func emptyIndents(b []byte, openOff, closePos int) (indent, closeIndent string) {
	closeIndent = "    "
	if ls := bytes.LastIndexByte(b[:closePos], '\n'); ls >= 0 {
		seg := b[ls+1 : closePos]
		if len(bytes.TrimLeft(seg, " \t")) == 0 {
			closeIndent = string(seg)
		}
	}
	return closeIndent + "  ", closeIndent
}

func quote(s string) []byte {
	q, _ := json.Marshal(s)
	return q
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// structuralWrite unmarshals the whole document, injects the allow entries, and
// re-marshals. Used only when no allow array can be located; object-key order
// is not preserved by encoding/json.
func structuralWrite(path string, b []byte, want []string) error {
	doc := map[string]any{}
	if len(b) > 0 {
		if json.Unmarshal(b, &doc) != nil {
			return fmt.Errorf("%s is not valid JSON", path)
		}
	}
	perms, _ := doc["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
		doc["permissions"] = perms
	}
	var allow []any
	for _, e := range want {
		allow = append(allow, e)
	}
	perms["allow"] = allow
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(out, '\n'))
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
