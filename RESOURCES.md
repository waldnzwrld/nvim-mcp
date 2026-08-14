# Resources

The external resources this project depends on and builds upon — libraries,
runtime services, protocols, and reference material. For usage and tool
documentation, see the [README](README.md).

## Runtime dependencies

The server is a shim: it holds an msgpack-RPC connection to Neovim and dispatches
Lua. Everything below is what that requires at run time.

| Resource | What it provides | Notes |
|---|---|---|
| **Neovim** (running instance) | The actual editor — LSP, Treesitter, buffers, windows. All real work runs here via `nvim_exec_lua`. | Auto-discovered via `v:servername` under `stdpath('run')`; see `:help rpc-connecting`. Requires a modern Neovim (uses `vim.version()`, not the removed `nvim_get_api_info`). |
| **Neovim msgpack-RPC socket** | The transport between server and editor. | Resolution order: `--socket` → `NVIM_SOCKET_PATH` → `NVIM`/`NVIM_LISTEN_ADDRESS` → newest live default socket on disk. |
| **stdio** | JSON-RPC transport between the MCP client (Claude Code) and this server. | stdout is the protocol channel; stderr is used for logs. |

## Go module dependencies

Declared in [`go.mod`](go.mod) (Go 1.26.4).

### Direct

| Module | Version | Role |
|---|---|---|
| [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) | v1.7.0 | Official MCP Go SDK — server, tools, resources, prompts, stdio transport, subscriptions. |
| [`github.com/neovim/go-client`](https://github.com/neovim/go-client) | v1.2.1 | First-party Neovim msgpack-RPC client; connects to the socket and drives `nvim_exec_lua`. |

### Transitive

Pulled in by the two direct dependencies above:

| Module | Version | Pulled in by |
|---|---|---|
| `github.com/google/jsonschema-go` | v0.4.3 | MCP SDK — tool input/output schemas. |
| `github.com/segmentio/asm` | v1.1.3 | encoding (SIMD helpers). |
| `github.com/segmentio/encoding` | v0.5.4 | fast JSON encoding. |
| `github.com/yosida95/uritemplate/v3` | v3.0.2 | MCP SDK — URI templates. |
| `golang.org/x/oauth2` | v0.35.0 | MCP SDK — auth support. |
| `golang.org/x/sync` | v0.20.0 | concurrency primitives. |
| `golang.org/x/sys` | v0.41.0 | low-level syscalls. |
| `golang.org/x/time` | v0.15.0 | rate limiting. |

Exact checksums are pinned in [`go.sum`](go.sum).

## Protocols & specifications

| Resource | Where it's used |
|---|---|
| [Model Context Protocol](https://modelcontextprotocol.io) | The wire protocol this server speaks to Claude Code — tools, resources, prompts, and `notifications/resources/updated`. |
| [Neovim RPC API](https://neovim.io/doc/user/api.html) | The API surface every tool ultimately calls (`nvim_exec_lua`, buffer/window/option functions). |
| Neovim Lua API (`vim.*`) | Each tool is a Lua chunk run inside nvim (`vim.api`, `vim.lsp`, `vim.diagnostic`, `vim.treesitter`, `vim.json`). |

## MCP resources exposed

This server also *publishes* MCP resources back to the client (the other sense of
"resources"). Defined in [`internal/tools/resources.go`](internal/tools/resources.go):

| URI | Content | MIME |
|---|---|---|
| `nvim://session` | Current editor state: file, cursor, mode, layout, cwd, LSP. | `text/plain` |
| `nvim://buffers` | Open, listed buffers with name, filetype, modified status. | `application/json` |
| `nvim://diagnostics` | LSP diagnostics across all buffers. | `text/plain` |

These support live subscriptions, pushed on Neovim autocmds (`DiagnosticChanged`,
buffer add/delete/write).

## Tooling & environment

| Resource | Role |
|---|---|
| **Go toolchain** (`go build`, `go vet`) | Builds the single static binary; `go install` for distribution. |
| **Claude Code** | Reference MCP client; registered via `claude mcp add` or [`.mcp.json`](.mcp.json). |
| `bash` / `python3` / `realpath` | Used by the optional [`hooks/surface-in-nvim.sh`](hooks/surface-in-nvim.sh) PostToolUse hook to add edited files into the live session. |

## Prior art & references

| Resource | Relevance |
|---|---|
| [`neovim/go-client`](https://github.com/neovim/go-client) docs | First-party Go RPC client reference. |
| Author's Neovim skill scripts | The `vim_lsp_*`, `vim_treesitter_context`, `vim_quickfix`, and `vim_inject_todo` tools are Go/Lua ports of these. |
