// Command nvim-mcp is a Model Context Protocol server that exposes a live
// Neovim instance to an MCP client (e.g. Claude Code) over stdio.
//
// It connects to Neovim's msgpack-RPC socket and maps each MCP tool to a Lua
// round-trip, so all real work runs inside nvim and the server stays a tiny,
// low-footprint shim.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/config"
	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
	"github.com/waldnzwrld/nvim-mcp/internal/surface"
	"github.com/waldnzwrld/nvim-mcp/internal/tools"
)

// version is stamped into the MCP server implementation info.
const version = "0.3.1"

// instructions are surfaced to the MCP client (the model) at initialize time.
// They drive the one-time, in-chat setup prompts. The first tool result of a
// session also carries a [nvim-mcp first-run setup] banner listing whatever is
// still unconfigured, so these prompts surface deterministically on launch.
const instructions = `This server drives a live Neovim session shared with the human user, so your edits are visible to them in their editor.

First-run setup: the first tool result of a session may include a "[nvim-mcp first-run setup]" note listing choices the user has not made yet. When you see it, raise those choices with the user before continuing, then record each answer with the tool named in the note.

Edit-change highlighting: lines you change with vim_edit can be highlighted in the user's buffer so they can see what you touched; the highlight clears when they save that buffer. If unconfigured (vim_edit_highlight status returns "unset"), ask once whether to enable it, then set vim_edit_highlight (on/off).

Auto-open of edited files: a bundled "nvim-mcp hook" PostToolUse hook can open every file you edit (via your Edit/Write tools) into this Neovim session, so the human sees your work in their editor. The MCP server cannot install it — it lives in Claude's settings.json. If it is not installed and the choice is unset (vim_surface_hook status), offer: "Would you like me to automatically open new and edited files in Neovim?" (default: yes). On yes, call vim_surface_hook on — it returns the exact command to run — and add that as a PostToolUse hook (matcher "Edit|Write|MultiEdit") to settings.json via the update-config skill (it takes effect next session); on no, set vim_surface_hook off. Respect the stored choices afterward; the user can change either anytime by asking.`

func main() {
	// `nvim-mcp hook` is the self-contained PostToolUse hook (see internal/surface).
	// It reads a hook payload on stdin, surfaces the edited file into the live
	// Neovim session, and always exits 0 so it can never block a tool call. It is
	// handled before flag parsing so the bare subcommand needs no flags.
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		surface.Run(os.Stdin, os.Getenv("NVIM_SOCKET_PATH"))
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
