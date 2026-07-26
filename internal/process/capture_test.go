package process

import (
	"strings"
	"testing"
)

func TestLimitedCapture(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		capture := NewLimitedCapture(5)
		if written, err := capture.Write([]byte("abc")); err != nil || written != 3 {
			t.Fatalf("Write() = %d, %v", written, err)
		}
		if written, err := capture.Write([]byte("de")); err != nil || written != 2 {
			t.Fatalf("Write() = %d, %v", written, err)
		}
		got, err := capture.Bytes()
		if err != nil || string(got) != "abcde" {
			t.Fatalf("Bytes() = %q, %v", got, err)
		}
	})

	t.Run("overflow still consumes writer input", func(t *testing.T) {
		capture := NewLimitedCapture(4)
		written, err := capture.Write([]byte("abcdef"))
		if err != nil || written != 6 {
			t.Fatalf("Write() = %d, %v", written, err)
		}
		if _, err := capture.Bytes(); err == nil || !strings.Contains(err.Error(), "exceeds 4") {
			t.Fatalf("Bytes() error = %v", err)
		}
	})
}
