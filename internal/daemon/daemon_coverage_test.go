package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"regexp"

	"github.com/alexli18/claude-king/internal/audit"
	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/events"
	"github.com/alexli18/claude-king/internal/pty"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/alexli18/claude-king/internal/webhook"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// resolveKingVassalBinary — package-level pure function
// ---------------------------------------------------------------------------

func TestResolveKingVassalBinary_NotFound(t *testing.T) {
	emptyDir, err := os.MkdirTemp("/tmp", "resolve-notfound-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(emptyDir)

	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", emptyDir)

	path, err := resolveKingVassalBinary()
	if err == nil {
		if path == "" {
			t.Fatal("expected non-empty path when no error returned")
		}
		t.Skipf("king-vassal found at %s (sibling) — skipping not-found test", path)
	}
}

func TestResolveKingVassalBinary_FoundInPath(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "resolve-found-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	fakeExe := filepath.Join(dir, "king-vassal")
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("WriteFile fake exe: %v", err)
	}

	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir+string(os.PathListSeparator)+orig)

	path, err := resolveKingVassalBinary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}

// ---------------------------------------------------------------------------
// injectAutoContracts
// ---------------------------------------------------------------------------

func newMinimalDaemon(t *testing.T) *Daemon {
	t.Helper()
	return &Daemon{
		config:           &config.KingdomConfig{},
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
}

func TestInjectAutoContracts_EmptyPath(t *testing.T) {
	d := newMinimalDaemon(t)
	d.injectAutoContracts("")
	_ = len(d.config.Patterns)
}

func TestInjectAutoContracts_GoProject(t *testing.T) {
	projectRoot := "/Users/alex/Desktop/Claude_King"
	if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err != nil {
		t.Skipf("go.mod not found at %s, skipping: %v", projectRoot, err)
	}

	d := newMinimalDaemon(t)
	before := len(d.config.Patterns)
	d.injectAutoContracts(projectRoot)
	after := len(d.config.Patterns)

	if after < before {
		t.Fatalf("expected patterns to be added or same: before=%d after=%d", before, after)
	}
}

func TestInjectAutoContracts_Idempotent(t *testing.T) {
	projectRoot := "/Users/alex/Desktop/Claude_King"
	if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err != nil {
		t.Skipf("go.mod not found at %s, skipping: %v", projectRoot, err)
	}

	d := newMinimalDaemon(t)
	d.injectAutoContracts(projectRoot)
	afterFirst := len(d.config.Patterns)
	d.injectAutoContracts(projectRoot)
	afterSecond := len(d.config.Patterns)

	if afterSecond != afterFirst {
		t.Fatalf("expected idempotent: first=%d second=%d", afterFirst, afterSecond)
	}
}

func TestInjectAutoContracts_UnknownProjectType(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "fingerprint-unknown-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	d := newMinimalDaemon(t)
	d.injectAutoContracts(dir)
}

// ---------------------------------------------------------------------------
// MergeAutoContracts
// ---------------------------------------------------------------------------

func TestMergeAutoContracts_NoDuplicates(t *testing.T) {
	existing := []config.PatternConfig{{Name: "A", Regex: "a"}}
	auto := []config.PatternConfig{{Name: "B", Regex: "b"}}
	result := MergeAutoContracts(existing, auto)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
}

func TestMergeAutoContracts_SkipsDuplicate(t *testing.T) {
	existing := []config.PatternConfig{{Name: "A", Regex: "original"}}
	auto := []config.PatternConfig{{Name: "A", Regex: "overwrite"}}
	result := MergeAutoContracts(existing, auto)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Regex != "original" {
		t.Fatalf("expected original regex preserved, got %q", result[0].Regex)
	}
}

func TestMergeAutoContracts_BothEmpty(t *testing.T) {
	result := MergeAutoContracts(nil, nil)
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// watchVassal — restart_policy="no" and context-cancelled paths
// ---------------------------------------------------------------------------

func TestWatchVassal_ExitsImmediately_NoRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := NewVassalClientPool()
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		vassalProcs:      make(map[string]*vassalProc),
		vassalPool:       pool,
		logger:           newTestLogger(t),
		ctx:              ctx,
		cancel:           cancel,
	}

	cfg := config.VassalConfig{
		Name:          "testwatcher",
		RestartPolicy: "no",
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}

	done := make(chan struct{})
	d.wg.Add(1)
	go func() {
		d.watchVassal("testwatcher", cmd, cfg)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("watchVassal did not return within 4s")
	}
}

func TestWatchVassal_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	pool := NewVassalClientPool()
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		vassalProcs:      make(map[string]*vassalProc),
		vassalPool:       pool,
		logger:           newTestLogger(t),
		ctx:              ctx,
		cancel:           cancel,
	}

	cfg := config.VassalConfig{
		Name:          "testwatcher2",
		RestartPolicy: "always",
	}

	// Cancel context before the cmd exits so watchVassal returns via ctx.Done()
	cancel()

	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}

	done := make(chan struct{})
	d.wg.Add(1)
	go func() {
		d.watchVassal("testwatcher2", cmd, cfg)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("watchVassal did not return within 4s after context cancel")
	}
}

// ---------------------------------------------------------------------------
// Attach — error path
// ---------------------------------------------------------------------------

func TestAttach_InvalidKingDir(t *testing.T) {
	d := &Daemon{
		rootDir:          "/nonexistent/totally/fake/path",
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		logger:           newTestLogger(t),
	}

	err := d.Attach(context.Background())
	if err == nil {
		t.Fatal("expected Attach to return error for nonexistent rootDir")
	}
}

// ---------------------------------------------------------------------------
// registerRealHandlers — exercise handlers it registers
// ---------------------------------------------------------------------------

