package control

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"
	"chapterbrake/internal/runstate"
)

type QueueService interface {
	Queue() (queue.Queue, error)
}

type Engine interface {
	Run(context.Context) (runner.Result, error)
	PauseCurrent() error
	ResumeCurrent() error
	CurrentPaused() bool
	Alert() (runstate.State, error)
	DismissAlert(string) error
	SetCallbacks(runner.Callbacks)
}

type Current struct {
	JobID           string  `json:"job_id"`
	Stage           string  `json:"stage"`
	Progress        float64 `json:"progress"`
	ETASeconds      int64   `json:"eta_seconds"`
	DurationSeconds int64   `json:"duration_seconds"`
	EncodingPaused  bool    `json:"encoding_paused"`
	LogPath         string  `json:"log_path,omitempty"`
}

type Failure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	LogPath string `json:"log_path,omitempty"`
}

type Snapshot struct {
	Running           bool           `json:"running"`
	QueuePaused       bool           `json:"queue_paused"`
	PauseAfterCurrent bool           `json:"pause_after_current"`
	Summary           string         `json:"summary,omitempty"`
	Current           *Current       `json:"current,omitempty"`
	QueueETASeconds   int64          `json:"queue_eta_seconds,omitempty"`
	Failure           *Failure       `json:"failure,omitempty"`
	PersistentState   runstate.State `json:"persistent_state"`
}

type Controller struct {
	service QueueService
	engine  Engine
	now     func() time.Time

	mu                sync.Mutex
	running           bool
	cancel            context.CancelFunc
	runDone           chan struct{}
	queuePaused       bool
	pauseAfterCurrent bool
	summary           string
	currentJobID      string
	currentStage      string
	currentProgress   float64
	currentETA        time.Duration
	currentDuration   time.Duration
	currentStartedAt  time.Time
	currentPausedAt   time.Time
	currentPausedFor  time.Duration
	encodingPaused    bool
	speedFactor       float64
	currentLogPath    string
	lastFailure       *Failure
	changed           chan struct{}
}

func New(service QueueService, engine Engine) (*Controller, error) {
	if service == nil {
		return nil, errors.New("queue service is nil")
	}
	if engine == nil {
		return nil, errors.New("queue engine is nil")
	}
	return &Controller{
		service: service,
		engine:  engine,
		now:     time.Now,
		changed: make(chan struct{}),
	}, nil
}

func (controller *Controller) Start() error {
	controller.mu.Lock()
	if controller.running {
		controller.mu.Unlock()
		return nil
	}
	q, err := controller.service.Queue()
	if err != nil {
		controller.mu.Unlock()
		return fmt.Errorf("load queue: %w", err)
	}
	if len(q.Jobs) == 0 {
		controller.mu.Unlock()
		return errors.New("queue is empty")
	}
	ctx, cancel := context.WithCancel(context.Background())
	controller.running = true
	controller.cancel = cancel
	controller.runDone = make(chan struct{})
	controller.queuePaused = false
	controller.pauseAfterCurrent = false
	controller.summary = ""
	controller.currentJobID = q.Jobs[0].ID
	controller.currentStage = "starting"
	controller.currentProgress = 0
	controller.currentETA = 0
	controller.currentDuration = time.Duration(q.Jobs[0].DurationSeconds) * time.Second
	controller.currentStartedAt = time.Time{}
	controller.currentPausedAt = time.Time{}
	controller.currentPausedFor = 0
	controller.encodingPaused = false
	controller.speedFactor = 0
	controller.lastFailure = nil
	controller.engine.SetCallbacks(runner.Callbacks{
		Stage:          controller.onStage,
		Progress:       controller.onProgress,
		Completed:      controller.onCompleted,
		PauseRequested: controller.pauseRequested,
		LogOpened:      controller.onLogOpened,
	})
	done := controller.runDone
	controller.notifyLocked()
	controller.mu.Unlock()

	go controller.run(ctx, done)
	return nil
}

func (controller *Controller) StartAutomatically() error {
	controller.mu.Lock()
	paused := controller.queuePaused
	controller.mu.Unlock()
	if paused {
		return nil
	}
	return controller.Start()
}

func (controller *Controller) PauseEncoding() error {
	controller.mu.Lock()
	if !controller.running || controller.currentStage != "handbrake" {
		controller.mu.Unlock()
		return errors.New("HandBrake encoding is not running")
	}
	if controller.encodingPaused {
		controller.mu.Unlock()
		return nil
	}
	controller.mu.Unlock()
	if err := controller.engine.PauseCurrent(); err != nil {
		return err
	}
	controller.mu.Lock()
	controller.encodingPaused = true
	controller.currentPausedAt = controller.now()
	controller.notifyLocked()
	controller.mu.Unlock()
	return nil
}

