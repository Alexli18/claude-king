// internal/discovery/discovery_test.go
package discovery_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexli18/claude-king/internal/discovery"
)

func TestFindKingdomSocket_Found(t *testing.T) {
	// Build a temp tree: /tmp/root/.king/king-aabbccdd.sock
	root := t.TempDir()
	kingDir := filepath.Join(root, ".king")
	if err := os.MkdirAll(kingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(kingDir, "king-aabbccdd.sock")
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	// Search from a subdirectory.
	subDir := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	gotSock, gotRoot, err := discovery.FindKingdomSocket(subDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSock != sockPath {
		t.Errorf("socket: got %q, want %q", gotSock, sockPath)
	}
	if gotRoot != root {
		t.Errorf("root: got %q, want %q", gotRoot, root)
	}
}

func TestFindKingdomSocket_NotFound(t *testing.T) {
	root := t.TempDir()
	_, _, err := discovery.FindKingdomSocket(root)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != discovery.ErrNoKingdom {
		t.Errorf("expected ErrNoKingdom, got %v", err)
	}
}

func TestFindKingdomSocket_CurrentDir(t *testing.T) {
	// Socket in the start dir itself.
	root := t.TempDir()
	kingDir := filepath.Join(root, ".king")
	if err := os.MkdirAll(kingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sockPath := filepath.Join(kingDir, "king-deadbeef.sock")
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	gotSock, gotRoot, err := discovery.FindKingdomSocket(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSock != sockPath {
		t.Errorf("socket: got %q, want %q", gotSock, sockPath)
	}
	if gotRoot != root {
		t.Errorf("root: got %q, want %q", gotRoot, root)
	}
}

func TestFindAllKingdomSockets_MultipleKingdoms(t *testing.T) {
	// Two kingdoms: one in child dir, one in parent dir
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(filepath.Join(child, ".king"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, ".king"), 0o755); err != nil {
		t.Fatal(err)
	}
	childSock := filepath.Join(child, ".king", "king-aaaaaaaa.sock")
	parentSock := filepath.Join(parent, ".king", "king-bbbbbbbb.sock")
	_ = os.WriteFile(childSock, nil, 0o600)
	_ = os.WriteFile(parentSock, nil, 0o600)

	kingdoms, err := discovery.FindAllKingdomSockets(child)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kingdoms) != 2 {
		t.Fatalf("expected 2 kingdoms, got %d: %v", len(kingdoms), kingdoms)
	}
}

func TestFindAllKingdomSockets_None(t *testing.T) {
	root := t.TempDir()
	kingdoms, err := discovery.FindAllKingdomSockets(root)
	if err != discovery.ErrNoKingdom {
		t.Errorf("expected ErrNoKingdom, got err=%v kingdoms=%v", err, kingdoms)
	}
}
