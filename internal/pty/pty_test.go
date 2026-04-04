package pty_test

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/pty"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newMemStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedKingdom(t *testing.T, s *store.Store) string {
	t.Helper()
	id := uuid.New().String()
	_ = s.CreateKingdom(store.Kingdom{
		ID: id, Name: "k", RootPath: "/tmp/" + id,
		Status: "running", CreatedAt: "2026-01-01 00:00:00", UpdatedAt: "2026-01-01 00:00:00",
	})
	return id
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// RingBuffer
// ---------------------------------------------------------------------------

func TestRingBuffer_WriteAndRead(t *testing.T) {
	rb := pty.NewRingBuffer(64)
	data := []byte("hello world")
	n, err := rb.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected n=%d, got %d", len(data), n)
	}
	got := rb.Bytes()
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	// Buffer size 8; write more than 8 bytes so it wraps.
	rb := pty.NewRingBuffer(8)
	_, _ = rb.Write([]byte("AAAAAAAA")) // fills buffer
	_, _ = rb.Write([]byte("BBB"))      // overwrites oldest 3 bytes

	got := rb.Bytes()
	// After wrap: oldest 3 bytes (AAA) overwritten by BBB; buffer = AAAAAABBB → order: BBBAAAAA → no, size=8
	// Actually: write 8 A's (full), then write 3 B's overwriting first 3 positions.
	// buf = [B, B, B, A, A, A, A, A], write pointer at 3, full=true
	// Bytes() returns from write pointer: buf[3:] + buf[:3] = AAAAABBB
	if len(got) != 8 {
		t.Errorf("expected 8 bytes from full ring buffer, got %d", len(got))
	}
	if !strings.HasSuffix(string(got), "BBB") {
		t.Errorf("expected last 3 bytes to be BBB, got %q", got)
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	rb := pty.NewRingBuffer(16)
	got := rb.Bytes()
	if len(got) != 0 {
		t.Errorf("expected empty buffer, got %d bytes", len(got))
	}
}

func TestRingBuffer_WriteChunks(t *testing.T) {
	rb := pty.NewRingBuffer(100)
	_, _ = rb.Write([]byte("foo"))
	_, _ = rb.Write([]byte("bar"))
	_, _ = rb.Write([]byte("baz"))
	got := string(rb.Bytes())
	if got != "foobarbaz" {
		t.Errorf("expected foobarbaz, got %q", got)
	}
}

