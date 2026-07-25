package httpclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDrainResponseCompletesWithinBoundAndCloses(t *testing.T) {
	t.Parallel()

	var closed atomic.Int64
	response := &http.Response{Body: &responseTestBody{
		Reader: strings.NewReader("discard me"), closed: &closed,
	}}
	if err := DrainResponse(response, DrainOptions{MaximumBytes: 16}); err != nil {
		t.Fatalf("drain response: %v", err)
	}
	if closed.Load() != 1 {
		t.Fatalf("body closes = %d", closed.Load())
	}
	exact := &http.Response{Body: io.NopCloser(strings.NewReader("exact"))}
	if err := DrainResponse(exact, DrainOptions{MaximumBytes: 5}); err != nil {
		t.Fatalf("exact-limit drain: %v", err)
	}
}

func TestDrainResponseReturnsTypedLimitAndCloses(t *testing.T) {
	t.Parallel()

	var closed atomic.Int64
	response := &http.Response{Body: &responseTestBody{
		Reader: strings.NewReader("too large"), closed: &closed,
	}}
	err := DrainResponse(response, DrainOptions{MaximumBytes: 3})
	var limitError *ResponseDrainLimitError
	if !errors.As(err, &limitError) || !errors.Is(err, ErrResponseDrainLimit) ||
		limitError.Limit != 3 || closed.Load() != 1 {
		t.Fatalf("drain limit = %#v, closes = %d", err, closed.Load())
	}
}

func TestDrainResponsePreservesReadAndCloseCausesWithoutRendering(t *testing.T) {
	t.Parallel()

	readFailure := errors.New("read-secret")
	closeFailure := errors.New("close-secret")
	response := &http.Response{Body: &responseTestBody{
		Reader: &responseErrorReader{err: readFailure}, closeErr: closeFailure,
	}}
	err := DrainResponse(response, DrainOptions{MaximumBytes: 16})
	var bodyError *ResponseBodyError
	if !errors.As(err, &bodyError) || !errors.Is(err, readFailure) ||
		!errors.Is(err, closeFailure) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("drain dependency error = %#v", err)
	}
}

func TestDrainResponseValidatesPolicyAndDefaultBound(t *testing.T) {
	t.Parallel()

	if err := DrainResponse(nil, DrainOptions{}); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("nil response error = %v", err)
	}
	if err := DrainResponse(&http.Response{}, DrainOptions{}); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("nil body error = %v", err)
	}
	for _, maximum := range []int64{-1, maximumResponseDrainBytes + 1} {
		var closed atomic.Int64
		response := &http.Response{Body: &responseTestBody{Reader: strings.NewReader("body"), closed: &closed}}
		if err := DrainResponse(response, DrainOptions{MaximumBytes: maximum}); !errors.Is(err, ErrInvalidResponsePolicy) || closed.Load() != 1 {
			t.Fatalf("maximum %d error = %v, closes = %d", maximum, err, closed.Load())
		}
	}

	response := &http.Response{Body: io.NopCloser(strings.NewReader(
		strings.Repeat("x", defaultMaximumResponseDrainBytes+1),
	))}
	if err := DrainResponse(response, DrainOptions{}); !errors.Is(err, ErrResponseDrainLimit) {
		t.Fatalf("default drain error = %v", err)
	}
	if (&ResponseDrainLimitError{}).Error() == "" {
		t.Fatal("drain limit error rendered empty text")
	}
}
