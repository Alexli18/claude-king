package daemon

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexli18/claude-king/internal/artifacts"
	"github.com/alexli18/claude-king/internal/audit"
	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/events"
	"github.com/alexli18/claude-king/internal/mcp"
	"github.com/alexli18/claude-king/internal/pty"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

const (
	kingDirName  = ".king"
	pidFileName  = "king.pid"  // legacy, kept for reference
	sockFileName = "king.sock" // legacy, kept for reference
	dbFileName   = "king.db"
)

// SocketPathForRoot returns a unique socket path based on the root directory hash.
// This prevents collisions when multiple Kingdoms run in different directories.
func SocketPathForRoot(rootDir string) string {
	h := sha256.Sum256([]byte(rootDir))
	return filepath.Join(rootDir, kingDirName, fmt.Sprintf("king-%x.sock", h[:8]))
}

// pidPathForRoot returns a unique PID file path based on the root directory hash.
func pidPathForRoot(rootDir string) string {
	h := sha256.Sum256([]byte(rootDir))
	return filepath.Join(rootDir, kingDirName, fmt.Sprintf("king-%x.pid", h[:8]))
}

// ---------------------------------------------------------------------------
// JSON-RPC types
// ---------------------------------------------------------------------------

// RPCRequest is a JSON-RPC request envelope.
type RPCRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     int             `json:"id"`
}

