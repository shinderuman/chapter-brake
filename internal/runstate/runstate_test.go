package runstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/queue"
)

func stateJob() queue.Job {
	return queue.Job{
		ID:              "job-1",
		CreatedAt:       time.Now(),
		Input:           "/input/source.mkv",
		Output:          "/output/source_01.mkv",
		Preset:          "MKV Presets",
		Container:       queue.ContainerMKV,
		ChapterStart:    1,
		ChapterEnd:      2,
		AudioSelections: []queue.AudioSelection{{Track: 1, Quality: queue.AudioHigh}},
		Subtitles:       []int{},
	}
}

func TestStoreLifecycleAndInterruptedRecovery(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "state.json")}
	state, err := store.LoadOrCreate()
	if err != nil || state.Status != StatusIdle {
		t.Fatalf("LoadOrCreate() = %#v, %v", state, err)
	}
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	job := stateJob()
	if err := store.MarkRunning(job, "handbrake", now); err != nil {
		t.Fatalf("MarkRunning() error = %v", err)
	}
	recovered, err := store.RecoverInterrupted(now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecoverInterrupted() error = %v", err)
	}
	if recovered.Status != StatusFailed || !strings.Contains(recovered.Message, "実行中に終了") {
		t.Fatalf("recovered state = %#v", recovered)
	}
	logPath := filepath.Join(t.TempDir(), "job.log")
	if err := store.MarkFailed(job, "handbrake", "write failed", logPath, now); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	state, err = store.Load()
	if err != nil || state != (State{Version: Version, Status: StatusIdle}) {
		t.Fatalf("Load() after Clear = %#v, %v", state, err)
	}
}

func TestStoreDoesNotOverwriteInvalidState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"status":"running"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: path}
	if _, err := store.LoadOrCreate(); err == nil {
		t.Fatal("LoadOrCreate() error = nil")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"version":1,"status":"running"}` {
		t.Fatalf("invalid state was overwritten: %q", got)
	}
}

func TestStateValidation(t *testing.T) {
	tests := []State{
		{},
		{Version: Version, Status: "unknown"},
		{Version: Version, Status: StatusIdle, Message: "bad"},
		{Version: Version, Status: StatusRunning},
		{Version: Version, Status: StatusFailed, JobID: "job", Output: "/out", Stage: "stage", Message: "failed", LogPath: "relative", UpdatedAt: time.Now()},
	}
	for _, state := range tests {
		if err := state.Validate(); err == nil {
			t.Fatalf("Validate(%#v) error = nil", state)
		}
	}
}
