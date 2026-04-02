package mcp_test

import (
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/mcp"
	"github.com/alexli18/claude-king/internal/store"
)

func TestParseSinceDuration_Valid(t *testing.T) {
	cases := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"30s", 30 * time.Second},
	}
	for _, tc := range cases {
		got, err := mcp.ParseSinceDuration(tc.input)
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", tc.input, err)
		}
		if got != tc.expected {
			t.Errorf("input %q: expected %v, got %v", tc.input, tc.expected, got)
		}
	}
}

func TestParseSinceDuration_Invalid(t *testing.T) {
	_, err := mcp.ParseSinceDuration("garbage")
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestFilterEventsBySeverity_All(t *testing.T) {
	events := []store.Event{
		{Severity: "error"},
		{Severity: "warning"},
		{Severity: "info"},
	}
	got := mcp.FilterEventsBySeverity(events, "")
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
}

func TestFilterEventsBySeverity_Specific(t *testing.T) {
	events := []store.Event{
		{Severity: "error"},
		{Severity: "warning"},
		{Severity: "error"},
	}
	got := mcp.FilterEventsBySeverity(events, "error")
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	for _, e := range got {
		if e.Severity != "error" {
			t.Errorf("expected only error events, got %q", e.Severity)
		}
	}
}
