package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
)

const probeCaptureLimit = 16 << 20

type Probe struct {
	Streams  []ProbeStream  `json:"streams"`
	Chapters []ProbeChapter `json:"chapters"`
	Format   ProbeFormat    `json:"format"`
}

type ProbeStream struct {
	Index      int               `json:"index"`
	CodecName  string            `json:"codec_name"`
	CodecType  string            `json:"codec_type"`
	TimeBase   string            `json:"time_base"`
	StartTime  string            `json:"start_time"`
	Duration   string            `json:"duration"`
	SampleRate string            `json:"sample_rate"`
	Channels   int               `json:"channels"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	Tags       map[string]string `json:"tags"`
}

type ProbeChapter struct {
	ID        int               `json:"id"`
	TimeBase  string            `json:"time_base"`
	Start     int64             `json:"start"`
	End       int64             `json:"end"`
	StartTime string            `json:"start_time"`
	EndTime   string            `json:"end_time"`
	Tags      map[string]string `json:"tags"`
}

type ProbeFormat struct {
	FormatName string            `json:"format_name"`
	StartTime  string            `json:"start_time"`
	Duration   string            `json:"duration"`
	Tags       map[string]string `json:"tags"`
}

func ParseProbe(data []byte) (Probe, error) {
	var probe Probe
	if err := json.Unmarshal(data, &probe); err != nil {
		return Probe{}, fmt.Errorf("decode ffprobe JSON: %w", err)
	}
	if len(probe.Streams) == 0 {
		return Probe{}, fmt.Errorf("ffprobe returned no streams")
	}
	if probe.Format.FormatName == "" {
		return Probe{}, fmt.Errorf("ffprobe returned no format name")
	}
	return probe, nil
}

func (p Probe) Title() string {
	return p.Format.Tags["title"]
}

func (p Probe) MajorBrand() string {
	return p.Format.Tags["major_brand"]
}

func VerifyTitleAndStructure(before, after Probe, expectedTitle string, container queue.Container) error {
	if after.Title() != expectedTitle {
		return fmt.Errorf("title = %q, want %q", after.Title(), expectedTitle)
	}
	if err := verifyContainer(after.Format.FormatName, container); err != nil {
		return err
	}
	if !reflect.DeepEqual(streamStructures(before.Streams), streamStructures(after.Streams)) {
		return fmt.Errorf("stream structure changed during title processing")
	}
	if !reflect.DeepEqual(chapterStructures(before.Chapters), chapterStructures(after.Chapters)) {
		return fmt.Errorf("chapter structure changed during title processing")
	}
	if before.Format.StartTime != after.Format.StartTime || before.Format.Duration != after.Format.Duration {
		return fmt.Errorf("format timing changed during title processing")
	}
	return nil
}

func verifyContainer(formatName string, container queue.Container) error {
	formats := strings.Split(formatName, ",")
	for _, format := range formats {
		switch container {
		case queue.ContainerMKV:
			if format == "matroska" || format == "webm" {
				return nil
			}
		case queue.ContainerMP4:
			if format == "mov" || format == "mp4" {
				return nil
			}
		default:
			return fmt.Errorf("unsupported container %q", container)
		}
	}
	return fmt.Errorf("ffprobe format %q does not match container %q", formatName, container)
}

type streamStructure struct {
	Index      int
	CodecName  string
	CodecType  string
	TimeBase   string
	StartTime  string
	Duration   string
	SampleRate string
	Channels   int
	Width      int
	Height     int
	Language   string
}

func streamStructures(streams []ProbeStream) []streamStructure {
	result := make([]streamStructure, len(streams))
	for i, stream := range streams {
		language := ""
		if stream.CodecType == "audio" || stream.CodecType == "subtitle" {
			language = stream.Tags["language"]
		}
		result[i] = streamStructure{
			Index:      stream.Index,
			CodecName:  stream.CodecName,
			CodecType:  stream.CodecType,
			TimeBase:   stream.TimeBase,
			StartTime:  stream.StartTime,
			Duration:   stream.Duration,
			SampleRate: stream.SampleRate,
			Channels:   stream.Channels,
			Width:      stream.Width,
			Height:     stream.Height,
			Language:   language,
		}
	}
	return result
}

type chapterStructure struct {
	ID        int
	TimeBase  string
	Start     int64
	End       int64
	StartTime string
	EndTime   string
	Title     string
}

func chapterStructures(chapters []ProbeChapter) []chapterStructure {
	result := make([]chapterStructure, len(chapters))
	for i, chapter := range chapters {
		result[i] = chapterStructure{
			ID:        chapter.ID,
			TimeBase:  chapter.TimeBase,
			Start:     chapter.Start,
			End:       chapter.End,
			StartTime: chapter.StartTime,
			EndTime:   chapter.EndTime,
			Title:     chapter.Tags["title"],
		}
	}
	return result
}

type Prober struct {
	Executor process.Executor
	FFProbe  string
}

func (p Prober) Probe(ctx context.Context, path string, stdout, stderr io.Writer) (Probe, error) {
	if p.Executor == nil {
		return Probe{}, fmt.Errorf("ffprobe executor is nil")
	}
	args, err := FFProbeArgs(path)
	if err != nil {
		return Probe{}, err
	}
	executable := p.FFProbe
	if executable == "" {
		executable = "ffprobe"
	}
	if stdout == nil {
		stdout = io.Discard
	}
	capture := process.NewLimitedCapture(probeCaptureLimit)
	if err := p.Executor.Run(
		ctx,
		process.Invocation{Executable: executable, Args: args},
		io.MultiWriter(stdout, capture),
		stderr,
	); err != nil {
		return Probe{}, fmt.Errorf("ffprobe %s: %w", filepath.Base(path), err)
	}
	data, err := capture.Bytes()
	if err != nil {
		return Probe{}, fmt.Errorf("capture ffprobe output: %w", err)
	}
	probe, err := ParseProbe(data)
	if err != nil {
		return Probe{}, fmt.Errorf("parse ffprobe output for %s: %w", filepath.Base(path), err)
	}
	return probe, nil
}
