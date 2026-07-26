package handbrake

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"chapterbrake/internal/process"
)

const presetListCaptureLimit = 8 << 20

type StandardPreset struct {
	Category string
	Name     string
}

type Catalog struct {
	Executor      process.Executor
	HandBrake     string
	TempDirectory string
}

func (c Catalog) Curated() []Preset {
	return CuratedPresets()
}

func (c Catalog) ListStandard(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
) ([]StandardPreset, error) {
	if c.Executor == nil {
		return nil, fmt.Errorf("preset executor is nil")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	stdoutCapture := process.NewLimitedCapture(presetListCaptureLimit)
	stderrCapture := process.NewLimitedCapture(presetListCaptureLimit)
	err := c.Executor.Run(
		ctx,
		process.Invocation{Executable: c.executable(), Args: PresetListArgs()},
		io.MultiWriter(stdout, stdoutCapture),
		io.MultiWriter(stderr, stderrCapture),
	)
	if err != nil {
		return nil, fmt.Errorf("list HandBrake presets: %w", err)
	}
	stdoutData, err := stdoutCapture.Bytes()
	if err != nil {
		return nil, fmt.Errorf("capture HandBrake preset list stdout: %w", err)
	}
	stderrData, err := stderrCapture.Bytes()
	if err != nil {
		return nil, fmt.Errorf("capture HandBrake preset list stderr: %w", err)
	}
	for _, data := range [][]byte{stdoutData, stderrData} {
		if presets, parseErr := ParsePresetList(data); parseErr == nil {
			return presets, nil
		}
	}
	combined := make([]byte, 0, len(stdoutData)+len(stderrData)+1)
	combined = append(combined, stdoutData...)
	combined = append(combined, '\n')
	combined = append(combined, stderrData...)
	presets, err := ParsePresetList(combined)
	if err != nil {
		return nil, fmt.Errorf("parse HandBrake preset list: %w", err)
	}
	return presets, nil
}

func (c Catalog) Resolve(
	ctx context.Context,
	displayName string,
	stdout io.Writer,
	stderr io.Writer,
) (Preset, error) {
	if preset, ok := resolveCuratedPreset(displayName); ok {
		return preset, nil
	}
	if c.Executor == nil {
		return Preset{}, fmt.Errorf("preset executor is nil")
	}

	tempDirectory := c.TempDirectory
	if tempDirectory == "" {
		tempDirectory = os.TempDir()
	}
	temp, err := os.CreateTemp(tempDirectory, "chapterbrake-preset-*.json")
	if err != nil {
		return Preset{}, fmt.Errorf("reserve preset export path: %w", err)
	}
	exportPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(exportPath)
		return Preset{}, fmt.Errorf("close preset export placeholder: %w", err)
	}
	if err := os.Remove(exportPath); err != nil {
		return Preset{}, fmt.Errorf("remove preset export placeholder: %w", err)
	}
	defer os.Remove(exportPath)

	args, err := PresetExportArgs(displayName, "chapterbrake-probe", exportPath)
	if err != nil {
		return Preset{}, err
	}
	if err := c.Executor.Run(
		ctx,
		process.Invocation{Executable: c.executable(), Args: args},
		stdout,
		stderr,
	); err != nil {
		return Preset{}, fmt.Errorf("export HandBrake preset %q: %w", displayName, err)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		return Preset{}, fmt.Errorf("read exported HandBrake preset %q: %w", displayName, err)
	}
	preset, err := ParseExportedPreset(data, displayName)
	if err != nil {
		return Preset{}, fmt.Errorf("resolve HandBrake preset %q: %w", displayName, err)
	}
	preset.HandBrakeName = displayName
	return preset, nil
}

func (c Catalog) executable() string {
	if c.HandBrake != "" {
		return c.HandBrake
	}
	return "HandBrakeCLI"
}

func ParsePresetList(output []byte) ([]StandardPreset, error) {
	var presets []StandardPreset
	category := ""
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t\r")
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, "/") {
			category = strings.TrimSuffix(line, "/")
			continue
		}
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "        ") && category != "" {
			name := strings.TrimSpace(line)
			if name != "" {
				presets = append(presets, StandardPreset{Category: category, Name: name})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan preset list: %w", err)
	}
	if len(presets) == 0 {
		return nil, fmt.Errorf("no standard presets found")
	}
	return presets, nil
}
