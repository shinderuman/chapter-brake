package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOSExecutorRun(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("success preserves separate streams", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		err := (OSExecutor{}).Run(
			context.Background(),
			Invocation{Executable: executable, Args: []string{"-test.run=TestProcessHelper", "--", "success"}},
			&stdout,
			&stderr,
		)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if stdout.String() != "stdout-日本語\n" {
			t.Fatalf("stdout = %q", stdout.String())
		}
		if stderr.String() != "stderr-日本語\n" {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})

	t.Run("exit error", func(t *testing.T) {
		err := (OSExecutor{}).Run(
			context.Background(),
			Invocation{Executable: executable, Args: []string{"-test.run=TestProcessHelper", "--", "failure"}},
			nil,
			nil,
		)
		var commandErr *Error
		if !errors.As(err, &commandErr) {
			t.Fatalf("Run() error = %T %v, want *Error", err, err)
		}
		if commandErr.ExitCode != 7 || commandErr.Canceled {
			t.Fatalf("command error = %#v", commandErr)
		}
	})

	t.Run("cancellation interrupts process group", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		err := (OSExecutor{InterruptGrace: time.Second}).Run(
			ctx,
			Invocation{Executable: executable, Args: []string{"-test.run=TestProcessHelper", "--", "wait"}},
			nil,
			nil,
		)
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("cancellation took %v", elapsed)
		}
		var commandErr *Error
		if !errors.As(err, &commandErr) || !commandErr.Canceled {
			t.Fatalf("Run() error = %T %v, want canceled *Error", err, err)
		}
	})

	t.Run("already canceled does not start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := (OSExecutor{}).Run(ctx, Invocation{Executable: executable}, nil, nil)
		var commandErr *Error
		if !errors.As(err, &commandErr) || !commandErr.Canceled {
			t.Fatalf("Run() error = %T %v", err, err)
		}
	})
}

func TestInvocationValidate(t *testing.T) {
	if err := (Invocation{}).Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
	if err := (Invocation{Executable: "/bin/tool"}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProcessHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}

	switch os.Args[separator+1] {
	case "success":
		fmt.Fprintln(os.Stdout, "stdout-日本語")
		fmt.Fprintln(os.Stderr, "stderr-日本語")
		os.Exit(0)
	case "failure":
		os.Exit(7)
	case "wait":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	default:
		if strings.TrimSpace(os.Args[separator+1]) != "" {
			os.Exit(9)
		}
	}
}
