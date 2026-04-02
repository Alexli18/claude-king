package vassal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
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
		defer ln.Close()
		// Close the listener when context is cancelled so Accept unblocks.
		go func() {
			<-ctx.Done()
			ln.Close()
		}()

		for {
			conn, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.logger.Error("accept error", "err", err)
				return
			}
			go s.serveConn(ctx, conn)
		}
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

	t := NewTask(s.name, taskDesc, ctx)
	s.activeTask = t
	s.mu.Unlock()

	if err := SaveTask(s.kingDir, t); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("save task: %v", err)), nil
	}

	go s.runTask(t)

	result, _ := json.Marshal(map[string]string{
		"task_id": t.ID,
		"status":  string(t.Status),
	})
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

	result, _ := json.Marshal(map[string]any{
		"task_id":    t.ID,
		"status":     t.Status,
		"output":     t.Output,
		"artifacts":  t.Artifacts,
		"error":      t.Error,
		"created_at": t.CreatedAt.Format(time.RFC3339),
		"updated_at": t.UpdatedAt.Format(time.RFC3339),
	})
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

// runTask is a stub — will be implemented in Task 6.
func (s *VassalServer) runTask(t *Task) {
	s.logger.Info("runTask stub called", "task_id", t.ID)
	t.Status = TaskStatusFailed
	t.Error = "runTask not yet implemented"
	_ = SaveTask(s.kingDir, t)
	s.mu.Lock()
	s.activeTask = nil
	s.mu.Unlock()
}
