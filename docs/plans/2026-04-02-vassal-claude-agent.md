# Vassal-as-Claude-Agent Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable King to dispatch structured tasks to Claude Code vassal agents via bidirectional MCP, with `king-vassal` as the adapter between King and headless Claude Code.

**Architecture:** Each `type: "claude"` vassal spawns a `king-vassal` sidecar process that exposes an MCP server (`dispatch_task`, `get_task_status`, `abort_task`) to King. `king-vassal` launches Claude Code headless with a generated `VASSAL.md` context file, then connects back to King daemon to register artifacts. King daemon maintains a pool of MCP clients to each running vassal.

**Tech Stack:** Go 1.22+, `mark3labs/mcp-go`, `os/exec`, Unix Domain Sockets, `claude -p` headless mode.

---

### Task 1: Add `Type` and `Host` fields to `VassalConfig`

**Files:**
- Modify: `internal/config/types.go`

**Step 1: Add fields to VassalConfig**

In `internal/config/types.go`, add two new fields to `VassalConfig`:

```go
type VassalConfig struct {
    Name          string            `yaml:"name"`
    Type          string            `yaml:"type"`            // "shell" (default) | "claude"
    Command       string            `yaml:"command"`
    Cwd           string            `yaml:"cwd"`
    RepoPath      string            `yaml:"repo_path"`
    Host          string            `yaml:"host"`            // SSH host for remote vassals (phase 2, ignored in MVP)
    SSHUser       string            `yaml:"ssh_user"`        // SSH user (phase 2, ignored in MVP)
    Env           map[string]string `yaml:"env"`
    Autostart     *bool             `yaml:"autostart"`
    RestartPolicy string            `yaml:"restart_policy"`
}
```

Add a helper method:

```go
// TypeOrDefault returns "shell" if Type is empty.
func (vc *VassalConfig) TypeOrDefault() string {
    if vc.Type == "" {
        return "shell"
    }
    return vc.Type
}
```

**Step 2: Build to verify**

```bash
cd /Users/alex/Desktop/Claude_King
go build ./...
```
Expected: no errors.

**Step 3: Commit**

```bash
git add internal/config/types.go
git commit -m "feat: add Type and Host fields to VassalConfig"
```

---

### Task 2: Task state files — define structs and read/write helpers

**Files:**
- Create: `internal/vassal/task.go`

**Step 1: Create the package and file**

```go
package vassal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// TaskStatus represents the lifecycle state of a dispatched task.
type TaskStatus string

const (
	TaskStatusAccepted TaskStatus = "accepted"
	TaskStatusRunning  TaskStatus = "running"
	TaskStatusDone     TaskStatus = "done"
	TaskStatusFailed   TaskStatus = "failed"
	TaskStatusTimeout  TaskStatus = "timeout"
	TaskStatusAborted  TaskStatus = "aborted"
)

// Task represents a unit of work dispatched to a vassal.
type Task struct {
	ID         string            `json:"id"`
	VassalName string            `json:"vassal_name"`
	Task       string            `json:"task"`
	Context    map[string]any    `json:"context,omitempty"`
	Status     TaskStatus        `json:"status"`
	Output     string            `json:"output,omitempty"`
	Artifacts  []string          `json:"artifacts,omitempty"`
	Error      string            `json:"error,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// NewTask creates a new Task with a generated ID and accepted status.
