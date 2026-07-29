package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"chapterbrake-web-poc/chapter-brake/internal/backend"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "chapterbrake-poc-backend:", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	socket := os.Getenv("LOCAL_WEB_SOCKET")
	queuePath := os.Getenv("CHAPTERBRAKE_QUEUE_PATH")
	if queuePath == "" {
		queuePath = filepath.Join(home, "Documents", "ChapterBrake", "queue.json")
	}
	statePath := os.Getenv("CHAPTERBRAKE_STATE_PATH")
	if statePath == "" {
		statePath = filepath.Join(home, "Documents", "ChapterBrake", "state.json")
	}
	server, err := backend.New(backend.Config{
		Socket:       socket,
		QueuePath:    queuePath,
		StatePath:    statePath,
		CancelMarker: os.Getenv("CHAPTERBRAKE_CANCEL_MARKER"),
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Serve(ctx)
}
