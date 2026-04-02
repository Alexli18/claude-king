// internal/registry/registry_test.go
package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/registry"
)

func TestRegister_WritesEntry(t *testing.T) {
	dir := t.TempDir()
	reg := registry.NewRegistry(filepath.Join(dir, "registry.json"))

	entry := registry.Entry{
		Socket: "/tmp/king-test.sock",
		PID:    os.Getpid(),
		Name:   "test-kingdom",
	}

	if err := reg.Register("/home/test/project", entry); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	entries, err := reg.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if _, ok := entries["/home/test/project"]; !ok {
		t.Fatal("registered entry not found")
	}
}

func TestUnregister_RemovesEntry(t *testing.T) {
	dir := t.TempDir()
	reg := registry.NewRegistry(filepath.Join(dir, "registry.json"))

	reg.Register("/home/test/project", registry.Entry{PID: os.Getpid(), Name: "x"})

	if err := reg.Unregister("/home/test/project"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	entries, _ := reg.List()
	if _, ok := entries["/home/test/project"]; ok {
		t.Fatal("entry should have been removed")
	}
}

func TestList_PrunesStale(t *testing.T) {
	dir := t.TempDir()
	reg := registry.NewRegistry(filepath.Join(dir, "registry.json"))

	// PID 999999 almost certainly does not exist
	reg.Register("/stale/project", registry.Entry{PID: 999999, Name: "stale"})
	reg.Register("/home/test/project", registry.Entry{PID: os.Getpid(), Name: "alive"})

	entries, err := reg.ListAlive()
	if err != nil {
		t.Fatalf("ListAlive failed: %v", err)
	}
	if _, ok := entries["/stale/project"]; ok {
		t.Error("stale entry should have been pruned")
	}
	if _, ok := entries["/home/test/project"]; !ok {
		t.Error("alive entry should be present")
	}
}

func TestList_SkipsDeadSocket(t *testing.T) {
	dir := t.TempDir()
	reg := registry.NewRegistry(filepath.Join(dir, "registry.json"))

	reg.Register("/home/test/dead", registry.Entry{
		PID:    os.Getpid(),
		Name:   "dead",
		Socket: "/tmp/nonexistent-socket-king-test-" + t.Name() + ".sock",
	})

	entries, err := reg.ListAlive()
	if err != nil {
		t.Fatalf("ListAlive failed: %v", err)
	}
	e, ok := entries["/home/test/dead"]
	if !ok {
		t.Fatal("entry with alive PID should exist")
	}
	if e.Reachable {
		t.Error("socket should be unreachable (no listener)")
	}
}
