package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	apps []*InstalledApp

	mu       sync.RWMutex
	commands map[string]*exec.Cmd
	cancel   context.CancelFunc
}

func New(apps []*InstalledApp) (*Server, error) {
	if len(apps) == 0 {
		return nil, fmt.Errorf("at least one app is required")
	}
	return &Server{apps: apps, commands: make(map[string]*exec.Cmd)}, nil
}

func (s *Server) StartBackends(ctx context.Context) error {
	runContext, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	for _, app := range s.apps {
		if err := s.startBackend(runContext, app); err != nil {
			s.Close()
			return err
		}
	}
	return nil
}

func (s *Server) startBackend(ctx context.Context, app *InstalledApp) error {
	if err := os.Remove(app.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket for %s: %w", app.Manifest.ID, err)
	}
	command := exec.Command(app.Backend, app.Manifest.Backend.Args...)
	command.Env = append(os.Environ(), "LOCAL_WEB_SOCKET="+app.Socket)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start backend %s: %w", app.Manifest.ID, err)
	}
	s.mu.Lock()
	s.commands[app.Manifest.ID] = command
	s.mu.Unlock()
	go func() {
		err := command.Wait()
		s.mu.Lock()
		delete(s.commands, app.Manifest.ID)
		app.available = false
		if err != nil && ctx.Err() == nil {
			app.lastError = err.Error()
		}
		s.mu.Unlock()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, err := net.DialTimeout("unix", app.Socket, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			s.mu.Lock()
			app.available = true
			app.lastError = ""
			s.mu.Unlock()
			return nil
		}
		s.mu.RLock()
		_, running := s.commands[app.Manifest.ID]
		s.mu.RUnlock()
		if !running {
			return fmt.Errorf("backend %s exited before socket became ready: %s", app.Manifest.ID, app.lastError)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("backend %s did not create socket %s", app.Manifest.ID, app.Socket)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (s *Server) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.RLock()
	commands := make([]*exec.Cmd, 0, len(s.commands))
	for _, command := range s.commands {
		commands = append(commands, command)
	}
	s.mu.RUnlock()
	for _, command := range commands {
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		remaining := len(s.commands)
		s.mu.RUnlock()
		if remaining == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.mu.RLock()
	remaining := make([]*exec.Cmd, 0, len(s.commands))
	for _, command := range s.commands {
		remaining = append(remaining, command)
	}
	s.mu.RUnlock()
	for _, command := range remaining {
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
	}
	for _, app := range s.apps {
		_ = os.Remove(app.Socket)
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/" {
		s.serveIndex(writer)
		return
	}
	if request.URL.Path == "/_local/status" {
		s.serveStatus(writer)
		return
	}
	app, remainder, ok := s.matchApp(request.URL.Path)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	if remainder == "" {
		if !strings.HasSuffix(request.URL.Path, "/") {
			http.Redirect(writer, request, request.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
	}
	if remainder == "api" || strings.HasPrefix(remainder, "api/") {
		s.proxyAPI(app, "/"+remainder, writer, request)
		return
	}
	prefix := "/apps/" + app.Manifest.ID + "/"
	http.StripPrefix(prefix, http.FileServer(http.Dir(app.WebRoot))).ServeHTTP(writer, request)
}

func (s *Server) matchApp(requestPath string) (*InstalledApp, string, bool) {
	clean := strings.TrimPrefix(path.Clean(requestPath), "/")
	parts := strings.SplitN(clean, "/", 3)
	if len(parts) < 2 || parts[0] != "apps" {
		return nil, "", false
	}
	for _, app := range s.apps {
		if app.Manifest.ID == parts[1] {
			remainder := ""
			if len(parts) == 3 {
				remainder = parts[2]
			}
			return app, remainder, true
		}
	}
	return nil, "", false
}

func (s *Server) proxyAPI(app *InstalledApp, backendPath string, writer http.ResponseWriter, request *http.Request) {
	s.mu.RLock()
	available := app.available
	lastError := app.lastError
	s.mu.RUnlock()
	if !available {
		writeJSON(writer, http.StatusBadGateway, map[string]any{
			"error":  "backend unavailable",
			"detail": lastError,
		})
		return
	}
	target := &url.URL{Scheme: "http", Host: "local-backend"}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			proxyRequest.SetURL(target)
			proxyRequest.Out.URL.Path = backendPath
			proxyRequest.Out.URL.RawPath = ""
			proxyRequest.Out.Host = "local-backend"
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", app.Socket)
			},
		},
		FlushInterval: -1,
		ErrorHandler: func(response http.ResponseWriter, _ *http.Request, err error) {
			writeJSON(response, http.StatusBadGateway, map[string]any{
				"error":  "backend proxy failed",
				"detail": err.Error(),
			})
		},
	}
	proxy.ServeHTTP(writer, request)
}

func (s *Server) serveStatus(writer http.ResponseWriter) {
	type appStatus struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Available bool   `json:"available"`
		Error     string `json:"error,omitempty"`
	}
	statuses := make([]appStatus, 0, len(s.apps))
	s.mu.RLock()
	for _, app := range s.apps {
		statuses = append(statuses, appStatus{
			ID:        app.Manifest.ID,
			Name:      app.Manifest.Name,
			Available: app.available,
			Error:     app.lastError,
		})
	}
	s.mu.RUnlock()
	writeJSON(writer, http.StatusOK, map[string]any{"apps": statuses})
}

func (s *Server) serveIndex(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(writer, `<!doctype html>
<html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Local Web Apps</title><style>
:root{color-scheme:dark;font-family:system-ui,sans-serif;background:#101318;color:#f4f7fb}
body{max-width:920px;margin:0 auto;padding:48px 24px}h1{font-size:30px}.apps{display:grid;gap:16px}
a{display:block;padding:22px;border:1px solid #303844;border-radius:14px;background:#1b222c;color:inherit;text-decoration:none}
a:hover{border-color:#7aa2f7}.state{color:#9cabc0;font-size:13px}
</style></head><body><h1>Local Web Apps</h1><div class="apps">`)
	s.mu.RLock()
	for _, app := range s.apps {
		state := "backend unavailable"
		if app.available {
			state = "backend ready"
		}
		_, _ = fmt.Fprintf(
			writer,
			`<a href="/apps/%s/"><strong>%s</strong><div class="state">%s</div></a>`,
			app.Manifest.ID,
			html.EscapeString(app.Manifest.Name),
			state,
		)
	}
	s.mu.RUnlock()
	_, _ = io.WriteString(writer, `</div></body></html>`)
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
