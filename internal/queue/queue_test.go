package queue

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validJob() Job {
	return Job{
		ID:           "20260726T090000.000000000-0001",
		CreatedAt:    time.Date(2026, 7, 26, 9, 0, 0, 0, time.FixedZone("JST", 9*60*60)),
		Input:        "/Volumes/Video/source.mkv",
		Output:       "/Volumes/Output/source_01.mkv",
		Preset:       "1080p MKV",
		Container:    ContainerMKV,
		ChapterStart: 1,
		ChapterEnd:   4,
		AudioSelections: []AudioSelection{
			{Track: 1, Quality: AudioHigh},
			{Track: 2, Quality: AudioStandard},
		},
		Subtitles: []int{1},
	}
}

func TestJobValidate(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Job)
		errText string
	}{
		{"valid MKV", func(*Job) {}, ""},
		{"valid MP4", func(j *Job) {
			j.Container = ContainerMP4
			j.Output = "/Volumes/Output/source_01.mp4"
			j.Subtitles = []int{}
		}, ""},
		{"valid track three audio selection", func(j *Job) {
			j.AudioSelections = []AudioSelection{{Track: 1, Quality: AudioHigh}, {Track: 3, Quality: AudioStandard}}
		}, ""},
		{"valid audio-less job", func(j *Job) {
			j.AudioSelections = []AudioSelection{}
		}, ""},
		{"invalid id", func(j *Job) { j.ID = "../bad" }, "invalid id"},
		{"zero created", func(j *Job) { j.CreatedAt = time.Time{} }, "created_at"},
		{"relative input", func(j *Job) { j.Input = "source.mkv" }, "input must be absolute"},
		{"non-MKV input", func(j *Job) { j.Input = "/input/source.mp4" }, "must be an MKV"},
		{"relative output", func(j *Job) { j.Output = "source.mkv" }, "output must be absolute"},
		{"same paths", func(j *Job) { j.Output = j.Input }, "must differ"},
		{"empty preset", func(j *Job) { j.Preset = " " }, "preset"},
		{"relative preset file", func(j *Job) { j.PresetFile = "My Presets.json" }, "preset_file"},
		{"bad container", func(j *Job) { j.Container = "webm" }, "unsupported container"},
		{"extension mismatch", func(j *Job) { j.Output = "/Volumes/Output/source.mp4" }, "does not match"},
		{"chapter below one", func(j *Job) { j.ChapterStart = 0 }, "chapter range"},
		{"chapter reversed", func(j *Job) { j.ChapterStart, j.ChapterEnd = 4, 3 }, "chapter range"},
		{"negative duration", func(j *Job) { j.DurationSeconds = -1 }, "duration_seconds"},
		{"nil audio", func(j *Job) { j.AudioSelections = nil }, "JSON array"},
		{"invalid audio selection track", func(j *Job) {
			j.AudioSelections = []AudioSelection{{Track: 0, Quality: AudioHigh}}
		}, "invalid track"},
		{"invalid audio selection quality", func(j *Job) {
			j.AudioSelections = []AudioSelection{{Track: 1, Quality: "lossless"}}
		}, "invalid quality"},
		{"duplicate audio selection", func(j *Job) {
			j.AudioSelections = []AudioSelection{{Track: 3, Quality: AudioHigh}, {Track: 3, Quality: AudioStandard}}
		}, "duplicate"},
		{"nil subtitles", func(j *Job) { j.Subtitles = nil }, "JSON array"},
		{"bad subtitle", func(j *Job) { j.Subtitles = []int{0} }, "invalid"},
		{"duplicate subtitle", func(j *Job) { j.Subtitles = []int{1, 1} }, "duplicate"},
		{"MP4 subtitle", func(j *Job) {
			j.Container = ContainerMP4
			j.Output = "/Volumes/Output/source.mp4"
		}, "must not contain subtitles"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := validJob()
			tt.change(&job)
			err := job.Validate()
			if tt.errText == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.errText != "" && (err == nil || !strings.Contains(err.Error(), tt.errText)) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}

func TestQueueOperations(t *testing.T) {
	q := Empty()
	if _, ok := q.Peek(); ok {
		t.Fatal("empty Peek() ok = true")
	}
	if _, err := q.RemoveHead(); err == nil {
		t.Fatal("empty RemoveHead() error = nil")
	}

	first := validJob()
	second := validJob()
	second.ID = "20260726T090100.000000000-0002"
	second.Output = "/Volumes/Output/source_02.mkv"
	q, err := q.Append(first, second)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	got, ok := q.Peek()
	if !ok || got.ID != first.ID {
		t.Fatalf("Peek() = %#v, %v", got, ok)
	}

	q, err = q.RemoveHead()
	if err != nil {
		t.Fatalf("RemoveHead() error = %v", err)
	}
	got, ok = q.Peek()
	if !ok || got.ID != second.ID {
		t.Fatalf("Peek() after RemoveHead = %#v, %v", got, ok)
	}

	q, err = q.RemoveHead()
	if err != nil {
		t.Fatalf("RemoveHead() last job error = %v", err)
	}
	if q.Jobs == nil || len(q.Jobs) != 0 {
		t.Fatalf("RemoveHead() last job leaves %#v, want non-nil empty slice", q.Jobs)
	}
}

func TestQueueRemoveJob(t *testing.T) {
	first := validJob()
	second := validJob()
	second.ID = "second"
	second.Output = "/Volumes/Output/source_02.mkv"
	q, err := Empty().Append(first, second)
	if err != nil {
		t.Fatal(err)
	}

	q, err = q.RemoveJob(first.ID)
	if err != nil {
		t.Fatalf("RemoveJob() error = %v", err)
	}
	if len(q.Jobs) != 1 || q.Jobs[0].ID != second.ID {
		t.Fatalf("RemoveJob() = %#v", q)
	}
	if _, err := q.RemoveJob("missing"); err == nil {
		t.Fatal("RemoveJob(missing) error = nil")
	}
}

func TestQueueRejectsDuplicateIDs(t *testing.T) {
	job := validJob()
	q := Queue{Version: Version, Jobs: []Job{job, job}}
	if err := q.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestQueueMoveJob(t *testing.T) {
	first := validJob()
	second := validJob()
	second.ID = "second"
	second.Output = "/Volumes/Output/source_02.mkv"
	third := validJob()
	third.ID = "third"
	third.Output = "/Volumes/Output/source_03.mkv"
	q := Queue{Version: Version, Jobs: []Job{first, second, third}}

	moved, err := q.MoveJob(third.ID, -1)
	if err != nil {
		t.Fatalf("MoveJob() error = %v", err)
	}
	got := []string{moved.Jobs[0].ID, moved.Jobs[1].ID, moved.Jobs[2].ID}
	want := []string{first.ID, third.ID, second.ID}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MoveJob() order = %v, want %v", got, want)
	}
	if _, err := q.MoveJob(second.ID, 2); err == nil {
		t.Fatal("MoveJob(invalid delta) error = nil")
	}
}

func TestQueueMoveJobTo(t *testing.T) {
	first := validJob()
	second := validJob()
	second.ID = "second"
	second.Output = "/Volumes/Output/source_02.mkv"
	third := validJob()
	third.ID = "third"
	third.Output = "/Volumes/Output/source_03.mkv"
	q := Queue{Version: Version, Jobs: []Job{first, second, third}}

	moved, err := q.MoveJobTo(third.ID, 0)
	if err != nil {
		t.Fatalf("MoveJobTo() error = %v", err)
	}
	got := []string{moved.Jobs[0].ID, moved.Jobs[1].ID, moved.Jobs[2].ID}
	want := []string{third.ID, first.ID, second.ID}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MoveJobTo() order = %v, want %v", got, want)
	}
	if _, err := q.MoveJobTo(second.ID, 3); err == nil {
		t.Fatal("MoveJobTo(out of range) error = nil")
	}
}

