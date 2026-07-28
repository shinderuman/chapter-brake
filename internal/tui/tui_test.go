package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/metadata"
	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"
	"chapterbrake/internal/runstate"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type uiStore struct {
	q      queue.Queue
	active string
}

func (s *uiStore) Load() (queue.Queue, error) { return s.q, nil }
func (s *uiStore) AppendJobs(jobs ...queue.Job) error {
	next, err := s.q.Append(jobs...)
	if err == nil {
		s.q = next
	}
	return err
}
func (s *uiStore) DeleteJob(id string) error {
	if s.active == id {
		return errors.New("job is active")
	}
	next, err := s.q.RemoveJob(id)
	if err == nil {
		s.q = next
	}
	return err
}
func (s *uiStore) MoveJob(id string, delta int) error {
	next, err := s.q.MoveJob(id, delta)
	if err == nil {
		s.q = next
	}
	return err
}
func (s *uiStore) ClaimHead() (queue.Job, bool, error) {
	if s.active != "" {
		return queue.Job{}, false, errors.New("job already active")
	}
	head, ok := s.q.Peek()
	if ok {
		s.active = head.ID
	}
	return head, ok, nil
}
func (s *uiStore) ReleaseHead(id string) error {
	if s.active != id {
		return errors.New("active job changed")
	}
	s.active = ""
	return nil
}
func (s *uiStore) CompleteHead(id string) error {
	head, ok := s.q.Peek()
	if !ok || head.ID != id {
		return errors.New("queue head changed")
	}
	if s.active != id {
		return errors.New("job is not active")
	}
	next, err := s.q.RemoveHead()
	if err == nil {
		s.q = next
		s.active = ""
	}
	return err
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

type uiPausableExecutor struct {
	paused bool
}

func (*uiPausableExecutor) Run(context.Context, process.Invocation, io.Writer, io.Writer) error {
	return nil
}

func (e *uiPausableExecutor) Pause() error {
	e.paused = true
	return nil
}

func (e *uiPausableExecutor) Resume() error {
	e.paused = false
	return nil
}

func (e *uiPausableExecutor) IsPaused() bool {
	return e.paused
}

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
	layout, ok := primitive.(*tview.Flex)
	if !ok {
		t.Fatalf("main primitive = %T", primitive)
	}
	list, ok := layout.GetItem(0).(*tview.List)
	if !ok {
		t.Fatalf("main menu = %T", layout.GetItem(0))
	}
	if list.GetItemCount() != 3 {
		t.Fatalf("menu items = %d, want 3", list.GetItemCount())
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

func TestQueueOverviewDurationsAndETA(t *testing.T) {
	jobs := []queue.Job{
		{ID: "first", Output: "/output/first.mkv", DurationSeconds: 1420},
		{ID: "second", Output: "/output/second.mkv", DurationSeconds: 600},
	}
	q := queue.Queue{Version: queue.Version, Jobs: jobs}
	snapshot := queueSnapshot{
		running:         true,
		currentJobID:    "first",
		currentProgress: 0.5,
		currentETA:      time.Minute,
		speedFactor:     2,
	}
	eta, ok := estimateQueueETA(q, snapshot)
	if !ok || eta != 21*time.Minute {
		t.Fatalf("estimateQueueETA() = %s, %t", eta, ok)
	}
	text := formatQueueOverview(q, snapshot)
	for _, want := range []string{"全体ETA 約21:00", "約23:40", "約10:00", "first.mkv", "second.mkv"} {
		if !strings.Contains(text, want) {
			t.Fatalf("queue overview %q does not contain %q", text, want)
		}
	}
}

func TestPersistentFailureMakesMainAndQueueBordersRed(t *testing.T) {
	root := t.TempDir()
	job := queue.Job{
		ID:           "job-1",
		CreatedAt:    time.Now(),
		Input:        filepath.Join(root, "source.mkv"),
		Output:       filepath.Join(root, "output.mkv"),
		Preset:       "MKV Presets",
		Container:    queue.ContainerMKV,
		ChapterStart: 1,
		ChapterEnd:   1,
		AudioTracks:  []int{1},
		Subtitles:    []int{},
	}
	store := &uiStore{q: queue.Queue{Version: queue.Version, Jobs: []queue.Job{job}}}
	stateStore := &runstate.Store{Path: filepath.Join(root, "state.json")}
	if _, err := stateStore.LoadOrCreate(); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.MarkFailed(job, "handbrake", "I/O error", "/logs/job.log", time.Now()); err != nil {
		t.Fatal(err)
	}
	ui, err := New(
		&app.Service{QueueStore: store, Presets: uiCatalog{}, OutputDirectory: root},
		&runner.Runner{Store: store, State: stateStore},
		root,
	)
	if err != nil {
		t.Fatal(err)
	}
	main := frontPrimitive[*tview.Flex](t, ui)
	if main.GetBorderColor() != tcell.ColorRed || !strings.Contains(main.GetTitle(), "異常停止") {
		t.Fatalf("main alert border/title = %v, %q", main.GetBorderColor(), main.GetTitle())
	}
	ui.showQueue()
	queueList := frontPrimitive[*tview.List](t, ui)
	if queueList.GetBorderColor() != tcell.ColorRed || !strings.Contains(queueList.GetTitle(), "異常停止") {
		t.Fatalf("queue alert border/title = %v, %q", queueList.GetBorderColor(), queueList.GetTitle())
	}
}

func TestRunningQueueDetailPausesAndResumesCurrentEncode(t *testing.T) {
	root := t.TempDir()
	job := queue.Job{
		ID:              "job-1",
		CreatedAt:       time.Now(),
		Input:           filepath.Join(root, "source.mkv"),
		Output:          filepath.Join(root, "output.mkv"),
		Preset:          "MKV Presets",
		Container:       queue.ContainerMKV,
		ChapterStart:    1,
		ChapterEnd:      1,
		DurationSeconds: 1420,
		AudioTracks:     []int{1},
		Subtitles:       []int{},
	}
	store := &uiStore{q: queue.Queue{Version: queue.Version, Jobs: []queue.Job{job}}}
	executor := &uiPausableExecutor{}
	ui, err := New(
		&app.Service{QueueStore: store, Presets: uiCatalog{}, OutputDirectory: root},
		&runner.Runner{Store: store, Executor: executor},
		root,
	)
	if err != nil {
		t.Fatal(err)
	}
	ui.running = true
	ui.currentJobID = job.ID
	ui.currentStage = "handbrake"
	ui.currentStartedAt = time.Now()
	ui.showQueueJob(job)

	detail := frontPrimitive[*tview.Flex](t, ui)
	controls := detail.GetItem(1).(*tview.Form)
	if got := controls.GetButton(0).GetLabel(); got != "エンコードを一時停止" {
		t.Fatalf("pause button = %q", got)
	}
	controls.GetButton(0).InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		func(tview.Primitive) {},
	)
	if !executor.paused || !ui.encodingPaused {
		t.Fatalf("pause state = executor:%t UI:%t", executor.paused, ui.encodingPaused)
	}
	detail = frontPrimitive[*tview.Flex](t, ui)
	controls = detail.GetItem(1).(*tview.Form)
	if got := controls.GetButton(0).GetLabel(); got != "エンコードを再開" {
		t.Fatalf("resume button = %q", got)
	}
	controls.GetButton(0).InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		func(tview.Primitive) {},
	)
	if executor.paused || ui.encodingPaused {
		t.Fatalf("resume state = executor:%t UI:%t", executor.paused, ui.encodingPaused)
	}
}

