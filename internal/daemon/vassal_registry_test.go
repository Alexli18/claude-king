package daemon_test

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/daemon"
)

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
