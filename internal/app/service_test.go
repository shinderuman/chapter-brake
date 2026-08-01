package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/config"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
)

type serviceStore struct {
	q queue.Queue
}

func (s *serviceStore) Load() (queue.Queue, error) { return s.q, nil }
func (s *serviceStore) AppendJobs(jobs ...queue.Job) error {
	next, err := s.q.Append(jobs...)
	if err == nil {
		s.q = next
	}
	return err
}
func (s *serviceStore) DeleteJob(id string) error {
	next, err := s.q.RemoveJob(id)
	if err == nil {
		s.q = next
	}
	return err
}
func (s *serviceStore) MoveJob(id string, delta int) error {
	next, err := s.q.MoveJob(id, delta)
	if err == nil {
		s.q = next
	}
	return err
}
func (s *serviceStore) MoveJobTo(id string, destination int) error {
	next, err := s.q.MoveJobTo(id, destination)
	if err == nil {
		s.q = next
	}
	return err
}

type serviceScanner struct {
	info media.Info
}

func (s serviceScanner) Scan(context.Context, string, io.Writer, io.Writer) (media.Info, error) {
	return s.info, nil
}

type serviceCatalog struct{}

func (serviceCatalog) Curated() []handbrake.Preset { return handbrake.CuratedPresets() }
func (serviceCatalog) ListStandard(context.Context, io.Writer, io.Writer) ([]handbrake.StandardPreset, error) {
	return nil, nil
}
func (serviceCatalog) Resolve(_ context.Context, name string, _ io.Writer, _ io.Writer) (handbrake.Preset, error) {
	return handbrake.ResolveQueuedPreset(name, queue.ContainerMP4)
}

func testService(t *testing.T) (*Service, Draft, *serviceStore) {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "番組.mkv")
	if err := os.WriteFile(input, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := media.Info{
		Duration: 48 * time.Minute,
		Chapters: []media.Chapter{
			{Number: 1, Start: 0},
			{Number: 2, Start: 23*time.Minute + 39*time.Second},
			{Number: 3, Start: 47*time.Minute + 30*time.Second},
		},
		AudioTracks: []media.AudioTrack{
			{Number: 1, Codec: "ac3", Channels: 6},
			{Number: 2, Codec: "aac", Channels: 2},
			{Number: 3, Codec: "aac", Channels: 2},
		},
		SubtitleTracks: []media.SubtitleTrack{{Number: 1}, {Number: 2}},
	}
	store := &serviceStore{q: queue.Empty()}
	service := &Service{
		QueueStore:      store,
		Scanner:         serviceScanner{info: info},
		Presets:         serviceCatalog{},
		OutputDirectory: root,
		ChapterInterval: media.DefaultEpisodeInterval,
		InputDirectory:  root,
		SettingsStore:   config.Store{Path: filepath.Join(root, "settings.json")},
		Now: func() time.Time {
			return time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
		},
	}
	draft, err := service.Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return service, draft, store
}

func TestServiceSettings(t *testing.T) {
	service, _, _ := testService(t)
	output := filepath.Join(t.TempDir(), "not-created")
	settings := config.Settings{
		Version:         config.Version,
		InputDirectory:  service.InputDirectory,
		OutputDirectory: output,
		ChapterInterval: "45:00",
	}
	if err := service.UpdateSettings(settings); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if got := service.CurrentSettings(); !reflect.DeepEqual(got, settings) {
		t.Fatalf("CurrentSettings() = %#v, want %#v", got, settings)
	}
	stored, err := service.SettingsStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(stored, settings) {
		t.Fatalf("stored settings = %#v, want %#v", stored, settings)
	}
}

func TestAnalyzeInitialSelections(t *testing.T) {
	_, draft, _ := testService(t)
	if draft.Base != "番組" {
		t.Fatalf("base = %q", draft.Base)
	}
	if draft.ChapterInterval != media.DefaultEpisodeInterval {
		t.Fatalf("chapter interval = %s", draft.ChapterInterval)
	}
	if !reflect.DeepEqual(draft.SelectedChapters, []int{1, 2}) {
		t.Fatalf("selected chapters = %v", draft.SelectedChapters)
	}
	if !reflect.DeepEqual(draft.AudioTracks, []int{1, 2}) {
		t.Fatalf("audio tracks = %v", draft.AudioTracks)
	}
	if draft.Subtitles == nil || len(draft.Subtitles) != 0 {
		t.Fatalf("subtitles = %#v", draft.Subtitles)
	}
}

