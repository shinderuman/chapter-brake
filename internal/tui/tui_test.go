package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/metadata"
	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type uiStore struct {
	q queue.Queue
}

func (s *uiStore) Load() (queue.Queue, error) { return s.q, nil }
func (s *uiStore) Save(q queue.Queue) error {
	s.q = q
	return nil
}

type uiScanner struct{}

func (uiScanner) Scan(context.Context, string, io.Writer, io.Writer) (media.Info, error) {
	return media.Info{}, nil
}

type uiCatalog struct{}

func (uiCatalog) Curated() []handbrake.Preset { return handbrake.CuratedPresets() }
func (uiCatalog) ListStandard(context.Context, io.Writer, io.Writer) ([]handbrake.StandardPreset, error) {
	return nil, nil
}
func (uiCatalog) Resolve(context.Context, string, io.Writer, io.Writer) (handbrake.Preset, error) {
	return handbrake.Preset{}, nil
}

type uiExecutor struct{}

func (uiExecutor) Run(context.Context, process.Invocation, io.Writer, io.Writer) error { return nil }

type cancelExecutor struct {
	started chan struct{}
}

func (e cancelExecutor) Run(
	ctx context.Context,
	invocation process.Invocation,
	_ io.Writer,
	_ io.Writer,
) error {
	if output := testArgumentAfter(invocation.Args, "-o"); output != "" {
		_ = os.WriteFile(output, []byte("partial"), 0o600)
	}
	select {
	case e.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

type uiProber struct{}

func (uiProber) Probe(context.Context, string, io.Writer, io.Writer) (metadata.Probe, error) {
	return metadata.Probe{}, nil
}

func TestNewBuildsMainMenu(t *testing.T) {
	store := &uiStore{q: queue.Empty()}
	service := &app.Service{
		QueueStore:      store,
		Scanner:         uiScanner{},
		Presets:         uiCatalog{},
		OutputDirectory: t.TempDir(),
	}
	queueRunner := &runner.Runner{
		Store:        store,
		Executor:     uiExecutor{},
		Scanner:      uiScanner{},
		Prober:       uiProber{},
		LogDirectory: t.TempDir(),
	}
	ui, err := New(service, queueRunner, t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if ui.Application() == nil || ui.Pages() == nil {
		t.Fatal("New() returned nil application components")
	}
	name, primitive := ui.Pages().GetFrontPage()
	if name != "main" {
		t.Fatalf("front page = %q", name)
	}
	list, ok := primitive.(*tview.List)
	if !ok {
		t.Fatalf("main primitive = %T", primitive)
	}
	if list.GetItemCount() != 4 {
		t.Fatalf("menu items = %d, want 4", list.GetItemCount())
	}
}

func TestHelpers(t *testing.T) {
	if got := sortedKeys(map[int]bool{3: true, 1: true, 2: false}); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("sortedKeys() = %v", got)
	}
	if got := intSet([]int{2, 4}); !reflect.DeepEqual(got, map[int]bool{2: true, 4: true}) {
		t.Fatalf("intSet() = %v", got)
	}
	if got := formatDuration(2*time.Hour + 3*time.Minute + 4*time.Second); got != "2:03:04" {
		t.Fatalf("formatDuration() = %q", got)
	}
	if got := formatDuration(3*time.Minute + 4*time.Second); got != "3:04" {
		t.Fatalf("formatDuration() = %q", got)
	}
	if got := formatDuration(-time.Second); got != "-" {
		t.Fatalf("formatDuration(-1s) = %q", got)
	}
	for size, want := range map[int64]string{
		42:                     "42 B",
		2 * 1024:               "2.0 KiB",
		2 * 1024 * 1024:        "2.0 MiB",
		2 * 1024 * 1024 * 1024: "2.0 GiB",
	} {
		if got := humanSize(size); got != want {
			t.Fatalf("humanSize(%d) = %q, want %q", size, got, want)
		}
	}
	for channels, want := range map[int]string{
		1: "Mono",
		2: "Stereo",
		6: "5.1ch",
		8: "8ch",
	} {
		if got := channelLabel(channels); got != want {
			t.Fatalf("channelLabel(%d) = %q, want %q", channels, got, want)
		}
	}
}

func TestWorkflowScreensBuildFromDraft(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "日本語 source.mkv")
	if err := os.WriteFile(input, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &uiStore{q: queue.Empty()}
	service := &app.Service{
		QueueStore:      store,
		Scanner:         uiScanner{},
		Presets:         uiCatalog{},
		OutputDirectory: root,
		Now: func() time.Time {
			return time.Date(2026, 7, 26, 9, 0, 0, 0, time.Local)
		},
	}
	queueRunner := &runner.Runner{
		Store:        store,
		Executor:     uiExecutor{},
		Scanner:      uiScanner{},
		Prober:       uiProber{},
		LogDirectory: filepath.Join(root, "logs"),
	}
	ui, err := New(service, queueRunner, root)
	if err != nil {
		t.Fatal(err)
	}
	ui.draft = app.Draft{
		Input: input,
		Media: media.Info{
			Duration: 60 * time.Minute,
			Chapters: []media.Chapter{
				{Number: 1, Start: 0, Title: "chapter 1"},
				{Number: 2, Start: 20 * time.Minute, Title: "chapter 2"},
				{Number: 3, Start: 40 * time.Minute, Title: "chapter 3"},
			},
			AudioTracks: []media.AudioTrack{
				{Number: 1, Language: "jpn", Codec: "ac3", Channels: 6},
				{Number: 2, Language: "jpn", Codec: "aac", Channels: 2},
				{Number: 3, Language: "eng", Codec: "aac", Channels: 2},
			},
			SubtitleTracks: []media.SubtitleTrack{
				{Number: 1, Language: "jpn", Format: "subrip", Name: "日本語"},
				{Number: 2, Language: "eng", Format: "subrip", Name: "English"},
			},
		},
		Base:             "日本語 source",
		SelectedChapters: []int{1, 2},
		AudioTracks:      []int{1, 2},
		Subtitles:        []int{1, 2},
		AutoChapters:     true,
	}

	ui.showFiles(root)
	assertFrontPage(t, ui, "files")
	files := frontPrimitive[*tview.List](t, ui)
	if files.GetItemCount() != 3 {
		t.Fatalf("file list items = %d, want parent, directory, and MKV only", files.GetItemCount())
	}

	ui.showPreset()
	assertFrontPage(t, ui, "preset")
	presets := frontPrimitive[*tview.List](t, ui)
	if presets.GetItemCount() != len(handbrake.CuratedPresets())+1 {
		t.Fatalf("preset items = %d", presets.GetItemCount())
	}

	ui.showStandardPresets([]handbrake.StandardPreset{
		{Name: "Fast 1080p30", Category: "General"},
	})
	assertFrontPage(t, ui, "standard-presets")

	ui.selectPreset(handbrake.CuratedPresets()[1])
	assertFrontPage(t, ui, "naming")
	if ui.draft.StartIndex != 1 {
		t.Fatalf("start index = %d", ui.draft.StartIndex)
	}

	ui.showChapters()
	assertFrontPage(t, ui, "chapters")
	chapters := frontPrimitive[*tview.Form](t, ui)
	if chapters.GetFormItemCount() != len(ui.draft.Media.Chapters) {
		t.Fatalf("chapter items = %d", chapters.GetFormItemCount())
	}

	ui.showAudio()
	assertFrontPage(t, ui, "audio")
	audio := frontPrimitive[*tview.Form](t, ui)
	if audio.GetFormItemCount() != 4 {
		t.Fatalf("audio items = %d, want tracks 1-3 plus policy text", audio.GetFormItemCount())
	}

	ui.showSubtitles()
	assertFrontPage(t, ui, "subtitles")
	subtitles := frontPrimitive[*tview.Form](t, ui)
	if subtitles.GetFormItemCount() != 4 {
		t.Fatalf("subtitle items = %d, want none, two tracks, and burn policy", subtitles.GetFormItemCount())
	}

	ui.showPreview()
	assertFrontPage(t, ui, "preview")
	if len(ui.preview.Jobs) != 2 {
		t.Fatalf("preview jobs = %d", len(ui.preview.Jobs))
	}

	if err := service.AddPreview(ui.preview, true); err != nil {
		t.Fatal(err)
	}
	ui.showQueue()
	assertFrontPage(t, ui, "queue")
	queued := frontPrimitive[*tview.List](t, ui)
	if queued.GetItemCount() != 2 {
		t.Fatalf("queue items = %d", queued.GetItemCount())
	}

	ui.showBusy("busy")
	assertFrontPage(t, ui, "busy")
	ui.showError("error", errors.New("test error"), ui.showMain)
	assertFrontPage(t, ui, "message")
}

func TestWorkflowScreensHandleEmptyQueueAndMP4Preview(t *testing.T) {
	root := t.TempDir()
	store := &uiStore{q: queue.Empty()}
	service := &app.Service{
		QueueStore:      store,
		Scanner:         uiScanner{},
		Presets:         uiCatalog{},
		OutputDirectory: root,
	}
	ui, err := New(service, &runner.Runner{Store: store}, root)
	if err != nil {
		t.Fatal(err)
	}
	ui.showQueue()
	queueList := frontPrimitive[*tview.List](t, ui)
	if queueList.GetItemCount() != 1 {
		t.Fatalf("empty queue items = %d", queueList.GetItemCount())
	}
	ui.startQueue()
	assertFrontPage(t, ui, "message")

	ui.draft = app.Draft{
		Input: filepath.Join(root, "source.mkv"),
		Media: media.Info{
			Duration:    time.Minute,
			Chapters:    []media.Chapter{{Number: 1, Start: 0}},
			AudioTracks: []media.AudioTrack{{Number: 1, Codec: "ac3", Channels: 6}},
		},
		Preset:           handbrake.CuratedPresets()[0],
		Base:             "source",
		StartIndex:       1,
		SelectedChapters: []int{1},
		AudioTracks:      []int{1},
		Subtitles:        []int{},
	}
	ui.showAudio()
	assertFrontPage(t, ui, "audio")
	if _, err := service.BuildPreview(ui.draft); err != nil {
		t.Fatalf("BuildPreview() error = %v", err)
	}
	ui.showPreview()
	assertFrontPage(t, ui, "preview")
}

func TestElapsedForDisplayErrorsAndEmptySelection(t *testing.T) {
	chapters := []media.Chapter{{Number: 1, Start: 0}}
	elapsed, available := elapsedForDisplay(chapters, nil)
	if len(elapsed) != 1 || len(available) != 1 || available[0] {
		t.Fatalf("empty selection = %v, %v", elapsed, available)
	}
	elapsed, available = elapsedForDisplay(chapters, []int{2})
	if len(elapsed) != 1 || len(available) != 1 || available[0] {
		t.Fatalf("invalid selection = %v, %v", elapsed, available)
	}
}

func assertFrontPage(t *testing.T, ui *UI, want string) {
	t.Helper()
	name, _ := ui.Pages().GetFrontPage()
	if name != want {
		t.Fatalf("front page = %q, want %q", name, want)
	}
}

func frontPrimitive[T tview.Primitive](t *testing.T, ui *UI) T {
	t.Helper()
	_, primitive := ui.Pages().GetFrontPage()
	result, ok := primitive.(T)
	if !ok {
		t.Fatalf("front primitive = %T", primitive)
	}
	return result
}

func TestNewValidation(t *testing.T) {
	queueRunner := &runner.Runner{}
	if _, err := New(nil, queueRunner, filepath.Clean("/tmp")); err == nil {
		t.Fatal("New(nil service) error = nil")
	}
	service := &app.Service{}
	service.Presets = uiCatalog{}
	if _, err := New(service, nil, filepath.Clean("/tmp")); err == nil {
		t.Fatal("New(nil runner) error = nil")
	}
	if _, err := New(service, queueRunner, "relative"); err == nil {
		t.Fatal("New(relative) error = nil")
	}
}

func TestUIRunsAndStopsOnSimulationScreen(t *testing.T) {
	store := &uiStore{q: queue.Empty()}
	service := &app.Service{
		QueueStore:      store,
		Scanner:         uiScanner{},
		Presets:         uiCatalog{},
		OutputDirectory: t.TempDir(),
	}
	queueRunner := &runner.Runner{
		Store:        store,
		Executor:     uiExecutor{},
		Scanner:      uiScanner{},
		Prober:       uiProber{},
		LogDirectory: t.TempDir(),
	}
	ui, err := New(service, queueRunner, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(80, 24)
	ui.Application().SetScreen(screen)

	done := make(chan error, 1)
	go func() {
		done <- ui.Run()
	}()
	time.Sleep(20 * time.Millisecond)
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		ui.Stop()
		t.Fatal("UI did not stop after Ctrl+C")
	}
}

func TestCtrlCCancelsRunningQueueAndReturnsWithoutFreeze(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "source.mkv")
	output := filepath.Join(root, "output.mkv")
	if err := os.WriteFile(input, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := queue.Job{
		ID:           "job-1",
		CreatedAt:    time.Now(),
		Input:        input,
		Output:       output,
		Preset:       "1080p MKV",
		Container:    queue.ContainerMKV,
		ChapterStart: 1,
		ChapterEnd:   1,
		AudioTracks:  []int{1},
		Subtitles:    []int{},
	}
	store := &uiStore{q: queue.Queue{Version: queue.Version, Jobs: []queue.Job{job}}}
	scanner := fixedUIScanner{}
	executor := cancelExecutor{started: make(chan struct{}, 1)}
	service := &app.Service{
		QueueStore:      store,
		Scanner:         scanner,
		Presets:         uiCatalog{},
		OutputDirectory: root,
	}
	queueRunner := &runner.Runner{
		Store:        store,
		Executor:     executor,
		Scanner:      scanner,
		Prober:       uiProber{},
		LogDirectory: filepath.Join(root, "logs"),
	}
	ui, err := New(service, queueRunner, root)
	if err != nil {
		t.Fatal(err)
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(80, 24)
	ui.Application().SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- ui.Run() }()
	time.Sleep(20 * time.Millisecond)
	ui.Application().QueueUpdateDraw(ui.startQueue)

	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		ui.Stop()
		t.Fatal("runner did not start")
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone))

	deadline := time.Now().Add(2 * time.Second)
	for {
		ui.mu.Lock()
		running := ui.running
		ui.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			ui.Stop()
			t.Fatal("running state did not clear after cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(store.q.Jobs) != 1 {
		t.Fatalf("queue head removed after cancellation: %#v", store.q)
	}
	paths, _ := metadata.TemporaryPaths(job)
	if _, err := os.Stat(paths.Encode); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial encode remains: %v", err)
	}
	ui.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UI did not stop")
	}
}

type fixedUIScanner struct{}

func (fixedUIScanner) Scan(context.Context, string, io.Writer, io.Writer) (media.Info, error) {
	return media.Info{
		Duration:    time.Second,
		Chapters:    []media.Chapter{{Number: 1, Start: 0}},
		AudioTracks: []media.AudioTrack{{Number: 1, Codec: "ac3", Channels: 6}},
	}, nil
}

func testArgumentAfter(args []string, key string) string {
	for i, arg := range args {
		if arg == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
