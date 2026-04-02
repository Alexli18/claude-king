package audit

import (
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

const (
	defaultBatchSize  = 100
	defaultFlushEvery = 1 * time.Second

	// Sampling: when ingestion rate exceeds samplingThreshold lines/sec,
	// only 1 in samplingRate lines is recorded (with sampled=true).
	samplingThreshold = 1000
	samplingRate      = 10
)

// BatchWriter accumulates audit entries and flushes them in batches
// to reduce SQLite I/O overhead from high-volume ingestion writes.
// It also tracks line rate and applies sampling when ingestion exceeds
// samplingThreshold lines/second.
type BatchWriter struct {
	store     *store.Store
	kingdomID string
	logger    *slog.Logger

	mu      sync.Mutex
	buffer  []store.AuditEntry
	stopCh  chan struct{}
	stopped bool

	// Rate tracking for sampling.
	windowStart time.Time
	windowCount int
	sampling    bool // true when rate exceeds threshold
	sampleSeq   int  // counts lines within a sampling window
}

// NewBatchWriter creates a BatchWriter that flushes on batch size or time interval.
func NewBatchWriter(s *store.Store, kingdomID string, logger *slog.Logger) *BatchWriter {
	bw := &BatchWriter{
		store:       s,
		kingdomID:   kingdomID,
		logger:      logger,
		buffer:      make([]store.AuditEntry, 0, defaultBatchSize),
		stopCh:      make(chan struct{}),
		windowStart: time.Now(),
	}
	go bw.flushLoop()
	return bw
}

// Add queues an ingestion audit entry for batched writing.
// It tracks line rate and applies sampling when throughput exceeds the threshold.
func (bw *BatchWriter) Add(vassalName, vassalID, line string, sampled bool) {
	bw.mu.Lock()

	// Rate tracking: count lines per 1-second window.
	now := time.Now()
	if now.Sub(bw.windowStart) >= time.Second {
		rate := bw.windowCount
		bw.windowStart = now
		bw.windowCount = 0
		// Switch sampling on/off based on previous window rate.
		bw.sampling = rate >= samplingThreshold
		bw.sampleSeq = 0
	}
	bw.windowCount++

	// Apply sampling when above threshold.
	shouldRecord := true
	isSampled := sampled
	if bw.sampling {
		bw.sampleSeq++
		if bw.sampleSeq%samplingRate != 0 {
			shouldRecord = false
		} else {
			isSampled = true
		}
	}

	if !shouldRecord {
		bw.mu.Unlock()
		return
	}

	entry := store.AuditEntry{
		ID:        uuid.New().String(),
		KingdomID: bw.kingdomID,
		Layer:     "ingestion",
		Source:    vassalName,
		SourceID:  vassalID,
		Content:   sanitizeLine(line),
		Sampled:   isSampled,
		CreatedAt: now.UTC().Format("2006-01-02 15:04:05"),
	}

	bw.buffer = append(bw.buffer, entry)
	needsFlush := len(bw.buffer) >= defaultBatchSize
	bw.mu.Unlock()

	if needsFlush {
		bw.flush()
	}
}

// sanitizeLine replaces non-printable control characters (except newline/tab)
// with the placeholder "·" to prevent binary garbage in the audit log.
func sanitizeLine(line string) string {
	if !strings.ContainsFunc(line, func(r rune) bool {
		return r < 0x20 && r != '\t' && r != '\n'
	}) {
		return line
	}
	var b strings.Builder
	b.Grow(len(line))
	for _, r := range line {
		if r < 0x20 && r != '\t' && r != '\n' {
			b.WriteRune('·')
		} else if !unicode.IsPrint(r) && r != '\t' && r != '\n' {
			b.WriteRune('·')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Stop flushes remaining entries and stops the background flush loop.
func (bw *BatchWriter) Stop() {
	bw.mu.Lock()
	if bw.stopped {
		bw.mu.Unlock()
		return
	}
	bw.stopped = true
	bw.mu.Unlock()

	close(bw.stopCh)
	bw.flush()
}

func (bw *BatchWriter) flushLoop() {
	ticker := time.NewTicker(defaultFlushEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bw.flush()
		case <-bw.stopCh:
			return
		}
	}
}

func (bw *BatchWriter) flush() {
	bw.mu.Lock()
	if len(bw.buffer) == 0 {
		bw.mu.Unlock()
		return
	}
	batch := bw.buffer
	bw.buffer = make([]store.AuditEntry, 0, defaultBatchSize)
	bw.mu.Unlock()

	if err := bw.store.CreateAuditEntryBatch(batch); err != nil {
		bw.logger.Warn("failed to flush audit batch", "count", len(batch), "error", err)
	}
}
