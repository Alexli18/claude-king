package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/alexli18/claude-king/internal/store"
)

// ---------------------------------------------------------------------------
// Fake PTY manager / session
// ---------------------------------------------------------------------------

type fakePTYSession struct {
	output   string
	exitCode int
	err      error
}

func (f *fakePTYSession) Write(data []byte) (int, error)       { return len(data), nil }
func (f *fakePTYSession) GetOutput() []byte                    { return []byte(f.output) }
func (f *fakePTYSession) ExecCommand(command string, timeout time.Duration) (string, int, time.Duration, error) {
	return f.output, f.exitCode, 0, f.err
}

type fakePTYManager struct {
	sessions map[string]*fakePTYSession
}

func (f *fakePTYManager) GetSession(name string) (PTYSession, bool) {
	s, ok := f.sessions[name]
	return s, ok
}

func (f *fakePTYManager) ListSessions() []PTYSessionInfo {
	infos := make([]PTYSessionInfo, 0, len(f.sessions))
	for name := range f.sessions {
		infos = append(infos, PTYSessionInfo{Name: name, Status: "running"})
	}
	return infos
}

// ---------------------------------------------------------------------------
// Fake VassalPool / VassalCaller
// ---------------------------------------------------------------------------

type fakeVassalCaller struct {
	result string
	err    error
}

func (f *fakeVassalCaller) CallTool(_ context.Context, toolName string, args map[string]any) (string, error) {
	return f.result, f.err
}

type fakeVassalPool struct {
	vassals map[string]*fakeVassalCaller
}

func (f *fakeVassalPool) Get(name string) (VassalCaller, bool) {
	c, ok := f.vassals[name]
	return c, ok
}

func (f *fakeVassalPool) Names() []string {
	names := make([]string, 0, len(f.vassals))
	for n := range f.vassals {
		names = append(names, n)
	}
	return names
}

// ---------------------------------------------------------------------------
// Fake ArtifactLedger
// ---------------------------------------------------------------------------

type fakeArtifactLedger struct {
	artifact *store.Artifact
	err      error
}

func (f *fakeArtifactLedger) Register(name, filePath, producerID, mimeType string) (*store.Artifact, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.artifact != nil {
		return f.artifact, nil
	}
	return &store.Artifact{Name: name, FilePath: filePath, ProducerID: producerID, MimeType: mimeType, Version: 1}, nil
}

func (f *fakeArtifactLedger) Resolve(name string) (*store.Artifact, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.artifact != nil {
		return f.artifact, nil
	}
	return &store.Artifact{Name: name}, nil
}

// ---------------------------------------------------------------------------
// Helper: newTestServer builds a bare Server without NewServer's side effects
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T, ptyMgr PTYManager, st *store.Store, ledger ArtifactLedger) *Server {
	t.Helper()
	mcpSrv := mcpserver.NewMCPServer(
		"test-king",
		"0.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	srv := &Server{
		mcpServer:        mcpSrv,
		ptyMgr:           ptyMgr,
		store:            st,
		ledger:           ledger,
		kingdomID:        "test-kingdom",
		rootDir:          t.TempDir(),
		logger:           slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		activeHeartbeats: make(map[string]context.CancelFunc),
	}
	return srv
}

// newTestStore opens a temp SQLite store for use in tests.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mcp-test-store-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	st, err := store.NewStore(dir + "/king.db")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// makeRequest builds a mcp.CallToolRequest with the given named string arguments.
func makeRequest(args map[string]any) mcpgo.CallToolRequest {
	return mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Arguments: args,
		},
	}
}

// ---------------------------------------------------------------------------
// handleDelegateStatus tests — no daemon needed, reads local state
// ---------------------------------------------------------------------------

