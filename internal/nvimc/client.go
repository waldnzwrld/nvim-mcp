// Package nvimc holds a persistent msgpack-RPC connection to a running Neovim
// instance and exposes a small helper surface (Lua / Command / Exec) that the
// MCP tools are built on. The connection is lazy and self-healing: it dials on
// first use and reconnects once if a call fails on a dropped socket.
package nvimc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/neovim/go-client/nvim"
)

// Client is a goroutine-safe wrapper around a single Neovim RPC connection. The
// address is resolved lazily at connect time (see DiscoverAddress) so a Neovim
// started after this process is still found.
type Client struct {
	flagAddr string

	mu      sync.Mutex
	conn    *nvim.Nvim
	onEvent func(kind string)
}

// luaSetupEvents installs autocmds that push change notifications back to this
// client's RPC channel, so the MCP server can emit resources/updated. The
// channel id is passed as the sole argument.
const luaSetupEvents = `
local chan = ...
local grp = vim.api.nvim_create_augroup('NvimMcpEvents', {clear = true})
vim.api.nvim_create_autocmd('DiagnosticChanged', {
  group = grp,
  callback = function() pcall(vim.rpcnotify, chan, 'nvim_mcp_event', 'diagnostics') end,
})
vim.api.nvim_create_autocmd({'BufAdd', 'BufDelete', 'BufWritePost', 'BufFilePost'}, {
  group = grp,
  callback = function() pcall(vim.rpcnotify, chan, 'nvim_mcp_event', 'buffers') end,
})
-- Edit-change highlighting support: define the highlight group once, and clear
-- a buffer's edit highlights when it is saved (saving = the user accepting the
-- change). Applying the highlights themselves is done by the vim_edit tool.
vim.api.nvim_set_hl(0, 'NvimMcpEdit', {link = 'DiffAdd', default = true})
vim.api.nvim_create_autocmd('BufWritePost', {
  group = grp,
  callback = function(a)
    local ns = vim.api.nvim_create_namespace('nvim_mcp_edit')
    pcall(vim.api.nvim_buf_clear_namespace, a.buf, ns, 0, -1)
  end,
})
return 1
`

// New returns a client. flagAddr is an optional explicit address override; when
// empty the default Neovim socket is discovered at connect time.
func New(flagAddr string) *Client {
	return &Client{flagAddr: flagAddr}
}

// Address reports the address the client would currently dial (may be empty if
// no running Neovim is found yet).
func (c *Client) Address() string { return DiscoverAddress(c.flagAddr) }

// SetOnEvent registers a callback invoked when Neovim reports a change (kind is
// "diagnostics" or "buffers"). It takes effect on the next connect. The callback
// runs on the RPC receive goroutine and must not call back into the client.
func (c *Client) SetOnEvent(fn func(kind string)) {
	c.mu.Lock()
	c.onEvent = fn
	c.mu.Unlock()
}

// Connect eagerly establishes the connection (and event bridge). It is
// best-effort: an error is returned but is safe to ignore, since the next tool
// call will retry.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.ensure()
	return err
}

// ensure returns a live connection, dialing if necessary. Caller must hold mu.
func (c *Client) ensure() (*nvim.Nvim, error) {
	if c.conn != nil {
		return c.conn, nil
	}
	addr := DiscoverAddress(c.flagAddr)
	if addr == "" {
		return nil, fmt.Errorf("no running Neovim found; start Neovim, or set NVIM_SOCKET_PATH / --socket")
	}
	conn, err := nvim.Dial(addr)
	if err != nil {
		return nil, err
	}
	c.conn = conn
	c.setupEvents(conn)
	return conn, nil
}

// setupEvents wires the nvim→MCP change bridge on a freshly dialed connection.
// Caller holds mu. Failures are non-fatal (subscriptions simply won't fire).
func (c *Client) setupEvents(conn *nvim.Nvim) {
	if c.onEvent == nil {
		return
	}
	fn := c.onEvent
	if err := conn.RegisterHandler("nvim_mcp_event", func(kind string) {
		fn(kind)
	}); err != nil {
		return
	}
	_ = conn.ExecLua(luaSetupEvents, nil, conn.ChannelID())
}

