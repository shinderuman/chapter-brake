package runner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"chapterbrake/internal/media"
	"chapterbrake/internal/metadata"
	"chapterbrake/internal/process"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runstate"
)

type memoryStore struct {
	q       queue.Queue
	saves   []queue.Queue
	saveErr error
	active  string
}

func (s *memoryStore) ClaimHead() (queue.Job, bool, error) {
	if s.active != "" {
		return queue.Job{}, false, errors.New("job already active")
	}
	head, ok := s.q.Peek()
	if ok {
		s.active = head.ID
	}
	return head, ok, nil
}

func (s *memoryStore) ReleaseHead(id string) error {
	if s.active != id {
		return errors.New("active job changed")
	}
	s.active = ""
	return nil
}

func (s *memoryStore) CompleteHead(id string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	head, ok := s.q.Peek()
	if !ok || head.ID != id {
		return errors.New("queue head changed")
	}
	if s.active != id {
		return errors.New("job is not active")
	}
	next, err := s.q.RemoveHead()
	if err != nil {
		return err
	}
	s.q = next
	s.active = ""
	s.saves = append(s.saves, next)
	return nil
}

func (s *memoryStore) AppendJobs(jobs ...queue.Job) error {
	next, err := s.q.Append(jobs...)
	if err == nil {
		s.q = next
	}
	return err
}

type fakeScanner struct {
	info  media.Info
	err   error
	calls []string
}

func (s *fakeScanner) Scan(
	_ context.Context,
	input string,
	stdout io.Writer,
	stderr io.Writer,
) (media.Info, error) {
	s.calls = append(s.calls, input)
	_, _ = io.WriteString(stdout, "scan stdout\n")
	_, _ = io.WriteString(stderr, "scan stderr\n")
	return s.info, s.err
}

type fakeProber struct {
	title     string
	container queue.Container
	errAtCall int
	calls     []string
}

func (p *fakeProber) Probe(
	_ context.Context,
	path string,
	stdout io.Writer,
	stderr io.Writer,
) (metadata.Probe, error) {
	p.calls = append(p.calls, path)
	_, _ = io.WriteString(stdout, "{}\n")
	_, _ = io.WriteString(stderr, "probe diagnostic\n")
	if p.errAtCall == len(p.calls) {
		return metadata.Probe{}, errors.New("injected probe failure")
	}

	format := "matroska,webm"
	majorBrand := ""
	if p.container == queue.ContainerMP4 {
		format = "mov,mp4,m4a,3gp,3g2,mj2"
		majorBrand = "mp42"
	}
	title := "source title"
	if len(p.calls)%2 == 0 {
		title = p.title
	}
	return metadata.Probe{
		Streams: []metadata.ProbeStream{
			{Index: 0, CodecName: "h264", CodecType: "video", TimeBase: "1/1000", StartTime: "0", Duration: "6", Width: 320, Height: 180},
			{Index: 1, CodecName: "ac3", CodecType: "audio", TimeBase: "1/48000", StartTime: "0", Duration: "6", SampleRate: "48000", Channels: 6, Tags: map[string]string{"language": "jpn"}},
		},
		Chapters: []metadata.ProbeChapter{
			{ID: 0, TimeBase: "1/1000", Start: 0, End: 3000, StartTime: "0", EndTime: "3", Tags: map[string]string{"title": "One"}},
		},
		Format: metadata.ProbeFormat{
			FormatName: format,
			StartTime:  "0",
			Duration:   "6",
			Tags: map[string]string{
				"title":       title,
				"major_brand": majorBrand,
			},
		},
	}, nil
}

type fakeExecutor struct {
	store              *memoryStore
	invocations        []process.Invocation
	failExecutable     string
	cancelExecutable   string
	expectedAbsentPath string
	headObserved       bool
}

type pausableFakeExecutor struct {
	fakeExecutor
	paused bool
}

func (executor *pausableFakeExecutor) Pause() error {
	executor.paused = true
	return nil
}

func (executor *pausableFakeExecutor) Resume() error {
	executor.paused = false
	return nil
}

func (executor *pausableFakeExecutor) IsPaused() bool {
	return executor.paused
}

