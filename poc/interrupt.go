// interrupt.go is a disposable Milestone 0 spike for macOS process-group
// cancellation. It is not product code.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type countingWriter struct {
	mu sync.Mutex
	w  io.Writer
	n  int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func (w *countingWriter) bytes() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

type result struct {
	Command            []string `json:"command"`
	PID                int      `json:"pid"`
	Signal             string   `json:"signal"`
	CancellationTried  bool     `json:"cancellation_tried"`
	CompletedBefore    bool     `json:"completed_before_cancellation"`
	SignalError        string   `json:"signal_error,omitempty"`
	EscalatedToKill    bool     `json:"escalated_to_kill"`
	WaitError          string   `json:"wait_error,omitempty"`
	ExitCode           int      `json:"exit_code"`
	Signaled           bool     `json:"signaled"`
	TermSignal         string   `json:"term_signal,omitempty"`
	StopElapsedMS      int64    `json:"stop_elapsed_ms"`
	ProcessGroupGone   bool     `json:"process_group_gone"`
	PartialOutputFound bool     `json:"partial_output_found"`
	PartialOutputGone  bool     `json:"partial_output_gone"`
	StdoutBytes        int64    `json:"stdout_bytes"`
	StderrBytes        int64    `json:"stderr_bytes"`
}

func main() {
	delay := flag.Duration("delay", 500*time.Millisecond, "delay before cancellation")
	grace := flag.Duration("grace", 2*time.Second, "grace period before SIGKILL")
	output := flag.String("output", "", "partial output to remove after process exit")
	stdoutPath := flag.String("stdout-log", "", "complete child stdout log")
	stderrPath := flag.String("stderr-log", "", "complete child stderr log")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 || *stdoutPath == "" || *stderrPath == "" {
		fmt.Fprintln(os.Stderr, "usage: go run interrupt.go [flags] -- command args...")
		os.Exit(2)
	}

	stdoutFile, err := os.Create(*stdoutPath)
	must(err)
	defer stdoutFile.Close()
	stderrFile, err := os.Create(*stderrPath)
	must(err)
	defer stderrFile.Close()

	stdoutCounter := &countingWriter{w: io.MultiWriter(stdoutFile, os.Stdout)}
	stderrCounter := &countingWriter{w: io.MultiWriter(stderrFile, os.Stderr)}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = stdoutCounter
	cmd.Stderr = stderrCounter

	must(cmd.Start())
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	stopStarted := time.Now()
	var waitErr error
	escalated := false
	completedBefore := false
	cancellationTried := false
	var signalErr error
	delayTimer := time.NewTimer(*delay)
	select {
	case waitErr = <-waitCh:
		completedBefore = true
	case <-delayTimer.C:
		cancellationTried = true
		stopStarted = time.Now()
		signalErr = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
		if signalErr != nil && !errors.Is(signalErr, syscall.ESRCH) && !errors.Is(signalErr, syscall.EPERM) {
			must(signalErr)
		}
	}

	if !completedBefore {
		select {
		case waitErr = <-waitCh:
		case <-time.After(*grace):
			escalated = true
			killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if killErr != nil && !errors.Is(killErr, syscall.ESRCH) && !errors.Is(killErr, syscall.EPERM) {
				must(killErr)
			}
			waitErr = <-waitCh
		}
	}

	res := result{
		Command:           args,
		PID:               cmd.Process.Pid,
		Signal:            syscall.SIGINT.String(),
		CancellationTried: cancellationTried,
		CompletedBefore:   completedBefore,
		EscalatedToKill:   escalated,
		ExitCode:          cmd.ProcessState.ExitCode(),
		StopElapsedMS:     time.Since(stopStarted).Milliseconds(),
		ProcessGroupGone:  errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH),
		StdoutBytes:       stdoutCounter.bytes(),
		StderrBytes:       stderrCounter.bytes(),
	}
	if signalErr != nil {
		res.SignalError = signalErr.Error()
	}
	if waitErr != nil {
		res.WaitError = waitErr.Error()
	}
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		res.Signaled = status.Signaled()
		if status.Signaled() {
			res.TermSignal = status.Signal().String()
		}
	}

	if *output != "" {
		if _, err := os.Stat(*output); err == nil {
			res.PartialOutputFound = true
			must(os.Remove(*output))
		} else if !errors.Is(err, os.ErrNotExist) {
			must(err)
		}
		_, err := os.Stat(*output)
		res.PartialOutputGone = errors.Is(err, os.ErrNotExist)
	}

	must(json.NewEncoder(os.Stdout).Encode(res))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
