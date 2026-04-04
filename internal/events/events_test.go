package events_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/events"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newMemStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedKingdomVassal inserts a kingdom and vassal, returning their IDs.
func seedKingdomVassal(t *testing.T, s *store.Store) (kingdomID, vassalID string) {
	t.Helper()
	kingdomID = uuid.New().String()
	vassalID = uuid.New().String()
	_ = s.CreateKingdom(store.Kingdom{
		ID:        kingdomID,
		Name:      "k",
		RootPath:  "/tmp/" + kingdomID,
		Status:    "running",
		CreatedAt: "2026-01-01 00:00:00",
		UpdatedAt: "2026-01-01 00:00:00",
	})
	_ = s.CreateVassal(store.Vassal{
		ID:        vassalID,
		KingdomID: kingdomID,
		Name:      "worker",
		Command:   "claude",
		Status:    "running",
		CreatedAt: "2026-01-01 00:00:00",
	})
	return
}

// ---------------------------------------------------------------------------
// CompiledPattern tests
// ---------------------------------------------------------------------------

func TestCompilePatterns_Valid(t *testing.T) {
	patterns := []config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
		{Name: "warn",  Regex: `WARN.*`, Severity: "warn"},
	}
	compiled, err := events.CompilePatterns(patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compiled) != 2 {
		t.Errorf("expected 2 compiled patterns, got %d", len(compiled))
	}
}

func TestCompilePatterns_EmptyRegex(t *testing.T) {
	patterns := []config.PatternConfig{{Name: "bad", Regex: "", Severity: "error"}}
	_, err := events.CompilePatterns(patterns)
	if err == nil {
		t.Fatal("expected error for empty regex, got nil")
	}
}

func TestCompilePatterns_InvalidRegex(t *testing.T) {
	patterns := []config.PatternConfig{{Name: "bad", Regex: `[invalid`, Severity: "error"}}
	_, err := events.CompilePatterns(patterns)
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
}

func TestCompiledPattern_Match_BasicMatch(t *testing.T) {
	cp, _ := events.CompilePatterns([]config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
	})
	summary, matched := cp[0].Match("panic: something went wrong", "worker")
	if !matched {
		t.Fatal("expected match, got none")
	}
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestCompiledPattern_Match_NoMatch(t *testing.T) {
	cp, _ := events.CompilePatterns([]config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
	})
	_, matched := cp[0].Match("everything is fine", "worker")
	if matched {
		t.Fatal("expected no match, got match")
	}
}

func TestCompiledPattern_Match_SourceFilter(t *testing.T) {
	cp, _ := events.CompilePatterns([]config.PatternConfig{
		{Name: "p", Regex: `error`, Severity: "error", Source: "alpha"},
	})

	// Matches for correct vassal.
	_, matchedAlpha := cp[0].Match("error occurred", "alpha")
	if !matchedAlpha {
		t.Error("expected match for vassal=alpha")
	}

	// Does not match for different vassal.
	_, matchedBeta := cp[0].Match("error occurred", "beta")
	if matchedBeta {
		t.Error("expected no match for vassal=beta (source filter)")
	}
}

func TestCompiledPattern_Match_SummaryTemplate(t *testing.T) {
	cp, _ := events.CompilePatterns([]config.PatternConfig{
		{
			Name:            "p",
			Regex:           `error: (.+)`,
			Severity:        "error",
			SummaryTemplate: "vassal={vassal} msg={group.1}",
		},
	})
	summary, matched := cp[0].Match("error: disk full", "worker")
	if !matched {
		t.Fatal("expected match")
	}
	if summary != "vassal=worker msg=disk full" {
		t.Errorf("unexpected summary: %q", summary)
	}
}

func TestCompiledPattern_Match_SummaryTemplateMatch(t *testing.T) {
	cp, _ := events.CompilePatterns([]config.PatternConfig{
		{Name: "p", Regex: `PANIC`, Severity: "error", SummaryTemplate: "got: {match}"},
	})
	summary, matched := cp[0].Match("PANIC detected", "worker")
	if !matched {
		t.Fatal("expected match")
	}
	if summary != "got: PANIC" {
		t.Errorf("unexpected summary: %q", summary)
	}
}

// ---------------------------------------------------------------------------
// Sieve tests
// ---------------------------------------------------------------------------

func newSieve(t *testing.T, s *store.Store, kingdomID string, patterns []config.PatternConfig) *events.Sieve {
	t.Helper()
	cp, err := events.CompilePatterns(patterns)
	if err != nil {
		t.Fatalf("CompilePatterns: %v", err)
	}
	return events.NewSieve(cp, s, kingdomID, 1, discardLogger())
}

