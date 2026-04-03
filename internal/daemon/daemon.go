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
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexli18/claude-king/internal/artifacts"
	"github.com/alexli18/claude-king/internal/audit"
	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/discovery"
	"github.com/alexli18/claude-king/internal/events"
	"github.com/alexli18/claude-king/internal/fingerprint"
	"github.com/alexli18/claude-king/internal/mcp"
	"github.com/alexli18/claude-king/internal/pty"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

const (
	kingDirName = ".king"
	// Legacy fixed filenames (pre-hash scheme): king.pid, king.sock
	dbFileName = "king.db"
)

const (
	vassalRestartInitialBackoff = 1 * time.Second
	vassalRestartMaxBackoff     = 60 * time.Second
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

// ExternalVassalInfo holds metadata about a vassal registered via vassal.register RPC.
// Used for vassals started externally (e.g. via --stdio mode).
type ExternalVassalInfo struct {
	Name     string `json:"name"`
	RepoPath string `json:"repo_path"`
	Socket   string `json:"socket"`
	PID      int    `json:"pid"`
}

// vassalProc holds runtime state for a running claude vassal subprocess.
type vassalProc struct {
	process *os.Process
	pgid    int
}

// DelegationInfo tracks an active MCP session that has taken control of a vassal.
type DelegationInfo struct {
	SessionPID    int
	LastHeartbeat time.Time
}

// GuardResult is the outcome of a single guard health check.
type GuardResult struct {
	OK        bool
	Message   string
	CheckedAt time.Time
}

// GuardState tracks the runtime health of a single guard for a vassal.
type GuardState struct {
	VassalName       string
	GuardIndex       int
	GuardType        string
	ConsecutiveFails int
	LastCheckTime    time.Time
	LastResult       GuardResult
	CircuitOpen      bool // true = circuit breaker triggered; AI modifications blocked
}

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
	vassalPool        *VassalClientPool
	vassalProcs       map[string]*vassalProc // name → running king-vassal process
	vassalProcsMu     sync.RWMutex
	externalVassals   map[string]ExternalVassalInfo
	externalVassalsMu sync.RWMutex
	delegatedVassals  map[string]DelegationInfo
	delegationMu      sync.RWMutex
	guardStates       map[string]*GuardState
	guardStatesMu     sync.RWMutex
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
		rootDir:     absRoot,
		pidFile:     pidPathForRoot(absRoot),
		sockPath:    SocketPathForRoot(absRoot),
		logger:      logger,
		handlers:         make(map[string]rpcHandler),
		vassalProcs:      make(map[string]*vassalProc),
		externalVassals:  make(map[string]ExternalVassalInfo),
		delegatedVassals: make(map[string]DelegationInfo),
		guardStates:      make(map[string]*GuardState),
	}

	d.vassalPool = NewVassalClientPool()

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

	// Pre-inject serial contracts into config.Patterns before sieve is compiled (T-Hardware).
	// startVassals would do this too, but sieve must be created with the full pattern set.
	for _, vc := range d.config.Vassals {
		if vc.TypeOrDefault() != "serial" {
			continue
		}
		proto := vc.SerialProtocol
		if proto == "" {
			if pt := fingerprint.SerialProtocolForBaud(vc.BaudRate); pt != fingerprint.ProjectTypeUnknown {
				proto = string(pt)
			}
		}
		if proto != "" {
			autoContracts := fingerprint.DefaultContracts(fingerprint.ProjectType(proto), "")
			for i := range autoContracts {
				autoContracts[i].Source = vc.Name
			}
			d.config.Patterns = mergePatterns(d.config.Patterns, autoContracts)
		}
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

	// Launch king-vassal subprocesses for claude-type vassals.
	for _, v := range d.config.Vassals {
		if v.TypeOrDefault() == "claude" {
			if err := d.startClaudeVassal(v); err != nil {
				d.listener.Close()
				os.Remove(d.sockPath)
				d.removePIDFile()
				d.store.Close()
				return fmt.Errorf("start claude vassal %q: %w", v.Name, err)
			}
		}
	}

	// Create artifact ledger and MCP server.
	ledger := artifacts.NewLedgerWithSettings(d.store, d.kingdom.ID, d.config.Settings)
	adapter := &ptyManagerAdapter{mgr: d.ptyMgr}
	d.mcpSrv = mcp.NewServer(adapter, d.store, ledger, d.kingdom.ID, d.rootDir, d.logger.With("component", "mcp"))

	// Wire ApprovalManager into MCP server for respond_approval signaling.
	d.mcpSrv.SetApprovalManager(
		d.approvalMgr,
		d.config.Settings.SovereignApproval,
		d.config.Settings.SovereignApprovalTimeout,
	)

	// Wire VassalClientPool into MCP server for dispatch_task/get_task_status/abort_task.
	d.mcpSrv.SetVassalPool(&vassalPoolAdapter{pool: d.vassalPool})

	// Update RPC handlers to use real implementations.
	d.registerRealHandlers()

	// Start guard runners for all configured vassal guards.
	d.startGuardRunners(d.ctx)

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

	d.wg.Add(1)
	go d.runDelegationWarden()

	d.logger.Info("daemon started",
		"root", d.rootDir,
		"pid", os.Getpid(),
		"socket", d.sockPath,
		"kingdom_id", d.kingdom.ID,
	)

	return nil
}

