package handbrake

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"chapterbrake/internal/queue"
)

type Preset struct {
	DisplayName       string
	Summary           string
	HandBrakeName     string
	ImportFile        string
	Container         queue.Container
	CropMode          string
	ChapterBrakeOwned bool
}

func CuratedPresets() []Preset {
	return []Preset{
		{
			DisplayName:       "MP4 Presets",
			Summary:           "1080p MP4・自動クロップ",
			HandBrakeName:     "Super HQ 1080p30 Surround",
			Container:         queue.ContainerMP4,
			CropMode:          "auto",
			ChapterBrakeOwned: true,
		},
		{
			DisplayName:       "MKV Presets",
			Summary:           "1080p MKV・自動クロップ",
			HandBrakeName:     "H.264 MKV 1080p30",
			Container:         queue.ContainerMKV,
			CropMode:          "auto",
			ChapterBrakeOwned: true,
		},
		{
			DisplayName:       "My Old Presets",
			Summary:           "480p MP4・自動クロップ",
			HandBrakeName:     "Fast 480p30",
			Container:         queue.ContainerMP4,
			CropMode:          "auto",
			ChapterBrakeOwned: true,
		},
		{
			DisplayName:       "GCCX",
			Summary:           "480p MP4・クロップなし",
			HandBrakeName:     "Fast 480p30",
			Container:         queue.ContainerMP4,
			CropMode:          "none",
			ChapterBrakeOwned: true,
		},
	}
}

func (p Preset) Validate() error {
	if strings.TrimSpace(p.DisplayName) == "" {
		return fmt.Errorf("preset display name must not be empty")
	}
	if strings.TrimSpace(p.HandBrakeName) == "" {
		return fmt.Errorf("HandBrake preset name must not be empty")
	}
	if p.ImportFile != "" && !filepath.IsAbs(p.ImportFile) {
		return fmt.Errorf("preset import file must be absolute: %q", p.ImportFile)
	}
	if p.Container != queue.ContainerMKV && p.Container != queue.ContainerMP4 {
		return fmt.Errorf("unsupported preset container %q", p.Container)
	}
	if p.CropMode != "" && p.CropMode != "auto" && p.CropMode != "none" {
		return fmt.Errorf("unsupported preset crop mode %q", p.CropMode)
	}
	return nil
}

func ResolveQueuedPreset(name string, container queue.Container, presetFiles ...string) (Preset, error) {
	if len(presetFiles) > 1 {
		return Preset{}, fmt.Errorf("at most one preset file may be specified")
	}
	if len(presetFiles) == 1 && presetFiles[0] != "" {
		presets, err := LoadPresetFile(presetFiles[0])
		if err != nil {
			return Preset{}, err
		}
		for _, preset := range presets {
			if preset.DisplayName != name {
				continue
			}
			if preset.Container != container {
				return Preset{}, fmt.Errorf(
					"queued container %q does not match imported preset %q container %q",
					container,
					name,
					preset.Container,
				)
			}
			return preset, nil
		}
		return Preset{}, fmt.Errorf("preset %q does not exist in %s", name, presetFiles[0])
	}
	if preset, ok := resolveCuratedPreset(name); ok {
		if preset.Container != container {
			return Preset{}, fmt.Errorf(
				"queued container %q does not match curated preset %q container %q",
				container,
				name,
				preset.Container,
			)
		}
		return preset, nil
	}
	preset := Preset{
		DisplayName:   name,
		HandBrakeName: name,
		Container:     container,
	}
	if err := preset.Validate(); err != nil {
		return Preset{}, err
	}
	return preset, nil
}

func LoadPresetFile(path string) ([]Preset, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("preset file must be absolute: %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read preset file %s: %w", path, err)
	}
	presets, err := ParseExportedPresets(data, path)
	if err != nil {
		return nil, fmt.Errorf("parse preset file %s: %w", path, err)
	}
	return presets, nil
}

func LoadPresetFileOrDefaults(path string) ([]Preset, error) {
	presets, err := LoadPresetFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return CuratedPresets(), nil
	}
	return presets, err
}

func resolveCuratedPreset(name string) (Preset, bool) {
	for _, preset := range CuratedPresets() {
		if preset.DisplayName == name {
			return preset, true
		}
	}
	index := -1
	switch name {
	case "1080p MP4":
		index = 0
	case "1080p MKV":
		index = 1
	case "480p MP4":
		index = 2
	}
	if index < 0 {
		return Preset{}, false
	}
	preset := CuratedPresets()[index]
	preset.DisplayName = name
	return preset, true
}

type presetDocument struct {
	PresetList []presetNode `json:"PresetList"`
}

