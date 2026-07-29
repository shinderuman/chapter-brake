package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

const manifestSchemaVersion = 1

var appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Manifest struct {
	SchemaVersion int     `json:"schema_version"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	WebRoot       string  `json:"web_root"`
	Backend       Backend `json:"backend"`
}

type Backend struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
}

type InstalledApp struct {
	Manifest  Manifest
	Root      string
	WebRoot   string
	Backend   string
	Socket    string
	available bool
	lastError string
}

func LoadApps(appsDirectory, runtimeDirectory string) ([]*InstalledApp, error) {
	appsDirectory, err := filepath.Abs(appsDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve apps directory: %w", err)
	}
	runtimeDirectory, err = filepath.Abs(runtimeDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime directory: %w", err)
	}
	entries, err := os.ReadDir(appsDirectory)
	if err != nil {
		return nil, fmt.Errorf("read apps directory: %w", err)
	}
	if err := os.MkdirAll(runtimeDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create runtime directory: %w", err)
	}
	apps := make([]*InstalledApp, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root := filepath.Join(appsDirectory, entry.Name())
		app, err := loadApp(root, runtimeDirectory)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[app.Manifest.ID]; duplicate {
			return nil, fmt.Errorf("duplicate app id %q", app.Manifest.ID)
		}
		seen[app.Manifest.ID] = struct{}{}
		apps = append(apps, app)
	}
	if len(apps) == 0 {
		return nil, fmt.Errorf("no installed apps found in %s", appsDirectory)
	}
	return apps, nil
}

func loadApp(root, runtimeDirectory string) (*InstalledApp, error) {
	data, err := os.ReadFile(filepath.Join(root, "app.json"))
	if err != nil {
		return nil, fmt.Errorf("read app manifest %s: %w", root, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode app manifest %s: %w", root, err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("validate app manifest %s: %w", root, err)
	}
	webRoot, err := secureChild(root, manifest.WebRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve web_root: %w", err)
	}
	backend, err := secureChild(root, manifest.Backend.Executable)
	if err != nil {
		return nil, fmt.Errorf("resolve backend executable: %w", err)
	}
	if info, err := os.Stat(webRoot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("web_root is not a directory: %s", webRoot)
	}
	if info, err := os.Stat(backend); err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return nil, fmt.Errorf("backend is not executable: %s", backend)
	}
	return &InstalledApp{
		Manifest: manifest,
		Root:     root,
		WebRoot:  webRoot,
		Backend:  backend,
		Socket:   filepath.Join(runtimeDirectory, manifest.ID+".sock"),
	}, nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", m.SchemaVersion)
	}
	if !appIDPattern.MatchString(m.ID) {
		return fmt.Errorf("invalid app id %q", m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if m.WebRoot == "" {
		return fmt.Errorf("web_root must not be empty")
	}
	if m.Backend.Executable == "" {
		return fmt.Errorf("backend executable must not be empty")
	}
	return nil
}

func secureChild(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("path must be relative: %q", relative)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidate := filepath.Clean(filepath.Join(root, relative))
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("path escapes app root: %q", relative)
	}
	return candidate, nil
}
