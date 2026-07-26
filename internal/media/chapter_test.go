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
		interval time.Duration
		want     []int
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
			interval: DefaultEpisodeInterval,
			want:     []int{1, 3, 5},
		},
		{
			name:     "tie chooses earlier",
			chapters: chaptersAt(0, 20*minute, 28*minute),
			interval: 24 * minute,
			want:     []int{1, 2, 3},
		},
		{
			name:     "single chapter",
			chapters: chaptersAt(0),
			interval: DefaultEpisodeInterval,
			want:     []int{1},
		},
		{
			name:     "last chapter is selected when no close target remains",
			chapters: chaptersAt(0, 5*minute),
			interval: DefaultEpisodeInterval,
			want:     []int{1, 2},
		},
		{"no chapters", nil, DefaultEpisodeInterval, nil, "no chapters"},
		{"invalid interval", chaptersAt(0), 0, nil, "positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApproximateStarts(tt.chapters, tt.interval)
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