func TestStoreLoadOrCreateAndInvalidFiles(t *testing.T) {
	t.Run("create and round trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "data", "queue.json")
		store := Store{Path: path}
		q, err := store.LoadOrCreate()
		if err != nil {
			t.Fatalf("LoadOrCreate() error = %v", err)
		}
		if q.Version != Version || q.Jobs == nil || len(q.Jobs) != 0 {
			t.Fatalf("LoadOrCreate() = %#v", q)
		}

		withJob, err := q.Append(validJob())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Save(withJob); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		got, err := store.Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(got.Jobs) != 1 || got.Jobs[0].ID != withJob.Jobs[0].ID {
			t.Fatalf("Load() = %#v", got)
		}

		second := validJob()
		second.ID = "second"
		second.Output = "/Volumes/Output/source_02.mkv"
		if err := store.AppendJobs(second); err != nil {
			t.Fatalf("AppendJobs() error = %v", err)
		}
		if err := store.DeleteJob(second.ID); err != nil {
			t.Fatalf("DeleteJob() error = %v", err)
		}
		head, ok, err := store.ClaimHead()
		if err != nil || !ok || head.ID != withJob.Jobs[0].ID {
			t.Fatalf("ClaimHead() = %#v, %t, %v", head, ok, err)
		}
		if err := store.DeleteJob(head.ID); err == nil {
			t.Fatal("DeleteJob(active) error = nil")
		}
		if err := store.CompleteHead(head.ID); err != nil {
			t.Fatalf("CompleteHead() error = %v", err)
		}
		got, err = store.Load()
		if err != nil || len(got.Jobs) != 0 {
			t.Fatalf("queue after mutations = %#v, %v", got, err)
		}
	})

	t.Run("release claimed head", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "queue.json")
		store := Store{Path: path}
		if err := store.Save(Queue{Version: Version, Jobs: []Job{validJob()}}); err != nil {
			t.Fatal(err)
		}
		head, ok, err := store.ClaimHead()
		if err != nil || !ok {
			t.Fatalf("ClaimHead() = %#v, %t, %v", head, ok, err)
		}
		if _, _, err := store.ClaimHead(); err == nil {
			t.Fatal("second ClaimHead() error = nil")
		}
		if err := store.ReleaseHead(head.ID); err != nil {
			t.Fatalf("ReleaseHead() error = %v", err)
		}
		if _, ok, err := store.ClaimHead(); err != nil || !ok {
			t.Fatalf("ClaimHead(after release) ok = %t, error = %v", ok, err)
		}
	})

	t.Run("append while head is active", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "queue.json")
		store := Store{Path: path}
		first := validJob()
		if err := store.Save(Queue{Version: Version, Jobs: []Job{first}}); err != nil {
			t.Fatal(err)
		}
		head, ok, err := store.ClaimHead()
		if err != nil || !ok {
			t.Fatalf("ClaimHead() = %#v, %t, %v", head, ok, err)
		}
		second := validJob()
		second.ID = "second"
		second.Output = "/Volumes/Output/source_02.mkv"
		if err := store.AppendJobs(second); err != nil {
			t.Fatalf("AppendJobs(active) error = %v", err)
		}
		if err := store.CompleteHead(first.ID); err != nil {
			t.Fatalf("CompleteHead() error = %v", err)
		}
		got, err := store.Load()
		if err != nil || len(got.Jobs) != 1 || got.Jobs[0].ID != second.ID {
			t.Fatalf("queue after append and complete = %#v, %v", got, err)
		}
	})

	t.Run("move waiting jobs while head is active", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "queue.json")
		store := Store{Path: path}
		first := validJob()
		second := validJob()
		second.ID = "second"
		second.Output = "/Volumes/Output/source_02.mkv"
		third := validJob()
		third.ID = "third"
		third.Output = "/Volumes/Output/source_03.mkv"
		if err := store.Save(Queue{Version: Version, Jobs: []Job{first, second, third}}); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := store.ClaimHead(); err != nil || !ok {
			t.Fatalf("ClaimHead() ok = %t, error = %v", ok, err)
		}
		if err := store.MoveJob(third.ID, -1); err != nil {
			t.Fatalf("MoveJob(waiting) error = %v", err)
		}
		if err := store.MoveJob(third.ID, -1); err == nil {
			t.Fatal("MoveJob(ahead of active) error = nil")
		}
		if err := store.MoveJob(first.ID, 1); err == nil {
			t.Fatal("MoveJob(active) error = nil")
		}
		if err := store.MoveJobTo(second.ID, 1); err != nil {
			t.Fatalf("MoveJobTo(waiting) error = %v", err)
		}
		if err := store.MoveJobTo(second.ID, 0); err == nil {
			t.Fatal("MoveJobTo(ahead of active) error = nil")
		}
		got, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if got.Jobs[0].ID != first.ID || got.Jobs[1].ID != second.ID || got.Jobs[2].ID != third.ID {
			t.Fatalf("queue order = %#v", got.Jobs)
		}
	})

	tests := []struct {
		name    string
		content string
		errText string
	}{
		{"malformed", `{`, "decode"},
		{"unknown version", `{"version":2,"jobs":[]}`, "unsupported queue version"},
		{"missing jobs", `{"version":1}`, "jobs must be a JSON array"},
		{"invalid job", `{"version":1,"jobs":[{}]}`, "job 1"},
		{"unknown field", `{"version":1,"jobs":[],"extra":true}`, "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.json")
			original := []byte(tt.content)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			store := Store{Path: path}
			_, err := store.LoadOrCreate()
			if err == nil || !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("LoadOrCreate() error = %v, want containing %q", err, tt.errText)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(original) {
				t.Fatalf("invalid canonical file changed: %q", got)
			}
		})
	}
}
