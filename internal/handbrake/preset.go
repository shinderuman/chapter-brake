package handbrake

import (
	"encoding/json"
	"fmt"
	"strings"

	"chapterbrake/internal/queue"
)

type Preset struct {
	DisplayName       string
	HandBrakeName     string
	Container         queue.Container
	ChapterBrakeOwned bool
}

func CuratedPresets() []Preset {
	return []Preset{
		{
			DisplayName:       "1080p MP4",
			HandBrakeName:     "Super HQ 1080p30 Surround",
			Container:         queue.ContainerMP4,
			ChapterBrakeOwned: true,
		},
		{
			DisplayName:       "1080p MKV",
			HandBrakeName:     "H.264 MKV 1080p30",
			Container:         queue.ContainerMKV,
			ChapterBrakeOwned: true,
		},
		{
			DisplayName:       "480p MP4",
			HandBrakeName:     "Fast 480p30",
			Container:         queue.ContainerMP4,
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
	if p.Container != queue.ContainerMKV && p.Container != queue.ContainerMP4 {
		return fmt.Errorf("unsupported preset container %q", p.Container)
	}
	return nil
}

func ResolveQueuedPreset(name string, container queue.Container) (Preset, error) {
	for _, preset := range CuratedPresets() {
		if preset.DisplayName == name {
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

type presetDocument struct {
	PresetList []presetNode `json:"PresetList"`
}

type presetNode struct {
	PresetName    string       `json:"PresetName"`
	FileFormat    string       `json:"FileFormat"`
	Folder        bool         `json:"Folder"`
	ChildrenArray []presetNode `json:"ChildrenArray"`
}

// ParseExportedPreset reads HandBrake's preset export JSON. It requires exactly
// one non-folder preset so callers cannot silently select the wrong node.
func ParseExportedPreset(data []byte, displayName string) (Preset, error) {
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
			return Preset{}, fmt.Errorf("decode preset export: %w", err)
		}
		document.PresetList = make([]presetNode, 0, len(raw.PresetList))
		for _, item := range raw.PresetList {
			var node presetNode
			if rawErr := json.Unmarshal(item, &node); rawErr != nil {
				return Preset{}, fmt.Errorf("decode preset node: %w", rawErr)
			}
			document.PresetList = append(document.PresetList, node)
		}
	}

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
