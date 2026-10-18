package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeDir returns Claude Code's user config directory: $CLAUDE_CONFIG_DIR,
// else ~/.claude.
func ClaudeDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// PiDir returns pi's agent directory: $PI_CODING_AGENT_DIR (with a leading ~
// expanded, as pi does), else ~/.pi/agent.
func PiDir() (string, error) {
	home, err := os.UserHomeDir()
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		if err == nil && (dir == "~" || strings.HasPrefix(dir, "~/")) {
			dir = filepath.Join(home, dir[1:])
		}
		return dir, nil
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "agent"), nil
}
