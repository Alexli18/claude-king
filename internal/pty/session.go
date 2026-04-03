package pty

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	ptylib "github.com/creack/pty/v2"
)

// timedLine holds a single output line with its arrival timestamp.
type timedLine struct {
	t    time.Time
	text string
}

// maxRecentLines is the maximum number of recent lines retained per session.
const maxRecentLines = 1000

// ansiRegex strips ANSI/VT100 escape sequences from terminal output.
var ansiRegex = regexp.MustCompile(
	`\x1b\[[0-9;?]*[a-zA-Z]` + // CSI sequences: ESC [ ... letter
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC sequences: ESC ] ... BEL or ST
		`|\x1b[=>]` + // Keypad mode
		`|\x1b\(B` + // Character set
		`|\x07` + // BEL
		`|\r` + // Carriage return
		`|\x08.?`, // Backspace + char
)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// promptIndicators are substrings commonly found in shell prompt lines.
var promptIndicators = []string{
	"➜", "❯", "❮", "▶", "λ", // common prompt chars
	"$ ", "# ", // basic prompts
}

// isPromptLine returns true if the line looks like a shell prompt rather than command output.
func isPromptLine(line string) bool {
	// Short lines starting with % are zsh trailing-newline markers.
	if strings.HasPrefix(line, "%") && len(line) > 20 {
		return true
	}
	for _, ind := range promptIndicators {
		if strings.Contains(line, ind) {
			return true
		}
	}
	return false
}

// defaultRingSize is the default output ring buffer size (10 MB).
const defaultRingSize = 10 * 1024 * 1024

// Session status constants.
const (
	StatusIdle       = "idle"
	StatusRunning    = "running"
	StatusError      = "error"
	StatusTerminated = "terminated"
)

// CommandResult holds the result of a command execution.
type CommandResult struct {
	Output   string
	ExitCode int
	Duration time.Duration
	TraceID  string
	Err      error
}

// QueueStatus represents the current queue state for a session.
type QueueStatus struct {
	Position int
	Total    int
}

// execRequest represents a pending command execution request on a session.
type execRequest struct {
	command  string
	timeout  time.Duration
	resultCh chan CommandResult
}

// Session wraps a process running in a pseudo-terminal.
type Session struct {
	ID      string
	Name    string
	Command string
	PID     int
	Status  string

	cwd string
	env map[string]string

	cmd       *exec.Cmd
	ptmx      *os.File
	outputBuf *RingBuffer
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	onOutput  func(line string) // callback for output monitoring (Semantic Sieve)

	// Command execution queue for serializing concurrent ExecCommand calls.
	execQueue chan execRequest
	// outputSubscribers allows the readLoop to fan out lines to waiting ExecCommand calls.
	outputSubs   map[string]chan string
	outputSubsMu sync.Mutex

	// lastOutput tracks the time of the most recent output for hang detection (T058).
	lastOutput   time.Time
	lastOutputMu sync.Mutex

	// bytesWritten counts total bytes received from the PTY (for data_rate guard).
	bytesWritten atomic.Int64

	// recentLines holds up to maxRecentLines timestamped output lines (for log_watch guard).
	recentLines   []timedLine
	recentLinesMu sync.Mutex
}

// RingBuffer is a fixed-size circular buffer for terminal output.
type RingBuffer struct {
	buf   []byte
	size  int
	write int
	full  bool
	mu    sync.Mutex
}

// NewRingBuffer creates a new ring buffer with the given capacity in bytes.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

// Write appends p to the ring buffer, overwriting the oldest data when full.
func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n = len(p)
	for len(p) > 0 {
		space := rb.size - rb.write
		if space == 0 {
			rb.write = 0
			rb.full = true
			space = rb.size
		}
		chunk := space
		if chunk > len(p) {
			chunk = len(p)
		}
		copy(rb.buf[rb.write:rb.write+chunk], p[:chunk])
		rb.write += chunk
		if rb.write >= rb.size {
			rb.write = 0
			rb.full = true
		}
		p = p[chunk:]
	}
	return n, nil
}