func (controller *Controller) ResumeEncoding() error {
	controller.mu.Lock()
	if !controller.running || controller.currentStage != "handbrake" {
		controller.mu.Unlock()
		return errors.New("HandBrake encoding is not running")
	}
	if !controller.encodingPaused {
		controller.mu.Unlock()
		return nil
	}
	controller.mu.Unlock()
	if err := controller.engine.ResumeCurrent(); err != nil {
		return err
	}
	controller.mu.Lock()
	if !controller.currentPausedAt.IsZero() {
		controller.currentPausedFor += controller.now().Sub(controller.currentPausedAt)
	}
	controller.currentPausedAt = time.Time{}
	controller.encodingPaused = false
	controller.notifyLocked()
	controller.mu.Unlock()
	return nil
}

func (controller *Controller) SetPauseAfterCurrent(enabled bool) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if !controller.running {
		return errors.New("queue is not running")
	}
	controller.pauseAfterCurrent = enabled
	controller.notifyLocked()
	return nil
}

func (controller *Controller) Abort(ctx context.Context) error {
	controller.mu.Lock()
	if !controller.running || controller.cancel == nil {
		controller.mu.Unlock()
		return errors.New("queue is not running")
	}
	cancel := controller.cancel
	done := controller.runDone
	controller.mu.Unlock()
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (controller *Controller) ShutdownAfterCurrent(ctx context.Context) error {
	controller.mu.Lock()
	if !controller.running {
		controller.mu.Unlock()
		return nil
	}
	controller.pauseAfterCurrent = true
	paused := controller.encodingPaused
	done := controller.runDone
	controller.notifyLocked()
	controller.mu.Unlock()
	if paused {
		if err := controller.ResumeEncoding(); err != nil {
			return fmt.Errorf("resume encoding for shutdown: %w", err)
		}
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (controller *Controller) DismissAlert(jobID string) error {
	if err := controller.engine.DismissAlert(jobID); err != nil {
		return err
	}
	controller.mu.Lock()
	controller.lastFailure = nil
	controller.notifyLocked()
	controller.mu.Unlock()
	return nil
}

func (controller *Controller) Snapshot() (Snapshot, error) {
	q, err := controller.service.Queue()
	if err != nil {
		return Snapshot{}, err
	}
	persistent, err := controller.engine.Alert()
	if err != nil {
		return Snapshot{}, err
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	snapshot := Snapshot{
		Running:           controller.running,
		QueuePaused:       controller.queuePaused,
		PauseAfterCurrent: controller.pauseAfterCurrent,
		Summary:           controller.summary,
		PersistentState:   persistent,
	}
	if controller.running {
		snapshot.Current = &Current{
			JobID:           controller.currentJobID,
			Stage:           controller.currentStage,
			Progress:        controller.currentProgress,
			ETASeconds:      seconds(controller.currentETA),
			DurationSeconds: seconds(controller.currentDuration),
			EncodingPaused:  controller.encodingPaused,
			LogPath:         controller.currentLogPath,
		}
	}
	if controller.lastFailure != nil {
		failure := *controller.lastFailure
		snapshot.Failure = &failure
	} else if persistent.Status == runstate.StatusFailed {
		snapshot.Failure = &Failure{
			Stage:   persistent.Stage,
			Message: persistent.Message,
			LogPath: persistent.LogPath,
		}
	}
	if eta, ok := controller.queueETALocked(q); ok {
		snapshot.QueueETASeconds = seconds(eta)
	}
	return snapshot, nil
}

func (controller *Controller) Changes() <-chan struct{} {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.changed
}

func (controller *Controller) QueueChanged() {
	controller.mu.Lock()
	controller.notifyLocked()
	controller.mu.Unlock()
}

func (controller *Controller) run(ctx context.Context, done chan struct{}) {
	result, runErr := controller.engine.Run(ctx)
	remaining, queueErr := controller.service.Queue()
	hasRemaining := queueErr == nil && len(remaining.Jobs) > 0
	var jobError *runner.JobError
	canceled := errors.As(runErr, &jobError) && jobError.Canceled

	controller.engine.SetCallbacks(runner.Callbacks{})
	controller.mu.Lock()
	controller.running = false
	controller.cancel = nil
	controller.currentJobID = ""
	controller.currentStage = ""
	controller.currentProgress = 0
	controller.currentETA = 0
	controller.currentDuration = 0
	controller.currentStartedAt = time.Time{}
	controller.currentPausedAt = time.Time{}
	controller.currentPausedFor = 0
	controller.encodingPaused = false
	controller.speedFactor = 0
	controller.currentLogPath = ""
	switch {
	case canceled:
		controller.queuePaused = true
		controller.summary = "即時中断により一時停止"
		controller.lastFailure = nil
	case runErr != nil:
		controller.queuePaused = true
		controller.summary = "失敗により一時停止"
		controller.lastFailure = failureFromError(runErr)
	case result.Paused && hasRemaining:
		controller.queuePaused = true
		controller.summary = fmt.Sprintf("%d件完了後に一時停止", result.Completed)
		controller.lastFailure = nil
	default:
		controller.queuePaused = false
		controller.summary = fmt.Sprintf("%d件完了", result.Completed)
		controller.lastFailure = nil
	}
	controller.pauseAfterCurrent = false
	controller.runDone = nil
	close(done)
	controller.notifyLocked()
	controller.mu.Unlock()
}

func (controller *Controller) onStage(jobID, stage string) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.currentJobID = jobID
	controller.currentStage = stage
	if stage == "handbrake" && controller.currentStartedAt.IsZero() {
		controller.currentStartedAt = controller.now()
		controller.currentPausedAt = time.Time{}
		controller.currentPausedFor = 0
		controller.encodingPaused = false
		if q, err := controller.service.Queue(); err == nil {
			for _, job := range q.Jobs {
				if job.ID == jobID {
					controller.currentDuration = time.Duration(job.DurationSeconds) * time.Second
					break
				}
			}
		}
	} else if stage != "handbrake" {
		controller.currentProgress = 0
		controller.currentETA = 0
		controller.currentStartedAt = time.Time{}
		controller.currentPausedAt = time.Time{}
		controller.currentPausedFor = 0
		controller.encodingPaused = false
	}
	controller.notifyLocked()
}

func (controller *Controller) onProgress(jobID string, progress handbrake.Progress) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.currentJobID = jobID
	controller.currentStage = "handbrake"
	controller.currentProgress = progress.Fraction
	controller.currentETA = time.Duration(progress.ETASeconds) * time.Second
	activeElapsed := controller.now().Sub(controller.currentStartedAt) - controller.currentPausedFor
	if !controller.currentPausedAt.IsZero() {
		activeElapsed -= controller.now().Sub(controller.currentPausedAt)
	}
	if progress.Fraction > 0 && controller.currentDuration > 0 && activeElapsed > 0 {
		controller.speedFactor = float64(activeElapsed) / progress.Fraction / float64(controller.currentDuration)
	}
	controller.notifyLocked()
}

func (controller *Controller) onCompleted(string) {
	controller.mu.Lock()
	controller.notifyLocked()
	controller.mu.Unlock()
}

func (controller *Controller) onLogOpened(jobID, path string) {
	controller.mu.Lock()
	controller.currentJobID = jobID
	controller.currentLogPath = path
	controller.notifyLocked()
	controller.mu.Unlock()
}

func (controller *Controller) pauseRequested() bool {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.pauseAfterCurrent
}

func (controller *Controller) queueETALocked(q queue.Queue) (time.Duration, bool) {
	if !controller.running || controller.speedFactor <= 0 {
		return 0, false
	}
	current := -1
	for index, job := range q.Jobs {
		if job.ID == controller.currentJobID {
			current = index
			break
		}
	}
	if current < 0 {
		return 0, false
	}
	total := controller.currentETA
	currentDuration := q.Jobs[current].DurationSeconds
	if controller.currentStage != "handbrake" && currentDuration > 0 {
		total += time.Duration(float64(time.Duration(currentDuration)*time.Second) * controller.speedFactor)
	}
	for _, job := range q.Jobs[current+1:] {
		duration := job.DurationSeconds
		if duration <= 0 {
			duration = currentDuration
		}
		if duration > 0 {
			total += time.Duration(float64(time.Duration(duration)*time.Second) * controller.speedFactor)
		}
	}
	return total, total >= 0
}

func (controller *Controller) notifyLocked() {
	close(controller.changed)
	controller.changed = make(chan struct{})
}

func failureFromError(err error) *Failure {
	var jobError *runner.JobError
	if errors.As(err, &jobError) {
		return &Failure{Stage: jobError.Stage, Message: jobError.Error(), LogPath: jobError.LogPath}
	}
	return &Failure{Stage: "queue-run", Message: err.Error()}
}

func seconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return int64(duration.Round(time.Second) / time.Second)
}
