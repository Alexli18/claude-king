# Vassal Kingdom Registration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When `king-vassal` starts it discovers all kingdoms, selects one (interactively if multiple), registers itself with King, and `king list` shows vassals nested under their kingdoms.

**Architecture:** `FindAllKingdomSockets` finds all kingdoms via dir-walk + global registry. `vassal.register` RPC lets vassals register with their King (in-memory map). `king list` calls `list_vassals` on each alive King. `DefaultConfig` writes `kingdom.yml` with `vassals: []` and a commented example.

**Tech Stack:** Go 1.22+, `internal/daemon.Client` (existing JSON-RPC client), `internal/registry` (existing global registry), `golang.org/x/sys/unix` or `os.Stdin.Stat()` for isatty.

---

### Task 1: Add `KingdomInfo` + `FindAllKingdomSockets` to discovery

**Files:**
- Modify: `internal/discovery/discovery.go`
- Modify: `internal/discovery/discovery_test.go`

**Step 1: Write the failing test**

Add to `internal/discovery/discovery_test.go`:

```go
func TestFindAllKingdomSockets_MultipleKingdoms(t *testing.T) {
	// Two kingdoms: one in a parent dir, one in current dir
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(filepath.Join(child, ".king"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, ".king"), 0o755); err != nil {
		t.Fatal(err)
	}
	childSock := filepath.Join(child, ".king", "king-aaaaaaaa.sock")
	parentSock := filepath.Join(parent, ".king", "king-bbbbbbbb.sock")
	_ = os.WriteFile(childSock, nil, 0o600)
	_ = os.WriteFile(parentSock, nil, 0o600)

	kingdoms, err := discovery.FindAllKingdomSockets(child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kingdoms) != 2 {
		t.Fatalf("expected 2 kingdoms, got %d: %v", len(kingdoms), kingdoms)
	}
}

func TestFindAllKingdomSockets_None(t *testing.T) {
	root := t.TempDir()
	kingdoms, err := discovery.FindAllKingdomSockets(root)
	if err != discovery.ErrNoKingdom {
		t.Errorf("expected ErrNoKingdom, got err=%v kingdoms=%v", err, kingdoms)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/alex/Desktop/Claude_King
go test ./internal/discovery/... -run TestFindAllKingdomSockets -v
```
Expected: FAIL — `FindAllKingdomSockets` undefined

**Step 3: Add `KingdomInfo` struct and `FindAllKingdomSockets` to `discovery.go`**

Add after the existing `FindKingdomSocket` function:

```go
// KingdomInfo describes a discovered kingdom.
type KingdomInfo struct {
	Name       string
	RootDir    string
	SocketPath string
}

// FindAllKingdomSockets walks from startDir up to / collecting all kingdom
// sockets, then merges in any alive kingdoms from the global registry at
// registryPath (~/.king/registry.json). Deduplicates by RootDir.
// Returns ErrNoKingdom if none found.
func FindAllKingdomSockets(startDir string) ([]KingdomInfo, error) {
	seen := make(map[string]bool)
	var kingdoms []KingdomInfo

	// Walk up the directory tree.
	dir := startDir
	for {
		matches, _ := filepath.Glob(filepath.Join(dir, ".king", "king-*.sock"))
		for _, m := range matches {
			if _, statErr := os.Stat(m); statErr == nil && !seen[dir] {
				seen[dir] = true
				kingdoms = append(kingdoms, KingdomInfo{
					Name:       filepath.Base(dir),
					RootDir:    dir,
					SocketPath: m,
				})
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Merge in global registry entries not already found.
	home, err := os.UserHomeDir()
	if err == nil {
		regPath := filepath.Join(home, ".king", "registry.json")
		if data, err := os.ReadFile(regPath); err == nil {
			var entries map[string]struct {
				Socket string `json:"socket"`
				Name   string `json:"name"`
			}
			if err := json.Unmarshal(data, &entries); err == nil {
				for rootDir, e := range entries {
					if seen[rootDir] {
						continue
					}
					if _, statErr := os.Stat(e.Socket); statErr == nil {
						seen[rootDir] = true
						kingdoms = append(kingdoms, KingdomInfo{
							Name:       e.Name,
							RootDir:    rootDir,
							SocketPath: e.Socket,
						})
					}
				}
			}
		}
	}

	if len(kingdoms) == 0 {
		return nil, ErrNoKingdom
	}
	return kingdoms, nil
}
```

