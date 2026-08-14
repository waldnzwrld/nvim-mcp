package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

type searchIn struct {
	Pattern    string `json:"pattern" jsonschema:"a Vim regex to search for"`
	IgnoreCase bool   `json:"ignoreCase,omitempty" jsonschema:"case-insensitive match"`
	WholeWord  bool   `json:"wholeWord,omitempty" jsonschema:"match whole words only"`
}

type replaceIn struct {
	Pattern     string `json:"pattern" jsonschema:"a Vim regex to find"`
	Replacement string `json:"replacement" jsonschema:"replacement text"`
	Global      bool   `json:"global,omitempty" jsonschema:"replace all matches on each line"`
	IgnoreCase  bool   `json:"ignoreCase,omitempty" jsonschema:"case-insensitive match"`
	Confirm     bool   `json:"confirm,omitempty" jsonschema:"prompt before each replacement"`
}

type grepIn struct {
	Pattern     string `json:"pattern" jsonschema:"a Vim regex to search for across files"`
	FilePattern string `json:"filePattern,omitempty" jsonschema:"file glob to search (default **/*)"`
}

const luaSearch = `
local pattern, ic, ww = ...
local rx = pattern
if ww then rx = '\\<' .. rx .. '\\>' end
rx = (ic and '\\c' or '\\C') .. rx
local buf = vim.api.nvim_get_current_buf()
local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
local out = {}
for i, l in ipairs(lines) do
  local col = vim.fn.match(l, rx)
  if col >= 0 then out[#out+1] = string.format('%d:%d\t%s', i, col + 1, l) end
end
if #out == 0 then return 'no matches' end
return table.concat(out, '\n')
`

const luaSearchReplace = `
local pat, rep, g, ic, confirm = ...
local flags = ''
if g then flags = flags .. 'g' end
if ic then flags = flags .. 'i' end
if confirm then flags = flags .. 'c' end
local cmd = string.format('%%s/%s/%s/%s', pat, rep, flags)
local ok, err = pcall(function() vim.cmd(cmd) end)
if not ok then return 'error: ' .. tostring(err) end
return 'substituted'
`

const luaGrep = `
local pat, fp = ...
if fp == nil or fp == '' then fp = '**/*' end
local ok, err = pcall(function() vim.cmd(string.format('silent! vimgrep /%s/j %s', pat, fp)) end)
if not ok then return 'error: ' .. tostring(err) end
local out = {}
for _, v in ipairs(vim.fn.getqflist()) do
  local n = ''
  if v.bufnr and v.bufnr > 0 then n = vim.api.nvim_buf_get_name(v.bufnr) end
  out[#out+1] = string.format('%s:%d:%d\t%s', n, v.lnum, v.col, (v.text or ''):gsub('^%s+', ''))
end
if #out == 0 then return 'no matches' end
return table.concat(out, '\n')
`

// luaQuickfix ports the get-quickfix-list skill.
const luaQuickfix = `
local qf = vim.fn.getqflist({all = 1})
local out = {}
for _, v in ipairs(qf.items or {}) do
  local n = ''
  if v.bufnr and v.bufnr > 0 then n = vim.api.nvim_buf_get_name(v.bufnr) end
  out[#out+1] = string.format('%s:%d:%d [%s] %s', n, v.lnum or 0, v.col or 0, v.type or '', (v.text or ''):gsub('^%s+', ''))
end
if #out == 0 then return 'quickfix list empty' end
local title = (qf.title ~= nil and qf.title ~= '') and (qf.title .. '\n') or ''
return title .. table.concat(out, '\n')
`

func registerSearch(s *mcp.Server, c *nvimc.Client) {
	addText(s, "vim_search",
		"Search within the current buffer with regex support. Returns matching line:col positions.",
		func(_ context.Context, in searchIn) (string, error) {
			var out string
			err := c.Lua(luaSearch, &out, in.Pattern, in.IgnoreCase, in.WholeWord)
			return out, err
		})

	addText(s, "vim_search_replace",
		"Find and replace across the current buffer with global/ignoreCase/confirm options.",
		func(_ context.Context, in replaceIn) (string, error) {
			var out string
			err := c.Lua(luaSearchReplace, &out, in.Pattern, in.Replacement, in.Global, in.IgnoreCase, in.Confirm)
			return out, err
		})

	addText(s, "vim_grep",
		"Project-wide search using vimgrep; returns quickfix results with file locations.",
		func(_ context.Context, in grepIn) (string, error) {
			var out string
			err := c.Lua(luaGrep, &out, in.Pattern, in.FilePattern)
			return out, err
		})

	addText(s, "vim_quickfix",
		"Read the current quickfix list (LSP errors, compiler output, grep results, test failures). (Skill: get-quickfix-list.)",
		func(_ context.Context, _ struct{}) (string, error) {
			var out string
			err := c.Lua(luaQuickfix, &out)
			return out, err
		})
}
