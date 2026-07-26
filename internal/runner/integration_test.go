package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"chapterbrake/internal/media"
	"chapterbrake/internal/metadata"
	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
)

func TestRealToolchainIntegration(t *testing.T) {
	if os.Getenv("CHAPTERBRAKE_INTEGRATION") != "1" {
		t.Skip("set CHAPTERBRAKE_INTEGRATION=1 to run real media tools")
	}
	fixture := os.Getenv("CHAPTERBRAKE_FIXTURE")
	if fixture == "" {
		t.Fatal("CHAPTERBRAKE_FIXTURE must name the PoC MKV")
	}
	fixture, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"HandBrakeCLI", "ffmpeg", "ffprobe", "mkvpropedit"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required: %v", tool, err)
		}
	}

	toolExecutor := process.OSExecutor{InterruptGrace: 3 * time.Second}
	for _, container := range []queue.Container{queue.ContainerMKV, queue.ContainerMP4} {
		t.Run(string(container), func(t *testing.T) {
			root := os.Getenv("CHAPTERBRAKE_ACCEPTANCE_OUTPUT")
			if root == "" {
				root = t.TempDir()
			} else {
				root, err = filepath.Abs(root)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			output := filepath.Join(root, "統合 Title #01."+string(container))
			preset := "1080p MKV"
			subtitles := []int{1}
			if container == queue.ContainerMP4 {
				preset = "1080p MP4"
				subtitles = []int{}
			}
			job := queue.Job{
				ID:           "integration-" + string(container),
				CreatedAt:    time.Now(),
				Input:        fixture,
				Output:       output,
				Preset:       preset,
				Container:    container,
				ChapterStart: 1,
				ChapterEnd:   2,
				AudioTracks:  []int{1},
				Subtitles:    subtitles,
			}
			store := queue.Store{Path: filepath.Join(root, "queue.json")}
			if err := store.Save(queue.Queue{Version: queue.Version, Jobs: []queue.Job{job}}); err != nil {
				t.Fatal(err)
			}
			run := Runner{
				Store:        store,
				Executor:     toolExecutor,
				Scanner:      media.Scanner{Executor: toolExecutor},
				Prober:       metadata.Prober{Executor: toolExecutor},
				LogDirectory: filepath.Join(root, "logs"),
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			result, err := run.Run(ctx)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.Completed != 1 {
				t.Fatalf("Run() result = %#v", result)
			}
			probe, err := (metadata.Prober{Executor: toolExecutor}).Probe(
				context.Background(),
				output,
				nil,
				nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			if probe.Title() != "統合 Title #01" {
				t.Fatalf("final title = %q", probe.Title())
			}
			q, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(q.Jobs) != 0 {
				t.Fatalf("queue still has %d jobs", len(q.Jobs))
			}
		})
	}
}
