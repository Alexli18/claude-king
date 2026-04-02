package tui

import (
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/alexli18/claude-king/internal/daemon"
	"github.com/alexli18/claude-king/internal/tui/components"
)

// tab represents which tab is currently active.
type tab int

const (
	tabVassals tab = iota
	tabEvents
	tabHealth
	tabAudit
	tabCount // sentinel for modular tab switching
)

// model is the top-level Bubbletea model for the dashboard.
type model struct {
	client    *daemon.Client
	activeTab tab
	vassals   components.VassalsModel
	events    components.EventsModel
	health    components.HealthModel
	audit     components.AuditModel
	width     int
	height    int
	err       error
}

// tickMsg triggers periodic data refresh.
type tickMsg time.Time

// dataMsg carries refreshed data from the daemon.
type dataMsg struct {
	vassals   []components.VassalInfo
	events    []components.EventInfo
	status    components.StatusInfo
	audit     []components.AuditInfo
	approvals []components.ApprovalInfo
}

// errMsg carries an error from async operations.
type errMsg struct{ err error }

// approvalResultMsg signals success/failure of an approval RPC call.
type approvalResultMsg struct{ err error }

// NewApp creates and returns a new Bubbletea program with alt-screen mode.
func NewApp(client *daemon.Client) *tea.Program {
	return tea.NewProgram(
		initialModel(client),
		tea.WithAltScreen(),
	)
}

