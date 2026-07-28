package handbrake

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
)

func TestCuratedPresets(t *testing.T) {
	got := CuratedPresets()
	if len(got) != 4 {
		t.Fatalf("len(CuratedPresets()) = %d, want 4", len(got))
	}
	wantNames := []string{"MP4 Presets", "MKV Presets", "My Old Presets", "GCCX"}
	for i, preset := range got {
		if err := preset.Validate(); err != nil {
			t.Fatalf("preset %d invalid: %v", i, err)
		}
		if preset.DisplayName != wantNames[i] {
			t.Fatalf("preset %d name = %q, want %q", i, preset.DisplayName, wantNames[i])
		}
		if !preset.ChapterBrakeOwned {
			t.Fatalf("preset %d is not ChapterBrake-owned", i)
		}
	}
	if got[1].HandBrakeName != "H.264 MKV 1080p30" {
		t.Fatalf("1080p MKV HandBrake base = %q", got[1].HandBrakeName)
	}
	if got[2].CropMode != "auto" || got[3].CropMode != "none" {
		t.Fatalf("480p crop modes = %q, %q", got[2].CropMode, got[3].CropMode)
	}
}

func TestResolveQueuedPreset(t *testing.T) {
	curated, err := ResolveQueuedPreset("1080p MKV", queue.ContainerMKV)
	if err != nil || !curated.ChapterBrakeOwned || curated.HandBrakeName != "H.264 MKV 1080p30" {
		t.Fatalf("ResolveQueuedPreset(curated) = %#v, %v", curated, err)
	}
	standard, err := ResolveQueuedPreset("Fast 720p30", queue.ContainerMP4)
	if err != nil || standard.ChapterBrakeOwned || standard.HandBrakeName != "Fast 720p30" {
		t.Fatalf("ResolveQueuedPreset(standard) = %#v, %v", standard, err)
	}
	if _, err := ResolveQueuedPreset("1080p MKV", queue.ContainerMP4); err == nil {
		t.Fatal("ResolveQueuedPreset(mismatched curated container) error = nil")
	}
	current, err := ResolveQueuedPreset("GCCX", queue.ContainerMP4)
	if err != nil || current.CropMode != "none" {
		t.Fatalf("ResolveQueuedPreset(GCCX) = %#v, %v", current, err)
	}
}

