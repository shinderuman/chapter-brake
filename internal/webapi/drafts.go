package webapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
)

type createDraftRequest struct {
	Input      string `json:"input"`
	AnalysisID string `json:"analysis_id,omitempty"`
}

type presetRequest struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type namingRequest struct {
	Base       string `json:"base"`
	StartIndex int    `json:"start_index"`
}

type chaptersRequest struct {
	Interval         string `json:"interval"`
	SelectedChapters []int  `json:"selected_chapters"`
	ExcludeFinal     bool   `json:"exclude_final"`
	Approximate      bool   `json:"approximate"`
}

type tracksRequest struct {
	Tracks []int `json:"tracks"`
}

type addQueueRequest struct {
	OverwriteApproved bool `json:"overwrite_approved"`
}

type draftView struct {
	ID                string              `json:"id"`
	Input             string              `json:"input"`
	DurationSeconds   int64               `json:"duration_seconds"`
	Chapters          []chapterView       `json:"chapters"`
	AudioTracks       []audioTrackView    `json:"audio_tracks"`
	SubtitleTracks    []subtitleTrackView `json:"subtitle_tracks"`
	Preset            *presetView         `json:"preset,omitempty"`
	Base              string              `json:"base"`
	StartIndex        int                 `json:"start_index"`
	ChapterInterval   string              `json:"chapter_interval"`
	SelectedChapters  []int               `json:"selected_chapters"`
	SelectedAudio     []int               `json:"selected_audio"`
	SelectedSubtitles []int               `json:"selected_subtitles"`
	AutoChapters      bool                `json:"auto_chapters"`
	TailMerged        bool                `json:"tail_merged"`
	ExcludeFinal      bool                `json:"exclude_final"`
	Preview           *previewView        `json:"preview,omitempty"`
}

type chapterView struct {
	Number                int    `json:"number"`
	StartSeconds          int64  `json:"start_seconds"`
	DurationSeconds       int64  `json:"duration_seconds"`
	OutputDurationSeconds *int64 `json:"output_duration_seconds,omitempty"`
	Title                 string `json:"title,omitempty"`
}

