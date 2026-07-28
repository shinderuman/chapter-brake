package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/logging"
	"chapterbrake/internal/media"
	"chapterbrake/internal/metadata"
	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runstate"
)

type QueueStore interface {
	ClaimHead() (queue.Job, bool, error)
	ReleaseHead(string) error
	CompleteHead(string) error
}

type MediaScanner interface {
	Scan(context.Context, string, io.Writer, io.Writer) (media.Info, error)
}

type MediaProber interface {
	Probe(context.Context, string, io.Writer, io.Writer) (metadata.Probe, error)
}

type Runner struct {
	Store        QueueStore
	Executor     process.Executor
	Scanner      MediaScanner
	Prober       MediaProber
	LogDirectory string
	AppLogger    *slog.Logger
	State        *runstate.Store

	HandBrake      string
	FFmpeg         string
	FFProbe        string
	MKVPropEdit    string
	Now            func() time.Time
	Progress       func(string, handbrake.Progress)
	Stage          func(string, string)
	Completed      func(string)
	PauseRequested func() bool
}

type pausableExecutor interface {
	process.Executor
	Pause() error
	Resume() error
	IsPaused() bool
}

type Result struct {
	Completed int
	Paused    bool
}

type JobError struct {
	JobID    string
	Stage    string
	LogPath  string
	Canceled bool
	Err      error
}

func (r *Runner) PauseCurrent() error {
	executor, ok := r.Executor.(pausableExecutor)
	if !ok {
		return fmt.Errorf("current command executor does not support pause")
	}
	return executor.Pause()
}

func (r *Runner) ResumeCurrent() error {
	executor, ok := r.Executor.(pausableExecutor)
	if !ok {
		return fmt.Errorf("current command executor does not support resume")
	}
	return executor.Resume()
}

func (r *Runner) CurrentPaused() bool {
	executor, ok := r.Executor.(pausableExecutor)
	return ok && executor.IsPaused()
}

func (r *Runner) Alert() (runstate.State, error) {
	if r.State == nil {
		return runstate.Idle(), nil
	}
	return r.State.Load()
}

func (r *Runner) DismissAlert(jobID string) error {
	if r.State == nil {
		return nil
	}
	state, err := r.State.Load()
	if err != nil {
		return err
	}
	if state.Status != runstate.StatusFailed || state.JobID != jobID {
		return nil
	}
	return r.clearState()
}

func (e *JobError) Error() string {
	if e.Canceled {
		return fmt.Sprintf("job %s canceled during %s: %v", e.JobID, e.Stage, e.Err)
	}
	return fmt.Sprintf("job %s failed during %s: %v", e.JobID, e.Stage, e.Err)
}

func (e *JobError) Unwrap() error {
	return e.Err
}

func (r Runner) Run(ctx context.Context) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	var result Result
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		job, ok, err := r.Store.ClaimHead()
		if err != nil {
			return result, fmt.Errorf("claim queue head: %w", err)
		}
		if !ok {
			if err := r.clearState(); err != nil {
				return result, err
			}
			return result, nil
		}
		if err := r.markRunning(job, "starting"); err != nil {
			_ = r.Store.ReleaseHead(job.ID)
			return result, err
		}
		r.logInfo("job starting", "job_id", job.ID, "output", job.Output)

		logPath, stage, err := r.runJob(ctx, job)
		if err != nil {
			releaseErr := r.Store.ReleaseHead(job.ID)
			err = errors.Join(err, releaseErr)
			canceled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
			if canceled {
				err = errors.Join(err, r.clearState())
			} else {
				err = errors.Join(err, r.markFailed(job, stage, logPath, err))
			}
			r.logError("job failed",
				"job_id", job.ID,
				"stage", stage,
				"canceled", canceled,
				"log_path", logPath,
				"error", err,
			)
			return result, &JobError{
				JobID:    job.ID,
				Stage:    stage,
				LogPath:  logPath,
				Canceled: canceled,
				Err:      err,
			}
		}

		if err := r.Store.CompleteHead(job.ID); err != nil {
			err = errors.Join(err, r.Store.ReleaseHead(job.ID))
			err = errors.Join(err, r.markFailed(job, "queue-save", logPath, err))
			return result, &JobError{JobID: job.ID, Stage: "queue-save", LogPath: logPath, Err: err}
		}
		if err := r.clearState(); err != nil {
			return result, err
		}
		result.Completed++
		if r.Completed != nil {
			r.Completed(job.ID)
		}
		r.logInfo("job completed", "job_id", job.ID, "output", job.Output, "log_path", logPath)
		if r.PauseRequested != nil && r.PauseRequested() {
			if err := r.clearState(); err != nil {
				return result, err
			}
			result.Paused = true
			return result, nil
		}
	}
}

