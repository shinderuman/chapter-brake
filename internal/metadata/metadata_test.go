package metadata

import (
	"reflect"
	"testing"
	"time"

	"chapterbrake/internal/queue"
)

func metadataJob(container queue.Container) queue.Job {
	return queue.Job{
		ID:              "job-1",
		CreatedAt:       time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
		Input:           "/input/source.mkv",
		Output:          "/output/日本語 Title #01." + string(container),
		Preset:          "preset",
		Container:       container,
		ChapterStart:    1,
		ChapterEnd:      2,
		AudioSelections: []queue.AudioSelection{{Track: 1, Quality: queue.AudioHigh}},
		Subtitles:       []int{},
	}
}

func TestTitleFromOutput(t *testing.T) {
	got, err := TitleFromOutput("/output/日本語 Title #01.mkv")
	if err != nil || got != "日本語 Title #01" {
		t.Fatalf("TitleFromOutput() = %q, %v", got, err)
	}
	for _, path := range []string{"relative.mkv", "/output/no-extension"} {
		if _, err := TitleFromOutput(path); err == nil {
			t.Fatalf("TitleFromOutput(%q) error = nil", path)
		}
	}
}

func TestTemporaryPaths(t *testing.T) {
	tests := []struct {
		container queue.Container
		want      Paths
	}{
		{
			container: queue.ContainerMKV,
			want: Paths{
				Final:  "/output/日本語 Title #01.mkv",
				Encode: "/output/.chapterbrake-job-1-encode.mkv",
			},
		},
		{
			container: queue.ContainerMP4,
			want: Paths{
				Final:    "/output/日本語 Title #01.mp4",
				Encode:   "/output/.chapterbrake-job-1-encode.mp4",
				Metadata: "/output/.chapterbrake-job-1-metadata.mp4",
			},
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.container), func(t *testing.T) {
			got, err := TemporaryPaths(metadataJob(tt.container))
			if err != nil {
				t.Fatalf("TemporaryPaths() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("TemporaryPaths() = %#v, want %#v", got, tt.want)
			}
			titleTarget, err := got.TitleTarget(tt.container)
			if err != nil {
				t.Fatalf("TitleTarget() error = %v", err)
			}
			publishSource, err := got.PublishSource(tt.container)
			if err != nil {
				t.Fatalf("PublishSource() error = %v", err)
			}
			if titleTarget != publishSource {
				t.Fatalf("title target = %q, publish source = %q", titleTarget, publishSource)
			}
			if tt.container == queue.ContainerMKV && titleTarget != got.Encode {
				t.Fatalf("MKV title target = %q, want encode path", titleTarget)
			}
			if tt.container == queue.ContainerMP4 && titleTarget != got.Metadata {
				t.Fatalf("MP4 title target = %q, want metadata path", titleTarget)
			}
		})
	}
}

func TestPathTransitionValidation(t *testing.T) {
	tests := []struct {
		name      string
		paths     Paths
		container queue.Container
	}{
		{"missing MKV encode", Paths{}, queue.ContainerMKV},
		{"missing MP4 metadata", Paths{Encode: "/output/encode.mp4"}, queue.ContainerMP4},
		{"unsupported container", Paths{Encode: "/output/encode"}, "webm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.paths.TitleTarget(tt.container); err == nil {
				t.Fatal("TitleTarget() error = nil")
			}
		})
	}
}

func TestMKVPropEditArgs(t *testing.T) {
	got, err := MKVPropEditArgs("/output/temp.mkv", "日本語 Title #01")
	want := []string{"/output/temp.mkv", "--edit", "info", "--set", "title=日本語 Title #01"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("MKVPropEditArgs() = %q, %v", got, err)
	}
}

func TestMP4MetadataArgs(t *testing.T) {
	got, err := MP4MetadataArgs(
		"/output/encode.mp4",
		"/output/metadata.mp4",
		"日本語 Title #02",
		"mp42",
	)
	want := []string{
		"-i", "/output/encode.mp4",
		"-map", "0:v",
		"-map", "0:a?",
		"-map_metadata", "0",
		"-map_chapters", "0",
		"-c", "copy",
		"-metadata", "title=日本語 Title #02",
		"-brand", "mp42",
		"/output/metadata.mp4",
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("MP4MetadataArgs() = %q, %v", got, err)
	}
	for i := 0; i+1 < len(got); i++ {
		if got[i] == "-map" && got[i+1] == "0" {
			t.Fatal("MP4MetadataArgs() contains forbidden -map 0")
		}
	}
}

func TestMP4MetadataArgsValidation(t *testing.T) {
	tests := [][]string{
		{"relative.mp4", "/output/metadata.mp4", "title", "mp42"},
		{"/output/encode.mp4", "relative.mp4", "title", "mp42"},
		{"/output/same.mp4", "/output/same.mp4", "title", "mp42"},
		{"/output/encode.mp4", "/other/metadata.mp4", "title", "mp42"},
		{"/output/encode.mp4", "/output/metadata.mp4", "", "mp42"},
		{"/output/encode.mp4", "/output/metadata.mp4", "title", ""},
	}
	for _, args := range tests {
		if _, err := MP4MetadataArgs(args[0], args[1], args[2], args[3]); err == nil {
			t.Fatalf("MP4MetadataArgs(%q) error = nil", args)
		}
	}
}

func TestFFProbeArgs(t *testing.T) {
	got, err := FFProbeArgs("/output/file.mkv")
	want := []string{
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-show_chapters",
		"-of", "json",
		"/output/file.mkv",
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("FFProbeArgs() = %q, %v", got, err)
	}
	if _, err := FFProbeArgs("file.mkv"); err == nil {
		t.Fatal("FFProbeArgs(relative) error = nil")
	}
}