func TestParseExportedPreset(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		display string
		want    Preset
		errText string
	}{
		{
			name:    "MP4 leaf with unrelated HandBrake fields",
			json:    `{"VersionMajor":72,"PresetList":[{"PresetName":"Fast 1080p30","FileFormat":"av_mp4","Folder":false,"VideoEncoder":"x264"}]}`,
			display: "",
			want: Preset{
				DisplayName:   "Fast 1080p30",
				HandBrakeName: "Fast 1080p30",
				Container:     queue.ContainerMP4,
			},
		},
		{
			name: "nested MKV",
			json: `{"PresetList":[{
				"PresetName":"Matroska","Folder":true,
				"ChildrenArray":[{"PresetName":"H.264 MKV 1080p30","FileFormat":"av_mkv","Folder":false}]
			}]}`,
			display: "Other MKV",
			want: Preset{
				DisplayName:   "Other MKV",
				HandBrakeName: "H.264 MKV 1080p30",
				Container:     queue.ContainerMKV,
			},
		},
		{"unsupported format", `{"PresetList":[{"PresetName":"MOV","FileFormat":"av_mov"}]}`, "", Preset{}, "unsupported"},
		{"multiple leaves", `{"PresetList":[{"PresetName":"A","FileFormat":"av_mp4"},{"PresetName":"B","FileFormat":"av_mkv"}]}`, "", Preset{}, "2 leaf"},
		{"invalid JSON", `{`, "", Preset{}, "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExportedPreset([]byte(tt.json), tt.display)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("ParseExportedPreset() error = %v, want containing %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExportedPreset() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseExportedPreset() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestLoadPresetFileReadsGUIFolderExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "My Presets.json")
	data := []byte(`{"PresetList":[{
		"PresetName":"My Presets","Folder":true,"ChildrenArray":[
			{"PresetName":"MP4 Presets","FileFormat":"av_mp4","Folder":false,"PictureWidth":1920,"PictureHeight":1080,"PictureCropMode":0},
			{"PresetName":"MKV Presets","FileFormat":"av_mkv","Folder":false,"PictureWidth":720,"PictureHeight":480,"PictureCropMode":2}
		]
	}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPresetFile(path)
	if err != nil {
		t.Fatalf("LoadPresetFile() error = %v", err)
	}
	if len(got) != 2 || got[0].DisplayName != "MP4 Presets" || got[1].Container != queue.ContainerMKV {
		t.Fatalf("LoadPresetFile() = %#v", got)
	}
	if got[0].Summary != "1920x1080・MP4・自動クロップ" ||
		got[1].Summary != "720x480・MKV・クロップなし" {
		t.Fatalf("imported summaries = %q, %q", got[0].Summary, got[1].Summary)
	}
	for _, preset := range got {
		if preset.ImportFile != path || !preset.ChapterBrakeOwned {
			t.Fatalf("imported preset = %#v", preset)
		}
	}
	resolved, err := ResolveQueuedPreset("MKV Presets", queue.ContainerMKV, path)
	if err != nil || resolved.ImportFile != path {
		t.Fatalf("ResolveQueuedPreset(imported) = %#v, %v", resolved, err)
	}
	if _, err := ResolveQueuedPreset("missing", queue.ContainerMKV, path); err == nil {
		t.Fatal("ResolveQueuedPreset(missing imported preset) error = nil")
	}
}

func TestAudioPlan(t *testing.T) {
	available := []media.AudioTrack{
		{Number: 1, Codec: "AC-3", Channels: 6},
		{Number: 2, Codec: "AAC", Channels: 2},
	}
	tests := []struct {
		name      string
		selected  []int
		container queue.Container
		available []media.AudioTrack
		want      []AudioOutput
		errText   string
	}{
		{
			name:      "AC3 passthrough and standard",
			selected:  []int{1},
			container: queue.ContainerMKV,
			want: []AudioOutput{
				{1, AudioHigh, "copy:ac3", 640, "5point1", "auto"},
				{1, AudioStandard, "ca_aac", 160, "stereo", "auto"},
			},
		},
		{
			name:      "AAC uses safe high fallback",
			selected:  []int{2},
			container: queue.ContainerMP4,
			want: []AudioOutput{
				{2, AudioHigh, "ca_aac", 640, "stereo", "auto"},
				{2, AudioStandard, "ca_aac", 160, "stereo", "auto"},
			},
		},
		{
			name:      "input order normalized",
			selected:  []int{2, 1},
			container: queue.ContainerMP4,
			want: []AudioOutput{
				{1, AudioHigh, "copy:ac3", 640, "5point1", "auto"},
				{1, AudioStandard, "ca_aac", 160, "stereo", "auto"},
				{2, AudioHigh, "ca_aac", 640, "stereo", "auto"},
				{2, AudioStandard, "ca_aac", 160, "stereo", "auto"},
			},
		},
		{name: "none", selected: nil, container: queue.ContainerMKV, errText: "at least one"},
		{name: "missing", selected: []int{2}, container: queue.ContainerMKV, available: available[:1], errText: "does not exist"},
		{
			name:      "unsupported track",
			selected:  []int{3},
			container: queue.ContainerMKV,
			available: append(append([]media.AudioTrack(nil), available...), media.AudioTrack{Number: 3}),
			errText:   "not supported",
		},
		{name: "duplicate selected", selected: []int{1, 1}, container: queue.ContainerMKV, errText: "more than once"},
		{name: "bad container", selected: []int{1}, container: "webm", errText: "unsupported container"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracks := available
			if tt.available != nil {
				tracks = tt.available
			}
			got, err := AudioPlan(tt.selected, tracks, tt.container)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("AudioPlan() error = %v, want containing %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("AudioPlan() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("AudioPlan() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func encodeJob() queue.Job {
	return queue.Job{
		ID:           "job-1",
		CreatedAt:    time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
		Input:        "/input/番組.mkv",
		Output:       "/output/番組_01.mkv",
		Preset:       "MKV Presets",
		Container:    queue.ContainerMKV,
		ChapterStart: 2,
		ChapterEnd:   5,
		AudioTracks:  []int{1, 2},
		Subtitles:    []int{1, 2},
	}
}

func TestEncodeArgs(t *testing.T) {
	job := encodeJob()
	preset := CuratedPresets()[1]
	tracks := []media.AudioTrack{
		{Number: 1, Codec: "AC3", Channels: 6},
		{Number: 2, Codec: "AAC", Channels: 2},
	}
	got, err := EncodeArgs(job, "/output/.chapterbrake-job-1-encode.mkv", preset, tracks)
	if err != nil {
		t.Fatalf("EncodeArgs() error = %v", err)
	}
	want := []string{
		"--json",
		"--preset", "H.264 MKV 1080p30",
		"-i", "/input/番組.mkv",
		"-o", "/output/.chapterbrake-job-1-encode.mkv",
		"--chapters", "2-5",
		"--markers",
		"--crop-mode", "auto",
		"--audio", "1,1,2,2",
		"--aencoder", "copy:ac3,ca_aac,ca_aac,ca_aac",
		"--ab", "640,160,640,160",
		"--mixdown", "5point1,stereo,stereo,stereo",
		"--arate", "auto,auto,auto,auto",
		"--subtitle", "1,2",
		"--subtitle-burned=none",
		"--subtitle-default=none",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EncodeArgs() =\n%q\nwant\n%q", got, want)
	}
	for _, forbidden := range []string{"--start-at", "--stop-at", "--preset-import-gui"} {
		for _, arg := range got {
			if arg == forbidden {
				t.Fatalf("EncodeArgs() contains forbidden %q", forbidden)
			}
		}
	}
}

func TestEncodeArgsUsesCuratedCropMode(t *testing.T) {
	job := encodeJob()
	job.Output = "/output/番組_01.mp4"
	job.Preset = "GCCX"
	job.Container = queue.ContainerMP4
	job.Subtitles = []int{}
	preset := CuratedPresets()[3]
	tracks := []media.AudioTrack{
		{Number: 1, Codec: "AC3", Channels: 6},
		{Number: 2, Codec: "AC3", Channels: 6},
	}
	got, err := EncodeArgs(job, "/output/.chapterbrake-job-1-encode.mp4", preset, tracks)
	if err != nil {
		t.Fatalf("EncodeArgs() error = %v", err)
	}
	if !containsPair(got, "--crop-mode", "none") {
		t.Fatalf("EncodeArgs() crop mode = %q", got)
	}
}

func TestEncodeArgsImportsGUIPresetFile(t *testing.T) {
	job := encodeJob()
	job.PresetFile = "/settings/My Presets.json"
	preset := CuratedPresets()[1]
	preset.ImportFile = job.PresetFile
	preset.CropMode = ""
	got, err := EncodeArgs(
		job,
		"/output/.chapterbrake-job-1-encode.mkv",
		preset,
		[]media.AudioTrack{{Number: 1, Codec: "AC3", Channels: 6}, {Number: 2, Codec: "AAC", Channels: 2}},
	)
	if err != nil {
		t.Fatalf("EncodeArgs() error = %v", err)
	}
	if !containsPair(got, "--preset-import-file", job.PresetFile) {
		t.Fatalf("EncodeArgs() = %q", got)
	}
}

func TestEncodeArgsMP4HasNoSubtitles(t *testing.T) {
	job := encodeJob()
	job.Output = "/output/番組_01.mp4"
	job.Preset = "MP4 Presets"
	job.Container = queue.ContainerMP4
	job.Subtitles = []int{}
	preset := CuratedPresets()[0]
	tracks := []media.AudioTrack{
		{Number: 1, Codec: "AC3", Channels: 6},
		{Number: 2, Codec: "AC3", Channels: 6},
	}
	got, err := EncodeArgs(job, "/output/.chapterbrake-job-1-encode.mp4", preset, tracks)
	if err != nil {
		t.Fatalf("EncodeArgs() error = %v", err)
	}
	if !containsPair(got, "--subtitle", "none") {
		t.Fatalf("EncodeArgs() lacks explicit subtitle none: %q", got)
	}
}

func TestEncodeArgsValidation(t *testing.T) {
	job := encodeJob()
	preset := CuratedPresets()[1]
	tracks := []media.AudioTrack{{Number: 1, Codec: "AC3", Channels: 6}, {Number: 2, Codec: "AC3", Channels: 6}}
	tests := []struct {
		name       string
		changeJob  func(*queue.Job)
		changePath func() string
		change     func(*Preset)
	}{
		{"preset mismatch", func(j *queue.Job) { j.Preset = "other" }, nil, func(*Preset) {}},
		{"container mismatch", func(j *queue.Job) {}, nil, func(p *Preset) { p.Container = queue.ContainerMP4 }},
		{"relative temp", func(j *queue.Job) {}, func() string { return "temp.mkv" }, func(*Preset) {}},
		{"different directory", func(j *queue.Job) {}, func() string { return "/other/temp.mkv" }, func(*Preset) {}},
		{"final path", func(j *queue.Job) {}, func() string { return job.Output }, func(*Preset) {}},
		{"bad crop", func(j *queue.Job) {}, nil, func(p *Preset) { p.CropMode = "invalid" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testJob := job
			testPreset := preset
			tt.changeJob(&testJob)
			tt.change(&testPreset)
			path := "/output/temp.mkv"
			if tt.changePath != nil {
				path = tt.changePath()
			}
			if _, err := EncodeArgs(testJob, path, testPreset, tracks); err == nil {
				t.Fatal("EncodeArgs() error = nil")
			}
		})
	}
}

func TestSmallArgumentBuilders(t *testing.T) {
	if got := PresetListArgs(); !reflect.DeepEqual(got, []string{"--preset-list"}) {
		t.Fatalf("PresetListArgs() = %q", got)
	}
	export, err := PresetExportArgs("Fast 1080p30", "probe", "/tmp/probe.json")
	want := []string{"--preset", "Fast 1080p30", "--preset-export", "probe", "--preset-export-file", "/tmp/probe.json"}
	if err != nil || !reflect.DeepEqual(export, want) {
		t.Fatalf("PresetExportArgs() = %q, %v", export, err)
	}
}

func containsPair(values []string, first, second string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
}
