package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealHandBrakePauseResume(t *testing.T) {
	if os.Getenv("CHAPTERBRAKE_INTEGRATION") != "1" {
		t.Skip("set CHAPTERBRAKE_INTEGRATION=1 to run real HandBrake test")
	}
	fixture := os.Getenv("CHAPTERBRAKE_FIXTURE")
	presetFile := os.Getenv("CHAPTERBRAKE_PRESET_FILE")
	if fixture == "" || presetFile == "" {
		t.Skip("set CHAPTERBRAKE_FIXTURE and CHAPTERBRAKE_PRESET_FILE")
	}

	handBrake, err := exec.LookPath("HandBrakeCLI")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "pause-check.mp4")
	executor := &OSExecutor{InterruptGrace: 5 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- executor.Run(ctx, Invocation{
			Executable: handBrake,
			Args: []string{
				"--json",
				"--preset-import-file", presetFile,
				"--preset", "MP4 Presets",
				"-i", fixture,
				"-o", output,
			},
		}, nil, nil)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		err = executor.Pause()
		if err == nil {
			break
		}
		select {
		case runErr := <-done:
			t.Fatalf("HandBrake finished before pause: %v", runErr)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("HandBrake did not become pausable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	executor.mu.Lock()
	pid := executor.pid
	executor.mu.Unlock()
	stateOutput, err := exec.Command("ps", "-o", "state=", "-p", stringPID(pid)).Output()
	if err != nil {
		cancel()
		t.Fatalf("inspect paused HandBrake process: %v", err)
	}
	if !strings.Contains(string(stateOutput), "T") {
		cancel()
		t.Fatalf("HandBrake process state = %q, want stopped", strings.TrimSpace(string(stateOutput)))
	}
	if err := executor.Resume(); err != nil {
		cancel()
		t.Fatalf("Resume() error = %v", err)
	}
	cancel()
	select {
	case runErr := <-done:
		var commandErr *Error
		if !errors.As(runErr, &commandErr) || !commandErr.Canceled {
			t.Fatalf("Run() error = %v, want canceled", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("HandBrake did not stop after resume and cancel")
	}
}

func stringPID(pid int) string {
	return strconv.Itoa(pid)
}
