#!/bin/bash
#
# Claude Code PostToolUse hook: surface files Claude edits *inside the current
# project* into the live Neovim session, so the human sees the agent's work in
# their editor.
#
# Scoping: only files that resolve to somewhere under the session's project
# directory (the hook payload's `cwd`) are surfaced. That deliberately excludes
# transient edits Claude makes outside the project — the scratchpad
# (/private/tmp/claude-*/...), plan files (~/.claude/plans/...), memory, etc. —
# so your buffer list stays limited to what this session is actually working on.
#
# Wire it in ~/.claude/settings.json:
#
#   "hooks": {
#     "PostToolUse": [
#       {
#         "matcher": "Edit|Write|MultiEdit",
#         "hooks": [
#           { "type": "command",
#             "command": "/Users/waldnzwrld/Code/nvim-mcp/hooks/surface-in-nvim.sh" }
#         ]
#       }
#     ]
#   }
#
# It is intentionally silent and always exit 0 so it never blocks a tool call.

# Resolve the Neovim socket: an explicit override, the inherited $NVIM, or the
# newest live socket Neovim created automatically under stdpath('run').
resolve_socket() {
  if [[ -n "${NVIM_SOCKET_PATH:-}" ]]; then echo "$NVIM_SOCKET_PATH"; return; fi
  if [[ -n "${NVIM:-}" ]]; then echo "$NVIM"; return; fi
  local base="${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}"
  ls -t "$base"/nvim."${USER}"/*/nvim.* 2>/dev/null | head -1
}

NVIM_SOCKET="$(resolve_socket)"
[[ -n "$NVIM_SOCKET" && -S "$NVIM_SOCKET" ]] || exit 0

# Pull both the edited file and the session cwd from the hook payload (one read).
read -r file cwd < <(python3 -c '
import sys, json
d = json.load(sys.stdin)
print(d.get("tool_input", {}).get("file_path", ""), d.get("cwd", ""))
' 2>/dev/null)
[[ -z "$file" || ! -f "$file" ]] && exit 0

# Fall back to $PWD if the payload carried no cwd; without a project root we
# can't scope, so bail rather than surface everything.
[[ -z "$cwd" ]] && cwd="$PWD"
[[ -z "$cwd" ]] && exit 0

abs="$(realpath "$file")"
root="$(realpath "$cwd")"

# Only surface files that live under the project root. Compare on a trailing
# slash so a sibling dir sharing a prefix (…/nvim-mcp-other) can't match.
case "$abs/" in
  "$root"/*) ;;   # inside the project — surface it
  *) exit 0 ;;    # outside (scratchpad, plans, memory, …) — skip
esac

nvim --server "$NVIM_SOCKET" --remote-expr \
  "execute('badd $abs | set autoread | checktime')" >/dev/null 2>&1

exit 0