// Attach initializes daemon state for MCP-only mode (no daemon socket, no vassal launching).
// Use this when a daemon is already running and we just need to serve MCP over stdio.
func (d *Daemon) Attach(ctx context.Context) error {
	d.ctx, d.cancel = context.WithCancel(ctx)

	if err := config.EnsureKingDir(d.rootDir); err != nil {
		return fmt.Errorf("ensure king dir: %w", err)
	}

	dbPath := filepath.Join(d.rootDir, kingDirName, dbFileName)
	s, err := store.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	d.store = s

	cfg, err := config.LoadOrCreateConfig(d.rootDir)
	if err != nil {
		d.store.Close()
		return fmt.Errorf("load config: %w", err)
	}
	d.config = cfg

	d.kingdom, err = LoadKingdom(d.store, d.rootDir, d.logger)
	if err != nil {
		d.store.Close()
		return fmt.Errorf("load kingdom: %w", err)
	}

	compiledPatterns, _ := events.CompilePatterns(d.config.Patterns)
	d.sieve = events.NewSieve(compiledPatterns, d.store, d.kingdom.ID,
		d.config.Settings.EventCooldownSeconds, d.logger.With("component", "sieve"))
	d.auditRecorder = audit.NewAuditRecorder(d.store, d.kingdom.ID, d.logger.With("component", "audit"))
	d.approvalMgr = audit.NewApprovalManager()

	d.ptyMgr = pty.NewManager(d.store, d.kingdom.ID, d.logger.With("component", "pty"))

	// Auto-connect to existing vassal sockets.
	vassalSockDir := filepath.Join(d.rootDir, kingDirName, "vassals")
	if entries, err := os.ReadDir(vassalSockDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sock") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".sock")
			sockPath := filepath.Join(vassalSockDir, e.Name())
			if _, err := d.vassalPool.Connect(name, sockPath); err != nil {
				d.logger.Warn("could not connect to vassal socket", "name", name, "err", err)
			}
		}
	}

	ledger := artifacts.NewLedgerWithSettings(d.store, d.kingdom.ID, d.config.Settings)
	adapter := &ptyManagerAdapter{mgr: d.ptyMgr}
	d.mcpSrv = mcp.NewServer(adapter, d.store, ledger, d.kingdom.ID, d.rootDir, d.logger.With("component", "mcp"))
	d.mcpSrv.SetApprovalManager(d.approvalMgr, d.config.Settings.SovereignApproval, d.config.Settings.SovereignApprovalTimeout)
	d.mcpSrv.SetVassalPool(&vassalPoolAdapter{pool: d.vassalPool})

	d.wg.Add(1)
	go d.auditCleanupLoop()

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

// mergePatterns appends newPatterns to existing, skipping any with duplicate Name.
func mergePatterns(existing, newPatterns []config.PatternConfig) []config.PatternConfig {
	seen := make(map[string]bool)
	for _, p := range existing {
		seen[p.Name] = true
	}
	for _, p := range newPatterns {
		if !seen[p.Name] {
			existing = append(existing, p)
			seen[p.Name] = true
		}
	}
	return existing
}

