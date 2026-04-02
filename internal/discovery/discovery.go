// internal/discovery/discovery.go
package discovery

import (
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
