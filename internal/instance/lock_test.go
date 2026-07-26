package instance

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireRejectsSecondInstanceAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapterbrake.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	if _, err := Acquire(path); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Acquire(second) error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire(after release) error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}
}

func TestAcquireRejectsRelativePath(t *testing.T) {
	if _, err := Acquire("relative.lock"); err == nil {
		t.Fatal("Acquire(relative) error = nil")
	}
}
