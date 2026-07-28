package runstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chapterbrake/internal/jsonstore"
	"chapterbrake/internal/queue"
)

const Version = 1

type Status string

const (
	StatusIdle    Status = "idle"
	StatusRunning Status = "running"
	StatusFailed  Status = "failed"
)

type State struct {
	Version   int       `json:"version"`
	Status    Status    `json:"status"`
	JobID     string    `json:"job_id,omitempty"`
	Output    string    `json:"output,omitempty"`
	Stage     string    `json:"stage,omitempty"`
	Message   string    `json:"message,omitempty"`
	LogPath   string    `json:"log_path,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func Idle() State {
	return State{Version: Version, Status: StatusIdle}
}

func (s State) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("unsupported run state version %d", s.Version)
	}
	switch s.Status {
	case StatusIdle:
		if s.JobID != "" || s.Output != "" || s.Stage != "" || s.Message != "" || s.LogPath != "" || !s.UpdatedAt.IsZero() {
			return fmt.Errorf("idle run state must not contain job details")
		}
	case StatusRunning:
		if strings.TrimSpace(s.JobID) == "" || !filepath.IsAbs(s.Output) || strings.TrimSpace(s.Stage) == "" || s.UpdatedAt.IsZero() {
			return fmt.Errorf("running state requires job, output, stage, and updated_at")
		}
		if s.Message != "" || s.LogPath != "" {
			return fmt.Errorf("running state must not contain failure details")
		}
	case StatusFailed:
		if strings.TrimSpace(s.JobID) == "" || !filepath.IsAbs(s.Output) || strings.TrimSpace(s.Stage) == "" ||
			strings.TrimSpace(s.Message) == "" || s.UpdatedAt.IsZero() {
			return fmt.Errorf("failed state requires job, output, stage, message, and updated_at")
		}
		if s.LogPath != "" && !filepath.IsAbs(s.LogPath) {
			return fmt.Errorf("failure log path must be absolute: %q", s.LogPath)
		}
	default:
		return fmt.Errorf("unsupported run status %q", s.Status)
	}
	return nil
}

type Store struct {
	Path string
}

func (s Store) LoadOrCreate() (State, error) {
	state, err := s.Load()
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return State{}, err
	}
	state = Idle()
	if err := s.Save(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s Store) Load() (State, error) {
	var state State
	if err := jsonstore.Read(s.Path, &state); err != nil {
		return State{}, err
	}
	if err := state.Validate(); err != nil {
		return State{}, fmt.Errorf("validate run state %s: %w", s.Path, err)
	}
	return state, nil
}

func (s Store) Save(state State) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate run state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create run state directory: %w", err)
	}
	if err := jsonstore.Write(s.Path, state); err != nil {
		return fmt.Errorf("save run state: %w", err)
	}
	return nil
}

func (s Store) MarkRunning(job queue.Job, stage string, now time.Time) error {
	return s.Save(State{
		Version:   Version,
		Status:    StatusRunning,
		JobID:     job.ID,
		Output:    job.Output,
		Stage:     stage,
		UpdatedAt: now,
	})
}

func (s Store) MarkFailed(job queue.Job, stage, message, logPath string, now time.Time) error {
	return s.Save(State{
		Version:   Version,
		Status:    StatusFailed,
		JobID:     job.ID,
		Output:    job.Output,
		Stage:     stage,
		Message:   message,
		LogPath:   logPath,
		UpdatedAt: now,
	})
}

func (s Store) Clear() error {
	return s.Save(Idle())
}

func (s Store) RecoverInterrupted(now time.Time) (State, error) {
	state, err := s.LoadOrCreate()
	if err != nil {
		return State{}, err
	}
	if state.Status != StatusRunning {
		return state, nil
	}
	state.Status = StatusFailed
	state.Message = "前回のChapterBrakeがジョブ実行中に終了しました"
	state.UpdatedAt = now
	if err := s.Save(state); err != nil {
		return State{}, err
	}
	return state, nil
}
