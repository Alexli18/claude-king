package mcp

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeDaemonRequest mirrors the wire format used by daemonClient.Call.
type fakeDaemonRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     int             `json:"id"`
}

// fakeDaemonResponse mirrors the wire format returned to daemonClient.Call.
type fakeDaemonResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	ID int `json:"id"`
}

// HandlerFunc is called by the fake daemon for each incoming RPC method.
// Return (result, "") for success or ("", errMsg) for an RPC error.
type HandlerFunc func(params json.RawMessage) (result interface{}, errMsg string)

// startFakeDaemon creates a Unix socket in a temp directory, starts accepting
// connections in a background goroutine, and returns the socket path.
// For each incoming connection it reads newline-delimited JSON-RPC requests
// and dispatches them to the handlers map. Unknown methods return an error.
// The listener is closed automatically when the test finishes.
func startFakeDaemon(t *testing.T, handlers map[string]HandlerFunc) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "fake-daemon-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "king.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go serveFakeConn(conn, handlers)
		}
	}()

	return sockPath
}

// serveFakeConn handles one client connection on the fake daemon.
func serveFakeConn(conn net.Conn, handlers map[string]HandlerFunc) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(conn)

	for scanner.Scan() {
		var req fakeDaemonRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return
		}

		resp := fakeDaemonResponse{ID: req.ID}

		handler, ok := handlers[req.Method]
		if !ok {
			resp.Error = &struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}{Code: -32601, Message: "method not found: " + req.Method}
		} else {
			result, errMsg := handler(req.Params)
			if errMsg != "" {
				resp.Error = &struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}{Code: -32000, Message: errMsg}
			} else {
				raw, _ := json.Marshal(result)
				resp.Result = raw
			}
		}

		_ = enc.Encode(resp)
	}
}