func NewTask(vassalName, taskDesc string, context map[string]any) *Task {
	return &Task{
		ID:         "t-" + uuid.New().String()[:8],
		VassalName: vassalName,
		Task:       taskDesc,
		Context:    context,
		Status:     TaskStatusAccepted,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// taskPath returns the path for a task JSON file.
func taskPath(kingDir, taskID string) string {
	return filepath.Join(kingDir, "tasks", taskID+".json")
}

// SaveTask writes a Task to .king/tasks/<id>.json.
func SaveTask(kingDir string, t *Task) error {
	dir := filepath.Join(kingDir, "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create tasks dir: %w", err)
	}
	t.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	return os.WriteFile(taskPath(kingDir, t.ID), data, 0o644)
}

// LoadTask reads a Task from .king/tasks/<id>.json.
func LoadTask(kingDir, taskID string) (*Task, error) {
	data, err := os.ReadFile(taskPath(kingDir, taskID))
	if err != nil {
		return nil, fmt.Errorf("read task file: %w", err)
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse task file: %w", err)
	}
	return &t, nil
}
```

**Step 2: Build**

```bash
go build ./...
```
Expected: no errors.

**Step 3: Commit**

```bash
git add internal/vassal/task.go
git commit -m "feat: add vassal task state structs and file helpers"
```

---

### Task 3: Generate `VASSAL.md` context file

**Files:**
- Create: `internal/vassal/context.go`

**Step 1: Create the file**

```go
package vassal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArtifactRef is a resolved artifact for context injection.
type ArtifactRef struct {
	Name     string
	FilePath string
}

// WriteVassalMD generates VASSAL.md in repoPath with task context.
// This file is read by Claude Code to understand its role and task.
func WriteVassalMD(repoPath, vassalName string, t *Task, artifacts []ArtifactRef) error {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# You are a Vassal: %s\n\n", vassalName))
	sb.WriteString("Your King has assigned you a task. Work autonomously, then report completion.\n\n")
	sb.WriteString("## Task\n\n")
	sb.WriteString(t.Task + "\n\n")

	if len(artifacts) > 0 {
		sb.WriteString("## Available artifacts from other vassals\n\n")
		for _, a := range artifacts {
			sb.WriteString(fmt.Sprintf("- **%s** → `%s`\n", a.Name, a.FilePath))
		}
		sb.WriteString("\n")
	}

	if notes, ok := t.Context["notes"].(string); ok && notes != "" {
		sb.WriteString("## Notes from King\n\n")
		sb.WriteString(notes + "\n\n")
	}

	sb.WriteString("## When done\n\n")
	sb.WriteString(fmt.Sprintf("Run: `kingctl report-done --task %s`\n", t.ID))
	sb.WriteString("(This signals King that the task is complete.)\n")

	return os.WriteFile(filepath.Join(repoPath, "VASSAL.md"), []byte(sb.String()), 0o644)
}
```

**Step 2: Build**

```bash
go build ./...
```
Expected: no errors.

**Step 3: Commit**

```bash
git add internal/vassal/context.go
git commit -m "feat: add VASSAL.md generator for Claude Code context injection"
```

---

### Task 4: `king-vassal` binary — MCP server skeleton

**Files:**
- Create: `cmd/king-vassal/main.go`

**Step 1: Create the binary entry point**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexli18/claude-king/internal/vassal"
)

func main() {
	name := flag.String("name", "", "vassal name (required)")
	repoPath := flag.String("repo", "", "path to vassal repo (required)")
	kingDir := flag.String("king-dir", ".king", "path to .king directory")
	kingSocket := flag.String("king-sock", "", "path to king daemon socket (required)")
	timeoutMin := flag.Int("timeout", 10, "task timeout in minutes")
	flag.Parse()

	if *name == "" || *repoPath == "" || *kingSocket == "" {
		fmt.Fprintln(os.Stderr, "error: --name, --repo, and --king-sock are required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "king-vassal", "name", *name)

	srv := vassal.NewVassalServer(*name, *repoPath, *kingDir, *kingSocket, *timeoutMin, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	sockPath, err := srv.Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting vassal server: %v\n", err)
		os.Exit(1)
	}
	logger.Info("vassal MCP server started", "socket", sockPath)

	select {
	case <-sigCh:
		logger.Info("shutting down")
	case <-ctx.Done():
	}
}
```

**Step 2: Create `internal/vassal/server.go` stub**

```go
package vassal

import (
	"context"
	"log/slog"
)

// VassalServer is an MCP server that manages a single Claude Code vassal.
type VassalServer struct {
	name       string
	repoPath   string
	kingDir    string
	kingSocket string
	timeoutMin int
	logger     *slog.Logger
}

// NewVassalServer creates a new VassalServer.
func NewVassalServer(name, repoPath, kingDir, kingSocket string, timeoutMin int, logger *slog.Logger) *VassalServer {
	return &VassalServer{
		name:       name,
		repoPath:   repoPath,
		kingDir:    kingDir,
		kingSocket: kingSocket,
		timeoutMin: timeoutMin,
		logger:     logger,
	}
}

// Start starts the MCP server and returns its socket path.
// Stub implementation — returns immediately.
func (s *VassalServer) Start(_ context.Context) (string, error) {
	return "", nil
}
```

**Step 3: Build**

```bash
go build ./cmd/king-vassal/
```
Expected: no errors.

**Step 4: Commit**

```bash
git add cmd/king-vassal/main.go internal/vassal/server.go
git commit -m "feat: add king-vassal binary skeleton and VassalServer stub"
```

---

### Task 5: `king-vassal` MCP server — implement `dispatch_task`, `get_task_status`, `abort_task`

**Files:**
- Modify: `internal/vassal/server.go`

**Step 1: Replace stub `Start()` with real MCP server**

Replace the entire `internal/vassal/server.go` with:

```go
package vassal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// VassalServer is an MCP server that manages a single Claude Code vassal.
type VassalServer struct {
	name       string
	repoPath   string
	kingDir    string
	kingSocket string
	timeoutMin int
	logger     *slog.Logger

	mu          sync.Mutex
	activeTask  *Task
	mcpServer   *server.MCPServer
}

// NewVassalServer creates a new VassalServer.
func NewVassalServer(name, repoPath, kingDir, kingSocket string, timeoutMin int, logger *slog.Logger) *VassalServer {
	return &VassalServer{
		name:       name,
		repoPath:   repoPath,
		kingDir:    kingDir,
		kingSocket: kingSocket,
		timeoutMin: timeoutMin,
		logger:     logger,
	}
}

// Start starts the MCP server on a Unix socket and returns its socket path.
func (s *VassalServer) Start(ctx context.Context) (string, error) {
	sockPath := filepath.Join(s.kingDir, "vassals", s.name+".sock")
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return "", fmt.Errorf("create vassals dir: %w", err)
	}
	// Remove stale socket.
	_ = os.Remove(sockPath)

	s.mcpServer = server.NewMCPServer("king-vassal-"+s.name, "1.0.0")
	s.registerTools()

	go func() {
		if err := server.ServeUnix(s.mcpServer, sockPath); err != nil && ctx.Err() == nil {
			s.logger.Error("MCP server error", "err", err)
		}
	}()

	return sockPath, nil
}

// registerTools registers MCP tools on the server.
func (s *VassalServer) registerTools() {
	// dispatch_task
	s.mcpServer.AddTool(mcp.NewTool("dispatch_task",
		mcp.WithDescription("Dispatch a task to this Claude Code vassal"),
		mcp.WithString("task", mcp.Required(), mcp.Description("Task description")),
		mcp.WithObject("context", mcp.Description("Optional context: artifacts list, notes")),
	), s.handleDispatchTask)

	// get_task_status
	s.mcpServer.AddTool(mcp.NewTool("get_task_status",
		mcp.WithDescription("Get the status of a dispatched task"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID returned by dispatch_task")),
	), s.handleGetTaskStatus)

	// abort_task
	s.mcpServer.AddTool(mcp.NewTool("abort_task",
		mcp.WithDescription("Abort a running task"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID to abort")),
	), s.handleAbortTask)
}

func (s *VassalServer) handleDispatchTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskDesc, _ := req.Params.Arguments["task"].(string)
	if taskDesc == "" {
		return mcp.NewToolResultError("task is required"), nil
	}

	var ctx map[string]any
	if raw, ok := req.Params.Arguments["context"]; ok && raw != nil {
		if m, ok := raw.(map[string]any); ok {
			ctx = m
		}
	}

	s.mu.Lock()
	if s.activeTask != nil && (s.activeTask.Status == TaskStatusAccepted || s.activeTask.Status == TaskStatusRunning) {
		s.mu.Unlock()
		return mcp.NewToolResultError(fmt.Sprintf("vassal busy with task %s (status: %s)", s.activeTask.ID, s.activeTask.Status)), nil
	}

	t := NewTask(s.name, taskDesc, ctx)
	s.activeTask = t
	s.mu.Unlock()

	if err := SaveTask(s.kingDir, t); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save task: %v", err)), nil
	}

	// Launch Claude Code asynchronously.
	go s.runTask(t)

	result, _ := json.Marshal(map[string]string{
		"task_id": t.ID,
		"status":  string(t.Status),
	})
	return mcp.NewToolResultText(string(result)), nil
}

func (s *VassalServer) handleGetTaskStatus(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, _ := req.Params.Arguments["task_id"].(string)
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	t, err := LoadTask(s.kingDir, taskID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("task not found: %v", err)), nil
	}

	result, _ := json.Marshal(map[string]any{
		"task_id":   t.ID,
		"status":    t.Status,
		"output":    t.Output,
		"artifacts": t.Artifacts,
		"error":     t.Error,
		"created_at": t.CreatedAt.Format(time.RFC3339),
		"updated_at": t.UpdatedAt.Format(time.RFC3339),
	})
	return mcp.NewToolResultText(string(result)), nil
}

func (s *VassalServer) handleAbortTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, _ := req.Params.Arguments["task_id"].(string)
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	t, err := LoadTask(s.kingDir, taskID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("task not found: %v", err)), nil
	}

	if t.Status != TaskStatusAccepted && t.Status != TaskStatusRunning {
		result, _ := json.Marshal(map[string]string{"status": string(t.Status), "message": "task already finished"})
		return mcp.NewToolResultText(string(result)), nil
	}

	t.Status = TaskStatusAborted
	_ = SaveTask(s.kingDir, t)

	s.mu.Lock()
	if s.activeTask != nil && s.activeTask.ID == taskID {
		s.activeTask = nil
	}
	s.mu.Unlock()

	result, _ := json.Marshal(map[string]string{"task_id": taskID, "status": "aborted"})
	return mcp.NewToolResultText(string(result)), nil
}
```

**Step 2: Build**

```bash
go build ./...
```
Expected: no errors.

**Step 3: Commit**

```bash
git add internal/vassal/server.go
git commit -m "feat: implement dispatch_task, get_task_status, abort_task MCP tools in king-vassal"
```

---

### Task 6: `king-vassal` — run Claude Code headless (`runTask`)

**Files:**
- Modify: `internal/vassal/server.go` (add `runTask` method)

**Step 1: Add `runTask` method to `VassalServer`**

Add this method to the bottom of `internal/vassal/server.go`:

```go
// runTask generates VASSAL.md, launches claude headless, and updates task state.
func (s *VassalServer) runTask(t *Task) {
	s.logger.Info("running task", "task_id", t.ID, "task", t.Task)

	// Mark as running.
	t.Status = TaskStatusRunning
	_ = SaveTask(s.kingDir, t)

	// Write VASSAL.md — Claude Code reads this as context.
	// Artifact resolution is best-effort (no artifacts in MVP).
	if err := WriteVassalMD(s.repoPath, s.name, t, nil); err != nil {
		s.logger.Warn("could not write VASSAL.md", "err", err)
	}

	// Build the prompt: task description + reference to VASSAL.md.
	prompt := fmt.Sprintf("%s\n\n(See VASSAL.md in this directory for your role and context.)", t.Task)

	// Run claude headless with a timeout.
	timeout := time.Duration(s.timeoutMin) * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "text",
	)
	cmd.Dir = s.repoPath

	out, err := cmd.Output()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if aborted while claude was running.
	current, loadErr := LoadTask(s.kingDir, t.ID)
	if loadErr == nil && current.Status == TaskStatusAborted {
		s.activeTask = nil
		return
	}

	if ctx.Err() == context.DeadlineExceeded {
		t.Status = TaskStatusTimeout
		t.Error = fmt.Sprintf("task exceeded %d minute timeout", s.timeoutMin)
	} else if err != nil {
		t.Status = TaskStatusFailed
		t.Error = err.Error()
		t.Output = string(out)
	} else {
		t.Status = TaskStatusDone
		t.Output = string(out)
	}

	_ = SaveTask(s.kingDir, t)
	s.activeTask = nil
	s.logger.Info("task finished", "task_id", t.ID, "status", t.Status)
}
```

**Step 2: Add `os/exec` and `context` to imports in `server.go`**

Add to the import block:
```go
"os/exec"
```
(`context` is already imported.)

**Step 3: Build**

```bash
go build ./...
```
Expected: no errors.

**Step 4: Commit**

```bash
git add internal/vassal/server.go
git commit -m "feat: implement runTask — launches claude headless with VASSAL.md context"
```

---

### Task 7: King daemon — vassal MCP client pool

**Files:**
- Create: `internal/daemon/vassal_client.go`

**Step 1: Create the file**

```go
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"time"
)

// vassalRPCResult holds a parsed response from a vassal MCP call.
type vassalRPCResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// VassalClientPool maintains one MCP connection per active claude-type vassal.
type VassalClientPool struct {
	mu      sync.RWMutex
	clients map[string]*vassalClient // key: vassal name
	kingDir string
	logger  *slog.Logger
}

type vassalClient struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
	nextID  int
	mu      sync.Mutex
}

// NewVassalClientPool creates an empty client pool.
func NewVassalClientPool(kingDir string, logger *slog.Logger) *VassalClientPool {
	return &VassalClientPool{
		clients: make(map[string]*vassalClient),
		kingDir: kingDir,
		logger:  logger,
	}
}

// sockPath returns the socket path for a vassal by name.
func (p *VassalClientPool) sockPath(name string) string {
	return filepath.Join(p.kingDir, "vassals", name+".sock")
}

// Connect connects to a vassal's MCP socket. Retries up to 1s.
func (p *VassalClientPool) Connect(name string) error {
	sockPath := p.sockPath(name)
	var conn net.Conn
	var err error
	for i := 0; i < 20; i++ {
		conn, err = net.Dial("unix", sockPath)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("connect to vassal %s: %w", name, err)
	}

	p.mu.Lock()
	p.clients[name] = &vassalClient{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
	}
	p.mu.Unlock()
	return nil
}

// Disconnect removes and closes a vassal connection.
func (p *VassalClientPool) Disconnect(name string) {
	p.mu.Lock()
	c, ok := p.clients[name]
	if ok {
		_ = c.conn.Close()
		delete(p.clients, name)
	}
	p.mu.Unlock()
}

// CallTool calls an MCP tool on a vassal and returns the raw JSON text result.
func (p *VassalClientPool) CallTool(ctx context.Context, vassalName, tool string, args map[string]any) (string, error) {
	p.mu.RLock()
	c, ok := p.clients[vassalName]
	p.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("vassal %q not connected", vassalName)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := c.nextID

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": args,
		},
	}
	if err := c.encoder.Encode(req); err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}

	var resp struct {
		Result vassalRPCResult `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.decoder.Decode(&resp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("vassal error: %s", resp.Error.Message)
	}
	if len(resp.Result.Content) == 0 {
		return "", nil
	}
	return resp.Result.Content[0].Text, nil
}
```

**Step 2: Build**

```bash
go build ./...
```
Expected: no errors.

**Step 3: Commit**

```bash
git add internal/daemon/vassal_client.go
git commit -m "feat: add VassalClientPool for MCP connections to claude vassals"
```

---

### Task 8: Wire `king-vassal` launch in `daemon.go` for `type: "claude"` vassals

**Files:**
- Modify: `internal/daemon/daemon.go`

**Step 1: Add `vassalPool` field to `Daemon` struct**

In `internal/daemon/daemon.go`, find the `Daemon` struct and add:

```go
vassalPool    *VassalClientPool
vassalProcs   map[string]*exec.Cmd  // running king-vassal processes
```

Also add `"os/exec"` to imports if not present.

**Step 2: Initialize `vassalPool` in `NewDaemon`**

In `NewDaemon`, after creating the `Daemon` struct, add:

```go
d.vassalPool = NewVassalClientPool(filepath.Join(absRoot, ".king"), logger)
d.vassalProcs = make(map[string]*exec.Cmd)
```

**Step 3: In `startVassals()`, handle `type: "claude"` vassals**

Find `startVassals()` in `daemon.go`. Inside the loop over `d.config.Vassals`, add a branch before the existing `pty.CreateSession` call:

```go
for _, vc := range d.config.Vassals {
    if !vc.AutostartOrDefault() {
        continue
    }

    if vc.TypeOrDefault() == "claude" {
        if err := d.startClaudeVassal(vc); err != nil {
            d.logger.Error("failed to start claude vassal", "name", vc.Name, "err", err)
        }
        continue
    }

    // existing shell vassal code below...
```

**Step 4: Add `startClaudeVassal` method**

Add this method to `daemon.go`:

```go
// startClaudeVassal launches a king-vassal sidecar for a claude-type vassal.
func (d *Daemon) startClaudeVassal(vc config.VassalConfig) error {
	exe, err := exec.LookPath("king-vassal")
	if err != nil {
		// Fall back to binary in same dir as king.
		selfExe, _ := os.Executable()
		exe = filepath.Join(filepath.Dir(selfExe), "king-vassal")
	}

	kingDir := filepath.Join(d.rootDir, ".king")
	repoPath := vc.RepoPath
	if !filepath.IsAbs(repoPath) {
		repoPath = filepath.Join(d.rootDir, repoPath)
	}

	cmd := exec.Command(exe,
		"--name", vc.Name,
		"--repo", repoPath,
		"--king-dir", kingDir,
		"--king-sock", d.sockPath,
	)
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start king-vassal %s: %w", vc.Name, err)
	}

	d.vassalProcs[vc.Name] = cmd
	d.logger.Info("claude vassal started", "name", vc.Name, "pid", cmd.Process.Pid)

	// Connect pool to vassal MCP socket (polls up to 1s).
	if err := d.vassalPool.Connect(vc.Name); err != nil {
		d.logger.Warn("could not connect to claude vassal MCP", "name", vc.Name, "err", err)
	}

	return nil
}
```

**Step 5: Stop claude vassals in `Stop()`**

Find the `Stop()` method in `daemon.go`. Before or after stopping PTY sessions, add:

```go
// Stop claude vassal processes.
for name, cmd := range d.vassalProcs {
    d.vassalPool.Disconnect(name)
    if cmd.Process != nil {
        _ = cmd.Process.Signal(syscall.SIGTERM)
    }
}
```

**Step 6: Build**

```bash
go build ./...
```
Expected: no errors.

**Step 7: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "feat: launch king-vassal sidecar for claude-type vassals in daemon"
```

