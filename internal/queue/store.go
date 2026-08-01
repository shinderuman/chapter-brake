package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"chapterbrake/internal/jsonstore"
)

type Store struct {
	Path     string
	mu       sync.Mutex
	activeID string
}

func (s *Store) Load() (Queue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (Queue, error) {
	var q Queue
	if err := jsonstore.Read(s.Path, &q); err != nil {
		return Queue{}, err
	}
	if err := q.Validate(); err != nil {
		return Queue{}, fmt.Errorf("validate queue %s: %w", s.Path, err)
	}
	return q, nil
}

func (s *Store) LoadOrCreate() (Queue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, err := s.load()
	if err == nil {
		return q, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Queue{}, err
	}

	q = Empty()
	if err := s.save(q); err != nil {
		return Queue{}, err
	}
	return q, nil
}

func (s *Store) Save(q Queue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(q)
}

func (s *Store) save(q Queue) error {
	if err := q.Validate(); err != nil {
		return fmt.Errorf("validate queue: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create queue directory: %w", err)
	}
	if err := jsonstore.Write(s.Path, q); err != nil {
		return fmt.Errorf("save queue: %w", err)
	}
	return nil
}

func (s *Store) AppendJobs(jobs ...Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, err := s.load()
	if err != nil {
		return err
	}
	next, err := q.Append(jobs...)
	if err != nil {
		return err
	}
	return s.save(next)
}

func (s *Store) DeleteJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeID == id {
		return fmt.Errorf("queue job %q is currently running", id)
	}
	q, err := s.load()
	if err != nil {
		return err
	}
	next, err := q.RemoveJob(id)
	if err != nil {
		return err
	}
	return s.save(next)
}

func (s *Store) MoveJob(id string, delta int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, err := s.load()
	if err != nil {
		return err
	}
	index := -1
	for i, job := range q.Jobs {
		if job.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("queue job %q does not exist", id)
	}
	if s.activeID == id {
		return fmt.Errorf("queue job %q is currently running", id)
	}
	if s.activeID != "" && index+delta == 0 {
		return fmt.Errorf("cannot move a queued job ahead of the running job")
	}
	next, err := q.MoveJob(id, delta)
	if err != nil {
		return err
	}
	return s.save(next)
}

func (s *Store) MoveJobTo(id string, destination int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, err := s.load()
	if err != nil {
		return err
	}
	index := -1
	for i, job := range q.Jobs {
		if job.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("queue job %q does not exist", id)
	}
	if s.activeID == id {
		return fmt.Errorf("queue job %q is currently running", id)
	}
	if s.activeID != "" && destination == 0 {
		return fmt.Errorf("cannot move a queued job ahead of the running job")
	}
	next, err := q.MoveJobTo(id, destination)
	if err != nil {
		return err
	}
	return s.save(next)
}

func (s *Store) ClaimHead() (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeID != "" {
		return Job{}, false, fmt.Errorf("queue job %q is already running", s.activeID)
	}
	q, err := s.load()
	if err != nil {
		return Job{}, false, err
	}
	head, ok := q.Peek()
	if !ok {
		return Job{}, false, nil
	}
	s.activeID = head.ID
	return head, true, nil
}

func (s *Store) ReleaseHead(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeID == "" {
		return nil
	}
	if s.activeID != id {
		return fmt.Errorf("active queue job changed from %q to %q", id, s.activeID)
	}
	s.activeID = ""
	return nil
}

func (s *Store) CompleteHead(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeID != id {
		return fmt.Errorf("queue job %q is not the active job", id)
	}
	q, err := s.load()
	if err != nil {
		return err
	}
	head, ok := q.Peek()
	if !ok {
		return fmt.Errorf("queue is empty while completing job %q", id)
	}
	if head.ID != id {
		return fmt.Errorf("queue head changed from %q to %q", id, head.ID)
	}
	next, err := q.RemoveHead()
	if err != nil {
		return err
	}
	if err := s.save(next); err != nil {
		return err
	}
	s.activeID = ""
	return nil
}
