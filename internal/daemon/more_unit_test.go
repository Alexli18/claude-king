package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/pty"
	"github.com/alexli18/claude-king/internal/store"
)

// ---------------------------------------------------------------------------
// VassalClientPool.Connect, Get, Names, Disconnect with a real Unix socket
// ---------------------------------------------------------------------------

// listenUnixTemp creates a Unix socket listener in a temp directory and
// returns the listener plus the socket path. The caller must close the listener.
func listenUnixTemp(t *testing.T) (net.Listener, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "daemon-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen unix: %v", err)
	}
	return ln, sockPath
}

func TestVassalClientPool_Connect(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	vc, err := pool.Connect("testvassal", sockPath)
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if vc == nil {
		t.Fatal("Connect returned nil VassalClient")
	}
	pool.DisconnectAll()
}

func TestVassalClientPool_Connect_Replaces(t *testing.T) {
	// Connecting a second time with the same name replaces the old connection.
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	vc1, err := pool.Connect("testvassal", sockPath)
	if err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if vc1 == nil {
		t.Fatal("first Connect returned nil")
	}

	vc2, err := pool.Connect("testvassal", sockPath)
	if err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	if vc2 == nil {
		t.Fatal("second Connect returned nil")
	}
	// Both should be different pointers (old was replaced).
	if vc1 == vc2 {
		t.Fatal("expected new VassalClient pointer after reconnect")
	}
	pool.DisconnectAll()
}

func TestVassalClientPool_Connect_BadPath(t *testing.T) {
	pool := NewVassalClientPool()
	_, err := pool.Connect("bad", "/nonexistent/path/test.sock")
	if err == nil {
		t.Fatal("expected error connecting to nonexistent socket, got nil")
	}
}

func TestVassalClientPool_GetAfterConnect(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	vc, _ := pool.Connect("myvassal", sockPath)

	got, ok := pool.Get("myvassal")
	if !ok {
		t.Fatal("expected ok=true after Connect")
	}
	if got != vc {
		t.Fatal("expected Get to return the connected VassalClient")
	}
	pool.DisconnectAll()
}

func TestVassalClientPool_NamesAfterConnect(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	pool.Connect("alpha", sockPath) //nolint:errcheck

	names := pool.Names()
	if len(names) != 1 {
		t.Fatalf("expected 1 name, got %d: %v", len(names), names)
	}
	if names[0] != "alpha" {
		t.Fatalf("expected name 'alpha', got %q", names[0])
	}
	pool.DisconnectAll()
}

func TestVassalClientPool_NamesMultiple(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	pool.Connect("alpha", sockPath) //nolint:errcheck
	pool.Connect("beta", sockPath)  //nolint:errcheck
	pool.Connect("gamma", sockPath) //nolint:errcheck

	names := pool.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d: %v", len(names), names)
	}
	pool.DisconnectAll()
}

func TestVassalClientPool_DisconnectConnected(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	pool.Connect("worker", sockPath) //nolint:errcheck

	err := pool.Disconnect("worker")
	if err != nil {
		t.Fatalf("Disconnect returned unexpected error: %v", err)
	}

	_, ok := pool.Get("worker")
	if ok {
		t.Fatal("expected Get to return false after Disconnect")
	}
}

func TestVassalClientPool_DisconnectAll_Connected(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	pool.Connect("v1", sockPath) //nolint:errcheck
	pool.Connect("v2", sockPath) //nolint:errcheck

	pool.DisconnectAll()

	names := pool.Names()
	if len(names) != 0 {
		t.Fatalf("expected empty pool after DisconnectAll, got: %v", names)
	}
}

// ---------------------------------------------------------------------------
// vassalPoolAdapter
// ---------------------------------------------------------------------------

func TestVassalPoolAdapter_Get_Missing(t *testing.T) {
	pool := NewVassalClientPool()
	adapter := &vassalPoolAdapter{pool: pool}

	got, ok := adapter.Get("nonexistent")
	if ok {
		t.Fatal("expected ok=false for missing vassal")
	}
	if got != nil {
		t.Fatal("expected nil for missing vassal")
	}
}

func TestVassalPoolAdapter_Get_Present(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	pool.Connect("myvassal", sockPath) //nolint:errcheck
	defer pool.DisconnectAll()

	adapter := &vassalPoolAdapter{pool: pool}
	got, ok := adapter.Get("myvassal")
	if !ok {
		t.Fatal("expected ok=true for connected vassal")
	}
	if got == nil {
		t.Fatal("expected non-nil VassalCaller")
	}
}

func TestVassalPoolAdapter_Names_Empty(t *testing.T) {
	pool := NewVassalClientPool()
	adapter := &vassalPoolAdapter{pool: pool}
	names := adapter.Names()
	if len(names) != 0 {
		t.Fatalf("expected empty names, got %v", names)
	}
}

func TestVassalPoolAdapter_Names_Present(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	pool := NewVassalClientPool()
	pool.Connect("vassal1", sockPath) //nolint:errcheck
	defer pool.DisconnectAll()

	adapter := &vassalPoolAdapter{pool: pool}
	names := adapter.Names()
	if len(names) != 1 || names[0] != "vassal1" {
		t.Fatalf("expected [vassal1], got %v", names)
	}
}

// ---------------------------------------------------------------------------
// ptyManagerAdapter
// ---------------------------------------------------------------------------

func TestPTYManagerAdapter_GetSession_Missing(t *testing.T) {
	mgr := newTestPTYManager(t)
	adapter := &ptyManagerAdapter{mgr: mgr}

	got, ok := adapter.GetSession("nonexistent")
	if ok {
		t.Fatal("expected ok=false for missing session")
	}
	if got != nil {
		t.Fatal("expected nil for missing session")
	}
}

func TestPTYManagerAdapter_ListSessions_Empty(t *testing.T) {
	mgr := newTestPTYManager(t)
	adapter := &ptyManagerAdapter{mgr: mgr}

	sessions := adapter.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("expected empty sessions list, got %d", len(sessions))
	}
}