---

### Task 9: Add `dispatch_task`, `get_task_status`, `abort_task` to King MCP tools

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/tools.go`

**Step 1: Add `VassalPool` interface to `server.go`**

In `internal/mcp/server.go`, add a new interface:

```go
// VassalPool allows dispatching tasks to claude-type vassals.
type VassalPool interface {
	CallTool(ctx context.Context, vassalName, tool string, args map[string]any) (string, error)
}
```

Add `vassalPool VassalPool` field to the `Server` struct.

Add a setter method:

```go
// SetVassalPool sets the vassal client pool on the MCP server.
func (s *Server) SetVassalPool(p VassalPool) {
	s.vassalPool = p
}
```

Register the new tools in `RegisterTools()`:

```go
s.mcpServer.AddTool(mcp.NewTool("dispatch_task",
    mcp.WithDescription("Dispatch a task to a Claude Code vassal agent"),
    mcp.WithString("vassal", mcp.Required(), mcp.Description("Vassal name")),
    mcp.WithString("task", mcp.Required(), mcp.Description("Task description")),
    mcp.WithObject("context", mcp.Description("Optional: artifacts ([]string), notes (string)")),
), s.handleDispatchTask)

s.mcpServer.AddTool(mcp.NewTool("get_task_status",
    mcp.WithDescription("Get status of a task dispatched to a vassal"),
    mcp.WithString("vassal", mcp.Required(), mcp.Description("Vassal name")),
    mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID from dispatch_task")),
), s.handleGetTaskStatus)

