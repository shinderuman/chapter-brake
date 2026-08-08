package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

var ErrAlreadyRunning = errors.New("ChapterBrake is already running")

type Lock struct {
	file *os.File
}

func Acquire(path string) (*Lock, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("lock path must be absolute: %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("acquire instance lock: %w", err)
	}
	return &Lock{file: file}, nil
}

func AcquireWithTimeout(path string, timeout time.Duration) (*Lock, error) {
	deadline := time.Now().Add(timeout)
	for {
		lock, err := Acquire(path)
		if err == nil || !errors.Is(err, ErrAlreadyRunning) || time.Now().After(deadline) {
			return lock, err
		}
		remaining := time.Until(deadline)
		if remaining > 50*time.Millisecond {
			remaining = 50 * time.Millisecond
		}
		time.Sleep(remaining)
	}
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}