func TestAnalyzeWithProgressFallback(t *testing.T) {
	service, draft, _ := testService(t)
	var progress []float64
	if _, err := service.AnalyzeWithProgress(context.Background(), draft.Input, func(value float64) {
		progress = append(progress, value)
	}); err != nil {
		t.Fatalf("AnalyzeWithProgress() error = %v", err)
	}
	if !reflect.DeepEqual(progress, []float64{0, 1}) {
		t.Fatalf("progress = %v", progress)
	}
}

func TestAnalyzeUsesConfiguredChapterInterval(t *testing.T) {
	service, draft, _ := testService(t)
	service.ChapterInterval = 45 * time.Minute
	got, err := service.Analyze(context.Background(), draft.Input)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if got.ChapterInterval != 45*time.Minute {
		t.Fatalf("chapter interval = %s", got.ChapterInterval)
	}
	if !reflect.DeepEqual(got.SelectedChapters, []int{1}) {
		t.Fatalf("selected chapters = %v, want [1]", got.SelectedChapters)
	}

	service.ChapterInterval = 0
	if _, err := service.Analyze(context.Background(), draft.Input); err == nil ||
		!strings.Contains(err.Error(), "chapter interval") {
		t.Fatalf("Analyze() invalid interval error = %v", err)
	}
}

func TestAnalyzeAndPreviewExcludeShortFinalChapter(t *testing.T) {
	service, draft, _ := testService(t)
	info := draft.Media
	info.Duration = 47*time.Minute + 31*time.Second
	info.Chapters[2].Start = 47*time.Minute + 30*time.Second
	service.Scanner = serviceScanner{info: info}

	draft, err := service.Analyze(context.Background(), draft.Input)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if !draft.ExcludeFinal {
		t.Fatal("ExcludeFinal = false")
	}
	if !reflect.DeepEqual(draft.SelectedChapters, []int{1, 2}) {
		t.Fatalf("selected chapters = %v, want [1 2]", draft.SelectedChapters)
	}

	draft.Preset = handbrake.CuratedPresets()[1]
	draft.StartIndex = 1
	preview, err := service.BuildPreview(draft)
	if err != nil {
		t.Fatalf("BuildPreview() error = %v", err)
	}
	if len(preview.Jobs) != 2 || preview.Jobs[1].ChapterEnd != 2 {
		t.Fatalf("preview jobs = %#v", preview.Jobs)
	}
	if preview.ExcludedFinal == nil ||
		*preview.ExcludedFinal != (media.ChapterRange{Start: 3, End: 3}) {
		t.Fatalf("excluded final = %#v", preview.ExcludedFinal)
	}

	draft.ExcludeFinal = false
	preview, err = service.BuildPreview(draft)
	if err != nil {
		t.Fatalf("BuildPreview(include final) error = %v", err)
	}
	if preview.Jobs[len(preview.Jobs)-1].ChapterEnd != 3 || preview.ExcludedFinal != nil {
		t.Fatalf("included final preview = %#v", preview)
	}
}

func TestInitializeNamingUsesQueueAndFiles(t *testing.T) {
	service, draft, store := testService(t)
	draft.Preset = handbrake.CuratedPresets()[1]
	queued := queue.Job{
		ID:           "queued",
		CreatedAt:    time.Now(),
		Input:        draft.Input,
		Output:       filepath.Join(service.OutputDirectory, "番組", "番組_03.mkv"),
		Preset:       "1080p MKV",
		Container:    queue.ContainerMKV,
		ChapterStart: 1,
		ChapterEnd:   1,
		AudioTracks:  []int{1},
		Subtitles:    []int{},
	}
	store.q.Jobs = append(store.q.Jobs, queued)
	outputDirectory := filepath.Join(service.OutputDirectory, "番組")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDirectory, "番組_09.mkv"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.InitializeNaming(&draft); err != nil {
		t.Fatalf("InitializeNaming() error = %v", err)
	}
	if draft.StartIndex != 10 {
		t.Fatalf("start index = %d, want 10", draft.StartIndex)
	}
}

