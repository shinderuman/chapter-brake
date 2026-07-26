package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"chapterbrake/internal/jsonstore"
)

const (
	Version                = 1
	DefaultOutputDirectory = "/Volumes/2TB HDD/mp4/"
)

type Settings struct {
	Version         int    `json:"version"`
	OutputDirectory string `json:"output_directory"`
}

func DefaultSettings() Settings {
	return Settings{
		Version:         Version,
		OutputDirectory: DefaultOutputDirectory,
	}
}

func DataDirectory(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("home directory must be absolute: %q", home)
	}
	return filepath.Join(home, "Documents", "ChapterBrake"), nil
}

func (s Settings) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("unsupported settings version %d", s.Version)
	}
	if !filepath.IsAbs(s.OutputDirectory) {
		return fmt.Errorf("output directory must be absolute: %q", s.OutputDirectory)
	}
	return nil
}

func (s Settings) ValidateOutputDirectory() error {
	if err := s.Validate(); err != nil {
		return err
	}

	info, err := os.Stat(s.OutputDirectory)
	if err != nil {
		return fmt.Errorf("stat output directory %s: %w", s.OutputDirectory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output path is not a directory: %s", s.OutputDirectory)
	}

	probe, err := os.CreateTemp(s.OutputDirectory, ".chapterbrake-write-test-*")
	if err != nil {
		return fmt.Errorf("output directory is not writable %s: %w", s.OutputDirectory, err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("close output directory write test %s: %w", probePath, err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("remove output directory write test %s: %w", probePath, err)
	}
	return nil
}

type Store struct {
	Path string
}

func (s Store) Load() (Settings, error) {
	var settings Settings
	if err := jsonstore.Read(s.Path, &settings); err != nil {
		return Settings{}, err
	}
	if err := settings.ValidateOutputDirectory(); err != nil {
		return Settings{}, fmt.Errorf("validate settings %s: %w", s.Path, err)
	}
	return settings, nil
}

func (s Store) LoadOrCreate(defaults Settings) (Settings, error) {
	settings, err := s.Load()
	if err == nil {
		return settings, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Settings{}, err
	}

	if err := defaults.ValidateOutputDirectory(); err != nil {
		return Settings{}, fmt.Errorf("validate default settings: %w", err)
	}
	if err := s.Save(defaults); err != nil {
		return Settings{}, err
	}
	return defaults, nil
}

func (s Store) Save(settings Settings) error {
	if err := settings.ValidateOutputDirectory(); err != nil {
		return fmt.Errorf("validate settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	if err := jsonstore.Write(s.Path, settings); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