func (r Runner) runJob(ctx context.Context, job queue.Job) (logPath string, stage string, resultErr error) {
	setStage := func(value string) {
		stage = value
		if r.Stage != nil {
			r.Stage(job.ID, value)
		}
	}
	setStage("validate")
	if err := os.MkdirAll(filepath.Dir(job.Output), 0o755); err != nil {
		return "", "create-output-directory", fmt.Errorf("create output directory: %w", err)
	}
	if err := validateRuntimePaths(job); err != nil {
		return "", stage, err
	}
	paths, err := metadata.TemporaryPaths(job)
	if err != nil {
		return "", stage, err
	}

	jobLog, err := logging.OpenJob(r.LogDirectory, job, r.now())
	if err != nil {
		return "", "open-log", err
	}
	logPath = jobLog.Path()
	defer func() {
		closeErr := jobLog.Close()
		if closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close job log: %w", closeErr))
		}
	}()

	published := false
	defer func() {
		if resultErr == nil || published {
			return
		}
		cleanupErr := removePaths(paths.Encode, paths.Metadata, paths.Final)
		if cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("clean failed job outputs: %w", cleanupErr))
		}
	}()

	setStage("prepare-output")
	if err := removePaths(paths.Final, paths.Encode, paths.Metadata); err != nil {
		return logPath, stage, err
	}
	jobLog.Event("output-prepared", "final", paths.Final, "encode", paths.Encode, "metadata", paths.Metadata)

	setStage("scan")
	info, err := r.scan(ctx, job, jobLog)
	if err != nil {
		return logPath, stage, err
	}
	preset, err := handbrake.ResolveQueuedPreset(job.Preset, job.Container, job.PresetFile)
	if err != nil {
		setStage("resolve-preset")
		return logPath, stage, err
	}
	encodeArgs, err := handbrake.EncodeArgs(job, paths.Encode, preset, info.AudioTracks)
	if err != nil {
		setStage("build-handbrake-args")
		return logPath, stage, err
	}

	setStage("handbrake")
	if err := r.runCommand(ctx, jobLog, job.ID, stage, process.Invocation{
		Executable: r.handBrake(),
		Args:       encodeArgs,
	}); err != nil {
		return logPath, stage, err
	}

	setStage("ffprobe-before")
	before, err := r.probe(ctx, paths.Encode, jobLog, stage)
	if err != nil {
		return logPath, stage, err
	}
	title, err := metadata.TitleFromOutput(job.Output)
	if err != nil {
		setStage("derive-title")
		return logPath, stage, err
	}

	publishSource := paths.Encode
	switch job.Container {
	case queue.ContainerMKV:
		setStage("mkvpropedit")
		args, err := metadata.MKVPropEditArgs(paths.Encode, title)
		if err != nil {
			return logPath, stage, err
		}
		if err := r.runCommand(ctx, jobLog, job.ID, stage, process.Invocation{
			Executable: r.mkvPropEdit(),
			Args:       args,
		}); err != nil {
			return logPath, stage, err
		}
	case queue.ContainerMP4:
		setStage("ffmpeg-metadata")
		args, err := metadata.MP4MetadataArgs(
			paths.Encode,
			paths.Metadata,
			title,
			before.MajorBrand(),
		)
		if err != nil {
			return logPath, stage, err
		}
		if err := r.runCommand(ctx, jobLog, job.ID, stage, process.Invocation{
			Executable: r.ffmpeg(),
			Args:       args,
		}); err != nil {
			return logPath, stage, err
		}
		publishSource = paths.Metadata
	default:
		setStage("title")
		return logPath, stage, fmt.Errorf("unsupported container %q", job.Container)
	}

	setStage("ffprobe-after")
	after, err := r.probe(ctx, publishSource, jobLog, stage)
	if err != nil {
		return logPath, stage, err
	}
	setStage("verify")
	if err := metadata.VerifyTitleAndStructure(before, after, title, job.Container); err != nil {
		return logPath, stage, err
	}
	jobLog.Event("title-verified", "title", title, "publish_source", publishSource)

	if job.Container == queue.ContainerMP4 {
		setStage("remove-encode-temp")
		if err := removePaths(paths.Encode); err != nil {
			return logPath, stage, err
		}
	}

	setStage("publish")
	if err := os.Rename(publishSource, paths.Final); err != nil {
		return logPath, stage, fmt.Errorf("rename %s to %s: %w", publishSource, paths.Final, err)
	}
	if err := syncDirectory(filepath.Dir(paths.Final)); err != nil {
		setStage("sync-output-directory")
		return logPath, stage, err
	}
	jobLog.Event("job-success", "final", paths.Final, "title", title)
	if err := jobLog.Close(); err != nil {
		setStage("close-log")
		return logPath, stage, err
	}
	published = true
	return logPath, "", nil
}

func (r Runner) scan(ctx context.Context, job queue.Job, jobLog *logging.JobLog) (media.Info, error) {
	args, _ := media.ScanArgs(job.Input)
	invocation := process.Invocation{Executable: r.handBrake(), Args: args}
	commandLog, err := jobLog.OpenCommand("scan", invocation)
	if err != nil {
		return media.Info{}, err
	}
	info, scanErr := r.Scanner.Scan(ctx, job.Input, commandLog.Stdout, commandLog.Stderr)
	closeErr := commandLog.Close(scanErr)
	return info, errors.Join(scanErr, closeErr)
}

