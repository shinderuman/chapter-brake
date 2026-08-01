package media

import (
	"bytes"
	"encoding/json"
	"sync"
)

const scanProgressBufferLimit = 1 << 20

type scanProgressDocument struct {
	State    string `json:"State"`
	Scanning struct {
		Progress float64 `json:"Progress"`
	} `json:"Scanning"`
}

type scanProgressWriter struct {
	mu       sync.Mutex
	buffer   []byte
	callback func(float64)
}

func newScanProgressWriter(callback func(float64)) *scanProgressWriter {
	return &scanProgressWriter{callback: callback}
}

func (writer *scanProgressWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.buffer = append(writer.buffer, data...)
	if len(writer.buffer) > scanProgressBufferLimit {
		writer.buffer = append([]byte(nil), writer.buffer[len(writer.buffer)-scanProgressBufferLimit:]...)
	}
	writer.consume()
	return len(data), nil
}

func (writer *scanProgressWriter) consume() {
	marker := []byte("Progress:")
	for {
		markerIndex := bytes.Index(writer.buffer, marker)
		if markerIndex < 0 {
			if len(writer.buffer) > len(marker) {
				writer.buffer = append([]byte(nil), writer.buffer[len(writer.buffer)-len(marker):]...)
			}
			return
		}
		objectStartRelative := bytes.IndexByte(writer.buffer[markerIndex+len(marker):], '{')
		if objectStartRelative < 0 {
			writer.buffer = append([]byte(nil), writer.buffer[markerIndex:]...)
			return
		}
		objectStart := markerIndex + len(marker) + objectStartRelative
		objectEnd, complete := completeScanProgressObject(writer.buffer, objectStart)
		if !complete {
			writer.buffer = append([]byte(nil), writer.buffer[markerIndex:]...)
			return
		}

		var document scanProgressDocument
		if err := json.Unmarshal(writer.buffer[objectStart:objectEnd], &document); err == nil &&
			document.State == "SCANNING" && writer.callback != nil {
			writer.callback(max(0, min(1, document.Scanning.Progress)))
		}
		writer.buffer = append([]byte(nil), writer.buffer[objectEnd:]...)
	}
}

func completeScanProgressObject(data []byte, start int) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(data); index++ {
		char := data[index]
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
				return index + 1, true
			}
		}
	}
	return 0, false
}