// Bytes returns all buffered content in chronological order.
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.full {
		out := make([]byte, rb.write)
		copy(out, rb.buf[:rb.write])
		return out
	}

	// Buffer has wrapped: oldest data starts at rb.write.
	out := make([]byte, rb.size)
	n := copy(out, rb.buf[rb.write:])
	copy(out[n:], rb.buf[:rb.write])
	return out
}

// NewSession creates a new PTY session but does not start it yet.
// Call Start to launch the process.
func NewSession(id, name, command, cwd string, env map[string]string) (*Session, error) {
	if command == "" {
		return nil, fmt.Errorf("command must not be empty")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:         id,
		Name:       name,
		Command:    command,
		Status:     StatusIdle,
		cwd:        cwd,
		env:        env,
		outputBuf:  NewRingBuffer(defaultRingSize),
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
		execQueue:  make(chan execRequest, 64),
		outputSubs: make(map[string]chan string),
	}

	// Start the command queue processor.
	go s.processExecQueue()

	return s, nil
}

// Start launches the process in a PTY.
func (s *Session) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Status == StatusRunning {
		return fmt.Errorf("session %s is already running", s.Name)
	}

	cmd := exec.CommandContext(s.ctx, "/bin/sh", "-c", s.Command)
	if s.cwd != "" {
		cmd.Dir = s.cwd
	}
	if len(s.env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range s.env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	ptmx, err := ptylib.Start(cmd)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}

	s.cmd = cmd
	s.ptmx = ptmx
	s.PID = cmd.Process.Pid
	s.Status = StatusRunning

	// Initialize last output time for hang detection (T058).
	s.lastOutputMu.Lock()
	s.lastOutput = time.Now()
	s.lastOutputMu.Unlock()

	// Start the output reader goroutine.
	go s.readLoop()

	// Start a goroutine to wait for the process to exit.
	go s.waitLoop()

	return nil
}

// readLoop reads from the PTY master into the ring buffer and invokes the
// onOutput callback for each complete line.
func (s *Session) readLoop() {
	reader := io.TeeReader(s.ptmx, s.outputBuf)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		now := time.Now()

		// Track byte throughput for data_rate guard.
		s.bytesWritten.Add(int64(len(scanner.Bytes()) + 1)) // +1 for newline

		// Maintain timestamped line history for log_watch guard.
		s.recentLinesMu.Lock()
		s.recentLines = append(s.recentLines, timedLine{t: now, text: line})
		if len(s.recentLines) > maxRecentLines {
			s.recentLines = s.recentLines[len(s.recentLines)-maxRecentLines:]
		}
		s.recentLinesMu.Unlock()

		// Update last output time for hang detection (T058).
		s.lastOutputMu.Lock()
		s.lastOutput = now
		s.lastOutputMu.Unlock()

		s.mu.RLock()
		fn := s.onOutput
		s.mu.RUnlock()

		if fn != nil {
			fn(line)
		}

		// Fan out to any waiting ExecCommand subscribers.
		s.outputSubsMu.Lock()
		for _, ch := range s.outputSubs {
			select {
			case ch <- line:
			default:
				// Drop if subscriber is not keeping up.
			}
		}
		s.outputSubsMu.Unlock()
	}
	// Scanner exits when the PTY master is closed (process exit or Stop).
}

// waitLoop waits for the process to exit and updates the session status.
func (s *Session) waitLoop() {
	defer close(s.done)

	err := s.cmd.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Close the PTY master after the process exits.
	if s.ptmx != nil {
		_ = s.ptmx.Close()
		s.ptmx = nil
	}

	if s.Status == StatusTerminated {
		// Already set by Stop() — keep it.
	} else if err == nil {
		s.Status = StatusIdle
	} else {
		s.Status = StatusError
	}
	s.PID = 0
}

