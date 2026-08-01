package handbrake

import (
	"fmt"
	"sort"
	"strings"

	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
)

const (
	highFallbackEncoder = "ca_aac"
	highBitrate         = 640
	standardEncoder     = "ca_aac"
	standardBitrate     = 160
)

type AudioOutput struct {
	InputTrack int
	Quality    queue.AudioQuality
	Encoder    string
	Bitrate    int
	Mixdown    string
	SampleRate string
}

// AudioPlan creates one output for each selected input/quality pair.
func AudioPlan(selected []queue.AudioSelection, available []media.AudioTrack, container queue.Container) ([]AudioOutput, error) {
	if container != queue.ContainerMKV && container != queue.ContainerMP4 {
		return nil, fmt.Errorf("unsupported container %q", container)
	}
	if len(selected) == 0 {
		return []AudioOutput{}, nil
	}

	tracks := make(map[int]media.AudioTrack, len(available))
	for _, track := range available {
		if track.Number < 1 {
			return nil, fmt.Errorf("available audio track has invalid number %d", track.Number)
		}
		if _, exists := tracks[track.Number]; exists {
			return nil, fmt.Errorf("available audio track %d is duplicated", track.Number)
		}
		tracks[track.Number] = track
	}

	ordered := append([]queue.AudioSelection(nil), selected...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Track < ordered[j].Track })
	outputs := make([]AudioOutput, 0, len(ordered))
	seen := make(map[int]struct{}, len(ordered))
	for _, selection := range ordered {
		if _, exists := seen[selection.Track]; exists {
			return nil, fmt.Errorf("audio track %d is selected more than once", selection.Track)
		}
		seen[selection.Track] = struct{}{}

		track, ok := tracks[selection.Track]
		if !ok {
			return nil, fmt.Errorf("selected audio track %d does not exist", selection.Track)
		}
		switch selection.Quality {
		case queue.AudioHigh:
			output := AudioOutput{
				InputTrack: selection.Track,
				Quality:    queue.AudioHigh,
				Encoder:    highFallbackEncoder,
				Bitrate:    highBitrate,
				Mixdown:    highMixdown(track.Channels),
				SampleRate: "auto",
			}
			if encoder, ok := passthroughEncoder(track.Codec, container); ok {
				output.Encoder = encoder
			}
			outputs = append(outputs, output)
		case queue.AudioStandard:
			outputs = append(outputs, AudioOutput{
				InputTrack: selection.Track,
				Quality:    queue.AudioStandard,
				Encoder:    standardEncoder,
				Bitrate:    standardBitrate,
				Mixdown:    "stereo",
				SampleRate: "auto",
			})
		default:
			return nil, fmt.Errorf("audio track %d has unsupported quality %q", selection.Track, selection.Quality)
		}
	}
	return outputs, nil
}

func passthroughEncoder(codec string, container queue.Container) (string, bool) {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(codec))
	// AC-3 passthrough in MP4 and MKV is the locally verified path. Other
	// codecs deliberately use the safe high-quality AAC fallback until their
	// container combinations are verified with real encodes.
	if normalized == "ac3" {
		return "copy:ac3", true
	}
	return "", false
}

func highMixdown(channels int) string {
	if channels > 2 {
		return "5point1"
	}
	return "stereo"
}
