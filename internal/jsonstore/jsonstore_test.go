package jsonstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type document struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

func TestRead(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    document
		errText string
	}{
		{
			name:    "valid",
			content: `{"version":1,"name":"日本語"}`,
			want:    document{Version: 1, Name: "日本語"},
		},
		{
			name:    "unknown field",
			content: `{"version":1,"name":"x","extra":true}`,
			errText: "unknown field",
		},
		{
			name:    "trailing value",
			content: `{"version":1,"name":"x"} {}`,
			errText: "trailing JSON value",
		},
		{
			name:    "malformed",
			content: `{"version":`,
			errText: "unexpected EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "document.json")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}

			var got document
			err := Read(path, &got)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("Read() error = %v, want containing %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Read() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.json")
	want := document{Version: 1, Name: "空白 を含む"}

	if err := Write(path, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}

	var got document
	if err := Read(path, &got); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestWriteRenameFailurePreservesCanonicalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.json")
	original := []byte("{\"version\":1,\"name\":\"original\"}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	renameErr := errors.New("injected rename failure")
	err := write(path, document{Version: 1, Name: "replacement"}, func(string, string) error {
		return renameErr
	})
	if !errors.Is(err, renameErr) {
		t.Fatalf("write() error = %v, want wrapping %v", err, renameErr)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("canonical content = %q, want %q", got, original)
	}

	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".document.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}
