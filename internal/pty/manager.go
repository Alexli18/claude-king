package pty

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexli18/claude-king/internal/store"
)

// Manager coordinates multiple PTY sessions.
type Manager struct {
	sessions map[string]*Session
	order    []string // creation order for reverse teardown
	mu       sync.RWMutex
	store    *store.Store
	kingdom  string // kingdom ID for store operations
	logger   *slog.Logger
}

// NewManager creates a new PTY session manager.
func NewManager(s *store.Store, kingdomID string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		sessions: make(map[string]*Session),
		store:    s,
		kingdom:  kingdomID,
		logger:   logger,
	}
}

// CreateSession creates and starts a new PTY session. It persists the vassal
// to the store and tracks the session by name.
func (m *Manager) CreateSession(id, name, command, cwd string, env map[string]string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[name]; exists {
		return nil, fmt.Errorf("session %q already exists", name)
	}

	session, err := NewSession(id, name, command, cwd, env)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Persist the vassal to the store. If an entry with the same kingdom+name
	// already exists (e.g., from a previous daemon run), reuse it.
	now := time.Now().UTC().Format(time.DateTime)
	existing, _ := m.store.GetVassalByName(m.kingdom, name)
	if existing != nil {
		id = existing.ID
		session.ID = id
		_ = m.store.UpdateVassalStatus(id, StatusIdle)
	} else {
		if err := m.store.CreateVassal(store.Vassal{
			ID:           id,
			KingdomID:    m.kingdom,
			Name:         name,
			Command:      command,
			Status:       StatusIdle,
			CreatedAt:    now,
			LastActivity: now,
		}); err != nil {
			return nil, fmt.Errorf("persist vassal: %w", err)
		}
	}

	if err := session.Start(); err != nil {
		// Clean up the store entry on start failure.
		_ = m.store.DeleteVassal(id)
		if isPTYExhausted(err) {
			return nil, fmt.Errorf("PTY_EXHAUSTED: no available PTY slots. Close some sessions and retry")
		}
		return nil, fmt.Errorf("start session: %w", err)
	}

	// Use GetPID() to avoid a data race with waitLoop which zeroes PID under
	// the session mutex when the process exits.
	pid := session.GetPID()

	// Update the store with PID and running status.
	if err := m.store.UpdateVassalPID(id, pid); err != nil {
		m.logger.Warn("failed to update vassal PID in store",
			slog.String("name", name),
			slog.Int("pid", pid),
			slog.String("error", err.Error()),
		)
	}
	if err := m.store.UpdateVassalStatus(id, StatusRunning); err != nil {
		m.logger.Warn("failed to update vassal status to running in store",
			slog.String("name", name),
			slog.String("error", err.Error()),
		)
	}

	m.sessions[name] = session
	m.order = append(m.order, name)

	m.logger.Info("session created",
		slog.String("name", name),
		slog.String("command", command),
		slog.Int("pid", pid),
	)

	// Monitor session exit in the background and update store.
	go m.monitorSession(session)

	return session, nil
}

// monitorSession waits for a session to exit and updates the store status.
func (m *Manager) monitorSession(s *Session) {
	startedAt := time.Now()
	_ = s.Wait()
	duration := time.Since(startedAt)

	s.mu.RLock()
	status := s.Status
	cmd := s.cmd
	s.mu.RUnlock()

	// Extract the integer exit code from the process state.
	exitCode := 0
	if cmd != nil && cmd.ProcessState != nil {
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			exitCode = ws.ExitStatus()
		} else {
			exitCode = cmd.ProcessState.ExitCode()
		}
	}

	if err := m.store.UpdateVassalStatus(s.ID, status); err != nil {
		m.logger.Error("failed to update vassal status",
			slog.String("name", s.Name),
			slog.String("error", err.Error()),
		)
	}
	if err := m.store.UpdateVassalPID(s.ID, 0); err != nil {
		m.logger.Warn("failed to clear vassal PID in store",
			slog.String("name", s.Name),
			slog.String("error", err.Error()),
		)
	}

	if exitCode != 0 {
		m.logger.Warn("VASSAL_FAILED",
			"name", s.Name,
			"exit", exitCode,
			"duration", duration.String(),
		)
	} else {
		m.logger.Info("session exited",
			slog.String("name", s.Name),
			slog.String("status", status),
		)
	}
}

// GetSessionBytesWritten returns total bytes received from the named session's
// PTY output, or 0 if the session does not exist. Used by the data_rate guard.
func (m *Manager) GetSessionBytesWritten(name string) int64 {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return s.BytesWritten()
}

