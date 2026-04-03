package vassal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
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

	mu         sync.Mutex
	activeTask *Task
	taskCancel context.CancelFunc // Critical 2: cancel func for the running subprocess
	mcpServer  *server.MCPServer
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
	_ = os.Remove(sockPath) // remove stale socket

	s.mcpServer = server.NewMCPServer("king-vassal-"+s.name, "1.0.0")
	s.registerTools()

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return "", fmt.Errorf("listen unix %s: %w", sockPath, err)
	}

	// sync.Once ensures ln.Close() is called exactly once even if both goroutines race.
	var closeOnce sync.Once
	closeLn := func() { closeOnce.Do(func() { ln.Close() }) }

	go func() {
		defer func() {
			closeLn()
			_ = os.Remove(sockPath)
		}()
		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return // context cancelled, clean exit
				}
				s.logger.Error("accept error", "err", err)
				return
			}
			// Serve one connection at a time (StdioServer is single-connection).
			s.serveConn(ctx, conn)
			// After connection closes, loop back and accept the next one.
		}
	}()

	// Cancel listener on context done.
	go func() {
		<-ctx.Done()
		closeLn()
	}()

	return sockPath, nil
}

// StartStdio serves MCP over the provided reader/writer (stdio mode).
// Used when launched by Claude Code via .mcp.json.
func (s *VassalServer) StartStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	s.mcpServer = server.NewMCPServer("king-vassal-"+s.name, "1.0.0")
	s.registerTools()
	stdioSrv := server.NewStdioServer(s.mcpServer)
	return stdioSrv.Listen(ctx, r, w)
}

// serveConn handles a single MCP client connection using the stdio transport
// over the provided net.Conn (which implements both io.Reader and io.Writer).
func (s *VassalServer) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	stdioSrv := server.NewStdioServer(s.mcpServer)
	if err := stdioSrv.Listen(ctx, conn, conn); err != nil && ctx.Err() == nil {
		s.logger.Error("connection error", "err", err)
	}
}

// registerTools registers all MCP tools on the server.
func (s *VassalServer) registerTools() {
	s.mcpServer.AddTool(mcp.NewTool("dispatch_task",
		mcp.WithDescription("Dispatch a task to this Claude Code vassal"),
		mcp.WithString("task", mcp.Required(), mcp.Description("Task description")),
		mcp.WithObject("context", mcp.Description("Optional context: artifacts list, notes")),
	), s.handleDispatchTask)

	s.mcpServer.AddTool(mcp.NewTool("get_task_status",
		mcp.WithDescription("Get the status of a dispatched task"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID returned by dispatch_task")),
	), s.handleGetTaskStatus)

	s.mcpServer.AddTool(mcp.NewTool("abort_task",
		mcp.WithDescription("Abort a running task"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("Task ID to abort")),
	), s.handleAbortTask)
}

func (s *VassalServer) handleDispatchTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskDesc := req.GetString("task", "")
	if taskDesc == "" {
		return mcp.NewToolResultError("task is required"), nil
	}

	var ctx map[string]any
	if args := req.GetArguments(); args != nil {
		if raw, ok := args["context"]; ok && raw != nil {
			if m, ok := raw.(map[string]any); ok {
				ctx = m
			}
		}
	}

	// Minor: first check avoids unnecessary I/O.
	s.mu.Lock()
	if s.activeTask != nil && (s.activeTask.Status == TaskStatusAccepted || s.activeTask.Status == TaskStatusRunning) {
		s.mu.Unlock()
		return mcp.NewToolResultError(fmt.Sprintf("vassal busy with task %s (status: %s)", s.activeTask.ID, s.activeTask.Status)), nil
	}
	s.mu.Unlock()

	// SaveTask outside lock (I/O).
	t := NewTask(s.name, taskDesc, ctx)
	if err := SaveTask(s.kingDir, t); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save task: %v", err)), nil
	}

	// Minor: re-check and set activeTask atomically (authoritative guard).
	s.mu.Lock()
	if s.activeTask != nil && (s.activeTask.Status == TaskStatusAccepted || s.activeTask.Status == TaskStatusRunning) {
		s.mu.Unlock()
		return mcp.NewToolResultError(fmt.Sprintf("vassal busy with task %s (status: %s)", s.activeTask.ID, s.activeTask.Status)), nil
	}
	s.activeTask = t
	s.mu.Unlock()

	go s.runTask(t)

	result, err := json.Marshal(map[string]string{
		"task_id": t.ID,
		"status":  string(t.Status),
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

func (s *VassalServer) handleGetTaskStatus(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := req.GetString("task_id", "")
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	t, err := LoadTask(s.kingDir, taskID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("task not found: %v", err)), nil
	}

	result, err := json.Marshal(map[string]any{
		"task_id":    t.ID,
		"status":     t.Status,
		"output":     t.Output,
		"artifacts":  t.Artifacts,
		"error":      t.Error,
		"created_at": t.CreatedAt.Format(time.RFC3339),
		"updated_at": t.UpdatedAt.Format(time.RFC3339),
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

func (s *VassalServer) handleAbortTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID := req.GetString("task_id", "")
	if taskID == "" {
		return mcp.NewToolResultError("task_id is required"), nil
	}

	t, err := LoadTask(s.kingDir, taskID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("task not found: %v", err)), nil
	}

	if t.Status != TaskStatusAccepted && t.Status != TaskStatusRunning {
		result, err := json.Marshal(map[string]string{"status": string(t.Status), "message": "task already finished"})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
		}
		return mcp.NewToolResultText(string(result)), nil
	}

	t.Status = TaskStatusAborted
	_ = SaveTask(s.kingDir, t)

	// Critical 2: cancel the subprocess and clear activeTask atomically.
	s.mu.Lock()
	if s.activeTask != nil && s.activeTask.ID == taskID {
		s.activeTask = nil
		if s.taskCancel != nil {
			s.taskCancel()
			s.taskCancel = nil
		}
	}
	s.mu.Unlock()

	result, err := json.Marshal(map[string]string{"task_id": taskID, "status": "aborted"})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(result)), nil
}

