package httpclient

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClassifyResponseLeavesAcceptedBodyCallerOwned(t *testing.T) {
	t.Parallel()

	var closed atomic.Int64
	response := &http.Response{
		StatusCode: http.StatusCreated, Header: make(http.Header),
		Body: &responseTestBody{Reader: strings.NewReader("body"), closed: &closed},
	}
	if err := ClassifyResponse(response, StatusOptions{}); err != nil {
		t.Fatalf("classify success: %v", err)
	}
	if closed.Load() != 0 {
		t.Fatalf("accepted body closes = %d", closed.Load())
	}
	_ = response.Body.Close()
}

func TestClassifyResponseReturnsRedactedMappedStatusAndCloses(t *testing.T) {
	t.Parallel()

	secret := []byte("token=secret")
	var closed atomic.Int64
	response := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header: http.Header{
			"Content-Type": {"application/json"}, "X-Request-Id": {"request-1"},
		},
		Body: &responseTestBody{Reader: bytes.NewReader(secret), closed: &closed},
	}
	err := ClassifyResponse(response, StatusOptions{
		MaximumExcerptBytes: 64,
		RedactExcerpt: func(content []byte) ([]byte, error) {
			return bytes.ReplaceAll(content, []byte("secret"), []byte("[redacted]")), nil
		},
		Retryable: func(status int, _ http.Header) bool { return status == http.StatusTooManyRequests },
		MapVendorError: func(snapshot StatusSnapshot) (string, error) {
			if string(snapshot.Excerpt) != "token=[redacted]" {
				t.Fatalf("mapper excerpt = %q", snapshot.Excerpt)
			}
			return "rate_limited", errors.New("vendor-cause")
		},
	})
	var statusError *HTTPStatusError
	if !errors.As(err, &statusError) || !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("status error = %#v", err)
	}
	if statusError.StatusCode != http.StatusTooManyRequests ||
		statusError.VendorCode != "rate_limited" || !statusError.Retryable ||
		statusError.RequestID != "request-1" || string(statusError.Excerpt) != "token=[redacted]" ||
		closed.Load() != 1 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("status error = %#v, closes %d", statusError, closed.Load())
	}
	statusError.Header.Set("changed", "true")
	statusError.Excerpt[0] = 'X'
	if response.Header.Get("changed") != "" || secret[0] == 'X' {
		t.Fatal("status error aliases response state")
	}
}

func TestClassifyResponseRequiresRedactionAndBoundsDrain(t *testing.T) {
	t.Parallel()

	response := &http.Response{
		StatusCode: http.StatusBadRequest, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("body")),
	}
	if err := ClassifyResponse(response, StatusOptions{MaximumExcerptBytes: 4}); !errors.Is(err, ErrInvalidStatusPolicy) {
		t.Fatalf("missing redactor error = %v", err)
	}

	var reads atomic.Int64
	var closed atomic.Int64
	response = &http.Response{
		StatusCode: http.StatusBadGateway, Header: make(http.Header),
		Body: &responseTestBody{
			Reader: &countedInfiniteReader{reads: &reads}, closed: &closed,
		},
	}
	if err := ClassifyResponse(response, StatusOptions{MaximumDrainBytes: 8}); !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("bounded drain status error = %v", err)
	}
	if reads.Load() > 2 || closed.Load() != 1 {
		t.Fatalf("drain reads = %d, closes = %d", reads.Load(), closed.Load())
	}
}

func TestClassifyResponseFailureAndPolicyBoundaries(t *testing.T) {
	t.Parallel()

	if err := ClassifyResponse(nil, StatusOptions{}); !errors.Is(err, ErrInvalidStatusPolicy) {
		t.Fatalf("nil response error = %v", err)
	}
	accepted := &http.Response{
		StatusCode: http.StatusNotFound, Header: make(http.Header), Body: http.NoBody,
	}
	if err := ClassifyResponse(accepted, StatusOptions{
		Accept: func(status int) bool { return status == http.StatusNotFound },
	}); err != nil {
		t.Fatalf("custom accepted response = %v", err)
	}

	failure := errors.New("status failure")
	for _, test := range []struct {
		name    string
		body    io.ReadCloser
		options StatusOptions
		want    error
	}{
		{
			name: "close", body: &responseTestBody{Reader: strings.NewReader(""), closeErr: failure},
			want: failure,
		},
		{
			name: "excerpt read", body: &compressionErrorBody{Reader: &responseErrorReader{err: failure}},
			options: StatusOptions{
				MaximumExcerptBytes: 4,
				RedactExcerpt:       func(content []byte) ([]byte, error) { return content, nil },
			},
			want: failure,
		},
		{
			name: "redactor", body: io.NopCloser(strings.NewReader("body")),
			options: StatusOptions{
				MaximumExcerptBytes: 4,
				RedactExcerpt:       func([]byte) ([]byte, error) { return nil, failure },
			},
			want: failure,
		},
		{
			name: "redactor expansion", body: io.NopCloser(strings.NewReader("body")),
			options: StatusOptions{
				MaximumExcerptBytes: 4,
				RedactExcerpt:       func([]byte) ([]byte, error) { return []byte("expanded"), nil },
			},
			want: ErrInvalidStatusPolicy,
		},
		{
			name: "drain", body: &compressionErrorBody{Reader: &responseErrorReader{err: failure}},
			want: failure,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: test.body,
			}
			err := ClassifyResponse(response, test.options)
			if !errors.Is(err, ErrHTTPStatus) || !errors.Is(err, test.want) {
				t.Fatalf("classification error = %v, want %v", err, test.want)
			}
		})
	}
}

type countedInfiniteReader struct{ reads *atomic.Int64 }

func (reader *countedInfiniteReader) Read(buffer []byte) (int, error) {
	reader.reads.Add(1)
	for index := range buffer {
		buffer[index] = 'x'
	}
	return len(buffer), nil
}
