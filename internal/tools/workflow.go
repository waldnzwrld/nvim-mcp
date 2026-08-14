package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

type macroIn struct {
	Action   string `json:"action" jsonschema:"one of: record, stop, play"`
	Register string `json:"register" jsonschema:"macro register a-z"`
	Count    *int   `json:"count,omitempty" jsonschema:"repeat count for play (default 1)"`
}

type tabIn struct {
	Action   string `json:"action" jsonschema:"one of: new, close, next, prev, first, last, list"`
	Filename string `json:"filename,omitempty" jsonschema:"file to open in a new tab (with action=new)"`
}

type foldIn struct {
	Action    string `json:"action" jsonschema:"one of: create, open, close, toggle, openall, closeall, delete"`
	StartLine *int   `json:"startLine,omitempty" jsonschema:"1-based start line (with action=create)"`
	EndLine   *int   `json:"endLine,omitempty" jsonschema:"1-based end line (with action=create)"`
}

const luaMacro = `
local action, reg, count = ...
if action == 'record' then
  vim.cmd('normal! q' .. reg)
  return 'recording @' .. reg
elseif action == 'stop' then
  vim.cmd('normal! q')
  return 'stopped'
elseif action == 'play' then
  local c = (count and count > 0) and count or 1
  vim.cmd('normal! ' .. c .. '@' .. reg)
  return string.format('played @%s x%d', reg, c)
end
return 'error: action must be record, stop, or play'
`

const luaTab = `
local action, fname = ...
if action == 'new' then
  if fname and fname ~= '' then vim.cmd('tabedit ' .. vim.fn.fnameescape(fname)) else vim.cmd('tabnew') end
  return 'tab created'
elseif action == 'close' then vim.cmd('tabclose'); return 'tab closed'
elseif action == 'next' then vim.cmd('tabnext'); return 'ok'
elseif action == 'prev' then vim.cmd('tabprevious'); return 'ok'
elseif action == 'first' then vim.cmd('tabfirst'); return 'ok'
elseif action == 'last' then vim.cmd('tablast'); return 'ok'
elseif action == 'list' then
  local out = {}
  local cur = vim.api.nvim_get_current_tabpage()
  for i, t in ipairs(vim.api.nvim_list_tabpages()) do
    local win = vim.api.nvim_tabpage_get_win(t)
    local name = vim.api.nvim_buf_get_name(vim.api.nvim_win_get_buf(win))
    out[#out+1] = string.format('%s %d: %s', (t == cur) and '*' or ' ', i, name ~= '' and name or '[No Name]')
  end
  return table.concat(out, '\n')
end
return 'error: unknown tab action'
`

const luaFold = `
local action, sl, el = ...
if action == 'create' then
  if not sl or not el then return 'error: create requires startLine and endLine' end
  vim.cmd(string.format('%d,%dfold', sl, el))
  return 'fold created'
elseif action == 'open' then vim.cmd('normal! zo'); return 'ok'
elseif action == 'close' then vim.cmd('normal! zc'); return 'ok'
elseif action == 'toggle' then vim.cmd('normal! za'); return 'ok'
elseif action == 'openall' then vim.cmd('normal! zR'); return 'ok'
elseif action == 'closeall' then vim.cmd('normal! zM'); return 'ok'
elseif action == 'delete' then vim.cmd('normal! zd'); return 'ok'
end
return 'error: unknown fold action'
`

const luaHealth = `
local v = vim.version()
return string.format('ok: nvim %d.%d.%d, pid %d', v.major, v.minor, v.patch, vim.fn.getpid())
`

func registerWorkflow(s *mcp.Server, c *nvimc.Client) {
	addText(s, "vim_macro",
		"Record, stop, or play Vim macros in a register.",
		func(_ context.Context, in macroIn) (string, error) {
			var out string
			err := c.Lua(luaMacro, &out, in.Action, in.Register, optInt(in.Count))
			return out, err
		})

	addText(s, "vim_tab",
		"Tab management: new, close, next, prev, first, last, list.",
		func(_ context.Context, in tabIn) (string, error) {
			var out string
			err := c.Lua(luaTab, &out, in.Action, in.Filename)
			return out, err
		})

	addText(s, "vim_fold",
		"Code folding: create, open, close, toggle, openall, closeall, delete.",
		func(_ context.Context, in foldIn) (string, error) {
			var out string
			err := c.Lua(luaFold, &out, in.Action, optInt(in.StartLine), optInt(in.EndLine))
			return out, err
		})

	addText(s, "vim_health",
		"Check Neovim connection health and version.",
		func(_ context.Context, _ struct{}) (string, error) {
			var out string
			err := c.Lua(luaHealth, &out)
			return out, err
		})
}
