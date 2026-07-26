package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
)

func logJob() queue.Job {
	return queue.Job{
		ID:           "job-1",
		CreatedAt:    time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
		Input:        "/input/source.mkv",
		Output:       "/output/日本語 Title #01.mkv",
		Preset:       "1080p MKV",
		Container:    queue.ContainerMKV,
		ChapterStart: 1,
		ChapterEnd:   2,
		AudioTracks:  []int{1},
		Subtitles:    []int{},
	}
}

func TestOpenApp(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "logs")
	logger, closer, path, err := OpenApp(directory, time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("OpenApp() error = %v", err)
	}
	logger.Info("起動", "jobs", 2)
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "app-2026-07-26.log" {
		t.Fatalf("path = %q", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "起動") || !strings.Contains(string(content), "jobs=2") {
		t.Fatalf("app log = %q", content)
	}
}

func TestJobAndCommandLogs(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 7, 26, 9, 0, 0, 123, time.UTC)
	jobLog, err := OpenJob(directory, logJob(), now)
	if err != nil {
		t.Fatalf("OpenJob() error = %v", err)
	}
	commandLog, err := jobLog.OpenCommand("handbrake", process.Invocation{
		Executable: "HandBrakeCLI",
		Args:       []string{"-i", "/input/source.mkv"},
	})
	if err != nil {
		t.Fatalf("OpenCommand() error = %v", err)
	}
	_, _ = commandLog.Stdout.Write([]byte("raw stdout\n"))
	_, _ = commandLog.Stderr.Write([]byte("raw stderr\n"))
	if err := commandLog.Close(nil); err != nil {
		t.Fatalf("CommandLog.Close() error = %v", err)
	}
	jobLog.Event("job-success", "title", "日本語 Title #01")
	path := jobLog.Path()
	if err := jobLog.Close(); err != nil {
		t.Fatalf("JobLog.Close() error = %v", err)
	}

	summary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"job-start", "command-start", "command-finish", "job-success"} {
		if !strings.Contains(string(summary), expected) {
			t.Fatalf("summary lacks %q: %s", expected, summary)
		}
	}
	matches, err := filepath.Glob(filepath.Join(directory, "*-handbrake.*.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("command logs = %v", matches)
	}
	for _, match := range matches {
		content, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		if len(content) == 0 {
			t.Fatalf("command log is empty: %s", match)
		}
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("日本語 Title #01"); got != "日本語_Title__01" {
		t.Fatalf("sanitize() = %q", got)
	}
	if _, err := OpenJob(t.TempDir(), queue.Job{}, time.Now()); err == nil {
		t.Fatal("OpenJob(invalid) error = nil")
	}
}