Add `"encoding/json"` to imports in `discovery.go`.

**Step 4: Run test to verify it passes**

```bash
go test ./internal/discovery/... -run TestFindAllKingdomSockets -v
```
Expected: PASS

**Step 5: Run all discovery tests**

```bash
go test ./internal/discovery/... -v
```
Expected: all PASS

**Step 6: Commit**

```bash
git add internal/discovery/discovery.go internal/discovery/discovery_test.go
git commit -m "feat(discovery): add FindAllKingdomSockets with multi-kingdom support"
```

---

### Task 2: Add `vassal.register` RPC to daemon

**Files:**
- Modify: `internal/daemon/daemon.go`
- Create: `internal/daemon/vassal_registry_test.go`

**Step 1: Write the failing test**

Create `internal/daemon/vassal_registry_test.go`:

```go
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

	// Start a minimal server just for RPC (no full daemon.Start needed).
	sockPath := filepath.Join(rootDir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		d.ServeConn(conn)
	}()

	// Connect and call vassal.register.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := map[string]interface{}{
		"method": "vassal.register",
		"params": map[string]interface{}{
			"name":      "firmware",
			"repo_path": "/tmp/firmware",
			"socket":    "/tmp/firmware.sock",
			"pid":       os.Getpid(),
		},
		"id": 1,
	}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(req); err != nil {
		t.Fatal(err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	dec := json.NewDecoder(conn)
	var resp map[string]interface{}
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != nil {
		t.Fatalf("RPC error: %v", resp["error"])
	}

	// Now call list_vassals and check firmware appears.
	req2 := map[string]interface{}{"method": "vassal.list", "params": nil, "id": 2}
	conn2, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	conn2.SetReadDeadline(time.Now().Add(time.Second))
	enc2 := json.NewEncoder(conn2)
	if err := enc2.Encode(req2); err != nil {
		t.Fatal(err)
	}
	var resp2 map[string]interface{}
	if err := json.NewDecoder(conn2).Decode(&resp2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, _ := resp2["result"].(map[string]interface{})
	vassals, _ := result["vassals"].([]interface{})
	found := false
	for _, v := range vassals {
		m, _ := v.(map[string]interface{})
		if m["name"] == "firmware" {
			found = true
		}
	}
	if !found {
		t.Errorf("firmware not found in vassal.list response: %v", resp2)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/daemon/... -run TestVassalRegisterRPC -v
```
Expected: FAIL — `ServeConn` undefined or `vassal.register` not found

**Step 3: Add `externalVassals` to `Daemon` struct in `daemon.go`**

In the `Daemon` struct (around line 85), add:

```go
	externalVassals   map[string]ExternalVassalInfo
	externalVassalsMu sync.RWMutex
```

Add `ExternalVassalInfo` type before the `Daemon` struct:

```go
// ExternalVassalInfo holds metadata about a vassal registered via vassal.register RPC.
// Used for vassals started externally (e.g. via --stdio mode).
type ExternalVassalInfo struct {
	Name     string `json:"name"`
	RepoPath string `json:"repo_path"`
	Socket   string `json:"socket"`
	PID      int    `json:"pid"`
}
```

Initialize in `NewDaemon` (after `vassalProcs: make(...)`):

```go
		externalVassals: make(map[string]ExternalVassalInfo),
```

**Step 4: Add `ServeConn` public method and register new RPC handlers**

Add `ServeConn` (public wrapper for testing) near the existing connection handling:

