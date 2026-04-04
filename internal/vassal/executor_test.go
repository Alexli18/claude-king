package vassal_test

import (
	"testing"

	"github.com/alexli18/claude-king/internal/vassal"
)

func TestNewExecutor_Claude(t *testing.T) {
	e, err := vassal.NewExecutor("claude", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestNewExecutor_Codex(t *testing.T) {
	e, err := vassal.NewExecutor("codex", "o4-mini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestNewExecutor_Gemini(t *testing.T) {
	e, err := vassal.NewExecutor("gemini", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestNewExecutor_EmptyType_DefaultsClaude(t *testing.T) {
	e, err := vassal.NewExecutor("", "")
	if err != nil {
		t.Fatalf("unexpected error for empty type: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil executor")
	}
}

func TestNewExecutor_UnknownType_ReturnsError(t *testing.T) {
	_, err := vassal.NewExecutor("gpt5", "")
	if err == nil {
		t.Fatal("expected error for unknown executor type")
	}
}
