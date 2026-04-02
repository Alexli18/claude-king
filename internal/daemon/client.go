package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
)

// Client connects to a running daemon via the Unix Domain Socket.
type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
	nextID  atomic.Int64
}

// NewClient connects to the daemon socket in the given root directory.
// Uses the per-directory hash-based socket path (T042).
func NewClient(rootDir string) (*Client, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	sockPath := SocketPathForRoot(absRoot)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	return &Client{conn: conn, scanner: scanner}, nil
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(method string, params interface{}) (json.RawMessage, error) {
	id := int(c.nextID.Add(1))

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}

	req := RPCRequest{
		Method: method,
		Params: rawParams,
		ID:     id,
	}

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

	var resp RPCResponse
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

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
