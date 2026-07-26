package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListInputEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Series"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "archive"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		name    string
		content string
	}{
		{name: "日本語.mkv", content: "1234"},
		{name: "UPPER.MKV", content: "12"},
		{name: "alpha.mkv", content: "a"},
		{name: "ignore.mp4", content: "x"},
		{name: ".hidden.mkv", content: "hidden"},
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(root, file.name), []byte(file.content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ListInputEntries(root)
	if err != nil {
		t.Fatalf("ListInputEntries() error = %v", err)
	}
	found := make(map[string]FileEntry)
	for _, entry := range entries {
		found[entry.Name] = entry
	}
	for _, name := range []string{"../", "alpha.mkv", "archive/", "Series/", "UPPER.MKV", "日本語.mkv"} {
		if _, ok := found[name]; !ok {
			t.Fatalf("entry %q missing from %#v", name, entries)
		}
	}
	for _, name := range []string{"ignore.mp4", ".hidden.mkv"} {
		if _, ok := found[name]; ok {
			t.Fatalf("unexpected entry %q", name)
		}
	}
	if found["日本語.mkv"].Size != 4 {
		t.Fatalf("file size = %d", found["日本語.mkv"].Size)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}
	wantNames := []string{"../", "alpha.mkv", "archive/", "Series/", "UPPER.MKV", "日本語.mkv"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("entry order = %v, want %v", names, wantNames)
	}
	if _, err := ListInputEntries("relative"); err == nil {
		t.Fatal("ListInputEntries(relative) error = nil")
	}
}