func TestSieve_ProcessLine_MatchPublishesEvent(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	sieve := newSieve(t, s, kingdomID, []config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
	})

	var received []store.Event
	sieve.Subscribe(func(e store.Event) {
		received = append(received, e)
	})

	sieve.ProcessLine("worker", vassalID, "panic: index out of range")

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Severity != "error" {
		t.Errorf("expected severity=error, got %s", received[0].Severity)
	}
	if received[0].Pattern != "panic" {
		t.Errorf("expected pattern=panic, got %s", received[0].Pattern)
	}
}

func TestSieve_ProcessLine_NoMatchNoEvent(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	sieve := newSieve(t, s, kingdomID, []config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
	})

	var received []store.Event
	sieve.Subscribe(func(e store.Event) { received = append(received, e) })

	sieve.ProcessLine("worker", vassalID, "all systems nominal")

	if len(received) != 0 {
		t.Errorf("expected 0 events, got %d", len(received))
	}
}

func TestSieve_ProcessLine_CooldownSuppresses(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	// 30-second cooldown (default when cooldownSeconds > 0 and > time.Since)
	cp, _ := events.CompilePatterns([]config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
	})
	sieve := events.NewSieve(cp, s, kingdomID, 30, discardLogger())

	var count int
	sieve.Subscribe(func(e store.Event) { count++ })

	// First call fires.
	sieve.ProcessLine("worker", vassalID, "panic: first")
	// Second call within cooldown should be suppressed.
	sieve.ProcessLine("worker", vassalID, "panic: second")

	if count != 1 {
		t.Errorf("expected 1 event (cooldown suppressed second), got %d", count)
	}
}

func TestSieve_ProcessLine_ANSIStripped(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	sieve := newSieve(t, s, kingdomID, []config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
	})

	var received []store.Event
	sieve.Subscribe(func(e store.Event) { received = append(received, e) })

	// Line with ANSI escape codes wrapping "panic:".
	sieve.ProcessLine("worker", vassalID, "\x1b[31mpanic:\x1b[0m oops")

	if len(received) != 1 {
		t.Errorf("expected 1 event (ANSI stripped), got %d", len(received))
	}
}

func TestSieve_ProcessLine_AuditCallback(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	sieve := newSieve(t, s, kingdomID, []config.PatternConfig{
		{Name: "panic", Regex: `panic:`, Severity: "error"},
	})

	var decisions []string
	sieve.SetAuditCallback(func(vassalName, vassalIDcb, content, decision, pattern, severity, summary string) {
		decisions = append(decisions, decision)
	})

	sieve.ProcessLine("worker", vassalID, "panic: oops")

	if len(decisions) != 1 || decisions[0] != "matched" {
		t.Errorf("expected [matched], got %v", decisions)
	}
}

func TestSieve_Subscribe_MultipleSubscribers(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	sieve := newSieve(t, s, kingdomID, []config.PatternConfig{
		{Name: "p", Regex: `error`, Severity: "error"},
	})

	var a, b int
	sieve.Subscribe(func(e store.Event) { a++ })
	sieve.Subscribe(func(e store.Event) { b++ })

	sieve.ProcessLine("worker", vassalID, "error detected")

	if a != 1 || b != 1 {
		t.Errorf("expected both subscribers to fire once, got a=%d b=%d", a, b)
	}
}

func TestSieve_OutputCallback(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	sieve := newSieve(t, s, kingdomID, []config.PatternConfig{
		{Name: "p", Regex: `error`, Severity: "error"},
	})

	var count int
	sieve.Subscribe(func(e store.Event) { count++ })

	cb := sieve.OutputCallback("worker", vassalID)
	cb("error: boom")

	if count != 1 {
		t.Errorf("expected 1 event via OutputCallback, got %d", count)
	}
}

func TestSieve_HealthSummary_NoEvents(t *testing.T) {
	s := newMemStore(t)
	kingdomID, _ := seedKingdomVassal(t, s)
	sieve := newSieve(t, s, kingdomID, nil)

	summary := sieve.HealthSummary()
	if summary != "No events recorded." {
		t.Errorf("unexpected summary: %q", summary)
	}
}

func TestSieve_HealthSummary_WithEvents(t *testing.T) {
	s := newMemStore(t)
	kingdomID, vassalID := seedKingdomVassal(t, s)

	sieve := newSieve(t, s, kingdomID, []config.PatternConfig{
		{Name: "p", Regex: `error`, Severity: "error"},
	})
	sieve.ProcessLine("worker", vassalID, "error: something")
	time.Sleep(10 * time.Millisecond) // let store write complete

	summary := sieve.HealthSummary()
	if summary == "No events recorded." {
		t.Errorf("expected event summary, got: %q", summary)
	}
}