// expandSerialCommand converts a type:serial VassalConfig into a shell command
// that streams the serial port to stdout.
// For non-serial vassals it returns the original Command unchanged.
func expandSerialCommand(v config.VassalConfig) string {
	if v.Type != "serial" {
		return v.Command
	}
	baud := v.BaudRateOrDefault()
	port := v.SerialPort
	// macOS uses `stty -f`, Linux uses `stty -F`.
	sttyFlag := "-F"
	if runtime.GOOS == "darwin" {
		sttyFlag = "-f"
	}
	return fmt.Sprintf("stty %s %s %d raw -echo && cat %s", sttyFlag, port, baud, port)
}

// startVassals creates PTY sessions for all autostart shell-type vassals in config (T014).
// Claude-type vassals are managed separately by startClaudeVassal.
func (d *Daemon) startVassals() error {
	for _, vc := range d.config.Vassals {
		if vc.TypeOrDefault() == "claude" {
			continue // managed by startClaudeVassal
		}
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

		// Resolve auto-detect serial port.
		if strings.HasPrefix(vc.SerialPort, "auto:") {
			hint := strings.TrimPrefix(vc.SerialPort, "auto:")
			resolved, err := discovery.FindSerialPort(hint)
			if err != nil {
				d.logger.Warn("serial autodetect failed, skipping vassal",
					"name", vc.Name, "hint", hint, "err", err)
				continue
			}
			d.logger.Info("serial port resolved", "name", vc.Name, "hint", hint, "port", resolved)
			vc.SerialPort = resolved
		}

		id := uuid.New().String()
		sess, err := d.ptyMgr.CreateSession(id, vc.Name, expandSerialCommand(vc), cwd, vc.Env)
		if err != nil {
			d.logger.Error("failed to start vassal", "name", vc.Name, "err", err)
			continue
		}

		// Wire up Semantic Sieve to monitor vassal output (T031).
		// Also record ingestion audit entries when audit_ingestion is enabled.
		if d.sieve != nil {
			// Use sess.ID (the actual DB vassal ID) rather than the locally generated id,
			// which may differ if the vassal was reused from a previous daemon run.
			sieveCallback := d.sieve.OutputCallback(vc.Name, sess.ID)
			auditIngestion := d.config.Settings.AuditIngestion
			recorder := d.auditRecorder
			vassalName := vc.Name
			vassalID := sess.ID
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
				d.logger.Warn("VASSAL_FAILED", "name", name, "exit", "hung", "duration", "5m")
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
				ledger := artifacts.NewLedgerWithSettings(d.store, d.kingdom.ID, d.config.Settings)
				for _, art := range manifest.Artifacts {
					artPath := filepath.Join(repoPath, art.Path)
					if _, regErr := ledger.Register(art.Name, artPath, id, art.MimeType); regErr != nil {
						d.logger.Warn("failed to register VMP artifact", "name", art.Name, "err", regErr)
					}
				}
				d.logger.Info("VMP manifest loaded", "vassal", vc.Name, "skills", len(manifest.Skills), "artifacts", len(manifest.Artifacts))
			}
			// Auto-Integrity: inject contracts based on project type.
			d.injectAutoContracts(repoPath)
		}

		// Auto-detect serial protocol and inject contracts by baud rate (T005).
		if vc.TypeOrDefault() == "serial" {
			proto := vc.SerialProtocol
			if proto == "" {
				pt := fingerprint.SerialProtocolForBaud(vc.BaudRate)
				if pt != fingerprint.ProjectTypeUnknown {
					proto = string(pt)
				}
			}
			if proto != "" {
				autoContracts := fingerprint.DefaultContracts(fingerprint.ProjectType(proto), "")
				for i := range autoContracts {
					autoContracts[i].Source = vc.Name
				}
				d.config.Patterns = mergePatterns(d.config.Patterns, autoContracts)
			}
		}

		d.logger.Info("vassal started", "name", vc.Name)
	}
	return nil
}

// NextBackoff returns the next exponential backoff duration, capped at vassalRestartMaxBackoff.
func NextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > vassalRestartMaxBackoff {
		return vassalRestartMaxBackoff
	}
	return next
}