func TestBuildAndAddPreview(t *testing.T) {
	service, draft, store := testService(t)
	draft.Preset = handbrake.CuratedPresets()[1]
	draft.SelectedChapters = []int{2, 3}
	draft.StartIndex = 1
	draft.Subtitles = []int{1, 2}
	outputDirectory := filepath.Join(service.OutputDirectory, "番組")
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	collision := filepath.Join(outputDirectory, "番組_01.mkv")
	if err := os.WriteFile(collision, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	preview, err := service.BuildPreview(draft)
	if err != nil {
		t.Fatalf("BuildPreview() error = %v", err)
	}
	if len(preview.Jobs) != 2 || len(preview.Collisions) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	if got, want := filepath.Dir(preview.Jobs[0].Output), outputDirectory; got != want {
		t.Fatalf("output directory = %q, want %q", got, want)
	}
	if preview.Excluded == nil || *preview.Excluded != (media.ChapterRange{Start: 1, End: 1}) {
		t.Fatalf("excluded = %#v", preview.Excluded)
	}
	if preview.Jobs[0].ChapterStart != 2 || preview.Jobs[0].ChapterEnd != 2 {
		t.Fatalf("first job chapters = %d-%d", preview.Jobs[0].ChapterStart, preview.Jobs[0].ChapterEnd)
	}
	if preview.Jobs[0].DurationSeconds != 1431 || preview.Jobs[1].DurationSeconds != 30 {
		t.Fatalf("job durations = %d, %d", preview.Jobs[0].DurationSeconds, preview.Jobs[1].DurationSeconds)
	}
	if err := service.AddPreview(preview, false); err == nil {
		t.Fatal("AddPreview(without approval) error = nil")
	}
	if err := service.AddPreview(preview, true); err != nil {
		t.Fatalf("AddPreview() error = %v", err)
	}
	if _, err := os.Stat(outputDirectory); err != nil {
		t.Fatalf("existing output directory changed unexpectedly: %v", err)
	}
	if len(store.q.Jobs) != 2 {
		t.Fatalf("queue jobs = %d", len(store.q.Jobs))
	}
	if err := service.DeleteQueuedJob(store.q.Jobs[0].ID); err != nil {
		t.Fatalf("DeleteQueuedJob() error = %v", err)
	}
	if len(store.q.Jobs) != 1 {
		t.Fatalf("queue jobs after delete = %d", len(store.q.Jobs))
	}
}

func TestAddPreviewDoesNotCreateOutputDirectory(t *testing.T) {
	service, draft, store := testService(t)
	draft.Preset = handbrake.CuratedPresets()[1]
	draft.StartIndex = 1
	outputDirectory := filepath.Join(service.OutputDirectory, draft.Base)
	preview, err := service.BuildPreview(draft)
	if err != nil {
		t.Fatalf("BuildPreview() error = %v", err)
	}
	if err := service.AddPreview(preview, true); err != nil {
		t.Fatalf("AddPreview() error = %v", err)
	}
	if _, err := os.Stat(outputDirectory); !os.IsNotExist(err) {
		t.Fatalf("output directory exists before execution: %v", err)
	}
	if len(store.q.Jobs) == 0 {
		t.Fatal("queue was not updated")
	}
}

func TestBuildPreviewValidation(t *testing.T) {
	service, draft, _ := testService(t)
	draft.Preset = handbrake.CuratedPresets()[0]
	draft.StartIndex = 1
	draft.Subtitles = []int{1}
	if _, err := service.BuildPreview(draft); err == nil || !strings.Contains(err.Error(), "MP4") {
		t.Fatalf("BuildPreview(MP4 subtitles) error = %v", err)
	}
	draft.Subtitles = []int{}
	draft.AudioTracks = nil
	if _, err := service.BuildPreview(draft); err == nil {
		t.Fatal("BuildPreview(no audio) error = nil")
	}
}

func TestBuildPreviewPreservesEmptySubtitleArray(t *testing.T) {
	service, draft, _ := testService(t)
	draft.Preset = handbrake.CuratedPresets()[0]
	draft.StartIndex = 1
	draft.Subtitles = []int{}

	preview, err := service.BuildPreview(draft)
	if err != nil {
		t.Fatalf("BuildPreview() error = %v", err)
	}
	for i, job := range preview.Jobs {
		if job.Subtitles == nil || len(job.Subtitles) != 0 {
			t.Fatalf("job %d subtitles = %#v, want empty JSON array", i+1, job.Subtitles)
		}
	}
}
