package vassal_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/vassal"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// NewTask
// ---------------------------------------------------------------------------

func TestNewTask_IDPrefix(t *testing.T) {
	task := vassal.NewTask("worker", "do something", nil)
	if len(task.ID) < 3 || task.ID[:2] != "t-" {
		t.Errorf("expected ID to start with 't-', got %q", task.ID)
	}
}

func TestNewTask_FieldsSet(t *testing.T) {
	ctx := map[string]any{"key": "value"}
	task := vassal.NewTask("worker", "build app", ctx)

	if task.VassalName != "worker" {
		t.Errorf("VassalName = %q, want %q", task.VassalName, "worker")
	}
	if task.Task != "build app" {
		t.Errorf("Task = %q, want %q", task.Task, "build app")
	}
	if task.Status != vassal.TaskStatusAccepted {
		t.Errorf("Status = %q, want %q", task.Status, vassal.TaskStatusAccepted)
	}
	if task.Context == nil {
		t.Error("expected non-nil Context")
	}
}

func TestNewTask_UniqueIDs(t *testing.T) {
	t1 := vassal.NewTask("w", "task", nil)
	t2 := vassal.NewTask("w", "task", nil)
	if t1.ID == t2.ID {
		t.Error("expected unique IDs for different tasks")
	}
}

func TestNewTask_NilContext(t *testing.T) {
	task := vassal.NewTask("w", "task", nil)
	if task.Context != nil {
		t.Errorf("expected nil context, got %v", task.Context)
	}
}

// ---------------------------------------------------------------------------
// SaveTask / LoadTask
// ---------------------------------------------------------------------------

func TestSaveAndLoadTask_RoundTrip(t *testing.T) {
	kingDir := t.TempDir()

	task := vassal.NewTask("worker", "build the thing", nil)
	task.Status = vassal.TaskStatusRunning

	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	loaded, err := vassal.LoadTask(kingDir, task.ID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}

	if loaded.ID != task.ID {
		t.Errorf("ID mismatch: got %s, want %s", loaded.ID, task.ID)
	}
	if loaded.Status != vassal.TaskStatusRunning {
		t.Errorf("Status mismatch: got %s, want %s", loaded.Status, vassal.TaskStatusRunning)
	}
	if loaded.Task != task.Task {
		t.Errorf("Task mismatch: got %q, want %q", loaded.Task, task.Task)
	}
}

func TestSaveTask_CreatesTasksDir(t *testing.T) {
	kingDir := t.TempDir()
	task := vassal.NewTask("w", "t", nil)

	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	tasksDir := filepath.Join(kingDir, "tasks")
	if _, err := os.Stat(tasksDir); err != nil {
		t.Errorf("expected tasks dir to be created: %v", err)
	}
}

