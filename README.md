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

## Register with Claude Code

One command — no hand-editing `~/.claude.json`:

```sh
claude mcp add --scope user nvim -- "$(go env GOPATH)/bin/nvim-mcp"
```

`--scope user` registers it for all your projects; drop it to add just the current project
(the default, `local`). If `$(go env GOPATH)/bin` is on your `PATH`, `nvim-mcp` alone works:

```sh
claude mcp add --scope user nvim -- nvim-mcp
```

Manage it with `claude mcp list`, `claude mcp get nvim`, `claude mcp remove nvim`, or `/mcp`
inside a chat.

A project-local [`.mcp.json`](.mcp.json) is available in this repo if you would like to confine this mcp to specific projects.

### auto-open hook

`nvim-mcp hook` is a `PostToolUse` hook — the binary invoking itself — that opens every
file Claude edits into the live session (anywhere on disk, not just the current project).
It skips temporary/scratch paths — the OS temp dirs (Claude's scratchpad) and the
machine-managed session subtrees under `~/.claude` (`plans/`, `projects/`, `sessions/`,
`shell-snapshots/`, …). Your own `~/.claude` config and content (`settings.json`,
`CLAUDE.md`, `agents/`, `skills/`, `memory/`, `hooks/`, …) is surfaced and reloaded
normally. 

The hook lives in Claude's `settings.json`, which the server cannot edit — so setup is
driven from chat: **ask Claude to auto-open your edits in Neovim** and it wires the entry in
for you (`vim_surface_hook on` prints the exact command; Claude adds it to `settings.json`,
which takes effect next session). The server also nudges Claude — through its MCP
instructions and a note on the first tool result — to offer this on its own, but that prompt
is *best-effort*: the model may not raise it, so if it doesn't come up, just ask. Toggle
later the same way — `vim_surface_hook` (`on`|`off`|`status`). Your choice is remembered in
`~/.config/nvim-mcp/config.json`, independent of whether the hook is currently installed.

The entry looks like:

```json
"hooks": {
  "PostToolUse": [
    { "matcher": "Edit|Write|MultiEdit",
      "hooks": [ { "type": "command", "command": "/path/to/nvim-mcp hook" } ] }
  ]
}
```

## Skip permission prompts

`nvim-mcp setup` adds every `vim_*` tool to `permissions.allow` in Claude Code's
`settings.json`, so the tools run without a per-call approval prompt.

```sh
nvim-mcp setup            # merge the allow entries
nvim-mcp setup --dry-run  # print what would be added, write nothing
```

- Entries are derived from the server's registered tools, so the list stays in
  sync as tools change.
- Idempotent: only missing entries are appended, as one contiguous block; a
  re-run is a no-op.
- Edits only the `allow` array; the rest of the file is left unchanged. Writes
  are atomic (temp file + rename).
- Target path is `$CLAUDE_CONFIG_DIR/settings.json`, else `~/.claude/settings.json`.
- Takes effect in the next session (Claude reads permissions at startup).

This grants the full surface, including `vim_command` and `vim_exec_lua`, which
can run shell/Lua. Omit `setup` and approve tools per call if you don't want that.

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
