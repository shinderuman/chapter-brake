package naming

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"chapterbrake/internal/queue"
)

func InputBase(inputPath string) (string, error) {
	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	if ext == "" {
		return "", fmt.Errorf("input has no extension: %q", inputPath)
	}
	return validateBase(strings.TrimSuffix(base, ext))
}

func NextIndex(base string, container queue.Container, queuedOutputs, existingNames []string) (int, error) {
	if _, err := validateBase(base); err != nil {
		return 0, err
	}
	if container != queue.ContainerMKV && container != queue.ContainerMP4 {
		return 0, fmt.Errorf("unsupported container %q", container)
	}

	maximum := 0
	for _, name := range append(append([]string(nil), queuedOutputs...), existingNames...) {
		number, ok := matchingIndex(filepath.Base(name), base, string(container))
		if ok && number > maximum {
			maximum = number
		}
	}
	if maximum == int(^uint(0)>>1) {
		return 0, fmt.Errorf("next index overflows int")
	}
	return maximum + 1, nil
}

func OutputPaths(directory, base string, start, count int, container queue.Container) ([]string, error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("output directory must be absolute: %q", directory)
	}
	if _, err := validateBase(base); err != nil {
		return nil, err
	}
	if start < 1 {
		return nil, fmt.Errorf("start index must be at least 1")
	}
	if count < 1 {
		return nil, fmt.Errorf("output count must be at least 1")
	}
	if container != queue.ContainerMKV && container != queue.ContainerMP4 {
		return nil, fmt.Errorf("unsupported container %q", container)
	}
	if count-1 > int(^uint(0)>>1)-start {
		return nil, fmt.Errorf("output index overflows int")
	}

	last := start + count - 1
	width := max(2, len(strconv.Itoa(last)))
	paths := make([]string, count)
	for i := range count {
		filename := fmt.Sprintf("%s_%0*d.%s", base, width, start+i, container)
		paths[i] = filepath.Join(directory, filename)
	}
	return paths, nil
}

func validateBase(base string) (string, error) {
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("invalid output base %q", base)
	}
	if filepath.Base(base) != base || strings.ContainsRune(base, 0) {
		return "", fmt.Errorf("output base must be one filename component: %q", base)
	}
	return base, nil
}

func matchingIndex(filename, base, extension string) (int, bool) {
	if !strings.EqualFold(filepath.Ext(filename), "."+extension) {
		return 0, false
	}
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	prefix := base + "_"
	if !strings.HasPrefix(stem, prefix) {
		return 0, false
	}
	digits := strings.TrimPrefix(stem, prefix)
	if digits == "" {
		return 0, false
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	number, err := strconv.Atoi(digits)
	if err != nil || number < 0 {
		return 0, false
	}
	return number, true
}
