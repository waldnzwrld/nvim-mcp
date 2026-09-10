// Package tools registers the MCP tool, resource, and prompt surface against a
// live Neovim connection. Every tool is a thin wrapper over a Lua chunk that
// runs inside nvim and returns a ready-to-emit string, keeping responses
// compact (token-efficient) by design.
package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/waldnzwrld/nvim-mcp/internal/config"
	"github.com/waldnzwrld/nvim-mcp/internal/nvimc"
)

// Register wires up the entire surface: the editor/buffer/window/search tools,
// the skill-derived additions (LSP, Treesitter, todo, open-at, quickfix), the
// runtime settings, the resources, and the workflow prompt. cfg holds persisted
// user preferences (e.g. edit-change highlighting).
func Register(s *mcp.Server, c *nvimc.Client, cfg *config.Store) {
	registerCore(s, c, cfg)
	registerBuffers(s, c)
	registerWindows(s, c)
	registerSearch(s, c)
	registerWorkflow(s, c)
	registerLSP(s, c)
	registerAnalysis(s, c)
	registerSettings(s, c, cfg)
	registerResources(s, c)
	registerPrompt(s, c)
}

// registeredNames records every tool name passed through addText, in
// registration order. It is the source of truth for ToolNames (used by the
// `setup` subcommand to derive the settings.json allow-list) so the two can
// never drift. Populated as a side effect of Register; the serving path fills
// it once and ignores it.
var registeredNames []string

// ToolNames runs the full tool registration against a throwaway server and
// returns every registered tool name. It needs no live Neovim: registration
// only builds tool definitions, it does not dial the editor.
func ToolNames() []string {
	registeredNames = nil
	c := nvimc.New("")
	defer c.Close()
	s := mcp.NewServer(&mcp.Implementation{Name: "nvim-mcp"}, nil)
	Register(s, c, config.Load())
	out := make([]string, len(registeredNames))
	copy(out, registeredNames)
	return out
}

// addText registers a tool whose typed input In produces a single string that is
// returned to the client as text content. Handler errors become MCP tool errors
// (IsError), so the model can see and self-correct.
func addText[In any](s *mcp.Server, name, desc string, fn func(context.Context, In) (string, error)) {
	registeredNames = append(registeredNames, name)
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			out, err := fn(ctx, in)
			if err != nil {
				return nil, nil, err
			}
			// Append the one-time first-run setup prompt (empty after it has
			// fired once, or when nothing is left to configure).
			out += firstRunBanner()
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: out}},
			}, nil, nil
		})
}

// optInt converts an optional pointer argument into the value/nil form that
// ExecLua marshals to a Lua number or Lua nil.
func optInt(p *int) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
