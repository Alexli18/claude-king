package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StatusInfo holds the display data for kingdom health.
type StatusInfo struct {
	KingdomID string
	Status    string
	Root      string
	Vassals   int
}

// GuardInfo holds the display data for a single guard in the health panel.
type GuardInfo struct {
	VassalName       string
	GuardIndex       int
	GuardType        string
	CircuitOpen      bool
	ConsecutiveFails int
	LastMessage      string
}

// HealthModel holds state for the health view.
type HealthModel struct {
	Status StatusInfo
	Guards []GuardInfo
}

// HealthView renders the system health display.
func HealthView(m HealthModel, width int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	var b strings.Builder

	b.WriteString(titleStyle.Render("  Kingdom Health"))
	b.WriteString("\n\n")

	// Status indicator.
	statusColor := "10" // green
	switch strings.ToLower(m.Status.Status) {
	case "running":
		statusColor = "10"
	case "starting", "stopping":
		statusColor = "11"
	case "stopped", "error":
		statusColor = "9"
	}
	statusStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(statusColor))

	rows := []struct {
		label string
		value string
	}{
		{"Kingdom ID", m.Status.KingdomID},
		{"Status", m.Status.Status},
		{"Root Path", m.Status.Root},
		{"Vassals", fmt.Sprintf("%d", m.Status.Vassals)},
	}

	for _, r := range rows {
		label := labelStyle.Render(fmt.Sprintf("  %-14s", r.label))
		var val string
		if r.label == "Status" {
			val = statusStyle.Render(r.value)
		} else {
			val = valueStyle.Render(r.value)
		}
		b.WriteString(fmt.Sprintf("%s %s\n", label, val))
	}

	// Guard health section (if any guards are configured).
	if len(m.Guards) > 0 {
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("  Guards"))
		b.WriteString("\n\n")

		openStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

		for _, g := range m.Guards {
			indicator := okStyle.Render("OK")
			if g.CircuitOpen {
				indicator = openStyle.Render("OPEN")
			}
			line := fmt.Sprintf("  %-20s [%d] %-12s %s",
				g.VassalName, g.GuardIndex, g.GuardType, indicator)
			if g.CircuitOpen {
				line += fmt.Sprintf(" (fails: %d)", g.ConsecutiveFails)
			}
			b.WriteString(valueStyle.Render(line))
			b.WriteString("\n")
		}
	}

	// Separator.
	b.WriteString("\n")
	sep := "  " + strings.Repeat("─", width-4)
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(sep))
	b.WriteString("\n")

	return b.String()
}
