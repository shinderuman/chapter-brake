package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/naming"
	"chapterbrake/internal/queue"
)

type QueueStore interface {
	Load() (queue.Queue, error)
	AppendJobs(...queue.Job) error
	DeleteJob(string) error
	MoveJob(string, int) error
}

type Scanner interface {
	Scan(context.Context, string, io.Writer, io.Writer) (media.Info, error)
}

type PresetCatalog interface {
	Curated() []handbrake.Preset
	ListStandard(context.Context, io.Writer, io.Writer) ([]handbrake.StandardPreset, error)
	Resolve(context.Context, string, io.Writer, io.Writer) (handbrake.Preset, error)
}

type Service struct {
	QueueStore      QueueStore
	Scanner         Scanner
	Presets         PresetCatalog
	OutputDirectory string
	ChapterInterval time.Duration
	Now             func() time.Time

	sequence atomic.Uint64
}

type Draft struct {
	Input            string
	Media            media.Info
	Preset           handbrake.Preset
	Base             string
	StartIndex       int
	ChapterInterval  time.Duration
	SelectedChapters []int
	AudioTracks      []int
	Subtitles        []int
	AutoChapters     bool
	TailMerged       bool
	ExcludeFinal     bool
}

type Preview struct {
	Jobs          []queue.Job
	Collisions    []string
	Excluded      *media.ChapterRange
	ExcludedFinal *media.ChapterRange
}

func (s *Service) Analyze(ctx context.Context, input string) (Draft, error) {
	if s.Scanner == nil {
		return Draft{}, fmt.Errorf("media scanner is nil")
	}
	info, err := os.Stat(input)
	if err != nil {
		return Draft{}, fmt.Errorf("stat selected input: %w", err)
	}
	if !info.Mode().IsRegular() || !filepath.IsAbs(input) {
		return Draft{}, fmt.Errorf("selected input must be an absolute regular file")
	}
	if filepath.Ext(input) == "" {
		return Draft{}, fmt.Errorf("selected input has no extension")
	}
	if !strings.EqualFold(filepath.Ext(input), ".mkv") {
		return Draft{}, fmt.Errorf("selected input must be an MKV file")
	}
	mediaInfo, err := s.Scanner.Scan(ctx, input, io.Discard, io.Discard)
	if err != nil {
		return Draft{}, err
	}
	if s.ChapterInterval <= 0 {
		return Draft{}, fmt.Errorf("chapter interval must be positive")
	}
	approximation, err := media.ApproximateStarts(mediaInfo.Chapters, mediaInfo.Duration, s.ChapterInterval)
	if err != nil {
		return Draft{}, err
	}
	selected := approximation.Starts
	excludeFinal, err := media.HasShortFinalChapter(mediaInfo.Chapters, mediaInfo.Duration)
	if err != nil {
		return Draft{}, err
	}
	if excludeFinal {
		selected = withoutChapter(selected, len(mediaInfo.Chapters))
	}
	base, err := naming.InputBase(input)
	if err != nil {
		return Draft{}, err
	}
	audio := make([]int, 0, 2)
	for _, track := range mediaInfo.AudioTracks {
		if track.Number == 1 || track.Number == 2 {
			audio = append(audio, track.Number)
		}
	}
	sort.Ints(audio)
	if len(audio) == 0 {
		return Draft{}, fmt.Errorf("input has no supported audio tracks 1 or 2")
	}
	return Draft{
		Input:            input,
		Media:            mediaInfo,
		Base:             base,
		ChapterInterval:  s.ChapterInterval,
		SelectedChapters: selected,
		AudioTracks:      audio,
		Subtitles:        []int{},
		AutoChapters:     true,
		TailMerged:       approximation.TailMerged,
		ExcludeFinal:     excludeFinal,
	}, nil
}

func (s *Service) InitializeNaming(draft *Draft) error {
	if draft == nil {
		return fmt.Errorf("draft is nil")
	}
	if err := draft.Preset.Validate(); err != nil {
		return fmt.Errorf("select a valid preset: %w", err)
	}
	if s.QueueStore == nil {
		return fmt.Errorf("queue store is nil")
	}
	q, err := s.QueueStore.Load()
	if err != nil {
		return err
	}
	outputDirectory := filepath.Join(s.OutputDirectory, draft.Base)
	var entries []string
	directoryEntries, err := os.ReadDir(outputDirectory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read output directory: %w", err)
	}
	for _, entry := range directoryEntries {
		entries = append(entries, entry.Name())
	}
	queued := make([]string, len(q.Jobs))
	for i, job := range q.Jobs {
		queued[i] = job.Output
	}
	next, err := naming.NextIndex(draft.Base, draft.Preset.Container, queued, entries)
	if err != nil {
		return err
	}
	draft.StartIndex = next
	return nil
}

