package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexli18/claude-king/internal/daemon"
	"github.com/alexli18/claude-king/internal/vassal"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "status":
		cmdStatus()
	case "list":
		cmdList()
	case "exec":
		cmdExec()
	case "logs":
		cmdLogs()
	case "events":
		cmdEvents()
	case "audit":
		cmdAudit()
	case "snapshot":
		cmdSnapshot()
	case "approvals":
		cmdApprovals()
	case "approve":
		cmdApproveReject(true)
	case "reject":
		cmdApproveReject(false)
	case "report-done":
		cmdReportDone()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: kingctl <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  status                   Show Kingdom status")
	fmt.Fprintln(os.Stderr, "  list                     List all vassals")
	fmt.Fprintln(os.Stderr, "  exec <name> <cmd>        Execute command in vassal")
	fmt.Fprintln(os.Stderr, "  logs <name>              Show output logs for a vassal")
	fmt.Fprintln(os.Stderr, "  events                   Show recent kingdom events")
	fmt.Fprintln(os.Stderr, "  audit                    Show audit log (--layer, --vassal, --limit, --since, --until, --trace)")
	fmt.Fprintln(os.Stderr, "  snapshot [--at <time>]   Show system snapshot at a point in time")
	fmt.Fprintln(os.Stderr, "  approvals                List pending Sovereign Approval requests")
	fmt.Fprintln(os.Stderr, "  approve <id>             Approve a pending request")
	fmt.Fprintln(os.Stderr, "  reject <id>              Reject a pending request")
	fmt.Fprintln(os.Stderr, "  report-done  --task <id> [--artifacts <files...>]  Mark a task as done")
}

func getClient() *daemon.Client {
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	client, err := daemon.NewClient(rootDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot connect to daemon: %v\n", err)
		fmt.Fprintln(os.Stderr, "Is the Kingdom running? Try: king up")
		os.Exit(1)
	}
	return client
}

func cmdStatus() {
	client := getClient()
	defer client.Close()

	result, err := client.Call("status", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var status struct {
		KingdomID string `json:"kingdom_id"`
		Status    string `json:"status"`
		Root      string `json:"root"`
		Vassals   int    `json:"vassals"`
	}
	if err := json.Unmarshal(result, &status); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Kingdom: %s (%s)\n", status.KingdomID[:8], status.Status)
	fmt.Printf("Root: %s\n", status.Root)
	fmt.Printf("Vassals: %d\n", status.Vassals)
}

func cmdList() {
	client := getClient()
	defer client.Close()

	result, err := client.Call("list_vassals", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var data struct {
		Vassals []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Command string `json:"command"`
			PID     int    `json:"pid"`
		} `json:"vassals"`
	}
	if err := json.Unmarshal(result, &data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(data.Vassals) == 0 {
		fmt.Println("No vassals running.")
		return
	}

	fmt.Printf("%-20s %-12s %-8s %s\n", "NAME", "STATUS", "PID", "COMMAND")
	for _, v := range data.Vassals {
		fmt.Printf("%-20s %-12s %-8d %s\n", v.Name, v.Status, v.PID, v.Command)
	}
}

func cmdExec() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "Usage: kingctl exec <vassal-name> <command>")
		os.Exit(1)
	}

	target := os.Args[2]
	command := os.Args[3]

	client := getClient()
	defer client.Close()

	params := map[string]string{
		"target":  target,
		"command": command,
	}

	result, err := client.Call("exec_in", params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(result))
}

