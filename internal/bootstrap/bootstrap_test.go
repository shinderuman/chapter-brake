package bootstrap

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/config"
	"chapterbrake/internal/instance"
	"chapterbrake/internal/media"
	"chapterbrake/internal/process"
	"chapterbrake/internal/webapi"
)

type fakeExecutor struct{}

func (fakeExecutor) Run(context.Context, process.Invocation, io.Writer, io.Writer) error {
	return nil
}

type fakeWebBackend struct {
	serve func(context.Context) error
}

func (backend *fakeWebBackend) Serve(ctx context.Context) error {
	if backend.serve != nil {
		return backend.serve(ctx)
	}
	return nil
}

func TestRunBuildsApplication(t *testing.T) {
	deps, dataDirectory, inputDirectory, outputDirectory := testDependencies(t)
	var gotService *app.Service
	var gotInitialDirectory string
	deps.absolutePath = func(string) (string, error) {
		t.Fatal("absolute path was requested without a directory option")
		return "", nil
	}
	deps.newWebBackend = func(config webapi.Config) (webBackend, error) {
		gotService = config.Application.(*app.Service)
		gotInitialDirectory = config.InitialDirectory
		return &fakeWebBackend{}, nil
	}

	if err := runWeb(context.Background(), deps, runOptions{}, filepath.Join(t.TempDir(), "backend.sock")); err != nil {
		t.Fatalf("runWeb() error = %v", err)
	}
	if gotService == nil {
		t.Fatal("application dependencies were not assembled")
	}
	if gotService.OutputDirectory != outputDirectory {
		t.Fatalf("output directory = %q", gotService.OutputDirectory)
	}
	if gotService.ChapterInterval != media.DefaultEpisodeInterval {
		t.Fatalf("chapter interval = %s", gotService.ChapterInterval)
	}
	if gotInitialDirectory != inputDirectory {
		t.Fatalf("initial directory = %q", gotInitialDirectory)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "queue.json")); err != nil {
		t.Fatalf("queue.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, "state.json")); err != nil {
		t.Fatalf("state.json: %v", err)
	}
	home := filepath.Dir(filepath.Dir(dataDirectory))
	logDirectory, err := config.LogDirectory(home)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := filepath.Glob(filepath.Join(logDirectory, "app-*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("app logs = %v, %v", logs, err)
	}
}

func TestRunLoadsExportedMyPresets(t *testing.T) {
	deps, dataDirectory, _, _ := testDependencies(t)
	presetPath := filepath.Join(dataDirectory, "My Presets.json")
	presetJSON := `{
		"PresetList": [{
			"PresetName": "My Presets",
			"Folder": true,
			"ChildrenArray": [{
				"PresetName": "Custom GUI MKV",
				"FileFormat": "av_mkv",
				"PictureWidth": 1920,
				"PictureHeight": 1080,
				"PictureCropMode": 2
			}]
		}]
	}`
	if err := os.WriteFile(presetPath, []byte(presetJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotService *app.Service
	deps.newWebBackend = func(config webapi.Config) (webBackend, error) {
		gotService = config.Application.(*app.Service)
		return &fakeWebBackend{}, nil
	}

	if err := runWeb(context.Background(), deps, runOptions{}, filepath.Join(t.TempDir(), "backend.sock")); err != nil {
		t.Fatalf("runWeb() error = %v", err)
	}
	presets := gotService.Presets.Curated()
	if len(presets) != 1 || presets[0].DisplayName != "Custom GUI MKV" ||
		presets[0].ImportFile != presetPath {
		t.Fatalf("curated presets = %#v", presets)
	}
}

func TestRunWebBuildsBackend(t *testing.T) {
	deps, _, inputDirectory, _ := testDependencies(t)
	var gotConfig webapi.Config
	deps.newWebBackend = func(config webapi.Config) (webBackend, error) {
		gotConfig = config
		return &fakeWebBackend{}, nil
	}
	socket := filepath.Join(t.TempDir(), "chapterbrake.sock")
	if err := runWeb(context.Background(), deps, runOptions{}, socket); err != nil {
		t.Fatalf("runWeb() error = %v", err)
	}
	if gotConfig.Socket != socket || gotConfig.InitialDirectory != inputDirectory {
		t.Fatalf("web config = %#v", gotConfig)
	}
	if gotConfig.Application == nil || gotConfig.Presets == nil || gotConfig.Controller == nil || gotConfig.Logger == nil {
		t.Fatalf("web dependencies are incomplete: %#v", gotConfig)
	}
}

func TestRunUsesInputDirectoryOption(t *testing.T) {
	deps, dataDirectory, inputDirectory, _ := testDependencies(t)
	overrideDirectory := t.TempDir()
	var gotPathArgument string
	deps.absolutePath = func(path string) (string, error) {
		gotPathArgument = path
		return overrideDirectory, nil
	}
	settingsPath := filepath.Join(dataDirectory, "settings.json")
	settingsBefore, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var gotInitialDirectory string
	deps.newWebBackend = func(config webapi.Config) (webBackend, error) {
		gotInitialDirectory = config.InitialDirectory
		return &fakeWebBackend{}, nil
	}

	if err := runWeb(
		context.Background(),
		deps,
		runOptions{inputDirectory: "."},
		filepath.Join(t.TempDir(), "backend.sock"),
	); err != nil {
		t.Fatalf("runWeb() error = %v", err)
	}
	if gotPathArgument != "." {
		t.Fatalf("absolute path argument = %q, want %q", gotPathArgument, ".")
	}
	if gotInitialDirectory != overrideDirectory {
		t.Fatalf("initial directory = %q, want override %q; settings was %q",
			gotInitialDirectory, overrideDirectory, inputDirectory)
	}
	settingsAfter, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(settingsAfter) != string(settingsBefore) {
		t.Fatal("directory option changed settings.json")
	}
}

func TestRunRejectsInvalidInputDirectoryOption(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "input.mkv")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		errText string
	}{
		{name: "missing", path: filepath.Join(root, "missing"), errText: "stat input directory"},
		{name: "file", path: file, errText: "input path is not a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, _, _, _ := testDependencies(t)
			deps.absolutePath = func(string) (string, error) {
				return tt.path, nil
			}
			err := runWeb(
				context.Background(),
				deps,
				runOptions{inputDirectory: "provided"},
				filepath.Join(t.TempDir(), "backend.sock"),
			)
			if err == nil || !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("runWeb() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}

func TestRunRequiresLocalWebSocket(t *testing.T) {
	t.Setenv("LOCAL_WEB_SOCKET", "")
	err := Run(nil)
	if err == nil || !strings.Contains(err.Error(), "Local Web App Server") {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantDirectory string
		wantErr       bool
	}{
		{name: "default"},
		{name: "long", args: []string{"--directory", "/input"}, wantDirectory: "/input"},
		{name: "short", args: []string{"-d", "."}, wantDirectory: "."},
		{name: "long equals", args: []string{"--directory=relative"}, wantDirectory: "relative"},
		{name: "missing value", args: []string{"--directory"}, wantErr: true},
		{name: "empty value", args: []string{"--directory="}, wantErr: true},
		{name: "removed cwd option", args: []string{"--cwd"}, wantErr: true},
		{name: "unknown", args: []string{"--unknown"}, wantErr: true},
		{name: "positional", args: []string{"input.mkv"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptions(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseOptions() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got.inputDirectory != tt.wantDirectory {
				t.Fatalf("inputDirectory = %q, want %q", got.inputDirectory, tt.wantDirectory)
			}
		})
	}
}

func TestRunShutsDownBackendWhenContextIsCanceled(t *testing.T) {
	deps, _, _, _ := testDependencies(t)
	deps.newWebBackend = func(webapi.Config) (webBackend, error) {
		return &fakeWebBackend{serve: func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	socket := filepath.Join(t.TempDir(), "backend.sock")
	go func() {
		done <- runWeb(ctx, deps, runOptions{}, socket)
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWeb() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runWeb() did not stop after cancellation")
	}
}

func TestRunImmediatelyShutsDownWhenHostLifetimeEnds(t *testing.T) {
	deps, _, _, _ := testDependencies(t)
	hostExpired := make(chan struct{})
	deps.hostExpired = hostExpired
	backendStarted := make(chan struct{})
	deps.newWebBackend = func(webapi.Config) (webBackend, error) {
		return &fakeWebBackend{serve: func(ctx context.Context) error {
			close(backendStarted)
			<-ctx.Done()
			return nil
		}}, nil
	}
	done := make(chan error, 1)
	go func() {
		done <- runWeb(context.Background(), deps, runOptions{}, filepath.Join(t.TempDir(), "backend.sock"))
	}()
	<-backendStarted
	close(hostExpired)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWeb() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runWeb() did not stop when its host lifetime ended")
	}
}

func TestRunRejectsSecondInstance(t *testing.T) {
	deps, dataDirectory, _, _ := testDependencies(t)
	lock, err := instance.Acquire(filepath.Join(dataDirectory, "chapterbrake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	err = runWeb(context.Background(), deps, runOptions{}, filepath.Join(t.TempDir(), "backend.sock"))
	if !errors.Is(err, instance.ErrAlreadyRunning) {
		t.Fatalf("runWeb() error = %v", err)
	}
}

func TestRunReportsStartupAndBackendErrors(t *testing.T) {
	expected := errors.New("failure")
	deps, _, _, _ := testDependencies(t)
	deps.homeDirectory = func() (string, error) {
		return "", expected
	}
	if err := runWeb(
		context.Background(),
		deps,
		runOptions{},
		filepath.Join(t.TempDir(), "backend.sock"),
	); !errors.Is(err, expected) ||
		!strings.Contains(err.Error(), "home directory") {
		t.Fatalf("home error = %v", err)
	}

	deps, _, _, _ = testDependencies(t)
	deps.newWebBackend = func(webapi.Config) (webBackend, error) {
		return &fakeWebBackend{serve: func(context.Context) error { return expected }}, nil
	}
	if err := runWeb(
		context.Background(),
		deps,
		runOptions{},
		filepath.Join(t.TempDir(), "backend.sock"),
	); !errors.Is(err, expected) {
		t.Fatalf("backend error = %v", err)
	}

	deps, _, _, _ = testDependencies(t)
	deps.absolutePath = func(string) (string, error) {
		return "", expected
	}
	if err := runWeb(
		context.Background(),
		deps,
		runOptions{inputDirectory: "."},
		filepath.Join(t.TempDir(), "backend.sock"),
	); !errors.Is(err, expected) || !strings.Contains(err.Error(), "input directory") {
		t.Fatalf("input directory resolution error = %v", err)
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
		ChapterInterval: config.DefaultChapterInterval,
	}); err != nil {
		t.Fatal(err)
	}

	return dependencies{
		homeDirectory: func() (string, error) {
			return home, nil
		},
		absolutePath: func(path string) (string, error) {
			return filepath.Abs(path)
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
