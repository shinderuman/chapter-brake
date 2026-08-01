package webapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/config"
	"chapterbrake/internal/control"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/media"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"
	"chapterbrake/internal/runstate"
)

type testScanner struct {
	info media.Info
	err  error
}

func (scanner testScanner) Scan(context.Context, string, io.Writer, io.Writer) (media.Info, error) {
	return scanner.info, scanner.err
}

type testPresets struct {
	curated []handbrake.Preset
}

func (presets testPresets) Curated() []handbrake.Preset {
	return append([]handbrake.Preset(nil), presets.curated...)
}

func (presets testPresets) ListStandard(context.Context, io.Writer, io.Writer) ([]handbrake.StandardPreset, error) {
	return []handbrake.StandardPreset{{Category: "General", Name: "Fast 1080p30"}}, nil
}

func (presets testPresets) Resolve(_ context.Context, name string, _ io.Writer, _ io.Writer) (handbrake.Preset, error) {
	return handbrake.Preset{
		DisplayName: name, HandBrakeName: name, Container: queue.ContainerMP4,
	}, nil
}

type testController struct {
	mu             sync.Mutex
	snapshot       control.Snapshot
	changed        chan struct{}
	autoStarts     int
	starts         int
	pauseCalls     int
	resumeCalls    int
	abortCalls     int
	shutdownCalls  int
	dismissed      string
	pauseAfter     bool
	operationError error
}

func newTestController() *testController {
	return &testController{
		snapshot: control.Snapshot{PersistentState: runstate.Idle()},
		changed:  make(chan struct{}),
	}
}

func (controller *testController) Start() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.starts++
	return controller.operationError
}

func (controller *testController) StartAutomatically() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.autoStarts++
	return controller.operationError
}

func (controller *testController) PauseEncoding() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.pauseCalls++
	return controller.operationError
}

func (controller *testController) ResumeEncoding() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.resumeCalls++
	return controller.operationError
}

func (controller *testController) SetPauseAfterCurrent(enabled bool) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.pauseAfter = enabled
	return controller.operationError
}

func (controller *testController) Abort(context.Context) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.abortCalls++
	return controller.operationError
}

func (controller *testController) ShutdownAfterCurrent(context.Context) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.shutdownCalls++
	return controller.operationError
}

func (controller *testController) DismissAlert(id string) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.dismissed = id
	return controller.operationError
}

func (controller *testController) Snapshot() (control.Snapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.snapshot, nil
}

func (controller *testController) Changes() <-chan struct{} {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.changed
}

func (controller *testController) QueueChanged() {
	controller.mu.Lock()
	close(controller.changed)
	controller.changed = make(chan struct{})
	controller.mu.Unlock()
}

