package events

import (
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

// ansiRegex strips ANSI escape sequences for clean pattern matching.
var ansiRegex = regexp.MustCompile(
	`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[=>]|\x1b\(B|\x07|\r|\x08.?`,
)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// Subscriber receives events when they are detected.
type Subscriber func(event store.Event)

// AuditCallback is called by Sieve to record audit decisions.
// Defined as a function type to avoid import cycles with audit package.
type AuditCallback func(vassalName, vassalID, content, decision, pattern, severity, summary string)

// Sieve monitors PTY output lines, applies pattern matching, deduplicates events,
// and publishes detected events to subscribers.
type Sieve struct {
	patterns      []*CompiledPattern
	store         *store.Store
	kingdomID     string
	cooldown      time.Duration
	lastFired     map[string]time.Time // pattern+source -> last fire time
	mu            sync.Mutex
	subscribers   []Subscriber
	auditCallback AuditCallback // func(vassalName, vassalID, content, decision, pattern, severity, summary)
	logger        *slog.Logger
}

// NewSieve creates a new Sieve with the given patterns, store, kingdom ID,
// cooldown duration in seconds, and logger.
func NewSieve(patterns []*CompiledPattern, s *store.Store, kingdomID string, cooldownSeconds int, logger *slog.Logger) *Sieve {
	if cooldownSeconds <= 0 {
		cooldownSeconds = 30
	}
	return &Sieve{
		patterns:  patterns,
		store:     s,
		kingdomID: kingdomID,
		cooldown:  time.Duration(cooldownSeconds) * time.Second,
		lastFired: make(map[string]time.Time),
		logger:    logger,
	}
}

// ProcessLine processes a single output line from a vassal.
// It matches against all patterns, deduplicates, persists events, and notifies subscribers.
func (s *Sieve) ProcessLine(vassalName, vassalID, line string) {
	// Strip ANSI escape codes before pattern matching.
	cleanLine := stripANSI(line)
	s.logger.Debug("sieve processing line", "vassal", vassalName, "clean", cleanLine[:min(len(cleanLine), 80)])
	for _, p := range s.patterns {
		summary, matched := p.Match(cleanLine, vassalName)
		if !matched {
			continue
		}

		// Deduplication: check cooldown.
		dedupeKey := p.Name + ":" + vassalName
		s.mu.Lock()
		last, exists := s.lastFired[dedupeKey]
		if exists && time.Since(last) < s.cooldown {
			cb := s.auditCallback
			s.mu.Unlock()
			s.logger.Debug("event suppressed by cooldown",
				"pattern", p.Name,
				"vassal", vassalName,
			)
			// Record suppressed decision to audit.
			if cb != nil {
				cb(vassalName, vassalID, cleanLine, "suppressed", p.Name, p.Severity, "")
			}
			continue
		}
		s.lastFired[dedupeKey] = time.Now()
		s.mu.Unlock()

		event := store.Event{
			ID:        uuid.New().String(),
			KingdomID: s.kingdomID,
			SourceID:  vassalID,
			Severity:  p.Severity,
			Pattern:   p.Name,
			Summary:   summary,
			RawOutput: line,
			CreatedAt: time.Now().UTC().Format(time.DateTime),
		}

		if err := s.store.CreateEvent(event); err != nil {
			s.logger.Error("failed to persist event",
				"pattern", p.Name,
				"vassal", vassalName,
				"error", err,
			)
			continue
		}

		s.logger.Info("event detected",
			"pattern", p.Name,
			"severity", p.Severity,
			"vassal", vassalName,
			"summary", summary,
		)

		// Record matched decision to audit.
		s.mu.Lock()
		cb := s.auditCallback
		s.mu.Unlock()
		if cb != nil {
			cb(vassalName, vassalID, cleanLine, "matched", p.Name, p.Severity, summary)
		}

		// Notify subscribers.
		s.mu.Lock()
		subs := make([]Subscriber, len(s.subscribers))
		copy(subs, s.subscribers)
		s.mu.Unlock()

		for _, fn := range subs {
			fn(event)
		}
	}
}

// SetAuditCallback sets the audit recorder callback for sieve-layer audit entries.
func (s *Sieve) SetAuditCallback(fn AuditCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditCallback = fn
}

// Subscribe adds an event subscriber.
func (s *Sieve) Subscribe(fn Subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers = append(s.subscribers, fn)
}

// OutputCallback returns a callback function suitable for pty.Session.SetOnOutput.
// The returned function will call ProcessLine with the correct vassal context.
func (s *Sieve) OutputCallback(vassalName, vassalID string) func(string) {
	return func(line string) {
		s.ProcessLine(vassalName, vassalID, line)
	}
}

// HealthSummary returns a brief status string summarizing recent events.
func (s *Sieve) HealthSummary() string {
	events, err := s.store.ListEvents(s.kingdomID, "", "", 100)
	if err != nil {
		return fmt.Sprintf("event query error: %v", err)
	}

	if len(events) == 0 {
		return "No events recorded."
	}

	// Count events by severity.
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Severity]++
	}

	parts := make([]string, 0, len(counts))
	for _, sev := range []string{"error", "warn", "info"} {
		if c, ok := counts[sev]; ok {
			parts = append(parts, fmt.Sprintf("%d %s", c, sev))
		}
	}

	// Include any other severities.
	for sev, c := range counts {
		if sev != "error" && sev != "warn" && sev != "info" {
			parts = append(parts, fmt.Sprintf("%d %s", c, sev))
		}
	}

	summary := fmt.Sprintf("%d recent events", len(events))
	if len(parts) > 0 {
		summary += " ("
		for i, p := range parts {
			if i > 0 {
				summary += ", "
			}
			summary += p
		}
		summary += ")"
	}

	return summary
}
