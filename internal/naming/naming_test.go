package naming

import (
	"reflect"
	"strings"
	"testing"

	"chapterbrake/internal/queue"
)

func TestInputBase(t *testing.T) {
	tests := []struct {
		path    string
		want    string
		errText string
	}{
		{"/Volumes/Video/番組.mkv", "番組", ""},
		{"/Volumes/Video/name.with.dots.MKV", "name.with.dots", ""},
		{"/Volumes/Video/no-extension", "", "no extension"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := InputBase(tt.path)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("InputBase() error = %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("InputBase() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestNextIndex(t *testing.T) {
	got, err := NextIndex(
		"番組",
		queue.ContainerMKV,
		[]string{"/output/番組_02.mkv", "/output/other_99.mkv"},
		[]string{"番組_09.MKV", "番組_abc.mkv", "番組_100.mp4", "番組2_30.mkv"},
	)
	if err != nil {
		t.Fatalf("NextIndex() error = %v", err)
	}
	if got != 10 {
		t.Fatalf("NextIndex() = %d, want 10", got)
	}

	got, err = NextIndex("番組", queue.ContainerMP4, nil, nil)
	if err != nil || got != 1 {
		t.Fatalf("NextIndex(empty) = %d, %v; want 1", got, err)
	}
}

func TestOutputPaths(t *testing.T) {
	tests := []struct {
		name      string
		start     int
		count     int
		container queue.Container
		want      []string
	}{
		{
			name:      "minimum two digits",
			start:     1,
			count:     3,
			container: queue.ContainerMKV,
			want: []string{
				"/output/番組_01.mkv",
				"/output/番組_02.mkv",
				"/output/番組_03.mkv",
			},
		},
		{
			name:      "expands for highest number",
			start:     99,
			count:     3,
			container: queue.ContainerMP4,
			want: []string{
				"/output/番組_099.mp4",
				"/output/番組_100.mp4",
				"/output/番組_101.mp4",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OutputPaths("/output", "番組", tt.start, tt.count, tt.container)
			if err != nil {
				t.Fatalf("OutputPaths() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("OutputPaths() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputPathsValidation(t *testing.T) {
	tests := []struct {
		name      string
		directory string
		base      string
		start     int
		count     int
		container queue.Container
	}{
		{"relative directory", "output", "name", 1, 1, queue.ContainerMKV},
		{"empty base", "/output", "", 1, 1, queue.ContainerMKV},
		{"base with slash", "/output", "a/b", 1, 1, queue.ContainerMKV},
		{"zero start", "/output", "name", 0, 1, queue.ContainerMKV},
		{"zero count", "/output", "name", 1, 0, queue.ContainerMKV},
		{"bad container", "/output", "name", 1, 1, "webm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := OutputPaths(tt.directory, tt.base, tt.start, tt.count, tt.container); err == nil {
				t.Fatal("OutputPaths() error = nil")
			}
		})
	}
}
