package components_test

import (
	"strings"
	"testing"

	"github.com/alexli18/claude-king/internal/tui/components"
)

// ---------------------------------------------------------------------------
// VassalsView
// ---------------------------------------------------------------------------

func TestVassalsView_Empty(t *testing.T) {
	m := components.VassalsModel{}
	out := components.VassalsView(m, 80)
	if !strings.Contains(out, "No vassals running") {
		t.Errorf("expected empty message, got: %q", out)
	}
}

func TestVassalsView_Single(t *testing.T) {
	m := components.VassalsModel{
		Vassals: []components.VassalInfo{
			{Name: "worker", Status: "running", Command: "claude", PID: 1234},
		},
	}
	out := components.VassalsView(m, 80)
	if !strings.Contains(out, "worker") {
		t.Errorf("expected vassal name 'worker' in output, got: %q", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected status 'running' in output, got: %q", out)
	}
}

func TestVassalsView_Cursor(t *testing.T) {
	m := components.VassalsModel{
		Vassals: []components.VassalInfo{
			{Name: "a", Status: "running"},
			{Name: "b", Status: "idle"},
		},
		Cursor: 1,
	}
	out := components.VassalsView(m, 80)
	if !strings.Contains(out, ">") {
		t.Errorf("expected cursor '>' for second row, got: %q", out)
	}
}

func TestVassalsView_LongCommandTruncated(t *testing.T) {
	longCmd := strings.Repeat("x", 200)
	m := components.VassalsModel{
		Vassals: []components.VassalInfo{
			{Name: "v", Status: "running", Command: longCmd},
		},
	}
	out := components.VassalsView(m, 80)
	if strings.Contains(out, longCmd) {
		t.Error("expected long command to be truncated")
	}
	if !strings.Contains(out, "...") {
		t.Error("expected truncation ellipsis '...'")
	}
}

func TestVassalsView_Header(t *testing.T) {
	m := components.VassalsModel{
		Vassals: []components.VassalInfo{{Name: "x", Status: "running"}},
	}
	out := components.VassalsView(m, 80)
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "STATUS") {
		t.Errorf("expected header with NAME and STATUS, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// StatusStyle
// ---------------------------------------------------------------------------

func TestStatusStyle_Running(t *testing.T) {
	style := components.StatusStyle("running")
	out := style.Render("test")
	if out == "" {
		t.Error("expected non-empty output for running status style")
	}
}

func TestStatusStyle_Error(t *testing.T) {
	style := components.StatusStyle("error")
	out := style.Render("test")
	if out == "" {
		t.Error("expected non-empty output for error status style")
	}
}

func TestStatusStyle_Unknown(t *testing.T) {
	style := components.StatusStyle("unknown-state")
	out := style.Render("test")
	if out == "" {
		t.Error("expected non-empty output for unknown status style")
	}
}

// ---------------------------------------------------------------------------
// EventsView
// ---------------------------------------------------------------------------

func TestEventsView_Empty(t *testing.T) {
	m := components.EventsModel{}
	out := components.EventsView(m, 80, 20)
	if !strings.Contains(out, "No events recorded") {
		t.Errorf("expected empty message, got: %q", out)
	}
}

func TestEventsView_Single(t *testing.T) {
	m := components.EventsModel{
		Events: []components.EventInfo{
			{ID: "e1", Severity: "error", Summary: "panic detected", Source: "worker", CreatedAt: "12:34:56"},
		},
	}
	out := components.EventsView(m, 80, 20)
	if !strings.Contains(out, "panic detected") {
		t.Errorf("expected summary in output, got: %q", out)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected severity in output, got: %q", out)
	}
}

func TestEventsView_ScrollIndicator(t *testing.T) {
	events := make([]components.EventInfo, 20)
	for i := range events {
		events[i] = components.EventInfo{Severity: "info", Summary: "msg", Source: "v"}
	}
	m := components.EventsModel{Events: events}
	out := components.EventsView(m, 80, 5) // small height to force scrolling
	if !strings.Contains(out, "of 20") {
		t.Errorf("expected scroll indicator, got: %q", out)
	}
}

func TestEventsView_OffsetClamp(t *testing.T) {
	events := make([]components.EventInfo, 5)
	for i := range events {
		events[i] = components.EventInfo{Severity: "info", Summary: "msg"}
	}
	// Offset larger than len — should clamp, not panic.
	m := components.EventsModel{Events: events, Offset: 999}
	out := components.EventsView(m, 80, 20)
	if out == "" {
		t.Error("expected non-empty output even with large offset")
	}
}

// ---------------------------------------------------------------------------
// SeverityStyle
// ---------------------------------------------------------------------------

func TestSeverityStyle_Error(t *testing.T) {
	out := components.SeverityStyle("error").Render("x")
	if out == "" {
		t.Error("expected output for error severity")
	}
}

func TestSeverityStyle_Critical(t *testing.T) {
	out := components.SeverityStyle("critical").Render("x")
	if out == "" {
		t.Error("expected output for critical severity")
	}
}

func TestSeverityStyle_Warning(t *testing.T) {
	out := components.SeverityStyle("warning").Render("x")
	if out == "" {
		t.Error("expected output for warning severity")
	}
}

// ---------------------------------------------------------------------------
// HealthView
// ---------------------------------------------------------------------------

func TestHealthView_Basic(t *testing.T) {
	m := components.HealthModel{
		Status: components.StatusInfo{
			KingdomID: "k-123",
			Status:    "running",
			Root:      "/tmp/kingdom",
			Vassals:   3,
		},
	}
	out := components.HealthView(m, 80)
	if !strings.Contains(out, "k-123") {
		t.Errorf("expected kingdom ID in output, got: %q", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected status in output, got: %q", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("expected vassal count in output, got: %q", out)
	}
}

func TestHealthView_WithGuards(t *testing.T) {
	m := components.HealthModel{
		Status: components.StatusInfo{Status: "running"},
		Guards: []components.GuardInfo{
			{VassalName: "worker", GuardIndex: 0, GuardType: "port_check", CircuitOpen: false},
			{VassalName: "worker", GuardIndex: 1, GuardType: "log_watch", CircuitOpen: true, ConsecutiveFails: 3},
		},
	}
	out := components.HealthView(m, 80)
	if !strings.Contains(out, "port_check") {
		t.Errorf("expected guard type in output, got: %q", out)
	}
	if !strings.Contains(out, "OPEN") {
		t.Errorf("expected OPEN circuit indicator, got: %q", out)
	}
	if !strings.Contains(out, "fails: 3") {
		t.Errorf("expected fail count, got: %q", out)
	}
}

func TestHealthView_StoppedStatus(t *testing.T) {
	m := components.HealthModel{Status: components.StatusInfo{Status: "stopped"}}
	out := components.HealthView(m, 80)
	if !strings.Contains(out, "stopped") {
		t.Errorf("expected 'stopped' status in output, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// AuditView
// ---------------------------------------------------------------------------

func TestAuditView_Empty(t *testing.T) {
	m := components.AuditModel{}
	out := components.AuditView(m, 80, 20)
	if !strings.Contains(out, "No audit entries recorded") {
		t.Errorf("expected empty message, got: %q", out)
	}
}

func TestAuditView_WithEntries(t *testing.T) {
	m := components.AuditModel{
		Entries: []components.AuditInfo{
			{ID: "a1", Layer: "sieve", Source: "worker", Content: "panic matched", CreatedAt: "2026-01-01 12:00:00"},
		},
	}
	out := components.AuditView(m, 80, 20)
	if !strings.Contains(out, "panic matched") {
		t.Errorf("expected content in output, got: %q", out)
	}
	if !strings.Contains(out, "sieve") {
		t.Errorf("expected layer in output, got: %q", out)
	}
}

func TestAuditView_PendingApprovals(t *testing.T) {
	m := components.AuditModel{
		Approvals: []components.ApprovalInfo{
			{ID: "r1", Command: "rm -rf /", VassalName: "worker", TraceID: "t1"},
		},
	}
	out := components.AuditView(m, 80, 20)
	if !strings.Contains(out, "PENDING APPROVAL") {
		t.Errorf("expected pending approval header, got: %q", out)
	}
	if !strings.Contains(out, "rm -rf /") {
		t.Errorf("expected command in approvals, got: %q", out)
	}
}

func TestAuditView_FilterMode(t *testing.T) {
	m := components.AuditModel{
		FilterMode:  true,
		FilterInput: "1h",
	}
	out := components.AuditView(m, 80, 20)
	if !strings.Contains(out, "Filter since:") {
		t.Errorf("expected filter mode UI, got: %q", out)
	}
}

func TestAuditView_FilterApplied(t *testing.T) {
	m := components.AuditModel{
		FilterSince: "2h",
		Entries: []components.AuditInfo{
			{Layer: "action", Content: "test", Source: "v"},
		},
	}
	out := components.AuditView(m, 80, 20)
	if !strings.Contains(out, "since=2h") {
		t.Errorf("expected filter indicator, got: %q", out)
	}
}

func TestAuditView_SampledMark(t *testing.T) {
	m := components.AuditModel{
		Entries: []components.AuditInfo{
			{Layer: "ingestion", Content: "sampled line", Source: "v", Sampled: true},
		},
	}
	out := components.AuditView(m, 80, 20)
	if !strings.Contains(out, "~") {
		t.Errorf("expected sampled marker '~', got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// LayerStyle
// ---------------------------------------------------------------------------

func TestLayerStyle_AllLayers(t *testing.T) {
	for _, layer := range []string{"ingestion", "sieve", "action", "unknown"} {
		out := components.LayerStyle(layer).Render("test")
		if out == "" {
			t.Errorf("expected non-empty output for layer %q", layer)
		}
	}
}
