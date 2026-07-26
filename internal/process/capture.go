package process

import (
	"bytes"
	"fmt"
)

type LimitedCapture struct {
	limit    int
	buffer   bytes.Buffer
	overflow bool
}

func NewLimitedCapture(limit int) *LimitedCapture {
	return &LimitedCapture{limit: limit}
}

func (c *LimitedCapture) Write(data []byte) (int, error) {
	if c.limit <= 0 {
		c.overflow = c.overflow || len(data) > 0
		return len(data), nil
	}
	remaining := c.limit - c.buffer.Len()
	if remaining > 0 {
		toWrite := data
		if len(toWrite) > remaining {
			toWrite = toWrite[:remaining]
		}
		_, _ = c.buffer.Write(toWrite)
	}
	if len(data) > remaining {
		c.overflow = true
	}
	return len(data), nil
}

func (c *LimitedCapture) Bytes() ([]byte, error) {
	if c.overflow {
		return nil, fmt.Errorf("captured output exceeds %d bytes", c.limit)
	}
	return c.buffer.Bytes(), nil
}
