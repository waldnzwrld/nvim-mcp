package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

// luaBuffersJSON ports get-open-buffers: a JSON array of loaded, listed buffers.
const luaBuffersJSON = `
local r = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.fn.buflisted(b) == 1 then
    local n = vim.api.nvim_buf_get_name(b)
    if n ~= '' then
      r[#r+1] = {
        nr = b,
        name = n,
        filetype = vim.api.nvim_get_option_value('filetype', {buf = b}),
        modified = vim.api.nvim_get_option_value('modified', {buf = b}),
      }
    end
  end
end
return vim.json.encode(r)
`

func registerResources(s *mcp.Server, c *nvimc.Client) {
	// nvim://session — current editor state (reuses the status snapshot).
	s.AddResource(
		&mcp.Resource{
			URI:         "nvim://session",
			Name:        "session",
			Description: "Current Neovim session state: file, cursor, mode, layout, cwd, LSP.",
			MIMEType:    "text/plain",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			var out string
			if err := c.Lua(luaStatus, &out); err != nil {
				return nil, err
			}
			return textResource(req.Params.URI, "text/plain", out), nil
		})

	// nvim://buffers — all open buffers with metadata (skill: get-open-buffers).
	s.AddResource(
		&mcp.Resource{
			URI:         "nvim://buffers",
			Name:        "buffers",
			Description: "All open, listed buffers with name, filetype, and modified status (JSON).",
			MIMEType:    "application/json",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			var out string
			if err := c.Lua(luaBuffersJSON, &out); err != nil {
				return nil, err
			}
			return textResource(req.Params.URI, "application/json", out), nil
		})

	// nvim://diagnostics — live LSP diagnostics across all buffers.
	s.AddResource(
		&mcp.Resource{
			URI:         "nvim://diagnostics",
			Name:        "diagnostics",
			Description: "LSP diagnostics across all buffers as compact file:line:col [SEV] message lines.",
			MIMEType:    "text/plain",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			var out string
			if err := c.Lua(luaDiagnostics, &out, -1); err != nil {
				return nil, err
			}
			return textResource(req.Params.URI, "text/plain", out), nil
		})
}

func textResource(uri, mime, text string) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mime, Text: text}},
	}
}

func registerPrompt(s *mcp.Server, _ *nvimc.Client) {
	s.AddPrompt(
		&mcp.Prompt{
			Name:        "neovim_workflow",
			Description: "Guidance for driving Neovim through this MCP server.",
		},
		func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			const guidance = `You are working inside a live Neovim session via the nvim-mcp server.

Reading state:
- vim_status / the nvim://session resource for cursor, mode, file, and LSP.
- vim_buffer to read a buffer with line numbers; nvim://buffers to list buffers.
- vim_lsp_diagnostics (or nvim://diagnostics) — investigate and fix ERROR diagnostics.
- vim_lsp_hover, vim_lsp_symbols, vim_treesitter_context to understand code structure.
- vim_quickfix to pick up the user's active investigation (errors, grep, tests).

Acting:
- vim_edit to change buffer text (insert/replace/replaceAll); vim_buffer_save to persist.
- vim_command for ex-commands; vim_search / vim_search_replace / vim_grep for search.
- vim_open_at to surface a finding by jumping the human's cursor to a line:col.
- vim_inject_todo to leave a FIX/TODO/NOTE marker above a line without changing logic.

Prefer compact, targeted calls. After editing, save and re-check vim_lsp_diagnostics.`
			return &mcp.GetPromptResult{
				Description: "How to drive Neovim through nvim-mcp.",
				Messages: []*mcp.PromptMessage{
					{Role: "user", Content: &mcp.TextContent{Text: guidance}},
				},
			}, nil
		})
}