```go
// ServeConn handles a single daemon RPC connection. Exported for testing.
func (d *Daemon) ServeConn(conn net.Conn) {
	d.handleConn(conn)
}
```

Find the existing `registerRealHandlers` function and add at the end, before the closing `}`:

```go
	// vassal.register — external vassals (e.g. --stdio mode) register themselves.
	d.handlers["vassal.register"] = func(params json.RawMessage) (interface{}, error) {
		var info ExternalVassalInfo
		if err := json.Unmarshal(params, &info); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if info.Name == "" {
			return nil, fmt.Errorf("name is required")
		}
		d.externalVassalsMu.Lock()
		d.externalVassals[info.Name] = info
		d.externalVassalsMu.Unlock()
		d.logger.Info("external vassal registered", "name", info.Name, "repo", info.RepoPath)
		return map[string]bool{"ok": true}, nil
	}

	// vassal.list — returns external vassals with liveness check.
	d.handlers["vassal.list"] = func(_ json.RawMessage) (interface{}, error) {
		d.externalVassalsMu.RLock()
		defer d.externalVassalsMu.RUnlock()
		type vassalEntry struct {
			Name     string `json:"name"`
			RepoPath string `json:"repo_path"`
			Socket   string `json:"socket"`
			PID      int    `json:"pid"`
			Alive    bool   `json:"alive"`
		}
		var result []vassalEntry
		for _, v := range d.externalVassals {
			alive := false
			if proc, err := os.FindProcess(v.PID); err == nil {
				alive = proc.Signal(syscall.Signal(0)) == nil
			}
			result = append(result, vassalEntry{
				Name: v.Name, RepoPath: v.RepoPath,
				Socket: v.Socket, PID: v.PID, Alive: alive,
			})
		}
		return map[string]interface{}{"vassals": result}, nil
	}
```

Also add to `registerStubHandlers` (the two new methods need stub versions too):

```go
	for _, method := range []string{"vassal.register", "vassal.list"} {
		d.handlers[method] = func(_ json.RawMessage) (interface{}, error) {
			return nil, fmt.Errorf("daemon not fully started")
		}
	}
```

**Step 5: Find `handleConn` and verify `ServeConn` compiles**

```bash
grep -n "func.*handleConn" internal/daemon/daemon.go
```

If the private method is named differently (e.g., `serveConn`, `handleConnection`), adjust `ServeConn` accordingly. Then:

```bash
go build ./internal/daemon/... 2>&1
```
Expected: no errors

**Step 6: Run test to verify it passes**

```bash
go test ./internal/daemon/... -run TestVassalRegisterRPC -v
```
Expected: PASS

**Step 7: Run all daemon tests**

```bash
go test ./internal/daemon/... -v -timeout 30s
```
Expected: all PASS