func TestDraftWorkflowAndQueueOperations(t *testing.T) {
	handler, service, controller, input := testHandler(t)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	response := requestJSON(t, httpServer.URL, http.MethodGet, "/api/files", nil)
	assertStatus(t, response, http.StatusOK)
	var filesPayload struct {
		Entries []app.FileEntry `json:"entries"`
	}
	decodeResponse(t, response, &filesPayload)
	foundInput := false
	for _, entry := range filesPayload.Entries {
		if entry.Path == input {
			foundInput = true
		}
	}
	if !foundInput {
		t.Fatalf("entries = %#v", filesPayload.Entries)
	}

	response = requestJSON(t, httpServer.URL, http.MethodGet, "/api/presets", nil)
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()
	response = requestJSON(t, httpServer.URL, http.MethodGet, "/api/presets?source=standard", nil)
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()

	response = requestJSON(t, httpServer.URL, http.MethodPost, "/api/drafts", map[string]any{"input": input})
	assertStatus(t, response, http.StatusCreated)
	var draft draftView
	decodeResponse(t, response, &draft)
	if draft.ID == "" || len(draft.Chapters) != 4 || len(draft.AudioTracks) != 2 {
		t.Fatalf("draft = %#v", draft)
	}
	draftPath := "/api/drafts/" + draft.ID
	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/chapters", chaptersRequest{
		Interval: "10:00", SelectedChapters: []int{1, 4}, ExcludeFinal: true,
	})
	assertStatus(t, response, http.StatusOK)
	decodeResponse(t, response, &draft)
	if len(draft.SelectedChapters) != 1 || draft.SelectedChapters[0] != 1 || !draft.ExcludeFinal {
		t.Fatalf("final exclusion draft = %#v", draft)
	}

	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/preset", presetRequest{Name: "My MKV", Source: "curated"})
	assertStatus(t, response, http.StatusOK)
	decodeResponse(t, response, &draft)
	if draft.Preset == nil || draft.Preset.Container != queue.ContainerMKV || draft.StartIndex != 1 {
		t.Fatalf("preset draft = %#v", draft)
	}

	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/naming", namingRequest{Base: "作品名 #1", StartIndex: 7})
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()
	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/chapters", chaptersRequest{
		Interval: "10:00", SelectedChapters: []int{3, 1}, Approximate: false,
	})
	assertStatus(t, response, http.StatusOK)
	decodeResponse(t, response, &draft)
	if len(draft.SelectedChapters) != 2 || draft.SelectedChapters[0] != 1 || draft.SelectedChapters[1] != 3 {
		t.Fatalf("selected chapters = %v", draft.SelectedChapters)
	}

	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/audio", tracksRequest{Tracks: []int{1}})
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()
	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/subtitles", tracksRequest{Tracks: []int{1}})
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()

	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/audio", tracksRequest{Tracks: []int{3}})
	assertStatus(t, response, http.StatusUnprocessableEntity)
	_ = response.Body.Close()
	response = requestJSON(t, httpServer.URL, http.MethodGet, draftPath, nil)
	decodeResponse(t, response, &draft)
	if len(draft.SelectedAudio) != 1 || draft.SelectedAudio[0] != 1 {
		t.Fatalf("invalid update changed selected audio: %v", draft.SelectedAudio)
	}
	response = requestJSON(t, httpServer.URL, http.MethodPut, draftPath+"/subtitles", tracksRequest{Tracks: []int{}})
	assertStatus(t, response, http.StatusOK)
	decodeResponse(t, response, &draft)
	if draft.SelectedSubtitles == nil || len(draft.SelectedSubtitles) != 0 {
		t.Fatalf("empty subtitles must be a JSON array: %#v", draft.SelectedSubtitles)
	}

	response = requestJSON(t, httpServer.URL, http.MethodPost, draftPath+"/preview", map[string]any{})
	assertStatus(t, response, http.StatusOK)
	decodeResponse(t, response, &draft)
	if draft.Preview == nil || len(draft.Preview.Jobs) != 2 {
		t.Fatalf("preview = %#v", draft.Preview)
	}
	firstID := draft.Preview.Jobs[0].ID
	secondID := draft.Preview.Jobs[1].ID

	controller.mu.Lock()
	controller.snapshot.QueuePaused = true
	controller.mu.Unlock()
	response = requestJSON(t, httpServer.URL, http.MethodPost, draftPath+"/queue", addQueueRequest{OverwriteApproved: false})
	assertStatus(t, response, http.StatusCreated)
	_ = response.Body.Close()
	if controller.starts != 1 || controller.autoStarts != 0 {
		t.Fatalf("empty queue starts = %d, automatic starts = %d", controller.starts, controller.autoStarts)
	}
	q, err := service.Queue()
	if err != nil || len(q.Jobs) != 2 {
		t.Fatalf("queue = %#v, %v", q, err)
	}

	response = requestJSON(t, httpServer.URL, http.MethodPost, "/api/queue/"+secondID+"/move", moveRequest{Direction: "up"})
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()
	q, _ = service.Queue()
	if q.Jobs[0].ID != secondID {
		t.Fatalf("queue order = %v", []string{q.Jobs[0].ID, q.Jobs[1].ID})
	}
	position := 1
	response = requestJSON(t, httpServer.URL, http.MethodPost, "/api/queue/"+secondID+"/move", moveRequest{Position: &position})
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()
	q, _ = service.Queue()
	if q.Jobs[1].ID != secondID {
		t.Fatalf("queue position move order = %v", []string{q.Jobs[0].ID, q.Jobs[1].ID})
	}
	response = requestJSON(t, httpServer.URL, http.MethodGet, "/api/queue/"+firstID, nil)
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()
	response = requestJSON(t, httpServer.URL, http.MethodDelete, "/api/queue/"+firstID, nil)
	assertStatus(t, response, http.StatusNoContent)
	_ = response.Body.Close()
	q, _ = service.Queue()
	if len(q.Jobs) != 1 || q.Jobs[0].ID != secondID {
		t.Fatalf("queue after delete = %#v", q)
	}

	response = requestJSON(t, httpServer.URL, http.MethodPost, "/api/drafts", map[string]any{"input": input})
	assertStatus(t, response, http.StatusCreated)
	decodeResponse(t, response, &draft)
	response = requestJSON(t, httpServer.URL, http.MethodDelete, "/api/drafts/"+draft.ID, nil)
	assertStatus(t, response, http.StatusNoContent)
	_ = response.Body.Close()
	response = requestJSON(t, httpServer.URL, http.MethodGet, "/api/drafts/"+draft.ID, nil)
	assertStatus(t, response, http.StatusNotFound)
	_ = response.Body.Close()
}