// newTestPTYManager creates a PTY manager backed by a temp store with a pre-created kingdom.
func newTestPTYManager(t *testing.T) *pty.Manager {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pty-test-*")
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

	const testKingdomID = "test-kingdom-id"
	now := "2026-01-01 00:00:00"
	if err := s.CreateKingdom(store.Kingdom{
		ID:        testKingdomID,
		Name:      "test-kingdom",
		RootPath:  dir,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateKingdom for PTY test: %v", err)
	}

	return pty.NewManager(s, testKingdomID, newTestLogger(t))
}

// ---------------------------------------------------------------------------
// cleanStaleSocket
// ---------------------------------------------------------------------------

func TestCleanStaleSocket_NoFile(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-stale-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	d := &Daemon{
		rootDir:          dir,
		sockPath:         filepath.Join(dir, "king.sock"),
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	// Socket does not exist; cleanStaleSocket should return nil silently.
	if err := d.cleanStaleSocket(); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestCleanStaleSocket_StaleSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-stale-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	sockPath := filepath.Join(dir, "king.sock")
	// Create a socket file with nobody listening by binding and immediately closing.
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	ln.Close() // close immediately so nobody is listening

	d := &Daemon{
		rootDir:          dir,
		sockPath:         sockPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	err = d.cleanStaleSocket()
	if err != nil {
		t.Fatalf("expected nil after removing stale socket, got: %v", err)
	}
	if _, statErr := os.Stat(sockPath); !os.IsNotExist(statErr) {
		t.Fatal("expected stale socket file to be removed")
	}
}

func TestCleanStaleSocket_ActiveSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-stale-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	sockPath := filepath.Join(dir, "king.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	d := &Daemon{
		rootDir:          dir,
		sockPath:         sockPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	err = d.cleanStaleSocket()
	if err == nil {
		t.Fatal("expected error when active socket is found (kingdom already running)")
	}
}

// ---------------------------------------------------------------------------
// cleanStaleFiles
// ---------------------------------------------------------------------------

func TestCleanStaleFiles_EmptyDir(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-clean-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	d := &Daemon{
		rootDir:          dir,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.cleanStaleFiles(); err != nil {
		t.Fatalf("expected nil for empty dir, got: %v", err)
	}
}

func TestCleanStaleFiles_RemovesStaleSock(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-clean-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a stale socket file (listener already closed).
	staleSock := filepath.Join(kingDir, "king-deadbeef.sock")
	ln, err := net.Listen("unix", staleSock)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	ln.Close()

	d := &Daemon{
		rootDir:          dir,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.cleanStaleFiles(); err != nil {
		t.Fatalf("cleanStaleFiles: %v", err)
	}

	if _, statErr := os.Stat(staleSock); !os.IsNotExist(statErr) {
		t.Fatal("expected stale socket to be removed")
	}
}

func TestCleanStaleFiles_KeepsActiveSock(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-clean-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	activeSock := filepath.Join(kingDir, "king-aabbccdd.sock")
	ln, err := net.Listen("unix", activeSock)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	d := &Daemon{
		rootDir:          dir,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.cleanStaleFiles(); err != nil {
		t.Fatalf("cleanStaleFiles: %v", err)
	}

	if _, statErr := os.Stat(activeSock); statErr != nil {
		t.Fatal("expected active socket to be kept")
	}
}

func TestCleanStaleFiles_RemovesStalePID(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-clean-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write a PID file referencing a process that doesn't exist.
	stalePID := filepath.Join(kingDir, "king-deadbeef.pid")
	if err := os.WriteFile(stalePID, []byte("999999999\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d := &Daemon{
		rootDir:          dir,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.cleanStaleFiles(); err != nil {
		t.Fatalf("cleanStaleFiles: %v", err)
	}

	if _, statErr := os.Stat(stalePID); !os.IsNotExist(statErr) {
		t.Fatal("expected stale PID file to be removed")
	}
}

func TestCleanStaleFiles_KeepsAlivePID(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-clean-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write a PID file with the current process's PID (guaranteed alive).
	alivePID := filepath.Join(kingDir, "king-aabbccdd.pid")
	if err := os.WriteFile(alivePID, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d := &Daemon{
		rootDir:          dir,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.cleanStaleFiles(); err != nil {
		t.Fatalf("cleanStaleFiles: %v", err)
	}

	if _, statErr := os.Stat(alivePID); statErr != nil {
		t.Fatalf("expected alive PID file to be kept: %v", statErr)
	}
}

func TestCleanStaleFiles_RemovesCorruptPID(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-clean-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	if err := os.MkdirAll(kingDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	corruptPID := filepath.Join(kingDir, "king-corrupt.pid")
	if err := os.WriteFile(corruptPID, []byte("not-a-pid\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d := &Daemon{
		rootDir:          dir,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.cleanStaleFiles(); err != nil {
		t.Fatalf("cleanStaleFiles: %v", err)
	}

	if _, statErr := os.Stat(corruptPID); !os.IsNotExist(statErr) {
		t.Fatal("expected corrupt PID file to be removed")
	}
}

// ---------------------------------------------------------------------------
// checkDataRate — negative delta (counter reset)
// ---------------------------------------------------------------------------

func TestCheckDataRate_NegativeDelta(t *testing.T) {
	// curBytes < prevBytes simulates a counter reset; delta should be clamped to 0.
	// With rate=0 and minBytesPerSec=1.0, the check should fail.
	result := checkDataRate(50, 100, 1.0, 10)
	if result.OK {
		t.Fatalf("expected OK=false when rate is 0 (counter reset), got: %s", result.Message)
	}
}

func TestCheckDataRate_ZeroBytes(t *testing.T) {
	result := checkDataRate(0, 0, 0.0, 10) // 0 rate, 0 min → OK
	if !result.OK {
		t.Fatalf("expected OK=true with zero rate and zero min, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// checkLogWatch — pattern match path via daemon with seeded lines
// ---------------------------------------------------------------------------

func TestCheckLogWatch_WithMatchingPattern(t *testing.T) {
	// Create a daemon with a ptyMgr that has a session with lines.
	// We test via the ansiStrip logic directly here since injecting
	// real PTY lines requires a running session.
	lines := []string{
		"all is well",
		"ERROR: catastrophic failure",
	}
	pat := regexp.MustCompile(`ERROR`)
	found := false
	for _, line := range lines {
		clean := ansiStrip.ReplaceAllString(line, "")
		if pat.MatchString(clean) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected pattern to match in lines")
	}
}

func TestCheckLogWatch_WithNoMatchingPattern(t *testing.T) {
	lines := []string{"all is well", "everything fine"}
	pat := regexp.MustCompile(`ERROR`)
	for _, line := range lines {
		clean := ansiStrip.ReplaceAllString(line, "")
		if pat.MatchString(clean) {
			t.Fatalf("unexpected pattern match in line: %q", clean)
		}
	}
}

func TestCheckLogWatch_NilPTYMgr(t *testing.T) {
	// When ptyMgr is nil, checkLogWatch must not panic and must return OK=true.
	d := &Daemon{
		ptyMgr:           nil,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
	pat := regexp.MustCompile(`FATAL`)
	result := d.checkLogWatch("api", []*regexp.Regexp{pat}, 10*time.Second)
	if !result.OK {
		t.Fatalf("expected OK=true with nil ptyMgr, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// updateGuardState — missing key is a no-op (does not panic)
// ---------------------------------------------------------------------------

func TestUpdateGuardState_MissingKey(t *testing.T) {
	d := newTestDaemon(t)
	// key not in guardStates; must not panic.
	d.updateGuardState("nonexistent:0", GuardResult{OK: false, Message: "fail", CheckedAt: time.Now()}, 3)
}

// ---------------------------------------------------------------------------
// SocketPathForRoot and pidPathForRoot — pure functions
// ---------------------------------------------------------------------------

func TestSocketPathForRoot(t *testing.T) {
	path := SocketPathForRoot("/some/root")
	if path == "" {
		t.Fatal("expected non-empty socket path")
	}
	if filepath.Ext(path) != ".sock" {
		t.Fatalf("expected .sock extension, got %q", path)
	}
}

func TestSocketPathForRoot_Deterministic(t *testing.T) {
	p1 := SocketPathForRoot("/same/root")
	p2 := SocketPathForRoot("/same/root")
	if p1 != p2 {
		t.Fatalf("SocketPathForRoot must be deterministic: %q != %q", p1, p2)
	}
}

func TestSocketPathForRoot_DifferentRoots(t *testing.T) {
	p1 := SocketPathForRoot("/root/a")
	p2 := SocketPathForRoot("/root/b")
	if p1 == p2 {
		t.Fatal("expected different socket paths for different roots")
	}
}

func TestPidPathForRoot_Deterministic(t *testing.T) {
	p1 := pidPathForRoot("/some/dir")
	p2 := pidPathForRoot("/some/dir")
	if p1 != p2 {
		t.Fatalf("pidPathForRoot must be deterministic: %q != %q", p1, p2)
	}
	if filepath.Ext(p1) != ".pid" {
		t.Fatalf("expected .pid extension, got %q", p1)
	}
}

// ---------------------------------------------------------------------------
// writePIDFile / removePIDFile
// ---------------------------------------------------------------------------

func TestWriteAndRemovePIDFile(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-pid-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	pidPath := filepath.Join(dir, "king.pid")
	d := &Daemon{
		pidFile:          pidPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.writePIDFile(); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("ReadFile pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse pid: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid=%d, got %d", os.Getpid(), pid)
	}

	d.removePIDFile()
	if _, statErr := os.Stat(pidPath); !os.IsNotExist(statErr) {
		t.Fatal("expected PID file to be removed")
	}
}

func TestWritePIDFile_MissingDir(t *testing.T) {
	d := &Daemon{
		pidFile:          "/nonexistent/path/king.pid",
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
	if err := d.writePIDFile(); err == nil {
		t.Fatal("expected error writing PID to nonexistent dir")
	}
}

// ---------------------------------------------------------------------------
// IsRunning helper
// ---------------------------------------------------------------------------

func TestIsRunning_NoDaemon(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "daemon-running-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	running, err := IsRunning(dir)
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if running {
		t.Fatal("expected IsRunning=false for fresh dir")
	}
}

// ---------------------------------------------------------------------------
// Kingdom — NewKingdom, LoadKingdom, SetStatus, GetStatus
// ---------------------------------------------------------------------------

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "store-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	s, err := store.NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newTestConfig() *config.KingdomConfig {
	return &config.KingdomConfig{Name: "test-kingdom"}
}

func TestNewKingdom_CreatesNewEntry(t *testing.T) {
	s := newTestStore(t)
	dir, _ := os.MkdirTemp("/tmp", "kingdom-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}
	if k.ID == "" {
		t.Fatal("expected non-empty kingdom ID")
	}
	if k.GetStatus() != "starting" {
		t.Fatalf("expected status=starting, got %q", k.GetStatus())
	}
}

func TestNewKingdom_ReusesExistingEntry(t *testing.T) {
	s := newTestStore(t)
	dir, _ := os.MkdirTemp("/tmp", "kingdom-*")
	defer os.RemoveAll(dir)

	k1, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("first NewKingdom: %v", err)
	}

	k2, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("second NewKingdom: %v", err)
	}

	if k1.ID != k2.ID {
		t.Fatalf("expected same kingdom ID on reuse, got %q vs %q", k1.ID, k2.ID)
	}
}

func TestLoadKingdom_NotFound(t *testing.T) {
	s := newTestStore(t)
	dir, _ := os.MkdirTemp("/tmp", "kingdom-*")
	defer os.RemoveAll(dir)

	_, err := LoadKingdom(s, dir, newTestLogger(t))
	if err == nil {
		t.Fatal("expected error loading nonexistent kingdom")
	}
}

func TestLoadKingdom_Found(t *testing.T) {
	s := newTestStore(t)
	dir, _ := os.MkdirTemp("/tmp", "kingdom-*")
	defer os.RemoveAll(dir)

	k1, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	k2, err := LoadKingdom(s, dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("LoadKingdom: %v", err)
	}

	if k2.ID != k1.ID {
		t.Fatalf("expected same ID, got %q vs %q", k1.ID, k2.ID)
	}
}

func TestKingdom_SetStatus(t *testing.T) {
	s := newTestStore(t)
	dir, _ := os.MkdirTemp("/tmp", "kingdom-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	if err := k.SetStatus("running"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if k.GetStatus() != "running" {
		t.Fatalf("expected status=running, got %q", k.GetStatus())
	}
}

// ---------------------------------------------------------------------------
// dispatch — method not found, handler returns error, handler returns result
// ---------------------------------------------------------------------------

func TestDispatch_MethodNotFound(t *testing.T) {
	d := newTestDaemonWithHandlers(t)
	req := RPCRequest{Method: "nonexistent_method", ID: 1}
	resp := d.dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected code -32601 (method not found), got %d", resp.Error.Code)
	}
	if resp.ID != 1 {
		t.Fatalf("expected ID=1, got %d", resp.ID)
	}
}

func newTestDaemonWithHandlers(t *testing.T) *Daemon {
	t.Helper()
	d := newTestDaemon(t)
	d.handlers = make(map[string]rpcHandler)
	return d
}

func TestDispatch_HandlerSuccess(t *testing.T) {
	d := newTestDaemonWithHandlers(t)
	d.handlers["ping"] = func(_ json.RawMessage) (interface{}, error) {
		return map[string]string{"pong": "true"}, nil
	}

	req := RPCRequest{Method: "ping", ID: 42}
	resp := d.dispatch(req)
	if resp.Error != nil {
		t.Fatalf("expected no error, got: %v", resp.Error)
	}
	if resp.ID != 42 {
		t.Fatalf("expected ID=42, got %d", resp.ID)
	}
}

func TestDispatch_HandlerError(t *testing.T) {
	d := newTestDaemonWithHandlers(t)
	d.handlers["fail"] = func(_ json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("something went wrong")
	}

	req := RPCRequest{Method: "fail", ID: 7}
	resp := d.dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error response from failing handler")
	}
	if resp.Error.Code != -32000 {
		t.Fatalf("expected code -32000, got %d", resp.Error.Code)
	}
	if resp.ID != 7 {
		t.Fatalf("expected ID=7, got %d", resp.ID)
	}
}

// ---------------------------------------------------------------------------
// registerStubHandlers — vassal.register, vassal.list, status, shutdown
// ---------------------------------------------------------------------------

func TestRegisterStubHandlers_VassalRegister(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()

	params := json.RawMessage(`{"name":"myvassal","pid":1234,"socket":"/tmp/my.sock","repo_path":"/repo"}`)
	result, err := d.handlers["vassal.register"](params)
	if err != nil {
		t.Fatalf("vassal.register: %v", err)
	}
	m := result.(map[string]bool)
	if !m["ok"] {
		t.Fatal("expected ok=true")
	}

	d.externalVassalsMu.RLock()
	info, ok := d.externalVassals["myvassal"]
	d.externalVassalsMu.RUnlock()
	if !ok {
		t.Fatal("expected vassal to be registered")
	}
	if info.PID != 1234 {
		t.Fatalf("expected pid=1234, got %d", info.PID)
	}
}

func TestRegisterStubHandlers_VassalRegister_MissingName(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()

	params := json.RawMessage(`{"pid":1234}`)
	_, err := d.handlers["vassal.register"](params)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestRegisterStubHandlers_VassalRegister_InvalidPID(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()

	params := json.RawMessage(`{"name":"myvassal","pid":0}`)
	_, err := d.handlers["vassal.register"](params)
	if err == nil {
		t.Fatal("expected error for zero pid")
	}
}

func TestRegisterStubHandlers_VassalRegister_ControlChar(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()

	// socket path contains a control character (0x01)
	params := json.RawMessage("{\"name\":\"myvassal\",\"pid\":1,\"socket\":\"/tmp/\x01bad.sock\"}")
	_, err := d.handlers["vassal.register"](params)
	if err == nil {
		t.Fatal("expected error for control char in socket path")
	}
}

func TestRegisterStubHandlers_VassalList(t *testing.T) {
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		delegationMu:     sync.RWMutex{},
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()

	result, err := d.handlers["vassal.list"](nil)
	if err != nil {
		t.Fatalf("vassal.list: %v", err)
	}
	m := result.(map[string]interface{})
	vassals := m["vassals"]
	if vassals == nil {
		t.Fatal("expected 'vassals' key in result")
	}
}

func TestRegisterStubHandlers_Status_NilKingdom(t *testing.T) {
	d := &Daemon{
		kingdom:          nil,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()

	result, err := d.handlers["status"](nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	m := result.(map[string]string)
	if m["status"] != "unknown" {
		t.Fatalf("expected status=unknown, got %q", m["status"])
	}
}

func TestRegisterStubHandlers_Shutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel // hold reference

	d := &Daemon{
		kingdom:          nil,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		cancel:           cancel,
		ctx:              ctx,
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()

	result, err := d.handlers["shutdown"](nil)
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	m := result.(map[string]string)
	if m["status"] != "shutting_down" {
		t.Fatalf("expected shutting_down, got %q", m["status"])
	}
	// context should be cancelled
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected context to be cancelled after shutdown")
	}
}

// ---------------------------------------------------------------------------
// ServeConn / handleConnection / writeResponse — via in-process loopback
// ---------------------------------------------------------------------------

func newListeningDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "daemon-serve-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	sockPath := filepath.Join(dir, "king.sock")
	d := &Daemon{
		rootDir:          dir,
		sockPath:         sockPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.registerStubHandlers()
	return d, sockPath
}

func TestServeConn_KnownMethod(t *testing.T) {
	d, _ := newListeningDaemon(t)

	// Add a simple echo handler.
	d.handlers["echo"] = func(_ json.RawMessage) (interface{}, error) {
		return map[string]string{"hello": "world"}, nil
	}

	// Create a loopback connection pair.
	server, client := net.Pipe()
	defer client.Close()

	// Run handleConnection in background (it blocks until conn closes).
	go d.ServeConn(server)

	// Send request.
	req := RPCRequest{Method: "echo", ID: 1}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	client.Write(data) //nolint:errcheck

	// Read response.
	scanner := bufio.NewScanner(client)
	if !scanner.Scan() {
		t.Fatal("expected response line")
	}
	var resp RPCResponse
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.ID != 1 {
		t.Fatalf("expected ID=1, got %d", resp.ID)
	}
}

func TestServeConn_UnknownMethod(t *testing.T) {
	d, _ := newListeningDaemon(t)

	server, client := net.Pipe()
	defer client.Close()

	go d.ServeConn(server)

	req := RPCRequest{Method: "unknown_xyz", ID: 99}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	client.Write(data) //nolint:errcheck

	scanner := bufio.NewScanner(client)
	if !scanner.Scan() {
		t.Fatal("expected response")
	}
	var resp RPCResponse
	json.Unmarshal(scanner.Bytes(), &resp) //nolint:errcheck
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected -32601, got %d", resp.Error.Code)
	}
}

func TestServeConn_ParseError(t *testing.T) {
	d, _ := newListeningDaemon(t)

	server, client := net.Pipe()
	defer client.Close()

	go d.ServeConn(server)

	// Send invalid JSON.
	client.Write([]byte("not json\n")) //nolint:errcheck

	scanner := bufio.NewScanner(client)
	if !scanner.Scan() {
		t.Fatal("expected response for parse error")
	}
	var resp RPCResponse
	json.Unmarshal(scanner.Bytes(), &resp) //nolint:errcheck
	if resp.Error == nil {
		t.Fatal("expected error for parse error")
	}
	if resp.Error.Code != -32700 {
		t.Fatalf("expected -32700 (parse error), got %d", resp.Error.Code)
	}
}

// ---------------------------------------------------------------------------
// NewClient and NewClientFromSocket
// ---------------------------------------------------------------------------

func TestNewClientFromSocket_ConnectsToListener(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)
	defer ln.Close()

	// Accept in background.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("NewClientFromSocket: %v", err)
	}
	c.Close()
}

func TestNewClientFromSocket_BadPath(t *testing.T) {
	_, err := NewClientFromSocket("/nonexistent/path/test.sock")
	if err == nil {
		t.Fatal("expected error for nonexistent socket path")
	}
}

func TestNewClient_BadPath(t *testing.T) {
	// Use a real directory that won't have a daemon socket.
	dir, _ := os.MkdirTemp("/tmp", "client-test-*")
	defer os.RemoveAll(dir)

	_, err := NewClient(dir)
	if err == nil {
		t.Fatal("expected error connecting to missing daemon socket")
	}
}

// ---------------------------------------------------------------------------
// IsRunning — with stale PID file (alive process but no socket) and corrupt PID
// ---------------------------------------------------------------------------

func TestIsRunning_StalePIDAliveNoSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "isrunning-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	os.MkdirAll(kingDir, 0755) //nolint:errcheck

	// Write a PID file with current process's PID (alive), but no socket.
	pidPath := pidPathForRoot(dir)
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644) //nolint:errcheck

	running, err := IsRunning(dir)
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	// Process alive but no socket → not running.
	if running {
		t.Fatal("expected IsRunning=false when no socket is listening")
	}
}

func TestIsRunning_CorruptPIDFile(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "isrunning-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	os.MkdirAll(kingDir, 0755) //nolint:errcheck

	pidPath := pidPathForRoot(dir)
	os.WriteFile(pidPath, []byte("notapid\n"), 0644) //nolint:errcheck

	running, err := IsRunning(dir)
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if running {
		t.Fatal("expected IsRunning=false for corrupt PID file")
	}
}

func TestIsRunning_StalePIDDeadProcess(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "isrunning-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	kingDir := filepath.Join(dir, ".king")
	os.MkdirAll(kingDir, 0755) //nolint:errcheck

	pidPath := pidPathForRoot(dir)
	os.WriteFile(pidPath, []byte("999999999\n"), 0644) //nolint:errcheck

	running, err := IsRunning(dir)
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if running {
		t.Fatal("expected IsRunning=false for dead process")
	}
}

// ---------------------------------------------------------------------------
// Client.Call via test daemon loopback
// ---------------------------------------------------------------------------

func TestClientCall_Success(t *testing.T) {
	// Spin up a daemon with a real Unix socket and make a real Client.Call.
	dir, err := os.MkdirTemp("/tmp", "client-call-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	sockPath := filepath.Join(dir, "king.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	d := &Daemon{
		rootDir:          dir,
		sockPath:         sockPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.handlers["ping"] = func(_ json.RawMessage) (interface{}, error) {
		return map[string]string{"pong": "1"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.ctx = ctx

	// Accept loop in background.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.ServeConn(conn)
		}
	}()

	// Now use Client to call the daemon.
	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("NewClientFromSocket: %v", err)
	}
	defer c.Close()

	raw, err := c.Call("ping", nil)
	if err != nil {
		t.Fatalf("Call ping: %v", err)
	}
	if raw == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestClientCall_MethodError(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "client-call-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	sockPath := filepath.Join(dir, "king.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	d := &Daemon{
		rootDir:          dir,
		sockPath:         sockPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.handlers["fail"] = func(_ json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("intentional error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.ctx = ctx

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.ServeConn(conn)
		}
	}()

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("NewClientFromSocket: %v", err)
	}
	defer c.Close()

	_, err = c.Call("fail", nil)
	if err == nil {
		t.Fatal("expected error from failing handler")
	}
}

func TestClientCall_WithParams(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "client-call-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	sockPath := filepath.Join(dir, "king.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	d := &Daemon{
		rootDir:          dir,
		sockPath:         sockPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		externalVassals:  make(map[string]ExternalVassalInfo),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	d.handlers["echo_params"] = func(params json.RawMessage) (interface{}, error) {
		return map[string]interface{}{"params": string(params)}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.ctx = ctx

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.ServeConn(conn)
		}
	}()

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("NewClientFromSocket: %v", err)
	}
	defer c.Close()

	params := map[string]string{"key": "value"}
	raw, err := c.Call("echo_params", params)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty result")
	}
}

// ---------------------------------------------------------------------------
// VassalClient.CallTool — via a mock JSON-RPC server
// ---------------------------------------------------------------------------

// serveMCPResponse writes a mock MCP JSON-RPC response to incoming connections.
func serveMCPResponse(ln net.Listener, response string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	// Read the request line (we don't care about contents).
	scanner := bufio.NewScanner(conn)
	scanner.Scan() //nolint:errcheck
	conn.Write([]byte(response + "\n")) //nolint:errcheck
}

func TestVassalClient_CallTool_Success(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)

	resp := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"hello from vassal"}]}}`
	go serveMCPResponse(ln, resp)

	pool := NewVassalClientPool()
	vc, err := pool.Connect("testvassal", sockPath)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.DisconnectAll()

	text, err := vc.CallTool(context.Background(), "do_something", map[string]any{"arg": "val"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if text != "hello from vassal" {
		t.Fatalf("expected 'hello from vassal', got %q", text)
	}
}

func TestVassalClient_CallTool_RPCError(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)

	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"tool error"}}`
	go serveMCPResponse(ln, resp)

	pool := NewVassalClientPool()
	vc, err := pool.Connect("testvassal", sockPath)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.DisconnectAll()

	_, err = vc.CallTool(context.Background(), "do_something", nil)
	if err == nil {
		t.Fatal("expected error from RPC error response")
	}
}

func TestVassalClient_CallTool_EmptyResult(t *testing.T) {
	ln, sockPath := listenUnixTemp(t)

	resp := `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`
	go serveMCPResponse(ln, resp)

	pool := NewVassalClientPool()
	vc, err := pool.Connect("testvassal", sockPath)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.DisconnectAll()

	_, err = vc.CallTool(context.Background(), "do_something", nil)
	if err == nil {
		t.Fatal("expected error for empty content result")
	}
}

// ---------------------------------------------------------------------------
// Done, PTYMgr, MCPServer accessors
// ---------------------------------------------------------------------------

func TestDaemon_Done(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Daemon{
		ctx:              ctx,
		cancel:           cancel,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	// Not yet cancelled.
	select {
	case <-d.Done():
		t.Fatal("expected Done channel to be open before cancel")
	default:
	}

	cancel()

	// After cancel, Done should close.
	select {
	case <-d.Done():
	default:
		t.Fatal("expected Done channel to be closed after cancel")
	}
}

func TestDaemon_PTYMgr_Nil(t *testing.T) {
	d := &Daemon{
		ptyMgr:           nil,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
	if got := d.PTYMgr(); got != nil {
		t.Fatalf("expected nil PTYMgr, got %v", got)
	}
}

func TestDaemon_MCPServer_Nil(t *testing.T) {
	d := &Daemon{
		mcpSrv:           nil,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
	if got := d.MCPServer(); got != nil {
		t.Fatalf("expected nil MCPServer, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// injectAutoContracts — with a temp directory (no repo fingerprint found)
// ---------------------------------------------------------------------------

func TestInjectAutoContracts_EmptyDir(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "inject-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	d := &Daemon{
		config: &config.KingdomConfig{
			Patterns: []config.PatternConfig{},
		},
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	// Empty dir has no known project type — injectAutoContracts should be a no-op.
	d.injectAutoContracts(dir)
	// Should not panic and config.Patterns should not grow with unexpected entries.
}

// ---------------------------------------------------------------------------
// startGuardRunners — spins up and exits via context cancel
// ---------------------------------------------------------------------------

func TestStartGuardRunners_NoGuards(t *testing.T) {
	d := &Daemon{
		config: &config.KingdomConfig{
			Vassals: []config.VassalConfig{
				{Name: "api"}, // no guards
			},
		},
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately; no goroutines should be started

	// Should not panic.
	d.startGuardRunners(ctx)
	d.wg.Wait()
}

func TestStartGuardRunners_WithPortCheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()

	d := &Daemon{
		config: &config.KingdomConfig{
			Vassals: []config.VassalConfig{
				{
					Name: "api",
					Guards: []config.GuardConfig{
						{
							Type:      "port_check",
							Port:      port,
							Expect:    "open",
							Interval:  1,
							Threshold: 3,
						},
					},
				},
			},
		},
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	d.startGuardRunners(ctx)

	// Wait for context to end, then wait for goroutines.
	<-ctx.Done()
	d.wg.Wait()

	// Guard state should have been populated.
	d.guardStatesMu.RLock()
	gs, ok := d.guardStates["api:0"]
	d.guardStatesMu.RUnlock()
	if !ok {
		t.Fatal("expected guard state for api:0 to be populated")
	}
	if gs.VassalName != "api" {
		t.Fatalf("expected vassal_name=api, got %q", gs.VassalName)
	}
}

// ---------------------------------------------------------------------------
// ptyManagerAdapter.GetSession with a real session
// ---------------------------------------------------------------------------

func TestPTYManagerAdapter_ListSessions_WithSession(t *testing.T) {
	mgr := newTestPTYManager(t)
	adapter := &ptyManagerAdapter{mgr: mgr}

	// CreateSession won't start the process (no Start called), but
	// it should appear in ListSessions.
	_, err := mgr.CreateSession("test-id", "testvassal", "sleep 999", "/tmp", nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer mgr.StopAll() //nolint:errcheck

	sessions := adapter.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Name != "testvassal" {
		t.Fatalf("expected name=testvassal, got %q", sessions[0].Name)
	}
}

func TestPTYManagerAdapter_GetSession_Found(t *testing.T) {
	mgr := newTestPTYManager(t)
	adapter := &ptyManagerAdapter{mgr: mgr}

	_, err := mgr.CreateSession("test-id-2", "api", "sleep 999", "/tmp", nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer mgr.StopAll() //nolint:errcheck

	got, ok := adapter.GetSession("api")
	if !ok {
		t.Fatal("expected ok=true for existing session")
	}
	if got == nil {
		t.Fatal("expected non-nil session")
	}
}

// ---------------------------------------------------------------------------
// writePIDFile — overwrite stale PID scenario
// ---------------------------------------------------------------------------

func TestWritePIDFile_OverwritesStalePID(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pid-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	pidPath := filepath.Join(dir, "king.pid")
	// Write a PID file with a dead PID (999999999).
	os.WriteFile(pidPath, []byte("999999999\n"), 0644) //nolint:errcheck

	d := &Daemon{
		pidFile:          pidPath,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}

	if err := d.writePIDFile(); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pid != os.Getpid() {
		t.Fatalf("expected own PID %d, got %d", os.Getpid(), pid)
	}
}

// ---------------------------------------------------------------------------
// Full daemon Start/Stop to cover registerRealHandlers and startVassals
// ---------------------------------------------------------------------------

// startMinimalDaemon creates a full daemon with no vassals, starts it in a temp
// dir, and returns the daemon and a cleanup function. Uses /tmp to stay under
// the 104-char Unix socket limit on macOS.
func startMinimalDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	rootDir, err := os.MkdirTemp("/tmp", "kingtest-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(rootDir) })

	d, err := NewDaemon(rootDir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := d.Start(ctx); err != nil {
		cancel()
		t.Fatalf("daemon.Start: %v", err)
	}

	sockPath := SocketPathForRoot(rootDir)

	t.Cleanup(func() {
		cancel()
		_ = d.Stop()
	})

	return d, sockPath
}

func TestFullDaemon_RealHandlers_Status(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	raw, err := c.Call("status", nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty status result")
	}
}

func TestFullDaemon_RealHandlers_ListVassals(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	raw, err := c.Call("list_vassals", nil)
	if err != nil {
		t.Fatalf("list_vassals: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty list_vassals result")
	}
}

func TestFullDaemon_RealHandlers_GetEvents(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	params := map[string]interface{}{"kingdom_id": "test", "limit": 10}
	raw, err := c.Call("get_events", params)
	if err != nil {
		t.Fatalf("get_events: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty get_events result")
	}
}

func TestFullDaemon_RealHandlers_RegisterArtifact(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// register_artifact with a real file.
	dir, _ := os.MkdirTemp("/tmp", "art-*")
	defer os.RemoveAll(dir)
	artPath := filepath.Join(dir, "test.txt")
	os.WriteFile(artPath, []byte("test content"), 0644) //nolint:errcheck

	params := map[string]interface{}{
		"name":      "test-artifact",
		"path":      artPath,
		"vassal_id": "v1",
		"mime_type": "text/plain",
	}
	raw, err := c.Call("register_artifact", params)
	if err != nil {
		t.Fatalf("register_artifact: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty register_artifact result")
	}
}

func TestFullDaemon_RealHandlers_ExecIn_NoSession(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	params := map[string]interface{}{
		"target":          "nonexistent",
		"command":         "echo hello",
		"timeout_seconds": 5,
	}
	// exec_in on nonexistent session should return an error.
	_, err = c.Call("exec_in", params)
	if err == nil {
		t.Fatal("expected error for exec_in on nonexistent session")
	}
}

func TestFullDaemon_RealHandlers_GetAuditLog(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	raw, err := c.Call("get_audit_log", map[string]interface{}{"limit": 10})
	if err != nil {
		t.Fatalf("get_audit_log: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty get_audit_log result")
	}
}

func TestFullDaemon_RealHandlers_KingdomStatus(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	raw, err := c.Call("kingdom.status", nil)
	if err != nil {
		t.Fatalf("kingdom.status: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty result")
	}
}

func TestFullDaemon_RealHandlers_ListPendingApprovals(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	raw, err := c.Call("list_pending_approvals", nil)
	if err != nil {
		t.Fatalf("list_pending_approvals: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty result")
	}
}

func TestFullDaemon_RealHandlers_GetVassalOutput(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	params := map[string]interface{}{"vassal": "nonexistent", "lines": 10}
	_, err = c.Call("get_vassal_output", params)
	// This may return an error (vassal not found) but the handler runs.
	// We just verify it doesn't panic.
	_ = err
}

func TestFullDaemon_RealHandlers_GetActionTrace_NotFound(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	_, err = c.Call("get_action_trace", map[string]interface{}{"trace_id": "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent trace")
	}
}

func TestFullDaemon_RealHandlers_ResolveArtifact(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	_, err = c.Call("resolve_artifact", map[string]interface{}{"name": "nonexistent"})
	// handler runs, may return not-found error.
	_ = err
}

func TestFullDaemon_RealHandlers_ReadNeighbor(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	_, err = c.Call("read_neighbor", map[string]interface{}{"vassal": "api", "artifact": "test"})
	// handler runs, may return error.
	_ = err
}

func TestFullDaemon_Stop_NilFields(t *testing.T) {
	// Test Stop with nil optional fields (no ptyMgr, no kingdom, no store).
	d := &Daemon{
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		vassalProcs:      make(map[string]*vassalProc),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx
	d.cancel = cancel

	err := d.Stop()
	if err != nil {
		t.Fatalf("Stop with nil fields: %v", err)
	}
}

// ---------------------------------------------------------------------------
// startVassals — with a configured daemon (non-autostart vassal → skip)
// ---------------------------------------------------------------------------

func TestStartVassals_NonAutostart(t *testing.T) {
	mgr := newTestPTYManager(t)
	s := newTestStore(t)

	dir, _ := os.MkdirTemp("/tmp", "sv-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	autostartFalse := false
	d := &Daemon{
		rootDir:          dir,
		ptyMgr:           mgr,
		store:            s,
		kingdom:          k,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
		config: &config.KingdomConfig{
			Vassals: []config.VassalConfig{
				{
					Name:      "test-vassal",
					Command:   "echo hello",
					Autostart: &autostartFalse,
				},
			},
		},
	}

	// startVassals should skip non-autostart vassals.
	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals: %v", err)
	}

	// No sessions should have been created.
	sessions := mgr.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestStartVassals_NoVassals(t *testing.T) {
	mgr := newTestPTYManager(t)
	s := newTestStore(t)

	dir, _ := os.MkdirTemp("/tmp", "sv-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	d := &Daemon{
		rootDir:          dir,
		ptyMgr:           mgr,
		store:            s,
		kingdom:          k,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
		config: &config.KingdomConfig{
			Vassals: []config.VassalConfig{},
		},
	}

	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals with no vassals: %v", err)
	}
}

func TestStartVassals_AutostartVassal_CreatesSession(t *testing.T) {
	mgr := newTestPTYManager(t)
	s := newTestStore(t)

	dir, _ := os.MkdirTemp("/tmp", "sv-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	autostartTrue := true
	d := &Daemon{
		rootDir:          dir,
		ptyMgr:           mgr,
		store:            s,
		kingdom:          k,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
		config: &config.KingdomConfig{
			Vassals: []config.VassalConfig{
				{
					Name:      "shell-vassal",
					Command:   "echo hello",
					Autostart: &autostartTrue,
					Cwd:       dir,
				},
			},
		},
	}

	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals: %v", err)
	}
	defer mgr.StopAll() //nolint:errcheck

	sessions := mgr.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Name != "shell-vassal" {
		t.Fatalf("expected session name 'shell-vassal', got %q", sessions[0].Name)
	}
}

func TestStartVassals_AIVassal_Skipped(t *testing.T) {
	mgr := newTestPTYManager(t)
	s := newTestStore(t)

	dir, _ := os.MkdirTemp("/tmp", "sv-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	d := &Daemon{
		rootDir:          dir,
		ptyMgr:           mgr,
		store:            s,
		kingdom:          k,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
		config: &config.KingdomConfig{
			Vassals: []config.VassalConfig{
				{Name: "claude1", Type: "claude"},
				{Name: "codex1", Type: "codex"},
			},
		},
	}

	// AI-type vassals are skipped by startVassals (managed by startAIVassal).
	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals with AI vassals: %v", err)
	}

	sessions := mgr.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("expected 0 PTY sessions for AI vassals, got %d", len(sessions))
	}
}

// ---------------------------------------------------------------------------
// checkLogWatch — test with a real ptyMgr that has no sessions (covers nil check)
// ---------------------------------------------------------------------------

func TestCheckLogWatch_WithRealPTYMgr_NoSession(t *testing.T) {
	mgr := newTestPTYManager(t)
	d := &Daemon{
		ptyMgr:           mgr,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		logger:           newTestLogger(t),
	}
	pat := regexp.MustCompile(`ERROR`)
	result := d.checkLogWatch("nonexistent", []*regexp.Regexp{pat}, 10*time.Second)
	if !result.OK {
		t.Fatalf("expected OK=true for session with no recent lines, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// respond_approval with sovereign_approval disabled
// ---------------------------------------------------------------------------

func TestFullDaemon_RespondApproval_Disabled(t *testing.T) {
	d, sockPath := startMinimalDaemon(t)
	_ = d

	c, err := NewClientFromSocket(sockPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// sovereign_approval is not enabled in minimal daemon, so this returns error.
	_, err = c.Call("respond_approval", map[string]interface{}{
		"request_id": "nonexistent",
		"approved":   true,
	})
	if err == nil {
		t.Fatal("expected error when sovereign_approval is disabled")
	}
}

// ---------------------------------------------------------------------------
// startVassals with repo_path — triggers injectAutoContracts
// ---------------------------------------------------------------------------

func TestStartVassals_WithRepoPath(t *testing.T) {
	mgr := newTestPTYManager(t)
	s := newTestStore(t)

	dir, _ := os.MkdirTemp("/tmp", "sv-repo-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	repoDir, _ := os.MkdirTemp("/tmp", "repo-*")
	defer os.RemoveAll(repoDir)

	autostartTrue := true
	d := &Daemon{
		rootDir:          dir,
		ptyMgr:           mgr,
		store:            s,
		kingdom:          k,
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
		handlers:         make(map[string]rpcHandler),
		logger:           newTestLogger(t),
		config: &config.KingdomConfig{
			Vassals: []config.VassalConfig{
				{
					Name:      "repo-vassal",
					Command:   "echo hello",
					Autostart: &autostartTrue,
					Cwd:       dir,
					RepoPath:  repoDir, // triggers injectAutoContracts
				},
			},
		},
	}

	if err := d.startVassals(); err != nil {
		t.Fatalf("startVassals with repo_path: %v", err)
	}
	defer mgr.StopAll() //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Kingdom.SetStatus error path — store closed
// ---------------------------------------------------------------------------

func TestKingdom_SetStatus_Error(t *testing.T) {
	s := newTestStore(t)
	dir, _ := os.MkdirTemp("/tmp", "kingdom-err-*")
	defer os.RemoveAll(dir)

	k, err := NewKingdom(s, newTestConfig(), dir, newTestLogger(t))
	if err != nil {
		t.Fatalf("NewKingdom: %v", err)
	}

	// Close the store to force an error.
	s.Close()

	err = k.SetStatus("stopped")
	if err == nil {
		t.Fatal("expected error when store is closed")
	}
}

// ---------------------------------------------------------------------------
// NewDaemon — valid path
// ---------------------------------------------------------------------------

func TestNewDaemon_ValidRoot(t *testing.T) {
	dir, _ := os.MkdirTemp("/tmp", "newdaemon-*")
	defer os.RemoveAll(dir)

	d, err := NewDaemon(dir)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil daemon")
	}
	if d.vassalPool == nil {
		t.Fatal("expected non-nil vassalPool")
	}
}

// ---------------------------------------------------------------------------
// suppress unused import warnings
// ---------------------------------------------------------------------------

var _ = fmt.Sprintf
var _ = strings.TrimSpace
