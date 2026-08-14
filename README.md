# nvim-mcp

A small, fast **Model Context Protocol (MCP) server that lets Claude work inside a live
Neovim session.** It attaches to Neovim's msgpack-RPC socket and maps each MCP tool to a
Lua round-trip, so all the real work (LSP, Treesitter, buffers) runs inside nvim and the
server stays a tiny static Go binary (~9 MB, a few MB RSS).

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

On first launch, if it isn't already in your Claude settings, Claude will offer to install
it (default: yes) and remember your answer. Toggle later by asking — drives
`vim_surface_hook` (`on`|`off`|`status`); `on` prints the exact command to wire in.

The hook lives in Claude's `settings.json`, so the server only *offers*; Claude writes it
(takes effect next session). The entry looks like:

```json
"hooks": {
  "PostToolUse": [
    { "matcher": "Edit|Write|MultiEdit",
      "hooks": [ { "type": "command", "command": "/path/to/nvim-mcp hook" } ] }
  ]
}
```

## Configuration

| Env var | Default | Meaning |
|---|---|---|
| `NVIM_SOCKET_PATH` | auto-discovered | override the nvim RPC socket/address to connect to |
| `ALLOW_SHELL_COMMANDS` | `false` | allow `vim_command` to run `!shell` commands |

## Edit-change highlighting

When enabled, lines `vim_edit` changes are highlighted (`DiffAdd`-linked `NvimMcpEdit`) in
your live buffer; highlights accumulate and clear when you **save** (save = accept).

Off until you decide — no env var. The choice is stored in `~/.config/nvim-mcp/config.json`
and set through chat: Claude asks on its first unconfigured edit, then respects it. Toggle
by asking — drives `vim_edit_highlight` (`on`|`off`|`status`).

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
