package upstream

import (
	"bytes"
	"strings"
)

var sseDataPrefix = []byte("data:")

const sseDonePayload = "[DONE]"

// sseDoneDetector detects whether an SSE stream has emitted `data: [DONE]`.
// It tolerates arbitrary read chunk boundaries.
type sseDoneDetector struct {
	pending []byte
	seen    bool
}

func (d *sseDoneDetector) Observe(chunk []byte) {
	if d == nil || d.seen || len(chunk) == 0 {
		return
	}
	d.pending = append(d.pending, chunk...)
	d.consumeCompleteLines()
}

func (d *sseDoneDetector) Finalize() {
	if d == nil || d.seen || len(d.pending) == 0 {
		return
	}
	d.inspectLine(d.pending)
	d.pending = nil
}

func (d *sseDoneDetector) Seen() bool {
	return d != nil && d.seen
}

func (d *sseDoneDetector) consumeCompleteLines() {
	for !d.seen {
		idx := bytes.IndexByte(d.pending, '\n')
		if idx < 0 {
			return
		}
		line := d.pending[:idx]
		d.pending = d.pending[idx+1:]
		d.inspectLine(line)
	}
}

func (d *sseDoneDetector) inspectLine(line []byte) {
	if d.seen {
		return
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	if !bytes.HasPrefix(line, sseDataPrefix) {
		return
	}
	payload := strings.TrimSpace(string(line[len(sseDataPrefix):]))
	if payload == sseDonePayload {
		d.seen = true
	}
}
