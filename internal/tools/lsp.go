package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

type diagnosticsIn struct {
	Bufnr int `json:"bufnr,omitempty" jsonschema:"limit to a buffer number; 0 or omitted returns all buffers"`
}

type hoverIn struct {
	File string `json:"file" jsonschema:"file path (must be a loaded buffer with an LSP client)"`
	Line int    `json:"line" jsonschema:"1-based line"`
	Col  int    `json:"col" jsonschema:"1-based column"`
}

type symbolsIn struct {
	File string `json:"file" jsonschema:"file path (must be a loaded buffer with an LSP client)"`
}

// luaDiagnostics ports get-lsp-diagnostics: compact file:line:col [SEV] message.
const luaDiagnostics = `
local bufnr = ...
if bufnr ~= nil and bufnr < 0 then bufnr = nil end
local d = vim.diagnostic.get(bufnr)
local sev = {[1] = 'ERROR', [2] = 'WARN', [3] = 'INFO', [4] = 'HINT'}
local out = {}
for _, v in ipairs(d) do
  local n = vim.api.nvim_buf_get_name(v.bufnr)
  if n ~= '' then
    out[#out+1] = string.format('%s:%d:%d [%s] %s', n, v.lnum + 1, v.col + 1,
      sev[v.severity] or '?', (v.message or ''):gsub('\n', ' '))
  end
end
if #out == 0 then return 'no diagnostics' end
return table.concat(out, '\n')
`

// luaHover ports get-lsp-hover.
const luaHover = `
local fname, line, char = ...
line = line - 1
char = char - 1
local bufnr = vim.fn.bufnr(fname)
if bufnr == -1 then return 'error: buffer not loaded (open the file first)' end
local params = {
  textDocument = {uri = vim.uri_from_fname(fname)},
  position = {line = line, character = char},
}
local results = vim.lsp.buf_request_sync(bufnr, 'textDocument/hover', params, 2000)
if not results then return 'no hover info' end
local function extract(contents)
  if type(contents) == 'string' then return contents end
  if contents.value then return contents.value end
  local parts = {}
  for _, item in ipairs(contents) do
    parts[#parts+1] = type(item) == 'string' and item or (item.value or '')
  end
  return table.concat(parts, '\n')
end
for _, res in pairs(results) do
  if res.result and res.result.contents then return extract(res.result.contents) end
end
return 'no hover info'
`

// luaSymbols ports get-lsp-symbols: flattened tree, depth-indented.
const luaSymbols = `
local fname = ...
local bufnr = vim.fn.bufnr(fname)
if bufnr == -1 then return 'error: buffer not loaded (open the file first)' end
local kinds = {
  'File','Module','Namespace','Package','Class','Method','Property','Field',
  'Constructor','Enum','Interface','Function','Variable','Constant','String',
  'Number','Boolean','Array','Object','Key','Null','EnumMember','Struct',
  'Event','Operator','TypeParameter',
}
local out = {}
local function flatten(syms, depth)
  for _, sym in ipairs(syms or {}) do
    local range = sym.range or (sym.location and sym.location.range) or {}
    local start = range.start or {}
    out[#out+1] = string.format('%s%s (%s) %d:%d',
      string.rep('  ', depth), sym.name, kinds[sym.kind] or tostring(sym.kind),
      (start.line or 0) + 1, (start.character or 0) + 1)
    if sym.children then flatten(sym.children, depth + 1) end
  end
end
local params = {textDocument = {uri = vim.uri_from_fname(fname)}}
local results = vim.lsp.buf_request_sync(bufnr, 'textDocument/documentSymbol', params, 3000)
if results then
  for _, res in pairs(results) do
    if res.result then flatten(res.result, 0) end
  end
end
if #out == 0 then return 'no symbols' end
return table.concat(out, '\n')
`

func registerLSP(s *mcp.Server, c *nvimc.Client) {
	addText(s, "vim_lsp_diagnostics",
		"Get LSP diagnostics as compact 'file:line:col [SEVERITY] message' lines. Omit bufnr for all buffers. (Skill: get-lsp-diagnostics.)",
		func(_ context.Context, in diagnosticsIn) (string, error) {
			bufnr := -1
			if in.Bufnr > 0 {
				bufnr = in.Bufnr
			}
			var out string
			err := c.Lua(luaDiagnostics, &out, bufnr)
			return out, err
		})

	addText(s, "vim_lsp_hover",
		"Get LSP hover documentation for the symbol at file:line:col. (Skill: get-lsp-hover.)",
		func(_ context.Context, in hoverIn) (string, error) {
			var out string
			err := c.Lua(luaHover, &out, in.File, in.Line, in.Col)
			return out, err
		})

	addText(s, "vim_lsp_symbols",
		"List LSP document symbols (functions, classes, methods, variables), nested and depth-indented. (Skill: get-lsp-symbols.)",
		func(_ context.Context, in symbolsIn) (string, error) {
			var out string
			err := c.Lua(luaSymbols, &out, in.File)
			return out, err
		})
}