func TestSaveTask_UpdatesUpdatedAt(t *testing.T) {
	kingDir := t.TempDir()
	task := vassal.NewTask("w", "t", nil)
	before := task.UpdatedAt

	time.Sleep(2 * time.Millisecond)
	if err := vassal.SaveTask(kingDir, task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	if !task.UpdatedAt.After(before) {
		t.Error("expected UpdatedAt to be updated after Save")
	}
}

func TestSaveTask_PersistsOutputAndError(t *testing.T) {
	kingDir := t.TempDir()
	task := vassal.NewTask("w", "t", nil)
	task.Output = "hello output"
	task.Error = "some error"
	task.Status = vassal.TaskStatusFailed

	_ = vassal.SaveTask(kingDir, task)

	loaded, err := vassal.LoadTask(kingDir, task.ID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if loaded.Output != "hello output" {
		t.Errorf("Output mismatch: %q", loaded.Output)
	}
	if loaded.Error != "some error" {
		t.Errorf("Error mismatch: %q", loaded.Error)
	}
	if loaded.Status != vassal.TaskStatusFailed {
		t.Errorf("Status mismatch: %s", loaded.Status)
	}
}

func TestSaveTask_PersistsArtifacts(t *testing.T) {
	kingDir := t.TempDir()
	task := vassal.NewTask("w", "t", nil)
	task.Artifacts = []string{"file1.txt", "file2.json"}

	_ = vassal.SaveTask(kingDir, task)

	loaded, _ := vassal.LoadTask(kingDir, task.ID)
	if len(loaded.Artifacts) != 2 {
		t.Errorf("expected 2 artifacts, got %d", len(loaded.Artifacts))
	}
}

func TestLoadTask_NotFound(t *testing.T) {
	kingDir := t.TempDir()
	_, err := vassal.LoadTask(kingDir, "t-nonexistent")
	if err == nil {
		t.Fatal("expected error for missing task, got nil")
	}
}

// ---------------------------------------------------------------------------
// RecoverOrphanedTasks
// ---------------------------------------------------------------------------

type testLogger struct {
	warns []string
}

func (l *testLogger) Warn(msg string, args ...any) {
	l.warns = append(l.warns, msg)
}

func TestRecoverOrphanedTasks_MarksRunningAsFailed(t *testing.T) {
	kingDir := t.TempDir()
	task := vassal.NewTask("worker", "some task", nil)
	task.Status = vassal.TaskStatusRunning
	_ = vassal.SaveTask(kingDir, task)

	logger := &testLogger{}
	vassal.RecoverOrphanedTasks(kingDir, "worker", logger)

	loaded, err := vassal.LoadTask(kingDir, task.ID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if loaded.Status != vassal.TaskStatusFailed {
		t.Errorf("expected status=failed, got %s", loaded.Status)
	}
	if loaded.Error == "" {
		t.Error("expected non-empty Error message")
	}
}

func TestRecoverOrphanedTasks_MarksAcceptedAsFailed(t *testing.T) {
	kingDir := t.TempDir()
	task := vassal.NewTask("worker", "some task", nil)
	// Status is accepted by default from NewTask
	_ = vassal.SaveTask(kingDir, task)

	vassal.RecoverOrphanedTasks(kingDir, "worker", discardLogger())

	loaded, _ := vassal.LoadTask(kingDir, task.ID)
	if loaded.Status != vassal.TaskStatusFailed {
		t.Errorf("expected status=failed, got %s", loaded.Status)
	}
}

func TestRecoverOrphanedTasks_IgnoresOtherVassals(t *testing.T) {
	kingDir := t.TempDir()
	task := vassal.NewTask("other-worker", "task", nil)
	task.Status = vassal.TaskStatusRunning
	_ = vassal.SaveTask(kingDir, task)

	vassal.RecoverOrphanedTasks(kingDir, "my-worker", discardLogger())

	loaded, _ := vassal.LoadTask(kingDir, task.ID)
	if loaded.Status != vassal.TaskStatusRunning {
		t.Errorf("expected status unchanged=running, got %s", loaded.Status)
	}
}

func TestRecoverOrphanedTasks_IgnoresDoneAndAborted(t *testing.T) {
	kingDir := t.TempDir()

	for _, status := range []vassal.TaskStatus{
		vassal.TaskStatusDone,
		vassal.TaskStatusAborted,
		vassal.TaskStatusTimeout,
		vassal.TaskStatusFailed,
	} {
		task := vassal.NewTask("worker", "t", nil)
		task.Status = status
		_ = vassal.SaveTask(kingDir, task)

		vassal.RecoverOrphanedTasks(kingDir, "worker", discardLogger())

		loaded, _ := vassal.LoadTask(kingDir, task.ID)
		if loaded.Status != status {
			t.Errorf("expected status=%s unchanged, got %s", status, loaded.Status)
		}
	}
}

func TestRecoverOrphanedTasks_EmptyDir(t *testing.T) {
	kingDir := t.TempDir()
	// tasks dir doesn't exist — should not panic or error
	vassal.RecoverOrphanedTasks(kingDir, "worker", discardLogger())
}

// ---------------------------------------------------------------------------
// TaskStatus constants
// ---------------------------------------------------------------------------

func TestTaskStatusConstants(t *testing.T) {
	cases := map[vassal.TaskStatus]string{
		vassal.TaskStatusAccepted: "accepted",
		vassal.TaskStatusRunning:  "running",
		vassal.TaskStatusDone:     "done",
		vassal.TaskStatusFailed:   "failed",
		vassal.TaskStatusTimeout:  "timeout",
		vassal.TaskStatusAborted:  "aborted",
	}
	for status, expected := range cases {
		if string(status) != expected {
			t.Errorf("TaskStatus %q != %q", status, expected)
		}
	}
}

// ---------------------------------------------------------------------------
// WriteVassalMD
// ---------------------------------------------------------------------------

func TestWriteVassalMD_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	task := vassal.NewTask("worker", "build the app", nil)

	if err := vassal.WriteVassalMD(dir, "worker", task, nil); err != nil {
		t.Fatalf("WriteVassalMD: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "VASSAL.md")); err != nil {
		t.Errorf("expected VASSAL.md to exist: %v", err)
	}
}

func TestWriteVassalMD_ContainsVassalNameAndTask(t *testing.T) {
	dir := t.TempDir()
	task := vassal.NewTask("my-vassal", "analyze data", nil)

	_ = vassal.WriteVassalMD(dir, "my-vassal", task, nil)

	data, err := os.ReadFile(filepath.Join(dir, "VASSAL.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !containsAll(content, "my-vassal", "analyze data") {
		t.Errorf("expected VASSAL.md to contain vassal name and task, got:\n%s", content)
	}
}

func TestWriteVassalMD_WithArtifacts(t *testing.T) {
	dir := t.TempDir()
	task := vassal.NewTask("worker", "use artifacts", nil)
	artifacts := []vassal.ArtifactRef{
		{Name: "report.pdf", FilePath: "/tmp/report.pdf"},
		{Name: "data.json", FilePath: "/tmp/data.json"},
	}

	_ = vassal.WriteVassalMD(dir, "worker", task, artifacts)

	data, _ := os.ReadFile(filepath.Join(dir, "VASSAL.md"))
	content := string(data)
	if !containsAll(content, "report.pdf", "data.json", "/tmp/report.pdf") {
		t.Errorf("expected VASSAL.md to contain artifact info, got:\n%s", content)
	}
}

func TestWriteVassalMD_WithNotes(t *testing.T) {
	dir := t.TempDir()
	task := vassal.NewTask("worker", "do work", map[string]any{"notes": "important context"})

	_ = vassal.WriteVassalMD(dir, "worker", task, nil)

	data, _ := os.ReadFile(filepath.Join(dir, "VASSAL.md"))
	if !contains(string(data), "important context") {
		t.Errorf("expected notes in VASSAL.md, got:\n%s", string(data))
	}
}

func TestWriteVassalMD_NilTask_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	err := vassal.WriteVassalMD(dir, "worker", nil, nil)
	if err == nil {
		t.Fatal("expected error for nil task, got nil")
	}
}

func TestWriteVassalMD_NoArtifacts_NoArtifactsSection(t *testing.T) {
	dir := t.TempDir()
	task := vassal.NewTask("worker", "simple task", nil)

	_ = vassal.WriteVassalMD(dir, "worker", task, nil)

	data, _ := os.ReadFile(filepath.Join(dir, "VASSAL.md"))
	if contains(string(data), "Available artifacts") {
		t.Error("expected no artifacts section when no artifacts provided")
	}
}

func TestWriteVassalMD_InvalidDir_ReturnsError(t *testing.T) {
	task := vassal.NewTask("worker", "t", nil)
	err := vassal.WriteVassalMD("/nonexistent/path/that/cannot/exist", "worker", task, nil)
	if err == nil {
		t.Fatal("expected error for invalid dir, got nil")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (len(s) >= len(substr)) &&
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}()
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}