// reset drops the current connection so the next call redials. Caller holds mu.
func (c *Client) reset() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// Lua executes a Lua chunk and decodes its returned value into result (which may
// be nil). Arguments are available as `...` inside the chunk. A chunk that
// should yield a value must prefix it with `return`. On a connection error the
// call is retried once against a fresh connection.
func (c *Client) Lua(code string, result interface{}, args ...interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := c.ensure()
	if err != nil {
		return err
	}
	if err := conn.ExecLua(code, result, args...); err != nil {
		if !isConnErr(err) {
			return err
		}
		c.reset()
		conn, err = c.ensure()
		if err != nil {
			return err
		}
		return conn.ExecLua(code, result, args...)
	}
	return nil
}

// Command runs a single ex-command.
func (c *Client) Command(cmd string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, err := c.ensure()
	if err != nil {
		return err
	}
	if err := conn.Command(cmd); err != nil && isConnErr(err) {
		c.reset()
		if conn, err = c.ensure(); err != nil {
			return err
		}
		return conn.Command(cmd)
	} else {
		return err
	}
}

// Close releases the underlying connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reset()
}

// isConnErr reports whether err looks like a broken/absent RPC connection, as
// opposed to a Lua/Vim error returned by an otherwise-healthy nvim.
func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{"closed", "broken pipe", "eof", "connection", "reset", "use of closed"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// DiscoverAddress resolves the nvim address to dial, in priority order:
//
//  1. an explicit flag value,
//  2. NVIM_SOCKET_PATH,
//  3. NVIM / NVIM_LISTEN_ADDRESS (set when launched inside Neovim),
//  4. the default server socket Neovim creates automatically at startup.
//
// It returns "" when no running Neovim can be found, so the caller can report a
// clear error rather than dialing a bogus path.
func DiscoverAddress(flag string) string {
	if flag != "" {
		return flag
	}
	for _, env := range []string{"NVIM_SOCKET_PATH", "NVIM", "NVIM_LISTEN_ADDRESS"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return defaultSocket()
}

// defaultSocket finds the socket Neovim creates automatically at startup under
// stdpath("run") — $XDG_RUNTIME_DIR/nvim.<user>/ on Linux, $TMPDIR/nvim.<user>/
// on macOS — as documented at :help rpc-connecting / server_address. It returns
// the newest socket that actually accepts a connection, skipping stale sockets
// left by crashed instances.
func defaultSocket() string {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}

	var dirs []string
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		dirs = append(dirs, x)
	}
	if t := os.Getenv("TMPDIR"); t != "" {
		dirs = append(dirs, strings.TrimRight(t, "/"))
	}
	dirs = append(dirs, "/tmp")

	var globs []string
	for _, d := range dirs {
		if user != "" {
			globs = append(globs, filepath.Join(d, "nvim."+user, "*", "nvim.*")) // stdpath('run')/nvim.<pid>.N
		}
		globs = append(globs,
			filepath.Join(d, "nvim.*", "*", "nvim.*"), // any user's run dir layout
			filepath.Join(d, "nvim.*"),                // flat XDG layout: nvim.<pid>.N
		)
	}

	type cand struct {
		path string
		mod  int64
	}
	seen := map[string]bool{}
	var found []cand
	for _, g := range globs {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			info, err := os.Stat(m)
			if err != nil || info.IsDir() || info.Mode()&os.ModeSocket == 0 {
				continue
			}
			found = append(found, cand{m, info.ModTime().UnixNano()})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].mod > found[j].mod })
	for _, c := range found {
		if socketAlive(c.path) {
			return c.path
		}
	}
	return ""
}

// socketAlive reports whether a unix socket currently accepts connections, so
// stale sockets from dead Neovim instances are skipped.
func socketAlive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
