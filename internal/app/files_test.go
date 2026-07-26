package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListInputEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Series"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"日本語.mkv":     "1234",
		"UPPER.MKV":   "12",
		"ignore.mp4":  "x",
		".hidden.mkv": "hidden",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
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
	for _, name := range []string{"../", "Series/", "日本語.mkv", "UPPER.MKV"} {
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
	if _, err := ListInputEntries("relative"); err == nil {
		t.Fatal("ListInputEntries(relative) error = nil")
	}
}
