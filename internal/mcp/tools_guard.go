package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerGuardStatus adds the guard_status MCP tool.
func (s *Server) registerGuardStatus() {
	tool := mcp.NewTool("guard_status",
		mcp.WithDescription("Query real-time health guard status for vassal processes. "+
			"Returns circuit breaker state, consecutive failure count, and the last check "+
			"result for each configured guard. A circuit_open=true means AI modifications "+
			"via delegate_control are currently blocked for that vassal."),
		mcp.WithString("vassal",
			mcp.Description("Filter results to a specific vassal name. Omit to return all vassals."),
		),
	)
	s.mcpServer.AddTool(tool, s.handleGuardStatus)
}

func (s *Server) handleGuardStatus(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.parentKingdomSocket == "" {
		return mcp.NewToolResultError("no parent kingdom detected; guard_status is only available inside a kingdom directory"), nil
	}

	client, err := newMCPDaemonClient(s.parentKingdomSocket)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot connect to parent daemon: %v", err)), nil
	}
	defer client.Close()

	params := map[string]interface{}{}
	if vassal := req.GetString("vassal", ""); vassal != "" {
		params["vassal"] = vassal
	}

	raw, err := client.Call("guard_status", params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("guard_status failed: %v", err)), nil
	}

	return mcp.NewToolResultText(string(raw)), nil
}