// GetSessionRecentLines returns output lines received since the given time for
// the named session, or nil if the session does not exist. Used by the log_watch guard.
func (m *Manager) GetSessionRecentLines(name string, since time.Time) []string {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return s.RecentOutputLines(since)
}

// GetSession returns a session by name.
func (m *Manager) GetSession(name string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[name]
	return s, ok
}

// ListSessions returns all tracked sessions.
func (m *Manager) ListSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Session, 0, len(m.sessions))
	for _, name := range m.order {
		if s, ok := m.sessions[name]; ok {
			result = append(result, s)
		}
	}
	return result
}

// StopSession stops a session by name.
func (m *Manager) StopSession(name string) error {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %q not found", name)
	}

	m.logger.Info("stopping session", slog.String("name", name))

	if err := s.Stop(); err != nil {
		return fmt.Errorf("stop session %q: %w", name, err)
	}

	m.logger.Info("session stopped", slog.String("name", name))
	return nil
}

// StopAll stops all sessions in reverse creation order.
func (m *Manager) StopAll() error {
	m.mu.RLock()
	// Copy order slice in reverse.
	names := make([]string, len(m.order))
	for i, name := range m.order {
		names[len(m.order)-1-i] = name
	}
	m.mu.RUnlock()

	var firstErr error
	for _, name := range names {
		m.logger.Info("stopping session (shutdown)", slog.String("name", name))
		if err := m.StopSession(name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// RecoverSessions checks the store for vassals that were previously running
// and attempts to detect if their processes are still alive. If alive,
// log that they exist but cannot be reattached (PTY fd is lost after restart).
// If dead, update their status to "terminated" in the store. (T045)
func (m *Manager) RecoverSessions() error {
	vassals, err := m.store.ListVassals(m.kingdom)
	if err != nil {
		return fmt.Errorf("list vassals for recovery: %w", err)
	}

	for _, v := range vassals {
		if v.Status != StatusRunning && v.Status != StatusIdle {
			continue
		}

		if v.PID <= 0 {
			// No PID recorded; mark as terminated.
			m.logger.Info("recovering vassal with no PID, marking terminated",
				slog.String("name", v.Name),
				slog.String("id", v.ID),
			)
			_ = m.store.UpdateVassalStatus(v.ID, StatusTerminated)
			_ = m.store.UpdateVassalPID(v.ID, 0)
			continue
		}

		// Check if the process is still alive.
		proc, findErr := os.FindProcess(v.PID)
		alive := false
		if findErr == nil {
			alive = proc.Signal(syscall.Signal(0)) == nil
		}

		if alive {
			// Process still alive but we cannot reattach to its PTY fd.
			m.logger.Warn("vassal process still alive but PTY cannot be reattached",
				slog.String("name", v.Name),
				slog.Int("pid", v.PID),
				slog.String("id", v.ID),
			)
			// Mark as terminated since we lost the PTY connection.
			_ = m.store.UpdateVassalStatus(v.ID, StatusTerminated)
			_ = m.store.UpdateVassalPID(v.ID, 0)
		} else {
			// Process is dead; update store.
			m.logger.Info("recovered dead vassal, marking terminated",
				slog.String("name", v.Name),
				slog.Int("pid", v.PID),
				slog.String("id", v.ID),
			)
			_ = m.store.UpdateVassalStatus(v.ID, StatusTerminated)
			_ = m.store.UpdateVassalPID(v.ID, 0)
		}
	}

	return nil
}

// isPTYExhausted checks if an error indicates PTY slot exhaustion.
// This can manifest as ENOMEM, EAGAIN, or ENOENT on /dev/ptmx.
func isPTYExhausted(err error) bool {
	if err == nil {
		return false
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ENOMEM, syscall.EAGAIN, syscall.ENOSPC:
			return true
		case syscall.ENOENT:
			// ENOENT on /dev/ptmx indicates no PTY devices available.
			return true
		}
	}

	// Check error message for common PTY exhaustion indicators.
	msg := err.Error()
	if strings.Contains(msg, "/dev/ptmx") || strings.Contains(msg, "out of pty") {
		return true
	}

	return false
}

// SetOnOutput sets the output callback for a session (for Semantic Sieve integration).
func (m *Manager) SetOnOutput(name string, fn func(string)) error {
	m.mu.RLock()
	s, ok := m.sessions[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %q not found", name)
	}

	s.SetOnOutput(fn)
	return nil
}
