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

	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
)

type serviceStore struct {
	q queue.Queue
}

func (s *serviceStore) Load() (queue.Queue, error) { return s.q, nil }
func (s *serviceStore) Save(q queue.Queue) error {
	s.q = q
	return nil
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

func TestAnalyzeInitialSelections(t *testing.T) {
	_, draft, _ := testService(t)
	if draft.Base != "番組" {
		t.Fatalf("base = %q", draft.Base)
	}
	if draft.ChapterInterval != media.DefaultEpisodeInterval {
		t.Fatalf("chapter interval = %s", draft.ChapterInterval)
	}
	if !reflect.DeepEqual(draft.SelectedChapters, []int{1, 2, 3}) {
		t.Fatalf("selected chapters = %v", draft.SelectedChapters)
	}
	if !reflect.DeepEqual(draft.AudioTracks, []int{1, 2}) {
		t.Fatalf("audio tracks = %v", draft.AudioTracks)
	}
	if draft.Subtitles == nil || len(draft.Subtitles) != 0 {
		t.Fatalf("subtitles = %#v", draft.Subtitles)
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
	if !reflect.DeepEqual(got.SelectedChapters, []int{1, 3}) {
		t.Fatalf("selected chapters = %v, want [1 3]", got.SelectedChapters)
	}

	service.ChapterInterval = 0
	if _, err := service.Analyze(context.Background(), draft.Input); err == nil ||
		!strings.Contains(err.Error(), "chapter interval") {
		t.Fatalf("Analyze() invalid interval error = %v", err)
	}
}

func TestInitializeNamingUsesQueueAndFiles(t *testing.T) {
	service, draft, store := testService(t)
	draft.Preset = handbrake.CuratedPresets()[1]
	queued := queue.Job{
		ID:           "queued",
		CreatedAt:    time.Now(),
		Input:        draft.Input,
		Output:       filepath.Join(service.OutputDirectory, "番組_03.mkv"),
		Preset:       "1080p MKV",
		Container:    queue.ContainerMKV,
		ChapterStart: 1,
		ChapterEnd:   1,
		AudioTracks:  []int{1},
		Subtitles:    []int{},
	}
	store.q.Jobs = append(store.q.Jobs, queued)
	if err := os.WriteFile(filepath.Join(service.OutputDirectory, "番組_09.mkv"), nil, 0o600); err != nil {
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
	collision := filepath.Join(service.OutputDirectory, "番組_01.mkv")
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
	if preview.Excluded == nil || *preview.Excluded != (media.ChapterRange{Start: 1, End: 1}) {
		t.Fatalf("excluded = %#v", preview.Excluded)
	}
	if preview.Jobs[0].ChapterStart != 2 || preview.Jobs[0].ChapterEnd != 2 {
		t.Fatalf("first job chapters = %d-%d", preview.Jobs[0].ChapterStart, preview.Jobs[0].ChapterEnd)
	}
	if err := service.AddPreview(preview, false); err == nil {
		t.Fatal("AddPreview(without approval) error = nil")
	}
	if err := service.AddPreview(preview, true); err != nil {
		t.Fatalf("AddPreview() error = %v", err)
	}
	if len(store.q.Jobs) != 2 {
		t.Fatalf("queue jobs = %d", len(store.q.Jobs))
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