func TestListNavigation(t *testing.T) {
	backCalls := 0
	escapeCalls := 0
	capture := listNavigation(
		func() { backCalls++ },
		func() { escapeCalls++ },
	)

	if event := capture(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)); event.Key() != tcell.KeyEnter {
		t.Fatalf("right mapped to %v, want Enter", event.Key())
	}
	if event := capture(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)); event != nil || backCalls != 1 {
		t.Fatalf("left result = %v, back calls = %d", event, backCalls)
	}
	if event := capture(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)); event != nil || backCalls != 2 {
		t.Fatalf("backspace result = %v, back calls = %d", event, backCalls)
	}
	if event := capture(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)); event != nil || escapeCalls != 1 {
		t.Fatalf("escape result = %v, escape calls = %d", event, escapeCalls)
	}
	up := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	if event := capture(up); event != up {
		t.Fatalf("up event was replaced: %v", event)
	}
}

func TestFormNavigation(t *testing.T) {
	backCalls := 0
	escapeCalls := 0
	nextCalls := 0
	form := tview.NewForm().
		AddInputField("name", "source", 20, nil, nil).
		AddCheckbox("selected", false, nil).
		AddButton("next", nil)
	capture := formNavigation(
		form,
		func() { backCalls++ },
		func() { escapeCalls++ },
		func() { nextCalls++ },
	)
	var focus func(tview.Primitive)
	focus = func(primitive tview.Primitive) {
		primitive.Focus(focus)
	}
	form.Focus(focus)

	form.SetFocus(0)
	for _, key := range []tcell.Key{tcell.KeyLeft, tcell.KeyRight, tcell.KeyBackspace, tcell.KeyBackspace2} {
		input := tcell.NewEventKey(key, 0, tcell.ModNone)
		if event := capture(input); event != input {
			t.Fatalf("input key %v was replaced with %v", key, event)
		}
	}
	if event := capture(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)); event.Key() != tcell.KeyTab {
		t.Fatalf("input down mapped to %v, want Tab", event.Key())
	}
	if event := capture(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)); event.Key() != tcell.KeyBacktab {
		t.Fatalf("input up mapped to %v, want Backtab", event.Key())
	}
	if event := capture(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)); event != nil || escapeCalls != 1 {
		t.Fatalf("input escape result = %v, escape calls = %d", event, escapeCalls)
	}
	if event := capture(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)); event != nil || nextCalls != 1 {
		t.Fatalf("input enter result = %v, next calls = %d", event, nextCalls)
	}

	form.SetFocus(1)
	checkbox := form.GetFormItem(1).(*tview.Checkbox)
	for _, key := range []tcell.Key{tcell.KeyLeft, tcell.KeyRight} {
		event := capture(tcell.NewEventKey(key, 0, tcell.ModNone))
		if event.Key() != tcell.KeyRune || event.Rune() != ' ' {
			t.Fatalf("checkbox key %v mapped to key=%v rune=%q", key, event.Key(), event.Rune())
		}
	}
	if event := capture(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)); event != nil || nextCalls != 2 {
		t.Fatalf("checkbox enter result = %v, next calls = %d", event, nextCalls)
	}
	if checkbox.IsChecked() {
		t.Fatal("checkbox changed when Enter was pressed")
	}
	if event := capture(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)); event != nil || backCalls != 1 {
		t.Fatalf("checkbox backspace result = %v, back calls = %d", event, backCalls)
	}

	form.SetFocus(2)
	if event := capture(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)); event.Key() != tcell.KeyBacktab {
		t.Fatalf("button left mapped to %v, want Backtab", event.Key())
	}
	if event := capture(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)); event.Key() != tcell.KeyTab {
		t.Fatalf("button right mapped to %v, want Tab", event.Key())
	}
}

