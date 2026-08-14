package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

type tsContextIn struct {
	File string `json:"file,omitempty" jsonschema:"file path; defaults to the current buffer"`
	Line *int   `json:"line,omitempty" jsonschema:"1-based line; defaults to the cursor line"`
	Col  *int   `json:"col,omitempty" jsonschema:"1-based column; defaults to the cursor column"`
}

type todoIn struct {
	Keyword string `json:"keyword" jsonschema:"todo-comments keyword: FIX, FAILED, TODO, HACK, WARN, NOTE, PERF"`
	File    string `json:"file" jsonschema:"file path to annotate"`
	Line    int    `json:"line" jsonschema:"1-based line; the comment is inserted above it"`
	Message string `json:"message" jsonschema:"the comment message"`
}

// luaTsContext ports get-treesitter-context: nearest enclosing scope.
const luaTsContext = `
local fname, line, col = ...
local scope_types = {
  ['function'] = true, function_definition = true, function_declaration = true,
  method_definition = true, method_declaration = true, arrow_function = true,
  function_item = true, impl_item = true, class_definition = true,
  class_declaration = true, class = true, struct_item = true, module = true,
}
local function find_scope(bufnr, row, c)
  -- Ensure the tree is parsed; on a freshly opened buffer the parser may be lazy.
  pcall(function()
    local p = vim.treesitter.get_parser(bufnr)
    if p then p:parse() end
  end)
  local ok, node = pcall(vim.treesitter.get_node, {bufnr = bufnr, pos = {row, c}})
  if not ok or not node then return nil end
  local t = node
  while t and not scope_types[t:type()] do t = t:parent() end
  if not t then return nil end
  local sr, sc, er, ec = t:range()
  local name_node = t:field('name')[1]
  local name = name_node and vim.treesitter.get_node_text(name_node, bufnr) or '?'
  return string.format('%s %s @ %d:%d-%d:%d', t:type(), name, sr + 1, sc + 1, er + 1, ec + 1)
end
local bufnr, row, c
if fname and fname ~= '' then
  bufnr = vim.fn.bufnr(fname)
  if bufnr == -1 then return 'error: buffer not loaded (open the file first)' end
  row = (line or 1) - 1
  c = (col or 1) - 1
else
  bufnr = vim.api.nvim_get_current_buf()
  local pos = vim.api.nvim_win_get_cursor(0)
  row = pos[1] - 1
  c = pos[2]
  fname = vim.api.nvim_buf_get_name(bufnr)
end
local scope = find_scope(bufnr, row, c)
if not scope then return 'no enclosing scope' end
return scope .. ' (' .. fname .. ')'
`

// luaInjectTodo ports inject-todo-comment: insert a todo-comments-style comment
// above a line, using the buffer's commentstring and matching indentation. The
// buffer is left modified (an intentional, visible co-pilot marker).
const luaInjectTodo = `
local keyword, fname, line, message = ...
local bufnr = vim.fn.bufnr(fname)
if bufnr == -1 then
  vim.cmd('badd ' .. vim.fn.fnameescape(fname))
  bufnr = vim.fn.bufnr(fname)
  vim.fn.bufload(bufnr)
end
if bufnr == -1 then return 'error: cannot open ' .. fname end
local cs = vim.api.nvim_get_option_value('commentstring', {buf = bufnr})
if cs == nil or cs == '' then cs = '// %s' end
local target = vim.api.nvim_buf_get_lines(bufnr, line - 1, line, false)[1] or ''
local indent = target:match('^%s*') or ''
local body = string.format('%s: %s', keyword, message)
local commented = cs:gsub('%%s', body)
local text = indent .. commented
vim.api.nvim_buf_set_lines(bufnr, line - 1, line - 1, false, {text})
return 'injected: ' .. text
`

func registerAnalysis(s *mcp.Server, c *nvimc.Client) {
	addText(s, "vim_treesitter_context",
		"Find the nearest enclosing function/class/method scope at a position via Treesitter. Defaults to the cursor. (Skill: get-treesitter-context.)",
		func(_ context.Context, in tsContextIn) (string, error) {
			var out string
			err := c.Lua(luaTsContext, &out, in.File, optInt(in.Line), optInt(in.Col))
			return out, err
		})

	addText(s, "vim_inject_todo",
		"Insert a todo-comments-style comment above a line to flag work without changing code logic. (Skill: inject-todo-comment.)",
		func(_ context.Context, in todoIn) (string, error) {
			var out string
			err := c.Lua(luaInjectTodo, &out, in.Keyword, in.File, in.Line, in.Message)
			return out, err
		})
}
