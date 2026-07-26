package handbrake

import (
	"bytes"
	"encoding/json"
	"sync"
)

const progressBufferLimit = 1 << 20

type Progress struct {
	State      string
	Fraction   float64
	ETASeconds int
	Pass       int
	PassCount  int
}

type progressDocument struct {
	State   string `json:"State"`
	Working struct {
		ETASeconds int     `json:"ETASeconds"`
		Pass       int     `json:"Pass"`
		PassCount  int     `json:"PassCount"`
		Progress   float64 `json:"Progress"`
	} `json:"Working"`
}

// ProgressWriter extracts HandBrake's multiline "Progress: {...}" objects.
// Parse failures never fail the underlying encode.
type ProgressWriter struct {
	mu       sync.Mutex
	buffer   []byte
	callback func(Progress)
}

func NewProgressWriter(callback func(Progress)) *ProgressWriter {
	return &ProgressWriter{callback: callback}
}

func (w *ProgressWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buffer = append(w.buffer, data...)
	if len(w.buffer) > progressBufferLimit {
		w.buffer = append([]byte(nil), w.buffer[len(w.buffer)-progressBufferLimit:]...)
	}
	w.consume()
	return len(data), nil
}

func (w *ProgressWriter) consume() {
	marker := []byte("Progress:")
	for {
		markerIndex := bytes.Index(w.buffer, marker)
		if markerIndex < 0 {
			if len(w.buffer) > len(marker) {
				w.buffer = append([]byte(nil), w.buffer[len(w.buffer)-len(marker):]...)
			}
			return
		}
		objectStartRelative := bytes.IndexByte(w.buffer[markerIndex+len(marker):], '{')
		if objectStartRelative < 0 {
			w.buffer = append([]byte(nil), w.buffer[markerIndex:]...)
			return
		}
		objectStart := markerIndex + len(marker) + objectStartRelative
		objectEnd, ok := completeJSONObject(w.buffer, objectStart)
		if !ok {
			w.buffer = append([]byte(nil), w.buffer[markerIndex:]...)
			return
		}

		var document progressDocument
		if err := json.Unmarshal(w.buffer[objectStart:objectEnd], &document); err == nil && w.callback != nil {
			w.callback(Progress{
				State:      document.State,
				Fraction:   document.Working.Progress,
				ETASeconds: document.Working.ETASeconds,
				Pass:       document.Working.Pass,
				PassCount:  document.Working.PassCount,
			})
		}
		w.buffer = append([]byte(nil), w.buffer[objectEnd:]...)
	}
}

func completeJSONObject(data []byte, start int) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(data); i++ {
		char := data[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch char {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}
