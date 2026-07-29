package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	Socket       string
	QueuePath    string
	StatePath    string
	CancelMarker string
	Exit         func(int)
}

type Server struct {
	config Config

	mu         sync.Mutex
	httpServer *http.Server
}

func New(config Config) (*Server, error) {
	if !filepath.IsAbs(config.Socket) {
		return nil, fmt.Errorf("socket must be absolute")
	}
	if !filepath.IsAbs(config.QueuePath) {
		return nil, fmt.Errorf("queue path must be absolute")
	}
	if !filepath.IsAbs(config.StatePath) {
		return nil, fmt.Errorf("state path must be absolute")
	}
	if config.Exit == nil {
		config.Exit = os.Exit
	}
	return &Server{config: config}, nil
}

func (s *Server) Serve(ctx context.Context) error {
	if err := os.Remove(s.config.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.config.Socket), 0o700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	listener, err := net.Listen("unix", s.config.Socket)
	if err != nil {
		return fmt.Errorf("listen on socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(s.config.Socket)
	if err := os.Chmod(s.config.Socket, 0o600); err != nil {
		return fmt.Errorf("protect socket: %w", err)
	}
	httpServer := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.mu.Lock()
	s.httpServer = httpServer
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		done <- httpServer.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownContext)
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/queue", s.queue)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/echo", s.echo)
	mux.HandleFunc("GET /api/slow", s.slow)
	mux.HandleFunc("POST /api/exit", s.exit)
	return mux
}

func (s *Server) status(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"ready":      true,
		"queue_path": s.config.QueuePath,
		"state_path": s.config.StatePath,
	})
}

func (s *Server) queue(writer http.ResponseWriter, _ *http.Request) {
	value, err := readJSONFile(s.config.QueuePath)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"queue": value})
}

func (s *Server) state(writer http.ResponseWriter, _ *http.Request) {
	value, err := readJSONFile(s.config.StatePath)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"state": value})
}

func (s *Server) events(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	lastDigest := ""
	ticker := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	sendSnapshot := func() bool {
		queueValue, queueErr := readJSONFile(s.config.QueuePath)
		stateValue, stateErr := readJSONFile(s.config.StatePath)
		payload := map[string]any{
			"queue": queueValue,
			"state": stateValue,
		}
		if queueErr != nil {
			payload["queue_error"] = queueErr.Error()
		}
		if stateErr != nil {
			payload["state_error"] = stateErr.Error()
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		digestBytes := sha256.Sum256(data)
		digest := hex.EncodeToString(digestBytes[:])
		if digest == lastDigest {
			return true
		}
		lastDigest = digest
		_, _ = fmt.Fprintf(writer, "event: snapshot\ndata: %s\n\n", data)
		flusher.Flush()
		return true
	}
	sendSnapshot()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
			sendSnapshot()
		case <-heartbeat.C:
			_, _ = io.WriteString(writer, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) echo(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"path": request.URL.Query().Get("path")})
}

func (s *Server) slow(writer http.ResponseWriter, request *http.Request) {
	select {
	case <-request.Context().Done():
		if s.config.CancelMarker != "" {
			_ = os.WriteFile(s.config.CancelMarker, []byte("canceled"), 0o600)
		}
	case <-time.After(30 * time.Second):
		writeJSON(writer, http.StatusOK, map[string]bool{"completed": true})
	}
}

func (s *Server) exit(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusAccepted, map[string]bool{"exiting": true})
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.config.Exit(23)
	}()
}

func readJSONFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return value, nil
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