**Step 8: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/vassal_registry_test.go
git commit -m "feat(daemon): add vassal.register and vassal.list RPC for external vassal tracking"
```

---

### Task 3: Update `king list` to show vassals

**Files:**
- Modify: `cmd/king/main.go`

**Step 1: Update `cmdList` function**

Replace the existing `cmdList` function body in `cmd/king/main.go`:

```go
func cmdList() {
	reg := registry.NewRegistry(registryPath())
	entries, err := reg.ListAlive()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading registry: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("No kingdoms registered.")
		return
	}

	for path, e := range entries {
		status := "running"
		if !e.Reachable {
			status = "unreachable"
		}
		fmt.Printf("Kingdom: %s  [%s, pid=%d]\n", path, status, e.PID)

		if !e.Reachable {
			fmt.Println("  vassals: (kingdom unreachable)")
			continue
		}

		// Query vassals from the daemon.
		client, err := daemon.NewClient(path)
		if err != nil {
			fmt.Println("  vassals: (could not connect)")
			continue
		}
		raw, err := client.Call("vassal.list", nil)
		client.Close()
		if err != nil {
			fmt.Println("  vassals: (none)")
			continue
		}
		var resp struct {
			Vassals []struct {
				Name     string `json:"name"`
				RepoPath string `json:"repo_path"`
				Alive    bool   `json:"alive"`
			} `json:"vassals"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Vassals) == 0 {
			fmt.Println("  vassals: (none)")
			continue
		}
		fmt.Println("  vassals:")
		for _, v := range resp.Vassals {
			alive := "alive"
			if !v.Alive {
				alive = "dead"
			}
			fmt.Printf("    %-20s %-40s %s\n", v.Name, v.RepoPath, alive)
		}
	}
}
```

Add `"encoding/json"` to imports if not already present.

**Step 2: Build and verify**

```bash
go build ./cmd/king/... 2>&1
```
Expected: no errors

**Step 3: Commit**

```bash
git add cmd/king/main.go
git commit -m "feat(cmd): king list shows vassals nested under each kingdom"
```

---

### Task 4: Multi-kingdom discovery + registration in `king-vassal`

**Files:**
- Modify: `cmd/king-vassal/main.go`

**Step 1: Replace discovery + add interactive selection + registration**

Replace the existing discovery block in `main.go`:

```go
// Current (remove this):
if !*stdio && *kingSocket == "" {
    sockPath, _, discErr := discovery.FindKingdomSocket(cwd)
    if discErr != nil {
        fmt.Fprintln(os.Stderr, "error: No Kingdom found. Run king up first.")
        os.Exit(1)
    }
    *kingSocket = sockPath
}
```

With:

```go
if *kingSocket == "" {
    kingdoms, discErr := discovery.FindAllKingdomSockets(cwd)
    if discErr != nil {
        fmt.Fprintln(os.Stderr, "error: No Kingdom found. Run king up first.")
        os.Exit(1)
    }
    switch len(kingdoms) {
    case 1:
        *kingSocket = kingdoms[0].SocketPath
    default:
        if *stdio {
            fmt.Fprintf(os.Stderr, "error: multiple kingdoms found, use --king-sock to specify one\n")
            for _, k := range kingdoms {
                fmt.Fprintf(os.Stderr, "  %s (%s)\n", k.Name, k.RootDir)
            }
            os.Exit(1)
        }
        // Interactive selection.
        fi, _ := os.Stdin.Stat()
        if (fi.Mode() & os.ModeCharDevice) == 0 {
            fmt.Fprintln(os.Stderr, "error: multiple kingdoms found but stdin is not a terminal, use --king-sock")
            os.Exit(1)
        }
        fmt.Println("Found multiple kingdoms:")
        for i, k := range kingdoms {
            fmt.Printf("  %d. %-20s (%s)\n", i+1, k.Name, k.RootDir)
        }
        fmt.Printf("\nSelect kingdom [1-%d]: ", len(kingdoms))
        var choice int
        if _, err := fmt.Scan(&choice); err != nil || choice < 1 || choice > len(kingdoms) {
            fmt.Fprintln(os.Stderr, "error: invalid selection")
            os.Exit(1)
        }
        *kingSocket = kingdoms[choice-1].SocketPath
    }
}
```

**Step 2: Add vassal self-registration after server creation**

After the existing `srv := vassal.NewVassalServer(...)` line, add:

```go
// Register with King daemon (best-effort, non-fatal).
if *kingSocket != "" {
    if client, err := daemon.NewClientFromSocket(*kingSocket); err == nil {
        _ = client.Call("vassal.register", map[string]interface{}{
            "name":      *name,
            "repo_path": *repoPath,
            "socket":    filepath.Join(*kingDir, "vassals", *name+".sock"),
            "pid":       os.Getpid(),
        })
        client.Close()
    }
}
```

**Step 3: Add `NewClientFromSocket` to `internal/daemon/client.go`**

Add after `NewClient`:

```go
// NewClientFromSocket connects to a daemon via a known socket path.
func NewClientFromSocket(sockPath string) (*Client, error) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon socket: %w", err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Client{conn: conn, scanner: scanner}, nil
}
```

Add import `"github.com/alexli18/claude-king/internal/daemon"` to `cmd/king-vassal/main.go` imports.

**Step 4: Build**

```bash
go build ./cmd/king-vassal/... 2>&1
```
Expected: no errors

**Step 5: Smoke test — single kingdom**

```bash
# With no kingdom running, should fail gracefully:
./king-vassal --stdio 2>&1 | head -3
```
Expected: `error: No Kingdom found...`

**Step 6: Run all tests**

```bash
go test ./... 2>&1
```
Expected: all PASS

**Step 7: Commit**

```bash
git add cmd/king-vassal/main.go internal/daemon/client.go
git commit -m "feat(vassal): multi-kingdom discovery, interactive selection, and self-registration with King"
```

---

### Task 5: Update `DefaultConfig` to write empty `vassals: []` with commented example

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Write the failing test**

Add to a new test or existing config test file. Check if `config_test.go` exists:

```bash
ls /Users/alex/Desktop/Claude_King/internal/config/
```

If no test file exists, create `internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/config"
)

func TestLoadOrCreateConfig_WritesEmptyVassals(t *testing.T) {
	rootDir := t.TempDir()

	cfg, err := config.LoadOrCreateConfig(rootDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Vassals) != 0 {
		t.Errorf("expected 0 vassals, got %d", len(cfg.Vassals))
	}

	// Check the written file contains "vassals: []" and a comment.
	data, err := os.ReadFile(filepath.Join(rootDir, ".king", "kingdom.yml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "vassals: []") {
		t.Errorf("expected 'vassals: []' in file, got:\n%s", content)
	}
	if !strings.Contains(content, "# Example") {
		t.Errorf("expected comment example in file, got:\n%s", content)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/config/... -run TestLoadOrCreateConfig_WritesEmptyVassals -v
```
Expected: FAIL — file contains default shell vassal, not `vassals: []`

**Step 3: Rewrite `LoadOrCreateConfig` to use a string template**

Replace the `LoadOrCreateConfig` function's "create" path in `config.go`. Change from using `yaml.Marshal(cfg)` to writing a string template:

```go
// Config doesn't exist; create a default one.
if err := EnsureKingDir(rootDir); err != nil {
    return nil, fmt.Errorf("creating .king directory: %w", err)
}

dirName := filepath.Base(rootDir)
template := "name: " + dirName + `
vassals: []
# Example vassal:
# vassals:
#   - name: shell
#     command: $SHELL
#     autostart: true
patterns:
  - name: generic-error
    regex: '(?i)error|FAIL|panic:'
    severity: error
    summary_template: "Error detected in {vassal}: {match}"
settings:
  log_retention_days: 7
  max_output_buffer: 10MB
  event_cooldown_seconds: 30
  audit_retention_days: 7
  audit_ingestion_retention_days: 1
  sovereign_approval_timeout: 300
  audit_max_trace_output: 10000
`

if err := os.WriteFile(configPath, []byte(template), 0644); err != nil {
    return nil, fmt.Errorf("writing default config: %w", err)
}

return LoadConfig(configPath)
```

Also update `DefaultConfig` to return empty vassals (used elsewhere):

```go
func DefaultConfig(dirName string) *KingdomConfig {
	return &KingdomConfig{
		Name:    dirName,
		Vassals: []VassalConfig{},
		Patterns: []PatternConfig{
			{
				Name:            "generic-error",
				Regex:           `(?i)error|FAIL|panic:`,
				Severity:        "error",
				SummaryTemplate: "Error detected in {vassal}: {match}",
			},
		},
		Settings: DefaultSettings(),
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/config/... -run TestLoadOrCreateConfig_WritesEmptyVassals -v
```
Expected: PASS

**Step 5: Run all tests**

```bash
go test ./... 2>&1
```
Expected: all PASS

**Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): default kingdom.yml uses vassals: [] with commented example"
```
