package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alexli18/claude-king/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ArtifactLedger is an interface for artifact registration and resolution,
// defined here to avoid an import cycle between the mcp and artifacts packages.
type ArtifactLedger interface {
	Register(name, filePath, producerID, mimeType string) (*store.Artifact, error)
	Resolve(name string) (*store.Artifact, error)
}

// PTYManager is an interface to the pty.Manager, defined here to avoid an
// import cycle between the mcp and pty packages.
type PTYManager interface {
	GetSession(name string) (PTYSession, bool)
	ListSessions() []PTYSessionInfo
}

// PTYSession represents a single PTY session that can be written to and read from.
type PTYSession interface {
	Write(data []byte) (int, error)
	GetOutput() []byte
	ExecCommand(command string, timeout time.Duration) (output string, exitCode int, duration time.Duration, err error)
}

// PTYSessionInfo holds summary information about a PTY session.
type PTYSessionInfo struct {
	Name         string
	Status       string
	Command      string
	PID          int
	LastActivity string
}

// AuditStore provides audit-related query methods, defined here to avoid
// import cycles between mcp and audit packages.
type AuditStore interface {
	ListAuditEntries(f store.AuditFilter) ([]store.AuditEntry, error)
	CountAuditEntries(f store.AuditFilter) (int, error)
	GetActionTrace(traceID string) (*store.ActionTrace, error)
	ListPendingApprovals(kingdomID string) ([]store.ApprovalRequest, error)
	GetApprovalRequest(id string) (*store.ApprovalRequest, error)
	GetApprovalRequestByTraceID(traceID string) (*store.ApprovalRequest, error)
	UpdateApprovalRequest(id, status, respondedAt string) error
}

// ApprovalManager provides sovereign approval gating.
// Defined here as an interface to avoid import cycles between mcp and audit packages.
type ApprovalManager interface {
	// Request registers a pending approval and returns a channel to block on.
	Request(requestID string) <-chan bool
	// Respond delivers the approval decision, unblocking the waiting exec_in.
	Respond(requestID string, approved bool) error
	// Cancel removes a pending request without delivering a response.
	// Returns true if the entry existed (was not already responded to).
	Cancel(requestID string) bool
}

// VassalCaller is the interface for calling a tool on a specific vassal.
// It is implemented by *daemon.VassalClient and defined here to avoid
// an import cycle between mcp and daemon packages.
type VassalCaller interface {
	CallTool(ctx context.Context, toolName string, args map[string]any) (string, error)
}

// VassalPool is the interface for looking up a connected vassal client by name.
// It is implemented by *daemon.VassalClientPool and defined here to avoid
// an import cycle between mcp and daemon packages.
type VassalPool interface {
	Get(name string) (VassalCaller, bool)
}

// Server wraps the mcp-go MCPServer and holds references to internal services
// needed by tool handlers.
type Server struct {
	mcpServer   *server.MCPServer
	ptyMgr      PTYManager
	store       *store.Store
	ledger      ArtifactLedger
	approvalMgr ApprovalManager // may be nil when sovereign_approval is disabled
	vassalPool  VassalPool      // may be nil before SetVassalPool is called
	kingdomID   string
	rootDir     string
	logger      *slog.Logger

	// Sovereign approval config (set via SetApprovalConfig).
	sovereignApproval        bool
	sovereignApprovalTimeout int // seconds

	parentKingdomSocket string // non-empty if this MCP runs inside another kingdom
	inObserverMode      bool

	activeHeartbeats map[string]context.CancelFunc
	heartbeatMu      sync.Mutex
}

