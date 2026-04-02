package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// EventInfo holds the display data for a single event.
type EventInfo struct {
	ID        string
	Severity  string
	Summary   string
	Source    string
	CreatedAt string
}

// EventsModel holds state for the event log view.
type EventsModel struct {
	Events []EventInfo
	Offset int
}

// SeverityStyle returns a lipgloss style based on event severity.
func SeverityStyle(severity string) lipgloss.Style {
	switch strings.ToLower(severity) {
	case "error", "critical":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red
	case "warning", "warn":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	case "info":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7")) // white
	}
}

// EventsView renders the scrollable event log.
func EventsView(m EventsModel, width, height int) string {
	if len(m.Events) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Render("  No events recorded.")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

	var b strings.Builder

	// Header.
	header := fmt.Sprintf("  %-10s %-10s %-15s %s", "SEVERITY", "SOURCE", "TIME", "SUMMARY")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	sep := "  " + strings.Repeat("─", width-4)
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(sep))
	b.WriteString("\n")

	// Visible rows (accounting for header + separator).
	visibleRows := height - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Clamp offset.
	maxOffset := len(m.Events) - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.Offset
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + visibleRows
	if end > len(m.Events) {
		end = len(m.Events)
	}

	for _, e := range m.Events[offset:end] {
		sevStr := SeverityStyle(e.Severity).Render(fmt.Sprintf("%-10s", e.Severity))

		summary := e.Summary
		maxSummary := width - 40
		if maxSummary < 10 {
			maxSummary = 10
		}
		if len(summary) > maxSummary {
			summary = summary[:maxSummary-3] + "..."
		}

		row := fmt.Sprintf("  %s %-10s %-15s %s", sevStr, e.Source, e.CreatedAt, summary)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator.
	if len(m.Events) > visibleRows {
		indicator := fmt.Sprintf("  [%d-%d of %d] (up/down to scroll)", offset+1, end, len(m.Events))
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(indicator))
		b.WriteString("\n")
	}

	return b.String()
}
