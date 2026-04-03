// internal/discovery/discovery.go
package discovery

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ErrNoKingdom is returned when no king daemon socket is found in the directory tree.
var ErrNoKingdom = errors.New("no Kingdom found: run king-daemon init first")

// FindKingdomSocket walks from startDir up to / looking for a king daemon socket
// in a .king subdirectory. It scans for files matching the pattern king-*.sock.
//
// Returns the socket path and the kingdom root directory on success.
func FindKingdomSocket(startDir string) (socketPath, rootDir string, err error) {
	dir := startDir
	for {
		matches, _ := filepath.Glob(filepath.Join(dir, ".king", "king-*.sock"))
		for _, m := range matches {
			if _, statErr := os.Stat(m); statErr == nil {
				return m, dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return "", "", ErrNoKingdom
		}
		dir = parent
	}
}

// KingdomInfo describes a discovered kingdom.
type KingdomInfo struct {
	Name       string
	RootDir    string
	SocketPath string
}

// FindAllKingdomSockets walks from startDir up to / collecting all kingdom
// sockets, then merges in any alive kingdoms from the global registry at
// ~/.king/registry.json. Deduplicates by RootDir.
// Returns ErrNoKingdom if none found.
func FindAllKingdomSockets(startDir string) ([]KingdomInfo, error) {
	seen := make(map[string]bool)
	var kingdoms []KingdomInfo

	// Walk up the directory tree.
	dir := startDir
	for {
		matches, _ := filepath.Glob(filepath.Join(dir, ".king", "king-*.sock"))
		for _, m := range matches {
			if _, statErr := os.Stat(m); statErr == nil && !seen[dir] {
				seen[dir] = true
				kingdoms = append(kingdoms, KingdomInfo{
					Name:       filepath.Base(dir),
					RootDir:    dir,
					SocketPath: m,
				})
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Merge in global registry entries not already found.
	home, err := os.UserHomeDir()
	if err == nil {
		regPath := filepath.Join(home, ".king", "registry.json")
		if data, err := os.ReadFile(regPath); err == nil {
			var entries map[string]struct {
				Socket string `json:"socket"`
				Name   string `json:"name"`
			}
			if err := json.Unmarshal(data, &entries); err == nil {
				for rootDir, e := range entries {
					if seen[rootDir] {
						continue
					}
					if _, statErr := os.Stat(e.Socket); statErr == nil {
						seen[rootDir] = true
						kingdoms = append(kingdoms, KingdomInfo{
							Name:       e.Name,
							RootDir:    rootDir,
							SocketPath: e.Socket,
						})
					}
				}
			}
		}
	}

	if len(kingdoms) == 0 {
		return nil, ErrNoKingdom
	}
	return kingdoms, nil
}
