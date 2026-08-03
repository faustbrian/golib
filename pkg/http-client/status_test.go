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
	vendorCause := errors.New("vendor-cause")
	var closed atomic.Int64
	response := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header: http.Header{
			"Content-Type": {"application/json"}, "X-Request-Id": {"request-1"},
			"X-Correlation-Id": {"request-2"},
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
			return "rate_limited", vendorCause
		},
	})
	var statusError *HTTPStatusError
	if !errors.As(err, &statusError) || !errors.Is(err, ErrHTTPStatus) || !errors.Is(err, vendorCause) {
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
	if defaultMaximumStatusDrainBytes != 64*1024 {
		t.Fatalf("default maximum status drain = %d", defaultMaximumStatusDrainBytes)
	}

	counted := &statusCountingReader{reader: strings.NewReader("0123456789")}
	response = &http.Response{
		StatusCode: http.StatusBadGateway, Header: make(http.Header),
		Body: io.NopCloser(counted),
	}
	if err := ClassifyResponse(response, StatusOptions{MaximumDrainBytes: 4}); !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("counted drain status error = %v", err)
	}
	if counted.bytes.Load() != 5 {
		t.Fatalf("counted drain bytes = %d, want 5", counted.bytes.Load())
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
	redirect := &http.Response{
		StatusCode: http.StatusMultipleChoices, Header: make(http.Header), Body: http.NoBody,
	}
	if err := ClassifyResponse(redirect, StatusOptions{}); !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("default status boundary error = %v", err)
	}

	for name, options := range map[string]StatusOptions{
		"negative excerpt":  {MaximumExcerptBytes: -1},
		"excessive excerpt": {MaximumExcerptBytes: maximumStatusExcerptBytes + 1, RedactExcerpt: func(content []byte) ([]byte, error) { return content, nil }},
		"negative drain":    {MaximumDrainBytes: -1},
		"excessive drain":   {MaximumDrainBytes: maximumStatusDrainBytes + 1},
	} {
		response := &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: http.NoBody}
		if err := ClassifyResponse(response, options); !errors.Is(err, ErrInvalidStatusPolicy) {
			t.Fatalf("%s policy error = %v", name, err)
		}
	}
	boundary := &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: http.NoBody}
	if err := ClassifyResponse(boundary, StatusOptions{
		MaximumExcerptBytes: maximumStatusExcerptBytes,
		MaximumDrainBytes:   maximumStatusDrainBytes,
		RedactExcerpt:       func(content []byte) ([]byte, error) { return content, nil },
	}); !errors.Is(err, ErrHTTPStatus) || errors.Is(err, ErrInvalidStatusPolicy) {
		t.Fatalf("exact policy maxima error = %v", err)
	}
	exactExcerpt := &http.Response{
		StatusCode: http.StatusBadRequest, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader("body")),
	}
	err := ClassifyResponse(exactExcerpt, StatusOptions{
		MaximumExcerptBytes: 4,
		RedactExcerpt:       func(content []byte) ([]byte, error) { return content, nil },
	})
	var exactStatus *HTTPStatusError
	if !errors.As(err, &exactStatus) || string(exactStatus.Excerpt) != "body" {
		t.Fatalf("exact-size redacted excerpt error = %#v", err)
	}

	failure := errors.New("status failure")
	for _, test := range []struct {
		name      string
		body      io.ReadCloser
		options   StatusOptions
		want      error
		operation string
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
			want: failure, operation: "excerpt read",
		},
		{
			name: "redactor", body: io.NopCloser(strings.NewReader("body")),
			options: StatusOptions{
				MaximumExcerptBytes: 4,
				RedactExcerpt:       func([]byte) ([]byte, error) { return nil, failure },
			},
			want: failure, operation: "excerpt redaction",
		},
		{
			name: "redactor expansion", body: io.NopCloser(strings.NewReader("body")),
			options: StatusOptions{
				MaximumExcerptBytes: 4,
				RedactExcerpt:       func([]byte) ([]byte, error) { return []byte("expanded"), nil },
			},
			want: ErrInvalidStatusPolicy, operation: "excerpt redaction",
		},
		{
			name: "drain", body: &compressionErrorBody{Reader: &responseErrorReader{err: failure}},
			want: failure, operation: "drain",
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
			if test.operation != "" {
				var bodyError *ResponseBodyError
				if !errors.As(err, &bodyError) || bodyError.Operation != test.operation {
					t.Fatalf("classification body error = %#v, want operation %q", err, test.operation)
				}
			}
		})
	}
}

type countedInfiniteReader struct{ reads *atomic.Int64 }

type statusCountingReader struct {
	reader io.Reader
	bytes  atomic.Int64
}

func (reader *statusCountingReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	reader.bytes.Add(int64(count))
	return count, err
}

func (reader *countedInfiniteReader) Read(buffer []byte) (int, error) {
	reader.reads.Add(1)
	for index := range buffer {
		buffer[index] = 'x'
	}
	return len(buffer), nil
}