func initialModel(client *daemon.Client) model {
	return model{
		client:    client,
		activeTab: tabVassals,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		fetchData(m.client, ""),
		tickCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// When in filter input mode, intercept keys for text input.
		if m.activeTab == tabAudit && m.audit.FilterMode {
			switch msg.String() {
			case "enter":
				m.audit.FilterSince = m.audit.FilterInput
				m.audit.FilterInput = ""
				m.audit.FilterMode = false
				m.audit.Offset = 0
				return m, fetchData(m.client, m.audit.FilterSince)
			case "esc":
				m.audit.FilterInput = ""
				m.audit.FilterMode = false
				return m, nil
			case "backspace":
				if len(m.audit.FilterInput) > 0 {
					m.audit.FilterInput = m.audit.FilterInput[:len(m.audit.FilterInput)-1]
				}
				return m, nil
			default:
				// Accept printable characters.
				if len(msg.String()) == 1 {
					m.audit.FilterInput += msg.String()
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % tabCount
			return m, nil
		case "shift+tab":
			m.activeTab = (m.activeTab + tabCount - 1) % tabCount
			return m, nil
		case "1":
			m.activeTab = tabVassals
			return m, nil
		case "2":
			m.activeTab = tabEvents
			return m, nil
		case "3":
			m.activeTab = tabHealth
			return m, nil
		case "4":
			m.activeTab = tabAudit
			return m, nil

		// Navigation within active tab.
		case "up", "k":
			switch m.activeTab {
			case tabVassals:
				if m.vassals.Cursor > 0 {
					m.vassals.Cursor--
				}
			case tabEvents:
				if m.events.Offset > 0 {
					m.events.Offset--
				}
			case tabAudit:
				if len(m.audit.Approvals) > 0 {
					if m.audit.ApprovalCursor > 0 {
						m.audit.ApprovalCursor--
					}
				} else if m.audit.Offset > 0 {
					m.audit.Offset--
				}
			}
			return m, nil
		case "down", "j":
			switch m.activeTab {
			case tabVassals:
				if m.vassals.Cursor < len(m.vassals.Vassals)-1 {
					m.vassals.Cursor++
				}
			case tabEvents:
				m.events.Offset++
			case tabAudit:
				if len(m.audit.Approvals) > 0 {
					if m.audit.ApprovalCursor < len(m.audit.Approvals)-1 {
						m.audit.ApprovalCursor++
					}
				} else {
					m.audit.Offset++
				}
			}
			return m, nil

		// Audit-specific keys.
		case "f":
			if m.activeTab == tabAudit {
				m.audit.FilterMode = true
				m.audit.FilterInput = ""
				return m, nil
			}
		case "c":
			if m.activeTab == tabAudit {
				m.audit.FilterSince = ""
				m.audit.Offset = 0
				return m, fetchData(m.client, "")
			}

		// Sovereign approval: approve / reject selected request.
		case "y":
			if m.activeTab == tabAudit && len(m.audit.Approvals) > 0 {
				idx := m.audit.ApprovalCursor
				if idx < len(m.audit.Approvals) {
					reqID := m.audit.Approvals[idx].ID
					return m, respondApproval(m.client, reqID, true)
				}
			}
		case "n":
			if m.activeTab == tabAudit && len(m.audit.Approvals) > 0 {
				idx := m.audit.ApprovalCursor
				if idx < len(m.audit.Approvals) {
					reqID := m.audit.Approvals[idx].ID
					return m, respondApproval(m.client, reqID, false)
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, fetchData(m.client, m.audit.FilterSince)

	case dataMsg:
		m.vassals.Vassals = msg.vassals
		m.events.Events = msg.events
		m.health.Status = msg.status
		m.audit.Entries = msg.audit
		m.audit.Approvals = msg.approvals
		// Reset approval cursor if out of bounds.
		if m.audit.ApprovalCursor >= len(m.audit.Approvals) {
			m.audit.ApprovalCursor = 0
		}
		m.err = nil
		return m, tickCmd()

	case approvalResultMsg:
		// After approve/reject, refresh immediately.
		return m, fetchData(m.client, m.audit.FilterSince)

	case errMsg:
		m.err = msg.err
		return m, tickCmd()
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var s string

	// Tab bar.
	s += renderTabBar(m.activeTab, m.width)
	s += "\n"

	// Error banner if present.
	if m.err != nil {
		errStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)
		s += errStyle.Render("  Error: "+m.err.Error()) + "\n"
	}

	// Content area - subtract tab bar (2 lines), help bar (2 lines), possible error (1 line).
	contentHeight := m.height - 4
	if m.err != nil {
		contentHeight--
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	switch m.activeTab {
	case tabVassals:
		s += components.VassalsView(m.vassals, m.width)
	case tabEvents:
		s += components.EventsView(m.events, m.width, contentHeight)
	case tabHealth:
		s += components.HealthView(m.health, m.width)
	case tabAudit:
		s += components.AuditView(m.audit, m.width, contentHeight)
	}

	// Help bar at bottom.
	s += "\n"
	s += renderHelpBar(m.activeTab, m.audit.FilterMode, m.width)

	return s
}

// renderTabBar draws the tab selector at the top.
func renderTabBar(active tab, width int) string {
	tabs := []struct {
		label string
		key   string
	}{
		{"Vassals", "1"},
		{"Events", "2"},
		{"Health", "3"},
		{"Audit Hall", "4"},
	}

	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("14")).
		Padding(0, 2)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Background(lipgloss.Color("8")).
		Padding(0, 2)

	var rendered []string
	for i, t := range tabs {
		label := "[" + t.key + "] " + t.label
		if tab(i) == active {
			rendered = append(rendered, activeStyle.Render(label))
		} else {
			rendered = append(rendered, inactiveStyle.Render(label))
		}
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("13"))

	title := titleStyle.Render(" Claude King ")

	return title + " " + lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// renderHelpBar draws the help text at the bottom.
func renderHelpBar(active tab, filterMode bool, width int) string {
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8"))

	var help string
	if active == tabAudit {
		if filterMode {
			help = "  type since filter (e.g. 1h, 30m) | Enter: apply | Esc: cancel"
		} else {
			help = "  tab/1-4: tabs | up/down: scroll | f: filter | c: clear | y/n: approve/reject | q: quit"
		}
	} else {
		help = "  tab/1-4: switch tabs | up/down: navigate | q: quit"
	}

	if len(help) < width {
		help += lipgloss.NewStyle().Render("")
	}

	return helpStyle.Render(help)
}

// tickCmd returns a command that fires a tickMsg after 2 seconds.
func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// fetchData calls the daemon RPC to get current state.
// filterSince is a relative or absolute time string passed to get_audit_log.
func fetchData(client *daemon.Client, filterSince string) tea.Cmd {
	return func() tea.Msg {
		// Fetch vassals/kingdom info.
		vassalData, err := client.Call("list_vassals", nil)
		if err != nil {
			return errMsg{err: err}
		}

		var vassalResp struct {
			Vassals []struct {
				Name    string `json:"name"`
				Status  string `json:"status"`
				Command string `json:"command"`
				PID     int    `json:"pid"`
			} `json:"vassals"`
		}
		if err := json.Unmarshal(vassalData, &vassalResp); err != nil {
			return errMsg{err: err}
		}

		vassals := make([]components.VassalInfo, len(vassalResp.Vassals))
		for i, v := range vassalResp.Vassals {
			vassals[i] = components.VassalInfo{
				Name:    v.Name,
				Status:  v.Status,
				Command: v.Command,
				PID:     v.PID,
			}
		}

		// Fetch status.
		statusData, err := client.Call("status", nil)
		if err != nil {
			return errMsg{err: err}
		}

		var statusResp struct {
			KingdomID string `json:"kingdom_id"`
			Status    string `json:"status"`
			Root      string `json:"root"`
			Vassals   int    `json:"vassals"`
		}
		if err := json.Unmarshal(statusData, &statusResp); err != nil {
			return errMsg{err: err}
		}

		status := components.StatusInfo{
			KingdomID: statusResp.KingdomID,
			Status:    statusResp.Status,
			Root:      statusResp.Root,
			Vassals:   statusResp.Vassals,
		}

		// Fetch events.
		eventsData, err := client.Call("get_events", nil)
		if err != nil {
			// Events may not be critical; return what we have.
			return dataMsg{
				vassals: vassals,
				status:  status,
			}
		}

		var eventsResp struct {
			Events []struct {
				ID        string `json:"id"`
				Severity  string `json:"severity"`
				Summary   string `json:"summary"`
				Source    string `json:"source"`
				CreatedAt string `json:"created_at"`
			} `json:"events"`
		}
		if err := json.Unmarshal(eventsData, &eventsResp); err != nil {
			return dataMsg{
				vassals: vassals,
				status:  status,
			}
		}

		events := make([]components.EventInfo, len(eventsResp.Events))
		for i, e := range eventsResp.Events {
			events[i] = components.EventInfo{
				ID:        e.ID,
				Severity:  e.Severity,
				Summary:   e.Summary,
				Source:    e.Source,
				CreatedAt: e.CreatedAt,
			}
		}

		// Fetch audit entries (with optional since filter).
		auditParams := map[string]interface{}{"limit": 100}
		if filterSince != "" {
			auditParams["since"] = filterSince
		}
		auditData, auditErr := client.Call("get_audit_log", auditParams)
		var auditEntries []components.AuditInfo
		if auditErr == nil {
			var auditResp struct {
				Entries []struct {
					ID        string `json:"id"`
					Layer     string `json:"layer"`
					Source    string `json:"source"`
					Content   string `json:"content"`
					TraceID   string `json:"trace_id"`
					Sampled   bool   `json:"sampled"`
					CreatedAt string `json:"created_at"`
				} `json:"entries"`
			}
			if json.Unmarshal(auditData, &auditResp) == nil {
				auditEntries = make([]components.AuditInfo, len(auditResp.Entries))
				for i, a := range auditResp.Entries {
					auditEntries[i] = components.AuditInfo{
						ID:        a.ID,
						Layer:     a.Layer,
						Source:    a.Source,
						Content:   a.Content,
						TraceID:   a.TraceID,
						Sampled:   a.Sampled,
						CreatedAt: a.CreatedAt,
					}
				}
			}
		}

		// Fetch pending approvals.
		var approvalEntries []components.ApprovalInfo
		approvalData, approvalErr := client.Call("list_pending_approvals", nil)
		if approvalErr == nil {
			var approvalResp struct {
				Approvals []struct {
					ID         string `json:"id"`
					Command    string `json:"command"`
					VassalName string `json:"vassal_name"`
					TraceID    string `json:"trace_id"`
					CreatedAt  string `json:"created_at"`
				} `json:"approvals"`
			}
			if json.Unmarshal(approvalData, &approvalResp) == nil {
				approvalEntries = make([]components.ApprovalInfo, len(approvalResp.Approvals))
				for i, a := range approvalResp.Approvals {
					approvalEntries[i] = components.ApprovalInfo{
						ID:         a.ID,
						Command:    a.Command,
						VassalName: a.VassalName,
						TraceID:    a.TraceID,
						CreatedAt:  a.CreatedAt,
					}
				}
			}
		}

		return dataMsg{
			vassals:   vassals,
			events:    events,
			status:    status,
			audit:     auditEntries,
			approvals: approvalEntries,
		}
	}
}

// respondApproval sends an approve/reject RPC and returns a command.
func respondApproval(client *daemon.Client, requestID string, approved bool) tea.Cmd {
	return func() tea.Msg {
		params := map[string]interface{}{
			"request_id": requestID,
			"approved":   approved,
		}
		_, err := client.Call("respond_approval", params)
		return approvalResultMsg{err: err}
	}
}
