package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/config"
	"chapterbrake/internal/control"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/instance"
	"chapterbrake/internal/logging"
	"chapterbrake/internal/media"
	"chapterbrake/internal/metadata"
	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"
	"chapterbrake/internal/runstate"
	"chapterbrake/internal/tui"
	"chapterbrake/internal/webapi"
)

type terminal interface {
	Run() error
	Shutdown()
}

type webBackend interface {
	Serve(context.Context) error
}

type dependencies struct {
	homeDirectory func() (string, error)
	absolutePath  func(string) (string, error)
	now           func() time.Time
	executor      process.Executor
	jobExecutor   process.Executor
	inspectTools  func(context.Context, process.Executor) ([]app.ToolInfo, error)
	newTerminal   func(*app.Service, *runner.Runner, string) (terminal, error)
	newWebBackend func(webapi.Config) (webBackend, error)
}

type runOptions struct {
	inputDirectory string
}

// Run initializes and runs ChapterBrake until the user exits or macOS asks it
// to terminate.
func Run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if socket := os.Getenv("LOCAL_WEB_SOCKET"); socket != "" {
		return runWeb(ctx, productionDependencies(), opts, socket)
	}
	return run(ctx, productionDependencies(), opts)
}

func parseOptions(args []string) (runOptions, error) {
	flags := flag.NewFlagSet("chapterbrake", flag.ContinueOnError)
	var parsed runOptions
	const directoryUsage = "start input file selection in this directory"
	flags.StringVar(&parsed.inputDirectory, "directory", "", directoryUsage)
	flags.StringVar(&parsed.inputDirectory, "d", "", directoryUsage+" (shorthand)")
	if err := flags.Parse(args); err != nil {
		return runOptions{}, err
	}
	if flags.NArg() != 0 {
		return runOptions{}, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	directorySpecified := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == "directory" || current.Name == "d" {
			directorySpecified = true
		}
	})
	if directorySpecified && parsed.inputDirectory == "" {
		return runOptions{}, errors.New("directory option must not be empty")
	}
	return parsed, nil
}

func productionDependencies() dependencies {
	return dependencies{
		homeDirectory: os.UserHomeDir,
		absolutePath:  filepath.Abs,
		now:           time.Now,
		executor:      &process.OSExecutor{},
		jobExecutor:   &process.OSExecutor{},
		inspectTools:  app.InspectTools,
		newTerminal: func(
			service *app.Service,
			queueRunner *runner.Runner,
			initialDirectory string,
		) (terminal, error) {
			return tui.New(service, queueRunner, initialDirectory)
		},
		newWebBackend: func(config webapi.Config) (webBackend, error) {
			return webapi.New(config)
		},
	}
}

func run(ctx context.Context, deps dependencies, opts runOptions) error {
	return runWithFrontend(ctx, deps, opts, func(
		ctx context.Context,
		service *app.Service,
		queueRunner *runner.Runner,
		initialDirectory string,
		_ *slog.Logger,
	) error {
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
		return nil
	})
}

func runWeb(ctx context.Context, deps dependencies, opts runOptions, socket string) error {
	return runWithFrontend(ctx, deps, opts, func(
		ctx context.Context,
		service *app.Service,
		queueRunner *runner.Runner,
		initialDirectory string,
		logger *slog.Logger,
	) error {
		controller, err := control.New(service, queueRunner)
		if err != nil {
			return err
		}
		backend, err := deps.newWebBackend(webapi.Config{
			Socket:           socket,
			InitialDirectory: initialDirectory,
			Application:      service,
			Presets:          service.Presets,
			Controller:       controller,
			Logger:           logger,
		})
		if err != nil {
			return err
		}
		return backend.Serve(ctx)
	})
}

func runWithFrontend(
	ctx context.Context,
	deps dependencies,
	opts runOptions,
	frontend func(context.Context, *app.Service, *runner.Runner, string, *slog.Logger) error,
) error {
	home, err := deps.homeDirectory()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	dataDirectory, err := config.DataDirectory(home)
	if err != nil {
		return err
	}
	logDirectory, err := config.LogDirectory(home)
	if err != nil {
		return err
	}
	appLock, err := instance.Acquire(filepath.Join(dataDirectory, "chapterbrake.lock"))
	if err != nil {
		return err
	}
	defer appLock.Close()

	settingsStore := config.Store{Path: filepath.Join(dataDirectory, "settings.json")}
	settings, err := settingsStore.LoadOrCreate(config.DefaultSettings())
	if err != nil {
		return err
	}
	chapterInterval, err := media.ParseChapterInterval(settings.ChapterInterval)
	if err != nil {
		return fmt.Errorf("parse configured chapter interval: %w", err)
	}
	initialDirectory := settings.InputDirectory
	initialDirectorySource := "settings"
	if opts.inputDirectory != "" {
		initialDirectory, err = deps.absolutePath(opts.inputDirectory)
		if err != nil {
			return fmt.Errorf("resolve input directory %q: %w", opts.inputDirectory, err)
		}
		info, err := os.Stat(initialDirectory)
		if err != nil {
			return fmt.Errorf("stat input directory %s: %w", initialDirectory, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("input path is not a directory: %s", initialDirectory)
		}
		initialDirectorySource = "command-line"
	}
	queueStore := &queue.Store{Path: filepath.Join(dataDirectory, "queue.json")}
	if _, err := queueStore.LoadOrCreate(); err != nil {
		return err
	}
	stateStore := &runstate.Store{Path: filepath.Join(dataDirectory, "state.json")}
	if _, err := stateStore.RecoverInterrupted(deps.now()); err != nil {
		return err
	}

	appLogger, appLogCloser, appLogPath, err := logging.OpenApp(logDirectory, deps.now())
	if err != nil {
		return err
	}
	defer appLogCloser.Close()
	appLogger.Info("application starting",
		"data_directory", dataDirectory,
		"input_directory", settings.InputDirectory,
		"output_directory", settings.OutputDirectory,
		"chapter_interval", settings.ChapterInterval,
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
	presetPath := filepath.Join(dataDirectory, "My Presets.json")
	myPresets, err := handbrake.LoadPresetFileOrDefaults(presetPath)
	if err != nil {
		return err
	}
	catalog := handbrake.Catalog{
		Executor:  deps.executor,
		HandBrake: handBrakePath,
		MyPresets: myPresets,
	}
	prober := metadata.Prober{Executor: deps.executor, FFProbe: ffprobePath}
	jobExecutor := deps.jobExecutor
	if jobExecutor == nil {
		jobExecutor = deps.executor
	}
	queueRunner := &runner.Runner{
		Store:        queueStore,
		Executor:     jobExecutor,
		Scanner:      scanner,
		Prober:       prober,
		LogDirectory: logDirectory,
		AppLogger:    appLogger,
		State:        stateStore,
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
		ChapterInterval: chapterInterval,
	}
	appLogger.Info("input browser initialized",
		"directory", initialDirectory,
		"source", initialDirectorySource,
	)
	if err := frontend(ctx, service, queueRunner, initialDirectory, appLogger); err != nil {
		return err
	}
	appLogger.Info("application stopped")
	return nil
}
