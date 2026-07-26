package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileEntry struct {
	Name  string
	Path  string
	IsDir bool
	Size  int64
}

func ListInputEntries(directory string) ([]FileEntry, error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("directory must be absolute: %q", directory)
	}
	handle, err := os.Open(directory)
	if err != nil {
		return nil, fmt.Errorf("open directory %s: %w", directory, err)
	}
	defer handle.Close()
	entries, err := handle.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", directory, err)
	}

	result := make([]FileEntry, 0, len(entries)+1)
	parent := filepath.Dir(directory)
	if parent != directory {
		result = append(result, FileEntry{Name: "../", Path: parent, IsDir: true})
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			result = append(result, FileEntry{Name: entry.Name() + "/", Path: path, IsDir: true})
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".mkv") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat input candidate %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		result = append(result, FileEntry{Name: entry.Name(), Path: path, Size: info.Size()})
	}
	sortStart := 0
	if len(result) > 0 && result[0].Name == "../" {
		sortStart = 1
	}
	sortable := result[sortStart:]
	sort.Slice(sortable, func(i, j int) bool {
		left := sortable[i].Name
		right := sortable[j].Name
		leftFolded := strings.ToLower(left)
		rightFolded := strings.ToLower(right)
		if leftFolded == rightFolded {
			return left < right
		}
		return leftFolded < rightFolded
	})
	return result, nil
}
