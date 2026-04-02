package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexli18/claude-king/internal/artifacts"
	"github.com/alexli18/claude-king/internal/audit"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
)

// handleListVassals handles the list_vassals MCP tool call.
// It returns vassal information from two sources: live PTY sessions from the
// PTYManager and persisted kingdom metadata from the store.
//
// Response shape:
//
//	{
//	  "vassals": [{"name": "...", "status": "...", "command": "...", "pid": N, "last_activity": "..."}],
//	  "kingdom": {"name": "...", "status": "...", "uptime_seconds": N}
//	}
func (s *Server) handleListVassals(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling list_vassals call")

	// Gather live session info from the PTY manager.
	sessions := s.ptyMgr.ListSessions()

	type vassalEntry struct {
		Name         string `json:"name"`
		Status       string `json:"status"`
		Command      string `json:"command"`
		PID          int    `json:"pid"`
		LastActivity string `json:"last_activity"`
	}

	vassals := make([]vassalEntry, 0, len(sessions))
	for _, sess := range sessions {
		vassals = append(vassals, vassalEntry{
			Name:         sess.Name,
			Status:       sess.Status,
			Command:      sess.Command,
			PID:          sess.PID,
			LastActivity: sess.LastActivity,
		})
	}

	// Fetch kingdom metadata from the store.
	kingdom, err := s.store.GetKingdom(s.kingdomID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to fetch kingdom: %v", err)), nil
	}

	kingdomInfo := map[string]any{
		"name":   "",
		"status": "unknown",
	}

	if kingdom != nil {
		var uptimeSeconds float64
		createdAt, parseErr := time.Parse(time.DateTime, kingdom.CreatedAt)
		if parseErr == nil {
			uptimeSeconds = time.Since(createdAt).Seconds()
		}

		kingdomInfo = map[string]any{
			"name":           kingdom.Name,
			"status":         kingdom.Status,
			"uptime_seconds": int64(uptimeSeconds),
		}
	}

	result := map[string]any{
		"vassals": vassals,
		"kingdom": kingdomInfo,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleExecIn executes a command inside a vassal's PTY session and returns
// the captured output, exit code, and duration.
func (s *Server) handleExecIn(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling exec_in call")

	// Parse required params.
	vassalName, err := request.RequireString("vassal")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: vassal name is required"), nil
	}

	command, err := request.RequireString("command")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: command is required"), nil
	}

	// Parse optional timeout (default 30 seconds).
	timeout := 30 * time.Second
	if ts := request.GetFloat("timeout_seconds", 0); ts > 0 {
		timeout = time.Duration(ts) * time.Second
	}

	// Look up the session.
	session, found := s.ptyMgr.GetSession(vassalName)
	if !found {
		return mcp.NewToolResultError(fmt.Sprintf("VASSAL_NOT_FOUND: no vassal named %q", vassalName)), nil
	}

	// Execute the command.
	output, exitCode, duration, err := session.ExecCommand(command, timeout)
	if err != nil {
		// Check if it was a timeout.
		if duration >= timeout {
			return mcp.NewToolResultError(fmt.Sprintf("TIMEOUT: command timed out after %v", timeout)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("EXEC_ERROR: %v", err)), nil
	}

	result := map[string]any{
		"output":      output,
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
	}

	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", marshalErr)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleGetEvents retrieves recent events from the kingdom event log with
// optional severity, source, and limit filters.
func (s *Server) handleGetEvents(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling get_events call")

	severity := request.GetString("severity", "")
	source := request.GetString("source", "")
	limit := int(request.GetFloat("limit", 20))

	if limit <= 0 {
		limit = 20
	}

	events, err := s.store.ListEvents(s.kingdomID, severity, source, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list events: %v", err)), nil
	}

	type eventEntry struct {
		ID           string `json:"id"`
		Severity     string `json:"severity"`
		Pattern      string `json:"pattern,omitempty"`
		Summary      string `json:"summary"`
		SourceID     string `json:"source_id,omitempty"`
		Acknowledged bool   `json:"acknowledged"`
		CreatedAt    string `json:"created_at"`
	}

	entries := make([]eventEntry, 0, len(events))
	for _, e := range events {
		entries = append(entries, eventEntry{
			ID:           e.ID,
			Severity:     e.Severity,
			Pattern:      e.Pattern,
			Summary:      e.Summary,
			SourceID:     e.SourceID,
			Acknowledged: e.Acknowledged,
			CreatedAt:    e.CreatedAt,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal events: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleRegisterArtifact registers a file-based artifact in the ledger.
func (s *Server) handleRegisterArtifact(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling register_artifact call")

	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: name is required"), nil
	}

	filePath, err := request.RequireString("file_path")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: file_path is required"), nil
	}

	producer := request.GetString("producer", "")
	mimeType := request.GetString("mime_type", "")

	a, err := s.ledger.Register(name, filePath, producer, mimeType)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("REGISTER_FAILED: %v", err)), nil
	}

	result := map[string]any{
		"name":     a.Name,
		"version":  a.Version,
		"uri":      artifacts.BuildURI(a.Name),
		"checksum": a.Checksum,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleResolveArtifact resolves an artifact name to its file path and metadata.
func (s *Server) handleResolveArtifact(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling resolve_artifact call")

	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: name is required"), nil
	}

	a, err := s.ledger.Resolve(name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("ARTIFACT_NOT_FOUND: %v", err)), nil
	}

	result := map[string]any{
		"name":       a.Name,
		"file_path":  a.FilePath,
		"version":    a.Version,
		"producer":   a.ProducerID,
		"updated_at": a.UpdatedAt,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleReadNeighbor reads a file from an allowed directory (kingdom root or
// vassal working directories) and returns its content.
func (s *Server) handleReadNeighbor(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling read_neighbor call")

	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: path is required"), nil
	}

	maxLines := int(request.GetFloat("max_lines", 200))
	if maxLines <= 0 {
		maxLines = 200
	}

	// Resolve to absolute path.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("INVALID_PARAMS: invalid path: %v", err)), nil
	}

	// Validate the path is within an allowed directory.
	if !isPathAllowed(absPath, s.rootDir) {
		return mcp.NewToolResultError("PERMISSION_DENIED: path is outside the kingdom root directory"), nil
	}

	// Check file exists.
	info, err := os.Stat(absPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("FILE_NOT_FOUND: %v", err)), nil
	}
	if info.IsDir() {
		return mcp.NewToolResultError("INVALID_PARAMS: path is a directory, not a file"), nil
	}

	// Read file content with line limit.
	content, lineCount, truncated, err := readFileLines(absPath, maxLines)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("FILE_NOT_FOUND: %v", err)), nil
	}

	result := map[string]any{
		"content":   content,
		"path":      absPath,
		"lines":     lineCount,
		"truncated": truncated,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}

// handleGetAuditLog retrieves audit entries with optional filters.
func (s *Server) handleGetAuditLog(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling get_audit_log call")

	layer := request.GetString("layer", "")
	vassal := request.GetString("vassal", "")
	since := request.GetString("since", "")
	until := request.GetString("until", "")
	traceID := request.GetString("trace_id", "")
	limit := int(request.GetFloat("limit", 50))

	if layer != "" && layer != "ingestion" && layer != "sieve" && layer != "action" {
		return mcp.NewToolResultError("invalid layer: must be ingestion, sieve, or action"), nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	f := store.AuditFilter{
		KingdomID: s.kingdomID,
		Layer:     layer,
		Source:    vassal,
		Since:     audit.ParseRelativeTime(since),
		Until:     audit.ParseRelativeTime(until),
		TraceID:   traceID,
		Limit:     limit,
	}

	entries, err := s.store.ListAuditEntries(f)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list audit entries: %v", err)), nil
	}

	total, _ := s.store.CountAuditEntries(f)

	type auditEntry struct {
		ID        string `json:"id"`
		Layer     string `json:"layer"`
		Source    string `json:"source"`
		Content   string `json:"content"`
		TraceID   string `json:"trace_id,omitempty"`
		Metadata  string `json:"metadata,omitempty"`
		Sampled   bool   `json:"sampled"`
		CreatedAt string `json:"created_at"`
	}

	result := make([]auditEntry, len(entries))
	for i, e := range entries {
		result[i] = auditEntry{
			ID:        e.ID,
			Layer:     e.Layer,
			Source:    e.Source,
			Content:   e.Content,
			TraceID:   e.TraceID,
			Metadata:  e.Metadata,
			Sampled:   e.Sampled,
			CreatedAt: e.CreatedAt,
		}
	}

	resp := map[string]any{
		"entries":    result,
		"total":     total,
		"filtered":  layer != "" || vassal != "" || since != "" || until != "" || traceID != "",
		"kingdom_id": s.kingdomID,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleGetActionTrace retrieves a detailed action trace.
func (s *Server) handleGetActionTrace(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling get_action_trace call")

	traceID, err := request.RequireString("trace_id")
	if err != nil {
		return mcp.NewToolResultError("trace_id is required"), nil
	}

	trace, err := s.store.GetActionTrace(traceID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get action trace: %v", err)), nil
	}
	if trace == nil {
		return mcp.NewToolResultError(fmt.Sprintf("action trace not found: %s", traceID)), nil
	}

	result := map[string]any{
		"trace_id":    trace.TraceID,
		"vassal_name": trace.VassalName,
		"command":     trace.Command,
		"status":      trace.Status,
		"exit_code":   trace.ExitCode,
		"output":      trace.Output,
		"duration_ms": trace.DurationMs,
		"started_at":  trace.StartedAt,
		"completed_at": trace.CompletedAt,
	}

	if trace.TriggerEventID != "" {
		result["trigger_event_id"] = trace.TriggerEventID
	}

	// Check for associated approval request across all statuses.
	if approval, _ := s.store.GetApprovalRequestByTraceID(traceID); approval != nil {
		result["approval"] = map[string]any{
			"id":           approval.ID,
			"status":       approval.Status,
			"responded_at": approval.RespondedAt,
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleRespondApproval responds to a pending sovereign approval request.
func (s *Server) handleRespondApproval(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling respond_approval call")

	if !s.sovereignApproval {
		return mcp.NewToolResultError("sovereign_approval is not enabled"), nil
	}

	requestID, err := request.RequireString("request_id")
	if err != nil {
		return mcp.NewToolResultError("request_id is required"), nil
	}

	approved := request.GetBool("approved", false)

	req, err := s.store.GetApprovalRequest(requestID)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to get approval request: %v", err)), nil
	}
	if req == nil {
		return mcp.NewToolResultError(fmt.Sprintf("approval request not found: %s", requestID)), nil
	}
	if req.Status != "pending" {
		return mcp.NewToolResultError(fmt.Sprintf("approval request is not pending (current: %s)", req.Status)), nil
	}

	status := "rejected"
	if approved {
		status = "approved"
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err := s.store.UpdateApprovalRequest(requestID, status, now); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to update approval: %v", err)), nil
	}

	// Signal the waiting exec_in goroutine via ApprovalManager.
	if s.approvalMgr != nil {
		// Ignore error: the request might have already timed out.
		_ = s.approvalMgr.Respond(requestID, approved)
	}

	result := map[string]any{
		"request_id":  requestID,
		"trace_id":    req.TraceID,
		"status":      status,
		"command":     req.Command,
		"vassal_name": req.VassalName,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal response: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// handleDispatchTask proxies a dispatch_task call to the named vassal's MCP server.
func (s *Server) handleDispatchTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling dispatch_task call")

	vassalName, err := request.RequireString("vassal")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: vassal name is required"), nil
	}

	task, err := request.RequireString("task")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: task is required"), nil
	}

	if s.vassalPool == nil {
		return mcp.NewToolResultError("vassal pool not available"), nil
	}

	client, ok := s.vassalPool.Get(vassalName)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("vassal not found or not a claude vassal: %q", vassalName)), nil
	}

	args := map[string]any{
		"task": task,
	}
	// Pass optional context object through if provided.
	if rawArgs := request.GetArguments(); rawArgs != nil {
		if taskCtx, exists := rawArgs["context"]; exists && taskCtx != nil {
			args["context"] = taskCtx
		}
	}

	result, err := client.CallTool(ctx, "dispatch_task", args)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("dispatch_task failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// handleGetTaskStatus proxies a get_task_status call to the named vassal's MCP server.
func (s *Server) handleGetTaskStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling get_task_status call")

	vassalName, err := request.RequireString("vassal")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: vassal name is required"), nil
	}

	taskID, err := request.RequireString("task_id")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: task_id is required"), nil
	}

	if s.vassalPool == nil {
		return mcp.NewToolResultError("vassal pool not available"), nil
	}

	client, ok := s.vassalPool.Get(vassalName)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("vassal not found or not a claude vassal: %q", vassalName)), nil
	}

	result, err := client.CallTool(ctx, "get_task_status", map[string]any{
		"task_id": taskID,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get_task_status failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// handleAbortTask proxies an abort_task call to the named vassal's MCP server.
func (s *Server) handleAbortTask(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling abort_task call")

	vassalName, err := request.RequireString("vassal")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: vassal name is required"), nil
	}

	taskID, err := request.RequireString("task_id")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: task_id is required"), nil
	}

	if s.vassalPool == nil {
		return mcp.NewToolResultError("vassal pool not available"), nil
	}

	client, ok := s.vassalPool.Get(vassalName)
	if !ok {
		return mcp.NewToolResultError(fmt.Sprintf("vassal not found or not a claude vassal: %q", vassalName)), nil
	}

	result, err := client.CallTool(ctx, "abort_task", map[string]any{
		"task_id": taskID,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("abort_task failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// ParseSinceDuration converts a user-supplied duration string like "5m", "1h", "30s"
// to a time.Duration. It is an exported wrapper over time.ParseDuration to provide
// a stable extension point for supporting additional formats in future.
func ParseSinceDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// FilterEventsBySeverity returns events matching the given severity.
// Empty severity returns all events.
func FilterEventsBySeverity(events []store.Event, severity string) []store.Event {
	if severity == "" {
		return events
	}
	out := make([]store.Event, 0)
	for _, e := range events {
		if e.Severity == severity {
			out = append(out, e)
		}
	}
	return out
}

// handleGetSerialEvents retrieves recent events from a serial vassal, filtered
// by a time window ("since") and an optional severity filter.
func (s *Server) handleGetSerialEvents(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debug("handling get_serial_events call")

	vassalName, err := request.RequireString("vassal")
	if err != nil {
		return mcp.NewToolResultError("INVALID_PARAMS: vassal name is required"), nil
	}

	sinceStr := request.GetString("since", "")
	severity := request.GetString("severity", "")

	if sinceStr == "" {
		sinceStr = "1h"
	}

	dur, err := ParseSinceDuration(sinceStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid since value %q: %v", sinceStr, err)), nil
	}

	after := time.Now().Add(-dur)

	// Fetch events pre-filtered by source at the DB layer to reduce load;
	// the time-window filter is applied in-memory below.
	all, err := s.store.ListEvents(s.kingdomID, "", vassalName, 0)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("store error: %v", err)), nil
	}

	var filtered []store.Event
	for _, e := range all {
		t, parseErr := time.Parse(time.RFC3339, e.CreatedAt)
		if parseErr != nil {
			// Fall back to SQLite datetime format.
			t, parseErr = time.Parse("2006-01-02 15:04:05", e.CreatedAt)
			if parseErr != nil {
				continue
			}
		}
		if t.After(after) {
			filtered = append(filtered, e)
		}
	}
	filtered = FilterEventsBySeverity(filtered, severity)

	out, err := json.Marshal(filtered)
	if err != nil {
		return mcp.NewToolResultError("json marshal error"), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

// isPathAllowed checks whether the given absolute path is within the kingdom
// root directory.
func isPathAllowed(absPath, rootDir string) bool {
	// Clean both paths to normalize.
	cleanRoot := filepath.Clean(rootDir)
	cleanPath := filepath.Clean(absPath)

	// The path must be within the root directory.
	return strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) || cleanPath == cleanRoot
}

// readFileLines reads up to maxLines lines from the file at path.
// Returns the content as a string, the number of lines read, and whether
// the output was truncated.
func readFileLines(path string, maxLines int) (content string, lines int, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false, err
	}
	defer f.Close()

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if lines >= maxLines {
			truncated = true
			break
		}
		if lines > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(scanner.Text())
		lines++
	}
	if err := scanner.Err(); err != nil {
		return "", 0, false, err
	}
	return b.String(), lines, truncated, nil
}
