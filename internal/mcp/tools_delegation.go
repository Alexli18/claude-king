package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// rpcRequest is a minimal JSON-RPC 2.0 request used to talk to the King daemon.
type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     int             `json:"id"`
}

// rpcResponse is a minimal JSON-RPC 2.0 response.
type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// daemonClient is a minimal JSON-RPC client for the King daemon Unix socket.
// It is defined here (not importing internal/daemon) to avoid an import cycle:
// internal/daemon already imports internal/mcp.
type daemonClient struct {
	conn    net.Conn
	scanner *bufio.Scanner
	nextID  atomic.Int64
}

// newMCPDaemonClient opens a connection to the daemon at the given Unix socket path.
func newMCPDaemonClient(socketPath string) (*daemonClient, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon socket: %w", err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &daemonClient{conn: conn, scanner: scanner}, nil
}

// Call sends a JSON-RPC request and returns the raw result bytes.
func (c *daemonClient) Call(method string, params interface{}) (json.RawMessage, error) {
	id := int(c.nextID.Add(1))

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}

	req := rpcRequest{Method: method, Params: rawParams, ID: id}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.conn.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		return nil, fmt.Errorf("connection closed")
	}

	var resp rpcResponse
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	result, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return result, nil
}

// Close closes the underlying connection.
func (c *daemonClient) Close() error {
	return c.conn.Close()
}

// registerDelegateControl adds the delegate_control MCP tool.
func (s *Server) registerDelegateControl() {
	tool := mcp.NewTool("delegate_control",
		mcp.WithDescription("Take full control of a vassal process. "+
			"The King daemon will stop managing restarts for this vassal "+
			"until delegate_release is called or this session ends."),
		mcp.WithString("vassal",
			mcp.Required(),
			mcp.Description("Name of the vassal to take control of (e.g. 'ui', 'api')"),
		),
		mcp.WithBoolean("force",
			mcp.Description("Override an existing delegation from another session. Default: false."),
		),
	)
	s.mcpServer.AddTool(tool, s.handleDelegateControl)
}

func (s *Server) handleDelegateControl(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	vassal, err := req.RequireString("vassal")
	if err != nil {
		return mcp.NewToolResultError("vassal is required"), nil
	}
	force := req.GetBool("force", false)

	if s.parentKingdomSocket == "" {
		return mcp.NewToolResultError("no parent kingdom detected; delegate_control is only available inside a kingdom directory"), nil
	}

	client, err := newMCPDaemonClient(s.parentKingdomSocket)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot connect to parent daemon: %v", err)), nil
	}
	defer client.Close()

	params := map[string]interface{}{
		"vassal":      vassal,
		"session_pid": os.Getpid(),
		"force":       force,
	}
	raw, err := client.Call("delegate_control", params)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delegate_control failed: %v", err)), nil
	}

	// Start heartbeat goroutine on first successful delegation
	s.startHeartbeat(vassal)

	return mcp.NewToolResultText(string(raw)), nil
}

// registerDelegateRelease adds the delegate_release MCP tool.
func (s *Server) registerDelegateRelease() {
	tool := mcp.NewTool("delegate_release",
		mcp.WithDescription("Return control of a vassal back to the King daemon."),
		mcp.WithString("vassal",
			mcp.Required(),
			mcp.Description("Name of the vassal to release"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleDelegateRelease)
}

func (s *Server) handleDelegateRelease(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	vassal, err := req.RequireString("vassal")
	if err != nil {
		return mcp.NewToolResultError("vassal is required"), nil
	}

	if s.parentKingdomSocket == "" {
		return mcp.NewToolResultError("no parent kingdom connection"), nil
	}

	client, err := newMCPDaemonClient(s.parentKingdomSocket)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot connect to parent daemon: %v", err)), nil
	}
	defer client.Close()

	raw, err := client.Call("delegate_release", map[string]interface{}{"vassal": vassal})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("delegate_release failed: %v", err)), nil
	}

	return mcp.NewToolResultText(string(raw)), nil
}

// registerDelegateStatus adds the delegate_status MCP tool.
func (s *Server) registerDelegateStatus() {
	tool := mcp.NewTool("delegate_status",
		mcp.WithDescription("Show the current delegation mode of this MCP session. "+
			"Reports whether a parent kingdom was detected and which vassals are currently delegated."),
	)
	s.mcpServer.AddTool(tool, s.handleDelegateStatus)
}

func (s *Server) handleDelegateStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result := map[string]interface{}{
		"observer_mode":         s.inObserverMode,
		"parent_kingdom_socket": s.parentKingdomSocket,
		"session_pid":           os.Getpid(),
	}
	b, _ := json.Marshal(result)
	return mcp.NewToolResultText(string(b)), nil
}

// startHeartbeat starts a background goroutine that pings the parent daemon
// every 10 seconds for the given vassal.
// Safe to call multiple times (no-op if already running for this vassal).
func (s *Server) startHeartbeat(vassal string) {
	s.heartbeatMu.Lock()
	defer s.heartbeatMu.Unlock()
	if _, ok := s.activeHeartbeats[vassal]; ok {
		return // already running
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.activeHeartbeats[vassal] = cancel

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sendHeartbeat(vassal)
			}
		}
	}()
}

func (s *Server) sendHeartbeat(vassal string) {
	if s.parentKingdomSocket == "" {
		return
	}
	client, err := newMCPDaemonClient(s.parentKingdomSocket)
	if err != nil {
		return
	}
	defer client.Close()

	raw, err := client.Call("delegate_heartbeat", map[string]interface{}{
		"vassal":      vassal,
		"session_pid": os.Getpid(),
	})
	if err != nil {
		return
	}

	var resp struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return
	}
	if !resp.Acknowledged {
		// Daemon restarted — re-delegate automatically
		s.logger.Info("MCP_HEARTBEAT_LOST", "vassal", vassal, "action", "re-delegating")
		client2, err := newMCPDaemonClient(s.parentKingdomSocket)
		if err != nil {
			return
		}
		defer client2.Close()
		client2.Call("delegate_control", map[string]interface{}{ //nolint:errcheck
			"vassal":      vassal,
			"session_pid": os.Getpid(),
			"force":       false,
		})
	}
}
