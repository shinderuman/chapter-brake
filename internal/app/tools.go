package app

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"chapterbrake/internal/process"
)

type ToolInfo struct {
	Name    string
	Path    string
	Version string
}

type toolSpec struct {
	name string
	args []string
}

func InspectTools(ctx context.Context, executor process.Executor) ([]ToolInfo, error) {
	if executor == nil {
		return nil, fmt.Errorf("tool executor is nil")
	}
	specs := []toolSpec{
		{name: "HandBrakeCLI", args: []string{"--version"}},
		{name: "ffmpeg", args: []string{"-version"}},
		{name: "ffprobe", args: []string{"-version"}},
		{name: "mkvpropedit", args: []string{"--version"}},
	}
	result := make([]ToolInfo, 0, len(specs))
	for _, spec := range specs {
		path, err := exec.LookPath(spec.name)
		if err != nil {
			return nil, fmt.Errorf("find required tool %s: %w", spec.name, err)
		}
		stdout := process.NewLimitedCapture(1 << 20)
		stderr := process.NewLimitedCapture(1 << 20)
		if err := executor.Run(
			ctx,
			process.Invocation{Executable: path, Args: spec.args},
			stdout,
			stderr,
		); err != nil {
			return nil, fmt.Errorf("inspect required tool %s: %w", spec.name, err)
		}
		stdoutData, err := stdout.Bytes()
		if err != nil {
			return nil, err
		}
		stderrData, err := stderr.Bytes()
		if err != nil {
			return nil, err
		}
		version := versionLine(spec.name, string(stdoutData), string(stderrData))
		if version == "" {
			return nil, fmt.Errorf("required tool %s returned no version text", spec.name)
		}
		result = append(result, ToolInfo{Name: spec.name, Path: path, Version: version})
	}
	return result, nil
}

func ToolPath(tools []ToolInfo, name string) string {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Path
		}
	}
	return ""
}

func versionLine(name string, outputs ...string) string {
	nameLower := strings.ToLower(name)
	var fallback string
	for _, output := range outputs {
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if fallback == "" {
				fallback = line
			}
			lower := strings.ToLower(line)
			if strings.Contains(lower, nameLower) ||
				(name == "HandBrakeCLI" && strings.Contains(lower, "handbrake ")) {
				return line
			}
		}
	}
	return fallback
}
