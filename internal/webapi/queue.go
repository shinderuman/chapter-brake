package webapi

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type moveRequest struct {
	Direction string `json:"direction"`
	Position  *int   `json:"position"`
}

type pauseAfterRequest struct {
	Enabled bool `json:"enabled"`
}

func (server *Server) getQueue(writer http.ResponseWriter, _ *http.Request) {
	q, err := server.config.Application.Queue()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "queue_read_failed", "queue", err)
		return
	}
	snapshot, err := server.config.Controller.Snapshot()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "status_failed", "queue", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"queue": q, "runtime": snapshot})
}

func (server *Server) getQueueJob(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request)
	if err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_id", "queue-detail", err)
		return
	}
	q, err := server.config.Application.Queue()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "queue_read_failed", "queue-detail", err)
		return
	}
	job, ok := findJob(q, id)
	if !ok {
		server.writeError(writer, http.StatusNotFound, "job_not_found", "queue-detail", errors.New("queue job does not exist"))
		return
	}
	snapshot, err := server.config.Controller.Snapshot()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "status_failed", "queue-detail", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"job": job, "runtime": snapshot})
}

func (server *Server) deleteQueueJob(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request)
	if err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_id", "queue-delete", err)
		return
	}
	if err := server.config.Application.DeleteQueuedJob(id); err != nil {
		server.writeError(writer, http.StatusConflict, "queue_delete_failed", "queue-delete", err)
		return
	}
	server.config.Controller.QueueChanged()
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) moveQueueJob(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request)
	if err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_id", "queue-move", err)
		return
	}
	var body moveRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "queue-move", err)
		return
	}
	if body.Position != nil {
		if body.Direction != "" {
			server.writeError(writer, http.StatusUnprocessableEntity, "invalid_move", "queue-move", errors.New("specify either direction or position"))
			return
		}
		if err := server.config.Application.MoveQueuedJobTo(id, *body.Position); err != nil {
			server.writeError(writer, http.StatusConflict, "queue_move_failed", "queue-move", err)
			return
		}
		server.config.Controller.QueueChanged()
		server.getQueue(writer, request)
		return
	}
	delta := 0
	switch body.Direction {
	case "up":
		delta = -1
	case "down":
		delta = 1
	default:
		server.writeError(writer, http.StatusUnprocessableEntity, "invalid_direction", "queue-move", errors.New("direction must be up or down"))
		return
	}
	if err := server.config.Application.MoveQueuedJob(id, delta); err != nil {
		server.writeError(writer, http.StatusConflict, "queue_move_failed", "queue-move", err)
		return
	}
	server.config.Controller.QueueChanged()
	server.getQueue(writer, request)
}

func (server *Server) startQueue(writer http.ResponseWriter, _ *http.Request) {
	if err := server.config.Controller.Start(); err != nil {
		server.writeError(writer, http.StatusConflict, "queue_start_failed", "queue-start", err)
		return
	}
	server.writeRuntime(writer, http.StatusAccepted)
}

func (server *Server) pauseEncoding(writer http.ResponseWriter, _ *http.Request) {
	if err := server.config.Controller.PauseEncoding(); err != nil {
		server.writeError(writer, http.StatusConflict, "encoding_pause_failed", "encoding-pause", err)
		return
	}
	server.writeRuntime(writer, http.StatusOK)
}

func (server *Server) resumeEncoding(writer http.ResponseWriter, _ *http.Request) {
	if err := server.config.Controller.ResumeEncoding(); err != nil {
		server.writeError(writer, http.StatusConflict, "encoding_resume_failed", "encoding-resume", err)
		return
	}
	server.writeRuntime(writer, http.StatusOK)
}

func (server *Server) pauseAfterCurrent(writer http.ResponseWriter, request *http.Request) {
	var body pauseAfterRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "pause-after-current", err)
		return
	}
	if err := server.config.Controller.SetPauseAfterCurrent(body.Enabled); err != nil {
		server.writeError(writer, http.StatusConflict, "pause_after_current_failed", "pause-after-current", err)
		return
	}
	server.writeRuntime(writer, http.StatusOK)
}

func (server *Server) abortQueue(writer http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.config.Controller.Abort(ctx); err != nil {
		server.writeError(writer, http.StatusConflict, "queue_abort_failed", "queue-abort", err)
		return
	}
	server.writeRuntime(writer, http.StatusOK)
}

func (server *Server) dismissAlert(writer http.ResponseWriter, request *http.Request) {
	id, err := routeID(request)
	if err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_id", "alert-dismiss", err)
		return
	}
	if err := server.config.Controller.DismissAlert(id); err != nil {
		server.writeError(writer, http.StatusConflict, "alert_dismiss_failed", "alert-dismiss", err)
		return
	}
	server.writeRuntime(writer, http.StatusOK)
}

func (server *Server) writeRuntime(writer http.ResponseWriter, status int) {
	snapshot, err := server.config.Controller.Snapshot()
	if err != nil {
		server.writeError(writer, http.StatusInternalServerError, "status_failed", "queue", err)
		return
	}
	writeJSON(writer, status, snapshot)
}
