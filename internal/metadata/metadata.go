package metadata

import (
	"fmt"
	"path/filepath"
	"strings"

	"chapterbrake/internal/queue"
)

type Paths struct {
	Final    string
	Encode   string
	Metadata string
}

// TitleTarget is the file modified or created by the title-setting step.
func (p Paths) TitleTarget(container queue.Container) (string, error) {
	switch container {
	case queue.ContainerMKV:
		if p.Encode == "" {
			return "", fmt.Errorf("MKV encode path is empty")
		}
		return p.Encode, nil
	case queue.ContainerMP4:
		if p.Metadata == "" {
			return "", fmt.Errorf("MP4 metadata path is empty")
		}
		return p.Metadata, nil
	default:
		return "", fmt.Errorf("unsupported container %q", container)
	}
}

// PublishSource is the fully titled and verified file renamed to Final.
func (p Paths) PublishSource(container queue.Container) (string, error) {
	return p.TitleTarget(container)
}

func TitleFromOutput(output string) (string, error) {
	if !filepath.IsAbs(output) {
		return "", fmt.Errorf("output path must be absolute: %q", output)
	}
	filename := filepath.Base(output)
	extension := filepath.Ext(filename)
	if extension == "" {
		return "", fmt.Errorf("output has no extension: %q", output)
	}
	title := strings.TrimSuffix(filename, extension)
	if title == "" {
		return "", fmt.Errorf("output stem must not be empty")
	}
	return title, nil
}

func TemporaryPaths(job queue.Job) (Paths, error) {
	if err := job.Validate(); err != nil {
		return Paths{}, fmt.Errorf("invalid job: %w", err)
	}
	dir := filepath.Dir(job.Output)
	extension := "." + string(job.Container)
	paths := Paths{
		Final:  job.Output,
		Encode: filepath.Join(dir, ".chapterbrake-"+job.ID+"-encode"+extension),
	}
	if job.Container == queue.ContainerMP4 {
		paths.Metadata = filepath.Join(dir, ".chapterbrake-"+job.ID+"-metadata.mp4")
	}
	if paths.Encode == paths.Final || paths.Metadata == paths.Final {
		return Paths{}, fmt.Errorf("temporary path collides with final output")
	}
	return paths, nil
}

func MKVPropEditArgs(path, title string) ([]string, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("MKV path must be absolute: %q", path)
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title must not be empty")
	}
	return []string{path, "--edit", "info", "--set", "title=" + title}, nil
}

func MP4MetadataArgs(input, output, title, majorBrand string) ([]string, error) {
	if !filepath.IsAbs(input) || !filepath.IsAbs(output) {
		return nil, fmt.Errorf("MP4 input and output paths must be absolute")
	}
	if input == output {
		return nil, fmt.Errorf("MP4 metadata input and output must differ")
	}
	if filepath.Dir(input) != filepath.Dir(output) {
		return nil, fmt.Errorf("MP4 metadata output must share the input directory")
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title must not be empty")
	}
	if strings.TrimSpace(majorBrand) == "" {
		return nil, fmt.Errorf("major brand must not be empty")
	}
	return []string{
		"-i", input,
		"-map", "0:v",
		"-map", "0:a?",
		"-map_metadata", "0",
		"-map_chapters", "0",
		"-c", "copy",
		"-metadata", "title=" + title,
		"-brand", majorBrand,
		output,
	}, nil
}

func FFProbeArgs(path string) ([]string, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("ffprobe path must be absolute: %q", path)
	}
	return []string{
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-show_chapters",
		"-of", "json",
		path,
	}, nil
}
