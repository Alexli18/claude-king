package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// VassalInfo holds the display data for a single vassal.
type VassalInfo struct {
	Name    string
	Status  string
	Command string
	PID     int
}

// VassalsModel holds state for the vassal list view.
type VassalsModel struct {
	Vassals  []VassalInfo
	Cursor   int
	Selected int
}

// StatusStyle returns a lipgloss style based on vassal status.
func StatusStyle(status string) lipgloss.Style {
	switch strings.ToLower(status) {
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	case "error", "crashed", "stopped":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red
	case "idle", "starting":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7")) // white
	}
}

// VassalsView renders the vassal table.
func VassalsView(m VassalsModel, width int) string {
	if len(m.Vassals) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Render("  No vassals running.")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))

	// Column widths.
	nameW := 20
	statusW := 12
	pidW := 10
	cmdW := width - nameW - statusW - pidW - 10
	if cmdW < 20 {
		cmdW = 20
	}

	var b strings.Builder

	// Header row.
	header := fmt.Sprintf("  %-*s %-*s %-*s %s",
		nameW, "NAME",
		statusW, "STATUS",
		pidW, "PID",
		"COMMAND",
	)
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Separator.
	sep := "  " + strings.Repeat("─", nameW+statusW+pidW+cmdW+3)
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(sep))
	b.WriteString("\n")

	// Rows.
	for i, v := range m.Vassals {
		prefix := "  "
		if i == m.Cursor {
			prefix = cursorStyle.Render("> ")
		}

		statusStr := StatusStyle(v.Status).Render(fmt.Sprintf("%-*s", statusW, v.Status))

		cmd := v.Command
		if len(cmd) > cmdW {
			cmd = cmd[:cmdW-3] + "..."
		}

		row := fmt.Sprintf("%s%-*s %s %-*d %s",
			prefix,
			nameW, truncate(v.Name, nameW),
			statusStr,
			pidW, v.PID,
			cmd,
		)
		b.WriteString(row)
		b.WriteString("\n")
	}

	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
