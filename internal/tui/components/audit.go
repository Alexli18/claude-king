package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// AuditInfo holds the display data for a single audit entry.
type AuditInfo struct {
	ID        string
	Layer     string
	Source    string
	Content   string
	TraceID   string
	Sampled   bool
	CreatedAt string
}

// ApprovalInfo holds display data for a pending approval request.
type ApprovalInfo struct {
	ID         string
	Command    string
	VassalName string
	TraceID    string
	CreatedAt  string
}

// AuditModel holds state for the Audit Hall view.
type AuditModel struct {
	Entries   []AuditInfo
	Approvals []ApprovalInfo
	Offset    int

	// Filter mode: press 'f' to activate, type a relative time (e.g. "1h"),
	// press Enter to apply. The applied filter is shown in the header.
	FilterMode    bool
	FilterInput   string // text being typed
	FilterSince   string // currently applied since filter
	ApprovalCursor int   // index into Approvals for y/n selection
}

// LayerStyle returns a lipgloss style based on audit layer.
func LayerStyle(layer string) lipgloss.Style {
	switch layer {
	case "ingestion":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray
	case "sieve":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	case "action":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // cyan
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7")) // white
	}
}

// AuditView renders the scrollable audit log (Audit Hall).
func AuditView(m AuditModel, width, height int) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))

	// --- Pending approvals section ---
	if len(m.Approvals) > 0 {
		b.WriteString(warnStyle.Render(fmt.Sprintf("  ⚠  %d PENDING APPROVAL(S) — use 'y' to approve, 'n' to reject", len(m.Approvals))))
		b.WriteString("\n")
		for i, a := range m.Approvals {
			cursor := "  "
			if i == m.ApprovalCursor {
				cursor = "► "
			}
			line := fmt.Sprintf("%s[%d] %s  vassal=%s  trace=%s  at=%s",
				cursor, i+1, a.Command, a.VassalName, a.TraceID, a.CreatedAt)
			if len(line) > width-2 {
				line = line[:width-5] + "..."
			}
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(line))
			b.WriteString("\n")
		}
		b.WriteString(dimStyle.Render("  " + strings.Repeat("─", width-4)))
		b.WriteString("\n")
	}

	// --- Filter bar ---
	if m.FilterMode {
		b.WriteString(headerStyle.Render(fmt.Sprintf("  Filter since: %s_", m.FilterInput)))
		b.WriteString(dimStyle.Render("  (Enter to apply, Esc to cancel)"))
		b.WriteString("\n")
	} else if m.FilterSince != "" {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  Filter: since=%s  (press 'f' to change, 'c' to clear)", m.FilterSince)))
		b.WriteString("\n")
	}

	if len(m.Entries) == 0 {
		b.WriteString(dimStyle.Render("  No audit entries recorded."))
		return b.String()
	}

	// --- Header row ---
	header := fmt.Sprintf("  %-15s %-10s %-10s %s", "TIME", "LAYER", "SOURCE", "CONTENT")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	sep := "  " + strings.Repeat("─", width-4)
	b.WriteString(dimStyle.Render(sep))
	b.WriteString("\n")

	// Account for approval section and filter bar in height.
	usedLines := 2 // header + sep
	if len(m.Approvals) > 0 {
		usedLines += len(m.Approvals) + 2 // approval rows + warning + divider
	}
	if m.FilterMode || m.FilterSince != "" {
		usedLines++
	}

	visibleRows := height - usedLines
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Clamp offset.
	maxOffset := len(m.Entries) - visibleRows
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
	if end > len(m.Entries) {
		end = len(m.Entries)
	}

	for _, e := range m.Entries[offset:end] {
		layerStr := LayerStyle(e.Layer).Render(fmt.Sprintf("%-10s", e.Layer))

		content := e.Content
		maxContent := width - 42
		if maxContent < 10 {
			maxContent = 10
		}
		if len(content) > maxContent {
			content = content[:maxContent-3] + "..."
		}

		// Show time portion (last 8 chars of created_at for HH:MM:SS).
		timeStr := e.CreatedAt
		if len(timeStr) > 8 {
			timeStr = timeStr[len(timeStr)-8:]
		}

		sampledMark := " "
		if e.Sampled {
			sampledMark = "~"
		}

		row := fmt.Sprintf("  %-15s %s %-10s %s%s", timeStr, layerStr, e.Source, sampledMark, content)
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Scroll indicator.
	if len(m.Entries) > visibleRows {
		indicator := fmt.Sprintf("  [%d-%d of %d] (up/down to scroll)", offset+1, end, len(m.Entries))
		b.WriteString(dimStyle.Render(indicator))
		b.WriteString("\n")
	}

	return b.String()
}
