package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"chapterbrake/internal/app"
	"chapterbrake/internal/config"
	"chapterbrake/internal/control"
	"chapterbrake/internal/handbrake"
	"chapterbrake/internal/queue"
	"chapterbrake/internal/runner"
)

const requestBodyLimit = 1 << 20

type Application interface {
	Analyze(context.Context, string) (app.Draft, error)
	InitializeNaming(*app.Draft) error
	BuildPreview(app.Draft) (app.Preview, error)
	AddPreview(app.Preview, bool) error
	Queue() (queue.Queue, error)
	DeleteQueuedJob(string) error
	MoveQueuedJob(string, int) error
	MoveQueuedJobTo(string, int) error
	CurrentSettings() config.Settings
	UpdateSettings(config.Settings) error
}

type PresetCatalog interface {
	Curated() []handbrake.Preset
	ListStandard(context.Context, io.Writer, io.Writer) ([]handbrake.StandardPreset, error)
	Resolve(context.Context, string, io.Writer, io.Writer) (handbrake.Preset, error)
}

type QueueController interface {
	Start() error
	StartAutomatically() error
	PauseEncoding() error
	ResumeEncoding() error
	SetPauseAfterCurrent(bool) error
	Abort(context.Context) error
	ShutdownAfterCurrent(context.Context) error
	DismissAlert(string) error
	Snapshot() (control.Snapshot, error)
	Changes() <-chan struct{}
	QueueChanged()
}

type Config struct {
	Socket           string
	InitialDirectory string
	Application      Application
	Presets          PresetCatalog
	Controller       QueueController
	Logger           *slog.Logger
}

type Server struct {
	config Config

	mu       sync.Mutex
	drafts   map[string]*draftState
	analyses map[string]float64
	sequence atomic.Uint64
}

type draftState struct {
	Draft   app.Draft
	Preview *app.Preview
}

type APIError struct {
	Code    string `json:"code"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
	LogPath string `json:"log_path,omitempty"`
}

func New(config Config) (*Server, error) {
	if !filepath.IsAbs(config.Socket) {
		return nil, errors.New("socket must be absolute")
	}
	if !filepath.IsAbs(config.InitialDirectory) {
		return nil, errors.New("initial directory must be absolute")
	}
	if config.Application == nil {
		return nil, errors.New("application service is nil")
	}
	if config.Presets == nil {
		return nil, errors.New("preset catalog is nil")
	}
	if config.Controller == nil {
		return nil, errors.New("queue controller is nil")
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		config:   config,
		drafts:   make(map[string]*draftState),
		analyses: make(map[string]float64),
	}, nil
}

func (server *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(server.config.Socket), 0o700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.Remove(server.config.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	listener, err := net.Listen("unix", server.config.Socket)
	if err != nil {
		return fmt.Errorf("listen on Unix socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(server.config.Socket)
	if err := os.Chmod(server.config.Socket, 0o600); err != nil {
		return fmt.Errorf("protect Unix socket: %w", err)
	}

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- httpServer.Serve(listener)
	}()
	select {
	case err := <-serveResult:
		queueErr := server.config.Controller.ShutdownAfterCurrent(context.Background())
		if errors.Is(err, http.ErrServerClosed) {
			return queueErr
		}
		return errors.Join(err, queueErr)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		httpErr := httpServer.Shutdown(shutdownContext)
		cancel()
		queueErr := server.config.Controller.ShutdownAfterCurrent(context.Background())
		return errors.Join(httpErr, queueErr)
	}
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", server.status)
	mux.HandleFunc("GET /api/settings", server.getSettings)
	mux.HandleFunc("PUT /api/settings", server.putSettings)
	mux.HandleFunc("GET /api/files", server.files)
	mux.HandleFunc("GET /api/presets", server.presets)
	mux.HandleFunc("POST /api/drafts", server.createDraft)
	mux.HandleFunc("GET /api/analysis-progress/{id}", server.getAnalysisProgress)
	mux.HandleFunc("GET /api/drafts/{id}", server.getDraft)
	mux.HandleFunc("DELETE /api/drafts/{id}", server.deleteDraft)
	mux.HandleFunc("PUT /api/drafts/{id}/preset", server.setDraftPreset)
	mux.HandleFunc("PUT /api/drafts/{id}/naming", server.setDraftNaming)
	mux.HandleFunc("PUT /api/drafts/{id}/chapters", server.setDraftChapters)
	mux.HandleFunc("PUT /api/drafts/{id}/audio", server.setDraftAudio)
	mux.HandleFunc("PUT /api/drafts/{id}/subtitles", server.setDraftSubtitles)
	mux.HandleFunc("POST /api/drafts/{id}/preview", server.buildDraftPreview)
	mux.HandleFunc("POST /api/drafts/{id}/queue", server.addDraftToQueue)
	mux.HandleFunc("GET /api/queue", server.getQueue)
	mux.HandleFunc("GET /api/queue/{id}", server.getQueueJob)
	mux.HandleFunc("DELETE /api/queue/{id}", server.deleteQueueJob)
	mux.HandleFunc("POST /api/queue/{id}/move", server.moveQueueJob)
	mux.HandleFunc("POST /api/queue/start", server.startQueue)
	mux.HandleFunc("POST /api/queue/encoding/pause", server.pauseEncoding)
	mux.HandleFunc("POST /api/queue/encoding/resume", server.resumeEncoding)
	mux.HandleFunc("PUT /api/queue/pause-after-current", server.pauseAfterCurrent)
	mux.HandleFunc("POST /api/queue/abort", server.abortQueue)
	mux.HandleFunc("POST /api/alerts/{id}/dismiss", server.dismissAlert)
	mux.HandleFunc("GET /api/events", server.events)
	return recoverPanics(server.config.Logger, mux)
}

func (server *Server) status(writer http.ResponseWriter, _ *http.Request) {
	snapshot, err := server.config.Controller.Snapshot()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "status_failed", "status", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"ready":             true,
		"initial_directory": server.initialDirectory(),
		"queue":             snapshot,
	})
}

func (server *Server) files(writer http.ResponseWriter, request *http.Request) {
	directory := request.URL.Query().Get("directory")
	if directory == "" {
		directory = server.initialDirectory()
	}
	entries, err := app.ListInputEntries(directory)
	if err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_directory", "files", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"directory": directory,
		"entries":   entries,
	})
}

func (server *Server) initialDirectory() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.config.InitialDirectory
}

func (server *Server) writeError(writer http.ResponseWriter, status int, code, stage string, err error) {
	apiError := APIError{Code: code, Stage: stage, Message: err.Error()}
	var jobError *runner.JobError
	if errors.As(err, &jobError) {
		apiError.Stage = jobError.Stage
		apiError.LogPath = jobError.LogPath
	}
	writeJSON(writer, status, map[string]APIError{"error": apiError})
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, requestBodyLimit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body contains multiple JSON values")
		}
		return err
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("API panic", "path", request.URL.Path, "panic", recovered)
				writeJSON(writer, http.StatusInternalServerError, map[string]APIError{
					"error": {
						Code:    "internal_error",
						Stage:   "http",
						Message: "internal server error",
					},
				})
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func findJob(q queue.Queue, id string) (queue.Job, bool) {
	for _, job := range q.Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return queue.Job{}, false
}

func routeID(request *http.Request) (string, error) {
	id := strings.TrimSpace(request.PathValue("id"))
	if id == "" {
		return "", errors.New("id is required")
	}
	return id, nil
}
