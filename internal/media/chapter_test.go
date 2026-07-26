package media

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func chaptersAt(starts ...time.Duration) []Chapter {
	chapters := make([]Chapter, len(starts))
	for i, start := range starts {
		chapters[i] = Chapter{Number: i + 1, Start: start}
	}
	return chapters
}

func TestRangesFromStarts(t *testing.T) {
	tests := []struct {
		name     string
		selected []int
		final    int
		want     []ChapterRange
		errText  string
	}{
		{
			name:     "multiple ranges",
			selected: []int{1, 6, 14, 18},
			final:    24,
			want: []ChapterRange{
				{Start: 1, End: 5},
				{Start: 6, End: 13},
				{Start: 14, End: 17},
				{Start: 18, End: 24},
			},
		},
		{
			name:     "first chapters excluded",
			selected: []int{3, 9, 17},
			final:    24,
			want: []ChapterRange{
				{Start: 3, End: 8},
				{Start: 9, End: 16},
				{Start: 17, End: 24},
			},
		},
		{
			name:     "one selected",
			selected: []int{4},
			final:    10,
			want:     []ChapterRange{{Start: 4, End: 10}},
		},
		{
			name:     "unordered selection is normalized",
			selected: []int{9, 3, 17},
			final:    24,
			want: []ChapterRange{
				{Start: 3, End: 8},
				{Start: 9, End: 16},
				{Start: 17, End: 24},
			},
		},
		{"no selection", nil, 4, nil, "at least one"},
		{"duplicate", []int{1, 1}, 4, nil, "duplicated"},
		{"below range", []int{0}, 4, nil, "outside"},
		{"above range", []int{5}, 4, nil, "outside"},
		{"invalid final", []int{1}, 0, nil, "final chapter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RangesFromStarts(tt.selected, tt.final)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("RangesFromStarts() error = %v, want containing %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("RangesFromStarts() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("RangesFromStarts() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestApproximateStarts(t *testing.T) {
	minute := time.Minute
	tests := []struct {
		name     string
		chapters []Chapter
		total    time.Duration
		interval time.Duration
		want     Approximation
		errText  string
	}{
		{
			name: "nearest repeated targets",
			chapters: chaptersAt(
				0,
				10*minute,
				23*minute+39*time.Second,
				24*minute+5*time.Second,
				47*minute+50*time.Second,
			),
			total:    72 * minute,
			interval: DefaultEpisodeInterval,
			want:     Approximation{Starts: []int{1, 3, 5}},
		},
		{
			name:     "tie chooses earlier",
			chapters: chaptersAt(0, 20*minute, 28*minute),
			total:    60 * minute,
			interval: 24 * minute,
			want:     Approximation{Starts: []int{1, 2, 3}},
		},
		{
			name:     "single chapter",
			chapters: chaptersAt(0),
			total:    40 * minute,
			interval: DefaultEpisodeInterval,
			want:     Approximation{Starts: []int{1}},
		},
		{
			name:     "last chapter remains attached to prior output",
			chapters: chaptersAt(0, 5*minute),
			total:    30 * minute,
			interval: DefaultEpisodeInterval,
			want:     Approximation{Starts: []int{1}, TailMerged: true},
		},
		{
			name:     "full final interval remains a separate output",
			chapters: chaptersAt(0, 24*minute),
			total:    48 * minute,
			interval: DefaultEpisodeInterval,
			want:     Approximation{Starts: []int{1, 2}},
		},
		{
			name:     "seven second final chapter is merged",
			chapters: chaptersAt(0, 23*time.Minute+40*time.Second),
			total:    23*time.Minute + 47*time.Second,
			interval: DefaultEpisodeInterval,
			want:     Approximation{Starts: []int{1}, TailMerged: true},
		},
		{
			name: "real title does not select chapter 21",
			chapters: chaptersAt(
				0,
				113*time.Second,
				203*time.Second,
				812*time.Second,
				1326*time.Second,
				1416*time.Second,
				1422*time.Second,
				1520*time.Second,
				1610*time.Second,
				2191*time.Second,
				2747*time.Second,
				2837*time.Second,
				2843*time.Second,
				2939*time.Second,
				3029*time.Second,
				3784*time.Second,
				4168*time.Second,
				4258*time.Second,
				4264*time.Second,
				4357*time.Second,
				4694*time.Second,
			),
			total:    5685 * time.Second,
			interval: DefaultEpisodeInterval,
			want:     Approximation{Starts: []int{1, 7, 13, 19}, TailMerged: true},
		},
		{name: "no chapters", interval: DefaultEpisodeInterval, errText: "no chapters"},
		{name: "invalid interval", chapters: chaptersAt(0), total: time.Minute, errText: "positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApproximateStarts(tt.chapters, tt.total, tt.interval)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("ApproximateStarts() error = %v, want containing %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApproximateStarts() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ApproximateStarts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAndFormatChapterInterval(t *testing.T) {
	tests := []struct {
		value   string
		want    time.Duration
		errText string
	}{
		{value: "23:40", want: 23*time.Minute + 40*time.Second},
		{value: "0:01", want: time.Second},
		{value: "90:00", want: 90 * time.Minute},
		{value: "", errText: "M:SS"},
		{value: "23", errText: "M:SS"},
		{value: "23:4", errText: "M:SS"},
		{value: " 23:40", errText: "M:SS"},
		{value: "-1:00", errText: "minutes"},
		{value: "1:60", errText: "seconds"},
		{value: "0:00", errText: "positive"},
		{value: "999999999999999999:00", errText: "too large"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseChapterInterval(tt.value)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("ParseChapterInterval() error = %v, want containing %q", err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseChapterInterval() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseChapterInterval() = %s, want %s", got, tt.want)
			}
			if formatted := FormatChapterInterval(got); formatted != tt.value {
				t.Fatalf("FormatChapterInterval() = %q, want %q", formatted, tt.value)
			}
		})
	}

	for _, invalid := range []time.Duration{0, -time.Second, time.Millisecond} {
		if got := FormatChapterInterval(invalid); got != "" {
			t.Fatalf("FormatChapterInterval(%s) = %q, want empty", invalid, got)
		}
	}
}

func TestChapterAndOutputDurations(t *testing.T) {
	chapters := chaptersAt(0, 10*time.Second, 25*time.Second, 40*time.Second)
	total := 47 * time.Second
	chapterDurations, err := ChapterDurations(chapters, total)
	if err != nil {
		t.Fatal(err)
	}
	if want := []time.Duration{10 * time.Second, 15 * time.Second, 15 * time.Second, 7 * time.Second}; !reflect.DeepEqual(chapterDurations, want) {
		t.Fatalf("ChapterDurations() = %v, want %v", chapterDurations, want)
	}
	outputDurations, available, err := OutputDurations(chapters, total, []int{1, 3}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if outputDurations[0] != 25*time.Second || outputDurations[2] != 22*time.Second ||
		!available[0] || !available[2] {
		t.Fatalf("OutputDurations() = %v, %v", outputDurations, available)
	}
}

func TestValidateChapters(t *testing.T) {
	tests := []struct {
		name    string
		value   []Chapter
		errText string
	}{
		{"valid", chaptersAt(0, time.Minute), ""},
		{"wrong number", []Chapter{{Number: 2, Start: 0}}, "want 1"},
		{"negative start", []Chapter{{Number: 1, Start: -1}}, "negative"},
		{"same start", chaptersAt(0, 0), "must be after"},
		{"backwards", chaptersAt(time.Minute, 0), "must be after"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateChapters(tt.value)
			if tt.errText == "" && err != nil {
				t.Fatalf("ValidateChapters() error = %v", err)
			}
			if tt.errText != "" && (err == nil || !strings.Contains(err.Error(), tt.errText)) {
				t.Fatalf("ValidateChapters() error = %v, want containing %q", err, tt.errText)
			}
		})
	}
}

func TestElapsedFromPreviousSelection(t *testing.T) {
	chapters := chaptersAt(0, time.Minute, 2*time.Minute, 5*time.Minute)
	got, available, err := ElapsedFromPreviousSelection(chapters, []int{2, 4})
	if err != nil {
		t.Fatalf("ElapsedFromPreviousSelection() error = %v", err)
	}
	want := []time.Duration{0, 0, time.Minute, 0}
	wantAvailable := []bool{false, true, true, true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("elapsed = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(available, wantAvailable) {
		t.Fatalf("available = %v, want %v", available, wantAvailable)
	}
}

func TestChapterRangeApproximateDuration(t *testing.T) {
	chapters := chaptersAt(0, 3*time.Minute, 7*time.Minute)
	got, err := (ChapterRange{Start: 1, End: 2}).ApproximateDuration(chapters, 10*time.Minute)
	if err != nil || got != 7*time.Minute {
		t.Fatalf("ApproximateDuration(1-2) = %v, %v", got, err)
	}
	got, err = (ChapterRange{Start: 3, End: 3}).ApproximateDuration(chapters, 10*time.Minute)
	if err != nil || got != 3*time.Minute {
		t.Fatalf("ApproximateDuration(3-3) = %v, %v", got, err)
	}
	if _, err := (ChapterRange{Start: 0, End: 1}).ApproximateDuration(chapters, 10*time.Minute); err == nil {
		t.Fatal("ApproximateDuration(invalid) error = nil")
	}
}

func TestShortFinalChapter(t *testing.T) {
	chapters := chaptersAt(0, 10*time.Second, 20*time.Second)
	duration, err := FinalChapterDuration(chapters, 21*time.Second)
	if err != nil || duration != time.Second {
		t.Fatalf("FinalChapterDuration() = %s, %v", duration, err)
	}
	short, err := HasShortFinalChapter(chapters, 22*time.Second)
	if err != nil || !short {
		t.Fatalf("HasShortFinalChapter(2s) = %t, %v", short, err)
	}
	short, err = HasShortFinalChapter(chapters, 22*time.Second+time.Millisecond)
	if err != nil || short {
		t.Fatalf("HasShortFinalChapter(over 2s) = %t, %v", short, err)
	}
	short, err = HasShortFinalChapter(chapters[:1], time.Second)
	if err != nil || short {
		t.Fatalf("HasShortFinalChapter(single) = %t, %v", short, err)
	}
	if _, err := FinalChapterDuration(chapters, 19*time.Second); err == nil {
		t.Fatal("FinalChapterDuration(before final start) error = nil")
	}
}
