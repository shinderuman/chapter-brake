package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestValidationAndSecureChild(t *testing.T) {
	valid := Manifest{
		SchemaVersion: 1,
		ID:            "chapter-brake",
		Name:          "ChapterBrake",
		WebRoot:       "web",
		Backend:       Backend{Executable: "bin/backend"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, manifest := range []Manifest{
		{},
		{SchemaVersion: 2, ID: "app", Name: "App", WebRoot: "web", Backend: Backend{Executable: "bin/app"}},
		{SchemaVersion: 1, ID: "../bad", Name: "App", WebRoot: "web", Backend: Backend{Executable: "bin/app"}},
		{SchemaVersion: 1, ID: "app", WebRoot: "web", Backend: Backend{Executable: "bin/app"}},
	} {
		if err := manifest.Validate(); err == nil {
			t.Fatalf("Validate(%#v) error = nil", manifest)
		}
	}
	root := t.TempDir()
	if _, err := secureChild(root, "../outside"); err == nil {
		t.Fatal("secureChild traversal error = nil")
	}
	if _, err := secureChild(root, "/absolute"); err == nil {
		t.Fatal("secureChild absolute error = nil")
	}
	got, err := secureChild(root, "web/index.html")
	if err != nil || got != filepath.Join(root, "web", "index.html") {
		t.Fatalf("secureChild() = %q, %v", got, err)
	}
}

func TestLoadApps(t *testing.T) {
	apps := t.TempDir()
	root := filepath.Join(apps, "chapter-brake")
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schema_version":1,"id":"chapter-brake","name":"ChapterBrake","web_root":"web","backend":{"executable":"bin/backend"}}`
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "backend"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadApps(apps, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Manifest.ID != "chapter-brake" {
		t.Fatalf("LoadApps() = %#v", loaded)
	}
}