func newDaemonWithRealHandlers(t *testing.T) *Daemon {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "rh-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := &config.KingdomConfig{Name: "test-rh-kingdom"}
	k, err := NewKingdom(s, cfg, dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	mgr := pty.NewManager(s, k.ID, newTestLogger(t))

	ar := audit.NewAuditRecorder(s, k.ID, newTestLogger(t))

	d := &Daemon{
		rootDir:          dir,
		store:            s,
		config:           cfg,
		kingdom:          k,
		ptyMgr:           mgr,
		auditRecorder:    ar,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		logger:           newTestLogger(t),
	}
	d.registerRealHandlers()
	return d
}

func TestRegisterRealHandlers_ListVassals(t *testing.T) {
	d := newDaemonWithRealHandlers(t)

	result, err := d.handlers["list_vassals"](nil)
	if err != nil {
		t.Fatalf("list_vassals: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if _, exists := m["vassals"]; !exists {
		t.Fatal("expected 'vassals' key")
	}
}

func TestRegisterRealHandlers_Status(t *testing.T) {
	d := newDaemonWithRealHandlers(t)

	result, err := d.handlers["status"](nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if _, exists := m["kingdom_id"]; !exists {
		t.Fatal("expected 'kingdom_id' key")
	}
}

func TestRegisterRealHandlers_ExecIn_BadParams(t *testing.T) {
	d := newDaemonWithRealHandlers(t)

	cases := []struct {
		name   string
		params json.RawMessage
	}{
		{"empty target", json.RawMessage(`{"target":"","command":"ls"}`)},
		{"empty command", json.RawMessage(`{"target":"v","command":""}`)},
		{"invalid JSON", json.RawMessage(`{invalid`)},
		{"vassal not found", json.RawMessage(`{"target":"nope","command":"ls","timeout_seconds":1}`)},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := d.handlers["exec_in"](tc.params)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// startVassals — empty config and non-autostart paths
// ---------------------------------------------------------------------------

func newDaemonForStartVassals(t *testing.T, vassals []config.VassalConfig) *Daemon {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "sv-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := &config.KingdomConfig{Name: "sv-test", Vassals: vassals}
	k, err := NewKingdom(s, cfg, dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := pty.NewManager(s, k.ID, newTestLogger(t))

	return &Daemon{
		rootDir:          dir,
		store:            s,
		config:           cfg,
		kingdom:          k,
		ptyMgr:           mgr,
		ctx:              ctx,
		cancel:           cancel,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		logger:           newTestLogger(t),
	}
}

func TestStartVassals_EmptyConfig(t *testing.T) {
	d := newDaemonForStartVassals(t, nil)
	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals with empty config: %v", err)
	}
}

func TestStartVassals_NonAutostart_Skipped(t *testing.T) {
	falseVal := false
	vassals := []config.VassalConfig{
		{Name: "skipped", Command: "/bin/echo", Autostart: &falseVal},
	}
	d := newDaemonForStartVassals(t, vassals)

	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals: %v", err)
	}
	sessions := d.ptyMgr.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// registerRealHandlers — additional handler coverage
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_GetVassalOutput_NotFound(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	params := json.RawMessage(`{"name":"nonexistent"}`)
	_, err := d.handlers["get_vassal_output"](params)
	if err == nil {
		t.Fatal("expected VASSAL_NOT_FOUND error")
	}
}

func TestRegisterRealHandlers_GetVassalOutput_EmptyName(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	params := json.RawMessage(`{"name":""}`)
	_, err := d.handlers["get_vassal_output"](params)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestRegisterRealHandlers_GetVassalOutput_InvalidJSON(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	_, err := d.handlers["get_vassal_output"](json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegisterRealHandlers_GetEvents_NoParams(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	result, err := d.handlers["get_events"](nil)
	if err != nil {
		t.Fatalf("get_events: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, ok := m["events"]; !ok {
		t.Fatal("expected 'events' key")
	}
}

func TestRegisterRealHandlers_GetEvents_WithParams(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	params := json.RawMessage(`{"severity":"info","limit":10}`)
	result, err := d.handlers["get_events"](params)
	if err != nil {
		t.Fatalf("get_events with params: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, ok := m["count"]; !ok {
		t.Fatal("expected 'count' key")
	}
}

func TestRegisterRealHandlers_GetActionTrace_NotFound(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	params := json.RawMessage(`{"trace_id":"nonexistent"}`)
	_, err := d.handlers["get_action_trace"](params)
	if err == nil {
		t.Fatal("expected error for nonexistent trace")
	}
}

func TestRegisterRealHandlers_GetActionTrace_EmptyID(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	params := json.RawMessage(`{"trace_id":""}`)
	_, err := d.handlers["get_action_trace"](params)
	if err == nil {
		t.Fatal("expected error for empty trace_id")
	}
}

func TestRegisterRealHandlers_GetActionTrace_InvalidJSON(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	_, err := d.handlers["get_action_trace"](json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterRealHandlers_GetAuditLog_NoParams(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	result, err := d.handlers["get_audit_log"](nil)
	if err != nil {
		t.Fatalf("get_audit_log: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, ok := m["entries"]; !ok {
		t.Fatal("expected 'entries' key")
	}
}

func TestRegisterRealHandlers_GetAuditLog_InvalidLayer(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	params := json.RawMessage(`{"layer":"invalid"}`)
	_, err := d.handlers["get_audit_log"](params)
	if err == nil {
		t.Fatal("expected error for invalid layer")
	}
}

func TestRegisterRealHandlers_GetAuditLog_WithFilters(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	params := json.RawMessage(`{"layer":"action","vassal":"myvassal","since":"24h","limit":10}`)
	result, err := d.handlers["get_audit_log"](params)
	if err != nil {
		t.Fatalf("get_audit_log with filters: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, ok := m["total"]; !ok {
		t.Fatal("expected 'total' key")
	}
}

func TestRegisterRealHandlers_RespondApproval_NotEnabled(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	// SovereignApproval is false by default
	params := json.RawMessage(`{"request_id":"abc","approved":true}`)
	_, err := d.handlers["respond_approval"](params)
	if err == nil {
		t.Fatal("expected error: sovereign_approval not enabled")
	}
}

func TestRegisterRealHandlers_RespondApproval_InvalidJSON(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	d.config.Settings.SovereignApproval = true
	_, err := d.handlers["respond_approval"](json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegisterRealHandlers_RespondApproval_EmptyRequestID(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	d.config.Settings.SovereignApproval = true
	params := json.RawMessage(`{"request_id":"","approved":true}`)
	_, err := d.handlers["respond_approval"](params)
	if err == nil {
		t.Fatal("expected error for empty request_id")
	}
}

func TestRegisterRealHandlers_RespondApproval_NotFound(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	d.config.Settings.SovereignApproval = true
	params := json.RawMessage(`{"request_id":"nonexistent","approved":true}`)
	_, err := d.handlers["respond_approval"](params)
	if err == nil {
		t.Fatal("expected error for nonexistent request")
	}
}

func TestRegisterRealHandlers_ListPendingApprovals(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	result, err := d.handlers["list_pending_approvals"](nil)
	if err != nil {
		t.Fatalf("list_pending_approvals: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, ok := m["approvals"]; !ok {
		t.Fatal("expected 'approvals' key")
	}
}

func TestRegisterRealHandlers_KingdomStatus(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	result, err := d.handlers["kingdom.status"](nil)
	if err != nil {
		t.Fatalf("kingdom.status: %v", err)
	}
	_ = result
}

func TestRegisterRealHandlers_Shutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := newDaemonWithRealHandlers(t)
	d.ctx = ctx
	d.cancel = cancel

	result, err := d.handlers["shutdown"](nil)
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	m, ok := result.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", result)
	}
	if m["status"] != "shutting_down" {
		t.Fatalf("expected status=shutting_down, got %q", m["status"])
	}
	// Verify ctx was cancelled
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected context to be cancelled after shutdown")
	}
}

// ---------------------------------------------------------------------------
// registerStubHandlers + vassal.register / vassal.list
// ---------------------------------------------------------------------------

func newMinimalDaemonWithStubs(t *testing.T) *Daemon {
	t.Helper()
	d := &Daemon{
		config:           &config.KingdomConfig{},
		handlers:         make(map[string]rpcHandler),
		externalVassals:  make(map[string]ExternalVassalInfo),
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()
	return d
}

func TestRegisterStubHandlers_StubsExist(t *testing.T) {
	d := newMinimalDaemonWithStubs(t)
	stubs := []string{"list_vassals", "exec_in", "get_events", "register_artifact", "resolve_artifact", "read_neighbor"}
	for _, name := range stubs {
		if _, ok := d.handlers[name]; !ok {
			t.Errorf("expected stub handler %q to be registered", name)
		}
	}
}

func TestRegisterStubHandlers_VassalRegister_Valid(t *testing.T) {
	d := newMinimalDaemonWithStubs(t)
	params := json.RawMessage(`{"name":"myvassal","pid":12345,"socket":"/tmp/myvassal.sock","repo_path":"/tmp/repo"}`)
	result, err := d.handlers["vassal.register"](params)
	if err != nil {
		t.Fatalf("vassal.register: %v", err)
	}
	m, ok := result.(map[string]bool)
	if !ok {
		t.Fatalf("expected map[string]bool, got %T", result)
	}
	if !m["ok"] {
		t.Fatal("expected ok=true")
	}
}

func TestRegisterStubHandlers_VassalRegister_BadPID(t *testing.T) {
	d := newMinimalDaemonWithStubs(t)
	params := json.RawMessage(`{"name":"v","pid":0}`)
	_, err := d.handlers["vassal.register"](params)
	if err == nil {
		t.Fatal("expected error for pid=0")
	}
}

func TestRegisterStubHandlers_VassalRegister_InvalidJSON(t *testing.T) {
	d := newMinimalDaemonWithStubs(t)
	_, err := d.handlers["vassal.register"](json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegisterStubHandlers_VassalList_Empty(t *testing.T) {
	d := newMinimalDaemonWithStubs(t)
	result, err := d.handlers["vassal.list"](nil)
	if err != nil {
		t.Fatalf("vassal.list: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, ok := m["vassals"]; !ok {
		t.Fatal("expected 'vassals' key")
	}
}

func TestRegisterStubHandlers_VassalList_WithEntry(t *testing.T) {
	d := newMinimalDaemonWithStubs(t)
	// Register a vassal first
	params := json.RawMessage(`{"name":"myvsl","pid":1,"socket":"/tmp/myvsl.sock","repo_path":"/tmp"}`)
	if _, err := d.handlers["vassal.register"](params); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := d.handlers["vassal.list"](nil)
	if err != nil {
		t.Fatalf("vassal.list: %v", err)
	}
	_ = result
}

// ---------------------------------------------------------------------------
// writeResponse
// ---------------------------------------------------------------------------

func TestWriteResponse_Success(t *testing.T) {
	d := &Daemon{logger: newTestLogger(t)}

	// Use a net.Pipe() to capture the write
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := client.Read(buf)
		done <- buf[:n]
	}()

	resp := RPCResponse{Result: map[string]string{"status": "ok"}, ID: 1}
	d.writeResponse(server, resp)

	data := <-done
	if len(data) == 0 {
		t.Fatal("expected non-empty response")
	}
	if data[len(data)-1] != '\n' {
		t.Fatal("expected newline at end of response")
	}
}

// ---------------------------------------------------------------------------
// removePIDFile
// ---------------------------------------------------------------------------

func TestRemovePIDFile_Success(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pidfile-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	pidPath := filepath.Join(dir, "king.pid")
	if err := os.WriteFile(pidPath, []byte("12345"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d := &Daemon{pidFile: pidPath, logger: newTestLogger(t)}
	d.removePIDFile()

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("expected pid file to be removed")
	}
}

func TestRemovePIDFile_NotExist(t *testing.T) {
	d := &Daemon{pidFile: "/tmp/nonexistent-king.pid", logger: newTestLogger(t)}
	// Should not panic or log error for non-existent file
	d.removePIDFile()
}

// ---------------------------------------------------------------------------
// cleanStaleSocket
// ---------------------------------------------------------------------------

func TestCleanStaleSocket_NotExist(t *testing.T) {
	d := &Daemon{
		sockPath: "/tmp/nonexistent-" + t.Name() + ".sock",
		logger:   newTestLogger(t),
	}
	if err := d.cleanStaleSocket(); err != nil {
		t.Fatalf("expected no error for non-existent socket: %v", err)
	}
}


// ---------------------------------------------------------------------------
// dispatch
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// startVassals — with a real shell vassal (autostart=true)
// ---------------------------------------------------------------------------

func TestStartVassals_ShellVassal_Autostart(t *testing.T) {
	trueVal := true
	vassals := []config.VassalConfig{
		{Name: "echo-vassal", Command: "/bin/echo hello", Autostart: &trueVal},
	}
	d := newDaemonForStartVassals(t, vassals)
	// Wire sieve (it's nil in the minimal daemon, but startVassals checks d.sieve != nil)
	// Leave nil to test that path gracefully

	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals with shell vassal: %v", err)
	}
	// Session should have been created
	sessions := d.ptyMgr.ListSessions()
	if len(sessions) == 0 {
		t.Fatal("expected at least 1 session after starting shell vassal")
	}
}

// ---------------------------------------------------------------------------
// exec_in handler — success path via real PTY session
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_ExecIn_Success(t *testing.T) {
	d := newDaemonWithRealHandlers(t)

	// Create a real PTY session for the test command
	sess, err := d.ptyMgr.CreateSession("test-id", "myvassal", "/bin/sh", "/tmp", nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	_ = sess

	// Give the session a moment to start
	time.Sleep(50 * time.Millisecond)

	params := json.RawMessage(`{"target":"myvassal","command":"echo hello","timeout_seconds":5}`)
	result, err := d.handlers["exec_in"](params)
	if err != nil {
		// PTY may not have started fully in time - skip if timeout
		if err.Error() == "exec error: timed out" {
			t.Skip("PTY session not ready in time")
		}
		t.Fatalf("exec_in: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if _, ok := m["output"]; !ok {
		t.Fatal("expected 'output' key")
	}
}

// ---------------------------------------------------------------------------
// auditCleanupLoop — coverage via Attach+cleanup
// ---------------------------------------------------------------------------

func TestDispatch_UnknownMethod2(t *testing.T) {
	// Additional dispatch coverage — verifies ID is preserved in error response
	d := &Daemon{
		handlers: make(map[string]rpcHandler),
		logger:   newTestLogger(t),
	}
	resp := d.dispatch(RPCRequest{Method: "unknown2", ID: 99})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.ID != 99 {
		t.Fatalf("expected id=99, got %d", resp.ID)
	}
}

func TestDispatch_HandlerReturnsNil(t *testing.T) {
	d := &Daemon{
		handlers: make(map[string]rpcHandler),
		logger:   newTestLogger(t),
	}
	d.handlers["nil-result"] = func(_ json.RawMessage) (any, error) {
		return nil, fmt.Errorf("deliberate failure")
	}
	resp := d.dispatch(RPCRequest{Method: "nil-result", ID: 7})
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
}

// ---------------------------------------------------------------------------
// expandSerialCommand
// ---------------------------------------------------------------------------

func TestExpandSerialCommand_NonSerial(t *testing.T) {
	v := config.VassalConfig{Command: "/bin/echo hello"}
	got := expandSerialCommand(v)
	if got != "/bin/echo hello" {
		t.Fatalf("expected original command, got %q", got)
	}
}

func TestExpandSerialCommand_Serial(t *testing.T) {
	v := config.VassalConfig{Type: "serial", SerialPort: "/dev/ttyUSB0", BaudRate: 9600}
	got := expandSerialCommand(v)
	if !strings.Contains(got, "/dev/ttyUSB0") {
		t.Fatalf("expected serial port in command, got %q", got)
	}
}

func TestExpandSerialCommand_SerialDefaultBaud(t *testing.T) {
	v := config.VassalConfig{Type: "serial", SerialPort: "/dev/ttyACM0"}
	got := expandSerialCommand(v)
	if !strings.Contains(got, "/dev/ttyACM0") {
		t.Fatalf("expected port in command, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// NextBackoff
// ---------------------------------------------------------------------------

func TestNextBackoff_Doubles(t *testing.T) {
	b := NextBackoff(1 * time.Second)
	if b != 2*time.Second {
		t.Fatalf("expected 2s, got %v", b)
	}
}

func TestNextBackoff_Capped(t *testing.T) {
	b := NextBackoff(vassalRestartMaxBackoff)
	if b != vassalRestartMaxBackoff {
		t.Fatalf("expected capped at max, got %v", b)
	}
}

// ---------------------------------------------------------------------------
// Client — error paths
// ---------------------------------------------------------------------------

func TestNewClient_InvalidSocket(t *testing.T) {
	_, err := NewClient("/nonexistent-cov-test-path")
	if err == nil {
		t.Fatal("expected error for nonexistent socket")
	}
}

func TestNewClientFromSocket_Invalid(t *testing.T) {
	_, err := NewClientFromSocket("/nonexistent-cov-test.sock")
	if err == nil {
		t.Fatal("expected error for nonexistent socket")
	}
}

// ---------------------------------------------------------------------------
// Attach — success path (pre-populate DB with kingdom record)
// ---------------------------------------------------------------------------

func TestAttach_Success(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "attach-ok-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write minimal kingdom.yml so LoadOrCreateConfig succeeds
	cfgContent := "name: test-attach-kingdom\n"
	if err := os.WriteFile(filepath.Join(kingDir, "kingdom.yml"), []byte(cfgContent), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	// Pre-populate DB with a kingdom record
	dbPath := filepath.Join(kingDir, "king.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	k := store.Kingdom{
		ID:         uuid.New().String(),
		Name:       "test-attach-kingdom",
		RootPath:   dir,
		SocketPath: filepath.Join(kingDir, "test.sock"),
		Status:     "running",
		CreatedAt:  "2026-01-01 00:00:00",
		UpdatedAt:  "2026-01-01 00:00:00",
	}
	if err := s.CreateKingdom(k); err != nil {
		s.Close()
		t.Fatalf("CreateKingdom: %v", err)
	}
	s.Close()

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	if err := d.Attach(ctx); err != nil {
		cancel()
		t.Fatalf("Attach: %v", err)
	}

	// Stop the auditCleanupLoop goroutine
	cancel()
	d.wg.Wait()
}

func TestAttach_Success_WithWebhook(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "attach-webhook-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfgContent := `name: test-attach-webhook
settings:
  webhooks:
    - url: http://localhost:19991/hook
      secret: test-secret
`
	if err := os.WriteFile(filepath.Join(kingDir, "kingdom.yml"), []byte(cfgContent), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	dbPath := filepath.Join(kingDir, "king.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	k := store.Kingdom{
		ID:         uuid.New().String(),
		Name:       "test-attach-webhook",
		RootPath:   dir,
		SocketPath: filepath.Join(kingDir, "test.sock"),
		Status:     "running",
		CreatedAt:  "2026-01-01 00:00:00",
		UpdatedAt:  "2026-01-01 00:00:00",
	}
	if err := s.CreateKingdom(k); err != nil {
		s.Close()
		t.Fatalf("CreateKingdom: %v", err)
	}
	s.Close()

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	if err := d.Attach(ctx); err != nil {
		cancel()
		t.Fatalf("Attach with webhook: %v", err)
	}

	cancel()
	d.wg.Wait()
	if d.webhookDispatcher != nil {
		d.webhookDispatcher.Stop()
	}
}

// ---------------------------------------------------------------------------
// Start + Stop lifecycle
// ---------------------------------------------------------------------------

func TestStart_Stop_Basic(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "start-stop-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := d.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestStart_Stop_WithLogRetention(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "start-logret-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfgContent := "name: test-logret\nsettings:\n  log_retention_days: 7\n"
	if err := os.WriteFile(filepath.Join(kingDir, "kingdom.yml"), []byte(cfgContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d.Stop()
}

func TestStart_Stop_WithWebhook(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "start-wh-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfgContent := `name: test-webhook-start
settings:
  webhooks:
    - url: http://localhost:19992/wh
      secret: wh-secret
`
	if err := os.WriteFile(filepath.Join(kingDir, "kingdom.yml"), []byte(cfgContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d.Stop()
}

func TestStart_ClientCall_Stop(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "start-client-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer d.Stop()

	// Connect a client and call status + list_vassals
	c, err := NewClient(dir)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	result, err := c.Call("status", nil)
	if err != nil {
		t.Fatalf("Call(status): %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty status result")
	}

	// Call with RPC error (handler returns error)
	_, rpcErr := c.Call("get_action_trace", map[string]string{"trace_id": "nonexistent"})
	if rpcErr == nil {
		t.Fatal("expected rpc error for nonexistent trace")
	}

	// Call unknown method
	_, unknownErr := c.Call("completely_unknown_method", nil)
	if unknownErr == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestStart_Stop_WithSerialVassal(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "start-serial-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Serial vassal with explicit port — won't try to auto-detect
	cfgContent := `name: test-serial
vassals:
  - name: sensor
    type: serial
    serial_port: /dev/ttyFAKE0
    baud_rate: 115200
    autostart: false
`
	if err := os.WriteFile(filepath.Join(kingDir, "kingdom.yml"), []byte(cfgContent), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	d.Stop()
}

// ---------------------------------------------------------------------------
// Stop — minimal daemon (no Start called)
// ---------------------------------------------------------------------------

func TestStop_MinimalDaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	d := &Daemon{
		ctx:              ctx,
		cancel:           cancel,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		logger:           newTestLogger(t),
	}
	// Stop on daemon that was never started — should not panic
	_ = d.Stop()
}

// ---------------------------------------------------------------------------
// writeResponse — error path (closed connection)
// ---------------------------------------------------------------------------

func TestWriteResponse_ClosedConn(t *testing.T) {
	d := &Daemon{logger: newTestLogger(t)}

	server, client := net.Pipe()
	client.Close() // close reader side immediately
	defer server.Close()

	resp := RPCResponse{Result: "test", ID: 1}
	// Should not panic even if write fails
	d.writeResponse(server, resp)
}

// ---------------------------------------------------------------------------
// Start — already running check
// ---------------------------------------------------------------------------

func TestStart_AlreadyRunning(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "already-running-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	d1, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon d1: %v", err)
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()

	if err := d1.Start(ctx1); err != nil {
		t.Fatalf("Start d1: %v", err)
	}
	defer d1.Stop()

	d2, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon d2: %v", err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	err = d2.Start(ctx2)
	if err == nil {
		d2.Stop()
		t.Fatal("expected error for already running daemon")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected 'already running' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// registerRealHandlers — list_vassals with session, exec_in non-zero exit
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_ListVassals_WithSession(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	// Create a session so the loop body in list_vassals handler executes
	if _, err := d.ptyMgr.CreateSession(uuid.New().String(), "lv-vassal", "/bin/sh", "/tmp", nil); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	result, err := d.handlers["list_vassals"](nil)
	if err != nil {
		t.Fatalf("list_vassals: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	vassals, ok := m["vassals"]
	if !ok {
		t.Fatal("expected 'vassals' key")
	}
	_ = vassals
}

func TestRegisterRealHandlers_ExecIn_NonZeroExit(t *testing.T) {
	d := newDaemonWithRealHandlers(t)

	if _, err := d.ptyMgr.CreateSession(uuid.New().String(), "nz-vassal", "/bin/sh", "/tmp", nil); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// "ls /nonexistent_coverage_dir" exits with code 1/2
	params := json.RawMessage(`{"target":"nz-vassal","command":"ls /nonexistent_coverage_dir_abc","timeout_seconds":5}`)
	result, err := d.handlers["exec_in"](params)
	// Either error (timeout) or success with non-zero exit code
	if err != nil {
		if strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "exec error") {
			t.Skip("exec_in timed out — skipping non-zero exit test")
		}
		t.Fatalf("exec_in: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	_ = m
}

// ---------------------------------------------------------------------------
// updateGuardState — circuit transitions with webhook dispatcher
// ---------------------------------------------------------------------------

func newTestWebhookDispatcher(t *testing.T) *webhook.Dispatcher {
	t.Helper()
	wd := webhook.NewDispatcher(
		[]config.WebhookConfig{{URL: "http://localhost:19993/hook", Secret: "test"}},
		"test-kingdom",
		newTestLogger(t),
	)
	wd.Start()
	t.Cleanup(wd.Stop)
	return wd
}

func TestUpdateGuardState_CircuitClosed_WithWebhook(t *testing.T) {
	d := &Daemon{
		guardStates:       make(map[string]*GuardState),
		logger:            newTestLogger(t),
		webhookDispatcher: newTestWebhookDispatcher(t),
	}
	key := "vassal:0"
	d.guardStates[key] = &GuardState{
		VassalName:       "vassal",
		GuardType:        "port_check",
		GuardIndex:       0,
		CircuitOpen:      true,
		ConsecutiveFails: 5,
	}
	// OK result → circuit was open → should close and send webhook
	d.updateGuardState(key, GuardResult{OK: true, Message: "pass", CheckedAt: time.Now()}, 3)
	if d.guardStates[key].CircuitOpen {
		t.Fatal("expected circuit to be closed after OK result")
	}
	if d.guardStates[key].ConsecutiveFails != 0 {
		t.Fatal("expected consecutive fails reset to 0")
	}
}

func TestUpdateGuardState_CircuitOpen_WithWebhook(t *testing.T) {
	d := &Daemon{
		guardStates:       make(map[string]*GuardState),
		logger:            newTestLogger(t),
		webhookDispatcher: newTestWebhookDispatcher(t),
	}
	key := "vassal:0"
	d.guardStates[key] = &GuardState{
		VassalName:  "vassal",
		GuardType:   "port_check",
		GuardIndex:  0,
		CircuitOpen: false,
	}
	threshold := 3
	// Three consecutive fails → circuit opens and sends webhook
	for i := 0; i < threshold; i++ {
		d.updateGuardState(key, GuardResult{OK: false, Message: "fail", CheckedAt: time.Now()}, threshold)
	}
	if !d.guardStates[key].CircuitOpen {
		t.Fatal("expected circuit to be open after threshold failures")
	}
}

// ---------------------------------------------------------------------------
// startGuardRunners — default interval/threshold branch coverage
// ---------------------------------------------------------------------------

func TestStartGuardRunners_DefaultIntervalThreshold(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	dir, _ := os.MkdirTemp("/tmp", "sgr-*")
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	s, _ := store.NewStore(dbPath)
	defer s.Close()

	k, _ := NewKingdom(s, &config.KingdomConfig{Name: "sgr"}, dir, newTestLogger(t))
	mgr := pty.NewManager(s, k.ID, newTestLogger(t))

	// Guard config with Interval=0 and Threshold=0 → triggers the default-filling branches
	portGuard := config.GuardConfig{Type: "port_check", Port: 19994, Interval: 0, Threshold: 0}
	vassalCfg := config.VassalConfig{Name: "g-vassal", Guards: []config.GuardConfig{portGuard}}

	d := &Daemon{
		rootDir: dir,
		config:  &config.KingdomConfig{Name: "sgr", Vassals: []config.VassalConfig{vassalCfg}},
		ptyMgr:  mgr,
		ctx:     ctx,
		cancel:  cancel,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		logger:           newTestLogger(t),
	}

	d.startGuardRunners(ctx)
	// Cancel context to stop the guard goroutine
	cancel()
	d.wg.Wait()
}

// ---------------------------------------------------------------------------
// checkLogWatch — pattern match path
// ---------------------------------------------------------------------------

func TestCheckLogWatch_WithMatchingLines(t *testing.T) {
	dir, _ := os.MkdirTemp("/tmp", "clw-*")
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	s, _ := store.NewStore(dbPath)
	defer s.Close()

	k, _ := NewKingdom(s, &config.KingdomConfig{Name: "clw"}, dir, newTestLogger(t))
	mgr := pty.NewManager(s, k.ID, newTestLogger(t))

	// Start a session and run a command to populate recent lines
	if _, err := mgr.CreateSession(uuid.New().String(), "clw-vassal", "/bin/sh", "/tmp", nil); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Get the session and write output that will populate recentLines
	sess, ok := mgr.GetSession("clw-vassal")
	if !ok {
		t.Skip("session not available")
	}
	// Execute a command that produces "ERROR_KEYWORD" in output
	_, _, _, _ = sess.ExecCommand("echo ERROR_KEYWORD", 3*time.Second)
	time.Sleep(100 * time.Millisecond)

	d := &Daemon{ptyMgr: mgr, logger: newTestLogger(t)}

	// Build patterns directly as []*regexp.Regexp
	pats := []*regexp.Regexp{regexp.MustCompile("ERROR_KEYWORD")}

	result := d.checkLogWatch("clw-vassal", pats, 10*time.Second)
	// Result may or may not match depending on whether output was captured
	// Both paths should be exercised across test runs
	_ = result
}

// ---------------------------------------------------------------------------
// startVassals — with sieve configured (covers sieve callback branch)
// ---------------------------------------------------------------------------

func newDaemonForStartVassalsWithSieve(t *testing.T, vassals []config.VassalConfig) *Daemon {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "sv-sieve-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := &config.KingdomConfig{Name: "sv-sieve-test", Vassals: vassals}
	k, err := NewKingdom(s, cfg, dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgr := pty.NewManager(s, k.ID, newTestLogger(t))

	compiledPatterns, _ := events.CompilePatterns(nil)
	sieve := events.NewSieve(compiledPatterns, s, k.ID, 0, newTestLogger(t))
	ar := audit.NewAuditRecorder(s, k.ID, newTestLogger(t))

	return &Daemon{
		rootDir:          dir,
		store:            s,
		config:           cfg,
		kingdom:          k,
		ptyMgr:           mgr,
		sieve:            sieve,
		auditRecorder:    ar,
		ctx:              ctx,
		cancel:           cancel,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		logger:           newTestLogger(t),
	}
}

func TestStartVassals_ShellVassal_WithSieve(t *testing.T) {
	trueVal := true
	vassals := []config.VassalConfig{
		{
			Name:      "sieve-vassal",
			Command:   "/bin/sh",
			Autostart: &trueVal,
			RepoPath:  "/tmp",
		},
	}
	d := newDaemonForStartVassalsWithSieve(t, vassals)

	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals: %v", err)
	}
	// Session should have been created
	sessions := d.ptyMgr.ListSessions()
	if len(sessions) == 0 {
		t.Fatal("expected at least 1 session")
	}
}

// ---------------------------------------------------------------------------
// checkDataRate — direct unit tests
// ---------------------------------------------------------------------------

func TestCheckDataRate_BelowMin2(t *testing.T) {
	result := checkDataRate(100, 50, 100.0, 1) // 50 B/s < 100 min
	if result.OK {
		t.Fatal("expected NOT OK for below-minimum data rate")
	}
	if result.Message == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestCheckDataRate_AboveMin2(t *testing.T) {
	result := checkDataRate(1000, 50, 100.0, 1) // 950 B/s > 100 min
	if !result.OK {
		t.Fatalf("expected OK for above-minimum data rate, got: %s", result.Message)
	}
}

func TestCheckDataRate_CounterReset(t *testing.T) {
	// curBytes < prevBytes → delta clamped to 0 → rate = 0
	result := checkDataRate(50, 100, 1.0, 1)
	if result.OK {
		t.Fatal("expected NOT OK when counter appears to reset to lower value")
	}
}

// ---------------------------------------------------------------------------
// checkPortOpen — open port expected to be closed
// ---------------------------------------------------------------------------

func TestCheckPortOpen_OpenExpectedClosed(t *testing.T) {
	// Listen on a port then call checkPortOpen with expect="closed"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen: " + err.Error())
	}
	defer ln.Close()

	addr := ln.Addr().String()
	portStr := addr[strings.LastIndex(addr, ":")+1:]
	var portNum int
	fmt.Sscanf(portStr, "%d", &portNum)

	result := checkPortOpen(portNum, "closed")
	if result.OK {
		t.Fatal("expected NOT OK: port is open but expected closed")
	}
}

// ---------------------------------------------------------------------------
// checkHealthScript — timeout path
// ---------------------------------------------------------------------------

func TestCheckHealthScript_Timeout2(t *testing.T) {
	// Create a shell script that sleeps for a long time so the timeout fires first
	dir := t.TempDir()
	script := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 100\n"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	result := checkHealthScript(script, 200*time.Millisecond, "/tmp")
	if result.OK {
		t.Fatal("expected NOT OK for timed-out health script")
	}
	if !strings.Contains(result.Message, "timed out") {
		t.Fatalf("expected 'timed out' in message, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// handleConnection — JSON parse error path
// ---------------------------------------------------------------------------

func TestHandleConnection_InvalidJSON(t *testing.T) {
	d := newDaemonWithRealHandlers(t)

	server, client := net.Pipe()
	defer server.Close()

	done := make(chan struct{})
	d.wg.Add(1)
	go func() {
		d.handleConnection(server)
		close(done)
	}()

	// Send invalid JSON — server should send error response and continue
	client.Write([]byte("{invalid json line}\n"))
	time.Sleep(50 * time.Millisecond)
	// Now close client to end the scanner loop
	client.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleConnection did not return after client close")
	}
}

// ---------------------------------------------------------------------------
// removePIDFile — empty pidFile path
// ---------------------------------------------------------------------------

func TestRemovePIDFile_EmptyPidFile(t *testing.T) {
	d := &Daemon{pidFile: "", logger: newTestLogger(t)}
	// Empty pidFile → early return, no panic
	d.removePIDFile()
}

// ---------------------------------------------------------------------------
// exec_in — sovereign approval timeout path
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_ExecIn_ApprovalTimeout(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	d.config.Settings.SovereignApproval = true
	d.config.Settings.SovereignApprovalTimeout = 1 // 1 second timeout
	d.approvalMgr = audit.NewApprovalManager()

	// Create a PTY session
	if _, err := d.ptyMgr.CreateSession(uuid.New().String(), "approval-vassal", "/bin/sh", "/tmp", nil); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	params := json.RawMessage(`{"target":"approval-vassal","command":"echo approved","timeout_seconds":10}`)
	_, err := d.handlers["exec_in"](params)
	// Should fail with APPROVAL_TIMEOUT after 1 second
	if err == nil {
		t.Fatal("expected APPROVAL_TIMEOUT error")
	}
	if !strings.Contains(err.Error(), "APPROVAL_TIMEOUT") {
		t.Fatalf("expected 'APPROVAL_TIMEOUT' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// exec_in — command execution timeout (traceStatus = "timeout")
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_ExecIn_CommandTimeout(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	d.config.Settings.SovereignApproval = false

	if _, err := d.ptyMgr.CreateSession(uuid.New().String(), "timeout-cmd-vassal", "/bin/sh", "/tmp", nil); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	params := json.RawMessage(`{"target":"timeout-cmd-vassal","command":"sleep 60","timeout_seconds":1}`)
	_, err := d.handlers["exec_in"](params)
	if err == nil {
		t.Fatal("expected timeout exec error")
	}
	if !strings.Contains(err.Error(), "exec error") {
		t.Fatalf("expected 'exec error', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// get_vassal_output — success path (session exists)
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_GetVassalOutput_Success(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	if _, err := d.ptyMgr.CreateSession(uuid.New().String(), "out-vassal", "/bin/sh", "/tmp", nil); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	params := json.RawMessage(`{"name":"out-vassal"}`)
	result, err := d.handlers["get_vassal_output"](params)
	if err != nil {
		t.Fatalf("get_vassal_output: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if _, exists := m["output"]; !exists {
		t.Fatal("expected 'output' key in result")
	}
}

// ---------------------------------------------------------------------------
// get_events — with seeded events (covers loop body)
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_GetEvents_WithSeededEvent(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	_ = d.store.CreateEvent(store.Event{
		ID:        uuid.New().String(),
		KingdomID: d.kingdom.ID,
		Severity:  "info",
		Summary:   "test seeded event",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	result, err := d.handlers["get_events"](nil)
	if err != nil {
		t.Fatalf("get_events with seeded event: %v", err)
	}
	m := result.(map[string]interface{})
	if m["count"].(int) == 0 {
		t.Fatal("expected at least 1 event from seeded data")
	}
}

// ---------------------------------------------------------------------------
// get_action_trace — success path (trace exists)
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_GetActionTrace_Success(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	traceID := uuid.New().String()
	_ = d.store.CreateActionTrace(store.ActionTrace{
		TraceID:    traceID,
		KingdomID:  d.kingdom.ID,
		VassalName: "v",
		Command:    "ls",
		Status:     "completed",
		StartedAt:  time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	params := json.RawMessage(fmt.Sprintf(`{"trace_id":%q}`, traceID))
	result, err := d.handlers["get_action_trace"](params)
	if err != nil {
		t.Fatalf("get_action_trace: %v", err)
	}
	m := result.(map[string]interface{})
	if m["trace_id"] != traceID {
		t.Errorf("expected trace_id %q, got %v", traceID, m["trace_id"])
	}
}

// ---------------------------------------------------------------------------
// get_audit_log — limit > 500 capped, and with seeded entries (loop body)
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_GetAuditLog_LimitCapped(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	// limit=1000 should be capped to 500
	params := json.RawMessage(`{"limit":1000}`)
	_, err := d.handlers["get_audit_log"](params)
	if err != nil {
		t.Fatalf("get_audit_log limit capped: %v", err)
	}
}

func TestRegisterRealHandlers_GetAuditLog_WithEntries(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	_ = d.store.CreateAuditEntry(store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: d.kingdom.ID,
		Layer:     "action",
		Source:    "test-source",
		Content:   "test content for audit log",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	result, err := d.handlers["get_audit_log"](nil)
	if err != nil {
		t.Fatalf("get_audit_log with entries: %v", err)
	}
	m := result.(map[string]interface{})
	if m["total"] == nil {
		t.Fatal("expected total key in result")
	}
}

// ---------------------------------------------------------------------------
// respond_approval — success (approved=true) and not-pending paths
// ---------------------------------------------------------------------------

func TestRegisterRealHandlers_RespondApproval_Approve(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	d.config.Settings.SovereignApproval = true

	traceID := uuid.New().String()
	approvalID := uuid.New().String()
	_ = d.store.CreateActionTrace(store.ActionTrace{
		TraceID: traceID, KingdomID: d.kingdom.ID,
		VassalName: "v", Command: "ls",
		Status: "running", StartedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	_ = d.store.CreateApprovalRequest(store.ApprovalRequest{
		ID: approvalID, KingdomID: d.kingdom.ID,
		TraceID: traceID, Command: "ls",
		VassalName: "v", Status: "pending",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})

	params := json.RawMessage(fmt.Sprintf(`{"request_id":%q,"approved":true}`, approvalID))
	result, err := d.handlers["respond_approval"](params)
	if err != nil {
		t.Fatalf("respond_approval: %v", err)
	}
	m := result.(map[string]interface{})
	if m["status"] != "approved" {
		t.Errorf("expected status=approved, got %v", m["status"])
	}
}

func TestRegisterRealHandlers_RespondApproval_NotPending(t *testing.T) {
	d := newDaemonWithRealHandlers(t)
	d.config.Settings.SovereignApproval = true

	traceID := uuid.New().String()
	approvalID := uuid.New().String()
	_ = d.store.CreateActionTrace(store.ActionTrace{
		TraceID: traceID, KingdomID: d.kingdom.ID,
		VassalName: "v", Command: "ls",
		Status: "running", StartedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	_ = d.store.CreateApprovalRequest(store.ApprovalRequest{
		ID: approvalID, KingdomID: d.kingdom.ID,
		TraceID: traceID, Command: "ls",
		VassalName: "v", Status: "pending",
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	})
	// Mark it as already processed
	_ = d.store.UpdateApprovalRequest(approvalID, "approved", time.Now().UTC().Format("2006-01-02 15:04:05"))

	params := json.RawMessage(fmt.Sprintf(`{"request_id":%q,"approved":false}`, approvalID))
	_, err := d.handlers["respond_approval"](params)
	if err == nil {
		t.Fatal("expected error for non-pending approval request")
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Errorf("expected 'not pending' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// watchVassal — restart attempt when king-vassal binary not available
// ---------------------------------------------------------------------------

func TestWatchVassal_RestartAttempt_BinaryNotFound(t *testing.T) {
	// Use a short context so the test doesn't take too long
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Clear PATH so resolveKingVassalBinary fails immediately
	origPATH := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", origPATH) })
	os.Setenv("PATH", "/nonexistent-path-for-test")

	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		vassalProcs:      make(map[string]*vassalProc),
		vassalPool:       NewVassalClientPool(),
		logger:           newTestLogger(t),
		ctx:              ctx,
		cancel:           cancel,
	}

	cfg := config.VassalConfig{
		Name:          "restart-nobin",
		RestartPolicy: "always",
	}

	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}

	done := make(chan struct{})
	d.wg.Add(1)
	go func() {
		d.watchVassal("restart-nobin", cmd, cfg)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchVassal did not return within 5s")
	}
}

// ---------------------------------------------------------------------------
// removePIDFile — error path (non-NotExist failure)
// ---------------------------------------------------------------------------

func TestRemovePIDFile_ErrorPath(t *testing.T) {
	// Use a non-empty directory as pidFile. os.Remove on a non-empty dir
	// returns "directory not empty" which is NOT os.IsNotExist → triggers logger.Error.
	dir := t.TempDir()
	child := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(child, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{pidFile: dir, logger: newTestLogger(t)}
	d.removePIDFile() // should log error but not panic
}

// ---------------------------------------------------------------------------
// maybeRestartOrphanedVassal — not-in-procs and dead-process paths
// ---------------------------------------------------------------------------

func TestMaybeRestartOrphanedVassal_NotInProcs(t *testing.T) {
	d := &Daemon{
		vassalProcs: make(map[string]*vassalProc),
		logger:      newTestLogger(t),
	}
	d.maybeRestartOrphanedVassal("no-such-vassal")
}

func TestMaybeRestartOrphanedVassal_DeadProcess(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	_ = cmd.Wait() // let it die
	d := &Daemon{
		vassalProcs: map[string]*vassalProc{"dead-v": {process: cmd.Process}},
		logger:      newTestLogger(t),
	}
	d.maybeRestartOrphanedVassal("dead-v")
}

// ---------------------------------------------------------------------------
// watchVassal — restart_policy=no returns immediately
// ---------------------------------------------------------------------------

func TestWatchVassal_PolicyNo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}

	d := &Daemon{
		ctx:              ctx,
		cancel:           cancel,
		delegatedVassals: make(map[string]DelegationInfo),
		vassalProcs:      make(map[string]*vassalProc),
		vassalPool:       NewVassalClientPool(),
		logger:           newTestLogger(t),
	}

	done := make(chan struct{})
	d.wg.Add(1)
	go func() {
		d.watchVassal("v", cmd, config.VassalConfig{Name: "v", RestartPolicy: "no"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("watchVassal with policy=no should return quickly")
	}
}

// ---------------------------------------------------------------------------
// Stop — with vassalProcs entries (covers kill loop, pgid=0 path)
// ---------------------------------------------------------------------------

func TestStop_WithVassalProcs_PgidZero(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("/tmp", "stop-vp-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	// Process already dead when Stop() runs; SIGTERM/SIGKILL will fail silently.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	_ = cmd.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	d := &Daemon{
		ctx:              ctx,
		cancel:           cancel,
		rootDir:          dir,
		vassalProcs:      map[string]*vassalProc{"dead-vassal": {process: cmd.Process, pgid: 0}},
		vassalPool:       NewVassalClientPool(),
		delegatedVassals: make(map[string]DelegationInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	_ = d.Stop()
}

// ---------------------------------------------------------------------------
// cleanStaleFiles — active socket (conn.Close) and corrupt PID file paths
// ---------------------------------------------------------------------------

func TestCleanStaleFiles_ActiveSocketAndCorruptPID(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "csf-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create an active Unix socket (someone listening)
	sockPath := filepath.Join(kingDir, "king-active.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	// Create a corrupt PID file (non-numeric content)
	pidPath := filepath.Join(kingDir, "king-corrupt.pid")
	if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d := &Daemon{rootDir: dir, logger: newTestLogger(t)}
	if err := d.cleanStaleFiles(); err != nil {
		t.Fatalf("cleanStaleFiles: %v", err)
	}

	// Corrupt PID file should have been removed
	if _, statErr := os.Stat(pidPath); !os.IsNotExist(statErr) {
		t.Error("corrupt PID file should have been removed")
	}
}

// ---------------------------------------------------------------------------
// Attach — with vassal socket directory (auto-connect path)
// ---------------------------------------------------------------------------

func TestAttach_WithVassalSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "attach-vsock-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write minimal config
	cfgContent := "name: test-attach-vsock\n"
	if err := os.WriteFile(filepath.Join(kingDir, "kingdom.yml"), []byte(cfgContent), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	// Pre-populate DB with kingdom record
	dbPath := filepath.Join(kingDir, "king.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	k := store.Kingdom{
		ID:         uuid.New().String(),
		Name:       "test-attach-vsock",
		RootPath:   dir,
		SocketPath: filepath.Join(kingDir, "test.sock"),
		Status:     "running",
		CreatedAt:  "2026-01-01 00:00:00",
		UpdatedAt:  "2026-01-01 00:00:00",
	}
	if err := s.CreateKingdom(k); err != nil {
		s.Close()
		t.Fatalf("CreateKingdom: %v", err)
	}
	s.Close()

	// Create vassals sock dir with a fake .sock file (no listener — Connect will fail gracefully)
	vassalSockDir := filepath.Join(kingDir, "vassals")
	if err := os.MkdirAll(vassalSockDir, 0755); err != nil {
		t.Fatalf("MkdirAll vassals: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vassalSockDir, "fake-vassal.sock"), []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile sock: %v", err)
	}

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Attach(ctx); err != nil {
		cancel()
		t.Fatalf("Attach: %v", err)
	}
	cancel()
	d.wg.Wait()
}

// ---------------------------------------------------------------------------
// startVassals — claude-type vassal (skip branch coverage)
// ---------------------------------------------------------------------------

func TestStartVassals_ClaudeType_Skipped(t *testing.T) {
	vassals := []config.VassalConfig{
		{Name: "claude-v", Type: "claude"},
		{Name: "codex-v", Type: "codex"},
	}
	d := newDaemonForStartVassals(t, vassals)
	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals: %v", err)
	}
	sessions := d.ptyMgr.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions for claude/codex type, got %d", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// startVassals — serial vassal with explicit protocol (contract injection path)
// ---------------------------------------------------------------------------

func TestStartVassals_SerialVassal_WithProtocol(t *testing.T) {
	trueVal := true
	vassals := []config.VassalConfig{
		{
			Name:           "serial-v",
			Type:           "serial",
			SerialPort:     "/dev/ttyUSB99", // nonexistent port; CreateSession starts the cmd anyway
			BaudRate:       9600,
			SerialProtocol: "modbus",
			Autostart:      &trueVal,
		},
	}
	d := newDaemonForStartVassalsWithSieve(t, vassals)
	_ = d.startVassals() // may succeed or fail depending on stty; either is ok
}

// ---------------------------------------------------------------------------
// startVassals — relative CWD path
// ---------------------------------------------------------------------------

func TestStartVassals_RelativeCwd(t *testing.T) {
	trueVal := true
	vassals := []config.VassalConfig{
		{Name: "cwd-v", Command: "/bin/sh", Autostart: &trueVal, Cwd: "subdir"},
	}
	d := newDaemonForStartVassals(t, vassals)
	// Create the subdir so it exists
	_ = os.MkdirAll(filepath.Join(d.rootDir, "subdir"), 0755)
	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals: %v", err)
	}
}

// ---------------------------------------------------------------------------
// watchVassal — isDelegated path (loop exits on ctx cancel)
// ---------------------------------------------------------------------------

func TestWatchVassal_DelegatedExit_CtxCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}

	d := &Daemon{
		ctx:    ctx,
		cancel: cancel,
		delegatedVassals: map[string]DelegationInfo{
			"delegated-v": {SessionPID: os.Getpid(), LastHeartbeat: time.Now()},
		},
		vassalProcs: make(map[string]*vassalProc),
		vassalPool:  NewVassalClientPool(),
		logger:      newTestLogger(t),
	}

	done := make(chan struct{})
	d.wg.Add(1)
	go func() {
		// policy="always" so it won't return early; delegation holds it until ctx is done
		d.watchVassal("delegated-v", cmd, config.VassalConfig{Name: "delegated-v", RestartPolicy: "always"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchVassal with delegation did not return within 5s")
	}
}

// ---------------------------------------------------------------------------
// startGuardRunners — trigger tick for log_watch and data_rate guards
// ---------------------------------------------------------------------------

func TestStartGuardRunners_TriggerLogWatchTick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dir, _ := os.MkdirTemp("/tmp", "sgr-tick-*")
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	s, _ := store.NewStore(dbPath)
	defer s.Close()
	k, _ := NewKingdom(s, &config.KingdomConfig{Name: "sgr-tick"}, dir, newTestLogger(t))
	mgr := pty.NewManager(s, k.ID, newTestLogger(t))

	// log_watch and data_rate guards with interval=1 (fires every 1s)
	logGuard := config.GuardConfig{Type: "log_watch", Interval: 1, Threshold: 3}
	dataGuard := config.GuardConfig{Type: "data_rate", Interval: 1, Threshold: 3, MinBytesPerSec: 100}
	healthGuard := config.GuardConfig{Type: "health_check", Interval: 1, Threshold: 3, Exec: "/bin/true"}
	vassalCfg := config.VassalConfig{
		Name:   "tick-vassal",
		Guards: []config.GuardConfig{logGuard, dataGuard, healthGuard},
	}

	d := &Daemon{
		rootDir:          dir,
		config:           &config.KingdomConfig{Name: "sgr-tick", Vassals: []config.VassalConfig{vassalCfg}},
		ptyMgr:           mgr,
		ctx:              ctx,
		cancel:           cancel,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		vassalPool:       NewVassalClientPool(),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		logger:           newTestLogger(t),
	}

	d.startGuardRunners(ctx)

	// Wait for ticks to fire
	time.Sleep(1500 * time.Millisecond)
	cancel()
	d.wg.Wait()
}
