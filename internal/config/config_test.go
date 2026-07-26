package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDirectory(t *testing.T) {
	got, err := DataDirectory("/Users/example")
	if err != nil {
		t.Fatalf("DataDirectory() error = %v", err)
	}
	want := filepath.Join("/Users/example", "Documents", "ChapterBrake")
	if got != want {
		t.Fatalf("DataDirectory() = %q, want %q", got, want)
	}

	if _, err := DataDirectory("relative"); err == nil {
		t.Fatal("DataDirectory(relative) error = nil")
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		value   Settings
		errText string
	}{
		{"valid", Settings{Version: 1, OutputDirectory: "/output"}, ""},
		{"unknown version", Settings{Version: 2, OutputDirectory: "/output"}, "unsupported"},
		{"relative output", Settings{Version: 1, OutputDirectory: "output"}, "absolute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.value.Validate()
			if tt.errText == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.errText != "" && (err == nil || !strings.Contains(err.Error(), tt.errText)) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}

func TestStoreLoadOrCreateAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "出力")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(root, "data", "settings.json")}
	defaults := Settings{Version: Version, OutputDirectory: output}

	got, err := store.LoadOrCreate(defaults)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	if got != defaults {
		t.Fatalf("LoadOrCreate() = %#v, want %#v", got, defaults)
	}

	got, err = store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != defaults {
		t.Fatalf("Load() = %#v, want %#v", got, defaults)
	}
}

func TestStoreDoesNotOverwriteInvalidJSON(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "output")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "settings.json")
	invalid := []byte("{not-json")
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}

	store := Store{Path: path}
	_, err := store.LoadOrCreate(Settings{Version: Version, OutputDirectory: output})
	if err == nil {
		t.Fatal("LoadOrCreate() error = nil")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(invalid) {
		t.Fatalf("invalid canonical file was changed: %q", got)
	}
}

func TestValidateOutputDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		errText string
	}{
		{"directory", root, ""},
		{"file", file, "not a directory"},
		{"missing", filepath.Join(root, "missing"), "stat output directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := Settings{Version: Version, OutputDirectory: tt.path}
			err := settings.ValidateOutputDirectory()
			if tt.errText == "" && err != nil {
				t.Fatalf("ValidateOutputDirectory() error = %v", err)
			}
			if tt.errText != "" && (err == nil || !strings.Contains(err.Error(), tt.errText)) {
				t.Fatalf("ValidateOutputDirectory() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}
