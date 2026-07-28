package handbrake

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"

	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
)

func TestRealCatalog(t *testing.T) {
	if os.Getenv("CHAPTERBRAKE_INTEGRATION") != "1" {
		t.Skip("set CHAPTERBRAKE_INTEGRATION=1 to use the installed HandBrakeCLI")
	}
	path, err := exec.LookPath("HandBrakeCLI")
	if err != nil {
		t.Fatal(err)
	}
	catalog := Catalog{
		Executor:  &process.OSExecutor{},
		HandBrake: path,
	}
	presets, err := catalog.ListStandard(context.Background(), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("ListStandard() error = %v", err)
	}
	found := false
	for _, preset := range presets {
		if preset.Name == "Fast 1080p30" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Fast 1080p30 is missing from the real standard preset list")
	}
	preset, err := catalog.Resolve(context.Background(), "Fast 1080p30", io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if preset.Container != queue.ContainerMP4 || preset.HandBrakeName != "Fast 1080p30" {
		t.Fatalf("Resolve() = %#v", preset)
	}
}
