// internal/registry/registry.go
package registry

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

// Entry describes a single running King daemon.
type Entry struct {
	Socket    string `json:"socket"`
	PID       int    `json:"pid"`
	Name      string `json:"name"`
	Updated   string `json:"updated"`
	Reachable bool   `json:"-"` // set by ListAlive, not persisted
}

// Registry manages the ~/.king/registry.json file.
type Registry struct {
	path string
	mu   sync.Mutex
}

// NewRegistry creates a Registry backed by the given JSON file path.
func NewRegistry(path string) *Registry {
	return &Registry{path: path}
}

// Register atomically writes or updates an entry for the given root path.
func (r *Registry) Register(rootPath string, entry Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	unlock, err := r.lockFile()
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer unlock()

	entries, _ := r.load()
	if entries == nil {
		entries = make(map[string]Entry)
	}
	entry.Updated = time.Now().UTC().Format(time.RFC3339)
	entries[rootPath] = entry
	return r.save(entries)
}

// Unregister removes the entry for the given root path.
func (r *Registry) Unregister(rootPath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	unlock, err := r.lockFile()
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer unlock()

	entries, _ := r.load()
	if entries == nil {
		entries = make(map[string]Entry)
	}
	delete(entries, rootPath)
	return r.save(entries)
}

// List returns all entries without liveness checks.
func (r *Registry) List() (map[string]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.load()
}

// ListAlive returns entries whose PID is alive, marking socket reachability.
// It prunes entries with dead PIDs from the file.
func (r *Registry) ListAlive() (map[string]Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	unlock, err := r.lockFile()
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer unlock()

	entries, err := r.load()
	if err != nil {
		return nil, err
	}

	alive := make(map[string]Entry)
	pruned := false
	for path, e := range entries {
		if !pidAlive(e.PID) {
			pruned = true
			continue
		}
		e.Reachable = socketReachable(e.Socket)
		alive[path] = e
	}
	if pruned {
		_ = r.save(alive)
	}
	return alive, nil
}

// lockFile acquires an exclusive advisory lock on <r.path>.lock.
// Returns a function that releases the lock and closes the file.
func (r *Registry) lockFile() (unlock func(), err error) {
	lockPath := r.path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// load reads and parses the JSON file. Returns empty map on missing file.
func (r *Registry) load() (map[string]Entry, error) {
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return make(map[string]Entry), nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]Entry
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// save writes the map atomically via a temp file + rename.
func (r *Registry) save(entries map[string]Entry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// pidAlive returns true if a process with the given PID exists and is running.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks process existence without sending a real signal.
	err = p.Signal(syscall.Signal(0))
	// nil = alive, EPERM = alive but no permission, any other error = dead
	return err == nil || err == syscall.EPERM
}

// socketReachable returns true if the Unix domain socket accepts connections.
func socketReachable(socketPath string) bool {
	if socketPath == "" {
		return false
	}
	conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
