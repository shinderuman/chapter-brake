package bootstrap

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

const hostLifetimeFDEnvironment = "LOCAL_WEB_LIFETIME_FD"

type hostLifetime struct {
	file     *os.File
	expired  chan struct{}
	stop     chan struct{}
	once     sync.Once
	closeErr error
}

func openHostLifetime(value string) (*hostLifetime, error) {
	if value == "" {
		return nil, nil
	}
	fd, err := strconv.Atoi(value)
	if err != nil || fd < 3 {
		return nil, fmt.Errorf("%s must identify an inherited file descriptor", hostLifetimeFDEnvironment)
	}
	file := os.NewFile(uintptr(fd), "local-web-host-lifetime")
	if file == nil {
		return nil, fmt.Errorf("open inherited host lifetime descriptor %d", fd)
	}
	return watchHostLifetime(file), nil
}

func watchHostLifetime(file *os.File) *hostLifetime {
	lifetime := &hostLifetime{
		file:    file,
		expired: make(chan struct{}),
		stop:    make(chan struct{}),
	}
	go func() {
		_, _ = io.Copy(io.Discard, file)
		select {
		case <-lifetime.stop:
		default:
			close(lifetime.expired)
		}
	}()
	return lifetime
}

func (lifetime *hostLifetime) Done() <-chan struct{} {
	if lifetime == nil {
		return nil
	}
	return lifetime.expired
}

func (lifetime *hostLifetime) Close() error {
	if lifetime == nil {
		return nil
	}
	lifetime.once.Do(func() {
		close(lifetime.stop)
		lifetime.closeErr = lifetime.file.Close()
	})
	return lifetime.closeErr
}
