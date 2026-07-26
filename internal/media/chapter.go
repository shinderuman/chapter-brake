package media

import (
	"fmt"
	"sort"
	"time"
)

const DefaultEpisodeInterval = 23*time.Minute + 40*time.Second

type Chapter struct {
	Number int
	Start  time.Duration
	Title  string
}

type ChapterRange struct {
	Start int
	End   int
}

func (r ChapterRange) ApproximateDuration(chapters []Chapter, total time.Duration) (time.Duration, error) {
	if err := ValidateChapters(chapters); err != nil {
		return 0, err
	}
	if r.Start < 1 || r.End < r.Start || r.End > len(chapters) {
		return 0, fmt.Errorf("invalid chapter range %d-%d", r.Start, r.End)
	}
	start := chapters[r.Start-1].Start
	end := total
	if r.End < len(chapters) {
		end = chapters[r.End].Start
	}
	if end < start {
		return 0, fmt.Errorf("range duration is negative")
	}
	return end - start, nil
}

func ValidateChapters(chapters []Chapter) error {
	if len(chapters) == 0 {
		return fmt.Errorf("media has no chapters")
	}
	for i, chapter := range chapters {
		wantNumber := i + 1
		if chapter.Number != wantNumber {
			return fmt.Errorf("chapter %d has number %d, want %d", i+1, chapter.Number, wantNumber)
		}
		if chapter.Start < 0 {
			return fmt.Errorf("chapter %d has negative start time", chapter.Number)
		}
		if i > 0 && chapter.Start <= chapters[i-1].Start {
			return fmt.Errorf("chapter %d start must be after chapter %d", chapter.Number, chapters[i-1].Number)
		}
	}
	return nil
}

// ApproximateStarts selects chapter starts nearest to repeated interval targets.
// Equal distances choose the earlier chapter.
func ApproximateStarts(chapters []Chapter, interval time.Duration) ([]int, error) {
	if err := ValidateChapters(chapters); err != nil {
		return nil, err
	}
	if interval <= 0 {
		return nil, fmt.Errorf("interval must be positive")
	}

	selected := []int{chapters[0].Number}
	currentIndex := 0
	for currentIndex < len(chapters)-1 {
		target := chapters[currentIndex].Start + interval
		bestIndex := currentIndex + 1
		bestDistance := durationDistance(chapters[bestIndex].Start, target)
		for i := bestIndex + 1; i < len(chapters); i++ {
			distance := durationDistance(chapters[i].Start, target)
			if distance < bestDistance {
				bestIndex = i
				bestDistance = distance
			}
		}
		selected = append(selected, chapters[bestIndex].Number)
		currentIndex = bestIndex
	}
	return selected, nil
}

func durationDistance(a, b time.Duration) time.Duration {
	if a >= b {
		return a - b
	}
	return b - a
}

func RangesFromStarts(selected []int, finalChapter int) ([]ChapterRange, error) {
	if finalChapter < 1 {
		return nil, fmt.Errorf("final chapter must be at least 1")
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("at least one chapter start must be selected")
	}

	starts := append([]int(nil), selected...)
	sort.Ints(starts)
	for i, start := range starts {
		if start < 1 || start > finalChapter {
			return nil, fmt.Errorf("selected chapter %d is outside 1-%d", start, finalChapter)
		}
		if i > 0 && start == starts[i-1] {
			return nil, fmt.Errorf("selected chapter %d is duplicated", start)
		}
	}

	ranges := make([]ChapterRange, len(starts))
	for i, start := range starts {
		end := finalChapter
		if i+1 < len(starts) {
			end = starts[i+1] - 1
		}
		ranges[i] = ChapterRange{Start: start, End: end}
	}
	return ranges, nil
}

// ElapsedFromPreviousSelection returns each chapter's elapsed time from the
// nearest selected chapter at or before it. Chapters before the first selection
// have no elapsed value.
func ElapsedFromPreviousSelection(chapters []Chapter, selected []int) ([]time.Duration, []bool, error) {
	if err := ValidateChapters(chapters); err != nil {
		return nil, nil, err
	}
	if _, err := RangesFromStarts(selected, len(chapters)); err != nil {
		return nil, nil, err
	}

	startSet := make(map[int]struct{}, len(selected))
	for _, number := range selected {
		startSet[number] = struct{}{}
	}

	elapsed := make([]time.Duration, len(chapters))
	available := make([]bool, len(chapters))
	var base time.Duration
	haveBase := false
	for i, chapter := range chapters {
		if _, ok := startSet[chapter.Number]; ok {
			base = chapter.Start
			haveBase = true
		}
		if haveBase {
			elapsed[i] = chapter.Start - base
			available[i] = true
		}
	}
	return elapsed, available, nil
}
