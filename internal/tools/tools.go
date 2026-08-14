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

// addText registers a tool whose typed input In produces a single string that is
// returned to the client as text content. Handler errors become MCP tool errors
// (IsError), so the model can see and self-correct.
func addText[In any](s *mcp.Server, name, desc string, fn func(context.Context, In) (string, error)) {
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
