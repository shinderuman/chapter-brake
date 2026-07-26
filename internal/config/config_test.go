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

func TestDefaultSettings(t *testing.T) {
	got := DefaultSettings()
	want := Settings{
		Version:         Version,
		InputDirectory:  "/Volumes/2TB HDD/Images",
		OutputDirectory: "/Volumes/2TB HDD/mp4/",
	}
	if got != want {
		t.Fatalf("DefaultSettings() = %#v, want %#v", got, want)
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		value   Settings
		errText string
	}{
		{"valid", Settings{Version: Version, InputDirectory: "/input", OutputDirectory: "/output"}, ""},
		{"unknown version", Settings{Version: Version + 1, InputDirectory: "/input", OutputDirectory: "/output"}, "unsupported"},
		{"relative input", Settings{Version: Version, InputDirectory: "input", OutputDirectory: "/output"}, "input directory"},
		{"relative output", Settings{Version: Version, InputDirectory: "/input", OutputDirectory: "output"}, "output directory"},
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
	input := filepath.Join(root, "入力")
	output := filepath.Join(root, "出力")
	if err := os.Mkdir(input, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(root, "data", "settings.json")}
	defaults := Settings{Version: Version, InputDirectory: input, OutputDirectory: output}

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
	input := filepath.Join(root, "input")
	output := filepath.Join(root, "output")
	if err := os.Mkdir(input, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "settings.json")
	invalid := []byte("{not-json")
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}

	store := Store{Path: path}
	_, err := store.LoadOrCreate(Settings{
		Version:         Version,
		InputDirectory:  input,
		OutputDirectory: output,
	})
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

func TestValidateDirectories(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		input   string
		output  string
		errText string
	}{
		{"directories", root, root, ""},
		{"input file", file, root, "input path is not a directory"},
		{"missing input", filepath.Join(root, "missing-input"), root, "stat input directory"},
		{"output file", root, file, "output path is not a directory"},
		{"missing output", root, filepath.Join(root, "missing-output"), "stat output directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := Settings{
				Version:         Version,
				InputDirectory:  tt.input,
				OutputDirectory: tt.output,
			}
			err := settings.ValidateDirectories()
			if tt.errText == "" && err != nil {
				t.Fatalf("ValidateDirectories() error = %v", err)
			}
			if tt.errText != "" && (err == nil || !strings.Contains(err.Error(), tt.errText)) {
				t.Fatalf("ValidateDirectories() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}

func TestStoreMigratesVersionOneSettings(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	output := filepath.Join(root, "output")
	if err := os.Mkdir(input, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "settings.json")
	if err := os.WriteFile(path, []byte("{\n  \"version\": 1,\n  \"output_directory\": \""+output+"\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := Store{Path: path}
	defaults := Settings{
		Version:         Version,
		InputDirectory:  input,
		OutputDirectory: filepath.Join(root, "unused-output"),
	}
	got, err := store.LoadOrCreate(defaults)
	if err != nil {
		t.Fatalf("LoadOrCreate() error = %v", err)
	}
	want := Settings{Version: Version, InputDirectory: input, OutputDirectory: output}
	if got != want {
		t.Fatalf("migrated settings = %#v, want %#v", got, want)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after migration error = %v", err)
	}
	if loaded != want {
		t.Fatalf("stored migrated settings = %#v, want %#v", loaded, want)
	}
}

func TestStoreDoesNotMigrateInvalidVersionOneSettings(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	if err := os.Mkdir(input, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "settings.json")
	invalid := []byte("{\n  \"version\": 1,\n  \"output_directory\": \"relative\"\n}\n")
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}

	store := Store{Path: path}
	_, err := store.LoadOrCreate(Settings{
		Version:         Version,
		InputDirectory:  input,
		OutputDirectory: root,
	})
	if err == nil {
		t.Fatal("LoadOrCreate() error = nil")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(invalid) {
		t.Fatalf("invalid version 1 settings were changed: %q", got)
	}
}