type presetNode struct {
	PresetName    string       `json:"PresetName"`
	FileFormat    string       `json:"FileFormat"`
	Folder        bool         `json:"Folder"`
	PictureWidth  int          `json:"PictureWidth"`
	PictureHeight int          `json:"PictureHeight"`
	PictureCrop   *int         `json:"PictureCropMode"`
	ChildrenArray []presetNode `json:"ChildrenArray"`
}

func parsePresetDocument(data []byte) (presetDocument, error) {
	var document presetDocument
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		// HandBrake preset objects contain many fields. Decode only the routing
		// fields with a second permissive pass while retaining strict top-level
		// shape through an explicit envelope.
		var raw struct {
			PresetList []json.RawMessage `json:"PresetList"`
		}
		if rawErr := json.Unmarshal(data, &raw); rawErr != nil {
			return presetDocument{}, fmt.Errorf("decode preset export: %w", err)
		}
		document.PresetList = make([]presetNode, 0, len(raw.PresetList))
		for _, item := range raw.PresetList {
			var node presetNode
			if rawErr := json.Unmarshal(item, &node); rawErr != nil {
				return presetDocument{}, fmt.Errorf("decode preset node: %w", rawErr)
			}
			document.PresetList = append(document.PresetList, node)
		}
	}
	return document, nil
}

func presetLeaves(document presetDocument) []presetNode {
	var leaves []presetNode
	var collect func([]presetNode)
	collect = func(nodes []presetNode) {
		for _, node := range nodes {
			if node.Folder {
				collect(node.ChildrenArray)
				continue
			}
			leaves = append(leaves, node)
		}
	}
	collect(document.PresetList)
	return leaves
}

// ParseExportedPresets reads all non-folder presets in a HandBrake GUI export.
func ParseExportedPresets(data []byte, importFile string) ([]Preset, error) {
	if importFile != "" && !filepath.IsAbs(importFile) {
		return nil, fmt.Errorf("preset import file must be absolute: %q", importFile)
	}
	document, err := parsePresetDocument(data)
	if err != nil {
		return nil, err
	}
	leaves := presetLeaves(document)
	if len(leaves) == 0 {
		return nil, fmt.Errorf("preset export contains no leaf presets")
	}
	presets := make([]Preset, 0, len(leaves))
	seen := make(map[string]struct{}, len(leaves))
	for _, leaf := range leaves {
		if _, duplicate := seen[leaf.PresetName]; duplicate {
			return nil, fmt.Errorf("preset export contains duplicate preset name %q", leaf.PresetName)
		}
		seen[leaf.PresetName] = struct{}{}
		container, err := containerFromFileFormat(leaf.FileFormat)
		if err != nil {
			return nil, fmt.Errorf("preset %q: %w", leaf.PresetName, err)
		}
		preset := Preset{
			DisplayName:       leaf.PresetName,
			Summary:           importedPresetSummary(leaf, container),
			HandBrakeName:     leaf.PresetName,
			ImportFile:        importFile,
			Container:         container,
			ChapterBrakeOwned: true,
		}
		if err := preset.Validate(); err != nil {
			return nil, err
		}
		presets = append(presets, preset)
	}
	return presets, nil
}

func importedPresetSummary(node presetNode, container queue.Container) string {
	parts := make([]string, 0, 3)
	if node.PictureWidth > 0 && node.PictureHeight > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", node.PictureWidth, node.PictureHeight))
	}
	parts = append(parts, strings.ToUpper(string(container)))
	if node.PictureCrop != nil {
		switch *node.PictureCrop {
		case 0:
			parts = append(parts, "自動クロップ")
		case 2:
			parts = append(parts, "クロップなし")
		}
	}
	return strings.Join(parts, "・")
}

// ParseExportedPreset reads a HandBrake export containing exactly one preset.
func ParseExportedPreset(data []byte, displayName string) (Preset, error) {
	document, err := parsePresetDocument(data)
	if err != nil {
		return Preset{}, err
	}
	leaves := presetLeaves(document)
	if len(leaves) != 1 {
		return Preset{}, fmt.Errorf("preset export contains %d leaf presets, want 1", len(leaves))
	}

	container, err := containerFromFileFormat(leaves[0].FileFormat)
	if err != nil {
		return Preset{}, err
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = leaves[0].PresetName
	}
	preset := Preset{
		DisplayName:   displayName,
		HandBrakeName: leaves[0].PresetName,
		Container:     container,
	}
	if err := preset.Validate(); err != nil {
		return Preset{}, err
	}
	return preset, nil
}

func containerFromFileFormat(format string) (queue.Container, error) {
	switch format {
	case "av_mkv":
		return queue.ContainerMKV, nil
	case "av_mp4":
		return queue.ContainerMP4, nil
	default:
		return "", fmt.Errorf("unsupported HandBrake file format %q", format)
	}
}