func (e *fakeExecutor) Run(
	_ context.Context,
	invocation process.Invocation,
	stdout io.Writer,
	stderr io.Writer,
) error {
	e.invocations = append(e.invocations, invocation)
	_, _ = io.WriteString(stdout, "command stdout\n")
	_, _ = io.WriteString(stderr, "command stderr\n")
	if e.store != nil && len(e.store.q.Jobs) > 0 {
		e.headObserved = true
	}
	if e.expectedAbsentPath != "" {
		if _, err := os.Stat(e.expectedAbsentPath); !errors.Is(err, os.ErrNotExist) {
			return errors.New("existing final output was not deleted before command")
		}
		e.expectedAbsentPath = ""
	}

	switch filepath.Base(invocation.Executable) {
	case e.failExecutable:
		return errors.New("injected command failure")
	case e.cancelExecutable:
		if output := argumentAfter(invocation.Args, "-o"); output != "" {
			_ = os.WriteFile(output, []byte("partial"), 0o600)
		}
		return context.Canceled
	}

	switch filepath.Base(invocation.Executable) {
	case "HandBrakeCLI":
		output := argumentAfter(invocation.Args, "-o")
		if output == "" {
			return errors.New("HandBrake output argument missing")
		}
		return os.WriteFile(output, []byte("encoded"), 0o600)
	case "ffmpeg":
		return os.WriteFile(invocation.Args[len(invocation.Args)-1], []byte("metadata"), 0o600)
	case "mkvpropedit":
		return nil
	default:
		return errors.New("unexpected executable")
	}
}

func argumentAfter(args []string, key string) string {
	for i, arg := range args {
		if arg == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func testJob(t *testing.T, container queue.Container) queue.Job {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "入力 source.mkv")
	if err := os.WriteFile(input, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "日本語 Title #01."+string(container))
	subtitles := []int{1}
	preset := "1080p MKV"
	if container == queue.ContainerMP4 {
		subtitles = []int{}
		preset = "1080p MP4"
	}
	return queue.Job{
		ID:           "job-1",
		CreatedAt:    time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
		Input:        input,
		Output:       output,
		Preset:       preset,
		Container:    container,
		ChapterStart: 1,
		ChapterEnd:   2,
		AudioTracks:  []int{1},
		Subtitles:    subtitles,
	}
}

func newTestRunner(t *testing.T, job queue.Job) (Runner, *memoryStore, *fakeExecutor, *fakeProber) {
	t.Helper()
	store := &memoryStore{q: queue.Queue{Version: queue.Version, Jobs: []queue.Job{job}}}
	executor := &fakeExecutor{store: store}
	prober := &fakeProber{
		title:     strings.TrimSuffix(filepath.Base(job.Output), filepath.Ext(job.Output)),
		container: job.Container,
	}
	scanner := &fakeScanner{info: media.Info{
		Duration: 6 * time.Second,
		Chapters: []media.Chapter{
			{Number: 1, Start: 0},
			{Number: 2, Start: 3 * time.Second},
		},
		AudioTracks: []media.AudioTrack{{Number: 1, Codec: "ac3", Channels: 6}},
	}}
	runner := Runner{
		Store:        store,
		Executor:     executor,
		Scanner:      scanner,
		Prober:       prober,
		LogDirectory: filepath.Join(filepath.Dir(job.Output), "logs"),
		AppLogger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		HandBrake:    "/tools/HandBrakeCLI",
		FFmpeg:       "/tools/ffmpeg",
		FFProbe:      "/tools/ffprobe",
		MKVPropEdit:  "/tools/mkvpropedit",
		Now: func() time.Time {
			return time.Date(2026, 7, 26, 9, 0, 0, 1, time.UTC)
		},
	}
	return runner, store, executor, prober
}

func TestRunnerMKVSuccessRemovesHeadOnlyAfterPublish(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	runner, store, executor, _ := newTestRunner(t, job)
	stateStore := &runstate.Store{Path: filepath.Join(t.TempDir(), "state.json")}
	if _, err := stateStore.LoadOrCreate(); err != nil {
		t.Fatal(err)
	}
	runner.State = stateStore
	var openedJobID string
	var openedLogPath string
	runner.SetCallbacks(Callbacks{
		LogOpened: func(jobID, logPath string) {
			openedJobID = jobID
			openedLogPath = logPath
		},
	})
	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Completed != 1 || len(store.q.Jobs) != 0 {
		t.Fatalf("result = %#v, queue = %#v", result, store.q)
	}
	if !executor.headObserved {
		t.Fatal("queue head was not present while commands ran")
	}
	if openedJobID != job.ID || !filepath.IsAbs(openedLogPath) {
		t.Fatalf("opened job log = %q, %q", openedJobID, openedLogPath)
	}
	content, err := os.ReadFile(job.Output)
	if err != nil {
		t.Fatalf("read final output: %v", err)
	}
	if string(content) != "encoded" {
		t.Fatalf("final content = %q", content)
	}
	paths, _ := metadata.TemporaryPaths(job)
	for _, path := range []string{paths.Encode, paths.Metadata} {
		if path != "" {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("temporary path remains: %s", path)
			}
		}
	}
	if len(executor.invocations) != 2 ||
		filepath.Base(executor.invocations[0].Executable) != "HandBrakeCLI" ||
		filepath.Base(executor.invocations[1].Executable) != "mkvpropedit" {
		t.Fatalf("invocations = %#v", executor.invocations)
	}
	state, err := stateStore.Load()
	if err != nil || state.Status != runstate.StatusIdle {
		t.Fatalf("successful run state = %#v, %v", state, err)
	}
}

