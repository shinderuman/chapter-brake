package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaticJSONSSEAndCancellationProxy(t *testing.T) {
	root := shortTempDir(t)
	webRoot := filepath.Join(root, "web")
	if err := os.Mkdir(webRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("ChapterBrake PoC"), 0o600); err != nil {
		t.Fatal(err)
	}
	specialName := "日本語 空白 #記号.txt"
	if err := os.WriteFile(filepath.Join(webRoot, specialName), []byte("special-ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "backend.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	cancelObserved := make(chan struct{}, 1)
	backend := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/json":
			writeJSON(writer, http.StatusOK, map[string]string{"path": request.URL.Query().Get("path")})
		case "/api/events":
			writer.Header().Set("Content-Type", "text/event-stream")
			flusher := writer.(http.Flusher)
			_, _ = io.WriteString(writer, "event: snapshot\ndata: {\"ok\":true}\n\n")
			flusher.Flush()
		case "/api/slow":
			<-request.Context().Done()
			cancelObserved <- struct{}{}
		default:
			http.NotFound(writer, request)
		}
	})}
	go backend.Serve(listener)
	t.Cleanup(func() {
		_ = backend.Close()
		_ = listener.Close()
	})

	app := &InstalledApp{
		Manifest:  Manifest{SchemaVersion: 1, ID: "chapter-brake", Name: "ChapterBrake", WebRoot: "web", Backend: Backend{Executable: "bin/backend"}},
		Root:      root,
		WebRoot:   webRoot,
		Socket:    socket,
		available: true,
	}
	localServer, err := New([]*InstalledApp{app})
	if err != nil {
		t.Fatal(err)
	}
	client, baseURL, closeFront := serveUnixHTTP(t, filepath.Join(root, "front.sock"), localServer.Handler())
	defer closeFront()

	response, err := client.Get(baseURL + "/apps/chapter-brake/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if !strings.Contains(string(body), "ChapterBrake PoC") {
		t.Fatalf("static body = %q", body)
	}
	response, err = client.Get(baseURL + "/apps/chapter-brake/" + url.PathEscape(specialName))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "special-ok" {
		t.Fatalf("special body = %q", body)
	}

	specialPath := "/Volumes/動画 HDD/記号 #1 & test.mkv"
	response, err = client.Get(baseURL + "/apps/chapter-brake/api/json?path=" + url.QueryEscape(specialPath))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if payload["path"] != specialPath {
		t.Fatalf("proxied path = %q", payload["path"])
	}

	response, err = client.Get(baseURL + "/apps/chapter-brake/api/events")
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	_ = response.Body.Close()
	if err != nil || line != "event: snapshot\n" {
		t.Fatalf("SSE first line = %q, %v", line, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/apps/chapter-brake/api/slow", nil)
	result := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-cancelObserved:
	case <-time.After(2 * time.Second):
		t.Fatal("backend did not observe proxy request cancellation")
	}
	if err := <-result; err == nil {
		t.Fatal("canceled client request error = nil")
	}
}

func TestUnavailableBackendIsVisible(t *testing.T) {
	app := &InstalledApp{
		Manifest:  Manifest{SchemaVersion: 1, ID: "chapter-brake", Name: "ChapterBrake", WebRoot: "web", Backend: Backend{Executable: "bin/backend"}},
		WebRoot:   t.TempDir(),
		lastError: "exit status 23",
	}
	localServer, err := New([]*InstalledApp{app})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/apps/chapter-brake/api/status", nil)
	recorder := httptest.NewRecorder()
	localServer.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.Bytes()
	if !strings.Contains(string(body), "backend unavailable") {
		t.Fatalf("body = %q", body)
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/private/tmp", "cbpoc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func serveUnixHTTP(t *testing.T, socket string, handler http.Handler) (*http.Client, string, func()) {
	t.Helper()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		},
	}
	client := &http.Client{Transport: transport}
	closeServer := func() {
		transport.CloseIdleConnections()
		_ = server.Close()
		_ = listener.Close()
	}
	return client, "http://unix", closeServer
}

func ExampleServer_Handler() {
	fmt.Println("generic routing only")
	// Output: generic routing only
}
