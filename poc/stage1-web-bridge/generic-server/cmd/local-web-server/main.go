package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"chapterbrake-web-poc/generic-server/internal/server"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "local-web-server:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("local-web-server", flag.ContinueOnError)
	listenAddress := flags.String("listen", "127.0.0.1:18765", "HTTP listen address")
	appsDirectory := flags.String("apps", "", "installed apps directory")
	runtimeDirectory := flags.String("runtime", "", "Unix socket directory")
	readyFile := flags.String("ready-file", "", "write selected URL to this file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *appsDirectory == "" || *runtimeDirectory == "" {
		return fmt.Errorf("-apps and -runtime are required")
	}
	host, _, err := net.SplitHostPort(*listenAddress)
	if err != nil {
		return fmt.Errorf("parse listen address: %w", err)
	}
	if host != "127.0.0.1" {
		return fmt.Errorf("listen host must be 127.0.0.1")
	}

	apps, err := server.LoadApps(*appsDirectory, *runtimeDirectory)
	if err != nil {
		return err
	}
	localServer, err := server.New(apps)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := localServer.StartBackends(ctx); err != nil {
		return err
	}
	defer localServer.Close()

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	url := "http://" + listener.Addr().String() + "/"
	if strings.HasSuffix(url, ":0/") {
		return fmt.Errorf("listener did not select a port")
	}
	if *readyFile != "" {
		if err := os.WriteFile(*readyFile, []byte(url), 0o600); err != nil {
			return fmt.Errorf("write ready file: %w", err)
		}
	}
	fmt.Println(url)

	httpServer := &http.Server{
		Handler:           localServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		done <- httpServer.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownContext)
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
