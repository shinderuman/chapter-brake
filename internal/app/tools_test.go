package app

import "testing"

func TestVersionLine(t *testing.T) {
	tests := []struct {
		name    string
		outputs []string
		want    string
	}{
		{"HandBrakeCLI", []string{"[log] startup\nHandBrake 1.11.2\n"}, "HandBrake 1.11.2"},
		{"ffmpeg", []string{"ffmpeg version 8.1.2\nbuilt with"}, "ffmpeg version 8.1.2"},
		{"ffprobe", []string{"", "ffprobe version 8.1.2\n"}, "ffprobe version 8.1.2"},
		{"mkvpropedit", []string{"mkvpropedit v100.0\n"}, "mkvpropedit v100.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionLine(tt.name, tt.outputs...); got != tt.want {
				t.Fatalf("versionLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolPath(t *testing.T) {
	tools := []ToolInfo{{Name: "ffmpeg", Path: "/opt/ffmpeg"}}
	if got := ToolPath(tools, "ffmpeg"); got != "/opt/ffmpeg" {
		t.Fatalf("ToolPath() = %q", got)
	}
	if got := ToolPath(tools, "missing"); got != "" {
		t.Fatalf("ToolPath(missing) = %q", got)
	}
}
