# nvim-mcp

A small, fast **Model Context Protocol (MCP) server that lets Claude work inside a live
Neovim session.** It attaches to Neovim's msgpack-RPC socket and maps each MCP tool to a
Lua round-trip, so all the real work (LSP, Treesitter, buffers) runs inside nvim and the
server stays a tiny static Go binary (~9 MB, a few MB RSS).

> **You can ask Claude to enable optional MCP tools for nvim-mcp** — turning on edit-change
> highlighting and enhanced buffer opening (auto-opening the files Claude edits into your
> live session). Both are off until you ask; see
> [Edit-change highlighting](#edit-change-highlighting) and [auto-open hook](#auto-open-hook).

## Why Go / this server

- **Single static binary**, zero runtime deps, cross-platform, macOS-native.
- **No companion Neovim plugin required** — it auto-discovers the socket.
- **Token-compact responses by design** (e.g. `file:line:col [ERROR] msg`, not raw LSP JSON).
- **Broad surface**: 28 `vim_*` tools covering buffers, windows, search, LSP
  diagnostics/hover/symbols, Treesitter context, quickfix, todo injection, and a
  jump-to-finding tool — built on the first-party
  [`neovim/go-client`](https://github.com/neovim/go-client).
- **Edit-change highlighting** (opt-in): lines Claude changes are highlighted in
  your live buffer until you save, so you can see its work at a glance.

## Install

```sh
go install github.com/waldnzwrld/nvim-mcp@latest   # -> $(go env GOPATH)/bin/nvim-mcp
# or from a clone:
go build -o nvim-mcp ./
```

## Connect Neovim

Just run Neovim normally — it creates an RPC socket automatically at startup
(`v:servername`, under `stdpath('run')`; see `:help rpc-connecting`). The server
discovers that socket on its own; there is nothing to configure.

Resolution order: `--socket` flag → `NVIM_SOCKET_PATH` → `NVIM` /
`NVIM_LISTEN_ADDRESS` → the newest live default Neovim socket on disk. An override
is only needed for a non-default target (e.g. a TCP `host:port`, or a specific
instance among several).

```sh
nvim-mcp --print-address    # show which running Neovim it discovered
```

## Setup

One command installs nvim-mcp into the harnesses you use:

```sh
nvim-mcp setup                       # detect harnesses, pick which to install into
nvim-mcp setup --harness claude,pi   # skip the prompt
nvim-mcp setup --dry-run             # print what would change, write nothing
```

Supported harnesses are Claude Code (`claude`) and [pi](https://pi.dev) (`pi`). A harness is
detected when its CLI is on `PATH` or its config directory exists. On a terminal, `setup`
lists them and asks which to install into (Enter = every detected one); without a terminal
it installs into every detected harness.

| Step | Claude Code | pi |
|---|---|---|
| Register the server as `nvim` | `claude mcp add --scope user nvim -- <abs path>` | `pi mcp add nvim -- <abs path>` |
| Allow the tools | adds every `mcp__nvim__*` tool to `permissions.allow` in `settings.json` | nothing to do: pi does not ask per tool call |
| Auto-open edited files | adds the `nvim-mcp hook` `PostToolUse` hook to `settings.json` | writes `extensions/nvim-mcp.ts` |

- Config locations: `$CLAUDE_CONFIG_DIR`, else `~/.claude`; `$PI_CODING_AGENT_DIR`, else
  `~/.pi/agent`.
- Idempotent: a step that is already done is skipped, so re-running is safe. Registration is
  skipped when a server named `nvim` already exists.
- Settings edits insert only the new entries; the rest of the file is left unchanged. Writes
  are atomic (temp file + rename).
- The registered path, hook and extension all use the absolute path of the binary that ran
  `setup`, so `PATH` does not matter.
- If a harness's CLI is not on `PATH`, `setup` prints the registration command to run.
- Auto-open is skipped when you turned it off with `vim_surface_hook off`; otherwise `setup`
  records it as on.
- Changes take effect in each harness's next session.

Allowing the tools grants the full surface, including `vim_command` and `vim_exec_lua`,
which can run shell/Lua. To approve tools per call instead, register by hand
(`claude mcp add --scope user nvim -- "$(go env GOPATH)/bin/nvim-mcp"`) and skip `setup`.

Manage the Claude registration with `claude mcp list`, `claude mcp get nvim`,
`claude mcp remove nvim`, or `/mcp` inside a chat; pi's with `pi mcp list` / `/mcp`.

A project-local [`.mcp.json`](.mcp.json) is available in this repo if you would like to confine this mcp to specific projects.

### auto-open hook

`nvim-mcp hook` is a `PostToolUse` hook — the binary invoking itself — that opens every
file Claude edits into the live session (anywhere on disk, not just the current project).
It skips temporary/scratch paths — the OS temp dirs (Claude's scratchpad) and the
machine-managed session subtrees under `~/.claude` (`plans/`, `projects/`, `sessions/`,
`shell-snapshots/`, …). Your own `~/.claude` config and content (`settings.json`,
`CLAUDE.md`, `agents/`, `skills/`, `memory/`, `hooks/`, …) is surfaced and reloaded
normally. 

`nvim-mcp setup` installs the hook. The server cannot edit `settings.json` itself, so
when it is missing and no choice is recorded, the server also asks Claude, through its MCP
instructions and a note on the first tool result, to offer it in chat. That prompt is
*best-effort*. Toggle later with `vim_surface_hook` (`on`|`off`|`status`). Your choice is
remembered in `~/.config/nvim-mcp/config.json`, independent of whether the hook is
currently installed.

The entry looks like:

```json
"hooks": {
  "PostToolUse": [
    { "matcher": "Edit|Write|MultiEdit",
      "hooks": [ { "type": "command", "command": "/path/to/nvim-mcp hook" } ] }
  ]
}
```

### Other clients

The hook setup prompts and auto-open instructions are only sent to Claude Code
(detected from the MCP `clientInfo.name`). Other clients can surface files with
`nvim-mcp open <path>...`, which applies the same rules as the hook (real files
only, temp/session paths skipped, relative paths resolved against the cwd) and
always exits 0.

For pi, `setup` writes a `tool_result` extension that runs `open` on every file pi's
edit and write tools change. `open` finds Neovim from pi's own environment:
`NVIM_SOCKET_PATH`, then `$NVIM` when pi runs inside a Neovim terminal, then Neovim's
default socket. A socket set only in the MCP server's `env` is not seen there.

## Configuration

| Env var | Default | Meaning |
|---|---|---|
| `NVIM_SOCKET_PATH` | auto-discovered | override the nvim RPC socket/address to connect to |
| `ALLOW_SHELL_COMMANDS` | `false` | allow `vim_command` to run `!shell` commands |

## Edit-change highlighting

When enabled, lines `vim_edit` changes are highlighted (`DiffAdd`-linked `NvimMcpEdit`) in
your live buffer; highlights accumulate and clear when you **save** (save = accept).

Off until you turn it on — there is no env var. The setting is stored in
`~/.config/nvim-mcp/config.json` and driven from chat: **ask Claude to highlight the lines
it edits** and it flips it on (`vim_edit_highlight` — `on`|`off`|`status`). The server also
nudges Claude — via its MCP instructions and a note appended to its first unconfigured edit —
to raise this with you once, but that prompt is *best-effort*: the model may not act on it,
so if it never comes up, just ask directly. The choice persists across sessions.

## Tools

29 tools, compact text output. Editor/buffer/window/search tools plus ports of the author's
Neovim skills (LSP, Treesitter, quickfix, todo) and the ability to execute lua commands.

**Core** — `vim_buffer`, `vim_command`, `vim_status`, `vim_edit`
**Buffers** — `vim_buffer_switch`, `vim_buffer_save`, `vim_file_open`
**Windows/positions** — `vim_window`, `vim_mark`, `vim_register`, `vim_visual`, `vim_jump`
**Search** — `vim_search`, `vim_search_replace`, `vim_grep`
**Workflow** — `vim_macro`, `vim_tab`, `vim_fold`, `vim_health`
**Settings** — `vim_edit_highlight` (toggle/query edit-change highlighting), `vim_surface_hook` (toggle/query auto-open of edited files)
**Additions** — `vim_open_at` (open + jump to line:col), `vim_quickfix`, `vim_lsp_diagnostics`,
`vim_lsp_hover`, `vim_lsp_symbols`, `vim_treesitter_context`, `vim_inject_todo`, `vim_exec_lua`

## Resources & prompt

- `nvim://session` — current editor state (text)
- `nvim://buffers` — open buffers with metadata (JSON)
- `nvim://diagnostics` — LSP diagnostics across buffers (text)

Resources support **live subscriptions**: `resources/subscribe` starts pushing
`notifications/resources/updated` driven by nvim autocmds (`DiagnosticChanged`, buffer
add/delete/write).

Prompt: `neovim_workflow` — guidance for driving nvim through these tools.

## Development

```sh
go build ./... && go vet ./...
```

The nvim-side logic lives as Lua string constants next to each tool (ported from the
original skill scripts). Adding a tool = a `In` struct + a Lua chunk that `return`s a string.

## License

[MIT](LICENSE) — free to use, modify, and redistribute.