// NewServer creates a new MCP Server with the given dependencies.
func NewServer(ptyMgr PTYManager, s *store.Store, ledger ArtifactLedger, kingdomID, rootDir string, logger *slog.Logger) *Server {
	mcpSrv := server.NewMCPServer(
		"claude-king",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	srv := &Server{
		mcpServer:        mcpSrv,
		ptyMgr:           ptyMgr,
		store:            s,
		ledger:           ledger,
		kingdomID:        kingdomID,
		rootDir:          rootDir,
		logger:           logger,
		activeHeartbeats: make(map[string]context.CancelFunc),
	}

	srv.RegisterTools()

	// Detect if running inside a parent kingdom (enables observer/delegate mode)
	if homeDir, err := os.UserHomeDir(); err == nil {
		regPath := filepath.Join(homeDir, ".king", "registry.json")
		srv.detectParentKingdom(regPath)
	}

	return srv
}

// SetApprovalManager wires an ApprovalManager into the server.
// Call this after creation when sovereign_approval is enabled.
func (s *Server) SetApprovalManager(mgr ApprovalManager, sovereignApproval bool, timeoutSec int) {
	s.approvalMgr = mgr
	s.sovereignApproval = sovereignApproval
	s.sovereignApprovalTimeout = timeoutSec
}

// SetVassalPool wires a VassalPool into the server so that dispatch_task,
// get_task_status, and abort_task can proxy calls to vassal MCP servers.
// Call this after creation when vassal pool is available.
func (s *Server) SetVassalPool(pool VassalPool) {
	s.vassalPool = pool
}

// Start starts the MCP server on the stdio transport. It blocks until the
// transport is closed or an error occurs.
func (s *Server) Start() error {
	s.logger.Info("starting MCP server on stdio", "kingdom", s.kingdomID)
	if err := server.ServeStdio(s.mcpServer); err != nil {
		return fmt.Errorf("mcp stdio server: %w", err)
	}
	return nil
}

// RegisterTools registers all Scepter Tools with the underlying MCPServer.
func (s *Server) RegisterTools() {
	s.registerListVassals()
	s.registerExecIn()
	s.registerGetEvents()
	s.registerRegisterArtifact()
	s.registerResolveArtifact()
	s.registerReadNeighbor()
	s.registerGetAuditLog()
	s.registerGetActionTrace()
	s.registerRespondApproval()
	s.registerDispatchTask()
	s.registerGetTaskStatus()
	s.registerAbortTask()
	s.registerGetSerialEvents()
	s.registerDelegateControl()
	s.registerDelegateRelease()
	s.registerDelegateStatus()
}

// registerListVassals registers the list_vassals tool.
func (s *Server) registerListVassals() {
	tool := mcp.NewTool("list_vassals",
		mcp.WithDescription("List all vassal processes and their current status within the kingdom"),
	)
	s.mcpServer.AddTool(tool, s.handleListVassals)
}

// registerExecIn registers a stub for the exec_in tool (Phase 4).
func (s *Server) registerExecIn() {
	tool := mcp.NewTool("exec_in",
		mcp.WithDescription("Execute a command inside a vassal's PTY session"),
		mcp.WithString("vassal",
			mcp.Required(),
			mcp.Description("Name of the vassal to execute in"),
		),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("Command string to send to the vassal's PTY"),
		),
		mcp.WithNumber("timeout_seconds",
			mcp.Description("Timeout in seconds (default: 30)"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleExecIn)
}

// registerGetEvents registers a stub for the get_events tool (Phase 5).
func (s *Server) registerGetEvents() {
	tool := mcp.NewTool("get_events",
		mcp.WithDescription("Retrieve recent events from the kingdom event log"),
		mcp.WithString("severity",
			mcp.Description("Filter by severity level (info, warn, error)"),
		),
		mcp.WithString("source",
			mcp.Description("Filter by source vassal name"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of events to return"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleGetEvents)
}

// registerRegisterArtifact registers a stub for the register_artifact tool (Phase 6).
func (s *Server) registerRegisterArtifact() {
	tool := mcp.NewTool("register_artifact",
		mcp.WithDescription("Register a file-based artifact produced by a vassal"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Unique name for the artifact"),
		),
		mcp.WithString("file_path",
			mcp.Required(),
			mcp.Description("Path to the artifact file"),
		),
		mcp.WithString("producer",
			mcp.Description("Name of the producing vassal"),
		),
		mcp.WithString("mime_type",
			mcp.Description("MIME type of the artifact"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleRegisterArtifact)
}

// registerResolveArtifact registers a stub for the resolve_artifact tool (Phase 6).
func (s *Server) registerResolveArtifact() {
	tool := mcp.NewTool("resolve_artifact",
		mcp.WithDescription("Resolve an artifact name to its file path and metadata"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the artifact to resolve"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleResolveArtifact)
}

// registerGetAuditLog registers the get_audit_log tool.
func (s *Server) registerGetAuditLog() {
	tool := mcp.NewTool("get_audit_log",
		mcp.WithDescription("Retrieve audit entries with optional filters for layer, vassal, time range, and trace ID"),
		mcp.WithString("layer",
			mcp.Description("Filter by layer: ingestion, sieve, or action"),
		),
		mcp.WithString("vassal",
			mcp.Description("Filter by vassal name"),
		),
		mcp.WithString("since",
			mcp.Description("Start time (RFC3339 or relative: 5m, 1h, 1d)"),
		),
		mcp.WithString("until",
			mcp.Description("End time (RFC3339 or relative)"),
		),
		mcp.WithString("trace_id",
			mcp.Description("Filter by specific Trace ID"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max entries to return (1-500, default: 50)"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleGetAuditLog)
}

// registerGetActionTrace registers the get_action_trace tool.
func (s *Server) registerGetActionTrace() {
	tool := mcp.NewTool("get_action_trace",
		mcp.WithDescription("Retrieve detailed Action Trace for a specific exec_in execution"),
		mcp.WithString("trace_id",
			mcp.Required(),
			mcp.Description("Trace ID of the action"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleGetActionTrace)
}

// registerRespondApproval registers the respond_approval tool.
func (s *Server) registerRespondApproval() {
	tool := mcp.NewTool("respond_approval",
		mcp.WithDescription("Respond to a pending Sovereign Approval request"),
		mcp.WithString("request_id",
			mcp.Required(),
			mcp.Description("ID of the approval request"),
		),
		mcp.WithBoolean("approved",
			mcp.Required(),
			mcp.Description("true to approve, false to reject"),
		),
		mcp.WithString("reason",
			mcp.Description("Optional reason for rejection"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleRespondApproval)
}

// registerReadNeighbor registers the read_neighbor tool.
func (s *Server) registerReadNeighbor() {
	tool := mcp.NewTool("read_neighbor",
		mcp.WithDescription("Read a file from a neighbor vassal's working directory"),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path to the file to read"),
		),
		mcp.WithNumber("max_lines",
			mcp.Description("Maximum number of lines to return (default: 200)"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleReadNeighbor)
}

// registerDispatchTask registers the dispatch_task tool.
func (s *Server) registerDispatchTask() {
	tool := mcp.NewTool("dispatch_task",
		mcp.WithDescription("Dispatch a task to a claude vassal and return the task ID"),
		mcp.WithString("vassal",
			mcp.Required(),
			mcp.Description("Name of the vassal to dispatch the task to"),
		),
		mcp.WithString("task",
			mcp.Required(),
			mcp.Description("Task description to send to the vassal"),
		),
		mcp.WithObject("context",
			mcp.Description("Optional context: {artifacts: [...], notes: \"...\"}"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleDispatchTask)
}

// registerGetTaskStatus registers the get_task_status tool.
func (s *Server) registerGetTaskStatus() {
	tool := mcp.NewTool("get_task_status",
		mcp.WithDescription("Get the status of a task running on a vassal"),
		mcp.WithString("vassal",
			mcp.Required(),
			mcp.Description("Name of the vassal that owns the task"),
		),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description("ID of the task to query"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleGetTaskStatus)
}

// registerAbortTask registers the abort_task tool.
func (s *Server) registerAbortTask() {
	tool := mcp.NewTool("abort_task",
		mcp.WithDescription("Abort a running task on a vassal"),
		mcp.WithString("vassal",
			mcp.Required(),
			mcp.Description("Name of the vassal that owns the task"),
		),
		mcp.WithString("task_id",
			mcp.Required(),
			mcp.Description("ID of the task to abort"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleAbortTask)
}

// registerGetSerialEvents registers the get_serial_events tool.
func (s *Server) registerGetSerialEvents() {
	tool := mcp.NewTool("get_serial_events",
		mcp.WithDescription("Get recent events from a serial vassal, filtered by time window and severity"),
		mcp.WithString("vassal",
			mcp.Required(),
			mcp.Description("Vassal name (must have type: serial)"),
		),
		mcp.WithString("since",
			mcp.Description("Duration window: '5m', '1h', '30s'. Default: '1h'"),
		),
		mcp.WithString("severity",
			mcp.Description("Filter: 'warning', 'error', 'critical', or '' for all"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleGetSerialEvents)
}
