package artifacts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/security"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

// Ledger manages artifact registration, versioning, and lookup.
type Ledger struct {
	store     *store.Store
	kingdomID string
	settings  config.Settings
}

// NewLedger creates a new Ledger bound to the given store and kingdom.
func NewLedger(s *store.Store, kingdomID string) *Ledger {
	return &Ledger{
		store:     s,
		kingdomID: kingdomID,
	}
}

// NewLedgerWithSettings creates a Ledger with access to kingdom Settings
// for external scanner configuration.
func NewLedgerWithSettings(s *store.Store, kingdomID string, settings config.Settings) *Ledger {
	return &Ledger{store: s, kingdomID: kingdomID, settings: settings}
}

// Register registers or updates an artifact in the ledger.
// If an artifact with the same name already exists in this kingdom, the
// existing record is updated with an incremented version number.
// Returns the artifact record.
func (l *Ledger) Register(name, filePath, producerID, mimeType string) (*store.Artifact, error) {
	// Validate file exists.
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("artifact file not found: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("artifact path is a directory, not a file: %s", filePath)
	}

	// Secret scan: block artifacts containing secrets.
	if result := security.Scan(filePath); result.Blocked {
		slog.Info("FILE_BLOCKED", "reason", result.Reason, "file", filePath)
		return nil, fmt.Errorf("FILE_BLOCKED: %s", result.Reason)
	}

	// External security scanner (plugin) — fail-open on timeout or missing binary.
	if l.settings.SecurityScanner != "" {
		if blocked, reason := l.runExternalScanner(filePath); blocked {
			slog.Info("FILE_BLOCKED", "reason", reason, "scanner", l.settings.SecurityScanner, "file", filePath)
			return nil, fmt.Errorf("FILE_BLOCKED reason=%s scanner=%s file=%s", reason, l.settings.SecurityScanner, filePath)
		}
	}

	// Auto-detect MIME type from extension if not provided.
	if mimeType == "" {
		ext := filepath.Ext(filePath)
		if ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
	}

	// Compute SHA-256 checksum.
	checksum, err := computeSHA256(filePath)
	if err != nil {
		return nil, fmt.Errorf("compute checksum: %w", err)
	}

	now := time.Now().UTC().Format(time.DateTime)

	// Check if artifact already exists.
	existing, err := l.store.GetArtifactByName(l.kingdomID, name)
	if err != nil {
		return nil, fmt.Errorf("lookup existing artifact: %w", err)
	}

	if existing != nil {
		// Update existing artifact: increment version.
		existing.FilePath = filePath
		existing.ProducerID = producerID
		existing.MimeType = mimeType
		existing.Version++
		existing.Checksum = "sha256:" + checksum

		if err := l.store.UpdateArtifact(*existing); err != nil {
			return nil, fmt.Errorf("update artifact: %w", err)
		}

		// Re-fetch to get the updated_at set by the DB.
		updated, err := l.store.GetArtifactByName(l.kingdomID, name)
		if err != nil {
			return nil, fmt.Errorf("re-fetch updated artifact: %w", err)
		}
		slog.Info("ARTIFACT_REGISTERED", "name", updated.Name, "version", updated.Version, "checksum", updated.Checksum)
		return updated, nil
	}

	// Create new artifact.
	a := store.Artifact{
		ID:         uuid.New().String(),
		KingdomID:  l.kingdomID,
		ProducerID: producerID,
		Name:       name,
		FilePath:   filePath,
		MimeType:   mimeType,
		Version:    1,
		Checksum:   "sha256:" + checksum,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := l.store.CreateArtifact(a); err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}

	slog.Info("ARTIFACT_REGISTERED", "name", a.Name, "version", a.Version, "checksum", a.Checksum)
	return &a, nil
}

// Resolve resolves an artifact name to its record within this kingdom.
func (l *Ledger) Resolve(name string) (*store.Artifact, error) {
	a, err := l.store.GetArtifactByName(l.kingdomID, name)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact: %w", err)
	}
	if a == nil {
		return nil, fmt.Errorf("artifact not found: %s", name)
	}
	return a, nil
}

// List returns all artifacts in the kingdom.
func (l *Ledger) List() ([]store.Artifact, error) {
	return l.store.ListArtifacts(l.kingdomID)
}

// computeSHA256 returns the hex-encoded SHA-256 digest of the file at path.
func computeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

const externalScannerTimeout = 5 * time.Second

func (l *Ledger) runExternalScanner(filePath string) (blocked bool, reason string) {
	args := append(
		[]string{"detect", "--source", filePath},
		l.settings.SecurityScannerArgs...,
	)
	args = append(args, "--exit-code", "1")

	ctx, cancel := context.WithTimeout(context.Background(), externalScannerTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, l.settings.SecurityScanner, args...)
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		// Fail-open on timeout
		return false, ""
	}

	var exitErr *exec.ExitError
	if errors.Is(err, exec.ErrNotFound) {
		// Fail-open when binary not found
		return false, ""
	}
	if err != nil && !errors.As(err, &exitErr) {
		// Other unexpected error (e.g. permission denied) — fail-open
		return false, ""
	}
	if exitErr != nil && exitErr.ExitCode() != 0 {
		return true, "SCANNER_REJECTED"
	}
	return false, ""
}
