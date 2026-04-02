package artifacts

import (
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"time"

	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

// Ledger manages artifact registration, versioning, and lookup.
type Ledger struct {
	store     *store.Store
	kingdomID string
}

// NewLedger creates a new Ledger bound to the given store and kingdom.
func NewLedger(s *store.Store, kingdomID string) *Ledger {
	return &Ledger{
		store:     s,
		kingdomID: kingdomID,
	}
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
