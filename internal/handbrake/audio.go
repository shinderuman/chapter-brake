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

type AudioQuality string

const (
	AudioHigh     AudioQuality = "high"
	AudioStandard AudioQuality = "standard"
)

type AudioOutput struct {
	InputTrack int
	Quality    AudioQuality
	Encoder    string
	Bitrate    int
	Mixdown    string
	SampleRate string
}

// AudioPlan creates two outputs per selected input in input-track order:
// high quality first, then standard quality.
func AudioPlan(selected []int, available []media.AudioTrack, container queue.Container) ([]AudioOutput, error) {
	if container != queue.ContainerMKV && container != queue.ContainerMP4 {
		return nil, fmt.Errorf("unsupported container %q", container)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one audio track must be selected")
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

	ordered := append([]int(nil), selected...)
	sort.Ints(ordered)
	outputs := make([]AudioOutput, 0, len(ordered)*2)
	previous := 0
	for _, number := range ordered {
		if number < 1 || number > 2 {
			return nil, fmt.Errorf("audio track %d is not supported in the initial version", number)
		}
		if number == previous {
			return nil, fmt.Errorf("audio track %d is selected more than once", number)
		}
		previous = number

		track, ok := tracks[number]
		if !ok {
			return nil, fmt.Errorf("selected audio track %d does not exist", number)
		}
		high := AudioOutput{
			InputTrack: number,
			Quality:    AudioHigh,
			Encoder:    highFallbackEncoder,
			Bitrate:    highBitrate,
			Mixdown:    highMixdown(track.Channels),
			SampleRate: "auto",
		}
		if encoder, ok := passthroughEncoder(track.Codec, container); ok {
			high.Encoder = encoder
		}
		outputs = append(outputs, high, AudioOutput{
			InputTrack: number,
			Quality:    AudioStandard,
			Encoder:    standardEncoder,
			Bitrate:    standardBitrate,
			Mixdown:    "stereo",
			SampleRate: "auto",
		})
	}
	return outputs, nil
}

func passthroughEncoder(codec string, container queue.Container) (string, bool) {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(codec))
	// AC-3 passthrough in MP4 and MKV is the path proven by the PoC. Other
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
