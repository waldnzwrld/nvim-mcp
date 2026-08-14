package tools

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/config"
	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

type bufferIn struct {
	Filename string `json:"filename,omitempty" jsonschema:"optional file path; defaults to the current buffer"`
}

type commandIn struct {
	Command string `json:"command" jsonschema:"an ex-command to run (e.g. 'w', 'bn', '10d'); a leading ! runs a shell command when ALLOW_SHELL_COMMANDS=true"`
}

type editIn struct {
	StartLine int    `json:"startLine" jsonschema:"1-based line to start editing at"`
	Mode      string `json:"mode" jsonschema:"one of: insert, replace, replaceAll"`
	Lines     string `json:"lines" jsonschema:"newline-separated text to insert or replace with"`
}

type execLuaIn struct {
	Code string `json:"code" jsonschema:"Lua to run inside Neovim; prefix with 'return' to return a value (rendered as a string)"`
}

const luaBuffer = `
local fname = ...
local buf
if fname == nil or fname == '' then
  buf = vim.api.nvim_get_current_buf()
else
  buf = vim.fn.bufnr(fname)
end
if buf == -1 or not vim.api.nvim_buf_is_loaded(buf) then
  return 'error: buffer not loaded'
end
local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
local out = {}
for i, l in ipairs(lines) do out[i] = string.format('%d\t%s', i, l) end
return table.concat(out, '\n')
`

const luaCommand = `
local cmd, allowShell = ...
if cmd:sub(1,1) == '!' and not allowShell then
  return 'error: shell commands are disabled (set ALLOW_SHELL_COMMANDS=true to enable)'
end
local ok, res = pcall(function() return vim.api.nvim_exec2(cmd, {output = true}) end)
if not ok then return 'error: ' .. tostring(res) end
local o = res.output or ''
if o == '' then return 'ok' end
return o
`

const luaStatus = `
local buf = vim.api.nvim_get_current_buf()
local win = vim.api.nvim_get_current_win()
local pos = vim.api.nvim_win_get_cursor(win)
local name = vim.api.nvim_buf_get_name(buf)
local clients = {}
for _, cl in ipairs(vim.lsp.get_clients({bufnr = buf})) do clients[#clients+1] = cl.name end
local out = {
  'file: ' .. (name ~= '' and name or '[No Name]'),
  string.format('cursor: %d:%d', pos[1], pos[2] + 1),
  'mode: ' .. vim.api.nvim_get_mode().mode,
  'filetype: ' .. vim.api.nvim_get_option_value('filetype', {buf = buf}),
  'modified: ' .. tostring(vim.api.nvim_get_option_value('modified', {buf = buf})),
  'lines: ' .. tostring(vim.api.nvim_buf_line_count(buf)),
  'windows: ' .. tostring(#vim.api.nvim_tabpage_list_wins(0)),
  'tabpage: ' .. tostring(vim.api.nvim_get_current_tabpage()),
  'cwd: ' .. vim.fn.getcwd(),
  'lsp: ' .. (#clients > 0 and table.concat(clients, ', ') or 'none'),
}
return table.concat(out, '\n')
`

const luaEdit = `
local startLine, mode, text, highlight = ...
local buf = vim.api.nvim_get_current_buf()
local newlines = vim.split(text, '\n', {plain = true})
local first, last
if mode == 'insert' then
  first = startLine - 1
  vim.api.nvim_buf_set_lines(buf, first, first, false, newlines)
  last = first + #newlines
elseif mode == 'replaceAll' then
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, newlines)
  first, last = 0, #newlines
elseif mode == 'replace' then
  first = startLine - 1
  vim.api.nvim_buf_set_lines(buf, first, first + #newlines, false, newlines)
  last = first + #newlines
else
  return 'error: mode must be insert, replace, or replaceAll'
end
if highlight then
  -- default=true so a user colorscheme that defines NvimMcpEdit wins.
  vim.api.nvim_set_hl(0, 'NvimMcpEdit', {link = 'DiffAdd', default = true})
  local ns = vim.api.nvim_create_namespace('nvim_mcp_edit')
  local lc = vim.api.nvim_buf_line_count(buf)
  for l = first, math.min(last, lc) - 1 do
    -- Per-line extmarks track the changed range and move with later edits;
    -- they accumulate across edits and are cleared when the buffer is saved.
    pcall(vim.api.nvim_buf_set_extmark, buf, ns, l, 0, {line_hl_group = 'NvimMcpEdit'})
  end
end
return string.format('%s: %d line(s) at %d', mode, #newlines, startLine)
`

func registerCore(s *mcp.Server, c *nvimc.Client, cfg *config.Store) {
	allowShell := os.Getenv("ALLOW_SHELL_COMMANDS") == "true"

	addText(s, "vim_buffer",
		"Get buffer contents with line numbers. Optional 'filename' selects a loaded buffer; defaults to the current one.",
		func(_ context.Context, in bufferIn) (string, error) {
			var out string
			err := c.Lua(luaBuffer, &out, in.Filename)
			return out, err
		})

	addText(s, "vim_command",
		"Send an ex-command to Neovim for navigation, spot editing, and line deletion. Returns command output or an error.",
		func(_ context.Context, in commandIn) (string, error) {
			var out string
			err := c.Lua(luaCommand, &out, in.Command, allowShell)
			return out, err
		})

	addText(s, "vim_status",
		"Get comprehensive Neovim status: file, cursor, mode, filetype, modified flag, window/tab layout, cwd, and attached LSP clients.",
		func(_ context.Context, _ struct{}) (string, error) {
			var out string
			err := c.Lua(luaStatus, &out)
			return out, err
		})

	addText(s, "vim_edit",
		"Edit the current buffer using insert, replace, or replaceAll modes. 'lines' may contain newlines.",
		func(_ context.Context, in editIn) (string, error) {
			hl, decided := cfg.HighlightEdits()
			var out string
			if err := c.Lua(luaEdit, &out, in.StartLine, in.Mode, in.Lines, hl); err != nil {
				return "", err
			}
			if !decided {
				out += "\n(note: edit-change highlighting is not configured. Ask the user whether to highlight the lines you edit in their live buffer, then record their choice with vim_edit_highlight.)"
			}
			return out, nil
		})

	addText(s, "vim_exec_lua",
		"Escape hatch: run arbitrary Lua inside Neovim and return its value as a string.",
		func(_ context.Context, in execLuaIn) (string, error) {
			var out interface{}
			if err := c.Lua(in.Code, &out); err != nil {
				return "", err
			}
			return fmt.Sprintf("%v", out), nil
		})
}
