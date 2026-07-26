package handbrake

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
)

const presetListText = `[09:00:00] HandBrake startup
General/
    Fast 1080p30
        H.264 video and AAC audio.
    Fast 480p30
        H.264 video.
Matroska/
    H.264 MKV 1080p30
        H.264 video in an MKV.
`

func TestParsePresetList(t *testing.T) {
	got, err := ParsePresetList([]byte(presetListText))
	if err != nil {
		t.Fatalf("ParsePresetList() error = %v", err)
	}
	want := []StandardPreset{
		{Category: "General", Name: "Fast 1080p30"},
		{Category: "General", Name: "Fast 480p30"},
		{Category: "Matroska", Name: "H.264 MKV 1080p30"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParsePresetList() = %#v, want %#v", got, want)
	}
	if _, err := ParsePresetList([]byte("no presets")); err == nil {
		t.Fatal("ParsePresetList(no presets) error = nil")
	}
}

type catalogExecutor struct {
	listOutput []byte
	exportJSON string
	err        error
	calls      []process.Invocation
}

func (e *catalogExecutor) Run(
	_ context.Context,
	invocation process.Invocation,
	stdout io.Writer,
	_ io.Writer,
) error {
	e.calls = append(e.calls, invocation)
	if e.err != nil {
		return e.err
	}
	if reflect.DeepEqual(invocation.Args, PresetListArgs()) {
		_, _ = stdout.Write(e.listOutput)
		return nil
	}
	for i, arg := range invocation.Args {
		if arg == "--preset-export-file" && i+1 < len(invocation.Args) {
			return os.WriteFile(invocation.Args[i+1], []byte(e.exportJSON), 0o600)
		}
	}
	return fmt.Errorf("unexpected invocation: %q", invocation.Args)
}

func TestCatalogListStandard(t *testing.T) {
	executor := &catalogExecutor{listOutput: []byte(presetListText)}
	var raw bytes.Buffer
	catalog := Catalog{Executor: executor, HandBrake: "/opt/homebrew/bin/HandBrakeCLI"}
	got, err := catalog.ListStandard(context.Background(), &raw, io.Discard)
	if err != nil {
		t.Fatalf("ListStandard() error = %v", err)
	}
	if len(got) != 3 || raw.String() != presetListText {
		t.Fatalf("ListStandard() = %#v, raw %q", got, raw.String())
	}
	if executor.calls[0].Executable != "/opt/homebrew/bin/HandBrakeCLI" {
		t.Fatalf("executable = %q", executor.calls[0].Executable)
	}
}

func TestCatalogResolve(t *testing.T) {
	t.Run("curated does not execute HandBrake", func(t *testing.T) {
		executor := &catalogExecutor{}
		catalog := Catalog{Executor: executor}
		got, err := catalog.Resolve(context.Background(), "1080p MKV", nil, nil)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if got.Container != queue.ContainerMKV || len(executor.calls) != 0 {
			t.Fatalf("Resolve() = %#v, calls = %d", got, len(executor.calls))
		}
	})

	t.Run("standard exports without GUI import", func(t *testing.T) {
		temp := t.TempDir()
		executor := &catalogExecutor{
			exportJSON: `{"PresetList":[{"PresetName":"chapterbrake-probe","FileFormat":"av_mp4"}]}`,
		}
		catalog := Catalog{Executor: executor, TempDirectory: temp}
		got, err := catalog.Resolve(context.Background(), "Fast 1080p30", io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		want := Preset{
			DisplayName:   "Fast 1080p30",
			HandBrakeName: "Fast 1080p30",
			Container:     queue.ContainerMP4,
		}
		if got != want {
			t.Fatalf("Resolve() = %#v, want %#v", got, want)
		}
		if len(executor.calls) != 1 {
			t.Fatalf("calls = %d", len(executor.calls))
		}
		for _, arg := range executor.calls[0].Args {
			if arg == "--preset-import-gui" {
				t.Fatal("Resolve() depends on GUI preset import")
			}
		}
		matches, err := filepath.Glob(filepath.Join(temp, "chapterbrake-preset-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Fatalf("temporary exports remain: %v", matches)
		}
	})
}

func TestCatalogErrors(t *testing.T) {
	commandErr := fmt.Errorf("command failed")
	catalog := Catalog{Executor: &catalogExecutor{err: commandErr}, TempDirectory: t.TempDir()}
	if _, err := catalog.ListStandard(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("ListStandard() error = %v", err)
	}
	if _, err := catalog.Resolve(context.Background(), "Other", nil, nil); err == nil || !strings.Contains(err.Error(), "command failed") {
		t.Fatalf("Resolve() error = %v", err)
	}
}
