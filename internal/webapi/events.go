package webapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type eventSnapshot struct {
	Queue   any       `json:"queue,omitempty"`
	Runtime any       `json:"runtime,omitempty"`
	Error   *APIError `json:"error,omitempty"`
}

type logEvent struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	Text   string `json:"text"`
}

func (server *Server) events(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		server.writeError(writer, http.StatusInternalServerError, "streaming_unsupported", "events", fmt.Errorf("streaming is unsupported"))
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	var logPath string
	var logOffset int64
	sendSnapshot := func() bool {
		q, queueErr := server.config.Application.Queue()
		runtime, runtimeErr := server.config.Controller.Snapshot()
		payload := eventSnapshot{Queue: q, Runtime: runtime}
		if queueErr != nil {
			payload.Error = &APIError{Code: "queue_read_failed", Stage: "events", Message: queueErr.Error()}
			payload.Queue = nil
		} else if runtimeErr != nil {
			payload.Error = &APIError{Code: "status_failed", Stage: "events", Message: runtimeErr.Error()}
			payload.Runtime = nil
		}
		if runtime.Current != nil && runtime.Current.LogPath != logPath {
			logPath = runtime.Current.LogPath
			logOffset = 0
		}
		if !writeSSE(writer, "snapshot", payload) {
			return false
		}
		flusher.Flush()
		return true
	}
	sendLog := func() bool {
		if logPath == "" {
			return true
		}
		file, err := os.Open(logPath)
		if err != nil {
			return true
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return true
		}
		if info.Size() < logOffset {
			logOffset = 0
		}
		if info.Size() == logOffset {
			return true
		}
		if _, err := file.Seek(logOffset, io.SeekStart); err != nil {
			return true
		}
		data, err := io.ReadAll(io.LimitReader(file, 256<<10))
		if err != nil || len(data) == 0 {
			return true
		}
		event := logEvent{Path: logPath, Offset: logOffset, Text: string(data)}
		logOffset += int64(len(data))
		if !writeSSE(writer, "log", event) {
			return false
		}
		flusher.Flush()
		return true
	}

	if !sendSnapshot() {
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	heartbeat := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		changed := server.config.Controller.Changes()
		select {
		case <-request.Context().Done():
			return
		case <-changed:
			if !sendSnapshot() || !sendLog() {
				return
			}
		case <-ticker.C:
			if !sendLog() {
				return
			}
		case <-heartbeat.C:
			if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(writer io.Writer, event string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
	return err == nil
}