func TestQueueControlAndStructuredErrors(t *testing.T) {
	handler, _, controller, _ := testHandler(t)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	tests := []struct {
		method string
		path   string
		body   any
		check  func()
	}{
		{http.MethodPost, "/api/queue/start", nil, func() {
			if controller.starts != 1 {
				t.Fatal("start not called")
			}
		}},
		{http.MethodPost, "/api/queue/encoding/pause", nil, func() {
			if controller.pauseCalls != 1 {
				t.Fatal("pause not called")
			}
		}},
		{http.MethodPost, "/api/queue/encoding/resume", nil, func() {
			if controller.resumeCalls != 1 {
				t.Fatal("resume not called")
			}
		}},
		{http.MethodPut, "/api/queue/pause-after-current", pauseAfterRequest{Enabled: true}, func() {
			if !controller.pauseAfter {
				t.Fatal("pause-after not set")
			}
		}},
		{http.MethodPost, "/api/queue/abort", nil, func() {
			if controller.abortCalls != 1 {
				t.Fatal("abort not called")
			}
		}},
		{http.MethodPost, "/api/alerts/job-1/dismiss", nil, func() {
			if controller.dismissed != "job-1" {
				t.Fatal("dismiss not called")
			}
		}},
	}
	for _, test := range tests {
		response := requestJSON(t, httpServer.URL, test.method, test.path, test.body)
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			data, _ := io.ReadAll(response.Body)
			t.Fatalf("%s status = %d: %s", test.path, response.StatusCode, data)
		}
		_ = response.Body.Close()
		test.check()
	}

	response := rawRequest(t, httpServer.URL, http.MethodPost, "/api/drafts", `{"input":"x","unknown":true}`)
	assertStatus(t, response, http.StatusBadRequest)
	var errorPayload struct {
		Error APIError `json:"error"`
	}
	decodeResponse(t, response, &errorPayload)
	if errorPayload.Error.Code != "invalid_json" || errorPayload.Error.Stage != "analyze" {
		t.Fatalf("error = %#v", errorPayload.Error)
	}

	controller.operationError = &runnerJobError
	response = requestJSON(t, httpServer.URL, http.MethodPost, "/api/queue/abort", nil)
	assertStatus(t, response, http.StatusConflict)
	decodeResponse(t, response, &errorPayload)
	if errorPayload.Error.Stage != "handbrake" || errorPayload.Error.LogPath != "/logs/job.log" {
		t.Fatalf("structured job error = %#v", errorPayload.Error)
	}
}

var runnerJobError = runner.JobError{
	JobID: "job-1", Stage: "handbrake", LogPath: "/logs/job.log", Err: errors.New("failed"),
}