// RPCResponse is a JSON-RPC response envelope.
type RPCResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  *RPCError   `json:"error,omitempty"`
	ID     int         `json:"id"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcHandler is a function that processes a JSON-RPC method call.
type rpcHandler func(params json.RawMessage) (interface{}, error)

// ---------------------------------------------------------------------------
// Daemon
// ---------------------------------------------------------------------------

// Daemon manages the lifecycle of a Kingdom daemon process, including the
// UDS server, store, config, and PID file.
type Daemon struct {
	rootDir       string
	store         *store.Store
	config        *config.KingdomConfig
	kingdom       *Kingdom
	ptyMgr        *pty.Manager
	sieve         *events.Sieve
	auditRecorder *audit.AuditRecorder
	approvalMgr   *audit.ApprovalManager
	mcpSrv        *mcp.Server
	listener      net.Listener
	pidFile       string
	sockPath      string
	ctx           context.Context
	cancel        context.CancelFunc
	logger        *slog.Logger
	handlers      map[string]rpcHandler
	wg            sync.WaitGroup
}

// NewDaemon creates a new daemon instance for the given root directory.
func NewDaemon(rootDir string) (*Daemon, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root dir: %w", err)
	}

	logLevel := slog.LevelInfo
	if os.Getenv("KING_DEBUG") != "" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})).With("component", "daemon")

	d := &Daemon{
		rootDir:  absRoot,
		pidFile:  pidPathForRoot(absRoot),
		sockPath: SocketPathForRoot(absRoot),
		logger:   logger,
		handlers: make(map[string]rpcHandler),
	}

	d.registerStubHandlers()

	return d, nil
}

// Start initializes the daemon: opens store, loads config, writes PID file,
// starts UDS server, and creates the Kingdom.
func (d *Daemon) Start(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)

	// Ensure .king directory exists.
	if err := config.EnsureKingDir(d.rootDir); err != nil {
		return fmt.Errorf("ensure king dir: %w", err)
	}

	// Check for duplicate daemon (T015).
	running, err := IsRunning(d.rootDir)
	if err != nil {
		return fmt.Errorf("check running: %w", err)
	}
	if running {
		return errors.New("kingdom already running")
	}

	// Clean up any stale socket/PID files from prior crashed daemons (T043).
	if err := d.cleanStaleFiles(); err != nil {
		return fmt.Errorf("clean stale files: %w", err)
	}

	// Clean up our own stale socket if present.
	if err := d.cleanStaleSocket(); err != nil {
		return fmt.Errorf("clean stale socket: %w", err)
	}

	// Open store.
	dbPath := filepath.Join(d.rootDir, kingDirName, dbFileName)
	d.store, err = store.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	d.logger.Info("store opened", "path", dbPath)

	// Load or create config.
	d.config, err = config.LoadOrCreateConfig(d.rootDir)
	if err != nil {
		d.store.Close()
		return fmt.Errorf("load config: %w", err)
	}
	d.logger.Info("config loaded", "name", d.config.Name, "vassals", len(d.config.Vassals))

	// Validate config.
	if err := config.Validate(d.config); err != nil {
		d.store.Close()
		return fmt.Errorf("validate config: %w", err)
	}

	// Write PID file.
	if err := d.writePIDFile(); err != nil {
		d.store.Close()
		return fmt.Errorf("write pid file: %w", err)
	}

	// Start UDS listener.
	d.listener, err = net.Listen("unix", d.sockPath)
	if err != nil {
		d.removePIDFile()
		d.store.Close()
		return fmt.Errorf("listen on socket: %w", err)
	}

	// Create Kingdom.
	d.kingdom, err = NewKingdom(d.store, d.config, d.rootDir, d.logger)
	if err != nil {
		d.listener.Close()
		os.Remove(d.sockPath)
		d.removePIDFile()
		d.store.Close()
		return fmt.Errorf("create kingdom: %w", err)
	}

	// Clean up old events based on retention policy (T055).
	if d.config.Settings.LogRetentionDays > 0 {
		if err := d.store.DeleteOldEvents(d.kingdom.ID, d.config.Settings.LogRetentionDays); err != nil {
			d.logger.Warn("failed to clean old events", "err", err)
		}
	}

	// Clean up old audit entries based on retention policy.
	if err := d.store.DeleteOldAuditEntries(d.kingdom.ID,
		d.config.Settings.AuditIngestionRetentionDays,
		d.config.Settings.AuditRetentionDays); err != nil {
		d.logger.Warn("failed to clean old audit entries", "err", err)
	}

	// Create Semantic Sieve for event detection (T031).
	compiledPatterns, err := events.CompilePatterns(d.config.Patterns)
	if err != nil {
		d.logger.Warn("failed to compile event patterns", "err", err)
		compiledPatterns = nil
	}
	d.sieve = events.NewSieve(compiledPatterns, d.store, d.kingdom.ID,
		d.config.Settings.EventCooldownSeconds, d.logger.With("component", "sieve"))

	// Create AuditRecorder.
	d.auditRecorder = audit.NewAuditRecorder(d.store, d.kingdom.ID, d.logger.With("component", "audit"))

	// Create ApprovalManager for Sovereign Approval gating.
	d.approvalMgr = audit.NewApprovalManager()

	// Mark any pending approvals from a previous (crashed) daemon run as expired (T043).
	if err := d.store.ExpirePendingApprovals(d.kingdom.ID); err != nil {
		d.logger.Warn("failed to expire pending approvals", "err", err)
	}

	// Set audit callback on Sieve for recording sieve-layer decisions.
	d.sieve.SetAuditCallback(func(vassalName, vassalID, content, decision, pattern, severity, summary string) {
		d.auditRecorder.RecordSieve(vassalName, vassalID, content, audit.SieveDecision{
			Decision: decision,
			Pattern:  pattern,
			Severity: severity,
			Summary:  summary,
		})
	})

	// Create PTY manager, recover stale sessions (T045), and start autostart vassals (T014).
	d.ptyMgr = pty.NewManager(d.store, d.kingdom.ID, d.logger.With("component", "pty"))
	if err := d.ptyMgr.RecoverSessions(); err != nil {
		d.logger.Error("failed to recover sessions", "err", err)
		// Non-fatal; continue startup.
	}
	if err := d.startVassals(); err != nil {
		d.listener.Close()
		os.Remove(d.sockPath)
		d.removePIDFile()
		d.store.Close()
		return fmt.Errorf("start vassals: %w", err)
	}

	// Create artifact ledger and MCP server.
	ledger := artifacts.NewLedger(d.store, d.kingdom.ID)
	adapter := &ptyManagerAdapter{mgr: d.ptyMgr}
	d.mcpSrv = mcp.NewServer(adapter, d.store, ledger, d.kingdom.ID, d.rootDir, d.logger.With("component", "mcp"))

	// Wire ApprovalManager into MCP server for respond_approval signaling.
	d.mcpSrv.SetApprovalManager(
		d.approvalMgr,
		d.config.Settings.SovereignApproval,
		d.config.Settings.SovereignApprovalTimeout,
	)

	// Update RPC handlers to use real implementations.
	d.registerRealHandlers()

	// Start periodic audit retention cleanup (every 6 hours) (T044).
	d.wg.Add(1)
	go d.auditCleanupLoop()

	if err := d.kingdom.SetStatus("running"); err != nil {
		d.listener.Close()
		os.Remove(d.sockPath)
		d.removePIDFile()
		d.store.Close()
		return fmt.Errorf("set kingdom status: %w", err)
	}

	// Accept connections in background.
	d.wg.Add(1)
	go d.acceptLoop()

	d.logger.Info("daemon started",
		"root", d.rootDir,
		"pid", os.Getpid(),
		"socket", d.sockPath,
		"kingdom_id", d.kingdom.ID,
	)

	return nil
}

// auditCleanupLoop periodically deletes old audit entries based on retention policy (T044).
func (d *Daemon) auditCleanupLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := d.store.DeleteOldAuditEntries(
				d.kingdom.ID,
				d.config.Settings.AuditIngestionRetentionDays,
				d.config.Settings.AuditRetentionDays,
			); err != nil {
				d.logger.Warn("periodic audit cleanup failed", "err", err)
			}
		case <-d.ctx.Done():
			return
		}
	}
}

// startVassals creates PTY sessions for all autostart vassals in config (T014).
func (d *Daemon) startVassals() error {
	for _, vc := range d.config.Vassals {
		if !vc.AutostartOrDefault() {
			d.logger.Info("skipping non-autostart vassal", "name", vc.Name)
			continue
		}

		cwd := vc.Cwd
		if cwd != "" && !filepath.IsAbs(cwd) {
			cwd = filepath.Join(d.rootDir, cwd)
		}
		if cwd == "" {
			cwd = d.rootDir
		}

		id := uuid.New().String()
		_, err := d.ptyMgr.CreateSession(id, vc.Name, vc.Command, cwd, vc.Env)
		if err != nil {
			d.logger.Error("failed to start vassal", "name", vc.Name, "err", err)
			continue
		}

		// Wire up Semantic Sieve to monitor vassal output (T031).
		// Also record ingestion audit entries when audit_ingestion is enabled.
		if d.sieve != nil {
			sieveCallback := d.sieve.OutputCallback(vc.Name, id)
			auditIngestion := d.config.Settings.AuditIngestion
			recorder := d.auditRecorder
			vassalName := vc.Name
			vassalID := id
			_ = d.ptyMgr.SetOnOutput(vc.Name, func(line string) {
				// Ingestion layer audit (if enabled).
				if auditIngestion && recorder != nil {
					recorder.RecordIngestion(vassalName, vassalID, line, false)
				}
				// Sieve processing.
				sieveCallback(line)
			})
		}

		// Start hang detector (T058).
		if sess, ok := d.ptyMgr.GetSession(vc.Name); ok {
			sess.StartHangDetector(5*time.Minute, func(name string) {
				d.logger.Warn("vassal appears hung (no output)", "name", name)
			})
		}

		// VMP auto-registration (T040): load vassal.json from repo_path.
		if vc.RepoPath != "" {
			repoPath := vc.RepoPath
			if !filepath.IsAbs(repoPath) {
				repoPath = filepath.Join(d.rootDir, repoPath)
			}
			manifestPath := filepath.Join(repoPath, "vassal.json")
			if manifest, err := config.LoadVassalManifest(manifestPath); err == nil {
				ledger := artifacts.NewLedger(d.store, d.kingdom.ID)
				for _, art := range manifest.Artifacts {
					artPath := filepath.Join(repoPath, art.Path)
					if _, regErr := ledger.Register(art.Name, artPath, id, art.MimeType); regErr != nil {
						d.logger.Warn("failed to register VMP artifact", "name", art.Name, "err", regErr)
					}
				}
				d.logger.Info("VMP manifest loaded", "vassal", vc.Name, "skills", len(manifest.Skills), "artifacts", len(manifest.Artifacts))
			}
		}

		d.logger.Info("vassal started", "name", vc.Name)
	}
	return nil
}

// PTYMgr returns the PTY manager for external use.
func (d *Daemon) PTYMgr() *pty.Manager {
	return d.ptyMgr
}

// MCPServer returns the MCP server for external use (e.g., stdio mode).
func (d *Daemon) MCPServer() *mcp.Server {
	return d.mcpSrv
}

// Stop gracefully shuts down the daemon (T044):
//  1. Set kingdom status to "stopping"
//  2. Send SIGTERM to all vassals (via StopAll which does SIGTERM then SIGKILL)
//  3. Wait up to 10 seconds for graceful exit
//  4. Close DB
//  5. Remove socket and PID files
//  6. Set status to "stopped"
func (d *Daemon) Stop() error {
	d.logger.Info("daemon stopping")

	// Step 1: Set kingdom status to "stopping".
	if d.kingdom != nil {
		if err := d.kingdom.SetStatus("stopping"); err != nil {
			d.logger.Error("failed to set kingdom stopping status", "err", err)
		}
	}

	// Stop audit recorder (flush pending batches).
	if d.auditRecorder != nil {
		d.auditRecorder.Stop()
	}

	// Signal cancellation to all goroutines.
	if d.cancel != nil {
		d.cancel()
	}

	// Step 2-3: Stop all PTY sessions (SIGTERM, wait, then SIGKILL).
	if d.ptyMgr != nil {
		d.logger.Info("stopping all vassals")
		stopDone := make(chan error, 1)
		go func() {
			stopDone <- d.ptyMgr.StopAll()
		}()

		select {
		case err := <-stopDone:
			if err != nil {
				d.logger.Error("failed to stop all vassals", "err", err)
			}
		case <-time.After(10 * time.Second):
			d.logger.Warn("vassal shutdown timed out after 10s, forcing kill")
		}
	}

	// Close the listener to unblock acceptLoop.
	if d.listener != nil {
		d.listener.Close()
	}

	// Wait for connection handlers to finish.
	d.wg.Wait()

	// Step 5: Remove socket and PID files.
	os.Remove(d.sockPath)
	d.removePIDFile()

	// Step 6: Set kingdom status to "stopped" before closing store.
	if d.kingdom != nil {
		if err := d.kingdom.SetStatus("stopped"); err != nil {
			d.logger.Error("failed to set kingdom stopped status", "err", err)
		}
	}

	// Step 4: Close store (DB).
	if d.store != nil {
		if err := d.store.Close(); err != nil {
			d.logger.Error("failed to close store", "err", err)
		}
	}

	d.logger.Info("daemon stopped")
	return nil
}

// IsRunning checks if another daemon is already running for this directory
// by checking the PID file and socket connectivity.
func IsRunning(rootDir string) (bool, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return false, fmt.Errorf("resolve root dir: %w", err)
	}

	pidPath := pidPathForRoot(absRoot)
	sockPath := SocketPathForRoot(absRoot)

	// Check PID file.
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		// Corrupt PID file; not running.
		return false, nil
	}

	// Check if the process is alive.
	if !processAlive(pid) {
		// Stale PID file; process is dead.
		return false, nil
	}

	// Process exists; verify it is listening on the socket.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		// Process alive but socket not responding; treat as not running.
		return false, nil
	}
	conn.Close()

	return true, nil
}

// ---------------------------------------------------------------------------
// UDS accept loop & connection handling
// ---------------------------------------------------------------------------

func (d *Daemon) acceptLoop() {
	defer d.wg.Done()

	for {
		conn, err := d.listener.Accept()
		if err != nil {
			// Check if we are shutting down.
			select {
			case <-d.ctx.Done():
				return
			default:
			}
			d.logger.Error("accept error", "err", err)
			return
		}

		d.wg.Add(1)
		go d.handleConnection(conn)
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer d.wg.Done()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	// Allow up to 1 MB per line for large JSON-RPC payloads.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req RPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := RPCResponse{
				Error: &RPCError{Code: -32700, Message: "parse error"},
				ID:    0,
			}
			d.writeResponse(conn, resp)
			continue
		}

		resp := d.dispatch(req)
		d.writeResponse(conn, resp)
	}
}

func (d *Daemon) writeResponse(conn net.Conn, resp RPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		d.logger.Error("marshal response", "err", err)
		return
	}
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		d.logger.Error("write response", "err", err)
	}
}

func (d *Daemon) dispatch(req RPCRequest) RPCResponse {
	d.logger.Debug("rpc call", "method", req.Method, "id", req.ID)

	handler, ok := d.handlers[req.Method]
	if !ok {
		d.logger.Debug("rpc method not found", "method", req.Method)
		return RPCResponse{
			Error: &RPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
			ID:    req.ID,
		}
	}

	result, err := handler(req.Params)
	if err != nil {
		d.logger.Debug("rpc call failed", "method", req.Method, "err", err)
		return RPCResponse{
			Error: &RPCError{Code: -32000, Message: err.Error()},
			ID:    req.ID,
		}
	}

	return RPCResponse{
		Result: result,
		ID:     req.ID,
	}
}

// ---------------------------------------------------------------------------
// PTY manager adapter for MCP interface
// ---------------------------------------------------------------------------

// ptyManagerAdapter bridges pty.Manager to the mcp.PTYManager interface.
type ptyManagerAdapter struct {
	mgr *pty.Manager
}

func (a *ptyManagerAdapter) GetSession(name string) (mcp.PTYSession, bool) {
	s, ok := a.mgr.GetSession(name)
	if !ok {
		return nil, false
	}
	return s, true
}

func (a *ptyManagerAdapter) ListSessions() []mcp.PTYSessionInfo {
	sessions := a.mgr.ListSessions()
	infos := make([]mcp.PTYSessionInfo, len(sessions))
	for i, s := range sessions {
		s.RLock()
		infos[i] = mcp.PTYSessionInfo{
			Name:    s.Name,
			Status:  s.Status,
			Command: s.Command,
			PID:     s.PID,
		}
		s.RUnlock()
	}
	return infos
}

// ---------------------------------------------------------------------------
// Real RPC handlers (wired to PTY manager and store)
// ---------------------------------------------------------------------------

func (d *Daemon) registerRealHandlers() {
	// list_vassals
	d.handlers["list_vassals"] = func(_ json.RawMessage) (interface{}, error) {
		sessions := d.ptyMgr.ListSessions()
		type vassalEntry struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Command string `json:"command"`
			PID     int    `json:"pid"`
		}
		vassals := make([]vassalEntry, len(sessions))
		for i, s := range sessions {
			s.RLock()
			vassals[i] = vassalEntry{Name: s.Name, Status: s.Status, Command: s.Command, PID: s.PID}
			s.RUnlock()
		}
		return map[string]interface{}{
			"vassals": vassals,
			"kingdom": map[string]interface{}{
				"id":     d.kingdom.ID,
				"name":   d.config.Name,
				"status": d.kingdom.GetStatus(),
			},
		}, nil
	}

	// status
	d.handlers["status"] = func(_ json.RawMessage) (interface{}, error) {
		sessions := d.ptyMgr.ListSessions()
		return map[string]interface{}{
			"kingdom_id": d.kingdom.ID,
			"status":     d.kingdom.GetStatus(),
			"root":       d.rootDir,
			"vassals":    len(sessions),
		}, nil
	}

	// exec_in
	d.handlers["exec_in"] = func(params json.RawMessage) (interface{}, error) {
		var p struct {
			Target  string `json:"target"`
			Command string `json:"command"`
			Timeout int    `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if p.Target == "" {
			return nil, fmt.Errorf("target is required")
		}
		if p.Command == "" {
			return nil, fmt.Errorf("command is required")
		}

		sess, ok := d.ptyMgr.GetSession(p.Target)
		if !ok {
			return nil, fmt.Errorf("VASSAL_NOT_FOUND: no vassal named %q", p.Target)
		}

		timeout := 30 * time.Second
		if p.Timeout > 0 {
			timeout = time.Duration(p.Timeout) * time.Second
		}

		// Look up vassal ID from store for audit trail.
		var vassalID string
		if v, err := d.store.GetVassalByName(d.kingdom.ID, p.Target); err == nil && v != nil {
			vassalID = v.ID
		}

		// Generate trace ID upfront (needed for approval request linkage).
		traceID := uuid.New().String()[:8]
		traceStartedAt := time.Now().UTC().Format("2006-01-02 15:04:05")

		// Create ActionTrace with status "running" before approval / execution so
		// the ApprovalRequest FK is always satisfied and in-flight traces are visible.
		initialTrace := store.ActionTrace{
			TraceID:    traceID,
			KingdomID:  d.kingdom.ID,
			VassalName: p.Target,
			VassalID:   vassalID,
			Command:    p.Command,
			Status:     "running",
			StartedAt:  traceStartedAt,
		}
		if storeErr := d.store.CreateActionTrace(initialTrace); storeErr != nil {
			d.logger.Warn("failed to create initial action trace", "err", storeErr)
		}

		// Sovereign Approval gate (T036): block until approved/rejected/timeout.
		if d.config.Settings.SovereignApproval && d.approvalMgr != nil {
			approvalID := uuid.New().String()
			approvalTimeout := time.Duration(d.config.Settings.SovereignApprovalTimeout) * time.Second
			if approvalTimeout <= 0 {
				approvalTimeout = 300 * time.Second
			}

			req := store.ApprovalRequest{
				ID:         approvalID,
				KingdomID:  d.kingdom.ID,
				TraceID:    traceID,
				Command:    p.Command,
				VassalName: p.Target,
				Status:     "pending",
				CreatedAt:  time.Now().UTC().Format("2006-01-02 15:04:05"),
			}
			if createErr := d.store.CreateApprovalRequest(req); createErr != nil {
				return nil, fmt.Errorf("create approval request: %w", createErr)
			}

			ch := d.approvalMgr.Request(approvalID)
			select {
			case approved := <-ch:
				respondedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
				if !approved {
					_ = d.store.UpdateApprovalRequest(approvalID, "rejected", respondedAt)
					// Update trace to failed so the FK is satisfied with a terminal state.
					_ = d.store.UpdateActionTrace(store.ActionTrace{
						TraceID:     traceID,
						Status:      "failed",
						CompletedAt: respondedAt,
					})
					return nil, fmt.Errorf("APPROVAL_REJECTED: command rejected by sovereign")
				}
				_ = d.store.UpdateApprovalRequest(approvalID, "approved", respondedAt)
			case <-time.After(approvalTimeout):
				// Only write "timeout" if we are the ones cancelling the pending entry.
				// If respond_approval raced us here, Cancel returns false and the
				// respond_approval DB write (approved/rejected) takes precedence.
				if d.approvalMgr.Cancel(approvalID) {
					now := time.Now().UTC().Format("2006-01-02 15:04:05")
					_ = d.store.UpdateApprovalRequest(approvalID, "timeout", now)
					_ = d.store.UpdateActionTrace(store.ActionTrace{
						TraceID:     traceID,
						Status:      "timeout",
						CompletedAt: now,
					})
				}
				return nil, fmt.Errorf("APPROVAL_TIMEOUT: approval request timed out after %v", approvalTimeout)
			}
		}

		output, exitCode, duration, err := sess.ExecCommand(p.Command, timeout)

		// Determine trace status.
		traceStatus := "completed"
		if err != nil {
			if strings.Contains(err.Error(), "timed out") {
				traceStatus = "timeout"
			} else {
				traceStatus = "failed"
			}
		} else if exitCode != 0 {
			traceStatus = "failed"
		}

		// Truncate output for trace storage.
		traceOutput := output
		maxOutput := d.config.Settings.AuditMaxTraceOutput
		if maxOutput > 0 && len(traceOutput) > maxOutput {
			traceOutput = traceOutput[:maxOutput]
		}

		completedAt := time.Now().UTC().Format("2006-01-02 15:04:05")

		// Update ActionTrace with final result.
		if storeErr := d.store.UpdateActionTrace(store.ActionTrace{
			TraceID:     traceID,
			Status:      traceStatus,
			ExitCode:    exitCode,
			Output:      traceOutput,
			DurationMs:  int(duration.Milliseconds()),
			CompletedAt: completedAt,
		}); storeErr != nil {
			d.logger.Warn("failed to update action trace", "err", storeErr)
		}

		// Record action audit entry.
		if d.auditRecorder != nil {
			desc := fmt.Sprintf("exec_in: %s (trace: %s, status: %s)", p.Command, traceID, traceStatus)
			d.auditRecorder.RecordAction(p.Target, vassalID, desc, traceID)
		}

		if err != nil {
			return nil, fmt.Errorf("exec error: %w", err)
		}

		return map[string]interface{}{
			"output":      output,
			"exit_code":   exitCode,
			"duration_ms": duration.Milliseconds(),
			"trace_id":    traceID,
		}, nil
	}

	// get_vassal_output (T046) - returns the ring buffer contents for a vassal.
	d.handlers["get_vassal_output"] = func(params json.RawMessage) (interface{}, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if p.Name == "" {
			return nil, fmt.Errorf("name is required")
		}

		sess, ok := d.ptyMgr.GetSession(p.Name)
		if !ok {
			return nil, fmt.Errorf("VASSAL_NOT_FOUND: no vassal named %q", p.Name)
		}

		output := sess.GetOutput()
		return map[string]interface{}{
			"name":   p.Name,
			"output": string(output),
		}, nil
	}

	// get_events (T046) - returns recent events from the store.
	d.handlers["get_events"] = func(params json.RawMessage) (interface{}, error) {
		var p struct {
			Severity string `json:"severity"`
			Source   string `json:"source"`
			Limit    int    `json:"limit"`
		}
		if params != nil {
			_ = json.Unmarshal(params, &p)
		}
		if p.Limit <= 0 {
			p.Limit = 50
		}

		events, err := d.store.ListEvents(d.kingdom.ID, p.Severity, p.Source, p.Limit)
		if err != nil {
			return nil, fmt.Errorf("list events: %w", err)
		}

		type eventEntry struct {
			ID          string `json:"id"`
			Source      string `json:"source"`
			Severity    string `json:"severity"`
			Summary     string `json:"summary"`
			Pattern     string `json:"pattern,omitempty"`
			Acknowledged bool  `json:"acknowledged"`
			CreatedAt   string `json:"created_at"`
		}
		entries := make([]eventEntry, len(events))
		for i, e := range events {
			entries[i] = eventEntry{
				ID:           e.ID,
				Source:       e.SourceID,
				Severity:     e.Severity,
				Summary:      e.Summary,
				Pattern:      e.Pattern,
				Acknowledged: e.Acknowledged,
				CreatedAt:    e.CreatedAt,
			}
		}

		return map[string]interface{}{
			"events": entries,
			"count":  len(entries),
		}, nil
	}

	// get_action_trace
	d.handlers["get_action_trace"] = func(params json.RawMessage) (interface{}, error) {
		var p struct {
			TraceID string `json:"trace_id"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if p.TraceID == "" {
			return nil, fmt.Errorf("trace_id is required")
		}

		trace, err := d.store.GetActionTrace(p.TraceID)
		if err != nil {
			return nil, fmt.Errorf("get action trace: %w", err)
		}
		if trace == nil {
			return nil, fmt.Errorf("action trace not found: %s", p.TraceID)
		}

		result := map[string]interface{}{
			"trace_id":     trace.TraceID,
			"vassal_name":  trace.VassalName,
			"command":      trace.Command,
			"status":       trace.Status,
			"exit_code":    trace.ExitCode,
			"output":       trace.Output,
			"duration_ms":  trace.DurationMs,
			"started_at":   trace.StartedAt,
			"completed_at": trace.CompletedAt,
		}
		if trace.TriggerEventID != "" {
			result["trigger_event_id"] = trace.TriggerEventID
		}
		return result, nil
	}

	// get_audit_log
	d.handlers["get_audit_log"] = func(params json.RawMessage) (interface{}, error) {
		var p struct {
			Layer   string `json:"layer"`
			Vassal  string `json:"vassal"`
			Since   string `json:"since"`
			Until   string `json:"until"`
			TraceID string `json:"trace_id"`
			Limit   int    `json:"limit"`
		}
		if params != nil {
			_ = json.Unmarshal(params, &p)
		}

		if p.Layer != "" && p.Layer != "ingestion" && p.Layer != "sieve" && p.Layer != "action" {
			return nil, fmt.Errorf("invalid layer: must be ingestion, sieve, or action")
		}

		if p.Limit <= 0 {
			p.Limit = 50
		}
		if p.Limit > 500 {
			p.Limit = 500
		}

		f := store.AuditFilter{
			KingdomID: d.kingdom.ID,
			Layer:     p.Layer,
			Source:    p.Vassal,
			Since:     audit.ParseRelativeTime(p.Since),
			Until:     audit.ParseRelativeTime(p.Until),
			TraceID:   p.TraceID,
			Limit:     p.Limit,
		}

		entries, err := d.store.ListAuditEntries(f)
		if err != nil {
			return nil, fmt.Errorf("list audit entries: %w", err)
		}

		total, _ := d.store.CountAuditEntries(f)

		type auditEntry struct {
			ID        string `json:"id"`
			Layer     string `json:"layer"`
			Source    string `json:"source"`
			Content   string `json:"content"`
			TraceID   string `json:"trace_id,omitempty"`
			Metadata  string `json:"metadata,omitempty"`
			Sampled   bool   `json:"sampled"`
			CreatedAt string `json:"created_at"`
		}

		result := make([]auditEntry, len(entries))
		for i, e := range entries {
			result[i] = auditEntry{
				ID:        e.ID,
				Layer:     e.Layer,
				Source:    e.Source,
				Content:   e.Content,
				TraceID:   e.TraceID,
				Metadata:  e.Metadata,
				Sampled:   e.Sampled,
				CreatedAt: e.CreatedAt,
			}
		}

		return map[string]interface{}{
			"entries":    result,
			"total":     total,
			"filtered":  p.Layer != "" || p.Vassal != "" || p.Since != "" || p.Until != "" || p.TraceID != "",
			"kingdom_id": d.kingdom.ID,
		}, nil
	}

	// respond_approval (T037)
	d.handlers["respond_approval"] = func(params json.RawMessage) (interface{}, error) {
		if !d.config.Settings.SovereignApproval {
			return nil, fmt.Errorf("sovereign_approval is not enabled")
		}
		var p struct {
			RequestID string `json:"request_id"`
			Approved  bool   `json:"approved"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if p.RequestID == "" {
			return nil, fmt.Errorf("request_id is required")
		}

		req, err := d.store.GetApprovalRequest(p.RequestID)
		if err != nil {
			return nil, fmt.Errorf("get approval request: %w", err)
		}
		if req == nil {
			return nil, fmt.Errorf("approval request not found: %s", p.RequestID)
		}
		if req.Status != "pending" {
			return nil, fmt.Errorf("approval request is not pending (status: %s)", req.Status)
		}

		status := "rejected"
		if p.Approved {
			status = "approved"
		}
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		if err := d.store.UpdateApprovalRequest(p.RequestID, status, now); err != nil {
			return nil, fmt.Errorf("update approval request: %w", err)
		}

		// Signal waiting exec_in goroutine.
		if d.approvalMgr != nil {
			_ = d.approvalMgr.Respond(p.RequestID, p.Approved)
		}

		return map[string]interface{}{
			"request_id":  p.RequestID,
			"trace_id":    req.TraceID,
			"status":      status,
			"command":     req.Command,
			"vassal_name": req.VassalName,
		}, nil
	}

	// list_pending_approvals (T039)
	d.handlers["list_pending_approvals"] = func(_ json.RawMessage) (interface{}, error) {
		approvals, err := d.store.ListPendingApprovals(d.kingdom.ID)
		if err != nil {
			return nil, fmt.Errorf("list pending approvals: %w", err)
		}

		type approvalEntry struct {
			ID         string `json:"id"`
			TraceID    string `json:"trace_id"`
			Command    string `json:"command"`
			VassalName string `json:"vassal_name"`
			Status     string `json:"status"`
			CreatedAt  string `json:"created_at"`
		}
		entries := make([]approvalEntry, len(approvals))
		for i, a := range approvals {
			entries[i] = approvalEntry{
				ID:         a.ID,
				TraceID:    a.TraceID,
				Command:    a.Command,
				VassalName: a.VassalName,
				Status:     a.Status,
				CreatedAt:  a.CreatedAt,
			}
		}
		return map[string]interface{}{
			"approvals": entries,
			"count":     len(entries),
		}, nil
	}

	// shutdown
	d.handlers["shutdown"] = func(_ json.RawMessage) (interface{}, error) {
		d.logger.Info("shutdown requested via RPC")
		if d.cancel != nil {
			d.cancel()
		}
		return map[string]string{"status": "shutting_down"}, nil
	}
}