func (r Runner) probe(
	ctx context.Context,
	path string,
	jobLog *logging.JobLog,
	stage string,
) (metadata.Probe, error) {
	args, _ := metadata.FFProbeArgs(path)
	invocation := process.Invocation{Executable: r.ffProbe(), Args: args}
	commandLog, err := jobLog.OpenCommand(stage, invocation)
	if err != nil {
		return metadata.Probe{}, err
	}
	probe, probeErr := r.Prober.Probe(ctx, path, commandLog.Stdout, commandLog.Stderr)
	closeErr := commandLog.Close(probeErr)
	return probe, errors.Join(probeErr, closeErr)
}

func (r Runner) runCommand(
	ctx context.Context,
	jobLog *logging.JobLog,
	jobID string,
	stage string,
	invocation process.Invocation,
) error {
	commandLog, err := jobLog.OpenCommand(stage, invocation)
	if err != nil {
		return err
	}
	stdout := commandLog.Stdout
	if stage == "handbrake" && r.Progress != nil {
		stdout = io.MultiWriter(stdout, handbrake.NewProgressWriter(func(progress handbrake.Progress) {
			r.Progress(jobID, progress)
		}))
	}
	runErr := r.Executor.Run(ctx, invocation, stdout, commandLog.Stderr)
	closeErr := commandLog.Close(runErr)
	return errors.Join(runErr, closeErr)
}

func (r Runner) validate() error {
	if r.Store == nil {
		return fmt.Errorf("queue store is nil")
	}
	if r.Executor == nil {
		return fmt.Errorf("command executor is nil")
	}
	if r.Scanner == nil {
		return fmt.Errorf("media scanner is nil")
	}
	if r.Prober == nil {
		return fmt.Errorf("media prober is nil")
	}
	if !filepath.IsAbs(r.LogDirectory) {
		return fmt.Errorf("log directory must be absolute: %q", r.LogDirectory)
	}
	return nil
}

func validateRuntimePaths(job queue.Job) error {
	if err := job.Validate(); err != nil {
		return err
	}
	info, err := os.Stat(job.Input)
	if err != nil {
		return fmt.Errorf("stat input %s: %w", job.Input, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input is not a regular file: %s", job.Input)
	}
	outputInfo, err := os.Stat(filepath.Dir(job.Output))
	if err != nil {
		return fmt.Errorf("stat output directory: %w", err)
	}
	if !outputInfo.IsDir() {
		return fmt.Errorf("output parent is not a directory: %s", filepath.Dir(job.Output))
	}
	probe, err := os.CreateTemp(filepath.Dir(job.Output), ".chapterbrake-write-test-*")
	if err != nil {
		return fmt.Errorf("output directory is not writable: %w", err)
	}
	probePath := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probePath)
	if closeErr != nil || removeErr != nil {
		return fmt.Errorf("clean output write test: %w", errors.Join(closeErr, removeErr))
	}
	return nil
}

func removePaths(paths ...string) error {
	var errs []error
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open output directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync output directory: %w", err)
	}
	return nil
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r Runner) handBrake() string {
	if r.HandBrake != "" {
		return r.HandBrake
	}
	return "HandBrakeCLI"
}

func (r Runner) ffmpeg() string {
	if r.FFmpeg != "" {
		return r.FFmpeg
	}
	return "ffmpeg"
}

func (r Runner) ffProbe() string {
	if r.FFProbe != "" {
		return r.FFProbe
	}
	return "ffprobe"
}

func (r Runner) mkvPropEdit() string {
	if r.MKVPropEdit != "" {
		return r.MKVPropEdit
	}
	return "mkvpropedit"
}

func (r Runner) logInfo(message string, args ...any) {
	if r.AppLogger != nil {
		r.AppLogger.Info(message, args...)
	}
}

func (r Runner) logError(message string, args ...any) {
	if r.AppLogger != nil {
		r.AppLogger.Error(message, args...)
	}
}

func (r Runner) markRunning(job queue.Job, stage string) error {
	if r.State == nil {
		return nil
	}
	if err := r.State.MarkRunning(job, stage, r.now()); err != nil {
		return fmt.Errorf("record running job state: %w", err)
	}
	return nil
}

func (r Runner) markFailed(job queue.Job, stage, logPath string, runErr error) error {
	if r.State == nil {
		return nil
	}
	if err := r.State.MarkFailed(job, stage, runErr.Error(), logPath, r.now()); err != nil {
		return fmt.Errorf("record failed job state: %w", err)
	}
	return nil
}

func (r Runner) clearState() error {
	if r.State == nil {
		return nil
	}
	if err := r.State.Clear(); err != nil {
		return fmt.Errorf("clear running job state: %w", err)
	}
	return nil
}
