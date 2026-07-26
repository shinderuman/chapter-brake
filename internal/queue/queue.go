package queue

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const Version = 1

type Container string

const (
	ContainerMKV Container = "mkv"
	ContainerMP4 Container = "mp4"
)

var jobIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Job struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	Input        string    `json:"input"`
	Output       string    `json:"output"`
	Preset       string    `json:"preset"`
	Container    Container `json:"container"`
	ChapterStart int       `json:"chapter_start"`
	ChapterEnd   int       `json:"chapter_end"`
	AudioTracks  []int     `json:"audio_tracks"`
	Subtitles    []int     `json:"subtitles"`
}

type Queue struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

func Empty() Queue {
	return Queue{Version: Version, Jobs: []Job{}}
}

func (q Queue) Validate() error {
	if q.Version != Version {
		return fmt.Errorf("unsupported queue version %d", q.Version)
	}
	if q.Jobs == nil {
		return fmt.Errorf("jobs must be a JSON array")
	}

	ids := make(map[string]struct{}, len(q.Jobs))
	for i, job := range q.Jobs {
		if err := job.Validate(); err != nil {
			return fmt.Errorf("job %d: %w", i+1, err)
		}
		if _, exists := ids[job.ID]; exists {
			return fmt.Errorf("job %d: duplicate id %q", i+1, job.ID)
		}
		ids[job.ID] = struct{}{}
	}
	return nil
}

func (j Job) Validate() error {
	if !jobIDPattern.MatchString(j.ID) {
		return fmt.Errorf("invalid id %q", j.ID)
	}
	if j.CreatedAt.IsZero() {
		return fmt.Errorf("created_at must not be zero")
	}
	if !filepath.IsAbs(j.Input) {
		return fmt.Errorf("input must be absolute: %q", j.Input)
	}
	if !strings.EqualFold(filepath.Ext(j.Input), ".mkv") {
		return fmt.Errorf("input must be an MKV file: %q", j.Input)
	}
	if !filepath.IsAbs(j.Output) {
		return fmt.Errorf("output must be absolute: %q", j.Output)
	}
	if filepath.Clean(j.Input) == filepath.Clean(j.Output) {
		return fmt.Errorf("input and output must differ")
	}
	if strings.TrimSpace(j.Preset) == "" {
		return fmt.Errorf("preset must not be empty")
	}
	if j.Container != ContainerMKV && j.Container != ContainerMP4 {
		return fmt.Errorf("unsupported container %q", j.Container)
	}
	if !strings.EqualFold(filepath.Ext(j.Output), "."+string(j.Container)) {
		return fmt.Errorf("output extension %q does not match container %q", filepath.Ext(j.Output), j.Container)
	}
	if j.ChapterStart < 1 || j.ChapterEnd < j.ChapterStart {
		return fmt.Errorf("invalid chapter range %d-%d", j.ChapterStart, j.ChapterEnd)
	}
	if err := validateTrackNumbers("audio_tracks", j.AudioTracks, true, 2); err != nil {
		return err
	}
	if err := validateTrackNumbers("subtitles", j.Subtitles, false, 0); err != nil {
		return err
	}
	if j.Container == ContainerMP4 && len(j.Subtitles) != 0 {
		return fmt.Errorf("MP4 jobs must not contain subtitles")
	}
	return nil
}

func validateTrackNumbers(name string, tracks []int, required bool, max int) error {
	if tracks == nil {
		return fmt.Errorf("%s must be a JSON array", name)
	}
	if required && len(tracks) == 0 {
		return fmt.Errorf("%s must contain at least one track", name)
	}

	seen := make(map[int]struct{}, len(tracks))
	for _, track := range tracks {
		if track < 1 {
			return fmt.Errorf("%s contains invalid track %d", name, track)
		}
		if max > 0 && track > max {
			return fmt.Errorf("%s contains unsupported track %d", name, track)
		}
		if _, exists := seen[track]; exists {
			return fmt.Errorf("%s contains duplicate track %d", name, track)
		}
		seen[track] = struct{}{}
	}
	return nil
}

func (q Queue) Append(jobs ...Job) (Queue, error) {
	combined := make([]Job, len(q.Jobs)+len(jobs))
	copy(combined, q.Jobs)
	copy(combined[len(q.Jobs):], jobs)
	next := Queue{
		Version: q.Version,
		Jobs:    combined,
	}
	if err := next.Validate(); err != nil {
		return Queue{}, err
	}
	return next, nil
}

func (q Queue) Peek() (Job, bool) {
	if len(q.Jobs) == 0 {
		return Job{}, false
	}
	return q.Jobs[0], true
}

func (q Queue) RemoveHead() (Queue, error) {
	if len(q.Jobs) == 0 {
		return Queue{}, fmt.Errorf("queue is empty")
	}
	remaining := make([]Job, len(q.Jobs)-1)
	copy(remaining, q.Jobs[1:])
	next := Queue{
		Version: q.Version,
		Jobs:    remaining,
	}
	if err := next.Validate(); err != nil {
		return Queue{}, err
	}
	return next, nil
}
