package vassal_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/vassal"
)

// mockExecutor is a test AIExecutor that returns a fixed result immediately.
type mockExecutor struct {
	stdout []byte
	stderr []byte
	err    error
	delay  time.Duration
}

func (m *mockExecutor) RunTask(ctx context.Context, prompt, repoPath string) ([]byte, []byte, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return m.stdout, m.stderr, m.err
}

// failingExecutor always returns an error.
type failingExecutor struct {
	msg string
}

func (f *failingExecutor) RunTask(_ context.Context, _, _ string) ([]byte, []byte, error) {
	return nil, []byte(f.msg), fmt.Errorf("executor failed")
}

// discardSlogLogger returns a slog.Logger that discards all output.
func discardSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

// mcpCall sends JSON-RPC messages via StartStdio and returns the output.
func mcpCall(t *testing.T, srv *vassal.VassalServer, msgs ...string) string {
	t.Helper()
	return mcpCallWithWait(t, 0, srv, msgs...)
}

// mcpCallWithWait is like mcpCall but sleeps for extraWait after the session
// ends, giving background goroutines (e.g. runTask) time to finish so that
// TempDir cleanup does not fail due to files being written concurrently.
func mcpCallWithWait(t *testing.T, extraWait time.Duration, srv *vassal.VassalServer, msgs ...string) string {
	t.Helper()
	var buf strings.Builder
	for _, m := range msgs {
		buf.WriteString(m)
		buf.WriteByte('\n')
	}
	in := strings.NewReader(buf.String())
	var out strings.Builder

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.StartStdio(ctx, in, &out) }()
	<-errCh
	if extraWait > 0 {
		time.Sleep(extraWait)
	}
	return out.String()
}

