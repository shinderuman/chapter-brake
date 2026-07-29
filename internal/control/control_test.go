package control

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"
	"chapterbrake/internal/runstate"
)

type fakeQueueService struct {
	queue queue.Queue
	err   error
}

func (service *fakeQueueService) Queue() (queue.Queue, error) {
	return service.queue, service.err
}

type fakeEngine struct {
	mu        sync.Mutex
	callbacks runner.Callbacks
	run       func(context.Context, runner.Callbacks) (runner.Result, error)
	paused    bool
	alert     runstate.State
	dismissed string
}

func (engine *fakeEngine) Run(ctx context.Context) (runner.Result, error) {
	engine.mu.Lock()
	callbacks := engine.callbacks
	run := engine.run
	engine.mu.Unlock()
	return run(ctx, callbacks)
}

func (engine *fakeEngine) PauseCurrent() error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.paused = true
	return nil
}

func (engine *fakeEngine) ResumeCurrent() error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.paused = false
	return nil
}

func (engine *fakeEngine) CurrentPaused() bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.paused
}

func (engine *fakeEngine) Alert() (runstate.State, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.alert, nil
}

func (engine *fakeEngine) DismissAlert(jobID string) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.dismissed = jobID
	engine.alert = runstate.Idle()
	return nil
}

func (engine *fakeEngine) SetCallbacks(callbacks runner.Callbacks) {
	engine.mu.Lock()
	engine.callbacks = callbacks
	engine.mu.Unlock()
}

