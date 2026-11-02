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
	return agentDir(filepath.Join(".pi", "agent"))
}

// OmpDir returns omp's active agent directory. A named profile (OMP_PROFILE
// when defined, else PI_PROFILE; "default" or blank means none) resolves to
// ~/<root>/profiles/<name>/agent; otherwise $PI_CODING_AGENT_DIR, else
// ~/<root>/agent. <root> is $PI_CONFIG_DIR, else .omp.
func OmpDir() (string, error) {
	root := os.Getenv("PI_CONFIG_DIR")
	if root == "" {
		root = ".omp"
	}
	profile, ok := os.LookupEnv("OMP_PROFILE")
	if !ok {
		profile = os.Getenv("PI_PROFILE")
	}
	if profile = strings.TrimSpace(profile); profile != "" && profile != "default" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, root, "profiles", profile, "agent"), nil
	}
	return agentDir(filepath.Join(root, "agent"))
}

// agentDir returns $PI_CODING_AGENT_DIR with a leading ~ expanded, else
// ~/<rel>.
func agentDir(rel string) (string, error) {
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
	return filepath.Join(home, rel), nil
}