func cmdLogs() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: kingctl logs <vassal-name>")
		os.Exit(1)
	}

	name := os.Args[2]

	client := getClient()
	defer client.Close()

	params := map[string]string{
		"name": name,
	}

	result, err := client.Call("get_vassal_output", params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var data struct {
		Name   string `json:"name"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(result, &data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if data.Output == "" {
		fmt.Printf("No output captured for vassal %q.\n", name)
		return
	}

	fmt.Print(data.Output)
}

func cmdEvents() {
	client := getClient()
	defer client.Close()

	result, err := client.Call("get_events", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var data struct {
		Events []struct {
			ID           string `json:"id"`
			Source       string `json:"source"`
			Severity     string `json:"severity"`
			Summary      string `json:"summary"`
			Acknowledged bool   `json:"acknowledged"`
			CreatedAt    string `json:"created_at"`
		} `json:"events"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(result, &data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(data.Events) == 0 {
		fmt.Println("No events recorded.")
		return
	}

	fmt.Printf("%-8s %-10s %-20s %-40s %s\n", "SEV", "ACK", "SOURCE", "SUMMARY", "TIME")
	for _, e := range data.Events {
		ack := " "
		if e.Acknowledged {
			ack = "yes"
		}
		summary := e.Summary
		if len(summary) > 40 {
			summary = summary[:37] + "..."
		}
		fmt.Printf("%-8s %-10s %-20s %-40s %s\n", e.Severity, ack, e.Source, summary, e.CreatedAt)
	}
}

func cmdAudit() {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	layer := fs.String("layer", "", "Filter by layer (ingestion, sieve, action)")
	vassal := fs.String("vassal", "", "Filter by vassal name")
	limit := fs.Int("limit", 50, "Max entries to return")
	since := fs.String("since", "", "Start time (RFC3339 or relative: 5m, 1h, 1d)")
	until := fs.String("until", "", "End time (RFC3339 or relative)")
	traceID := fs.String("trace", "", "Show detailed Action Trace for trace ID")
	fs.Parse(os.Args[2:])

	client := getClient()
	defer client.Close()

	// If --trace is specified, show action trace detail.
	if *traceID != "" {
		cmdAuditTrace(client, *traceID)
		return
	}

	params := map[string]interface{}{
		"limit": *limit,
	}
	if *layer != "" {
		params["layer"] = *layer
	}
	if *vassal != "" {
		params["vassal"] = *vassal
	}
	if *since != "" {
		params["since"] = *since
	}
	if *until != "" {
		params["until"] = *until
	}

	result, err := client.Call("get_audit_log", params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var data struct {
		Entries []struct {
			ID        string `json:"id"`
			Layer     string `json:"layer"`
			Source    string `json:"source"`
			Content   string `json:"content"`
			TraceID   string `json:"trace_id"`
			Sampled   bool   `json:"sampled"`
			CreatedAt string `json:"created_at"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(result, &data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(data.Entries) == 0 {
		fmt.Println("No audit entries recorded.")
		return
	}

	fmt.Printf("%-20s %-10s %-12s %s\n", "TIME", "LAYER", "SOURCE", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range data.Entries {
		content := e.Content
		if len(content) > 50 {
			content = content[:47] + "..."
		}
		sampled := ""
		if e.Sampled {
			sampled = "~"
		}
		fmt.Printf("%-20s %-10s %-12s %s%s\n", e.CreatedAt, e.Layer, e.Source, sampled, content)
	}
	fmt.Printf("\nShowing %d of %d entries\n", len(data.Entries), data.Total)
}

// cmdSnapshot shows active vassals, recent events, and recent actions at a given point in time.
// The --at flag accepts RFC3339 or relative times (5m, 1h, 1d).
func cmdSnapshot() {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	at := fs.String("at", "", "Point in time (RFC3339 or relative: 5m, 1h, 1d). Defaults to now.")
	fs.Parse(os.Args[2:])

	client := getClient()
	defer client.Close()

	// Use the --at time as an "until" filter so we get the state up to that moment.
	since := ""
	until := *at

	fmt.Println("=== Kingdom Snapshot ===")
	if until != "" {
		fmt.Printf("At: %s\n", until)
	} else {
		fmt.Println("At: now")
	}
	fmt.Println()

	// Active vassals.
	vassalData, err := client.Call("list_vassals", nil)
	if err == nil {
		var data struct {
			Vassals []struct {
				Name    string `json:"name"`
				Status  string `json:"status"`
				Command string `json:"command"`
				PID     int    `json:"pid"`
			} `json:"vassals"`
		}
		if json.Unmarshal(vassalData, &data) == nil {
			fmt.Printf("Vassals (%d):\n", len(data.Vassals))
			for _, v := range data.Vassals {
				fmt.Printf("  %-20s %-12s  pid=%-6d  %s\n", v.Name, v.Status, v.PID, v.Command)
			}
		}
	}
	fmt.Println()

	// Recent events (up to --at time).
	auditParams := map[string]interface{}{
		"layer": "sieve",
		"limit": 20,
	}
	if since != "" {
		auditParams["since"] = since
	}
	if until != "" {
		auditParams["until"] = until
	}
	auditData, err := client.Call("get_audit_log", auditParams)
	if err == nil {
		var data struct {
			Entries []struct {
				Layer     string `json:"layer"`
				Source    string `json:"source"`
				Content   string `json:"content"`
				CreatedAt string `json:"created_at"`
			} `json:"entries"`
		}
		if json.Unmarshal(auditData, &data) == nil {
			fmt.Printf("Recent Sieve Events (%d):\n", len(data.Entries))
			for _, e := range data.Entries {
				content := e.Content
				if len(content) > 60 {
					content = content[:57] + "..."
				}
				fmt.Printf("  %-20s %-12s  %s\n", e.CreatedAt, e.Source, content)
			}
		}
	}
	fmt.Println()

	// Recent actions.
	actionParams := map[string]interface{}{
		"layer": "action",
		"limit": 10,
	}
	if since != "" {
		actionParams["since"] = since
	}
	if until != "" {
		actionParams["until"] = until
	}
	actionData, err := client.Call("get_audit_log", actionParams)
	if err == nil {
		var data struct {
			Entries []struct {
				Source    string `json:"source"`
				Content   string `json:"content"`
				TraceID   string `json:"trace_id"`
				CreatedAt string `json:"created_at"`
			} `json:"entries"`
		}
		if json.Unmarshal(actionData, &data) == nil {
			fmt.Printf("Recent Actions (%d):\n", len(data.Entries))
			for _, e := range data.Entries {
				content := e.Content
				if len(content) > 60 {
					content = content[:57] + "..."
				}
				traceStr := ""
				if e.TraceID != "" {
					traceStr = "  [trace:" + e.TraceID + "]"
				}
				fmt.Printf("  %-20s %-12s  %s%s\n", e.CreatedAt, e.Source, content, traceStr)
			}
		}
	}
}

// cmdApprovals lists pending Sovereign Approval requests.
func cmdApprovals() {
	client := getClient()
	defer client.Close()

	result, err := client.Call("list_pending_approvals", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var data struct {
		Approvals []struct {
			ID         string `json:"id"`
			Command    string `json:"command"`
			VassalName string `json:"vassal_name"`
			TraceID    string `json:"trace_id"`
			Status     string `json:"status"`
			CreatedAt  string `json:"created_at"`
		} `json:"approvals"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(result, &data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(data.Approvals) == 0 {
		fmt.Println("No pending approval requests.")
		return
	}

	fmt.Printf("%-36s %-20s %-10s %s\n", "ID", "VASSAL", "TRACE", "COMMAND")
	fmt.Println(strings.Repeat("-", 90))
	for _, a := range data.Approvals {
		cmd := a.Command
		if len(cmd) > 40 {
			cmd = cmd[:37] + "..."
		}
		fmt.Printf("%-36s %-20s %-10s %s\n", a.ID, a.VassalName, a.TraceID, cmd)
	}
	fmt.Printf("\nTotal: %d pending\n", data.Count)
}

// cmdApproveReject approves or rejects a pending Sovereign Approval request.
func cmdApproveReject(approve bool) {
	if len(os.Args) < 3 {
		verb := "approve"
		if !approve {
			verb = "reject"
		}
		fmt.Fprintf(os.Stderr, "Usage: kingctl %s <request-id>\n", verb)
		os.Exit(1)
	}

	requestID := os.Args[2]
	client := getClient()
	defer client.Close()

	params := map[string]interface{}{
		"request_id": requestID,
		"approved":   approve,
	}
	result, err := client.Call("respond_approval", params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var data struct {
		RequestID  string `json:"request_id"`
		TraceID    string `json:"trace_id"`
		Status     string `json:"status"`
		Command    string `json:"command"`
		VassalName string `json:"vassal_name"`
	}
	if err := json.Unmarshal(result, &data); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Request %s: %s\n", data.RequestID, data.Status)
	fmt.Printf("Command: %s\n", data.Command)
	fmt.Printf("Vassal: %s\n", data.VassalName)
	fmt.Printf("Trace: %s\n", data.TraceID)
}

func cmdReportDone() {
	var taskID string
	var kingDirFlag string
	var artifacts []string

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --task requires a value")
				os.Exit(1)
			}
			i++
			taskID = args[i]
		case "--king-dir":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --king-dir requires a value")
				os.Exit(1)
			}
			i++
			kingDirFlag = args[i]
		case "--artifacts":
			// Collect all remaining args as artifacts
			artifacts = args[i+1:]
			i = len(args) // exit loop
		default:
			fmt.Fprintf(os.Stderr, "error: unknown flag: %s\n", args[i])
			os.Exit(1)
		}
	}

	if taskID == "" {
		fmt.Fprintln(os.Stderr, "error: --task is required")
		os.Exit(1)
	}

	var kingDir string
	var err error
	if kingDirFlag != "" {
		kingDir, err = filepath.Abs(kingDirFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		kingDir, err = findKingDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	t, err := vassal.LoadTask(kingDir, taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading task: %v\n", err)
		os.Exit(1)
	}

	t.Status = vassal.TaskStatusDone
	t.Artifacts = artifacts

	if err := vassal.SaveTask(kingDir, t); err != nil {
		fmt.Fprintf(os.Stderr, "error saving task: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Task %s marked as done\n", taskID)
	if len(artifacts) > 0 {
		fmt.Printf("Artifacts: %v\n", artifacts)
	}
}

func cmdAuditTrace(client *daemon.Client, traceID string) {
	result, err := client.Call("get_action_trace", map[string]string{"trace_id": traceID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var trace struct {
		TraceID        string `json:"trace_id"`
		VassalName     string `json:"vassal_name"`
		Command        string `json:"command"`
		Status         string `json:"status"`
		ExitCode       int    `json:"exit_code"`
		Output         string `json:"output"`
		DurationMs     int    `json:"duration_ms"`
		TriggerEventID string `json:"trigger_event_id"`
		StartedAt      string `json:"started_at"`
		CompletedAt    string `json:"completed_at"`
	}
	if err := json.Unmarshal(result, &trace); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Trace: %s\n", trace.TraceID)
	fmt.Printf("Command: %s\n", trace.Command)
	fmt.Printf("Vassal: %s\n", trace.VassalName)
	fmt.Printf("Status: %s\n", trace.Status)
	fmt.Printf("Exit Code: %d\n", trace.ExitCode)
	fmt.Printf("Duration: %dms\n", trace.DurationMs)
	if trace.TriggerEventID != "" {
		fmt.Printf("Trigger: %s\n", trace.TriggerEventID)
	}
	fmt.Printf("Started: %s\n", trace.StartedAt)
	if trace.CompletedAt != "" {
		fmt.Printf("Completed: %s\n", trace.CompletedAt)
	}
	if trace.Output != "" {
		fmt.Printf("\nOutput:\n%s\n", trace.Output)
	}
}

// findKingDir locates the .king directory by checking KING_DIR env var first,
// then walking up from cwd until it finds a directory containing .king/.
func findKingDir() (string, error) {
	// Respect explicit env var.
	if dir := os.Getenv("KING_DIR"); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, ".king"), nil
	}

	// Walk up from cwd.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, ".king")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fallback to cwd/.king for backwards compatibility.
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, ".king"), nil
}
