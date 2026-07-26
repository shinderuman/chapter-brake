package handbrake

import (
	"reflect"
	"testing"
)

func TestProgressWriter(t *testing.T) {
	var got []Progress
	writer := NewProgressWriter(func(progress Progress) {
		got = append(got, progress)
	})
	chunks := []string{
		"Version: {}\nPro",
		"gress: {\n\"State\":\"WORKING\",\"Working\":{\"ETASeconds\":12,",
		"\"Pass\":1,\"PassCount\":2,\"Progress\":0.25}}\n",
		"noise\nProgress: {\"State\":\"WORKDONE\",\"WorkDone\":{\"Error\":0}}\n",
	}
	for _, chunk := range chunks {
		written, err := writer.Write([]byte(chunk))
		if err != nil || written != len(chunk) {
			t.Fatalf("Write() = %d, %v", written, err)
		}
	}
	want := []Progress{
		{State: "WORKING", Fraction: 0.25, ETASeconds: 12, Pass: 1, PassCount: 2},
		{State: "WORKDONE"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("progress = %#v, want %#v", got, want)
	}
}

func TestProgressWriterIgnoresMalformedData(t *testing.T) {
	called := false
	writer := NewProgressWriter(func(Progress) { called = true })
	_, _ = writer.Write([]byte("Progress: {not json}\n"))
	_, _ = writer.Write([]byte("ordinary output\n"))
	if called {
		t.Fatal("malformed progress invoked callback")
	}
}

func TestCompleteJSONObjectHandlesStrings(t *testing.T) {
	data := []byte(`{"message":"brace } and quote \"","nested":{"ok":true}} trailing`)
	end, ok := completeJSONObject(data, 0)
	if !ok || string(data[:end]) != `{"message":"brace } and quote \"","nested":{"ok":true}}` {
		t.Fatalf("completeJSONObject() = %d, %v", end, ok)
	}
}