func TestRunnerCreatesMissingJobOutputDirectoryAtExecution(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	job.Output = filepath.Join(filepath.Dir(job.Output), "new-title", "日本語 Title #01.mkv")
	runner, store, _, _ := newTestRunner(t, job)
	if _, err := os.Stat(filepath.Dir(job.Output)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output directory exists before Run(): %v", err)
	}
	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Completed != 1 || len(store.q.Jobs) != 0 {
		t.Fatalf("result = %#v, queue = %#v", result, store.q)
	}
	if _, err := os.Stat(job.Output); err != nil {
		t.Fatalf("final output was not published: %v", err)
	}
}

func TestRunnerPausesAfterCurrentJob(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	second := job
	second.ID = "job-2"
	second.Output = filepath.Join(filepath.Dir(job.Output), "second_02.mkv")
	runner, store, _, _ := newTestRunner(t, job)
	store.q.Jobs = append(store.q.Jobs, second)
	runner.PauseRequested = func() bool { return true }

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Completed != 1 || !result.Paused {
		t.Fatalf("result = %#v", result)
	}
	if len(store.q.Jobs) != 1 || store.q.Jobs[0].ID != second.ID {
		t.Fatalf("queue after pause = %#v", store.q)
	}
}

func TestRunnerMP4SuccessUsesStreamCopy(t *testing.T) {
	job := testJob(t, queue.ContainerMP4)
	runner, store, executor, _ := newTestRunner(t, job)
	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Completed != 1 || len(store.q.Jobs) != 0 {
		t.Fatalf("result = %#v, queue jobs = %d", result, len(store.q.Jobs))
	}
	content, err := os.ReadFile(job.Output)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "metadata" {
		t.Fatalf("final content = %q", content)
	}
	if len(executor.invocations) != 2 || filepath.Base(executor.invocations[1].Executable) != "ffmpeg" {
		t.Fatalf("invocations = %#v", executor.invocations)
	}
	ffmpegArgs := executor.invocations[1].Args
	if !containsPair(ffmpegArgs, "-c", "copy") || containsPair(ffmpegArgs, "-map", "0") {
		t.Fatalf("ffmpeg args = %q", ffmpegArgs)
	}
}