// Stop terminates the session gracefully (SIGTERM, then SIGKILL after 5s).
func (s *Session) Stop() error {
	s.mu.RLock()
	if s.Status != StatusRunning {
		s.mu.RUnlock()
		return nil
	}
	proc := s.cmd.Process
	s.mu.RUnlock()

	if proc == nil {
		return nil
	}

	// Send SIGTERM.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited.
		return nil
	}

	// Wait up to 5 seconds for graceful exit.
	select {
	case <-s.done:
		return nil
	case <-time.After(5 * time.Second):
	}

	// Mark as terminated before SIGKILL so waitLoop doesn't override to "error".
	s.mu.Lock()
	s.Status = StatusTerminated
	s.mu.Unlock()

	// Force kill.
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		// Already exited.
		return nil
	}

	// Wait for the process to fully exit after SIGKILL.
	<-s.done

	s.cancel()
	return nil
}

// Write sends input to the PTY (stdin).
func (s *Session) Write(data []byte) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.ptmx == nil {
		return 0, fmt.Errorf("session %s is not running", s.Name)
	}
	return s.ptmx.Write(data)
}

// GetOutput returns the current output buffer contents.
func (s *Session) GetOutput() []byte {
	return s.outputBuf.Bytes()
}

// SetOnOutput sets the callback for output line monitoring.
func (s *Session) SetOnOutput(fn func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onOutput = fn
}

// StartHangDetector starts a goroutine that monitors for session inactivity.
// If no output is received for the given timeout duration, it fires the onHang
// callback with the session name. The detector resets when new output arrives
// and stops when the session stops (T058).
func (s *Session) StartHangDetector(timeout time.Duration, onHang func(name string)) {
	go func() {
		ticker := time.NewTicker(timeout / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.lastOutputMu.Lock()
				last := s.lastOutput
				s.lastOutputMu.Unlock()

				if time.Since(last) >= timeout {
					s.mu.RLock()
					status := s.Status
					s.mu.RUnlock()

					// Only fire for running sessions.
					if status == StatusRunning {
						onHang(s.Name)
					}
				}
			case <-s.done:
				return
			case <-s.ctx.Done():
				return
			}
		}
	}()
}

// Wait blocks until the session process exits.
func (s *Session) Wait() error {
	<-s.done
	return nil
}

// RLock acquires a read lock on the session for thread-safe field access.
func (s *Session) RLock() { s.mu.RLock() }

// RUnlock releases the read lock.
func (s *Session) RUnlock() { s.mu.RUnlock() }

// ExecCommand writes a command to the PTY and captures the output until a
// marker line appears, indicating command completion. Returns the output and
// duration. Supports configurable timeout. Concurrent calls are serialized
// through the command queue.
func (s *Session) ExecCommand(command string, timeout time.Duration) (output string, exitCode int, duration time.Duration, err error) {
	s.mu.RLock()
	status := s.Status
	s.mu.RUnlock()

	if status != StatusRunning {
		return "", -1, 0, fmt.Errorf("session %s is not running (status: %s)", s.Name, status)
	}

	req := execRequest{
		command:  command,
		timeout:  timeout,
		resultCh: make(chan CommandResult, 1),
	}

	select {
	case s.execQueue <- req:
	case <-s.ctx.Done():
		return "", -1, 0, fmt.Errorf("session %s is shutting down", s.Name)
	}

	select {
	case result := <-req.resultCh:
		return result.Output, result.ExitCode, result.Duration, result.Err
	case <-s.ctx.Done():
		return "", -1, 0, fmt.Errorf("session %s is shutting down", s.Name)
	}
}

// processExecQueue processes queued command execution requests serially.
func (s *Session) processExecQueue() {
	for {
		select {
		case req := <-s.execQueue:
			result := s.executeCommand(req.command, req.timeout)
			req.resultCh <- result
		case <-s.ctx.Done():
			return
		}
	}
}

