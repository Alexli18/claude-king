package vassal

import (
	"context"
	"encoding/json"
	"fmt"
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

	go func() {
		defer func() {
			ln.Close()
			_ = os.Remove(sockPath) // Fix 4: clean up socket on exit
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

	// Cancel listener on context done (separate goroutine, no double-close issue).
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	return sockPath, nil
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

	s.mu.Lock()
	if s.activeTask != nil && (s.activeTask.Status == TaskStatusAccepted || s.activeTask.Status == TaskStatusRunning) {
		s.mu.Unlock()
		return mcp.NewToolResultError(fmt.Sprintf("vassal busy with task %s (status: %s)", s.activeTask.ID, s.activeTask.Status)), nil
	}
	s.mu.Unlock()

	// Create and save task BEFORE setting activeTask.
	t := NewTask(s.name, taskDesc, ctx)
	if err := SaveTask(s.kingDir, t); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save task: %v", err)), nil
	}

	// Only set activeTask after successful save.
	s.mu.Lock()
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

	s.mu.Lock()
	if s.activeTask != nil && s.activeTask.ID == taskID {
		s.activeTask = nil
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

	// Mark as running.
	t.Status = TaskStatusRunning
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

	if err := SaveTask(s.kingDir, t); err != nil {
		s.logger.Error("failed to save task result", "task_id", t.ID, "err", err)
	}
	s.activeTask = nil
	s.logger.Info("task finished", "task_id", t.ID, "status", t.Status)
}
