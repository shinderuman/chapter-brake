package metadata

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
)

const probeJSON = `{
  "streams": [
    {"index":0,"codec_name":"h264","codec_type":"video","time_base":"1/1000","start_time":"0.000000","duration":"6.000000","width":320,"height":180},
    {"index":1,"codec_name":"ac3","codec_type":"audio","time_base":"1/48000","start_time":"0.000000","duration":"6.000000","sample_rate":"48000","channels":6,"tags":{"language":"jpn"}}
  ],
  "chapters": [
    {"id":0,"time_base":"1/1000","start":0,"end":3000,"start_time":"0.000000","end_time":"3.000000","tags":{"title":"One"}}
  ],
  "format": {
    "format_name":"matroska,webm","start_time":"0.000000","duration":"6.000000",
    "tags":{"title":"日本語 Title #01","major_brand":"mp42"}
  }
}`

func TestParseProbe(t *testing.T) {
	got, err := ParseProbe([]byte(probeJSON))
	if err != nil {
		t.Fatalf("ParseProbe() error = %v", err)
	}
	if got.Title() != "日本語 Title #01" || got.MajorBrand() != "mp42" {
		t.Fatalf("probe metadata = title %q, brand %q", got.Title(), got.MajorBrand())
	}
	for _, input := range []string{
		`{`,
		`{"format":{"format_name":"mkv"}}`,
		`{"streams":[{"index":0}]}`,
	} {
		if _, err := ParseProbe([]byte(input)); err == nil {
			t.Fatalf("ParseProbe(%q) error = nil", input)
		}
	}
}

func TestVerifyTitleAndStructure(t *testing.T) {
	before, err := ParseProbe([]byte(probeJSON))
	if err != nil {
		t.Fatal(err)
	}
	after := before
	after.Format.Tags = map[string]string{"title": "expected"}
	after.Streams = append([]ProbeStream(nil), before.Streams...)
	after.Streams = append(after.Streams, ProbeStream{
		Index:     2,
		CodecName: "bin_data",
		CodecType: "data",
		TimeBase:  "1/1000",
		Tags:      map[string]string{"language": "eng"},
	})
	before.Streams = append(append([]ProbeStream(nil), before.Streams...), ProbeStream{
		Index:     2,
		CodecName: "bin_data",
		CodecType: "data",
		TimeBase:  "1/1000",
		Tags:      map[string]string{"language": "und"},
	})
	if err := VerifyTitleAndStructure(before, after, "expected", queue.ContainerMKV); err != nil {
		t.Fatalf("VerifyTitleAndStructure() error = %v", err)
	}
	before, err = ParseProbe([]byte(probeJSON))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		change  func(*Probe)
		errText string
	}{
		{"wrong title", func(p *Probe) { p.Format.Tags["title"] = "wrong" }, "title"},
		{"wrong container", func(p *Probe) { p.Format.FormatName = "mov,mp4" }, "does not match"},
		{"stream changed", func(p *Probe) { p.Streams[0].CodecName = "hevc" }, "stream structure"},
		{"chapter changed", func(p *Probe) { p.Chapters[0].End++ }, "chapter structure"},
		{"timing changed", func(p *Probe) { p.Format.Duration = "7.0" }, "format timing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testAfter, err := ParseProbe([]byte(probeJSON))
			if err != nil {
				t.Fatal(err)
			}
			testAfter.Format.Tags["title"] = "expected"
			tt.change(&testAfter)
			err = VerifyTitleAndStructure(before, testAfter, "expected", queue.ContainerMKV)
			if err == nil || !strings.Contains(err.Error(), tt.errText) {
				t.Fatalf("VerifyTitleAndStructure() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}

func TestVerifyMP4Container(t *testing.T) {
	before, _ := ParseProbe([]byte(probeJSON))
	after, _ := ParseProbe([]byte(probeJSON))
	before.Format.FormatName = "mov,mp4,m4a,3gp,3g2,mj2"
	after.Format.FormatName = before.Format.FormatName
	after.Format.Tags["title"] = "title"
	if err := VerifyTitleAndStructure(before, after, "title", queue.ContainerMP4); err != nil {
		t.Fatalf("VerifyTitleAndStructure(MP4) error = %v", err)
	}
	if err := verifyContainer("anything", "webm"); err == nil {
		t.Fatal("verifyContainer(unsupported) error = nil")
	}
}

type probeExecutor struct {
	output []byte
	err    error
	got    process.Invocation
}

func (e *probeExecutor) Run(
	_ context.Context,
	invocation process.Invocation,
	stdout io.Writer,
	_ io.Writer,
) error {
	e.got = invocation
	_, _ = stdout.Write(e.output)
	return e.err
}

func TestProber(t *testing.T) {
	executor := &probeExecutor{output: []byte(probeJSON)}
	var raw bytes.Buffer
	prober := Prober{Executor: executor, FFProbe: "/opt/homebrew/bin/ffprobe"}
	got, err := prober.Probe(context.Background(), "/output/日本語.mkv", &raw, io.Discard)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if got.Title() != "日本語 Title #01" || raw.String() != probeJSON {
		t.Fatalf("Probe() = %#v, raw %q", got, raw.String())
	}
	wantArgs, _ := FFProbeArgs("/output/日本語.mkv")
	if executor.got.Executable != "/opt/homebrew/bin/ffprobe" || !reflect.DeepEqual(executor.got.Args, wantArgs) {
		t.Fatalf("invocation = %#v", executor.got)
	}
}

func TestProberCommandError(t *testing.T) {
	commandErr := errors.New("probe failed")
	prober := Prober{Executor: &probeExecutor{err: commandErr}}
	if _, err := prober.Probe(context.Background(), "/output/file.mkv", nil, nil); !errors.Is(err, commandErr) {
		t.Fatalf("Probe() error = %v", err)
	}
}
