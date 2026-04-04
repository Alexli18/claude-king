package store

import (
	"errors"
	"syscall"
	"testing"
)

// ---------------------------------------------------------------------------
// isDiskFull (unexported)
// ---------------------------------------------------------------------------

func TestIsDiskFull_NilError(t *testing.T) {
	if isDiskFull(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsDiskFull_ENOSPC(t *testing.T) {
	err := syscall.ENOSPC
	if !isDiskFull(err) {
		t.Error("expected true for ENOSPC error")
	}
}

func TestIsDiskFull_SQLiteFullMessage(t *testing.T) {
	err := errors.New("database or disk is full")
	if !isDiskFull(err) {
		t.Error("expected true for 'database or disk is full' message")
	}
}

func TestIsDiskFull_SQLITE_FULL_Message(t *testing.T) {
	err := errors.New("SQLITE_FULL: disk quota exceeded")
	if !isDiskFull(err) {
		t.Error("expected true for SQLITE_FULL message")
	}
}

func TestIsDiskFull_UnrelatedError(t *testing.T) {
	err := errors.New("some unrelated error")
	if isDiskFull(err) {
		t.Error("expected false for unrelated error")
	}
}

func TestIsDiskFull_EAGAIN(t *testing.T) {
	err := syscall.EAGAIN
	if isDiskFull(err) {
		t.Error("expected false for EAGAIN (not a disk full error)")
	}
}