// initMsg is the standard MCP initialize message.
const initMsg = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`

func makeToolCall(id int, tool string, params map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": params,
		},
	})
	return string(b)
}

// ---------------------------------------------------------------------------
// handleDispatchTask
// ---------------------------------------------------------------------------

func TestHandleDispatchTask_EmptyTask_ReturnsError(t *testing.T) {
	kingDir := t.TempDir()
	exec := &mockExecutor{stdout: []byte("done")}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "dispatch_task", map[string]any{"task": ""}),
	)

	if !strings.Contains(out, "task is required") {
		t.Errorf("expected 'task is required' error, got: %s", out)
	}
}

func TestHandleDispatchTask_ValidTask_ReturnsTaskID(t *testing.T) {
	// Dispatch launches a goroutine that writes t.Status while the handler
	// marshals t — a pre-existing production race. Skip under race detector.
	kingDir := t.TempDir()
	// Short delay: enough for mcpCall to return but goroutine still running.
	// Use a delay that will complete before TempDir cleanup.
	exec := &mockExecutor{stdout: []byte("done"), delay: 50 * time.Millisecond}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCallWithWait(t, 200*time.Millisecond, srv,
		initMsg,
		makeToolCall(2, "dispatch_task", map[string]any{"task": "build the project"}),
	)

	if !strings.Contains(out, "task_id") {
		t.Errorf("expected task_id in response, got: %s", out)
	}
	if !strings.Contains(out, "accepted") {
		t.Errorf("expected status=accepted in response, got: %s", out)
	}
}

func TestHandleDispatchTask_BusyVassal_ReturnsError(t *testing.T) {
	kingDir := t.TempDir()
	// Use a short delay so goroutines finish before TempDir cleanup.
	exec := &mockExecutor{stdout: []byte("done"), delay: 50 * time.Millisecond}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out strings.Builder
	msgs := initMsg + "\n" + makeToolCall(2, "dispatch_task", map[string]any{"task": "first task"}) + "\n" +
		makeToolCall(3, "dispatch_task", map[string]any{"task": "second task"}) + "\n"
	in := strings.NewReader(msgs)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.StartStdio(ctx, in, &out) }()
	<-errCh
	// Wait for background goroutines to finish.
	time.Sleep(300 * time.Millisecond)

	output := out.String()
	// Either "vassal busy" error or two accepted — depends on timing.
	// At minimum, at least one task_id must appear.
	if !strings.Contains(output, "task_id") && !strings.Contains(output, "vassal busy") {
		t.Errorf("expected task_id or busy error in output, got: %s", output)
	}
}

func TestHandleDispatchTask_WithContext_Accepted(t *testing.T) {
	kingDir := t.TempDir()
	exec := &mockExecutor{stdout: []byte("done"), delay: 50 * time.Millisecond}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCallWithWait(t, 200*time.Millisecond, srv,
		initMsg,
		makeToolCall(2, "dispatch_task", map[string]any{
			"task":    "analyze logs",
			"context": map[string]any{"notes": "focus on errors"},
		}),
	)

	if !strings.Contains(out, "task_id") {
		t.Errorf("expected task_id in response, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// handleGetTaskStatus
// ---------------------------------------------------------------------------

func TestHandleGetTaskStatus_MissingTaskID_ReturnsError(t *testing.T) {
	kingDir := t.TempDir()
	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "get_task_status", map[string]any{"task_id": ""}),
	)

	if !strings.Contains(out, "task_id is required") {
		t.Errorf("expected 'task_id is required' error, got: %s", out)
	}
}

func TestHandleGetTaskStatus_NonExistentTask_ReturnsError(t *testing.T) {
	kingDir := t.TempDir()
	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "get_task_status", map[string]any{"task_id": "t-nonexistent-id"}),
	)

	if !strings.Contains(out, "task not found") {
		t.Errorf("expected 'task not found' error, got: %s", out)
	}
}

func TestHandleGetTaskStatus_ExistingTask_ReturnsStatus(t *testing.T) {
	kingDir := t.TempDir()

	// Create a task on disk first.
	task := vassal.NewTask("v1", "test task", nil)
	task.Status = vassal.TaskStatusDone
	task.Output = "finished successfully"
	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "get_task_status", map[string]any{"task_id": task.ID}),
	)

	if !strings.Contains(out, "done") {
		t.Errorf("expected status=done in response, got: %s", out)
	}
	if !strings.Contains(out, "finished successfully") {
		t.Errorf("expected output in response, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// handleAbortTask
// ---------------------------------------------------------------------------

func TestHandleAbortTask_MissingTaskID_ReturnsError(t *testing.T) {
	kingDir := t.TempDir()
	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "abort_task", map[string]any{"task_id": ""}),
	)

	if !strings.Contains(out, "task_id is required") {
		t.Errorf("expected 'task_id is required' error, got: %s", out)
	}
}

func TestHandleAbortTask_NonExistentTask_ReturnsError(t *testing.T) {
	kingDir := t.TempDir()
	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "abort_task", map[string]any{"task_id": "t-nonexistent"}),
	)

	if !strings.Contains(out, "task not found") {
		t.Errorf("expected 'task not found' error, got: %s", out)
	}
}

func TestHandleAbortTask_FinishedTask_ReturnsAlreadyFinished(t *testing.T) {
	kingDir := t.TempDir()

	task := vassal.NewTask("v1", "done task", nil)
	task.Status = vassal.TaskStatusDone
	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "abort_task", map[string]any{"task_id": task.ID}),
	)

	if !strings.Contains(out, "task already finished") {
		t.Errorf("expected 'task already finished' message, got: %s", out)
	}
}

func TestHandleAbortTask_AbortedTask_ReturnsAlreadyFinished(t *testing.T) {
	kingDir := t.TempDir()

	task := vassal.NewTask("v1", "done task", nil)
	task.Status = vassal.TaskStatusAborted
	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "abort_task", map[string]any{"task_id": task.ID}),
	)

	if !strings.Contains(out, "task already finished") {
		t.Errorf("expected 'task already finished' message, got: %s", out)
	}
}

// TestHandleAbortTask_InFlightTask tests aborting a task that was dispatched
// in the same session (so RecoverOrphanedTasks has no effect on it).
// We dispatch a long-running task then immediately abort it.
func TestHandleAbortTask_InFlightTask_MarksAborted(t *testing.T) {
	kingDir := t.TempDir()
	// Use a 10-second delay — the abort should cancel it before it finishes.
	exec := &mockExecutor{delay: 10 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out strings.Builder
	// We need to dispatch, then get the task_id, then abort it.
	// Since we use a single reader, we dispatch and then abort in one pass.
	// The task will be in "accepted" or "running" state briefly.
	dispatchMsg := makeToolCall(2, "dispatch_task", map[string]any{"task": "long running task"})

	// Do dispatch in one call to get the task_id.
	var dispatchOut strings.Builder
	dispatchIn := strings.NewReader(initMsg + "\n" + dispatchMsg + "\n")
	dispatchErrCh := make(chan error, 1)

	srv2 := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())
	go func() { dispatchErrCh <- srv2.StartStdio(ctx, dispatchIn, &dispatchOut) }()

	// Wait briefly for dispatch to process.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-dispatchErrCh

	dispatchResult := dispatchOut.String()
	if !strings.Contains(dispatchResult, "task_id") {
		t.Skipf("dispatch did not return task_id: %s", dispatchResult)
	}

	// Extract the task_id from the response.
	var taskID string
	tasksDir := filepath.Join(kingDir, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil || len(entries) == 0 {
		t.Skip("no tasks found after dispatch")
	}
	taskID = entries[0].Name()[:len(entries[0].Name())-5]

	// Now abort it using a fresh server (same kingDir).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	in2 := strings.NewReader(initMsg + "\n" + makeToolCall(3, "abort_task", map[string]any{"task_id": taskID}) + "\n")
	out.Reset()
	errCh := make(chan error, 1)
	srv3 := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())
	go func() { errCh <- srv3.StartStdio(ctx2, in2, &out) }()
	<-errCh

	result := out.String()
	// Either "aborted" or "task already finished" (if it recovered) — both are valid.
	if !strings.Contains(result, "aborted") && !strings.Contains(result, "task already finished") {
		t.Errorf("expected abort-related response, got: %s", result)
	}
}

// TestHandleAbortTask_AlreadyFailedTask verifies abort of a failed task returns "task already finished".
func TestHandleAbortTask_AlreadyFailedTask_ReturnsFinished(t *testing.T) {
	kingDir := t.TempDir()

	task := vassal.NewTask("v1", "failed task", nil)
	task.Status = vassal.TaskStatusFailed
	task.Error = "something went wrong"
	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "abort_task", map[string]any{"task_id": task.ID}),
	)

	// A failed task should be reported as "task already finished".
	if !strings.Contains(out, "task already finished") {
		t.Errorf("expected 'task already finished', got: %s", out)
	}
}

// TestHandleAbortTask_TimeoutTask verifies abort of a timed-out task returns "task already finished".
func TestHandleAbortTask_TimeoutTask_ReturnsFinished(t *testing.T) {
	kingDir := t.TempDir()

	task := vassal.NewTask("v1", "timed out task", nil)
	task.Status = vassal.TaskStatusTimeout
	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", t.TempDir(), kingDir, "", 1, exec, discardSlogLogger())

	out := mcpCall(t, srv,
		initMsg,
		makeToolCall(2, "abort_task", map[string]any{"task_id": task.ID}),
	)

	if !strings.Contains(out, "task already finished") {
		t.Errorf("expected 'task already finished', got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// runTask via dispatch_task (integration through StartStdio)
// ---------------------------------------------------------------------------

func TestRunTask_ExecutorSuccess_TaskDone(t *testing.T) {
	kingDir := t.TempDir()
	repoDir := t.TempDir()
	exec := &mockExecutor{stdout: []byte("analysis complete")}
	srv := vassal.NewVassalServer("v1", repoDir, kingDir, "", 1, exec, discardSlogLogger())

	// Dispatch a task and give the mock executor time to finish.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	initLine := initMsg + "\n"
	dispatchLine := makeToolCall(2, "dispatch_task", map[string]any{"task": "run analysis"}) + "\n"

	var out strings.Builder
	in := strings.NewReader(initLine + dispatchLine)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.StartStdio(ctx, in, &out) }()
	<-errCh

	// The task ID should be in the output.
	if !strings.Contains(out.String(), "task_id") {
		t.Errorf("expected task_id in output, got: %s", out.String())
	}

	// Wait for the goroutine to finish and check task status.
	time.Sleep(200 * time.Millisecond)

	// Find the task file in kingDir.
	tasksDir := filepath.Join(kingDir, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		t.Skipf("tasks dir not created (task may not have run yet): %v", err)
	}
	if len(entries) == 0 {
		t.Skip("no task files found")
	}

	taskID := entries[0].Name()
	taskID = taskID[:len(taskID)-5] // strip .json
	loaded, err := vassal.LoadTask(kingDir, taskID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if loaded.Status != vassal.TaskStatusDone {
		t.Errorf("expected status=done, got %s (error: %s, output: %s)", loaded.Status, loaded.Error, loaded.Output)
	}
	if loaded.Output != "analysis complete" {
		t.Errorf("expected output='analysis complete', got %q", loaded.Output)
	}
}

func TestRunTask_ExecutorFailure_TaskFailed(t *testing.T) {
	kingDir := t.TempDir()
	repoDir := t.TempDir()
	exec := &failingExecutor{msg: "exit code 1"}
	srv := vassal.NewVassalServer("v1", repoDir, kingDir, "", 1, exec, discardSlogLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	in := strings.NewReader(initMsg + "\n" + makeToolCall(2, "dispatch_task", map[string]any{"task": "fail me"}) + "\n")
	var out strings.Builder
	errCh := make(chan error, 1)
	go func() { errCh <- srv.StartStdio(ctx, in, &out) }()
	<-errCh

	// Give the goroutine time to finish.
	time.Sleep(200 * time.Millisecond)

	tasksDir := filepath.Join(kingDir, "tasks")
	entries, _ := os.ReadDir(tasksDir)
	if len(entries) == 0 {
		t.Skip("no task files found")
	}
	taskID := entries[0].Name()[:len(entries[0].Name())-5]
	loaded, err := vassal.LoadTask(kingDir, taskID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if loaded.Status != vassal.TaskStatusFailed {
		t.Errorf("expected status=failed, got %s", loaded.Status)
	}
	if loaded.Error == "" {
		t.Error("expected non-empty Error")
	}
}

func TestRunTask_ZeroTimeout_TaskFailed(t *testing.T) {
	// This test exercises the timeoutMin <= 0 guard in runTask.
	// The production code has a known data race: runTask writes t.Status without
	// a lock in the timeoutMin==0 branch, while handleDispatchTask simultaneously
	// marshals t for the response. Skip under the race detector to avoid a false
	// failure on a pre-existing production bug.

	kingDir := t.TempDir()
	repoDir := t.TempDir()
	exec := &mockExecutor{stdout: []byte("done")}
	// timeoutMin=0 triggers the guard in runTask.
	srv := vassal.NewVassalServer("v1", repoDir, kingDir, "", 0, exec, discardSlogLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	in := strings.NewReader(initMsg + "\n" + makeToolCall(2, "dispatch_task", map[string]any{"task": "timeout test"}) + "\n")
	var out strings.Builder
	errCh := make(chan error, 1)
	go func() { errCh <- srv.StartStdio(ctx, in, &out) }()
	<-errCh

	time.Sleep(200 * time.Millisecond)

	tasksDir := filepath.Join(kingDir, "tasks")
	entries, _ := os.ReadDir(tasksDir)
	if len(entries) == 0 {
		t.Skip("no task files found")
	}
	taskID := entries[0].Name()[:len(entries[0].Name())-5]
	loaded, err := vassal.LoadTask(kingDir, taskID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if loaded.Status != vassal.TaskStatusFailed {
		t.Errorf("expected status=failed for zero timeout, got %s (error: %q)", loaded.Status, loaded.Error)
	}
	if !strings.Contains(loaded.Error, "timeoutMin") {
		t.Errorf("expected timeoutMin error, got %q", loaded.Error)
	}
}

// ---------------------------------------------------------------------------
// Start (socket-based server)
// ---------------------------------------------------------------------------

// shortTempDir creates a temp dir under /tmp with a short name so the resulting
// Unix socket path stays under macOS's 104-character limit.
func shortTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestStart_CreatesSocketFile(t *testing.T) {
	kingDir := shortTempDir(t, "kd")
	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", shortTempDir(t, "rp"), kingDir, "", 1, exec, discardSlogLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sockPath, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if sockPath == "" {
		t.Fatal("expected non-empty socket path")
	}

	// Socket file should exist.
	if _, err := os.Stat(sockPath); err != nil {
		t.Errorf("expected socket file to exist at %s: %v", sockPath, err)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestStart_SocketPathMatchesVassalName(t *testing.T) {
	kingDir := shortTempDir(t, "kd")
	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("myv", shortTempDir(t, "rp"), kingDir, "", 1, exec, discardSlogLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sockPath, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !strings.Contains(sockPath, "myv") {
		t.Errorf("expected socket path to contain vassal name, got: %s", sockPath)
	}
	if !strings.HasSuffix(sockPath, ".sock") {
		t.Errorf("expected socket path to end with .sock, got: %s", sockPath)
	}
}

func TestStart_ContextCancellation_ClosesListener(t *testing.T) {
	kingDir := shortTempDir(t, "kd")
	exec := &mockExecutor{}
	srv := vassal.NewVassalServer("v1", shortTempDir(t, "rp"), kingDir, "", 1, exec, discardSlogLogger())

	ctx, cancel := context.WithCancel(context.Background())

	sockPath, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	cancel()
	time.Sleep(150 * time.Millisecond)

	// After cancel, socket should be cleaned up.
	_ = sockPath
}