func TestEscapeReturnsActiveAddWorkflowToMain(t *testing.T) {
	store := &uiStore{q: queue.Empty()}
	service := &app.Service{
		QueueStore:      store,
		Scanner:         uiScanner{},
		Presets:         uiCatalog{},
		OutputDirectory: t.TempDir(),
	}
	ui, err := New(service, &runner.Runner{Store: store}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	ui.startAdd()
	assertFrontPage(t, ui, "files")
	addID := ui.addID
	if !ui.addActive(addID) {
		t.Fatal("add workflow is not active")
	}
	if event := ui.captureGlobalInput(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)); event != nil {
		t.Fatalf("escape result = %v", event)
	}
	assertFrontPage(t, ui, "main")
	if ui.addActive(addID) {
		t.Fatal("add workflow remains active after Escape")
	}

	ui.startAdd()
	if ui.addID == addID {
		t.Fatal("new add workflow reused stale ID")
	}
	if ui.addActive(addID) {
		t.Fatal("stale add workflow became active")
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
		ChapterInterval: media.DefaultEpisodeInterval,
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
		ChapterInterval:  media.DefaultEpisodeInterval,
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
	files.GetInputCapture()(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone))
	assertFrontPage(t, ui, "files")

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

	naming := frontPrimitive[*tview.Form](t, ui)
	naming.SetFocus(0)
	if event := naming.GetInputCapture()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)); event != nil {
		t.Fatalf("naming Enter event = %v, want nil", event)
	}
	assertFrontPage(t, ui, "chapters")
	chapters := frontPrimitive[*tview.Flex](t, ui)
	intervalForm := chapters.GetItem(0).(*tview.Form)
	interval := intervalForm.GetFormItem(0).(*tview.InputField)
	if interval.GetText() != "23:40" {
		t.Fatalf("initial interval = %q", interval.GetText())
	}
	interval.SetText("45:00")
	intervalForm.SetFocus(0)
	if event := intervalForm.GetInputCapture()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)); event != nil {
		t.Fatalf("chapter interval Enter event = %v, want nil", event)
	}
	assertFrontPage(t, ui, "audio")
	if ui.draft.ChapterInterval != 45*time.Minute {
		t.Fatalf("chapter interval = %s", ui.draft.ChapterInterval)
	}
	if !reflect.DeepEqual(ui.draft.SelectedChapters, []int{1}) {
		t.Fatalf("selected chapters = %v, want [1]", ui.draft.SelectedChapters)
	}
	ui.showChapters()
	chapters = frontPrimitive[*tview.Flex](t, ui)
	table := chapters.GetItem(3).(*tview.Table)
	if table.GetRowCount() != len(ui.draft.Media.Chapters)+1 {
		t.Fatalf("chapter rows = %d", table.GetRowCount())
	}
	wantHeaders := []string{"選択", "番号", "開始", "単体", "出力合計", "タイトル"}
	for column, want := range wantHeaders {
		if got := table.GetCell(0, column).Text; got != want {
			t.Fatalf("chapter header %d = %q, want %q", column, got, want)
		}
		if got := table.GetCell(1, column).Text; strings.TrimSpace(got) == "" {
			t.Fatalf("chapter row 1 column %d is empty", column)
		}
	}
	footer := chapters.GetItem(4).(*tview.Form)
	if label := footer.GetButton(0).GetLabel(); label != "入力した時間の近似値にチェック" {
		t.Fatalf("approximation button = %q", label)
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
	if len(ui.preview.Jobs) != 1 {
		t.Fatalf("preview jobs = %d", len(ui.preview.Jobs))
	}

	if err := service.AddPreview(ui.preview, true); err != nil {
		t.Fatal(err)
	}
	ui.showQueue()
	assertFrontPage(t, ui, "queue")
	queued := frontPrimitive[*tview.List](t, ui)
	if queued.GetItemCount() != 1 {
		t.Fatalf("queue items = %d", queued.GetItemCount())
	}
	_, queueDetail := queued.GetItemText(0)
	if !strings.Contains(queueDetail, "約1:00:00") {
		t.Fatalf("queue duration detail = %q", queueDetail)
	}
	ui.showQueueJob(store.q.Jobs[0])
	queueJob := frontPrimitive[*tview.Flex](t, ui)
	detailText := queueJob.GetItem(0).(*tview.TextView).GetText(false)
	if !strings.Contains(detailText, "動画時間: 約1:00:00") {
		t.Fatalf("queue job duration = %q", detailText)
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
		ChapterInterval:  media.DefaultEpisodeInterval,
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

func TestQueueDetailsDeleteButtonRemovesConfirmedWaitingJob(t *testing.T) {
	root := t.TempDir()
	first := queue.Job{
		ID:           "job-1",
		CreatedAt:    time.Now(),
		Input:        filepath.Join(root, "source.mkv"),
		Output:       filepath.Join(root, "first.mkv"),
		Preset:       "1080p MKV",
		Container:    queue.ContainerMKV,
		ChapterStart: 1,
		ChapterEnd:   1,
		AudioTracks:  []int{1},
		Subtitles:    []int{},
	}
	second := first
	second.ID = "job-2"
	second.Output = filepath.Join(root, "second.mkv")
	store := &uiStore{q: queue.Queue{Version: queue.Version, Jobs: []queue.Job{first, second}}}
	service := &app.Service{
		QueueStore:      store,
		Presets:         uiCatalog{},
		OutputDirectory: root,
	}
	ui, err := New(service, &runner.Runner{Store: store}, root)
	if err != nil {
		t.Fatal(err)
	}

	ui.showQueue()
	list := frontPrimitive[*tview.List](t, ui)
	list.SetCurrentItem(1)
	list.InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		func(tview.Primitive) {},
	)
	assertFrontPage(t, ui, "queue-detail")
	detail := frontPrimitive[*tview.Flex](t, ui)
	form := detail.GetItem(1).(*tview.Form)
	if label := form.GetButton(0).GetLabel(); label != "削除" {
		t.Fatalf("detail first button = %q, want 削除", label)
	}
	form.GetButton(0).InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		func(tview.Primitive) {},
	)
	assertFrontPage(t, ui, "queue-delete")
	modal := frontPrimitive[*tview.Modal](t, ui)
	handler := modal.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	assertFrontPage(t, ui, "queue")
	if len(store.q.Jobs) != 1 || store.q.Jobs[0].ID != first.ID {
		t.Fatalf("queue after delete = %#v", store.q)
	}
}

