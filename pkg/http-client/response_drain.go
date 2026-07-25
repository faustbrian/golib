package httpclient

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultMaximumResponseDrainBytes = 64 << 10
	maximumResponseDrainBytes        = 16 << 20
)

var (
	// ErrResponseDrainLimit indicates that bounded draining did not reach EOF.
	ErrResponseDrainLimit = errors.New("HTTP response drain limit reached")
)

// DrainOptions configures bounded response draining for connection reuse.
type DrainOptions struct {
	MaximumBytes int64
}

// ResponseDrainLimitError reports that draining exceeded its finite bound.
type ResponseDrainLimitError struct {
	Limit   int64
	Drained int64
}

// Error implements error.
func (*ResponseDrainLimitError) Error() string {
	return "HTTP response drain limit reached"
}

// Unwrap returns the stable drain-limit sentinel.
func (*ResponseDrainLimitError) Unwrap() error {
	return ErrResponseDrainLimit
}

// DrainResponse boundedly consumes and always closes response.Body. Reaching
// EOF permits connection reuse; exceeding the bound returns a typed error.
func DrainResponse(response *http.Response, options DrainOptions) (resultErr error) {
	if response == nil || response.Body == nil {
		return fmt.Errorf("%w: response or body is nil", ErrInvalidResponsePolicy)
	}
	body := response.Body
	defer func() {
		if err := body.Close(); err != nil {
			resultErr = errors.Join(resultErr, &ResponseBodyError{Operation: "close", Cause: err})
		}
	}()

	maximum := options.MaximumBytes
	if maximum == 0 {
		maximum = defaultMaximumResponseDrainBytes
	}
	if maximum < 1 || maximum > maximumResponseDrainBytes {
		return fmt.Errorf("%w: maximum drain is invalid", ErrInvalidResponsePolicy)
	}
	drained, err := io.Copy(io.Discard, io.LimitReader(body, maximum+1))
	if err != nil {
		return &ResponseBodyError{Operation: "drain", Cause: err}
	}
	if drained > maximum {
		return &ResponseDrainLimitError{Limit: maximum, Drained: drained}
	}

	return nil
}