// ---------------------------------------------------------------------------
// Stub RPC handlers
// ---------------------------------------------------------------------------

func (d *Daemon) registerStubHandlers() {
	stubs := []string{
		"list_vassals",
		"exec_in",
		"get_events",
		"register_artifact",
		"resolve_artifact",
		"read_neighbor",
	}

	for _, method := range stubs {
		d.handlers[method] = func(_ json.RawMessage) (interface{}, error) {
			return map[string]interface{}{}, nil
		}
	}

	// status returns the current kingdom state.
	d.handlers["status"] = func(_ json.RawMessage) (interface{}, error) {
		if d.kingdom == nil {
			return map[string]string{"status": "unknown"}, nil
		}
		return map[string]string{
			"kingdom_id": d.kingdom.ID,
			"status":     d.kingdom.GetStatus(),
			"root":       d.rootDir,
		}, nil
	}

	// shutdown triggers a graceful daemon stop.
	d.handlers["shutdown"] = func(_ json.RawMessage) (interface{}, error) {
		d.logger.Info("shutdown requested via RPC")
		// Cancel the daemon context; Stop() will be called by the caller.
		if d.cancel != nil {
			d.cancel()
		}
		return map[string]string{"status": "shutting_down"}, nil
	}
}

// ---------------------------------------------------------------------------
// PID file management
// ---------------------------------------------------------------------------