func TestRunnerFailureKeepsHeadStopsAndCleansOutputs(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	second := job
	second.ID = "job-2"
	second.Output = filepath.Join(filepath.Dir(job.Output), "second_02.mkv")
	runner, store, executor, _ := newTestRunner(t, job)
	stateStore := &runstate.Store{Path: filepath.Join(t.TempDir(), "state.json")}
	if _, err := stateStore.LoadOrCreate(); err != nil {
		t.Fatal(err)
	}
	runner.State = stateStore
	store.q.Jobs = append(store.q.Jobs, second)
	executor.failExecutable = "HandBrakeCLI"
	if err := os.WriteFile(job.Output, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor.expectedAbsentPath = job.Output

	result, err := runner.Run(context.Background())
	var jobErr *JobError
	if !errors.As(err, &jobErr) || jobErr.Stage != "handbrake" {
		t.Fatalf("Run() error = %T %v", err, err)
	}
	if result.Completed != 0 || len(store.q.Jobs) != 2 || store.q.Jobs[0].ID != job.ID {
		t.Fatalf("result = %#v, queue = %#v", result, store.q)
	}
	if len(executor.invocations) != 1 {
		t.Fatalf("later command ran: %#v", executor.invocations)
	}
	paths, _ := metadata.TemporaryPaths(job)
	for _, path := range []string{job.Output, paths.Encode, paths.Metadata} {
		if path != "" {
			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failed output remains: %s", path)
			}
		}
	}
	if jobErr.LogPath == "" {
		t.Fatal("JobError has no log path")
	}
	state, stateErr := stateStore.Load()
	if stateErr != nil || state.Status != runstate.StatusFailed ||
		state.JobID != job.ID || state.Stage != "handbrake" || state.LogPath != jobErr.LogPath {
		t.Fatalf("failed run state = %#v, %v", state, stateErr)
	}
	if err := runner.DismissAlert("another-job"); err != nil {
		t.Fatalf("DismissAlert(other) error = %v", err)
	}
	state, stateErr = stateStore.Load()
	if stateErr != nil || state.Status != runstate.StatusFailed {
		t.Fatalf("unrelated alert was dismissed: %#v, %v", state, stateErr)
	}
	if err := runner.DismissAlert(job.ID); err != nil {
		t.Fatalf("DismissAlert(job) error = %v", err)
	}
	state, stateErr = stateStore.Load()
	if stateErr != nil || state.Status != runstate.StatusIdle {
		t.Fatalf("matching alert remains: %#v, %v", state, stateErr)
	}
}

func TestRunnerCancellationKeepsHeadAndDeletesPartial(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	runner, store, executor, _ := newTestRunner(t, job)
	stateStore := &runstate.Store{Path: filepath.Join(t.TempDir(), "state.json")}
	if _, err := stateStore.LoadOrCreate(); err != nil {
		t.Fatal(err)
	}
	runner.State = stateStore
	executor.cancelExecutable = "HandBrakeCLI"

	result, err := runner.Run(context.Background())
	var jobErr *JobError
	if !errors.As(err, &jobErr) || !jobErr.Canceled {
		t.Fatalf("Run() error = %T %v", err, err)
	}
	if result.Completed != 0 || len(store.q.Jobs) != 1 {
		t.Fatalf("result = %#v, queue = %#v", result, store.q)
	}
	paths, _ := metadata.TemporaryPaths(job)
	if _, statErr := os.Stat(paths.Encode); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial encode remains: %v", statErr)
	}
	state, stateErr := stateStore.Load()
	if stateErr != nil || state.Status != runstate.StatusIdle {
		t.Fatalf("canceled run state = %#v, %v", state, stateErr)
	}
}

func TestRunnerVerificationFailureKeepsHead(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	runner, store, _, prober := newTestRunner(t, job)
	prober.title = "wrong title"

	_, err := runner.Run(context.Background())
	var jobErr *JobError
	if !errors.As(err, &jobErr) || jobErr.Stage != "verify" {
		t.Fatalf("Run() error = %T %v", err, err)
	}
	if len(store.q.Jobs) != 1 {
		t.Fatalf("queue jobs = %d", len(store.q.Jobs))
	}
	if _, statErr := os.Stat(job.Output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("final output exists after verification failure")
	}
}

func TestRunnerQueueSaveFailureLeavesPublishedFileAndHead(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	runner, store, _, _ := newTestRunner(t, job)
	saveErr := errors.New("save failed")
	store.saveErr = saveErr

	result, err := runner.Run(context.Background())
	var jobErr *JobError
	if !errors.As(err, &jobErr) || jobErr.Stage != "queue-save" || !errors.Is(err, saveErr) {
		t.Fatalf("Run() error = %T %v", err, err)
	}
	if result.Completed != 0 || len(store.q.Jobs) != 1 {
		t.Fatalf("result = %#v, queue = %#v", result, store.q)
	}
	if store.active != "" {
		t.Fatalf("active job remains after save failure: %q", store.active)
	}
	if _, statErr := os.Stat(job.Output); statErr != nil {
		t.Fatalf("published output missing after queue save failure: %v", statErr)
	}
}

