package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

type switchIn struct {
	Identifier string `json:"identifier" jsonschema:"buffer name or number to switch to"`
}

type saveIn struct {
	Filename string `json:"filename,omitempty" jsonschema:"optional buffer to save; defaults to the current buffer"`
}

type openIn struct {
	Filename string `json:"filename" jsonschema:"file path to open as a buffer"`
}

const luaBufferSwitch = `
local id = ...
local ok, err = pcall(function() vim.cmd('buffer ' .. tostring(id)) end)
if not ok then return 'error: ' .. tostring(err) end
return 'switched to ' .. tostring(id)
`

// luaBufferSave ports the save-buffer skill: write the current buffer, or a
// named one if it is loaded.
const luaBufferSave = `
local fname = ...
if fname == nil or fname == '' then
  local ok, err = pcall(function() vim.cmd('write') end)
  if not ok then return 'error: ' .. tostring(err) end
  return 'saved'
end
local b = vim.fn.bufnr(fname)
if b ~= -1 and vim.api.nvim_buf_is_loaded(b) then
  vim.api.nvim_buf_call(b, function() vim.cmd('write') end)
  return 'saved ' .. fname
end
return 'not found: ' .. fname
`

// luaFindBuf resolves symlinks (e.g. macOS /tmp -> /private/tmp) and returns a
// loaded buffer matching the path, or -1. Shared prefix for open tools.
const luaFindBuf = `
local function find_buf(fname)
  local target = vim.fn.resolve(vim.fn.fnamemodify(fname, ':p'))
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(b) then
      local n = vim.api.nvim_buf_get_name(b)
      if n ~= '' and vim.fn.resolve(n) == target then return b end
    end
  end
  return -1
end
`

// luaOpenHelpers extends luaFindBuf with window targeting so a file open never
// clobbers the chat/terminal pane. A window is a valid target only if its buffer
// is a real file (buftype "") or scratch (buftype "nofile"); terminals, help,
// quickfix, prompt and floating windows are skipped. The file is shown in a file
// pane without stealing focus from the caller (the chat terminal), so it never
// interrupts the human's typing.
const luaOpenHelpers = luaFindBuf + `
local function win_reusable(win)
  if vim.api.nvim_win_get_config(win).relative ~= '' then return false end -- floating
  local bt = vim.bo[vim.api.nvim_win_get_buf(win)].buftype
  return bt == '' or bt == 'nofile'
end

-- pick_win chooses where to show target_buf: a window already showing it, else
-- the current window if reusable, else any reusable window (real file panes
-- before scratch). Returns nil when only non-reusable windows (e.g. the chat
-- terminal) exist, signalling the caller to make a new split.
local function pick_win(target_buf)
  local wins = vim.api.nvim_tabpage_list_wins(0)
  if target_buf and target_buf ~= -1 then
    for _, w in ipairs(wins) do
      if win_reusable(w) and vim.api.nvim_win_get_buf(w) == target_buf then return w end
    end
  end
  local cur = vim.api.nvim_get_current_win()
  if win_reusable(cur) then return cur end
  local fallback
  for _, w in ipairs(wins) do
    if win_reusable(w) then
      if vim.bo[vim.api.nvim_win_get_buf(w)].buftype == '' then return w end
      fallback = fallback or w
    end
  end
  return fallback
end

-- show_file displays fname in a safe window without moving focus, and returns
-- (window, buffer). b is a preloaded buffer number or -1 to load fresh.
local function show_file(fname, b)
  if b == -1 then
    b = vim.fn.bufadd(vim.fn.resolve(vim.fn.fnamemodify(fname, ':p')))
    vim.fn.bufload(b)
    vim.bo[b].buflisted = true
  end
  local win = pick_win(b)
  if win == nil then
    -- Only non-reusable windows (the chat terminal): split off it and restore
    -- focus so the terminal is never overwritten and typing is uninterrupted.
    local orig = vim.api.nvim_get_current_win()
    vim.cmd('vsplit')
    win = vim.api.nvim_get_current_win()
    vim.api.nvim_set_current_win(orig)
  end
  vim.api.nvim_win_set_buf(win, b)
  return win, b
end
`

// luaFileOpen ports open-in-nvim / nvim-agent-open: show the file in a file pane
// (never the chat terminal) and refresh it from disk.
const luaFileOpen = luaOpenHelpers + `
local fname = ...
local win, b = show_file(fname, find_buf(fname))
vim.api.nvim_buf_call(b, function() pcall(vim.cmd, 'silent! checktime') end)
return 'opened ' .. fname .. ' in window ' .. win
`

func registerBuffers(s *mcp.Server, c *nvimc.Client) {
	addText(s, "vim_buffer_switch",
		"Switch to a buffer by name or number.",
		func(_ context.Context, in switchIn) (string, error) {
			var out string
			err := c.Lua(luaBufferSwitch, &out, in.Identifier)
			return out, err
		})

	addText(s, "vim_buffer_save",
		"Save the current buffer, or a specific loaded buffer by filename.",
		func(_ context.Context, in saveIn) (string, error) {
			var out string
			err := c.Lua(luaBufferSave, &out, in.Filename)
			return out, err
		})

	addText(s, "vim_file_open",
		"Open a file into a new buffer and refresh it from disk.",
		func(_ context.Context, in openIn) (string, error) {
			var out string
			err := c.Lua(luaFileOpen, &out, in.Filename)
			return out, err
		})
}
