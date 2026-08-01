package media

import (
	"reflect"
	"testing"
)

func TestScanProgressWriter(t *testing.T) {
	var got []float64
	writer := newScanProgressWriter(func(progress float64) { got = append(got, progress) })
	parts := []string{
		"noise Progr",
		"ess: {\"Scanning\":{\"Progress\":0.2},\"State\":\"SCANNING\"}\nProgress: {",
		"\"Scanning\":{\"Progress\":0.8},\"State\":\"SCANNING\"}",
	}
	for _, part := range parts {
		if _, err := writer.Write([]byte(part)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if !reflect.DeepEqual(got, []float64{0.2, 0.8}) {
		t.Fatalf("progress = %v", got)
	}
}