// launchClaudeVassal starts a king-vassal subprocess, waits for its socket,
// connects the vassal pool client, and returns the running cmd.
func (d *Daemon) launchClaudeVassal(v config.VassalConfig) (*exec.Cmd, error) {
	exe, err := resolveKingVassalBinary()
	if err != nil {
		return nil, fmt.Errorf("resolve king-vassal binary: %w", err)
	}

	kingDir := filepath.Join(d.rootDir, kingDirName)
	sockPath := filepath.Join(kingDir, "vassals", v.Name+".sock")

	if v.RepoPath != "" {
		repoPath := v.RepoPath
		if !filepath.IsAbs(repoPath) {
			repoPath = filepath.Join(d.rootDir, repoPath)
		}
		d.injectAutoContracts(repoPath)
	}

	// Resolve model: vassal-specific > kingdom default > (empty = claude default).
	model := v.Model
	if model == "" && d.config != nil {
		model = d.config.Settings.DefaultModel
	}
	args := []string{
		"--name", v.Name,
		"--repo", v.RepoPath,
		"--king-dir", kingDir,
		"--king-sock", d.sockPath,
		"--timeout", "10",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	// Disconnect any stale pool entry and remove stale socket so the poll loop
	// below waits for the fresh one (handles daemon restarts and vassal crashes).
	_ = d.vassalPool.Disconnect(v.Name)
	_ = os.Remove(sockPath)

	cmd := exec.Command(exe, args...)
	cmd.Dir = d.rootDir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start king-vassal %q: %w", v.Name, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			return nil, fmt.Errorf("king-vassal %q did not start within 3s", v.Name)
		}
		select {
		case <-d.ctx.Done():
			cmd.Process.Kill()
			return nil, fmt.Errorf("king-vassal %q: context cancelled", v.Name)
		case <-time.After(100 * time.Millisecond):
		}
	}

	if _, err := d.vassalPool.Connect(v.Name, sockPath); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("connect to king-vassal %q: %w", v.Name, err)
	}

	return cmd, nil
}

// startClaudeVassal launches a vassal and starts the watch/restart goroutine.
func (d *Daemon) startClaudeVassal(v config.VassalConfig) error {
	cmd, err := d.launchClaudeVassal(v)
	if err != nil {
		return err
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		d.logger.Warn("could not get vassal pgid, process group kill unavailable", "name", v.Name, "err", err)
	}
	d.vassalProcsMu.Lock()
	d.vassalProcs[v.Name] = &vassalProc{
		process: cmd.Process,
		pgid:    pgid,
	}
	d.vassalProcsMu.Unlock()

	d.wg.Add(1)
	go d.watchVassal(v.Name, cmd, v)

	d.logger.Info("claude vassal started", "name", v.Name)
	return nil
}