type audioTrackView struct {
	Number     int    `json:"number"`
	Language   string `json:"language,omitempty"`
	Name       string `json:"name,omitempty"`
	Codec      string `json:"codec,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
}

type subtitleTrackView struct {
	Number   int    `json:"number"`
	Language string `json:"language,omitempty"`
	Name     string `json:"name,omitempty"`
	Format   string `json:"format,omitempty"`
}

type presetView struct {
	Name      string          `json:"name"`
	Summary   string          `json:"summary,omitempty"`
	Container queue.Container `json:"container"`
	Source    string          `json:"source"`
}

type previewView struct {
	Jobs          []queue.Job       `json:"jobs"`
	Collisions    []string          `json:"collisions"`
	Excluded      *chapterRangeView `json:"excluded,omitempty"`
	ExcludedFinal *chapterRangeView `json:"excluded_final,omitempty"`
}

type chapterRangeView struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func (server *Server) createDraft(writer http.ResponseWriter, request *http.Request) {
	var body createDraftRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "analyze", err)
		return
	}
	if body.AnalysisID != "" {
		if err := validateAnalysisID(body.AnalysisID); err != nil {
			server.writeError(writer, http.StatusBadRequest, "invalid_analysis_id", "analyze", err)
			return
		}
		server.setAnalysisProgress(body.AnalysisID, 0)
		defer server.scheduleAnalysisProgressClear(body.AnalysisID)
	}
	progress := func(value float64) {
		if body.AnalysisID != "" {
			server.setAnalysisProgress(body.AnalysisID, value)
		}
	}
	var draft app.Draft
	var err error
	if analyzer, ok := server.config.Application.(interface {
		AnalyzeWithProgress(context.Context, string, func(float64)) (app.Draft, error)
	}); ok {
		draft, err = analyzer.AnalyzeWithProgress(request.Context(), body.Input, progress)
	} else {
		draft, err = server.config.Application.Analyze(request.Context(), body.Input)
	}
	if err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "analysis_failed", "analyze", err)
		return
	}
	id := fmt.Sprintf("draft-%06d", server.sequence.Add(1))
	state := &draftState{Draft: draft}
	server.mu.Lock()
	server.drafts[id] = state
	server.mu.Unlock()
	server.writeDraft(writer, http.StatusCreated, id, state)
}

func (server *Server) getAnalysisProgress(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request)
	if err != nil || validateAnalysisID(id) != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_analysis_id", "analyze", errors.New("invalid analysis id"))
		return
	}
	server.mu.Lock()
	progress, exists := server.analyses[id]
	server.mu.Unlock()
	if !exists {
		server.writeError(writer, http.StatusNotFound, "analysis_not_found", "analyze", errors.New("analysis is not running"))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]float64{"progress": progress})
}

func (server *Server) setAnalysisProgress(id string, progress float64) {
	server.mu.Lock()
	server.analyses[id] = max(0, min(1, progress))
	server.mu.Unlock()
}

func (server *Server) clearAnalysisProgress(id string) {
	server.mu.Lock()
	delete(server.analyses, id)
	server.mu.Unlock()
}

func (server *Server) scheduleAnalysisProgressClear(id string) {
	time.AfterFunc(5*time.Second, func() {
		server.clearAnalysisProgress(id)
	})
}

func validateAnalysisID(id string) error {
	if len(id) < 8 || len(id) > 128 {
		return errors.New("analysis id length is invalid")
	}
	for _, char := range id {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return errors.New("analysis id contains an invalid character")
	}
	return nil
}

func (server *Server) getDraft(writer http.ResponseWriter, request *http.Request) {
	id, state, ok := server.loadDraft(request)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "draft", errors.New("draft does not exist"))
		return
	}
	server.writeDraft(writer, http.StatusOK, id, state)
}

func (server *Server) deleteDraft(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request)
	if err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_id", "draft", err)
		return
	}
	server.mu.Lock()
	_, exists := server.drafts[id]
	delete(server.drafts, id)
	server.mu.Unlock()
	if !exists {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "draft", errors.New("draft does not exist"))
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) presets(writer http.ResponseWriter, request *http.Request) {
	source := request.URL.Query().Get("source")
	switch source {
	case "", "curated":
		presets := server.config.Presets.Curated()
		views := make([]presetView, len(presets))
		for index, preset := range presets {
			views[index] = viewPreset(preset)
		}
		writeJSON(writer, http.StatusOK, map[string]any{"source": "curated", "presets": views})
	case "standard":
		presets, err := server.config.Presets.ListStandard(request.Context(), nil, nil)
		if err != nil {
			server.writeError(writer, http.StatusInternalServerError, "preset_list_failed", "presets", err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"source": "standard", "presets": presets})
	default:
		server.writeError(writer, http.StatusBadRequest, "invalid_source", "presets", fmt.Errorf("unsupported preset source %q", source))
	}
}

func (server *Server) setDraftPreset(writer http.ResponseWriter, request *http.Request) {
	var body presetRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "preset", err)
		return
	}
	id, _, ok := server.loadDraft(request)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "preset", errors.New("draft does not exist"))
		return
	}
	var preset handbrake.Preset
	var err error
	switch body.Source {
	case "curated":
		for _, candidate := range server.config.Presets.Curated() {
			if candidate.DisplayName == body.Name {
				preset = candidate
				break
			}
		}
		if preset.DisplayName == "" {
			err = fmt.Errorf("curated preset %q does not exist", body.Name)
		}
	case "standard":
		preset, err = server.config.Presets.Resolve(request.Context(), body.Name, nil, nil)
	default:
		err = fmt.Errorf("unsupported preset source %q", body.Source)
	}
	if err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "preset_failed", "preset", err)
		return
	}

	server.mu.Lock()
	state, exists := server.drafts[id]
	if exists {
		state.Draft.Preset = preset
		state.Preview = nil
		err = server.config.Application.InitializeNaming(&state.Draft)
	}
	server.mu.Unlock()
	if !exists {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "preset", errors.New("draft does not exist"))
		return
	}
	if err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "naming_initialization_failed", "naming", err)
		return
	}
	server.writeDraft(writer, http.StatusOK, id, state)
}

func (server *Server) setDraftNaming(writer http.ResponseWriter, request *http.Request) {
	var body namingRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "naming", err)
		return
	}
	if strings.TrimSpace(body.Base) == "" || body.StartIndex < 1 {
		server.writeError(writer, http.StatusUnprocessableEntity, "invalid_naming", "naming", errors.New("base is required and start_index must be at least 1"))
		return
	}
	id, state, ok := server.loadDraft(request)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "naming", errors.New("draft does not exist"))
		return
	}
	server.mu.Lock()
	state.Draft.Base = body.Base
	state.Draft.StartIndex = body.StartIndex
	state.Preview = nil
	server.mu.Unlock()
	server.writeDraft(writer, http.StatusOK, id, state)
}

func (server *Server) setDraftChapters(writer http.ResponseWriter, request *http.Request) {
	var body chaptersRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "chapters", err)
		return
	}
	id, state, ok := server.loadDraft(request)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "chapters", errors.New("draft does not exist"))
		return
	}
	server.mu.Lock()
	draft := state.Draft
	server.mu.Unlock()
	interval, err := media.ParseChapterInterval(body.Interval)
	if err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "invalid_interval", "chapters", err)
		return
	}
	selected := append([]int(nil), body.SelectedChapters...)
	tailMerged := false
	if body.Approximate {
		approximation, approximationErr := media.ApproximateStarts(draft.Media.Chapters, draft.Media.Duration, interval)
		if approximationErr != nil {
			server.writeError(writer, http.StatusUnprocessableEntity, "chapter_approximation_failed", "chapters", approximationErr)
			return
		}
		selected = approximation.Starts
		tailMerged = approximation.TailMerged
	}
	finalChapter := len(draft.Media.Chapters)
	if body.ExcludeFinal {
		if finalChapter < 2 {
			server.writeError(writer, http.StatusUnprocessableEntity, "invalid_final_exclusion", "chapters", errors.New("the only chapter cannot be excluded"))
			return
		}
		selected = removeNumber(selected, finalChapter)
		finalChapter--
	}
	if _, err := media.RangesFromStarts(selected, finalChapter); err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "invalid_chapters", "chapters", err)
		return
	}
	sort.Ints(selected)
	server.mu.Lock()
	state.Draft.ChapterInterval = interval
	state.Draft.SelectedChapters = selected
	state.Draft.AutoChapters = body.Approximate
	state.Draft.TailMerged = tailMerged
	state.Draft.ExcludeFinal = body.ExcludeFinal
	state.Preview = nil
	server.mu.Unlock()
	server.writeDraft(writer, http.StatusOK, id, state)
}

func (server *Server) setDraftAudio(writer http.ResponseWriter, request *http.Request) {
	server.setTracks(writer, request, true)
}

func (server *Server) setDraftSubtitles(writer http.ResponseWriter, request *http.Request) {
	server.setTracks(writer, request, false)
}

func (server *Server) setTracks(writer http.ResponseWriter, request *http.Request, audio bool) {
	var body tracksRequest
	stage := "subtitles"
	if audio {
		stage = "audio"
	}
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", stage, err)
		return
	}
	if body.Tracks == nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "invalid_tracks", stage, errors.New("tracks must be a JSON array"))
		return
	}
	id, state, ok := server.loadDraft(request)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", stage, errors.New("draft does not exist"))
		return
	}
	server.mu.Lock()
	candidate := state.Draft
	if audio {
		candidate.AudioTracks = append([]int{}, body.Tracks...)
	} else {
		candidate.Subtitles = append([]int{}, body.Tracks...)
	}
	server.mu.Unlock()
	if _, err := server.config.Application.BuildPreview(candidate); err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "invalid_tracks", stage, err)
		return
	}
	server.mu.Lock()
	state.Draft = candidate
	state.Preview = nil
	server.mu.Unlock()
	server.writeDraft(writer, http.StatusOK, id, state)
}

func (server *Server) buildDraftPreview(writer http.ResponseWriter, request *http.Request) {
	id, state, ok := server.loadDraft(request)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "preview", errors.New("draft does not exist"))
		return
	}
	server.mu.Lock()
	draft := state.Draft
	server.mu.Unlock()
	preview, err := server.config.Application.BuildPreview(draft)
	if err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "preview_failed", "preview", err)
		return
	}
	server.mu.Lock()
	state.Preview = &preview
	server.mu.Unlock()
	server.writeDraft(writer, http.StatusOK, id, state)
}

func (server *Server) addDraftToQueue(writer http.ResponseWriter, request *http.Request) {
	var body addQueueRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "queue-add", err)
		return
	}
	id, state, ok := server.loadDraft(request)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "draft_not_found", "queue-add", errors.New("draft does not exist"))
		return
	}
	server.mu.Lock()
	var preview *app.Preview
	if state.Preview != nil {
		copied := *state.Preview
		preview = &copied
	}
	server.mu.Unlock()
	if preview == nil {
		server.writeError(writer, http.StatusConflict, "preview_required", "queue-add", errors.New("build and confirm a preview before adding it to the queue"))
		return
	}
	queueBeforeAdd, err := server.config.Application.Queue()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "queue_read_failed", "queue-add", err)
		return
	}
	if err := server.config.Application.AddPreview(*preview, body.OverwriteApproved); err != nil {
		server.writeError(writer, http.StatusConflict, "queue_add_failed", "queue-add", err)
		return
	}
	server.config.Controller.QueueChanged()
	var startErr error
	if len(queueBeforeAdd.Jobs) == 0 {
		startErr = server.config.Controller.Start()
	} else {
		startErr = server.config.Controller.StartAutomatically()
	}
	if startErr != nil && startErr.Error() != "queue is empty" {
		snapshot, snapshotErr := server.config.Controller.Snapshot()
		if snapshotErr != nil || !snapshot.QueuePaused {
			server.writeError(writer, http.StatusInternalServerError, "queue_start_failed", "queue-start", startErr)
			return
		}
	}
	server.mu.Lock()
	delete(server.drafts, id)
	server.mu.Unlock()
	q, err := server.config.Application.Queue()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "queue_read_failed", "queue", err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"added": len(preview.Jobs), "queue": q})
}

func (server *Server) loadDraft(request *http.Request) (string, *draftState, bool) {
	id, err := routeID(request)
	if err != nil {
		return "", nil, false
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	state, ok := server.drafts[id]
	return id, state, ok
}

func (server *Server) writeDraft(writer http.ResponseWriter, status int, id string, state *draftState) {
	server.mu.Lock()
	view, err := makeDraftView(id, state)
	server.mu.Unlock()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "draft_view_failed", "draft", err)
		return
	}
	writeJSON(writer, status, view)
}

func makeDraftView(id string, state *draftState) (draftView, error) {
	draft := state.Draft
	durations, err := media.ChapterDurations(draft.Media.Chapters, draft.Media.Duration)
	if err != nil {
		return draftView{}, err
	}
	outputDurations := make([]time.Duration, len(draft.Media.Chapters))
	outputAvailable := make([]bool, len(draft.Media.Chapters))
	finalChapter := len(draft.Media.Chapters)
	if draft.ExcludeFinal {
		finalChapter--
	}
	if len(draft.SelectedChapters) > 0 && finalChapter > 0 {
		outputDurations, outputAvailable, err = media.OutputDurations(
			draft.Media.Chapters, draft.Media.Duration, draft.SelectedChapters, finalChapter,
		)
		if err != nil {
			return draftView{}, err
		}
	}
	chapters := make([]chapterView, len(draft.Media.Chapters))
	for index, chapter := range draft.Media.Chapters {
		chapters[index] = chapterView{
			Number:          chapter.Number,
			StartSeconds:    durationSeconds(chapter.Start),
			DurationSeconds: durationSeconds(durations[index]),
			Title:           chapter.Title,
		}
		if outputAvailable[index] {
			value := durationSeconds(outputDurations[index])
			chapters[index].OutputDurationSeconds = &value
		}
	}
	audioTracks := make([]audioTrackView, len(draft.Media.AudioTracks))
	for index, track := range draft.Media.AudioTracks {
		audioTracks[index] = audioTrackView{
			Number: track.Number, Language: track.Language, Name: track.Name,
			Codec: track.Codec, Channels: track.Channels, SampleRate: track.SampleRate,
		}
	}
	subtitleTracks := make([]subtitleTrackView, len(draft.Media.SubtitleTracks))
	for index, track := range draft.Media.SubtitleTracks {
		subtitleTracks[index] = subtitleTrackView{
			Number: track.Number, Language: track.Language, Name: track.Name, Format: track.Format,
		}
	}
	view := draftView{
		ID: id, Input: draft.Input, DurationSeconds: durationSeconds(draft.Media.Duration),
		Chapters: chapters, AudioTracks: audioTracks, SubtitleTracks: subtitleTracks,
		Base: draft.Base, StartIndex: draft.StartIndex,
		ChapterInterval:   media.FormatChapterInterval(draft.ChapterInterval),
		SelectedChapters:  append([]int{}, draft.SelectedChapters...),
		SelectedAudio:     append([]int{}, draft.AudioTracks...),
		SelectedSubtitles: append([]int{}, draft.Subtitles...),
		AutoChapters:      draft.AutoChapters, TailMerged: draft.TailMerged, ExcludeFinal: draft.ExcludeFinal,
	}
	if draft.Preset.DisplayName != "" {
		preset := viewPreset(draft.Preset)
		view.Preset = &preset
	}
	if state.Preview != nil {
		view.Preview = viewPreview(*state.Preview)
	}
	return view, nil
}

func viewPreset(preset handbrake.Preset) presetView {
	source := "standard"
	if preset.ImportFile != "" || preset.ChapterBrakeOwned {
		source = "curated"
	}
	return presetView{
		Name: preset.DisplayName, Summary: preset.Summary, Container: preset.Container, Source: source,
	}
}

func viewPreview(preview app.Preview) *previewView {
	view := &previewView{
		Jobs:       append([]queue.Job{}, preview.Jobs...),
		Collisions: append([]string{}, preview.Collisions...),
	}
	if preview.Excluded != nil {
		view.Excluded = &chapterRangeView{Start: preview.Excluded.Start, End: preview.Excluded.End}
	}
	if preview.ExcludedFinal != nil {
		view.ExcludedFinal = &chapterRangeView{Start: preview.ExcludedFinal.Start, End: preview.ExcludedFinal.End}
	}
	return view
}

func removeNumber(values []int, remove int) []int {
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value != remove {
			result = append(result, value)
		}
	}
	sort.Ints(result)
	return result
}

func durationSeconds(duration time.Duration) int64 {
	return int64(duration.Round(time.Second) / time.Second)
}
