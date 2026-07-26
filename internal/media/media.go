package media

import "time"

type AudioTrack struct {
	Number     int
	Language   string
	Name       string
	Codec      string
	Channels   int
	SampleRate int
}

type SubtitleTrack struct {
	Number   int
	Language string
	Name     string
	Format   string
}

type Info struct {
	Duration       time.Duration
	Chapters       []Chapter
	AudioTracks    []AudioTrack
	SubtitleTracks []SubtitleTrack
}
