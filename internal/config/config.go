package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"chapterbrake/internal/jsonstore"
	"chapterbrake/internal/media"
)

const (
	Version                = 4
	legacyVersionOne       = 1
	legacyVersionTwo       = 2
	legacyVersionThree     = 3
	DefaultInputDirectory  = "/Volumes/2TB HDD/Images"
	DefaultOutputDirectory = "/Volumes/2TB HDD/Movies"
	DefaultChapterInterval = "23:40"
	legacyOutputDirectory  = "/Volumes/2TB HDD/mp4/"
)

type Settings struct {
	Version         int    `json:"version"`
	InputDirectory  string `json:"input_directory"`
	OutputDirectory string `json:"output_directory"`
	ChapterInterval string `json:"chapter_interval"`
}

func DefaultSettings() Settings {
	return Settings{
		Version:         Version,
		InputDirectory:  DefaultInputDirectory,
		OutputDirectory: DefaultOutputDirectory,
		ChapterInterval: DefaultChapterInterval,
	}
}

func DataDirectory(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("home directory must be absolute: %q", home)
	}
	return filepath.Join(home, "Documents", "ChapterBrake"), nil
}

func LogDirectory(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("home directory must be absolute: %q", home)
	}
	return filepath.Join(home, "Library", "Logs", "ChapterBrake"), nil
}

func (s Settings) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("unsupported settings version %d", s.Version)
	}
	if !filepath.IsAbs(s.InputDirectory) {
		return fmt.Errorf("input directory must be absolute: %q", s.InputDirectory)
	}
	if !filepath.IsAbs(s.OutputDirectory) {
		return fmt.Errorf("output directory must be absolute: %q", s.OutputDirectory)
	}
	if _, err := media.ParseChapterInterval(s.ChapterInterval); err != nil {
		return fmt.Errorf("invalid chapter interval: %w", err)
	}
	return nil
}

func (s Settings) ValidateDirectories() error {
	if err := s.Validate(); err != nil {
		return err
	}

	info, err := os.Stat(s.InputDirectory)
	if err != nil {
		return fmt.Errorf("stat input directory %s: %w", s.InputDirectory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("input path is not a directory: %s", s.InputDirectory)
	}

	info, err = os.Stat(s.OutputDirectory)
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
	settings, err := s.read()
	if err != nil {
		return Settings{}, err
	}
	if err := settings.ValidateDirectories(); err != nil {
		return Settings{}, fmt.Errorf("validate settings %s: %w", s.Path, err)
	}
	return settings, nil
}

func (s Store) read() (Settings, error) {
	var settings Settings
	if err := jsonstore.Read(s.Path, &settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s Store) LoadOrCreate(defaults Settings) (Settings, error) {
	settings, err := s.read()
	if err == nil {
		migrated := false
		switch {
		case settings.Version == legacyVersionOne &&
			settings.InputDirectory == "" &&
			settings.ChapterInterval == "":
			settings.Version = Version
			settings.InputDirectory = defaults.InputDirectory
			settings.ChapterInterval = defaults.ChapterInterval
			migrated = true
		case settings.Version == legacyVersionTwo && settings.ChapterInterval == "":
			settings.Version = Version
			settings.ChapterInterval = defaults.ChapterInterval
			migrated = true
		case settings.Version == legacyVersionThree:
			settings.Version = Version
			if filepath.Clean(settings.OutputDirectory) == filepath.Clean(legacyOutputDirectory) {
				settings.OutputDirectory = defaults.OutputDirectory
			}
			migrated = true
		}
		if migrated {
			if err := settings.ValidateDirectories(); err != nil {
				return Settings{}, fmt.Errorf("validate migrated settings %s: %w", s.Path, err)
			}
			if err := s.Save(settings); err != nil {
				return Settings{}, fmt.Errorf("migrate settings %s: %w", s.Path, err)
			}
			return settings, nil
		}
		if err := settings.ValidateDirectories(); err != nil {
			return Settings{}, fmt.Errorf("validate settings %s: %w", s.Path, err)
		}
		return settings, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Settings{}, err
	}

	if err := defaults.ValidateDirectories(); err != nil {
		return Settings{}, fmt.Errorf("validate default settings: %w", err)
	}
	if err := s.Save(defaults); err != nil {
		return Settings{}, err
	}
	return defaults, nil
}

func (s Store) Save(settings Settings) error {
	if err := settings.ValidateDirectories(); err != nil {
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
