package instance

import (
	"context"
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

func TestAcquireContextStopsWaitingWhenCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapterbrake.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := AcquireContext(ctx, path, time.Second)
		done <- err
	}()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("AcquireContext() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AcquireContext() did not stop after cancellation")
	}
}

func TestAcquireContextDoesNotAcquireAfterCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapterbrake.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	lock, err := AcquireContext(ctx, path, time.Millisecond)
	if lock != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("AcquireContext() = %#v, %v", lock, err)
	}
}
