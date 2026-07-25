package webhook

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCaptureBodyRejectsLimitThatCannotReserveOverflowByte(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("body"))
	if _, err := CaptureBody(request, math.MaxInt64); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("CaptureBody(MaxInt64) error = %v", err)
	}
}

func TestCaptureBodyPreservesExactBytesAndRestoresRequest(t *testing.T) {
	t.Parallel()

	want := []byte{0x00, 0xff, '\r', '\n', 'x'}
	request := &http.Request{
		Body:          io.NopCloser(bytes.NewReader(want)),
		ContentLength: int64(len(want)),
	}

	got, err := CaptureBody(request, int64(len(want)))
	if err != nil {
		t.Fatalf("CaptureBody() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("CaptureBody() = %v, want %v", got, want)
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("reading restored body: %v", err)
	}
	if !bytes.Equal(restored, want) {
		t.Fatalf("restored body = %v, want %v", restored, want)
	}
}

func TestCaptureBodyPreservesEmptyCompressedTrailersAndPartialReads(t *testing.T) {
	t.Parallel()

	empty := &http.Request{Body: http.NoBody, ContentLength: 0}
	if body, err := CaptureBody(empty, 1); err != nil || len(body) != 0 {
		t.Fatalf("CaptureBody(empty) = %v, %v", body, err)
	}

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(`{"compressed":true}`))
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}
	wireBytes := append([]byte(nil), compressed.Bytes()...)
	request := &http.Request{
		Header: http.Header{"Content-Encoding": {"gzip"}},
		Body:   &oneByteReader{reader: bytes.NewReader(wireBytes)}, ContentLength: -1,
		Trailer: http.Header{"Digest": {"sha-256=trailer-value"}},
	}
	body, err := CaptureBody(request, int64(len(wireBytes)))
	if err != nil || !bytes.Equal(body, wireBytes) {
		t.Fatalf("CaptureBody(compressed) preserved = %v, error = %v", bytes.Equal(body, wireBytes), err)
	}
	if request.Header.Get("Content-Encoding") != "gzip" || request.Trailer.Get("Digest") != "sha-256=trailer-value" {
		t.Fatalf("request metadata changed: headers = %v, trailers = %v", request.Header, request.Trailer)
	}
}

func TestCaptureBodyAfterPriorReadAuthenticatesOnlyRemainingBytes(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("prefix-body"))
	prefix := make([]byte, len("prefix-"))
	if _, err := io.ReadFull(request.Body, prefix); err != nil {
		t.Fatalf("prior read error = %v", err)
	}
	request.ContentLength = -1
	body, err := CaptureBody(request, 64)
	if err != nil || string(body) != "body" {
		t.Fatalf("CaptureBody(after prior read) = %q, %v", body, err)
	}
}

func TestCaptureBodyRejectsDeclaredOversizeBeforeReading(t *testing.T) {
	t.Parallel()

	body := &observedBody{reader: bytes.NewReader([]byte("payload"))}
	request := &http.Request{Body: body, ContentLength: 8}

	_, err := CaptureBody(request, 7)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("CaptureBody() error = %v, want ErrBodyTooLarge", err)
	}
	if body.reads != 0 {
		t.Fatalf("CaptureBody() performed %d reads before rejecting content length", body.reads)
	}
	if !body.closed {
		t.Fatal("CaptureBody() did not close rejected body")
	}
}

func TestCaptureBodyBoundsUnknownLengthBeforeAllocation(t *testing.T) {
	t.Parallel()

	body := &observedBody{reader: bytes.NewReader([]byte("123456789"))}
	request := &http.Request{Body: body, ContentLength: -1}

	_, err := CaptureBody(request, 7)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("CaptureBody() error = %v, want ErrBodyTooLarge", err)
	}
	if body.bytesRead != 8 {
		t.Fatalf("CaptureBody() read %d bytes, want max+1", body.bytesRead)
	}
	if !body.closed {
		t.Fatal("CaptureBody() did not close oversized body")
	}
}

func TestCaptureBodyRejectsUnsafeArguments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		request *http.Request
		limit   int64
	}{
		"nil request": {request: nil, limit: 1},
		"nil body":    {request: &http.Request{}, limit: 1},
		"zero limit":  {request: &http.Request{Body: http.NoBody}, limit: 0},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := CaptureBody(test.request, test.limit); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("CaptureBody() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

type observedBody struct {
	reader    *bytes.Reader
	reads     int
	bytesRead int
	closed    bool
}

type oneByteReader struct {
	reader *bytes.Reader
}

func (r *oneByteReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return r.reader.Read(buffer)
}

func (*oneByteReader) Close() error { return nil }

func (b *observedBody) Read(buffer []byte) (int, error) {
	b.reads++
	count, err := b.reader.Read(buffer)
	b.bytesRead += count

	return count, err
}

func (b *observedBody) Close() error {
	b.closed = true

	return nil
}
