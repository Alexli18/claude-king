package daemon_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/daemon"
)

// startTestDaemon starts a full daemon in a temp directory and returns the
// daemon instance and its socket path. It registers a cleanup to stop the
// daemon when the test ends.
//
// Uses os.MkdirTemp("", ...) to get a short path under /tmp, avoiding the
// 104-char Unix socket path limit on macOS.
func startTestDaemon(t *testing.T) (*daemon.Daemon, string) {
	t.Helper()

	// t.TempDir() produces paths under /var/folders/... which are too long for
	// Unix socket paths on macOS (limit: 104 chars). Use /tmp instead.
	rootDir, err := os.MkdirTemp("", "kingtest-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(rootDir) })

	d, err := daemon.NewDaemon(rootDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}

	sockPath := daemon.SocketPathForRoot(rootDir)

	t.Cleanup(func() {
		cancel()
		_ = d.Stop()
	})

	return d, sockPath
}

func TestVassalRegisterRPC(t *testing.T) {
	rootDir := t.TempDir()

	d, err := daemon.NewDaemon(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	// Start a minimal listener for RPC (no full daemon.Start needed).
	sockPath := filepath.Join(rootDir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.ServeConn(conn)
		}
	}()

	// Helper: dial + send one RPC + read response.
	callRPC := func(method string, params interface{}) map[string]interface{} {
		t.Helper()
		conn, err := net.Dial("unix", sockPath)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))

		req := map[string]interface{}{"method": method, "params": params, "id": 1}
		if err := json.NewEncoder(conn).Encode(req); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	// Register a vassal.
	resp := callRPC("vassal.register", map[string]interface{}{
		"name":      "firmware",
		"repo_path": "/tmp/firmware",
		"socket":    "/tmp/firmware.sock",
		"pid":       os.Getpid(),
	})
	if resp["error"] != nil {
		t.Fatalf("vassal.register error: %v", resp["error"])
	}

	// List vassals — firmware should appear.
	resp2 := callRPC("vassal.list", nil)
	if resp2["error"] != nil {
		t.Fatalf("vassal.list error: %v", resp2["error"])
	}

	result, _ := resp2["result"].(map[string]interface{})
	vassals, _ := result["vassals"].([]interface{})
	found := false
	for _, v := range vassals {
		m, _ := v.(map[string]interface{})
		if m["name"] == "firmware" {
			found = true
			if !m["alive"].(bool) {
				t.Error("expected vassal to be alive (same PID)")
			}
		}
	}
	if !found {
		t.Errorf("firmware not found in vassal.list response: %v", resp2)
	}
}

func TestKingdomStatusRPC(t *testing.T) {
	_, sockPath := startTestDaemon(t)

	client, err := daemon.NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	raw, err := client.Call("kingdom.status", nil)
	if err != nil {
		t.Fatalf("kingdom.status: %v", err)
	}

	var resp struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		PID    int    `json:"pid"`
		Root   string `json:"root"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if resp.Status == "" {
		t.Error("expected non-empty Status")
	}
	if resp.PID == 0 {
		t.Error("expected non-zero PID")
	}
}
