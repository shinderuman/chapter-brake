package instance

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
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

func TestAcquireWithTimeoutWaitsForPriorInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapterbrake.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = first.Close()
		close(released)
	}()
	second, err := AcquireWithTimeout(path, time.Second)
	if err != nil {
		t.Fatalf("AcquireWithTimeout() error = %v", err)
	}
	<-released
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireWithTimeoutPreservesSingleInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapterbrake.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := AcquireWithTimeout(path, 5*time.Millisecond); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("AcquireWithTimeout() error = %v", err)
	}
}