func TestSSESnapshotAndLogUpdates(t *testing.T) {
	handler, _, controller, _ := testHandler(t)
	logPath := filepath.Join(t.TempDir(), "job.log")
	if err := os.WriteFile(logPath, []byte("line 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller.snapshot = control.Snapshot{
		Running:         true,
		Current:         &control.Current{JobID: "job", Stage: "handbrake", LogPath: logPath},
		PersistentState: runstate.Idle(),
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	response, err := http.Get(httpServer.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "event: snapshot\n" {
		t.Fatalf("first SSE line = %q, %v", line, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "event: log\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("log event was not received")
		}
	}
	dataLine, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(dataLine, "line 1") {
		t.Fatalf("log data = %q, %v", dataLine, err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(httpServer.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader = bufio.NewReader(response.Body)
	line, err = reader.ReadString('\n')
	if err != nil || line != "event: snapshot\n" {
		t.Fatalf("reconnected SSE first line = %q, %v", line, err)
	}
}

func TestServeUnixSocketAndGracefulShutdown(t *testing.T) {
	_, service, controller, _ := testHandler(t)
	root, err := os.MkdirTemp("/private/tmp", "cbapi-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	server, err := New(Config{
		Socket: filepath.Join(root, "api.sock"), InitialDirectory: t.TempDir(),
		Application: service, Presets: service.Presets, Controller: controller,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	waitForSocket(t, server.config.Socket)
	client := unixClient(server.config.Socket)
	response, err := client.Get("http://unix/api/status")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
	if controller.shutdownCalls != 1 {
		t.Fatalf("shutdown calls = %d", controller.shutdownCalls)
	}
}

func TestNewValidation(t *testing.T) {
	valid := Config{
		Socket: "/tmp/api.sock", InitialDirectory: "/tmp",
		Application: &app.Service{}, Presets: testPresets{}, Controller: newTestController(),
	}
	tests := []Config{
		{},
		func() Config { value := valid; value.Socket = "relative"; return value }(),
		func() Config { value := valid; value.InitialDirectory = "relative"; return value }(),
		func() Config { value := valid; value.Application = nil; return value }(),
		func() Config { value := valid; value.Presets = nil; return value }(),
		func() Config { value := valid; value.Controller = nil; return value }(),
	}
	for index, config := range tests {
		if _, err := New(config); err == nil {
			t.Errorf("config %d accepted", index)
		}
	}
	if _, err := New(valid); err != nil {
		t.Fatal(err)
	}
}

func testHandler(t *testing.T) (http.Handler, *app.Service, *testController, string) {
	t.Helper()
	inputDirectory := t.TempDir()
	input := filepath.Join(inputDirectory, "日本語 #1.mkv")
	if err := os.WriteFile(input, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &queue.Store{Path: filepath.Join(t.TempDir(), "queue.json")}
	if _, err := store.LoadOrCreate(); err != nil {
		t.Fatal(err)
	}
	presetCatalog := testPresets{curated: []handbrake.Preset{{
		DisplayName: "My MKV", Summary: "1080p", HandBrakeName: "H.264 MKV 1080p30",
		Container: queue.ContainerMKV, ChapterBrakeOwned: true,
	}}}
	service := &app.Service{
		QueueStore: store,
		Scanner: testScanner{info: media.Info{
			Duration: 40 * time.Minute,
			Chapters: []media.Chapter{
				{Number: 1, Start: 0, Title: "Chapter 1"},
				{Number: 2, Start: 10 * time.Minute, Title: "Chapter 2"},
				{Number: 3, Start: 20 * time.Minute, Title: "Chapter 3"},
				{Number: 4, Start: 30 * time.Minute, Title: "Chapter 4"},
			},
			AudioTracks:    []media.AudioTrack{{Number: 1, Language: "jpn"}, {Number: 2, Language: "eng"}},
			SubtitleTracks: []media.SubtitleTrack{{Number: 1, Language: "jpn", Format: "PGS"}},
		}},
		Presets: presetCatalog, OutputDirectory: t.TempDir(),
		ChapterInterval: 10 * time.Minute, Now: func() time.Time {
			return time.Date(2026, 7, 29, 12, 0, 0, 0, time.Local)
		}, InputDirectory: inputDirectory,
		SettingsStore: config.Store{Path: filepath.Join(t.TempDir(), "settings.json")},
	}
	controller := newTestController()
	server, err := New(Config{
		Socket: filepath.Join(t.TempDir(), "api.sock"), InitialDirectory: inputDirectory,
		Application: service, Presets: presetCatalog, Controller: controller,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler(), service, controller, input
}

func TestSettingsAPI(t *testing.T) {
	handler, service, _, _ := testHandler(t)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	response := requestJSON(t, httpServer.URL, http.MethodGet, "/api/settings", nil)
	assertStatus(t, response, http.StatusOK)
	var initial config.Settings
	if err := json.NewDecoder(response.Body).Decode(&initial); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	output := filepath.Join(t.TempDir(), "future-output")
	updated := map[string]any{
		"input_directory":  initial.InputDirectory,
		"output_directory": output,
		"chapter_interval": "25:00",
	}
	response = requestJSON(t, httpServer.URL, http.MethodPut, "/api/settings", updated)
	assertStatus(t, response, http.StatusOK)
	_ = response.Body.Close()
	if got := service.CurrentSettings(); got.OutputDirectory != output || got.ChapterInterval != "25:00" {
		t.Fatalf("settings after update = %#v", got)
	}

	updated["input_directory"] = filepath.Join(t.TempDir(), "missing")
	response = requestJSON(t, httpServer.URL, http.MethodPut, "/api/settings", updated)
	assertStatus(t, response, http.StatusUnprocessableEntity)
	_ = response.Body.Close()
}

func TestAnalysisProgress(t *testing.T) {
	server := &Server{analyses: make(map[string]float64)}
	server.setAnalysisProgress("analysis-123", 0.42)
	request := httptest.NewRequest(http.MethodGet, "/api/analysis-progress/analysis-123", nil)
	request.SetPathValue("id", "analysis-123")
	recorder := httptest.NewRecorder()
	server.getAnalysisProgress(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Progress float64 `json:"progress"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Progress != 0.42 {
		t.Fatalf("progress = %v", payload.Progress)
	}

	server.scheduleAnalysisProgressClear("analysis-123")
	if _, exists := server.analyses["analysis-123"]; !exists {
		t.Fatal("scheduled progress was cleared before the client could stop polling")
	}
	server.clearAnalysisProgress("analysis-123")
	recorder = httptest.NewRecorder()
	server.getAnalysisProgress(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("cleared status = %d", recorder.Code)
	}
}

func requestJSON(t *testing.T, baseURL, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func rawRequest(t *testing.T, baseURL, method, path, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertStatus(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode != want {
		data, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		t.Fatalf("status = %d, want %d: %s", response.StatusCode, want, data)
	}
}

func decodeResponse(t *testing.T, response *http.Response, destination any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket was not created")
}

func unixClient(socket string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	return &http.Client{Transport: transport}
}
