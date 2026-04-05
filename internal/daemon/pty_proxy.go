package daemon

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/alexli18/claude-king/internal/mcp"
)

// rpcPTYProxy implements mcp.PTYManager by proxying calls to the daemon
// process over its RPC socket. Used in attach mode where the MCP gateway
// runs in a separate process from the daemon that owns the PTY sessions.
type rpcPTYProxy struct {
	client *Client
}

func newRPCPTYProxy(sockPath string) (*rpcPTYProxy, error) {
	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		return nil, fmt.Errorf("pty proxy: connect to daemon: %w", err)
	}
	return &rpcPTYProxy{client: c}, nil
}

func (p *rpcPTYProxy) ListSessions() []mcp.PTYSessionInfo {
	raw, err := p.client.Call("list_vassals", nil)
	if err != nil {
		return nil
	}

	var resp struct {
		Vassals []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Command string `json:"command"`
			PID     int    `json:"pid"`
		} `json:"vassals"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}

	infos := make([]mcp.PTYSessionInfo, len(resp.Vassals))
	for i, v := range resp.Vassals {
		infos[i] = mcp.PTYSessionInfo{
			Name:    v.Name,
			Status:  v.Status,
			Command: v.Command,
			PID:     v.PID,
		}
	}
	return infos
}

func (p *rpcPTYProxy) GetSession(name string) (mcp.PTYSession, bool) {
	// Check if the session exists by listing all sessions from the daemon.
	sessions := p.ListSessions()
	for _, s := range sessions {
		if s.Name == name {
			return &rpcPTYSessionProxy{client: p.client, name: name}, true
		}
	}
	return nil, false
}

func (p *rpcPTYProxy) Close() error {
	return p.client.Close()
}

// rpcPTYSessionProxy implements mcp.PTYSession by proxying exec_in to the daemon.
type rpcPTYSessionProxy struct {
	client *Client
	name   string
}

func (s *rpcPTYSessionProxy) ExecCommand(command string, timeout time.Duration) (string, int, time.Duration, error) {
	start := time.Now()
	raw, err := s.client.Call("exec_in", map[string]interface{}{
		"target":          s.name,
		"command":         command,
		"timeout_seconds": int(timeout.Seconds()),
	})
	dur := time.Since(start)

	if err != nil {
		return "", -1, dur, err
	}

	var resp struct {
		Output   string `json:"output"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", -1, dur, fmt.Errorf("unmarshal exec_in response: %w", err)
	}

	return resp.Output, resp.ExitCode, dur, nil
}

func (s *rpcPTYSessionProxy) Write(data []byte) (int, error) {
	return 0, fmt.Errorf("write not supported via RPC proxy")
}

func (s *rpcPTYSessionProxy) GetOutput() []byte {
	return nil
}
