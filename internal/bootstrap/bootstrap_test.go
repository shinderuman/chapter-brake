package bootstrap

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/config"
	"chapterbrake/internal/process"
	"chapterbrake/internal/runner"
)

type fakeExecutor struct{}

func (fakeExecutor) Run(context.Context, process.Invocation, io.Writer, io.Writer) error {
	return nil
}

type fakeTerminal struct {
	run      func() error
	stopped  chan struct{}
	stopOnce sync.Once
}

func (t *fakeTerminal) Run() error {
	if t.run != nil {
		return t.run()
	}
	return nil
}

func (t *fakeTerminal) Shutdown() {
	t.stopOnce.Do(func() {
		if t.stopped != nil {
			close(t.stopped)
		}
	})
}

func TestRunBuildsApplication(t *testing.T) {
	deps, dataDirectory, inputDirectory, outputDirectory := testDependencies(t)
	var gotService *app.Service
	var gotRunner *runner.Runner
	var gotInitialDirectory string
	deps.workingDirectory = func() (string, error) {
		t.Fatal("working directory was requested without --cwd")
		return "", nil
	}
	deps.newTerminal = func(
		service *app.Service,
		queueRunner *runner.Runner,
		initialDirectory string,
	) (terminal, error) {
		gotService = service
		gotRunner = queueRunner
		gotInitialDirectory = initialDirectory
		return &fakeTerminal{}, nil
	}

	if err := run(context.Background(), deps, runOptions{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if gotService == nil || gotRunner == nil {
		t.Fatal("application dependencies were not assembled")
	}
	if gotService.OutputDirectory != outputDirectory {
		t.Fatalf("output directory = %q", gotService.OutputDirectory)
	}
	if gotInitialDirectory != inputDirectory {
		t.Fatalf("initial directory = %q", gotInitialDirectory)
	}
	if gotRunner.HandBrake != "/tools/HandBrakeCLI" ||
		gotRunner.FFmpeg != "/tools/ffmpeg" ||
		gotRunner.FFProbe != "/tools/ffprobe" ||
		gotRunner.MKVPropEdit != "/tools/mkvpropedit" {
		t.Fatalf("runner tool paths = %#v", gotRunner)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "queue.json")); err != nil {
		t.Fatalf("queue.json: %v", err)
	}
	logs, err := filepath.Glob(filepath.Join(dataDirectory, "logs", "app-*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("app logs = %v, %v", logs, err)
	}
}

func TestRunUsesWorkingDirectoryOption(t *testing.T) {
	deps, dataDirectory, inputDirectory, _ := testDependencies(t)
	workingDirectory := t.TempDir()
	deps.workingDirectory = func() (string, error) {
		return workingDirectory, nil
	}
	settingsPath := filepath.Join(dataDirectory, "settings.json")
	settingsBefore, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var gotInitialDirectory string
	deps.newTerminal = func(
		_ *app.Service,
		_ *runner.Runner,
		initialDirectory string,
	) (terminal, error) {
		gotInitialDirectory = initialDirectory
		return &fakeTerminal{}, nil
	}

	if err := run(context.Background(), deps, runOptions{useWorkingDirectory: true}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if gotInitialDirectory != workingDirectory {
		t.Fatalf("initial directory = %q, want cwd %q; settings was %q",
			gotInitialDirectory, workingDirectory, inputDirectory)
	}
	settingsAfter, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(settingsAfter) != string(settingsBefore) {
		t.Fatal("--cwd changed settings.json")
	}
}

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantCWD bool
		wantErr bool
	}{
		{name: "default"},
		{name: "cwd", args: []string{"--cwd"}, wantCWD: true},
		{name: "unknown", args: []string{"--unknown"}, wantErr: true},
		{name: "positional", args: []string{"input.mkv"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptions(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseOptions() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got.useWorkingDirectory != tt.wantCWD {
				t.Fatalf("useWorkingDirectory = %t, want %t", got.useWorkingDirectory, tt.wantCWD)
			}
		})
	}
}

func TestRunShutsDownTerminalWhenContextIsCanceled(t *testing.T) {
	deps, _, _, _ := testDependencies(t)
	screen := &fakeTerminal{stopped: make(chan struct{})}
	screen.run = func() error {
		<-screen.stopped
		return nil
	}
	deps.newTerminal = func(*app.Service, *runner.Runner, string) (terminal, error) {
		return screen, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, deps, runOptions{})
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() did not stop after cancellation")
	}
}

func TestRunReportsStartupAndTerminalErrors(t *testing.T) {
	expected := errors.New("failure")
	deps, _, _, _ := testDependencies(t)
	deps.homeDirectory = func() (string, error) {
		return "", expected
	}
	if err := run(context.Background(), deps, runOptions{}); !errors.Is(err, expected) ||
		!strings.Contains(err.Error(), "home directory") {
		t.Fatalf("home error = %v", err)
	}

	deps, _, _, _ = testDependencies(t)
	deps.newTerminal = func(*app.Service, *runner.Runner, string) (terminal, error) {
		return &fakeTerminal{run: func() error { return expected }}, nil
	}
	if err := run(context.Background(), deps, runOptions{}); !errors.Is(err, expected) ||
		!strings.Contains(err.Error(), "run TUI") {
		t.Fatalf("terminal error = %v", err)
	}

	deps, _, _, _ = testDependencies(t)
	deps.workingDirectory = func() (string, error) {
		return "", expected
	}
	if err := run(
		context.Background(),
		deps,
		runOptions{useWorkingDirectory: true},
	); !errors.Is(err, expected) || !strings.Contains(err.Error(), "current directory") {
		t.Fatalf("working directory error = %v", err)
	}
}

func testDependencies(t *testing.T) (dependencies, string, string, string) {
	t.Helper()
	home := t.TempDir()
	inputDirectory := t.TempDir()
	outputDirectory := t.TempDir()
	dataDirectory, err := config.DataDirectory(home)
	if err != nil {
		t.Fatal(err)
	}
	settingsStore := config.Store{Path: filepath.Join(dataDirectory, "settings.json")}
	if err := settingsStore.Save(config.Settings{
		Version:         config.Version,
		InputDirectory:  inputDirectory,
		OutputDirectory: outputDirectory,
	}); err != nil {
		t.Fatal(err)
	}

	return dependencies{
		homeDirectory: func() (string, error) {
			return home, nil
		},
		workingDirectory: func() (string, error) {
			return outputDirectory, nil
		},
		now: func() time.Time {
			return time.Date(2026, 7, 26, 10, 0, 0, 0, time.Local)
		},
		executor: fakeExecutor{},
		inspectTools: func(context.Context, process.Executor) ([]app.ToolInfo, error) {
			return []app.ToolInfo{
				{Name: "HandBrakeCLI", Path: "/tools/HandBrakeCLI", Version: "HandBrake 1.11.2"},
				{Name: "ffmpeg", Path: "/tools/ffmpeg", Version: "ffmpeg 8.1.2"},
				{Name: "ffprobe", Path: "/tools/ffprobe", Version: "ffprobe 8.1.2"},
				{Name: "mkvpropedit", Path: "/tools/mkvpropedit", Version: "mkvpropedit 100.0"},
			}, nil
		},
	}, dataDirectory, inputDirectory, outputDirectory
}
