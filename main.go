// Command nvim-mcp is a Model Context Protocol server that exposes a live
// Neovim instance to an MCP client (e.g. Claude Code) over stdio.
//
// It connects to Neovim's msgpack-RPC socket and maps each MCP tool to a Lua
// round-trip, so all real work runs inside nvim and the server stays a tiny,
// low-footprint shim.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/term"

	"github.com/waldnzwrld/nvim-mcp/internal/config"
	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
	"github.com/waldnzwrld/nvim-mcp/internal/setup"
	"github.com/waldnzwrld/nvim-mcp/internal/surface"
	"github.com/waldnzwrld/nvim-mcp/internal/tools"
)

// version is stamped into the MCP server implementation info.
const version = "0.4.1"

// instructions are surfaced to the MCP client (the model) at initialize time.
// They drive the one-time, in-chat setup prompts. The first tool result of a
// session also carries a [nvim-mcp first-run setup] banner listing whatever is
// still unconfigured, so these prompts surface deterministically on launch.
const instructions = `This server drives a live Neovim session shared with the human user, so your edits are visible to them in their editor.

First-run setup: the first tool result of a session may include a "[nvim-mcp first-run setup]" note listing choices the user has not made yet. When you see it, raise those choices with the user before continuing, then record each answer with the tool named in the note.

Edit-change highlighting: lines you change with vim_edit can be highlighted in the user's buffer so they can see what you touched; the highlight clears when they save that buffer. If unconfigured (vim_edit_highlight status returns "unset"), ask once whether to enable it, then set vim_edit_highlight (on/off).`

// claudeInstructions are appended only for Claude Code clients: the auto-open
// hook is a Claude Code PostToolUse hook installed into Claude's settings.json.
const claudeInstructions = `Auto-open of edited files: a bundled "nvim-mcp hook" PostToolUse hook can open every file you edit (via your Edit/Write tools) into this Neovim session, so the human sees your work in their editor. The MCP server cannot install it — it lives in Claude's settings.json. If it is not installed and the choice is unset (vim_surface_hook status), offer: "Would you like me to automatically open new and edited files in Neovim?" (default: yes). On yes, call vim_surface_hook on — it returns the exact command to run — and add that as a PostToolUse hook (matcher "Edit|Write|MultiEdit") to settings.json via the update-config skill (it takes effect next session); on no, set vim_surface_hook off. Respect the stored choices afterward; the user can change either anytime by asking.`

func main() {
	// `nvim-mcp hook` is the self-contained PostToolUse hook (see internal/surface).
	// It reads a hook payload on stdin, surfaces the edited file into the live
	// Neovim session, and always exits 0 so it can never block a tool call. It is
	// handled before flag parsing so the bare subcommand needs no flags.
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		surface.Run(os.Stdin, os.Getenv("NVIM_SOCKET_PATH"))
		return
	}

	// `nvim-mcp open <path>...` surfaces files the same way as the hook, for
	// clients without Claude Code hooks (e.g. a pi extension). Always exits 0.
	if len(os.Args) > 1 && os.Args[1] == "open" {
		surface.Open(os.Getenv("NVIM_SOCKET_PATH"), os.Args[2:]...)
		return
	}

	// `nvim-mcp setup` installs the server into the selected harnesses (see
	// runSetup). `--dry-run` reports what it would change without writing.
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		runSetup(os.Args[2:])
		return
	}

	socket := flag.String("socket", "",
		"Neovim RPC socket/address override. By default the socket Neovim creates automatically is discovered.")
	printAddr := flag.Bool("print-address", false, "print the discovered nvim address and exit")
	flag.Parse()

	if *printAddr {
		if addr := nvimc.DiscoverAddress(*socket); addr != "" {
			fmt.Println(addr)
		} else {
			fmt.Fprintln(os.Stderr, "no running Neovim found")
			os.Exit(1)
		}
		return
	}

	client := nvimc.New(*socket)
	defer client.Close()

	cfg := config.Load()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "nvim-mcp",
		Title:   "Neovim MCP",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: instructions,
		// Advertise and accept resource subscriptions; the actual push is driven
		// by nvim autocmds via client.SetOnEvent below.
		HasResources:       true,
		SubscribeHandler:   func(context.Context, *mcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *mcp.UnsubscribeRequest) error { return nil },
	})

	server.AddReceivingMiddleware(claudeInstructionsMiddleware)
	tools.Register(server, client, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Bridge nvim change events to MCP resources/updated notifications.
	client.SetOnEvent(func(kind string) {
		var uri string
		switch kind {
		case "diagnostics":
			uri = "nvim://diagnostics"
		case "buffers":
			uri = "nvim://buffers"
		default:
			return
		}
		_ = server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri})
	})
	// Best-effort eager connect so autocmds are installed before the first call.
	_ = client.Connect()

	// stderr is safe for logs; stdout is the JSON-RPC transport.
	addr := client.Address()
	if addr == "" {
		addr = "(none found yet; will discover on first call)"
	}
	fmt.Fprintf(os.Stderr, "nvim-mcp %s: serving over stdio, nvim address %s\n", version, addr)

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "nvim-mcp: %v\n", err)
		os.Exit(1)
	}
}