// executeCommand performs the actual command execution: writes to PTY, captures
// output until the end marker is seen, and parses the exit code.
func (s *Session) executeCommand(command string, timeout time.Duration) CommandResult {
	start := time.Now()
	traceID := uuid.New().String()[:8]

	// Generate a unique end marker using a short ID for reliable detection.
	markerID := uuid.New().String()[:8]
	marker := fmt.Sprintf("__KING_%s", markerID)
	endPattern := marker + "_"

	// Subscribe to output lines.
	subID := uuid.New().String()
	lineCh := make(chan string, 256)
	s.outputSubsMu.Lock()
	s.outputSubs[subID] = lineCh
	s.outputSubsMu.Unlock()

	defer func() {
		s.outputSubsMu.Lock()
		delete(s.outputSubs, subID)
		s.outputSubsMu.Unlock()
	}()

	// Write the command followed by the marker echo to the PTY.
	// Use printf to avoid shell interpretation issues. The marker includes $? for exit code.
	fullCmd := fmt.Sprintf("%s\nprintf '\\n%s_%%d__\\n' $?\n", command, marker)
	if _, err := s.Write([]byte(fullCmd)); err != nil {
		return CommandResult{Err: fmt.Errorf("write to pty: %w", err), ExitCode: -1, Duration: time.Since(start), TraceID: traceID}
	}

	// Collect output lines until the marker appears or timeout.
	var lines []string
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Track whether we've seen the command echo-back (first occurrence of command text).
	seenEcho := false

	exitCode := -1
	for {
		select {
		case line := <-lineCh:
			// Strip ANSI escape codes for clean analysis.
			clean := stripANSI(line)

			// Check if this line contains our end marker.
			if idx := strings.Index(clean, endPattern); idx >= 0 {
				// Parse exit code: __KING_<id>_<exitcode>__
				markerPart := clean[idx+len(endPattern):]
				markerPart = strings.TrimSuffix(markerPart, "__")
				markerPart = strings.TrimSpace(markerPart)
				if code, err := strconv.Atoi(markerPart); err == nil {
					exitCode = code
				} else {
					exitCode = 0 // marker found but couldn't parse code
				}
				dur := time.Since(start)
				return CommandResult{
					Output:   strings.Join(lines, "\n"),
					ExitCode: exitCode,
					Duration: dur,
					TraceID:  traceID,
				}
			}

			// Skip lines containing the marker (e.g., the printf echo-back).
			if strings.Contains(clean, marker) {
				continue
			}

			// Skip command echo-back lines.
			trimmed := strings.TrimSpace(clean)
			if !seenEcho && len(command) > 2 && strings.Contains(clean, command) {
				seenEcho = true
				continue
			}

			// Skip empty lines, prompt artifacts, and shell prompt lines.
			if trimmed == "" || trimmed == "%" {
				continue
			}
			// Heuristic: skip lines that look like a shell prompt
			// (contain common prompt markers).
			if isPromptLine(trimmed) {
				continue
			}

			lines = append(lines, trimmed)
		case <-timer.C:
			dur := time.Since(start)
			return CommandResult{
				Output:   strings.Join(lines, "\n"),
				ExitCode: -1,
				Duration: dur,
				TraceID:  traceID,
				Err:      fmt.Errorf("command timed out after %v", timeout),
			}
		case <-s.ctx.Done():
			dur := time.Since(start)
			return CommandResult{
				Output:   strings.Join(lines, "\n"),
				ExitCode: -1,
				Duration: dur,
				TraceID:  traceID,
				Err:      fmt.Errorf("session terminated"),
			}
		}
	}
}

// BytesWritten returns the total number of bytes received from the PTY output.
// Used by the data_rate guard to measure throughput.
func (s *Session) BytesWritten() int64 {
	return s.bytesWritten.Load()
}

// RecentOutputLines returns output lines received since the given time.
// Used by the log_watch guard to scan for error patterns.
func (s *Session) RecentOutputLines(since time.Time) []string {
	s.recentLinesMu.Lock()
	defer s.recentLinesMu.Unlock()
	var result []string
	for _, tl := range s.recentLines {
		if !tl.t.Before(since) {
			result = append(result, tl.text)
		}
	}
	return result
}

// Resize changes the PTY window size.
func (s *Session) Resize(rows, cols uint16) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.ptmx == nil {
		return fmt.Errorf("session %s is not running", s.Name)
	}
	return ptylib.Setsize(s.ptmx, &ptylib.Winsize{
		Rows: rows,
		Cols: cols,
	})
}