func TestQueueJKReordersSameJobRepeatedlyAndMovesSelectionWithIt(t *testing.T) {
	root := t.TempDir()
	jobs := make([]queue.Job, 3)
	for i := range jobs {
		jobs[i] = queue.Job{
			ID:           fmt.Sprintf("job-%d", i+1),
			CreatedAt:    time.Now(),
			Input:        filepath.Join(root, "source.mkv"),
			Output:       filepath.Join(root, fmt.Sprintf("output-%d.mkv", i+1)),
			Preset:       "1080p MKV",
			Container:    queue.ContainerMKV,
			ChapterStart: 1,
			ChapterEnd:   1,
			AudioTracks:  []int{1},
			Subtitles:    []int{},
		}
	}
	store := &uiStore{q: queue.Queue{Version: queue.Version, Jobs: jobs}}
	service := &app.Service{
		QueueStore:      store,
		Presets:         uiCatalog{},
		OutputDirectory: root,
	}
	ui, err := New(service, &runner.Runner{Store: store}, root)
	if err != nil {
		t.Fatal(err)
	}

	ui.showQueueAt(2)
	list := frontPrimitive[*tview.List](t, ui)
	if event := list.GetInputCapture()(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone)); event != nil {
		t.Fatalf("k event = %v", event)
	}
	if got := []string{store.q.Jobs[0].ID, store.q.Jobs[1].ID, store.q.Jobs[2].ID}; !reflect.DeepEqual(got, []string{"job-1", "job-3", "job-2"}) {
		t.Fatalf("queue after k = %v", got)
	}
	list = frontPrimitive[*tview.List](t, ui)
	if list.GetCurrentItem() != 1 {
		t.Fatalf("selection after k = %d, want 1", list.GetCurrentItem())
	}
	if event := list.GetInputCapture()(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone)); event != nil {
		t.Fatalf("second k event = %v", event)
	}
	if got := []string{store.q.Jobs[0].ID, store.q.Jobs[1].ID, store.q.Jobs[2].ID}; !reflect.DeepEqual(got, []string{"job-3", "job-1", "job-2"}) {
		t.Fatalf("queue after second k = %v", got)
	}
	list = frontPrimitive[*tview.List](t, ui)
	if list.GetCurrentItem() != 0 {
		t.Fatalf("selection after second k = %d, want 0", list.GetCurrentItem())
	}
}

