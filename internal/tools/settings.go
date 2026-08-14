package tools

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/config"
	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

// pkgCfg and bannerShown back the one-shot first-run setup banner (see
// firstRunBanner). They are set once when the tool surface is registered.
var (
	pkgCfg      *config.Store
	bannerShown atomic.Bool
)

type editHighlightIn struct {
	Action string `json:"action" jsonschema:"one of: on, off, status. on/off set and persist whether lines changed by vim_edit are highlighted in the live buffer; status reports the current setting (including 'unset' before the user has chosen)"`
}

type surfaceHookIn struct {
	Action string `json:"action" jsonschema:"one of: on, off, status. Records whether files Claude edits should be auto-opened in Neovim via the surface-in-nvim PostToolUse hook. This only persists the user's choice — installing/removing the hook itself is a settings.json edit the agent performs (e.g. via the update-config skill). status also reports whether the hook is actually present in Claude's settings."`
}

// firstRunBanner returns a one-time, human-directed note listing setup choices
// that have not been made yet, or "" when there is nothing to prompt. It fires
// at most once per server process (i.e. once per session), appended to the
// first tool result — the deterministic "on launch" surface for setup prompts.
func firstRunBanner() string {
	if pkgCfg == nil || bannerShown.Load() {
		return ""
	}
	var items []string
	if _, decided := pkgCfg.HighlightEdits(); !decided {
		items = append(items, "- Edit-change highlighting is unconfigured — ask whether to highlight lines you edit, then set vim_edit_highlight.")
	}
	if _, decided := pkgCfg.SurfaceHook(); !decided && !config.SurfaceHookInstalled() {
		items = append(items, "- Auto-open of edited files is not set up — ask the user: \"Would you like me to automatically open new and edited files in Neovim?\" (default: yes). On yes, install the surface-in-nvim PostToolUse hook via the update-config skill and set vim_surface_hook on; on no, set vim_surface_hook off.")
	}
	if len(items) == 0 {
		return ""
	}
	if bannerShown.Swap(true) {
		return ""
	}
	return "\n\n[nvim-mcp first-run setup] Please raise with the user:\n" + strings.Join(items, "\n")
}

// luaClearEditHighlights removes all edit highlights from every loaded buffer,
// used when the user turns the feature off so stale highlights don't linger.
const luaClearEditHighlights = `
local ns = vim.api.nvim_create_namespace('nvim_mcp_edit')
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) then
    pcall(vim.api.nvim_buf_clear_namespace, b, ns, 0, -1)
  end
end
return 'ok'
`

// registerSettings exposes runtime-toggleable preferences. The edit-highlight
// setting is deliberately driven through a tool (not an env var) so the user
// can flip it mid-session with natural language.
func registerSettings(s *mcp.Server, c *nvimc.Client, cfg *config.Store) {
	pkgCfg = cfg // enable the first-run setup banner (see firstRunBanner)

	addText(s, "vim_edit_highlight",
		"Toggle or query edit-change highlighting: whether lines changed by vim_edit are highlighted in the live Neovim buffer (the highlight clears when that buffer is saved). action: on | off | status. The choice is persisted across sessions.",
		func(_ context.Context, in editHighlightIn) (string, error) {
			switch in.Action {
			case "on":
				if err := cfg.SetHighlightEdits(true); err != nil {
					return "", err
				}
				return "edit highlighting: on", nil
			case "off":
				if err := cfg.SetHighlightEdits(false); err != nil {
					return "", err
				}
				_ = c.Lua(luaClearEditHighlights, nil) // best-effort: wipe existing highlights
				return "edit highlighting: off", nil
			case "status", "":
				enabled, decided := cfg.HighlightEdits()
				switch {
				case !decided:
					return "edit highlighting: unset (not yet configured)", nil
				case enabled:
					return "edit highlighting: on", nil
				default:
					return "edit highlighting: off", nil
				}
			default:
				return "", fmt.Errorf("action must be on, off, or status")
			}
		})

	addText(s, "vim_surface_hook",
		"Record whether files Claude edits (via its Edit/Write tools) should be auto-opened into this Neovim session by the surface-in-nvim PostToolUse hook. action: on | off | status. This persists the user's choice only; the hook itself lives in Claude's settings.json and is installed/removed by the agent (e.g. via the update-config skill). status reports both the recorded choice and whether the hook is currently present in settings.",
		func(_ context.Context, in surfaceHookIn) (string, error) {
			switch in.Action {
			case "on":
				if err := cfg.SetSurfaceHook(true); err != nil {
					return "", err
				}
				return "surface hook: on (ensure the PostToolUse hook is present in settings.json; it takes effect next Claude session)", nil
			case "off":
				if err := cfg.SetSurfaceHook(false); err != nil {
					return "", err
				}
				return "surface hook: off (recorded; remove the PostToolUse hook from settings.json to disable it)", nil
			case "status", "":
				installed := config.SurfaceHookInstalled()
				enabled, decided := cfg.SurfaceHook()
				choice := "unset (not yet configured)"
				if decided {
					if enabled {
						choice = "on"
					} else {
						choice = "off"
					}
				}
				return fmt.Sprintf("surface hook: choice=%s, installed-in-settings=%t", choice, installed), nil
			default:
				return "", fmt.Errorf("action must be on, off, or status")
			}
		})
}
