package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

type Invocation struct {
	Executable string
	Args       []string
}

func (i Invocation) Validate() error {
	if i.Executable == "" {
		return fmt.Errorf("executable must not be empty")
	}
	return nil
}

type Executor interface {
	Run(context.Context, Invocation, io.Writer, io.Writer) error
}

type OSExecutor struct {
	InterruptGrace time.Duration
}

type Error struct {
	Invocation Invocation
	ExitCode   int
	Signal     string
	Canceled   bool
	Err        error
}

func (e *Error) Error() string {
	if e.Canceled {
		return fmt.Sprintf("%s canceled: %v", e.Invocation.Executable, e.Err)
	}
	if e.Signal != "" {
		return fmt.Sprintf("%s terminated by signal %s: %v", e.Invocation.Executable, e.Signal, e.Err)
	}
	if e.ExitCode >= 0 {
		return fmt.Sprintf("%s exited with code %d: %v", e.Invocation.Executable, e.ExitCode, e.Err)
	}
	return fmt.Sprintf("%s failed: %v", e.Invocation.Executable, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e OSExecutor) Run(
	ctx context.Context,
	invocation Invocation,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if err := invocation.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return &Error{Invocation: invocation, ExitCode: -1, Canceled: true, Err: err}
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	command := exec.Command(invocation.Executable, invocation.Args...)
	configureProcessGroup(command)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return &Error{Invocation: invocation, ExitCode: -1, Err: err}
	}

	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
	}()

	select {
	case err := <-waited:
		return commandError(invocation, err, false)
	case <-ctx.Done():
		if err := interruptProcessGroup(command.Process.Pid); err != nil && !errors.Is(err, errProcessDone) {
			_ = killProcessGroup(command.Process.Pid)
		}

		grace := e.InterruptGrace
		if grace <= 0 {
			grace = 3 * time.Second
		}
		timer := time.NewTimer(grace)
		defer timer.Stop()

		select {
		case err := <-waited:
			return commandError(invocation, errors.Join(ctx.Err(), err), true)
		case <-timer.C:
			killErr := killProcessGroup(command.Process.Pid)
			err := <-waited
			return commandError(invocation, errors.Join(ctx.Err(), killErr, err), true)
		}
	}
}

func commandError(invocation Invocation, err error, canceled bool) error {
	if err == nil && !canceled {
		return nil
	}

	result := &Error{
		Invocation: invocation,
		ExitCode:   -1,
		Canceled:   canceled,
		Err:        err,
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		result.Signal = exitSignal(exitErr)
	}
	return result
}
