package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/alexli18/claude-king/internal/config"
	"github.com/alexli18/claude-king/internal/store"
	"github.com/google/uuid"
)

// Kingdom manages the runtime state of a single kingdom instance.
type Kingdom struct {
	ID      string
	store   *store.Store
	config  *config.KingdomConfig
	rootDir string
	status  string
	logger  *slog.Logger
}

// NewKingdom creates or loads a Kingdom entity from the store.
// If an existing kingdom with the same root path is found, it reuses its ID
// and updates the status to "starting". Otherwise it creates a new record.
func NewKingdom(s *store.Store, cfg *config.KingdomConfig, rootDir string, logger *slog.Logger) (*Kingdom, error) {
	existing, err := s.GetKingdomByPath(rootDir)
	if err != nil {
		return nil, fmt.Errorf("lookup kingdom by path: %w", err)
	}

	k := &Kingdom{
		store:   s,
		config:  cfg,
		rootDir: rootDir,
		status:  "starting",
		logger:  logger,
	}

	if existing != nil {
		k.ID = existing.ID
		logger.Info("reusing existing kingdom", "id", k.ID, "root", rootDir)

		if err := s.UpdateKingdomStatus(k.ID, "starting"); err != nil {
			return nil, fmt.Errorf("update kingdom status: %w", err)
		}
		if err := s.UpdateKingdomPID(k.ID, os.Getpid()); err != nil {
			return nil, fmt.Errorf("update kingdom pid: %w", err)
		}
		return k, nil
	}

	k.ID = uuid.New().String()
	now := time.Now().UTC().Format(time.DateTime)

	logger.Info("creating new kingdom", "id", k.ID, "name", cfg.Name, "root", rootDir)

	rec := store.Kingdom{
		ID:        k.ID,
		Name:      cfg.Name,
		RootPath:  rootDir,
		PID:       os.Getpid(),
		Status:    "starting",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.CreateKingdom(rec); err != nil {
		return nil, fmt.Errorf("create kingdom: %w", err)
	}

	return k, nil
}

// SetStatus updates the Kingdom status in both memory and the store.
func (k *Kingdom) SetStatus(status string) error {
	if err := k.store.UpdateKingdomStatus(k.ID, status); err != nil {
		return fmt.Errorf("set kingdom status: %w", err)
	}
	k.status = status
	k.logger.Info("kingdom status changed", "id", k.ID, "status", status)
	return nil
}

// GetStatus returns the current in-memory status.
func (k *Kingdom) GetStatus() string {
	return k.status
}
