package vassal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// TaskStatus represents the lifecycle state of a dispatched task.
type TaskStatus string

const (
	TaskStatusAccepted TaskStatus = "accepted"
	TaskStatusRunning  TaskStatus = "running"
	TaskStatusDone     TaskStatus = "done"
	TaskStatusFailed   TaskStatus = "failed"
	TaskStatusTimeout  TaskStatus = "timeout"
	TaskStatusAborted  TaskStatus = "aborted"
)

// Task represents a unit of work dispatched to a vassal.
type Task struct {
	ID         string         `json:"id"`
	VassalName string         `json:"vassal_name"`
	Task       string         `json:"task"`
	Context    map[string]any `json:"context,omitempty"`
	Status     TaskStatus     `json:"status"`
	Output     string         `json:"output,omitempty"`
	Artifacts  []string       `json:"artifacts,omitempty"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// NewTask creates a new Task with a generated ID and accepted status.
func NewTask(vassalName, taskDesc string, context map[string]any) *Task {
	return &Task{
		ID:         "t-" + uuid.New().String(),
		VassalName: vassalName,
		Task:       taskDesc,
		Context:    context,
		Status:     TaskStatusAccepted,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// taskPath returns the path for a task JSON file.
func taskPath(kingDir, taskID string) string {
	return filepath.Join(kingDir, "tasks", taskID+".json")
}

// SaveTask writes a Task to .king/tasks/<id>.json.
// It updates t.UpdatedAt to the current time before writing.
func SaveTask(kingDir string, t *Task) error {
	dir := filepath.Join(kingDir, "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create tasks dir: %w", err)
	}
	t.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	tmp := taskPath(kingDir, t.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write task tmp: %w", err)
	}
	if err := os.Rename(tmp, taskPath(kingDir, t.ID)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename task file: %w", err)
	}
	return nil
}

// RecoverOrphanedTasks scans .king/tasks/ and marks any "running" or "accepted"
// tasks for this vassal as failed. Called on vassal startup so stale tasks from
// a previous crashed run don't stay stuck in running state forever.
func RecoverOrphanedTasks(kingDir, vassalName string, logger interface {
	Warn(msg string, args ...any)
}) {
	dir := filepath.Join(kingDir, "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5] // strip .json
		t, err := LoadTask(kingDir, id)
		if err != nil {
			continue
		}
		if t.VassalName != vassalName {
			continue
		}
		if t.Status != TaskStatusRunning && t.Status != TaskStatusAccepted {
			continue
		}
		t.Status = TaskStatusFailed
		t.Error = "vassal process restarted; task was orphaned"
		if saveErr := SaveTask(kingDir, t); saveErr != nil {
			logger.Warn("failed to recover orphaned task", "task_id", t.ID, "err", saveErr)
		} else {
			logger.Warn("recovered orphaned task", "task_id", t.ID, "vassal", vassalName)
		}
	}
}

// LoadTask reads a Task from .king/tasks/<id>.json.
func LoadTask(kingDir, taskID string) (*Task, error) {
	data, err := os.ReadFile(taskPath(kingDir, taskID))
	if err != nil {
		return nil, fmt.Errorf("read task file: %w", err)
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse task file: %w", err)
	}
	return &t, nil
}