func (s *Service) BuildPreview(draft Draft) (Preview, error) {
	if err := draft.Preset.Validate(); err != nil {
		return Preview{}, err
	}
	finalChapter := len(draft.Media.Chapters)
	selectedChapters := append([]int(nil), draft.SelectedChapters...)
	if draft.ExcludeFinal {
		if finalChapter < 2 {
			return Preview{}, fmt.Errorf("the only chapter cannot be excluded")
		}
		selectedChapters = withoutChapter(selectedChapters, finalChapter)
		finalChapter--
	}
	ranges, err := media.RangesFromStarts(selectedChapters, finalChapter)
	if err != nil {
		return Preview{}, err
	}
	if _, err := handbrake.AudioPlan(draft.AudioTracks, draft.Media.AudioTracks, draft.Preset.Container); err != nil {
		return Preview{}, err
	}
	if draft.Preset.Container == queue.ContainerMP4 && len(draft.Subtitles) != 0 {
		return Preview{}, fmt.Errorf("MP4 preview must not include subtitles")
	}
	if err := validateSubtitles(draft.Subtitles, draft.Media.SubtitleTracks); err != nil {
		return Preview{}, err
	}
	outputs, err := naming.OutputPaths(
		filepath.Join(s.OutputDirectory, draft.Base),
		draft.Base,
		draft.StartIndex,
		len(ranges),
		draft.Preset.Container,
	)
	if err != nil {
		return Preview{}, err
	}

	now := s.now()
	jobs := make([]queue.Job, len(ranges))
	var collisions []string
	for i, chapterRange := range ranges {
		jobs[i] = queue.Job{
			ID:           s.nextID(now, i),
			CreatedAt:    now,
			Input:        draft.Input,
			Output:       outputs[i],
			Preset:       draft.Preset.DisplayName,
			Container:    draft.Preset.Container,
			ChapterStart: chapterRange.Start,
			ChapterEnd:   chapterRange.End,
			AudioTracks:  append([]int{}, draft.AudioTracks...),
			Subtitles:    append([]int{}, draft.Subtitles...),
		}
		if err := jobs[i].Validate(); err != nil {
			return Preview{}, fmt.Errorf("job %d: %w", i+1, err)
		}
		if _, err := os.Stat(outputs[i]); err == nil {
			collisions = append(collisions, outputs[i])
		} else if !os.IsNotExist(err) {
			return Preview{}, fmt.Errorf("stat planned output %s: %w", outputs[i], err)
		}
	}
	preview := Preview{Jobs: jobs, Collisions: collisions}
	if ranges[0].Start > 1 {
		preview.Excluded = &media.ChapterRange{Start: 1, End: ranges[0].Start - 1}
	}
	if draft.ExcludeFinal {
		preview.ExcludedFinal = &media.ChapterRange{
			Start: len(draft.Media.Chapters),
			End:   len(draft.Media.Chapters),
		}
	}
	return preview, nil
}

func withoutChapter(chapters []int, excluded int) []int {
	result := make([]int, 0, len(chapters))
	for _, chapter := range chapters {
		if chapter != excluded {
			result = append(result, chapter)
		}
	}
	return result
}

func (s *Service) AddPreview(preview Preview, overwriteApproved bool) error {
	if len(preview.Jobs) == 0 {
		return fmt.Errorf("preview contains no jobs")
	}
	if len(preview.Collisions) > 0 && !overwriteApproved {
		return fmt.Errorf("%d existing outputs require explicit overwrite approval", len(preview.Collisions))
	}
	for _, job := range preview.Jobs {
		if err := os.MkdirAll(filepath.Dir(job.Output), 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	return s.QueueStore.AppendJobs(preview.Jobs...)
}

func (s *Service) Queue() (queue.Queue, error) {
	if s.QueueStore == nil {
		return queue.Queue{}, fmt.Errorf("queue store is nil")
	}
	return s.QueueStore.Load()
}

func (s *Service) DeleteQueuedJob(id string) error {
	if s.QueueStore == nil {
		return fmt.Errorf("queue store is nil")
	}
	return s.QueueStore.DeleteJob(id)
}

func (s *Service) MoveQueuedJob(id string, delta int) error {
	if s.QueueStore == nil {
		return fmt.Errorf("queue store is nil")
	}
	return s.QueueStore.MoveJob(id, delta)
}

func validateSubtitles(selected []int, available []media.SubtitleTrack) error {
	known := make(map[int]struct{}, len(available))
	for _, track := range available {
		known[track.Number] = struct{}{}
	}
	seen := make(map[int]struct{}, len(selected))
	for _, number := range selected {
		if _, ok := known[number]; !ok {
			return fmt.Errorf("selected subtitle track %d does not exist", number)
		}
		if _, duplicate := seen[number]; duplicate {
			return fmt.Errorf("subtitle track %d is selected more than once", number)
		}
		seen[number] = struct{}{}
	}
	return nil
}

func (s *Service) nextID(now time.Time, index int) string {
	sequence := s.sequence.Add(1)
	return fmt.Sprintf("%s-%06d-%04d", now.Format("20060102T150405.000000000"), sequence, index+1)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
