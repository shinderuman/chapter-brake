package main

import (
	"bytes"
	"testing"
)

func TestCountingWriterWritesAndCounts(t *testing.T) {
	var destination bytes.Buffer
	writer := &countingWriter{w: &destination}

	n, err := writer.Write([]byte("進捗 42%"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got, want := n, len([]byte("進捗 42%")); got != want {
		t.Fatalf("Write() bytes = %d, want %d", got, want)
	}
	if got, want := writer.bytes(), int64(len([]byte("進捗 42%"))); got != want {
		t.Fatalf("bytes() = %d, want %d", got, want)
	}
	if got, want := destination.String(), "進捗 42%"; got != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}
