package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"chapterbrake/internal/jsonstore"
)

type Store struct {
	Path string
}

func (s Store) Load() (Queue, error) {
	var q Queue
	if err := jsonstore.Read(s.Path, &q); err != nil {
		return Queue{}, err
	}
	if err := q.Validate(); err != nil {
		return Queue{}, fmt.Errorf("validate queue %s: %w", s.Path, err)
	}
	return q, nil
}

func (s Store) LoadOrCreate() (Queue, error) {
	q, err := s.Load()
	if err == nil {
		return q, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Queue{}, err
	}

	q = Empty()
	if err := s.Save(q); err != nil {
		return Queue{}, err
	}
	return q, nil
}

func (s Store) Save(q Queue) error {
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