s.mcpServer.AddTool(mcp.NewTool("abort_task",
    mcp.WithDescription("Abort a running task on a vassal"),
    mcp.WithString("vassal", mcp.Required(), mcp.Description("Vassal name")),
    mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID to abort")),
), s.handleAbortTask)
```

**Step 2: Add handlers to `tools.go`**

Add at the bottom of `internal/mcp/tools.go`:

```go
func (s *Server) handleDispatchTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	vassalName, _ := req.Params.Arguments["vassal"].(string)
	task, _ := req.Params.Arguments["task"].(string)
	if vassalName == "" || task == "" {
		return mcp.NewToolResultError("vassal and task are required"), nil
	}
	if s.vassalPool == nil {
		return mcp.NewToolResultError("no vassal pool configured"), nil
	}
	args := map[string]any{"task": task}
	if ctx, ok := req.Params.Arguments["context"]; ok {
		args["context"] = ctx
	}
	result, err := s.vassalPool.CallTool(ctx, vassalName, "dispatch_task", args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(result), nil
}

func (s *Server) handleGetTaskStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	vassalName, _ := req.Params.Arguments["vassal"].(string)
	taskID, _ := req.Params.Arguments["task_id"].(string)
	if vassalName == "" || taskID == "" {
		return mcp.NewToolResultError("vassal and task_id are required"), nil
	}
	if s.vassalPool == nil {
		return mcp.NewToolResultError("no vassal pool configured"), nil
	}
	result, err := s.vassalPool.CallTool(ctx, vassalName, "get_task_status", map[string]any{"task_id": taskID})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(result), nil
}