func TestRunnerEmptyQueueAndValidation(t *testing.T) {
	store := &memoryStore{q: queue.Empty()}
	runner := Runner{
		Store:        store,
		Executor:     &fakeExecutor{},
		Scanner:      &fakeScanner{},
		Prober:       &fakeProber{},
		LogDirectory: t.TempDir(),
	}
	result, err := runner.Run(context.Background())
	if err != nil || result.Completed != 0 {
		t.Fatalf("Run(empty) = %#v, %v", result, err)
	}

	invalid := runner
	invalid.Store = nil
	if _, err := invalid.Run(context.Background()); err == nil {
		t.Fatal("Run(invalid dependencies) error = nil")
	}
}

func TestRunnerPauseResumeAndAlertBoundaries(t *testing.T) {
	executor := &pausableFakeExecutor{}
	run := Runner{Executor: executor}
	if err := run.PauseCurrent(); err != nil {
		t.Fatal(err)
	}
	if !run.CurrentPaused() {
		t.Fatal("CurrentPaused() = false after pause")
	}
	if err := run.ResumeCurrent(); err != nil {
		t.Fatal(err)
	}
	if run.CurrentPaused() {
		t.Fatal("CurrentPaused() = true after resume")
	}
	idle, err := run.Alert()
	if err != nil || idle.Status != runstate.StatusIdle {
		t.Fatalf("nil state alert = %#v, %v", idle, err)
	}

	stateStore := &runstate.Store{Path: filepath.Join(t.TempDir(), "state.json")}
	failed := runstate.State{
		Version: runstate.Version, Status: runstate.StatusFailed, JobID: "job-1",
		Output: "/tmp/output.mkv", Stage: "handbrake", Message: "failed", UpdatedAt: time.Now(),
	}
	if err := stateStore.Save(failed); err != nil {
		t.Fatal(err)
	}
	run.State = stateStore
	alert, err := run.Alert()
	if err != nil || alert.Status != runstate.StatusFailed {
		t.Fatalf("failed alert = %#v, %v", alert, err)
	}
	jobError := &JobError{JobID: "job-1", Stage: "handbrake", Canceled: true, Err: context.Canceled}
	if !strings.Contains(jobError.Error(), "canceled") {
		t.Fatalf("job error = %q", jobError.Error())
	}
}

func TestRemovePaths(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removePaths(first, "", second, filepath.Join(root, "missing")); err != nil {
		t.Fatalf("removePaths() error = %v", err)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("path remains: %s", path)
		}
	}
}

func containsPair(values []string, first, second string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == first && values[i+1] == second {
			return true
		}
	}
	return false
}

func TestRunnerInvocationOrder(t *testing.T) {
	job := testJob(t, queue.ContainerMKV)
	runner, _, executor, _ := newTestRunner(t, job)
	var stages []string
	runner.Stage = func(jobID, stage string) {
		if jobID != job.ID {
			t.Errorf("stage job ID = %q, want %q", jobID, job.ID)
		}
		stages = append(stages, stage)
	}
	_, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(executor.invocations))
	for i, invocation := range executor.invocations {
		got[i] = filepath.Base(invocation.Executable)
	}
	want := []string{"HandBrakeCLI", "mkvpropedit"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command order = %v, want %v", got, want)
	}
	if len(stages) == 0 || stages[0] != "validate" || stages[len(stages)-1] != "publish" {
		t.Fatalf("stages = %v", stages)
	}
}

func TestRunnerProcessesJobAppendedDuringExecution(t *testing.T) {
	first := testJob(t, queue.ContainerMKV)
	runner, store, _, _ := newTestRunner(t, first)
	second := first
	second.ID = "job-2"
	secondDirectory := filepath.Join(filepath.Dir(first.Output), "second")
	if err := os.Mkdir(secondDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	second.Output = filepath.Join(secondDirectory, filepath.Base(first.Output))

	appended := false
	runner.Stage = func(jobID, stage string) {
		if jobID == first.ID && stage == "publish" && !appended {
			appended = true
			if err := store.AppendJobs(second); err != nil {
				t.Errorf("AppendJobs() error = %v", err)
			}
		}
	}
	var completed []string
	runner.Completed = func(id string) {
		completed = append(completed, id)
	}

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Completed != 2 || len(store.q.Jobs) != 0 {
		t.Fatalf("result = %#v, queue = %#v", result, store.q)
	}
	if !reflect.DeepEqual(completed, []string{first.ID, second.ID}) {
		t.Fatalf("completed = %v", completed)
	}
}