// watchVassal waits for a vassal process to exit and restarts it with
// exponential backoff unless the daemon is shutting down or restart_policy is "no".
func (d *Daemon) watchVassal(name string, cmd *exec.Cmd, cfg config.VassalConfig) {
	defer d.wg.Done()

	backoff := vassalRestartInitialBackoff

	for {
		_ = cmd.Wait()

		if d.ctx.Err() != nil {
			return
		}

		// If an MCP session has taken control of this vassal, don't restart it.
		if d.isDelegated(name) {
			d.logger.Info("VASSAL_DELEGATED_EXIT",
				"vassal", name,
				"msg", "process exited while delegated; awaiting MCP session or warden",
			)
			// Wait until delegation is released or daemon shuts down.
			for d.isDelegated(name) {
				select {
				case <-d.ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
			// Delegation released — fall through to normal restart logic.
		}

		policy := cfg.RestartPolicy
		if policy == "" {
			policy = "always"
		}
		if policy == "no" {
			d.logger.Info("vassal exited, restart disabled", "name", name)
			return
		}

		d.logger.Info("vassal exited, restarting", "name", name, "backoff", backoff)

		select {
		case <-time.After(backoff):
		case <-d.ctx.Done():
			return
		}

		backoff = NextBackoff(backoff)

		newCmd, err := d.launchClaudeVassal(cfg)
		if err != nil {
			d.logger.Error("failed to restart vassal", "name", name, "err", err)
			continue
		}
		// Reset backoff on successful launch
		backoff = vassalRestartInitialBackoff

		pgid, err := syscall.Getpgid(newCmd.Process.Pid)
		if err != nil {
			d.logger.Warn("could not get vassal pgid after restart", "name", name, "err", err)
		}
		d.vassalProcsMu.Lock()
		d.vassalProcs[name] = &vassalProc{
			process: newCmd.Process,
			pgid:    pgid,
		}
		d.vassalProcsMu.Unlock()
		cmd = newCmd
	}
}

// resolveKingVassalBinary finds the king-vassal binary by checking PATH first,
// then falling back to the same directory as the current executable.
func resolveKingVassalBinary() (string, error) {
	// Try PATH first.
	if path, err := exec.LookPath("king-vassal"); err == nil {
		return path, nil
	}
	// Fall back to same directory as the current executable.
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(filepath.Dir(exe), "king-vassal")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("king-vassal not found in PATH or alongside %s", exe)
}

// Done returns a channel that is closed when the daemon context is cancelled
// (i.e. when the shutdown RPC fires d.cancel()). Callers can wait on this to
// know that a graceful Stop() should follow.
func (d *Daemon) Done() <-chan struct{} {
	return d.ctx.Done()
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

	// Stop claude vassals.
	if d.vassalPool != nil {
		d.vassalPool.DisconnectAll()
	}
	d.vassalProcsMu.RLock()
	vassalSnapshot := make([]*vassalProc, 0, len(d.vassalProcs))
	vassalNames := make([]string, 0, len(d.vassalProcs))
	for name, vp := range d.vassalProcs {
		vassalSnapshot = append(vassalSnapshot, vp)
		vassalNames = append(vassalNames, name)
	}
	d.vassalProcsMu.RUnlock()

	for i, vp := range vassalSnapshot {
		d.logger.Info("stopping claude vassal", "name", vassalNames[i])
		if vp.pgid > 0 {
			_ = syscall.Kill(-vp.pgid, syscall.SIGTERM)
		} else {
			_ = vp.process.Signal(syscall.SIGTERM)
		}
	}
	// Wait up to 3s for all vassals to exit, then SIGKILL survivors.
	if len(vassalSnapshot) > 0 {
		deadline := time.After(3 * time.Second)
		<-deadline
		for _, vp := range vassalSnapshot {
			if vp.pgid > 0 {
				_ = syscall.Kill(-vp.pgid, syscall.SIGKILL)
			} else {
				_ = vp.process.Kill()
			}
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

// ServeConn handles a single daemon RPC connection. Exported for testing.
func (d *Daemon) ServeConn(conn net.Conn) {
	d.wg.Add(1)
	d.handleConnection(conn)
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

// vassalPoolAdapter bridges *VassalClientPool to the mcp.VassalPool interface.
type vassalPoolAdapter struct {
	pool *VassalClientPool
}

func (a *vassalPoolAdapter) Get(name string) (mcp.VassalCaller, bool) {
	vc, ok := a.pool.Get(name)
	if !ok {
		return nil, false
	}
	return vc, true
}

func (a *vassalPoolAdapter) Names() []string {
	return a.pool.Names()
}

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

	// kingdom.status — returns kingdom identity and runtime info.
	d.handlers["kingdom.status"] = func(_ json.RawMessage) (interface{}, error) {
		return struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Root   string `json:"root"`
			PID    int    `json:"pid"`
			Status string `json:"status"`
		}{
			ID:     d.kingdom.ID,
			Name:   d.config.Name,
			Root:   d.rootDir,
			PID:    os.Getpid(),
			Status: d.kingdom.GetStatus(),
		}, nil
	}

	registerDelegationHandlers(d)
	registerGuardHandlers(d)
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

	// vassal.register and vassal.list are available even before full daemon start
	// because they only depend on externalVassals which is initialised in NewDaemon.
	d.handlers["vassal.register"] = func(params json.RawMessage) (interface{}, error) {
		var info ExternalVassalInfo
		if err := json.Unmarshal(params, &info); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		if info.Name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if info.PID <= 0 {
			return nil, fmt.Errorf("pid must be positive")
		}
		// Reject control characters and ANSI escapes that could inject terminal sequences.
		for _, field := range []string{info.Socket, info.RepoPath} {
			for _, r := range field {
				if r < 0x20 || r == 0x7f {
					return nil, fmt.Errorf("invalid characters in socket or repo_path")
				}
			}
		}
		d.externalVassalsMu.Lock()
		d.externalVassals[info.Name] = info
		d.externalVassalsMu.Unlock()
		d.logger.Info("external vassal registered", "name", info.Name, "repo", info.RepoPath)
		return map[string]bool{"ok": true}, nil
	}

	d.handlers["vassal.list"] = func(_ json.RawMessage) (interface{}, error) {
		// Snapshot the map under read lock.
		d.externalVassalsMu.RLock()
		snapshot := make([]ExternalVassalInfo, 0, len(d.externalVassals))
		for _, v := range d.externalVassals {
			snapshot = append(snapshot, v)
		}
		d.externalVassalsMu.RUnlock()

		// Perform liveness checks outside the lock.
		type vassalEntry struct {
			Name          string `json:"name"`
			RepoPath      string `json:"repo_path"`
			Socket        string `json:"socket"`
			PID           int    `json:"pid"`
			Alive         bool   `json:"alive"`
			Delegated     bool   `json:"delegated"`
			ControllerPID int    `json:"controller_pid"`
			HeartbeatAgeS int    `json:"heartbeat_age_s"`
		}
		result := make([]vassalEntry, 0, len(snapshot))
		for _, v := range snapshot {
			alive := false
			if v.PID > 0 {
				if proc, err := os.FindProcess(v.PID); err == nil {
					alive = proc.Signal(syscall.Signal(0)) == nil
				}
			}
			controllerPID := func() int {
				d.delegationMu.RLock()
				defer d.delegationMu.RUnlock()
				if info, ok := d.delegatedVassals[v.Name]; ok {
					return info.SessionPID
				}
				return 0
			}()
			heartbeatAgeS := func() int {
				d.delegationMu.RLock()
				defer d.delegationMu.RUnlock()
				if info, ok := d.delegatedVassals[v.Name]; ok {
					return int(time.Since(info.LastHeartbeat).Seconds())
				}
				return 0
			}()
			result = append(result, vassalEntry{
				Name: v.Name, RepoPath: v.RepoPath,
				Socket: v.Socket, PID: v.PID, Alive: alive,
				Delegated:     d.isDelegated(v.Name),
				ControllerPID: controllerPID,
				HeartbeatAgeS: heartbeatAgeS,
			})
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
		return map[string]interface{}{"vassals": result}, nil
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

// ---------------------------------------------------------------------------
// Delegation helpers
// ---------------------------------------------------------------------------

// isDelegated reports whether the named vassal is currently under MCP session control.
func (d *Daemon) isDelegated(vassal string) bool {
	d.delegationMu.RLock()
	defer d.delegationMu.RUnlock()
	_, ok := d.delegatedVassals[vassal]
	return ok
}

// setDelegation marks a vassal as delegated to an external session.
func (d *Daemon) setDelegation(vassal string, pid int) {
	d.delegationMu.Lock()
	defer d.delegationMu.Unlock()
	d.delegatedVassals[vassal] = DelegationInfo{
		SessionPID:    pid,
		LastHeartbeat: timeNow(),
	}
}

// updateHeartbeat refreshes the LastHeartbeat for a delegated vassal.
// Returns false if the vassal is not currently delegated (daemon restarted / unknown).
func (d *Daemon) updateHeartbeat(vassal string, pid int) bool {
	d.delegationMu.Lock()
	defer d.delegationMu.Unlock()
	info, ok := d.delegatedVassals[vassal]
	if !ok || info.SessionPID != pid {
		return false
	}
	info.LastHeartbeat = timeNow()
	d.delegatedVassals[vassal] = info
	return true
}

// releaseDelegation removes a vassal from delegated control.
func (d *Daemon) releaseDelegation(vassal string) {
	d.delegationMu.Lock()
	defer d.delegationMu.Unlock()
	delete(d.delegatedVassals, vassal)
}