func (s *Server) handleAbortTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	vassalName, _ := req.Params.Arguments["vassal"].(string)
	taskID, _ := req.Params.Arguments["task_id"].(string)
	if vassalName == "" || taskID == "" {
		return mcp.NewToolResultError("vassal and task_id are required"), nil
	}
	if s.vassalPool == nil {
		return mcp.NewToolResultError("no vassal pool configured"), nil
	}
	result, err := s.vassalPool.CallTool(ctx, vassalName, "abort_task", map[string]any{"task_id": taskID})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(result), nil
}
```

**Step 3: Wire `vassalPool` into MCP server in `daemon.go`**

In `daemon.go`, after creating the MCP server (find `mcp.NewServer(...)` call), add:

```go
d.mcpSrv.SetVassalPool(d.vassalPool)
```

**Step 4: Build**

```bash
go build ./...
```
Expected: no errors.

**Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools.go internal/daemon/daemon.go
git commit -m "feat: add dispatch_task, get_task_status, abort_task to King MCP tools"
```

---

### Task 10: Add `kingctl report-done` subcommand

**Files:**
- Modify: `cmd/kingctl/main.go`

**Step 1: Add `report-done` to the switch in `main()`**

```go
case "report-done":
    cmdReportDone()
```

**Step 2: Add the command function**

