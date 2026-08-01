package handbrake

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
)

func PresetListArgs() []string {
	return []string{"--preset-list"}
}

func PresetExportArgs(name, exportName, output string) ([]string, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(exportName) == "" {
		return nil, fmt.Errorf("preset and export names must not be empty")
	}
	if !filepath.IsAbs(output) {
		return nil, fmt.Errorf("preset export path must be absolute: %q", output)
	}
	return []string{
		"--preset", name,
		"--preset-export", exportName,
		"--preset-export-file", output,
	}, nil
}

func EncodeArgs(job queue.Job, encodeOutput string, preset Preset, availableAudio []media.AudioTrack) ([]string, error) {
	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("invalid encode job: %w", err)
	}
	if err := preset.Validate(); err != nil {
		return nil, fmt.Errorf("invalid preset: %w", err)
	}
	if job.Preset != preset.DisplayName {
		return nil, fmt.Errorf("job preset %q does not match selected preset %q", job.Preset, preset.DisplayName)
	}
	if job.Container != preset.Container {
		return nil, fmt.Errorf("job container %q does not match preset container %q", job.Container, preset.Container)
	}
	if !filepath.IsAbs(encodeOutput) {
		return nil, fmt.Errorf("encode output must be absolute: %q", encodeOutput)
	}
	if filepath.Dir(encodeOutput) != filepath.Dir(job.Output) {
		return nil, fmt.Errorf("encode output must be in final output directory")
	}
	if encodeOutput == job.Output {
		return nil, fmt.Errorf("HandBrake output must not be the final output path")
	}

	audio, err := AudioPlan(job.AudioSelections, availableAudio, job.Container)
	if err != nil {
		return nil, err
	}

	args := []string{
		"--json",
	}
	if preset.ImportFile != "" {
		if job.PresetFile != preset.ImportFile {
			return nil, fmt.Errorf("job preset file %q does not match selected preset file %q", job.PresetFile, preset.ImportFile)
		}
		args = append(args, "--preset-import-file", preset.ImportFile)
	}
	args = append(args,
		"--preset", preset.HandBrakeName,
		"-i", job.Input,
		"-o", encodeOutput,
		"--chapters", fmt.Sprintf("%d-%d", job.ChapterStart, job.ChapterEnd),
		"--markers",
	)
	if preset.CropMode != "" {
		args = append(args, "--crop-mode", preset.CropMode)
	}
	if len(audio) == 0 {
		args = append(args, "--audio", "none")
	} else {
		audioTracks := make([]string, len(audio))
		encoders := make([]string, len(audio))
		bitrates := make([]string, len(audio))
		mixdowns := make([]string, len(audio))
		sampleRates := make([]string, len(audio))
		for i, output := range audio {
			audioTracks[i] = strconv.Itoa(output.InputTrack)
			encoders[i] = output.Encoder
			bitrates[i] = strconv.Itoa(output.Bitrate)
			mixdowns[i] = output.Mixdown
			sampleRates[i] = output.SampleRate
		}
		args = append(args,
			"--audio", strings.Join(audioTracks, ","),
			"--aencoder", strings.Join(encoders, ","),
			"--ab", strings.Join(bitrates, ","),
			"--mixdown", strings.Join(mixdowns, ","),
			"--arate", strings.Join(sampleRates, ","),
		)
	}

	subtitles := "none"
	if len(job.Subtitles) > 0 {
		numbers := make([]string, len(job.Subtitles))
		for i, number := range job.Subtitles {
			numbers[i] = strconv.Itoa(number)
		}
		subtitles = strings.Join(numbers, ",")
	}
	args = append(args,
		"--subtitle", subtitles,
		"--subtitle-burned=none",
		"--subtitle-default=none",
	)
	return args, nil
}
