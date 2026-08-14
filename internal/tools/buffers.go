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

// luaFileOpen ports open-in-nvim / nvim-agent-open: show the file in the current
// window. If it is already loaded, switch to that buffer (avoids E37 on a
// modified buffer); otherwise edit it fresh, then refresh from disk.
const luaFileOpen = luaFindBuf + `
local fname = ...
local b = find_buf(fname)
if b ~= -1 then
  vim.cmd('buffer ' .. b)
else
  vim.cmd('edit ' .. vim.fn.fnameescape(fname))
end
pcall(vim.cmd, 'silent! checktime')
return 'opened ' .. fname
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