func TestChaptersRejectInvalidChapterInterval(t *testing.T) {
	root := t.TempDir()
	service := &app.Service{
		QueueStore:      &uiStore{q: queue.Empty()},
		Presets:         uiCatalog{},
		OutputDirectory: root,
		ChapterInterval: media.DefaultEpisodeInterval,
	}
	ui, err := New(service, &runner.Runner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	ui.draft = app.Draft{
		Media: media.Info{
			Chapters: []media.Chapter{{Number: 1, Start: 0}},
		},
		Base:             "source",
		StartIndex:       1,
		ChapterInterval:  media.DefaultEpisodeInterval,
		SelectedChapters: []int{1},
	}
	ui.showChapters()
	chapters := frontPrimitive[*tview.Flex](t, ui)
	intervalForm := chapters.GetItem(0).(*tview.Form)
	intervalForm.GetFormItem(0).(*tview.InputField).SetText("23:60")
	intervalForm.SetFocus(0)
	if event := intervalForm.GetInputCapture()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)); event != nil {
		t.Fatalf("chapter interval Enter event = %v, want nil", event)
	}
	assertFrontPage(t, ui, "message")
	if ui.draft.ChapterInterval != media.DefaultEpisodeInterval {
		t.Fatalf("invalid input changed interval to %s", ui.draft.ChapterInterval)
	}
}

