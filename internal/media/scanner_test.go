package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/process"
)

const validScan = `Version: {"VersionString":"1.11.2"}
JSON Title Set: {
  "TitleList": [{
    "Duration": {"Hours":0,"Minutes":0,"Seconds":6,"Ticks":540000},
    "ChapterList": [
      {"Name":"赤","Duration":{"Ticks":270000}},
      {"Name":"青","Duration":{"Ticks":270000}}
    ],
    "AudioList": [{
      "TrackNumber":1,"Language":"日本語","LanguageCode":"jpn","Name":"Main",
      "CodecName":"ac3","ChannelCount":6,"SampleRate":48000
    }],
    "SubtitleList": [{
      "TrackNumber":1,"Language":"日本語 (UTF-8)","LanguageCode":"jpn",
      "Name":"字幕","Format":"text","SourceName":"UTF-8"
    }]
  }]
}
trailing log text`

func TestParseScanOutput(t *testing.T) {
	got, err := ParseScanOutput([]byte(validScan))
	if err != nil {
		t.Fatalf("ParseScanOutput() error = %v", err)
	}
	want := Info{
		Duration: 6 * time.Second,
		Chapters: []Chapter{
			{Number: 1, Start: 0, Title: "赤"},
			{Number: 2, Start: 3 * time.Second, Title: "青"},
		},
		AudioTracks: []AudioTrack{
			{Number: 1, Language: "日本語", Name: "Main", Codec: "ac3", Channels: 6, SampleRate: 48000},
		},
		SubtitleTracks: []SubtitleTrack{
			{Number: 1, Language: "日本語 (UTF-8)", Name: "字幕", Format: "UTF-8"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseScanOutput() = %#v, want %#v", got, want)
	}
}

func TestParseScanOutputErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		errText string
	}{
		{"marker missing", `{}`, "marker not found"},
		{"invalid JSON", `JSON Title Set: {`, "decode"},
		{"no title", `JSON Title Set: {"TitleList":[]}`, "0 titles"},
		{"multiple titles", `JSON Title Set: {"TitleList":[{},{}]}`, "2 titles"},
		{"no chapters", `JSON Title Set: {"TitleList":[{"Duration":{"Ticks":1}}]}`, "no chapters"},
		{
			"zero duration",
			`JSON Title Set: {"TitleList":[{"Duration":{},"ChapterList":[{"Duration":{"Ticks":1}}]}]}`,
			"duration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseScanOutput([]byte(tt.input)); err == nil || !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("ParseScanOutput() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}

type scanExecutor struct {
	output []byte
	err    error
	got    process.Invocation
}

func (e *scanExecutor) Run(
	_ context.Context,
	invocation process.Invocation,
	stdout io.Writer,
	_ io.Writer,
) error {
	e.got = invocation
	_, _ = stdout.Write(e.output)
	return e.err
}

func TestScanner(t *testing.T) {
	executor := &scanExecutor{output: []byte(validScan)}
	var raw bytes.Buffer
	scanner := Scanner{Executor: executor, HandBrake: "/opt/homebrew/bin/HandBrakeCLI"}
	info, err := scanner.Scan(context.Background(), "/input/日本語 source.mkv", &raw, io.Discard)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if info.Duration != 6*time.Second {
		t.Fatalf("Scan() duration = %v", info.Duration)
	}
	if raw.String() != validScan {
		t.Fatal("Scan() did not preserve raw stdout")
	}
	if executor.got.Executable != "/opt/homebrew/bin/HandBrakeCLI" {
		t.Fatalf("executable = %q", executor.got.Executable)
	}
	if !reflect.DeepEqual(executor.got.Args, []string{"--json", "-i", "/input/日本語 source.mkv", "--scan"}) {
		t.Fatalf("args = %q", executor.got.Args)
	}
}

func TestScannerWithProgress(t *testing.T) {
	output := `Progress: {"Scanning":{"Progress":0.4},"State":"SCANNING"}\n` + validScan
	executor := &scanExecutor{output: []byte(output)}
	var progress []float64
	scanner := Scanner{Executor: executor}
	if _, err := scanner.ScanWithProgress(
		context.Background(),
		"/input/source.mkv",
		io.Discard,
		io.Discard,
		func(value float64) { progress = append(progress, value) },
	); err != nil {
		t.Fatalf("ScanWithProgress() error = %v", err)
	}
	if !reflect.DeepEqual(progress, []float64{0.4}) {
		t.Fatalf("progress = %v", progress)
	}
}

func TestScannerCommandError(t *testing.T) {
	commandErr := errors.New("scan failed")
	scanner := Scanner{Executor: &scanExecutor{err: commandErr}}
	_, err := scanner.Scan(context.Background(), "/input/source.mkv", nil, nil)
	if !errors.Is(err, commandErr) {
		t.Fatalf("Scan() error = %v, want wrapping command error", err)
	}
}

func TestScanArgs(t *testing.T) {
	got, err := ScanArgs("/input/file.mkv")
	want := []string{"--json", "-i", "/input/file.mkv", "--scan"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanArgs() = %q, %v", got, err)
	}
	if _, err := ScanArgs("file.mkv"); err == nil {
		t.Fatal("ScanArgs(relative) error = nil")
	}
}
