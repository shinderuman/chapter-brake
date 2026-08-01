package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"chapterbrake/internal/process"
)

const scanCaptureLimit = 16 << 20

type Scanner struct {
	Executor  process.Executor
	HandBrake string
}

func (s Scanner) Scan(ctx context.Context, input string, stdout, stderr io.Writer) (Info, error) {
	return s.ScanWithProgress(ctx, input, stdout, stderr, nil)
}

func (s Scanner) ScanWithProgress(
	ctx context.Context,
	input string,
	stdout, stderr io.Writer,
	progress func(float64),
) (Info, error) {
	if s.Executor == nil {
		return Info{}, fmt.Errorf("scan executor is nil")
	}
	args, err := ScanArgs(input)
	if err != nil {
		return Info{}, err
	}
	executable := s.HandBrake
	if executable == "" {
		executable = "HandBrakeCLI"
	}

	capture := process.NewLimitedCapture(scanCaptureLimit)
	if stdout == nil {
		stdout = io.Discard
	}
	output := io.MultiWriter(stdout, capture)
	if progress != nil {
		output = io.MultiWriter(stdout, capture, newScanProgressWriter(progress))
	}
	if err := s.Executor.Run(
		ctx,
		process.Invocation{Executable: executable, Args: args},
		output,
		stderr,
	); err != nil {
		return Info{}, fmt.Errorf("scan %s: %w", input, err)
	}
	data, err := capture.Bytes()
	if err != nil {
		return Info{}, fmt.Errorf("capture HandBrake scan: %w", err)
	}
	info, err := ParseScanOutput(data)
	if err != nil {
		return Info{}, fmt.Errorf("parse HandBrake scan for %s: %w", filepath.Base(input), err)
	}
	return info, nil
}

func ScanArgs(input string) ([]string, error) {
	if !filepath.IsAbs(input) {
		return nil, fmt.Errorf("scan input must be absolute: %q", input)
	}
	return []string{"--json", "-i", input, "--scan"}, nil
}

type scanDocument struct {
	TitleList []scanTitle `json:"TitleList"`
}

type scanTitle struct {
	AudioList    []scanAudio    `json:"AudioList"`
	ChapterList  []scanChapter  `json:"ChapterList"`
	Duration     scanDuration   `json:"Duration"`
	SubtitleList []scanSubtitle `json:"SubtitleList"`
}

type scanDuration struct {
	Hours   int64 `json:"Hours"`
	Minutes int64 `json:"Minutes"`
	Seconds int64 `json:"Seconds"`
	Ticks   int64 `json:"Ticks"`
}

type scanAudio struct {
	ChannelCount int    `json:"ChannelCount"`
	CodecName    string `json:"CodecName"`
	Language     string `json:"Language"`
	LanguageCode string `json:"LanguageCode"`
	Name         string `json:"Name"`
	SampleRate   int    `json:"SampleRate"`
	TrackNumber  int    `json:"TrackNumber"`
}

type scanChapter struct {
	Duration scanDuration `json:"Duration"`
	Name     string       `json:"Name"`
}

type scanSubtitle struct {
	Format       string `json:"Format"`
	Language     string `json:"Language"`
	LanguageCode string `json:"LanguageCode"`
	Name         string `json:"Name"`
	SourceName   string `json:"SourceName"`
	TrackNumber  int    `json:"TrackNumber"`
}

func ParseScanOutput(output []byte) (Info, error) {
	const marker = "JSON Title Set:"
	index := strings.Index(string(output), marker)
	if index < 0 {
		return Info{}, fmt.Errorf("%q marker not found", marker)
	}
	jsonData := output[index+len(marker):]
	var document scanDocument
	if err := json.NewDecoder(bytes.NewReader(jsonData)).Decode(&document); err != nil {
		return Info{}, fmt.Errorf("decode title set: %w", err)
	}
	if len(document.TitleList) != 1 {
		return Info{}, fmt.Errorf("scan contains %d titles, want exactly 1 for an MKV", len(document.TitleList))
	}
	title := document.TitleList[0]
	if len(title.ChapterList) == 0 {
		return Info{}, fmt.Errorf("input has no chapters")
	}

	info := Info{
		Duration:       title.Duration.duration(),
		Chapters:       make([]Chapter, len(title.ChapterList)),
		AudioTracks:    make([]AudioTrack, len(title.AudioList)),
		SubtitleTracks: make([]SubtitleTrack, len(title.SubtitleList)),
	}
	var chapterStart time.Duration
	for i, chapter := range title.ChapterList {
		info.Chapters[i] = Chapter{Number: i + 1, Start: chapterStart, Title: chapter.Name}
		chapterStart += chapter.Duration.duration()
	}
	for i, track := range title.AudioList {
		info.AudioTracks[i] = AudioTrack{
			Number:     track.TrackNumber,
			Language:   firstNonempty(track.Language, track.LanguageCode),
			Name:       track.Name,
			Codec:      track.CodecName,
			Channels:   track.ChannelCount,
			SampleRate: track.SampleRate,
		}
	}
	for i, track := range title.SubtitleList {
		info.SubtitleTracks[i] = SubtitleTrack{
			Number:   track.TrackNumber,
			Language: firstNonempty(track.Language, track.LanguageCode),
			Name:     track.Name,
			Format:   firstNonempty(track.SourceName, track.Format),
		}
	}
	if err := ValidateChapters(info.Chapters); err != nil {
		return Info{}, err
	}
	if info.Duration <= 0 {
		return Info{}, fmt.Errorf("input duration must be positive")
	}
	return info, nil
}

func (d scanDuration) duration() time.Duration {
	if d.Ticks > 0 {
		return time.Duration(d.Ticks) * time.Second / 90000
	}
	return time.Duration(d.Hours)*time.Hour +
		time.Duration(d.Minutes)*time.Minute +
		time.Duration(d.Seconds)*time.Second
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