func TestRingBuffer_LargerThanBuffer(t *testing.T) {
	rb := pty.NewRingBuffer(4)
	// Writing more than the buffer size in one call.
	_, _ = rb.Write([]byte("ABCDEFGH")) // 8 bytes into 4-byte buffer
	got := rb.Bytes()
	if len(got) != 4 {
		t.Errorf("expected 4 bytes, got %d", len(got))
	}
	// Last 4 bytes of the input should remain.
	if string(got) != "EFGH" {
		t.Errorf("expected EFGH, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Session
// ---------------------------------------------------------------------------

func TestNewSession_EmptyCommand(t *testing.T) {
	_, err := pty.NewSession("id", "name", "", "/tmp", nil)
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

func TestSession_StartAndStop(t *testing.T) {
	s, err := pty.NewSession(uuid.New().String(), "test", "cat", "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if s.Status != pty.StatusRunning {
		t.Errorf("expected status=running, got %s", s.Status)
	}
	if s.PID == 0 {
		t.Error("expected non-zero PID after start")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSession_StartAlreadyRunning(t *testing.T) {
	s, _ := pty.NewSession(uuid.New().String(), "test", "cat", "/tmp", nil)
	_ = s.Start()
	defer s.Stop()

	err := s.Start()
	if err == nil {
		t.Fatal("expected error starting an already-running session")
	}
}

func TestSession_ExecCommand(t *testing.T) {
	s, err := pty.NewSession(uuid.New().String(), "bash-test", "/bin/bash", "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	output, exitCode, _, err := s.ExecCommand("echo hello", 10*time.Second)
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", output)
	}
}

func TestSession_ExecCommand_NonZeroExit(t *testing.T) {
	s, _ := pty.NewSession(uuid.New().String(), "bash-test", "/bin/bash", "/tmp", nil)
	_ = s.Start()
	defer s.Stop()

	// exit 42 kills the shell; ExecCommand either returns exit code 42 or a timeout error.
	_, exitCode, _, err := s.ExecCommand("exit 42", 3*time.Second)
	if err == nil && exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", exitCode)
	}
}

func TestSession_ExecCommand_NotRunning(t *testing.T) {
	s, _ := pty.NewSession(uuid.New().String(), "test", "cat", "/tmp", nil)
	// Don't start — should return error.
	_, _, _, err := s.ExecCommand("echo hi", time.Second)
	if err == nil {
		t.Fatal("expected error for exec on non-running session")
	}
}

func TestSession_SetOnOutput(t *testing.T) {
	s, _ := pty.NewSession(uuid.New().String(), "bash-test", "/bin/bash", "/tmp", nil)
	_ = s.Start()
	defer s.Stop()

	var lines []string
	s.SetOnOutput(func(line string) {
		lines = append(lines, line)
	})

	_, _, _, _ = s.ExecCommand("echo callback-test", 5*time.Second)
	time.Sleep(100 * time.Millisecond)

	found := false
	for _, l := range lines {
		if strings.Contains(l, "callback-test") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'callback-test' in output callback lines, got: %v", lines)
	}
}

func TestSession_BytesWritten(t *testing.T) {
	s, _ := pty.NewSession(uuid.New().String(), "bash-test", "/bin/bash", "/tmp", nil)
	_ = s.Start()
	defer s.Stop()

	_, _, _, _ = s.ExecCommand("echo bytes-test", 5*time.Second)

	if s.BytesWritten() == 0 {
		t.Error("expected BytesWritten > 0 after command output")
	}
}

func TestSession_RecentOutputLines(t *testing.T) {
	s, _ := pty.NewSession(uuid.New().String(), "bash-test", "/bin/bash", "/tmp", nil)
	_ = s.Start()
	defer s.Stop()

	before := time.Now()
	_, _, _, _ = s.ExecCommand("echo recent-line", 5*time.Second)
	time.Sleep(50 * time.Millisecond)

	lines := s.RecentOutputLines(before)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "recent-line") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'recent-line' in recent output, got: %v", lines)
	}
}

// ---------------------------------------------------------------------------
// Manager
// ---------------------------------------------------------------------------

func TestManager_CreateAndGetSession(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	sess, err := m.CreateSession(uuid.New().String(), "worker", "/bin/bash", "/tmp", nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer m.StopSession("worker")

	got, ok := m.GetSession("worker")
	if !ok {
		t.Fatal("GetSession returned false for existing session")
	}
	if got.Name != sess.Name {
		t.Errorf("name mismatch: got %s, want %s", got.Name, sess.Name)
	}
}

func TestManager_DuplicateSessionName(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	_, err := m.CreateSession(uuid.New().String(), "dup", "/bin/bash", "/tmp", nil)
	if err != nil {
		t.Fatalf("first CreateSession: %v", err)
	}
	defer m.StopSession("dup")

	_, err = m.CreateSession(uuid.New().String(), "dup", "/bin/bash", "/tmp", nil)
	if err == nil {
		t.Fatal("expected error for duplicate session name")
	}
}

func TestManager_ListSessions(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	for _, name := range []string{"a", "b", "c"} {
		_, err := m.CreateSession(uuid.New().String(), name, "/bin/bash", "/tmp", nil)
		if err != nil {
			t.Fatalf("CreateSession(%s): %v", name, err)
		}
	}
	defer m.StopAll()

	sessions := m.ListSessions()
	if len(sessions) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(sessions))
	}
}

func TestManager_StopSession_NotFound(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	err := m.StopSession("nonexistent")
	if err == nil {
		t.Fatal("expected error stopping non-existent session")
	}
}

func TestManager_SetOnOutput(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	_, err := m.CreateSession(uuid.New().String(), "out-test", "/bin/bash", "/tmp", nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer m.StopSession("out-test")

	if err := m.SetOnOutput("out-test", func(string) {}); err != nil {
		t.Errorf("SetOnOutput: %v", err)
	}

	if err := m.SetOnOutput("nonexistent", func(string) {}); err == nil {
		t.Error("expected error for SetOnOutput on nonexistent session")
	}
}

func TestManager_GetSessionBytesWritten_Missing(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	if n := m.GetSessionBytesWritten("nope"); n != 0 {
		t.Errorf("expected 0 for missing session, got %d", n)
	}
}

func TestManager_GetSessionRecentLines_Missing(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	if lines := m.GetSessionRecentLines("nope", time.Now()); lines != nil {
		t.Errorf("expected nil for missing session, got %v", lines)
	}
}

// ---------------------------------------------------------------------------
// Session.GetOutput
// ---------------------------------------------------------------------------

func TestSession_GetOutput_EmptyBeforeStart(t *testing.T) {
	s, err := pty.NewSession(uuid.New().String(), "test", "cat", "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	got := s.GetOutput()
	if len(got) != 0 {
		t.Errorf("expected empty output before start, got %d bytes", len(got))
	}
}

func TestSession_GetOutput_AfterCommand(t *testing.T) {
	s, err := pty.NewSession(uuid.New().String(), "sh-test", "/bin/sh", "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	_, _, _, _ = s.ExecCommand("echo getoutput-marker", 5*time.Second)
	time.Sleep(50 * time.Millisecond)

	out := s.GetOutput()
	if len(out) == 0 {
		t.Error("expected non-empty output after command")
	}
	if !strings.Contains(string(out), "getoutput-marker") {
		t.Errorf("expected 'getoutput-marker' in output, got %q", string(out))
	}
}

// ---------------------------------------------------------------------------
// Session.Resize
// ---------------------------------------------------------------------------

func TestSession_Resize_NotRunning(t *testing.T) {
	s, _ := pty.NewSession(uuid.New().String(), "test", "cat", "/tmp", nil)
	err := s.Resize(24, 80)
	if err == nil {
		t.Fatal("expected error resizing a non-running session, got nil")
	}
}

func TestSession_Resize_Running(t *testing.T) {
	s, err := pty.NewSession(uuid.New().String(), "bash-resize", "/bin/bash", "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	if err := s.Resize(40, 120); err != nil {
		t.Errorf("Resize on running session: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Session.StartHangDetector
// ---------------------------------------------------------------------------

func TestSession_StartHangDetector_Fires(t *testing.T) {
	s, err := pty.NewSession(uuid.New().String(), "bash-hang", "/bin/bash", "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	fired := make(chan string, 1)
	s.StartHangDetector(200*time.Millisecond, func(name string) {
		select {
		case fired <- name:
		default:
		}
	})

	select {
	case name := <-fired:
		if name != "bash-hang" {
			t.Errorf("expected fired name='bash-hang', got %q", name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hang detector did not fire within timeout")
	}
}

// ---------------------------------------------------------------------------
// Manager.RecoverSessions
// ---------------------------------------------------------------------------

func TestManager_RecoverSessions_NoVassals(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	if err := m.RecoverSessions(); err != nil {
		t.Errorf("RecoverSessions with no vassals: %v", err)
	}
}

func TestManager_RecoverSessions_MarksDeadVassalTerminated(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	vassalID := uuid.New().String()
	now := "2026-01-01 00:00:00"
	_ = s.CreateVassal(store.Vassal{
		ID:           vassalID,
		KingdomID:    kingdomID,
		Name:         "dead-vassal",
		Command:      "/bin/bash",
		Status:       pty.StatusRunning,
		PID:          999999999,
		CreatedAt:    now,
		LastActivity: now,
	})

	if err := m.RecoverSessions(); err != nil {
		t.Fatalf("RecoverSessions: %v", err)
	}

	v, err := s.GetVassal(vassalID)
	if err != nil {
		t.Fatalf("GetVassal: %v", err)
	}
	if v.Status != pty.StatusTerminated {
		t.Errorf("expected status=%s, got %q", pty.StatusTerminated, v.Status)
	}
}

func TestManager_RecoverSessions_MarksNoPIDTerminated(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	vassalID := uuid.New().String()
	now := "2026-01-01 00:00:00"
	_ = s.CreateVassal(store.Vassal{
		ID:           vassalID,
		KingdomID:    kingdomID,
		Name:         "no-pid-vassal",
		Command:      "/bin/bash",
		Status:       pty.StatusRunning,
		PID:          0,
		CreatedAt:    now,
		LastActivity: now,
	})

	if err := m.RecoverSessions(); err != nil {
		t.Fatalf("RecoverSessions: %v", err)
	}

	v, err := s.GetVassal(vassalID)
	if err != nil {
		t.Fatalf("GetVassal: %v", err)
	}
	if v.Status != pty.StatusTerminated {
		t.Errorf("expected status=%s for no-PID vassal, got %q", pty.StatusTerminated, v.Status)
	}
}

func TestManager_RecoverSessions_SkipsTerminated(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, discardLogger())

	vassalID := uuid.New().String()
	now := "2026-01-01 00:00:00"
	_ = s.CreateVassal(store.Vassal{
		ID:           vassalID,
		KingdomID:    kingdomID,
		Name:         "already-terminated",
		Command:      "/bin/bash",
		Status:       pty.StatusTerminated,
		PID:          0,
		CreatedAt:    now,
		LastActivity: now,
	})

	if err := m.RecoverSessions(); err != nil {
		t.Fatalf("RecoverSessions: %v", err)
	}

	v, _ := s.GetVassal(vassalID)
	if v.Status != pty.StatusTerminated {
		t.Errorf("expected status=terminated unchanged, got %q", v.Status)
	}
}

// ---------------------------------------------------------------------------
// NewManager — nil logger
// ---------------------------------------------------------------------------

func TestNewManager_NilLogger(t *testing.T) {
	s := newMemStore(t)
	kingdomID := seedKingdom(t, s)
	m := pty.NewManager(s, kingdomID, nil)
	if m == nil {
		t.Fatal("expected non-nil manager with nil logger")
	}
}
