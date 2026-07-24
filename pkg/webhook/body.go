package webhook

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
)

var (
	// ErrBodyTooLarge means a request body exceeded its configured hard limit.
	ErrBodyTooLarge = errors.New("webhook body too large")
	// ErrBodyRead means the exact request body could not be captured.
	ErrBodyRead = errors.New("webhook body read failed")
)

// CaptureBody reads at most maxBytes+1 bytes, closes the original body, and
// restores an independent reader only after a complete successful capture.
// A declared oversized body is rejected before the first read.
func CaptureBody(request *http.Request, maxBytes int64) ([]byte, error) {
	if request == nil || request.Body == nil || maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return nil, fmt.Errorf("%w: request, body, and a positive limit are required", ErrInvalidConfiguration)
	}
	if request.ContentLength > maxBytes {
		if err := request.Body.Close(); err != nil {
			return nil, fmt.Errorf("%w: closing rejected body: %v", ErrBodyRead, err)
		}

		return nil, ErrBodyTooLarge
	}

	original := request.Body
	body, readErr := io.ReadAll(io.LimitReader(original, maxBytes+1))
	closeErr := original.Close()
	if readErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrBodyRead, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("%w: closing body: %v", ErrBodyRead, closeErr)
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrBodyTooLarge
	}

	request.Body = io.NopCloser(bytes.NewReader(body))

	return append([]byte(nil), body...), nil
}
