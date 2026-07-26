package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/config"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/logging"
	"chapterbrake/internal/media"
	"chapterbrake/internal/metadata"
	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"
	"chapterbrake/internal/tui"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "chapterbrake:", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	dataDirectory, err := config.DataDirectory(home)
	if err != nil {
		return err
	}
	settingsStore := config.Store{Path: filepath.Join(dataDirectory, "settings.json")}
	settings, err := settingsStore.LoadOrCreate(config.DefaultSettings())
	if err != nil {
		return err
	}
	queueStore := queue.Store{Path: filepath.Join(dataDirectory, "queue.json")}
	if _, err := queueStore.LoadOrCreate(); err != nil {
		return err
	}

	logDirectory := filepath.Join(dataDirectory, "logs")
	appLogger, appLogCloser, appLogPath, err := logging.OpenApp(logDirectory, now())
	if err != nil {
		return err
	}
	defer appLogCloser.Close()
	appLogger.Info("application starting",
		"data_directory", dataDirectory,
		"output_directory", settings.OutputDirectory,
		"app_log", appLogPath,
	)

	commandExecutor := process.OSExecutor{}
	tools, err := app.InspectTools(context.Background(), commandExecutor)
	if err != nil {
		return err
	}
	for _, tool := range tools {
		appLogger.Info("external tool", "name", tool.Name, "path", tool.Path, "version", tool.Version)
	}
	handBrakePath := app.ToolPath(tools, "HandBrakeCLI")
	ffmpegPath := app.ToolPath(tools, "ffmpeg")
	ffprobePath := app.ToolPath(tools, "ffprobe")
	mkvPropEditPath := app.ToolPath(tools, "mkvpropedit")

	scanner := media.Scanner{Executor: commandExecutor, HandBrake: handBrakePath}
	catalog := handbrake.Catalog{Executor: commandExecutor, HandBrake: handBrakePath}
	prober := metadata.Prober{Executor: commandExecutor, FFProbe: ffprobePath}
	queueRunner := &runner.Runner{
		Store:        queueStore,
		Executor:     commandExecutor,
		Scanner:      scanner,
		Prober:       prober,
		LogDirectory: logDirectory,
		AppLogger:    appLogger,
		HandBrake:    handBrakePath,
		FFmpeg:       ffmpegPath,
		FFProbe:      ffprobePath,
		MKVPropEdit:  mkvPropEditPath,
	}
	service := &app.Service{
		QueueStore:      queueStore,
		Scanner:         scanner,
		Presets:         catalog,
		OutputDirectory: settings.OutputDirectory,
	}
	initialDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve current directory: %w", err)
	}
	ui, err := tui.New(service, queueRunner, initialDirectory)
	if err != nil {
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		<-signals
		ui.Shutdown()
	}()

	if err := ui.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	appLogger.Info("application stopped")
	return nil
}

var now = func() time.Time {
	return time.Now()
}
