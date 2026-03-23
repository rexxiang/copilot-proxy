package messages

import (
	"fmt"
	"io"
)

// errWriter wraps an io.Writer and stops writing after the first error.
// This prevents translation goroutines from continuing to read upstream
// data when the downstream consumer has disconnected.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	if err != nil {
		err = fmt.Errorf("write transformed stream: %w", err)
	}
	ew.err = err
	return n, err
}

func (ew *errWriter) failed() bool {
	return ew.err != nil
}
