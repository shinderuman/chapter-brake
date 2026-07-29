package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestReadOnlyEndpointsAndSSE(t *testing.T) {
	root := shortTempDir(t)
	queuePath := filepath.Join(root, "キュー queue.json")
	statePath := filepath.Join(root, "状態 #1.json")
	queueData := []byte(`{"version":1,"jobs":[]}`)
	stateData := []byte(`{"version":1,"status":"idle"}`)
	if err := os.WriteFile(queuePath, queueData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateData, 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(Config{
		Socket:    filepath.Join(root, "backend.sock"),
		QueuePath: queuePath,
		StatePath: statePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, baseURL, closeServer := serveUnixHTTP(t, filepath.Join(root, "http.sock"), server.Handler())
	defer closeServer()

	response, err := client.Get(baseURL + "/api/queue")
	if err != nil {
		t.Fatal(err)
	}
	var queuePayload map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&queuePayload); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	var gotQueue any
	var wantQueue any
	if err := json.Unmarshal(queuePayload["queue"], &gotQueue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(queueData, &wantQueue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotQueue, wantQueue) {
		t.Fatalf("queue = %#v, want %#v", gotQueue, wantQueue)
	}

	response, err = client.Get(baseURL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	_ = response.Body.Close()
	if err != nil || line != "event: snapshot\n" {
		t.Fatalf("SSE first line = %q, %v", line, err)
	}
}

func TestSlowEndpointObservesCancellation(t *testing.T) {
	root := shortTempDir(t)
	queuePath := filepath.Join(root, "queue.json")
	statePath := filepath.Join(root, "state.json")
	marker := filepath.Join(root, "cancel-marker")
	_ = os.WriteFile(queuePath, []byte(`{}`), 0o600)
	_ = os.WriteFile(statePath, []byte(`{}`), 0o600)
	server, err := New(Config{
		Socket:       filepath.Join(root, "backend.sock"),
		QueuePath:    queuePath,
		StatePath:    statePath,
		CancelMarker: marker,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, baseURL, closeServer := serveUnixHTTP(t, filepath.Join(root, "http.sock"), server.Handler())
	defer closeServer()
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/slow", nil)
	done := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancellation marker was not written")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() error = nil")
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/private/tmp", "cbback-")
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