```go
func cmdReportDone() {
	fs := flag.NewFlagSet("report-done", flag.ExitOnError)
	taskID := fs.String("task", "", "task ID to mark as done (required)")
	_ = fs.Parse(os.Args[2:])

	if *taskID == "" {
		fmt.Fprintln(os.Stderr, "error: --task is required")
		os.Exit(1)
	}

	// report-done runs inside the vassal repo, not the king root.
	// Find the task file by searching upward for .king/tasks/<id>.json.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	taskFile := findTaskFile(cwd, *taskID)
	if taskFile == "" {
		fmt.Fprintf(os.Stderr, "error: task file not found for %s\n", *taskID)
		os.Exit(1)
	}

	data, err := os.ReadFile(taskFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading task: %v\n", err)
		os.Exit(1)
	}

	var t map[string]any
	if err := json.Unmarshal(data, &t); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing task: %v\n", err)
		os.Exit(1)
	}
	t["status"] = "done"

	out, _ := json.MarshalIndent(t, "", "  ")
	if err := os.WriteFile(taskFile, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing task: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Task %s marked as done.\n", *taskID)
}

// findTaskFile searches for .king/tasks/<taskID>.json starting at dir and walking up.
func findTaskFile(dir, taskID string) string {
	for {
		candidate := filepath.Join(dir, ".king", "tasks", taskID+".json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
```

