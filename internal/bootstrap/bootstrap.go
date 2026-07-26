package bootstrap

import (
	"context"
	"flag"
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

type terminal interface {
	Run() error
	Shutdown()
}

type dependencies struct {
	homeDirectory    func() (string, error)
	workingDirectory func() (string, error)
	now              func() time.Time
	executor         process.Executor
	inspectTools     func(context.Context, process.Executor) ([]app.ToolInfo, error)
	newTerminal      func(*app.Service, *runner.Runner, string) (terminal, error)
}

type runOptions struct {
	useWorkingDirectory bool
}

// Run initializes and runs ChapterBrake until the user exits or macOS asks it
// to terminate.
func Run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, productionDependencies(), opts)
}

func parseOptions(args []string) (runOptions, error) {
	flags := flag.NewFlagSet("chapterbrake", flag.ContinueOnError)
	var parsed runOptions
	flags.BoolVar(
		&parsed.useWorkingDirectory,
		"cwd",
		false,
		"use the current working directory instead of settings.json input_directory",
	)
	if err := flags.Parse(args); err != nil {
		return runOptions{}, err
	}
	if flags.NArg() != 0 {
		return runOptions{}, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	return parsed, nil
}

func productionDependencies() dependencies {
	return dependencies{
		homeDirectory:    os.UserHomeDir,
		workingDirectory: os.Getwd,
		now:              time.Now,
		executor:         process.OSExecutor{},
		inspectTools:     app.InspectTools,
		newTerminal: func(
			service *app.Service,
			queueRunner *runner.Runner,
			initialDirectory string,
		) (terminal, error) {
			return tui.New(service, queueRunner, initialDirectory)
		},
	}
}

func run(ctx context.Context, deps dependencies, opts runOptions) error {
	home, err := deps.homeDirectory()
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
	appLogger, appLogCloser, appLogPath, err := logging.OpenApp(logDirectory, deps.now())
	if err != nil {
		return err
	}
	defer appLogCloser.Close()
	appLogger.Info("application starting",
		"data_directory", dataDirectory,
		"input_directory", settings.InputDirectory,
		"output_directory", settings.OutputDirectory,
		"app_log", appLogPath,
	)

	tools, err := deps.inspectTools(ctx, deps.executor)
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

	scanner := media.Scanner{Executor: deps.executor, HandBrake: handBrakePath}
	catalog := handbrake.Catalog{Executor: deps.executor, HandBrake: handBrakePath}
	prober := metadata.Prober{Executor: deps.executor, FFProbe: ffprobePath}
	queueRunner := &runner.Runner{
		Store:        queueStore,
		Executor:     deps.executor,
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
	initialDirectory := settings.InputDirectory
	initialDirectorySource := "settings"
	if opts.useWorkingDirectory {
		initialDirectory, err = deps.workingDirectory()
		if err != nil {
			return fmt.Errorf("resolve current directory: %w", err)
		}
		initialDirectorySource = "cwd"
	}
	appLogger.Info("input browser initialized",
		"directory", initialDirectory,
		"source", initialDirectorySource,
	)
	screen, err := deps.newTerminal(service, queueRunner, initialDirectory)
	if err != nil {
		return err
	}

	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			screen.Shutdown()
		case <-finished:
		}
	}()

	if err := screen.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	appLogger.Info("application stopped")
	return nil
}
