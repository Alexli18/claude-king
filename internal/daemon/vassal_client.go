package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// MCP JSON-RPC types for vassal communication
// ---------------------------------------------------------------------------

// mcpRequest is a minimal MCP JSON-RPC 2.0 request envelope.
type mcpRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Method  string         `json:"method"`
	Params  mcpToolParams  `json:"params"`
}

// mcpToolParams holds the parameters for the tools/call method.
type mcpToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// mcpResponse is a minimal MCP JSON-RPC 2.0 response envelope.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  *mcpToolResult  `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

// mcpToolResult holds the result of a tools/call response.
type mcpToolResult struct {
	Content []mcpContent `json:"content"`
}

// mcpContent is a single content item in a tool result.
type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// mcpRPCError represents a JSON-RPC error object from an MCP server.
type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// VassalClient
// ---------------------------------------------------------------------------

// VassalClient connects to a king-vassal MCP server over a Unix socket and
// allows the King daemon to call tools exposed by that vassal.
type VassalClient struct {
	name     string
	sockPath string
	conn     net.Conn
	reader   *bufio.Reader
	encoder  *json.Encoder
	mu       sync.Mutex
	nextID   atomic.Int64
}

// DefaultVassalTimeout is the fallback read deadline when the caller's context
// has no explicit deadline. Exported so MCP handlers can reference the value.
const DefaultVassalTimeout = 30 * time.Second

// readResult carries the outcome of a blocking ReadBytes call from a goroutine.
type readResult struct {
	line []byte
	err  error
}

// CallTool sends a tools/call JSON-RPC request to the vassal MCP server and
// returns the text content of the first result item.
//
// The mutex is held for the entire request/response cycle to serialise
// concurrent callers and prevent interleaved writes/reads on the connection.
// The blocking read is wrapped in a goroutine so the call can be cancelled
// via context — this prevents a stuck vassal from blocking the gateway.
func (vc *VassalClient) CallTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("vassal_client context: %w", err)
	}

	id := vc.nextID.Add(1)

	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params: mcpToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	// Write the request as a single JSON line.
	if err := vc.encoder.Encode(req); err != nil {
		return "", fmt.Errorf("vassal_client write request: %w", err)
	}

	// Set a read deadline on the socket so ReadBytes doesn't block forever
	// even if the context is not cancelled. Use the context deadline if
	// available, otherwise fall back to DefaultVassalTimeout.
	if deadline, ok := ctx.Deadline(); ok {
		_ = vc.conn.SetReadDeadline(deadline)
	} else {
		_ = vc.conn.SetReadDeadline(time.Now().Add(DefaultVassalTimeout))
	}
	defer vc.conn.SetReadDeadline(time.Time{}) //nolint:errcheck

	// Read in a goroutine so we can select on context cancellation.
	ch := make(chan readResult, 1)
	go func() {
		line, err := vc.reader.ReadBytes('\n')
		ch <- readResult{line, err}
	}()

	var line []byte
	select {
	case <-ctx.Done():
		// Force-unblock the reader by setting a past deadline.
		_ = vc.conn.SetReadDeadline(time.Now())
		<-ch // drain goroutine
		return "", fmt.Errorf("vassal %q: %w", vc.name, ctx.Err())
	case res := <-ch:
		if res.err != nil {
			if netErr, ok := res.err.(net.Error); ok && netErr.Timeout() {
				return "", fmt.Errorf("vassal %q timed out after %s", vc.name, DefaultVassalTimeout)
			}
			return "", fmt.Errorf("vassal_client read response: %w", res.err)
		}
		line = res.line
	}

	var resp mcpResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return "", fmt.Errorf("vassal_client unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("vassal_client rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if resp.Result == nil || len(resp.Result.Content) == 0 {
		return "", fmt.Errorf("vassal_client empty result")
	}

	return resp.Result.Content[0].Text, nil
}

// Close closes the underlying network connection to the vassal MCP server.
func (vc *VassalClient) Close() error {
	return vc.conn.Close()
}

// ---------------------------------------------------------------------------
// VassalClientPool
// ---------------------------------------------------------------------------

// VassalClientPool manages a set of VassalClient connections keyed by vassal
// name. It is safe for concurrent use.
type VassalClientPool struct {
	mu      sync.RWMutex
	clients map[string]*VassalClient
}

// NewVassalClientPool creates an empty VassalClientPool.
func NewVassalClientPool() *VassalClientPool {
	return &VassalClientPool{
		clients: make(map[string]*VassalClient),
	}
}

// Connect dials a Unix socket at sockPath and registers a new VassalClient
// under the given name. If a client with that name already exists, it is
// closed and replaced (idempotent reconnect).
func (p *VassalClientPool) Connect(name, sockPath string) (*VassalClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close stale client if present (e.g. vassal process was restarted).
	if old, exists := p.clients[name]; exists {
		_ = old.Close()
		delete(p.clients, name)
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("vassal_client_pool dial %q: %w", sockPath, err)
	}

	vc := &VassalClient{
		name:     name,
		sockPath: sockPath,
		conn:     conn,
		reader:   bufio.NewReader(conn),
		encoder:  json.NewEncoder(conn),
	}

	p.clients[name] = vc
	return vc, nil
}

// Get returns the VassalClient registered under name, or (nil, false) if none
// exists.
func (p *VassalClientPool) Get(name string) (*VassalClient, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	vc, ok := p.clients[name]
	return vc, ok
}

// Names returns a sorted list of all connected vassal names.
func (p *VassalClientPool) Names() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.clients))
	for name := range p.clients {
		names = append(names, name)
	}
	return names
}

// Disconnect closes the connection for the named vassal and removes it from
// the pool. It returns an error if no client with that name is registered.
func (p *VassalClientPool) Disconnect(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	vc, ok := p.clients[name]
	if !ok {
		return fmt.Errorf("vassal_client_pool: no client for %q", name)
	}

	delete(p.clients, name)

	if err := vc.Close(); err != nil {
		return fmt.Errorf("vassal_client_pool close %q: %w", name, err)
	}
	return nil
}

// DisconnectAll closes and removes every client in the pool. Errors are
// silently discarded to ensure all clients are attempted.
func (p *VassalClientPool) DisconnectAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, vc := range p.clients {
		_ = vc.Close()
		delete(p.clients, name)
	}
}