func (d *Daemon) writePIDFile() error {
	// Clean up stale PID file if present.
	if data, err := os.ReadFile(d.pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if !processAlive(pid) {
				os.Remove(d.pidFile)
				d.logger.Info("removed stale pid file", "stale_pid", pid)
			}
		}
	}

	pid := os.Getpid()
	if err := os.WriteFile(d.pidFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	d.logger.Info("wrote pid file", "pid", pid, "path", d.pidFile)
	return nil
}

func (d *Daemon) removePIDFile() {
	if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
		d.logger.Error("remove pid file", "err", err)
	}
}

// ---------------------------------------------------------------------------
// Stale file cleanup (T043)
// ---------------------------------------------------------------------------

// cleanStaleSocket removes a stale socket file for this daemon's own socket path.
func (d *Daemon) cleanStaleSocket() error {
	if _, err := os.Stat(d.sockPath); os.IsNotExist(err) {
		return nil
	}

	// Try connecting; if nobody answers, it is stale.
	conn, err := net.Dial("unix", d.sockPath)
	if err != nil {
		// Socket exists but nobody is listening; remove it.
		d.logger.Info("removing stale socket", "path", d.sockPath)
		return os.Remove(d.sockPath)
	}
	conn.Close()

	// Someone is listening; this should have been caught by IsRunning.
	return errors.New("kingdom already running")
}