func TestControllerRunPauseResumeAndPauseAfterCurrent(t *testing.T) {
	service := &fakeQueueService{queue: testQueue(t)}
	release := make(chan struct{})
	engine := &fakeEngine{alert: runstate.Idle()}
	engine.run = func(_ context.Context, callbacks runner.Callbacks) (runner.Result, error) {
		callbacks.Stage(service.queue.Jobs[0].ID, "handbrake")
		callbacks.Progress(service.queue.Jobs[0].ID, handbrake.Progress{Fraction: 0.5, ETASeconds: 12})
		<-release
		if !callbacks.PauseRequested() {
			return runner.Result{}, errors.New("pause was not requested")
		}
		return runner.Result{Completed: 1, Paused: true}, nil
	}
	controller, err := New(service, engine)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.Local)
	controller.now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		snapshot, _ := controller.Snapshot()
		return snapshot.Current != nil && snapshot.Current.Progress == 0.5
	})
	if err := controller.PauseEncoding(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := controller.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Current.EncodingPaused {
		t.Fatal("encoding_paused = false")
	}
	if err := controller.ResumeEncoding(); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetPauseAfterCurrent(true); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitFor(t, func() bool {
		snapshot, _ := controller.Snapshot()
		return !snapshot.Running
	})
	snapshot, _ = controller.Snapshot()
	if !snapshot.QueuePaused || snapshot.Summary != "1件完了後に一時停止" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestControllerAbortKeepsQueuePaused(t *testing.T) {
	service := &fakeQueueService{queue: testQueue(t)}
	started := make(chan struct{})
	engine := &fakeEngine{alert: runstate.Idle()}
	engine.run = func(ctx context.Context, callbacks runner.Callbacks) (runner.Result, error) {
		callbacks.Stage(service.queue.Jobs[0].ID, "handbrake")
		close(started)
		<-ctx.Done()
		return runner.Result{}, &runner.JobError{
			JobID:    service.queue.Jobs[0].ID,
			Stage:    "handbrake",
			Canceled: true,
			Err:      ctx.Err(),
		}
	}
	controller, err := New(service, engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := controller.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Running || !snapshot.QueuePaused || snapshot.Failure != nil {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := controller.StartAutomatically(); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = controller.Snapshot()
	if snapshot.Running {
		t.Fatal("automatic start resumed a paused queue")
	}
	engine.run = func(context.Context, runner.Callbacks) (runner.Result, error) {
		return runner.Result{Completed: 2}, nil
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		snapshot, _ := controller.Snapshot()
		return !snapshot.Running && !snapshot.QueuePaused
	})
}

func TestControllerFailureIsStructuredAndPausesQueue(t *testing.T) {
	service := &fakeQueueService{queue: testQueue(t)}
	engine := &fakeEngine{alert: runstate.Idle()}
	engine.run = func(context.Context, runner.Callbacks) (runner.Result, error) {
		return runner.Result{}, &runner.JobError{
			JobID: "job-1", Stage: "ffprobe-after", LogPath: "/logs/job.log", Err: errors.New("probe failed"),
		}
	}
	controller, err := New(service, engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		snapshot, _ := controller.Snapshot()
		return !snapshot.Running
	})
	snapshot, err := controller.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.QueuePaused || snapshot.Failure == nil ||
		snapshot.Failure.Stage != "ffprobe-after" || snapshot.Failure.LogPath != "/logs/job.log" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestControllerShutdownResumesPausedEncoding(t *testing.T) {
	service := &fakeQueueService{queue: testQueue(t)}
	engine := &fakeEngine{alert: runstate.Idle()}
	resumed := make(chan struct{})
	engine.run = func(_ context.Context, callbacks runner.Callbacks) (runner.Result, error) {
		callbacks.Stage(service.queue.Jobs[0].ID, "handbrake")
		for {
			engine.mu.Lock()
			paused := engine.paused
			engine.mu.Unlock()
			if !paused && callbacks.PauseRequested() {
				close(resumed)
				return runner.Result{Completed: 1, Paused: true}, nil
			}
			time.Sleep(time.Millisecond)
		}
	}
	controller, err := New(service, engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		snapshot, _ := controller.Snapshot()
		return snapshot.Current != nil && snapshot.Current.Stage == "handbrake"
	})
	if err := controller.PauseEncoding(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := controller.ShutdownAfterCurrent(ctx); err != nil {
		t.Fatal(err)
	}
	<-resumed
}

func TestControllerValidationAndDismissAlert(t *testing.T) {
	if _, err := New(nil, &fakeEngine{}); err == nil {
		t.Fatal("nil service accepted")
	}
	if _, err := New(&fakeQueueService{}, nil); err == nil {
		t.Fatal("nil engine accepted")
	}
	service := &fakeQueueService{queue: queue.Empty()}
	engine := &fakeEngine{alert: runstate.State{
		Version: 1, Status: runstate.StatusFailed, JobID: "job", Output: "/out.mkv",
		Stage: "handbrake", Message: "failed", UpdatedAt: time.Now(),
	}}
	engine.run = func(context.Context, runner.Callbacks) (runner.Result, error) {
		return runner.Result{}, nil
	}
	controller, err := New(service, engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(); err == nil {
		t.Fatal("empty queue started")
	}
	if err := controller.DismissAlert("job"); err != nil {
		t.Fatal(err)
	}
	if engine.dismissed != "job" {
		t.Fatalf("dismissed = %q", engine.dismissed)
	}
}

func TestControllerChangeNotificationsAndLogPath(t *testing.T) {
	service := &fakeQueueService{queue: testQueue(t)}
	release := make(chan struct{})
	engine := &fakeEngine{alert: runstate.Idle()}
	engine.run = func(_ context.Context, callbacks runner.Callbacks) (runner.Result, error) {
		callbacks.Stage("job-1", "handbrake")
		callbacks.LogOpened("job-1", "/logs/job-1.log")
		<-release
		callbacks.Completed("job-1")
		return runner.Result{Completed: 2}, nil
	}
	controller, err := New(service, engine)
	if err != nil {
		t.Fatal(err)
	}
	changed := controller.Changes()
	controller.QueueChanged()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("queue change notification was not delivered")
	}
	if err := controller.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		snapshot, _ := controller.Snapshot()
		return snapshot.Current != nil && snapshot.Current.LogPath == "/logs/job-1.log"
	})
	close(release)
	waitFor(t, func() bool {
		snapshot, _ := controller.Snapshot()
		return !snapshot.Running
	})
}

func testQueue(t *testing.T) queue.Queue {
	t.Helper()
	root := t.TempDir()
	now := time.Now()
	q := queue.Queue{
		Version: queue.Version,
		Jobs: []queue.Job{
			{
				ID: "job-1", CreatedAt: now, Input: filepath.Join(root, "input.mkv"),
				Output: filepath.Join(root, "one.mkv"), Preset: "Preset", Container: queue.ContainerMKV,
				ChapterStart: 1, ChapterEnd: 2, DurationSeconds: 1200, AudioTracks: []int{1}, Subtitles: []int{},
			},
			{
				ID: "job-2", CreatedAt: now, Input: filepath.Join(root, "input.mkv"),
				Output: filepath.Join(root, "two.mkv"), Preset: "Preset", Container: queue.ContainerMKV,
				ChapterStart: 3, ChapterEnd: 4, DurationSeconds: 1200, AudioTracks: []int{1}, Subtitles: []int{},
			},
		},
	}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	return q
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not satisfied")
}