func TestHandleDelegateStatus_NoParent(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	// parentKingdomSocket is empty → should succeed with observer_mode: false

	result, err := srv.handleDelegateStatus(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	// Parse the JSON text result
	text := extractText(t, result)
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["observer_mode"] != false {
		t.Errorf("expected observer_mode=false, got %v", resp["observer_mode"])
	}
	if resp["parent_kingdom_socket"] != "" {
		t.Errorf("expected empty parent socket, got %v", resp["parent_kingdom_socket"])
	}
}

func TestHandleDelegateStatus_WithParent(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = "/tmp/fake-parent.sock"
	srv.inObserverMode = true

	result, err := srv.handleDelegateStatus(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["observer_mode"] != true {
		t.Errorf("expected observer_mode=true")
	}
	if resp["parent_kingdom_socket"] != "/tmp/fake-parent.sock" {
		t.Errorf("expected parent socket, got %v", resp["parent_kingdom_socket"])
	}
}

// ---------------------------------------------------------------------------
// handleDelegateControl — no parent → error
// ---------------------------------------------------------------------------

func TestHandleDelegateControl_NoParent(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDelegateControl(context.Background(), makeRequest(map[string]any{
		"vassal": "ui",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when no parent kingdom")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "no parent kingdom") {
		t.Errorf("expected 'no parent kingdom' in error, got: %s", text)
	}
}

func TestHandleDelegateControl_MissingVassal(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDelegateControl(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when vassal is missing")
	}
}

func TestHandleDelegateControl_WithFakeDaemon(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"delegate_control": func(params json.RawMessage) (interface{}, string) {
			return map[string]interface{}{"delegated": true, "vassal": "ui"}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	result, err := srv.handleDelegateControl(context.Background(), makeRequest(map[string]any{
		"vassal": "ui",
		"force":  false,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "delegated") {
		t.Errorf("expected delegated in response, got: %s", text)
	}

	// Heartbeat should have been registered for "ui"
	srv.heartbeatMu.Lock()
	_, ok := srv.activeHeartbeats["ui"]
	srv.heartbeatMu.Unlock()
	if !ok {
		t.Error("expected heartbeat to be started for vassal 'ui'")
	}

	// Cleanup: cancel the heartbeat
	srv.heartbeatMu.Lock()
	if cancel, ok := srv.activeHeartbeats["ui"]; ok {
		cancel()
		delete(srv.activeHeartbeats, "ui")
	}
	srv.heartbeatMu.Unlock()
}

func TestHandleDelegateControl_DaemonError(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"delegate_control": func(params json.RawMessage) (interface{}, string) {
			return nil, "circuit breaker open for vassal ui"
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	result, err := srv.handleDelegateControl(context.Background(), makeRequest(map[string]any{
		"vassal": "ui",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when daemon returns error")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "delegate_control failed") {
		t.Errorf("expected 'delegate_control failed' in error, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// handleDelegateRelease tests
// ---------------------------------------------------------------------------

func TestHandleDelegateRelease_NoParent(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDelegateRelease(context.Background(), makeRequest(map[string]any{
		"vassal": "ui",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when no parent kingdom")
	}
}

func TestHandleDelegateRelease_WithFakeDaemon(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"delegate_release": func(params json.RawMessage) (interface{}, string) {
			return map[string]interface{}{"released": true}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	// Pre-register a fake heartbeat to test cancellation
	cancelCalled := false
	srv.heartbeatMu.Lock()
	srv.activeHeartbeats["ui"] = func() { cancelCalled = true }
	srv.heartbeatMu.Unlock()

	result, err := srv.handleDelegateRelease(context.Background(), makeRequest(map[string]any{
		"vassal": "ui",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", extractText(t, result))
	}

	if !cancelCalled {
		t.Error("expected heartbeat cancel to be called on release")
	}

	srv.heartbeatMu.Lock()
	_, ok := srv.activeHeartbeats["ui"]
	srv.heartbeatMu.Unlock()
	if ok {
		t.Error("expected heartbeat entry to be removed after release")
	}
}

func TestHandleDelegateRelease_MissingVassal(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDelegateRelease(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when vassal is missing")
	}
}

// ---------------------------------------------------------------------------
// handleGuardStatus tests
// ---------------------------------------------------------------------------

func TestHandleGuardStatus_NoParent(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleGuardStatus(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when no parent kingdom")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "no parent kingdom") {
		t.Errorf("expected 'no parent kingdom' in error, got: %s", text)
	}
}

func TestHandleGuardStatus_WithFakeDaemon(t *testing.T) {
	guardResponse := map[string]interface{}{
		"guards": []interface{}{
			map[string]interface{}{
				"vassal":       "api",
				"index":        0,
				"type":         "port_check",
				"circuit_open": false,
				"failures":     0,
			},
		},
	}
	handlers := map[string]HandlerFunc{
		"guard_status": func(params json.RawMessage) (interface{}, string) {
			return guardResponse, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	result, err := srv.handleGuardStatus(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "port_check") {
		t.Errorf("expected 'port_check' in response, got: %s", text)
	}
}

func TestHandleGuardStatus_WithVassalFilter(t *testing.T) {
	var receivedVassal string
	handlers := map[string]HandlerFunc{
		"guard_status": func(params json.RawMessage) (interface{}, string) {
			var p map[string]interface{}
			_ = json.Unmarshal(params, &p)
			if v, ok := p["vassal"].(string); ok {
				receivedVassal = v
			}
			return map[string]interface{}{"guards": []interface{}{}}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	result, err := srv.handleGuardStatus(context.Background(), makeRequest(map[string]any{
		"vassal": "myapi",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success")
	}
	if receivedVassal != "myapi" {
		t.Errorf("expected vassal filter 'myapi' passed to daemon, got %q", receivedVassal)
	}
}

func TestHandleGuardStatus_DaemonError(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"guard_status": func(params json.RawMessage) (interface{}, string) {
			return nil, "guard subsystem unavailable"
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	result, err := srv.handleGuardStatus(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when daemon returns error")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "guard_status failed") {
		t.Errorf("expected 'guard_status failed' in error, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// handleAbortTask tests
// ---------------------------------------------------------------------------

func TestHandleAbortTask_NoPool(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleAbortTask(context.Background(), makeRequest(map[string]any{
		"vassal":  "worker",
		"task_id": "task-123",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when vassal pool is nil")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "vassal pool not available") {
		t.Errorf("expected 'vassal pool not available', got: %s", text)
	}
}

func TestHandleAbortTask_MissingVassal(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleAbortTask(context.Background(), makeRequest(map[string]any{
		"task_id": "task-123",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing vassal param")
	}
}

func TestHandleAbortTask_MissingTaskID(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleAbortTask(context.Background(), makeRequest(map[string]any{
		"vassal": "worker",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing task_id param")
	}
}

func TestHandleAbortTask_VassalNotFound(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{vassals: map[string]*fakeVassalCaller{}}

	result, err := srv.handleAbortTask(context.Background(), makeRequest(map[string]any{
		"vassal":  "nonexistent",
		"task_id": "task-123",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when vassal not found in pool")
	}
}

func TestHandleAbortTask_Success(t *testing.T) {
	caller := &fakeVassalCaller{result: `{"aborted":true,"task_id":"task-123"}`}
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{
		vassals: map[string]*fakeVassalCaller{"worker": caller},
	}

	result, err := srv.handleAbortTask(context.Background(), makeRequest(map[string]any{
		"vassal":  "worker",
		"task_id": "task-123",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "task-123") {
		t.Errorf("expected task-123 in response, got: %s", text)
	}
}

func TestHandleAbortTask_CallerError(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{
		vassals: map[string]*fakeVassalCaller{
			"worker": {err: &mockError{"vassal unreachable"}},
		},
	}

	result, err := srv.handleAbortTask(context.Background(), makeRequest(map[string]any{
		"vassal":  "worker",
		"task_id": "task-abc",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when caller returns error")
	}
}

// mockError is a simple error implementation for tests.
type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// handleGetSerialEvents tests
// ---------------------------------------------------------------------------

func TestHandleGetSerialEvents_MissingVassal(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleGetSerialEvents(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing vassal param")
	}
}

func TestHandleGetSerialEvents_InvalidSince(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleGetSerialEvents(context.Background(), makeRequest(map[string]any{
		"vassal": "sensor",
		"since":  "not-a-duration",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid since duration")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "invalid since value") {
		t.Errorf("expected 'invalid since value', got: %s", text)
	}
}

func TestHandleGetSerialEvents_EmptyResult(t *testing.T) {
	st := newTestStore(t)
	// No events in store → returns empty array
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})

	result, err := srv.handleGetSerialEvents(context.Background(), makeRequest(map[string]any{
		"vassal": "sensor",
		"since":  "1h",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	// Should return null or [] for empty events
	if text != "null" && text != "[]" {
		// Any non-error JSON is fine
		var v interface{}
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			t.Errorf("expected valid JSON, got: %s", text)
		}
	}
}

func TestHandleGetSerialEvents_DefaultSince(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})

	// No "since" param → should default to "1h"
	result, err := srv.handleGetSerialEvents(context.Background(), makeRequest(map[string]any{
		"vassal": "sensor",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success with default since=1h, got: %v", extractText(t, result))
	}
}

func TestHandleGetSerialEvents_WithSeverityFilter(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})

	result, err := srv.handleGetSerialEvents(context.Background(), makeRequest(map[string]any{
		"vassal":   "sensor",
		"since":    "24h",
		"severity": "error",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
}

// ---------------------------------------------------------------------------
// handleDispatchTask tests
// ---------------------------------------------------------------------------

func TestHandleDispatchTask_NoPool(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleDispatchTask(context.Background(), makeRequest(map[string]any{
		"vassal": "worker",
		"task":   "do something",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when pool is nil")
	}
}

func TestHandleDispatchTask_Success(t *testing.T) {
	caller := &fakeVassalCaller{result: `{"task_id":"t-001","status":"dispatched"}`}
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{
		vassals: map[string]*fakeVassalCaller{"worker": caller},
	}

	result, err := srv.handleDispatchTask(context.Background(), makeRequest(map[string]any{
		"vassal": "worker",
		"task":   "analyze the data",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "t-001") {
		t.Errorf("expected task_id in response, got: %s", text)
	}
}

func TestHandleDispatchTask_VassalNotFound(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{vassals: map[string]*fakeVassalCaller{}}

	result, err := srv.handleDispatchTask(context.Background(), makeRequest(map[string]any{
		"vassal": "missing",
		"task":   "do something",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when vassal not found")
	}
}

// ---------------------------------------------------------------------------
// handleGetTaskStatus tests
// ---------------------------------------------------------------------------

func TestHandleGetTaskStatus_Success(t *testing.T) {
	caller := &fakeVassalCaller{result: `{"task_id":"t-001","status":"running"}`}
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{
		vassals: map[string]*fakeVassalCaller{"worker": caller},
	}

	result, err := srv.handleGetTaskStatus(context.Background(), makeRequest(map[string]any{
		"vassal":  "worker",
		"task_id": "t-001",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
}

func TestHandleGetTaskStatus_NoPool(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleGetTaskStatus(context.Background(), makeRequest(map[string]any{
		"vassal":  "worker",
		"task_id": "t-001",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when pool is nil")
	}
}

// ---------------------------------------------------------------------------
// handleListVassals tests
// ---------------------------------------------------------------------------

func TestHandleListVassals_Empty(t *testing.T) {
	st := newTestStore(t)
	// Insert a kingdom so GetKingdom doesn't fail
	if err := st.CreateKingdom(store.Kingdom{
		ID:        "test-kingdom",
		Name:      "test",
		RootPath:  "/tmp/test",
		Status:    "running",
		CreatedAt: time.Now().Format(time.DateTime),
		UpdatedAt: time.Now().Format(time.DateTime),
	}); err != nil {
		t.Fatalf("CreateKingdom: %v", err)
	}

	srv := newTestServer(t, &fakePTYManager{sessions: map[string]*fakePTYSession{}}, st, &fakeArtifactLedger{})

	result, err := srv.handleListVassals(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["vassals"]; !ok {
		t.Error("expected 'vassals' key in response")
	}
}

func TestHandleListVassals_WithSession(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateKingdom(store.Kingdom{
		ID:        "test-kingdom",
		Name:      "myking",
		RootPath:  "/tmp/test",
		Status:    "running",
		CreatedAt: time.Now().Format(time.DateTime),
		UpdatedAt: time.Now().Format(time.DateTime),
	}); err != nil {
		t.Fatalf("CreateKingdom: %v", err)
	}

	ptyMgr := &fakePTYManager{sessions: map[string]*fakePTYSession{
		"myshell": {output: "hello", exitCode: 0},
	}}

	srv := newTestServer(t, ptyMgr, st, &fakeArtifactLedger{})

	result, err := srv.handleListVassals(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "myshell") {
		t.Errorf("expected 'myshell' in vassals list, got: %s", text)
	}
}

func TestHandleListVassals_WithVassalPool(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateKingdom(store.Kingdom{
		ID:        "test-kingdom",
		Name:      "myking",
		RootPath:  "/tmp/test",
		Status:    "running",
		CreatedAt: time.Now().Format(time.DateTime),
		UpdatedAt: time.Now().Format(time.DateTime),
	}); err != nil {
		t.Fatalf("CreateKingdom: %v", err)
	}

	srv := newTestServer(t, &fakePTYManager{sessions: map[string]*fakePTYSession{}}, st, &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{
		vassals: map[string]*fakeVassalCaller{
			"claude-worker": {result: "ok"},
		},
	}

	result, err := srv.handleListVassals(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "claude-worker") {
		t.Errorf("expected 'claude-worker' in vassals list, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// handleExecIn tests
// ---------------------------------------------------------------------------

func TestHandleExecIn_VassalNotFound(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{sessions: map[string]*fakePTYSession{}}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleExecIn(context.Background(), makeRequest(map[string]any{
		"vassal":  "nonexistent",
		"command": "echo hello",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for nonexistent vassal")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "VASSAL_NOT_FOUND") {
		t.Errorf("expected VASSAL_NOT_FOUND, got: %s", text)
	}
}

func TestHandleExecIn_ClaudeVassalError(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{sessions: map[string]*fakePTYSession{}}, newTestStore(t), &fakeArtifactLedger{})
	srv.vassalPool = &fakeVassalPool{
		vassals: map[string]*fakeVassalCaller{"claude1": {result: "ok"}},
	}

	result, err := srv.handleExecIn(context.Background(), makeRequest(map[string]any{
		"vassal":  "claude1",
		"command": "echo hello",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for claude vassal exec_in")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "dispatch_task") {
		t.Errorf("expected 'dispatch_task' hint in error, got: %s", text)
	}
}

func TestHandleExecIn_Success(t *testing.T) {
	session := &fakePTYSession{output: "hello world\n", exitCode: 0}
	ptyMgr := &fakePTYManager{sessions: map[string]*fakePTYSession{"myshell": session}}
	srv := newTestServer(t, ptyMgr, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleExecIn(context.Background(), makeRequest(map[string]any{
		"vassal":          "myshell",
		"command":         "echo hello",
		"timeout_seconds": 10.0,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["exit_code"]; !ok {
		t.Error("expected 'exit_code' in response")
	}
}

func TestHandleExecIn_MissingVassal(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleExecIn(context.Background(), makeRequest(map[string]any{
		"command": "echo hello",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing vassal param")
	}
}

// ---------------------------------------------------------------------------
// handleGetEvents tests
// ---------------------------------------------------------------------------

func TestHandleGetEvents_Empty(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})

	result, err := srv.handleGetEvents(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["events"]; !ok {
		t.Error("expected 'events' key in response")
	}
}

// ---------------------------------------------------------------------------
// startHeartbeat / sendHeartbeat tests
// ---------------------------------------------------------------------------

func TestStartHeartbeat_IdempotentForSameVassal(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	srv.startHeartbeat("myvassal")
	srv.startHeartbeat("myvassal") // second call should be a no-op

	srv.heartbeatMu.Lock()
	count := len(srv.activeHeartbeats)
	srv.heartbeatMu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 active heartbeat, got %d", count)
	}

	// Cleanup
	srv.heartbeatMu.Lock()
	if cancel, ok := srv.activeHeartbeats["myvassal"]; ok {
		cancel()
		delete(srv.activeHeartbeats, "myvassal")
	}
	srv.heartbeatMu.Unlock()
}

func TestStartHeartbeat_MultipleDifferentVassals(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	srv.startHeartbeat("vassal1")
	srv.startHeartbeat("vassal2")

	srv.heartbeatMu.Lock()
	count := len(srv.activeHeartbeats)
	srv.heartbeatMu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 active heartbeats, got %d", count)
	}

	// Cleanup
	srv.heartbeatMu.Lock()
	for _, cancel := range srv.activeHeartbeats {
		cancel()
	}
	srv.activeHeartbeats = make(map[string]context.CancelFunc)
	srv.heartbeatMu.Unlock()
}

func TestSendHeartbeat_NoParentSocket(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	// Should be a no-op when parentKingdomSocket is empty
	srv.sendHeartbeat("vassal1") // must not panic
}

func TestSendHeartbeat_WithFakeDaemon_Acknowledged(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"delegate_heartbeat": func(params json.RawMessage) (interface{}, string) {
			return map[string]interface{}{"acknowledged": true}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	// Should complete without panic or error
	srv.sendHeartbeat("vassal1")
}

func TestSendHeartbeat_WithFakeDaemon_NotAcknowledged_Redelegates(t *testing.T) {
	redelegateCalled := false
	handlers := map[string]HandlerFunc{
		"delegate_heartbeat": func(params json.RawMessage) (interface{}, string) {
			return map[string]interface{}{"acknowledged": false}, ""
		},
		"delegate_control": func(params json.RawMessage) (interface{}, string) {
			redelegateCalled = true
			return map[string]interface{}{"delegated": true}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	srv.parentKingdomSocket = sockPath

	srv.sendHeartbeat("vassal1")

	if !redelegateCalled {
		t.Error("expected delegate_control to be called on re-delegation after heartbeat loss")
	}
}

// ---------------------------------------------------------------------------
// handleGetAuditLog tests
// ---------------------------------------------------------------------------

func TestHandleGetAuditLog_Empty(t *testing.T) {
	st := newTestStore(t)
	srv := newTestServer(t, &fakePTYManager{}, st, &fakeArtifactLedger{})

	result, err := srv.handleGetAuditLog(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
}

func TestHandleGetAuditLog_InvalidLayer(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleGetAuditLog(context.Background(), makeRequest(map[string]any{
		"layer": "invalid_layer",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid layer")
	}
}

// ---------------------------------------------------------------------------
// handleRespondApproval tests
// ---------------------------------------------------------------------------

func TestHandleRespondApproval_ApprovalDisabled(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})
	// sovereignApproval defaults to false

	result, err := srv.handleRespondApproval(context.Background(), makeRequest(map[string]any{
		"request_id": "req-001",
		"approved":   true,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when sovereign_approval is disabled")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "sovereign_approval is not enabled") {
		t.Errorf("expected 'sovereign_approval is not enabled', got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// handleReadNeighbor tests
// ---------------------------------------------------------------------------

func TestHandleReadNeighbor_PathOutsideRoot(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleReadNeighbor(context.Background(), makeRequest(map[string]any{
		"path": "/etc/passwd",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for path outside root")
	}
	text := extractText(t, result)
	if !strings.Contains(text, "PERMISSION_DENIED") {
		t.Errorf("expected PERMISSION_DENIED, got: %s", text)
	}
}

func TestHandleReadNeighbor_FileInsideRoot(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	// Create a file inside the root dir
	testFile := srv.rootDir + "/test.txt"
	if err := os.WriteFile(testFile, []byte("hello from neighbor\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := srv.handleReadNeighbor(context.Background(), makeRequest(map[string]any{
		"path": testFile,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "hello from neighbor") {
		t.Errorf("expected file content in response, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// newMCPDaemonClient tests
// ---------------------------------------------------------------------------

func TestNewMCPDaemonClient_SocketNotExist(t *testing.T) {
	_, err := newMCPDaemonClient("/tmp/nonexistent-king-test.sock")
	if err == nil {
		t.Fatal("expected error connecting to nonexistent socket")
	}
}

func TestNewMCPDaemonClient_WithFakeDaemon(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"ping": func(params json.RawMessage) (interface{}, string) {
			return map[string]interface{}{"pong": true}, ""
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	client, err := newMCPDaemonClient(sockPath)
	if err != nil {
		t.Fatalf("newMCPDaemonClient: %v", err)
	}
	defer client.Close()

	raw, err := client.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["pong"] != true {
		t.Errorf("expected pong=true, got %v", resp)
	}
}

func TestDaemonClient_RpcError(t *testing.T) {
	handlers := map[string]HandlerFunc{
		"fail": func(params json.RawMessage) (interface{}, string) {
			return nil, "something went wrong"
		},
	}
	sockPath := startFakeDaemon(t, handlers)

	client, err := newMCPDaemonClient(sockPath)
	if err != nil {
		t.Fatalf("newMCPDaemonClient: %v", err)
	}
	defer client.Close()

	_, err = client.Call("fail", nil)
	if err == nil {
		t.Fatal("expected error from RPC error response")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("expected 'something went wrong' in error, got: %v", err)
	}
}

func TestDaemonClient_UnknownMethod(t *testing.T) {
	handlers := map[string]HandlerFunc{}
	sockPath := startFakeDaemon(t, handlers)

	client, err := newMCPDaemonClient(sockPath)
	if err != nil {
		t.Fatalf("newMCPDaemonClient: %v", err)
	}
	defer client.Close()

	_, err = client.Call("no_such_method", nil)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
}

// ---------------------------------------------------------------------------
// handleRegisterArtifact / handleResolveArtifact tests
// ---------------------------------------------------------------------------

func TestHandleRegisterArtifact_Success(t *testing.T) {
	ledger := &fakeArtifactLedger{}
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), ledger)

	result, err := srv.handleRegisterArtifact(context.Background(), makeRequest(map[string]any{
		"name":      "my-artifact",
		"file_path": "/tmp/output.json",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
}

func TestHandleRegisterArtifact_MissingName(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleRegisterArtifact(context.Background(), makeRequest(map[string]any{
		"file_path": "/tmp/output.json",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing name")
	}
}

func TestHandleResolveArtifact_MissingName(t *testing.T) {
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), &fakeArtifactLedger{})

	result, err := srv.handleResolveArtifact(context.Background(), makeRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing name")
	}
}

func TestHandleResolveArtifact_Success(t *testing.T) {
	ledger := &fakeArtifactLedger{
		artifact: &store.Artifact{Name: "my-art", FilePath: "/tmp/a.json", Version: 1},
	}
	srv := newTestServer(t, &fakePTYManager{}, newTestStore(t), ledger)

	result, err := srv.handleResolveArtifact(context.Background(), makeRequest(map[string]any{
		"name": "my-art",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %v", extractText(t, result))
	}
	text := extractText(t, result)
	if !strings.Contains(text, "my-art") {
		t.Errorf("expected artifact name in response, got: %s", text)
	}
}

// ---------------------------------------------------------------------------
// extractText is a helper to get the text content from a CallToolResult.
// ---------------------------------------------------------------------------

func extractText(t *testing.T, result *mcpgo.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	for _, c := range result.Content {
		if tc, ok := c.(mcpgo.TextContent); ok {
			return tc.Text
		}
	}
	// Fallback: marshal content
	b, _ := json.Marshal(result.Content)
	return string(b)
}
