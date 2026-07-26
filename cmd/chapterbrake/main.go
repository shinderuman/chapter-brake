package main

import (
	"fmt"
	"os"

	"chapterbrake/internal/bootstrap"
)

func main() {
	if err := bootstrap.Run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "chapterbrake:", err)
		os.Exit(1)
	}
}
