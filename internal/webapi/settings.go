package webapi

import (
	"net/http"

	"chapterbrake/internal/config"
)

type settingsRequest struct {
	InputDirectory  string `json:"input_directory"`
	OutputDirectory string `json:"output_directory"`
	ChapterInterval string `json:"chapter_interval"`
}

func (server *Server) getSettings(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.config.Application.CurrentSettings())
}

func (server *Server) putSettings(writer http.ResponseWriter, request *http.Request) {
	var body settingsRequest
	if err := decodeJSON(writer, request, &body); err != nil {
		server.writeError(writer, http.StatusBadRequest, "invalid_json", "settings", err)
		return
	}
	settings := config.Settings{
		Version:         config.Version,
		InputDirectory:  body.InputDirectory,
		OutputDirectory: body.OutputDirectory,
		ChapterInterval: body.ChapterInterval,
	}
	if err := server.config.Application.UpdateSettings(settings); err != nil {
		server.writeError(writer, http.StatusUnprocessableEntity, "invalid_settings", "settings", err)
		return
	}
	server.mu.Lock()
	server.config.InitialDirectory = settings.InputDirectory
	server.mu.Unlock()
	writeJSON(writer, http.StatusOK, settings)
}
