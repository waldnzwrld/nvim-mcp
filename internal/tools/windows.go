package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

type windowIn struct {
	Command string `json:"command" jsonschema:"one of: split, vsplit, only, close, or 'wincmd h|j|k|l'"`
}

type markIn struct {
	Mark   string `json:"mark" jsonschema:"a single lowercase letter a-z"`
	Line   int    `json:"line" jsonschema:"1-based line"`
	Column int    `json:"column" jsonschema:"1-based column"`
}

type registerIn struct {
	Register string `json:"register" jsonschema:"register name (a-z or the unnamed register \")"`
	Content  string `json:"content" jsonschema:"text to store in the register"`
}

type visualIn struct {
	StartLine   int `json:"startLine" jsonschema:"1-based start line"`
	StartColumn int `json:"startColumn" jsonschema:"1-based start column"`
	EndLine     int `json:"endLine" jsonschema:"1-based end line"`
	EndColumn   int `json:"endColumn" jsonschema:"1-based end column"`
}

type jumpIn struct {
	Direction string `json:"direction" jsonschema:"one of: back, forward, list"`
}

// openAtIn / vim_open_at ports the open-at-location skill.
type openAtIn struct {
	File string `json:"file" jsonschema:"file path to open"`
	Line int    `json:"line" jsonschema:"1-based line to jump to"`
	Col  *int   `json:"col,omitempty" jsonschema:"optional 1-based column (default 1)"`
}

const luaWindow = `
local c = ...
local simple = {split = 'split', vsplit = 'vsplit', only = 'only', close = 'close'}
if simple[c] then vim.cmd(simple[c]); return 'ok: ' .. c end
local d = c:match('^wincmd%s+([hjkl])$')
if d then vim.cmd('wincmd ' .. d); return 'ok: ' .. c end
return 'error: unknown window command: ' .. c
`

const luaMark = `
local mark, line, col = ...
local buf = vim.api.nvim_get_current_buf()
local ok, err = pcall(vim.api.nvim_buf_set_mark, buf, mark, line, col - 1, {})
if not ok then return 'error: ' .. tostring(err) end
return string.format("mark '%s set at %d:%d", mark, line, col)
`

const luaRegister = `
local reg, content = ...
vim.fn.setreg(reg, content)
return 'register ' .. reg .. ' set'
`

const luaVisual = `
local sl, sc, el, ec = ...
vim.cmd(string.format('normal! %dG%d|v%dG%d|', sl, sc, el, ec))
return string.format('visual %d:%d-%d:%d', sl, sc, el, ec)
`

const luaJump = `
local dir = ...
if dir == 'back' then
  vim.cmd([[execute "normal! \<C-o>"]])
  return 'jumped back'
elseif dir == 'forward' then
  vim.cmd([[execute "normal! \<C-i>"]])
  return 'jumped forward'
elseif dir == 'list' then
  local jl = vim.fn.getjumplist()
  local out = {}
  for _, v in ipairs(jl[1]) do
    local n = ''
    if v.bufnr and v.bufnr > 0 then n = vim.api.nvim_buf_get_name(v.bufnr) end
    out[#out+1] = string.format('%s:%d:%d', n, v.lnum, v.col)
  end
  if #out == 0 then return 'empty jump list' end
  return table.concat(out, '\n')
end
return 'error: direction must be back, forward, or list'
`

const luaOpenAt = luaFindBuf + `
local fname, line, col = ...
col = col or 1
local b = find_buf(fname)
if b ~= -1 then
  vim.cmd('buffer ' .. b)
else
  vim.cmd('edit ' .. vim.fn.fnameescape(fname))
end
vim.api.nvim_win_set_cursor(0, {line, col - 1})
vim.cmd('normal! zz')
return string.format('opened %s at %d:%d', fname, line, col)
`

func registerWindows(s *mcp.Server, c *nvimc.Client) {
	addText(s, "vim_window",
		"Manipulate windows: split, vsplit, only, close, or move focus with 'wincmd h|j|k|l'.",
		func(_ context.Context, in windowIn) (string, error) {
			var out string
			err := c.Lua(luaWindow, &out, in.Command)
			return out, err
		})

	addText(s, "vim_mark",
		"Set a named mark (a-z) at a position.",
		func(_ context.Context, in markIn) (string, error) {
			var out string
			err := c.Lua(luaMark, &out, in.Mark, in.Line, in.Column)
			return out, err
		})

	addText(s, "vim_register",
		"Set the content of a register.",
		func(_ context.Context, in registerIn) (string, error) {
			var out string
			err := c.Lua(luaRegister, &out, in.Register, in.Content)
			return out, err
		})

	addText(s, "vim_visual",
		"Create a visual-mode selection between two positions.",
		func(_ context.Context, in visualIn) (string, error) {
			var out string
			err := c.Lua(luaVisual, &out, in.StartLine, in.StartColumn, in.EndLine, in.EndColumn)
			return out, err
		})

	addText(s, "vim_jump",
		"Navigate the jump list: back, forward, or list.",
		func(_ context.Context, in jumpIn) (string, error) {
			var out string
			err := c.Lua(luaJump, &out, in.Direction)
			return out, err
		})

	addText(s, "vim_open_at",
		"Open a file and jump the cursor to a line:col to surface a finding to the human. (Skill: open-at-location.)",
		func(_ context.Context, in openAtIn) (string, error) {
			var out string
			err := c.Lua(luaOpenAt, &out, in.File, in.Line, optInt(in.Col))
			return out, err
		})
}
