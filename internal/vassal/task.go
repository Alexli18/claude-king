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
		ID:         "t-" + uuid.New().String()[:8],
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
	return os.WriteFile(taskPath(kingDir, t.ID), data, 0o644)
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