// cleanStaleFiles scans the .king/ directory for any stale king-*.sock and
// king-*.pid files left behind by crashed daemons. For each socket, it checks
// whether anyone is listening; for each PID file, it checks whether the
// process is still alive. Stale files are removed.
func (d *Daemon) cleanStaleFiles() error {
	kingDir := filepath.Join(d.rootDir, kingDirName)

	// Clean stale socket files.
	sockFiles, err := filepath.Glob(filepath.Join(kingDir, "king-*.sock"))
	if err != nil {
		return fmt.Errorf("glob sock files: %w", err)
	}
	for _, sockFile := range sockFiles {
		conn, dialErr := net.Dial("unix", sockFile)
		if dialErr != nil {
			// Nobody listening; stale socket.
			d.logger.Info("removing stale socket file", "path", sockFile)
			os.Remove(sockFile)
		} else {
			conn.Close()
		}
	}

	// Clean stale PID files.
	pidFiles, err := filepath.Glob(filepath.Join(kingDir, "king-*.pid"))
	if err != nil {
		return fmt.Errorf("glob pid files: %w", err)
	}
	for _, pidFile := range pidFiles {
		data, readErr := os.ReadFile(pidFile)
		if readErr != nil {
			continue
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr != nil {
			// Corrupt PID file; remove it.
			d.logger.Info("removing corrupt pid file", "path", pidFile)
			os.Remove(pidFile)
			continue
		}
		if !processAlive(pid) {
			d.logger.Info("removing stale pid file", "path", pidFile, "stale_pid", pid)
			os.Remove(pidFile)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Process helpers
// ---------------------------------------------------------------------------

// processAlive checks whether a process with the given PID exists.
// On Unix, sending signal 0 checks for process existence without
// actually delivering a signal.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