func TestShortFinalChapterCheckboxCanRestoreFinalChapter(t *testing.T) {
	root := t.TempDir()
	service := &app.Service{
		QueueStore:      &uiStore{q: queue.Empty()},
		Presets:         uiCatalog{},
		OutputDirectory: root,
		ChapterInterval: media.DefaultEpisodeInterval,
	}
	ui, err := New(service, &runner.Runner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	ui.draft = app.Draft{
		Media: media.Info{
			Duration: 21 * time.Second,
			Chapters: []media.Chapter{
				{Number: 1, Start: 0},
				{Number: 2, Start: 10 * time.Second},
				{Number: 3, Start: 20 * time.Second},
			},
		},
		Base:             "source",
		StartIndex:       1,
		ChapterInterval:  media.DefaultEpisodeInterval,
		SelectedChapters: []int{1, 2},
		ExcludeFinal:     true,
	}

	ui.showChapters()
	chapters := frontPrimitive[*tview.Flex](t, ui)
	exclude := chapters.GetItem(2).(*tview.Form).GetFormItem(0).(*tview.Checkbox)
	if !exclude.IsChecked() || !strings.Contains(exclude.GetLabel(), "chapter 003 / 0:01") {
		t.Fatalf("exclude checkbox = %t, %q", exclude.IsChecked(), exclude.GetLabel())
	}
	table := chapters.GetItem(3).(*tview.Table)
	table.Select(3, 0)
	table.GetInputCapture()(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	if ui.draft.ExcludeFinal || exclude.IsChecked() {
		t.Fatalf("final chapter check did not disable exclusion: draft=%t checkbox=%t",
			ui.draft.ExcludeFinal, exclude.IsChecked())
	}
	if !reflect.DeepEqual(ui.draft.SelectedChapters, []int{1, 2, 3}) {
		t.Fatalf("selected chapters = %v", ui.draft.SelectedChapters)
	}

	exclude.SetChecked(true)
	if !ui.draft.ExcludeFinal || table.GetCell(3, 0).Text != "[ ]" {
		t.Fatalf("exclusion did not clear final start: draft=%t cell=%q",
			ui.draft.ExcludeFinal, table.GetCell(3, 0).Text)
	}
	if !reflect.DeepEqual(ui.draft.SelectedChapters, []int{1, 2}) {
		t.Fatalf("selected chapters after exclusion = %v", ui.draft.SelectedChapters)
	}
}

func TestExitWhileRunningPausesAfterCurrentJob(t *testing.T) {
	root := t.TempDir()
	store := &uiStore{q: queue.Empty()}
	service := &app.Service{
		QueueStore:      store,
		Presets:         uiCatalog{},
		OutputDirectory: root,
	}
	ui, err := New(service, &runner.Runner{Store: store}, root)
	if err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	ui.running = true
	ui.cancel = cancel

	ui.requestExit()
	assertFrontPage(t, ui, "exit-confirm")
	modal := frontPrimitive[*tview.Modal](t, ui)
	modal.InputHandler()(
		tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone),
		func(tview.Primitive) {},
	)
	assertFrontPage(t, ui, "main")

	ui.requestExit()
	modal = frontPrimitive[*tview.Modal](t, ui)
	modal.InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		func(tview.Primitive) {},
	)
	if !ui.pauseAfterCurrent || !ui.exitAfterCurrent {
		t.Fatalf(
			"graceful exit flags = pause:%t exit:%t",
			ui.pauseAfterCurrent,
			ui.exitAfterCurrent,
		)
	}
	assertFrontPage(t, ui, "queue")
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

func TestRunningQueueUsesIntegratedQueueScreenAndImmediateAbort(t *testing.T) {
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
	ui.draft.Input = input
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(80, 24)
	ui.Application().SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- ui.Run() }()
	time.Sleep(20 * time.Millisecond)
	ui.Application().QueueUpdateDraw(func() {
		ui.startQueueAutomatically(1)
	})

	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		ui.Stop()
		t.Fatal("runner did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		page := runningFrontPage(ui)
		if page == "files" {
			break
		}
		if time.Now().After(deadline) {
			ui.Stop()
			t.Fatalf("automatic queue start did not return to file selection; page = %q", page)
		}
		time.Sleep(10 * time.Millisecond)
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone))
	deadline = time.Now().Add(2 * time.Second)
	for {
		page := runningFrontPage(ui)
		if page == "main" {
			break
		}
		if time.Now().After(deadline) {
			ui.Stop()
			t.Fatalf("Escape did not background execution; page = %q", page)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ui.Application().QueueUpdateDraw(ui.showQueue)
	deadline = time.Now().Add(2 * time.Second)
	for {
		page := runningFrontPage(ui)
		if page == "queue" {
			break
		}
		if time.Now().After(deadline) {
			ui.Stop()
			t.Fatalf("integrated queue screen was not restored; page = %q", page)
		}
		time.Sleep(10 * time.Millisecond)
	}
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone))
	deadline = time.Now().Add(2 * time.Second)
	for {
		page := runningFrontPage(ui)
		if page == "queue-detail" {
			break
		}
		if time.Now().After(deadline) {
			ui.Stop()
			t.Fatalf("Ctrl+C did not open running job details; page = %q", page)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ui.Application().QueueUpdateDraw(func() {
		ui.confirmAbortQueueJob(job)
	})
	screen.PostEventWait(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	deadline = time.Now().Add(2 * time.Second)
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

func runningFrontPage(ui *UI) string {
	result := make(chan string, 1)
	ui.Application().QueueUpdateDraw(func() {
		name, _ := ui.Pages().GetFrontPage()
		result <- name
	})
	return <-result
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