**Step 3: Add `report-done` to `printUsage()`**

```go
fmt.Fprintln(os.Stderr, "  report-done --task <id>   Mark a task as done (run from vassal repo)")
```

**Step 4: Build**

```bash
go build ./cmd/kingctl/
```
Expected: no errors.

**Step 5: Commit**

```bash
git add cmd/kingctl/main.go
git commit -m "feat: add kingctl report-done subcommand for vassal task completion"
```

---

### Task 11: End-to-end smoke test

**Step 1: Build all binaries**

```bash
cd /Users/alex/Desktop/Claude_King
go build -o king ./cmd/king
go build -o kingctl ./cmd/kingctl
go build -o king-vassal ./cmd/king-vassal
```
All must succeed.

**Step 2: Create a test project with a claude vassal**

```bash
mkdir -p /tmp/king-test/subproject
cat > /tmp/king-test/.king/kingdom.yml << 'EOF'
name: test-kingdom
vassals:
  - name: worker
    type: claude
    repo_path: ./subproject
    autostart: true
EOF
```

**Step 3: Start the kingdom**

```bash
cd /tmp/king-test
/Users/alex/Desktop/Claude_King/king up --detach
```
Expected:
```
Kingdom started (pid: XXXXX)
```

**Step 4: Verify vassal is registered**

```bash
/Users/alex/Desktop/Claude_King/kingctl status
```
Expected: no crash. Vassal `worker` may show as "claude" type.

**Step 5: Check that `king-vassal` socket was created**

```bash
ls /tmp/king-test/.king/vassals/
```
Expected: `worker.sock` file exists.

**Step 6: Stop the kingdom**

```bash
/Users/alex/Desktop/Claude_King/king down
```

**Step 7: Commit**

```bash
cd /Users/alex/Desktop/Claude_King
git commit --allow-empty -m "test: manually verified king-vassal socket created on king up"
```