// runTask generates VASSAL.md, launches claude headless, and updates task state.
func (s *VassalServer) runTask(t *Task) {
	s.logger.Info("running task", "task_id", t.ID, "task", t.Task)

	// Important 4: guard against zero/negative timeout.
	if s.timeoutMin <= 0 {
		t.Status = TaskStatusFailed
		t.Error = "timeoutMin must be > 0"
		_ = SaveTask(s.kingDir, t)
		s.mu.Lock()
		s.activeTask = nil
		s.mu.Unlock()
		return
	}

	// Critical 1: write t.Status under lock so readers see a consistent value.
	s.mu.Lock()
	t.Status = TaskStatusRunning
	s.mu.Unlock()
	if err := SaveTask(s.kingDir, t); err != nil {
		s.logger.Error("failed to save running status", "task_id", t.ID, "err", err)
	}

	// Write VASSAL.md — Claude Code reads this as context.
	if err := WriteVassalMD(s.repoPath, s.name, t, nil); err != nil {
		s.logger.Warn("could not write VASSAL.md", "err", err)
	}

	// Build the prompt: task + reference to VASSAL.md.
	prompt := fmt.Sprintf("%s\n\n(See VASSAL.md in this directory for your role and context.)", t.Task)

	// Run claude headless with a timeout.
	timeout := time.Duration(s.timeoutMin) * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Critical 2: store cancel so handleAbortTask can kill the subprocess.
	s.mu.Lock()
	s.taskCancel = cancel
	s.mu.Unlock()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "text",
	)
	cmd.Dir = s.repoPath

	// Important 5: capture stderr so it can be included in error messages.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	out, err := cmd.Output()

	// Important 3: do LoadTask outside the lock to avoid blocking under I/O.
	current, loadErr := LoadTask(s.kingDir, t.ID)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Critical 2: clear taskCancel now that subprocess has finished.
	s.taskCancel = nil

	// Important 3: check abort status (loaded before acquiring lock).
	if loadErr != nil {
		s.logger.Warn("could not load task for abort-check, proceeding", "task_id", t.ID, "err", loadErr)
	}
	if loadErr == nil && current.Status == TaskStatusAborted {
		s.activeTask = nil
		return
	}

	if ctx.Err() == context.DeadlineExceeded {
		t.Status = TaskStatusTimeout
		t.Error = fmt.Sprintf("task exceeded %d minute timeout", s.timeoutMin)
	} else if err != nil {
		// Important 5: include stderr in the error message.
		t.Status = TaskStatusFailed
		errMsg := err.Error()
		if stderrBuf.Len() > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, stderrBuf.String())
		}
		t.Error = errMsg
		t.Output = string(out)
	} else {
		t.Status = TaskStatusDone
		t.Output = string(out)
	}

	if err := SaveTask(s.kingDir, t); err != nil {
		s.logger.Error("failed to save task result", "task_id", t.ID, "err", err)
	}
	s.activeTask = nil
	s.logger.Info("task finished", "task_id", t.ID, "status", t.Status)
}
