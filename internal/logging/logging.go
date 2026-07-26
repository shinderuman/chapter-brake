package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
)

func OpenApp(directory string, now time.Time) (*slog.Logger, io.Closer, string, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, nil, "", fmt.Errorf("create log directory: %w", err)
	}
	path := filepath.Join(directory, "app-"+now.Format("2006-01-02")+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, "", fmt.Errorf("open app log: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return logger, file, path, nil
}

type JobLog struct {
	mu     sync.Mutex
	file   *os.File
	path   string
	prefix string
	logDir string
}

func OpenJob(directory string, job queue.Job, now time.Time) (*JobLog, error) {
	if err := job.Validate(); err != nil {
		return nil, fmt.Errorf("invalid job for logging: %w", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	stem := strings.TrimSuffix(filepath.Base(job.Output), filepath.Ext(job.Output))
	prefix := fmt.Sprintf(
		"job-%s-%s-%s",
		now.Format("20060102T150405.000000000"),
		job.ID,
		sanitize(stem),
	)
	path := filepath.Join(directory, prefix+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create job log: %w", err)
	}
	log := &JobLog{file: file, path: path, prefix: prefix, logDir: directory}
	log.Event("job-start",
		"id", job.ID,
		"input", job.Input,
		"output", job.Output,
		"preset", job.Preset,
		"container", job.Container,
		"chapters", fmt.Sprintf("%d-%d", job.ChapterStart, job.ChapterEnd),
		"audio_tracks", fmt.Sprint(job.AudioTracks),
		"subtitles", fmt.Sprint(job.Subtitles),
	)
	return log, nil
}

func (l *JobLog) Path() string {
	return l.path
}

func (l *JobLog) Event(name string, keyValues ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	_, _ = fmt.Fprintf(l.file, "%s event=%q", time.Now().Format(time.RFC3339Nano), name)
	for i := 0; i+1 < len(keyValues); i += 2 {
		_, _ = fmt.Fprintf(l.file, " %v=%q", keyValues[i], fmt.Sprint(keyValues[i+1]))
	}
	_, _ = fmt.Fprintln(l.file)
}

func (l *JobLog) OpenCommand(stage string, invocation process.Invocation) (*CommandLog, error) {
	safeStage := sanitize(stage)
	if safeStage == "" {
		return nil, fmt.Errorf("command stage must not be empty")
	}
	stdoutPath := filepath.Join(l.logDir, l.prefix+"-"+safeStage+".stdout.log")
	stderrPath := filepath.Join(l.logDir, l.prefix+"-"+safeStage+".stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create %s stdout log: %w", stage, err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = stdout.Close()
		_ = os.Remove(stdoutPath)
		return nil, fmt.Errorf("create %s stderr log: %w", stage, err)
	}
	l.Event("command-start",
		"stage", stage,
		"executable", invocation.Executable,
		"args", fmt.Sprintf("%q", invocation.Args),
		"stdout_log", stdoutPath,
		"stderr_log", stderrPath,
	)
	return &CommandLog{
		Stdout:     stdout,
		Stderr:     stderr,
		stdout:     stdout,
		stderr:     stderr,
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
		parent:     l,
		stage:      stage,
	}, nil
}

func (l *JobLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

type CommandLog struct {
	Stdout io.Writer
	Stderr io.Writer

	stdoutPath string
	stderrPath string
	stdout     *os.File
	stderr     *os.File
	parent     *JobLog
	stage      string
}

func (l *CommandLog) Close(result error) error {
	stdoutErr := l.stdout.Close()
	stderrErr := l.stderr.Close()
	if result != nil {
		l.parent.Event("command-finish", "stage", l.stage, "result", "error", "error", result)
	} else {
		l.parent.Event("command-finish", "stage", l.stage, "result", "success")
	}
	closeErr := errors.Join(stdoutErr, stderrErr)
	if closeErr == nil {
		return nil
	}
	return fmt.Errorf("close command logs: %w", closeErr)
}

func sanitize(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r),
			unicode.IsDigit(r),
			r == '-',
			r == '_',
			r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return strings.Trim(builder.String(), "._-")
}