// claudeInstructionsMiddleware appends claudeInstructions to the initialize
// result when the connecting client is Claude Code.
func claudeInstructionsMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if ir, ok := res.(*mcp.InitializeResult); ok && err == nil {
			if p, ok := req.GetParams().(*mcp.InitializeParams); ok && tools.ClaudeCode(p) {
				ir.Instructions += "\n\n" + claudeInstructions
			}
		}
		return res, err
	}
}

// runSetup handles `nvim-mcp setup [--harness ids] [--dry-run]`: it detects the
// supported harnesses, selects which to install into (--harness, else an
// interactive prompt on a terminal, else every detected one), and installs the
// server into each.
func runSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	var dryRun bool
	fs.BoolVar(&dryRun, "dry-run", false, "report what would change, write nothing")
	fs.BoolVar(&dryRun, "n", false, "shorthand for --dry-run")
	only := fs.String("harness", "", "comma-separated harnesses to install into (claude, pi); skips the prompt")
	_ = fs.Parse(args)

	all := setup.Harnesses()
	selected, err := selectHarnesses(all, *only)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nvim-mcp setup: %v\n", err)
		os.Exit(2)
	}
	if len(selected) == 0 {
		fmt.Println("nvim-mcp setup: no harness selected, nothing to do")
		return
	}

	cfg := config.Load()
	on, decided := cfg.SurfaceHook()
	opts := setup.Options{Exe: config.Executable(), AutoOpen: on || !decided, DryRun: dryRun}
	for _, name := range tools.ToolNames() {
		opts.Allow = append(opts.Allow, "mcp__"+setup.ServerName+"__"+name)
	}

	failed := false
	for _, h := range selected {
		fmt.Printf("%s:\n", h.Name)
		lines, err := h.Install(opts)
		for _, l := range lines {
			fmt.Printf("  %s\n", l)
		}
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			failed = true
		}
	}
	if opts.AutoOpen && !decided && !dryRun && !failed {
		_ = cfg.SetSurfaceHook(true)
	}
	if !dryRun {
		fmt.Println("Changes take effect in each harness's next session.")
	}
	if failed {
		os.Exit(1)
	}
}

// selectHarnesses resolves which harnesses to install into: the ids in only
// when given, else the user's pick from a prompt when stdin is a terminal,
// else every detected harness.
func selectHarnesses(all []setup.Harness, only string) ([]setup.Harness, error) {
	var detected []setup.Harness
	for _, h := range all {
		if h.Detected {
			detected = append(detected, h)
		}
	}
	if only != "" {
		var out []setup.Harness
		for _, id := range strings.Split(only, ",") {
			id = strings.TrimSpace(id)
			i := slices.IndexFunc(all, func(h setup.Harness) bool { return h.ID == id })
			if i < 0 {
				return nil, fmt.Errorf("unknown harness %q", id)
			}
			out = appendUnique(out, all[i])
		}
		return out, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return detected, nil
	}

	fmt.Println("Install nvim-mcp into:")
	for i, h := range all {
		mark := "not detected"
		if h.Detected {
			mark = "detected"
		}
		fmt.Printf("  %d) %s (%s)\n", i+1, h.Name, mark)
	}
	fmt.Print("Numbers, comma-separated [Enter = all detected]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return detected, nil
	}
	var out []setup.Harness
	for _, f := range strings.Split(line, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 1 || n > len(all) {
			return nil, fmt.Errorf("invalid choice %q", strings.TrimSpace(f))
		}
		out = appendUnique(out, all[n-1])
	}
	return out, nil
}

func appendUnique(hs []setup.Harness, h setup.Harness) []setup.Harness {
	if slices.ContainsFunc(hs, func(x setup.Harness) bool { return x.ID == h.ID }) {
		return hs
	}
	return append(hs, h)
}
