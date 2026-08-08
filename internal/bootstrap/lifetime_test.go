package bootstrap

import (
	"os"
	"testing"
	"time"
)

func TestHostLifetimeExpiresWhenWriterCloses(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	lifetime := watchHostLifetime(reader)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lifetime.Done():
	case <-time.After(time.Second):
		t.Fatal("host lifetime did not expire after its writer closed")
	}
	if err := lifetime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClosingHostLifetimeMonitorDoesNotReportExpiration(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	lifetime := watchHostLifetime(reader)
	if err := lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lifetime.Done():
		t.Fatal("local monitor cleanup was reported as host expiration")
	default:
	}
}

func TestOpenHostLifetimeValidatesDescriptor(t *testing.T) {
	for _, value := range []string{"invalid", "2"} {
		if _, err := openHostLifetime(value); err == nil {
			t.Fatalf("openHostLifetime(%q) error = nil", value)
		}
	}
	if lifetime, err := openHostLifetime(""); err != nil || lifetime != nil {
		t.Fatalf("openHostLifetime(empty) = %#v, %v", lifetime, err)
	}
}
