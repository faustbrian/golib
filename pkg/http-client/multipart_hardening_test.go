package httpclient

import (
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMultipartBodyRejectsEveryBoundedMetadataClass(t *testing.T) {
	t.Parallel()

	valid, _ := NewBytesBody("text/plain", []byte("value"))
	tooMany := make([]MultipartPart, maximumMultipartParts+1)
	for index := range tooMany {
		tooMany[index] = MultipartPart{Name: "part", Body: valid}
	}
	invalidBody := &mutableRequestBody{contentType: "not a media type", contentLength: 1}
	longText := strings.Repeat("a", maximumMultipartTextBytes+1)

	for _, test := range []struct {
		name    string
		options MultipartOptions
	}{
		{name: "malformed boundary", options: MultipartOptions{Boundary: "bad\n", Parts: []MultipartPart{{Name: "part", Body: valid}}}},
		{name: "too many parts", options: MultipartOptions{Boundary: "boundary", Parts: tooMany}},
		{name: "negative maximum", options: MultipartOptions{Boundary: "boundary", MaximumBytes: -1, Parts: []MultipartPart{{Name: "part", Body: valid}}}},
		{name: "excessive maximum", options: MultipartOptions{Boundary: "boundary", MaximumBytes: maximumMultipartBytes + 1, Parts: []MultipartPart{{Name: "part", Body: valid}}}},
		{name: "nil body", options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{Name: "part"}}}},
		{name: "invalid body metadata", options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{Name: "part", Body: invalidBody}}}},
		{name: "control filename", options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{Name: "part", FileName: "bad\nname", Body: valid}}}},
		{name: "long name", options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{Name: longText, Body: valid}}}},
		{
			name: "invalid header name",
			options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{
				Name: "part", Body: valid, Header: http.Header{" bad": {"value"}},
			}}},
		},
		{
			name: "empty header values",
			options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{
				Name: "part", Body: valid, Header: http.Header{"X-Part": nil},
			}}},
		},
		{
			name: "control header value",
			options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{
				Name: "part", Body: valid, Header: http.Header{"X-Part": {"bad\nvalue"}},
			}}},
		},
		{
			name: "large headers",
			options: MultipartOptions{Boundary: "boundary", Parts: []MultipartPart{{
				Name: "part", Body: valid, Header: http.Header{"X-Part": {longText}},
			}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewMultipartBody(test.options); !errors.Is(err, ErrInvalidMultipart) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestMultipartBodyDefaultsLimitAndSnapshotsHeaders(t *testing.T) {
	t.Parallel()

	partBody, _ := NewBytesBody("", []byte("value"))
	header := http.Header{"X-Part": {"before"}}
	body, err := NewMultipartBody(MultipartOptions{
		Boundary: "snapshot", Parts: []MultipartPart{{Name: "part", Body: partBody, Header: header}},
	})
	if err != nil {
		t.Fatalf("construct multipart: %v", err)
	}
	header.Set("X-Part", "after")
	content := string(readMultipartBody(t, body))
	if !strings.Contains(content, "X-Part: before") || strings.Contains(content, "after") {
		t.Fatalf("multipart snapshot = %q", content)
	}
	if body.(*multipartRequestBody).maximumBytes != defaultMultipartMaximumBytes {
		t.Fatalf("default maximum = %d", body.(*multipartRequestBody).maximumBytes)
	}
	if (&MultipartError{}).Error() != "multipart body failed" {
		t.Fatalf("empty multipart error = %q", (&MultipartError{}).Error())
	}
}

func TestMultipartBodyBoundsUnknownContentAndJoinsOwnedFailures(t *testing.T) {
	t.Parallel()

	var limitedClosed atomic.Int64
	stream, _ := NewStreamingBody("application/octet-stream", -1, &responseTestBody{
		Reader: strings.NewReader(strings.Repeat("x", 256)), closed: &limitedClosed,
	})
	limited, err := NewMultipartBody(MultipartOptions{
		Boundary: "bounded", MaximumBytes: 80,
		Parts: []MultipartPart{{Name: "part", Body: stream}},
	})
	if err != nil {
		t.Fatalf("construct bounded multipart: %v", err)
	}
	opened, err := limited.Open()
	if err != nil {
		t.Fatalf("open bounded multipart: %v", err)
	}
	_, readErr := io.ReadAll(opened)
	closeErr := opened.Close()
	if !errors.Is(readErr, ErrMultipartLimit) || !errors.Is(closeErr, ErrMultipartLimit) || limitedClosed.Load() != 1 {
		t.Fatalf("bounded errors = %v, %v; closes = %d", readErr, closeErr, limitedClosed.Load())
	}

	readFailure := errors.New("read-secret")
	closeFailure := errors.New("close-secret")
	failing, _ := NewReplayableBody("text/plain", -1, func() (io.ReadCloser, error) {
		return &responseTestBody{Reader: &responseErrorReader{err: readFailure}, closeErr: closeFailure}, nil
	})
	body, err := NewMultipartBody(MultipartOptions{
		Boundary: "failure", Parts: []MultipartPart{{Name: "part", Body: failing}},
	})
	if err != nil {
		t.Fatalf("construct failing multipart: %v", err)
	}
	opened, _ = body.Open()
	_, readErr = io.ReadAll(opened)
	closeErr = opened.Close()
	if !errors.Is(readErr, readFailure) || !errors.Is(readErr, closeFailure) ||
		!errors.Is(closeErr, readFailure) || !errors.Is(closeErr, closeFailure) ||
		strings.Contains(readErr.Error(), "secret") || strings.Contains(closeErr.Error(), "secret") {
		t.Fatalf("owned errors = %v, %v", readErr, closeErr)
	}
	if secondClose := opened.Close(); !errors.Is(secondClose, readFailure) {
		t.Fatalf("second close error = %v", secondClose)
	}
}

func TestMultipartBodyHandlesNilOpenersOverflowAndEarlyClose(t *testing.T) {
	t.Parallel()

	var firstClosed atomic.Int64
	first, _ := NewReplayableBody("text/plain", 1, func() (io.ReadCloser, error) {
		return &responseTestBody{Reader: strings.NewReader("a"), closed: &firstClosed}, nil
	})
	body, err := NewMultipartBody(MultipartOptions{
		Boundary: "nil-open", Parts: []MultipartPart{{Name: "first", Body: first}, {Name: "nil", Body: nilOpeningBody{}}},
	})
	if err != nil {
		t.Fatalf("construct nil opener multipart: %v", err)
	}
	if _, err := body.Open(); !errors.Is(err, ErrInvalidBody) || firstClosed.Load() != 1 {
		t.Fatalf("nil opener error = %v, closes = %d", err, firstClosed.Load())
	}

	huge := &mutableRequestBody{contentLength: math.MaxInt64, opener: func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("")), nil
	}}
	for _, parts := range [][]MultipartPart{
		{{Name: "huge", Body: huge}, {Name: "another", Body: first}},
		{{Name: "huge", Body: huge}},
	} {
		if _, err := NewMultipartBody(MultipartOptions{Boundary: "overflow", Parts: parts}); !errors.Is(err, ErrMultipartLimit) {
			t.Fatalf("overflow error = %v", err)
		}
	}
	if _, err := multipartContentLength("bad\n", nil); !errors.Is(err, ErrInvalidMultipart) {
		t.Fatalf("internal boundary error = %v", err)
	}

	var earlyClosed atomic.Int64
	earlyStream, _ := NewStreamingBody("text/plain", -1, &responseTestBody{
		Reader: strings.NewReader(strings.Repeat("x", 1<<20)), closed: &earlyClosed,
	})
	early, _ := NewMultipartBody(MultipartOptions{
		Boundary: "early", Parts: []MultipartPart{{Name: "part", Body: earlyStream}},
	})
	opened, _ := early.Open()
	if err := opened.Close(); err == nil || earlyClosed.Load() != 1 {
		t.Fatalf("early close error = %v, closes = %d", err, earlyClosed.Load())
	}
}

func TestMultipartCopyAndLimitHelpersPropagateDependencyErrors(t *testing.T) {
	t.Parallel()

	readFailure := errors.New("read")
	if err := copyMultipartPart(io.Discard, &responseErrorReader{err: readFailure}, -1); !errors.Is(err, readFailure) {
		t.Fatalf("unknown copy error = %v", err)
	}
	if err := copyMultipartPart(io.Discard, multipartProbeErrorReader{err: readFailure}, 0); !errors.Is(err, readFailure) {
		t.Fatalf("probe error = %v", err)
	}

	writeFailure := errors.New("write")
	limited := &multipartLimitWriter{destination: multipartErrorWriter{err: writeFailure}, remaining: 1}
	if count, err := limited.Write([]byte("too long")); count != 0 ||
		!errors.Is(err, ErrMultipartLimit) || !errors.Is(err, writeFailure) {
		t.Fatalf("limited write = %d, %v", count, err)
	}
	limited = &multipartLimitWriter{destination: multipartErrorWriter{err: writeFailure}, remaining: 10}
	if _, err := limited.Write([]byte("short")); !errors.Is(err, writeFailure) {
		t.Fatalf("dependency write error = %v", err)
	}

	partBody, _ := NewBytesBody("text/plain", nil)
	part, _ := snapshotMultipartPart(MultipartPart{Name: "part", Body: partBody})
	for _, failAt := range []int{1, 2} {
		dependency := &multipartCallErrorWriter{failAt: failAt, err: writeFailure}
		counter := &multipartCountingWriter{destination: dependency}
		if _, err := measureMultipartContentLength("boundary", []multipartPart{part}, counter); !errors.Is(err, writeFailure) {
			t.Fatalf("measurement failure at call %d = %v", failAt, err)
		}
	}

	_, pipeWriter := io.Pipe()
	if err := writeMultipartBody(pipeWriter, "bad\n", 1, nil, nil); !errors.Is(err, ErrInvalidMultipart) {
		t.Fatalf("stream boundary error = %v", err)
	}
	_ = pipeWriter.Close()

	known, _ := NewBytesBody("text/plain", []byte("a"))
	measured, _ := NewMultipartBody(MultipartOptions{
		Boundary: "closing-limit", Parts: []MultipartPart{{Name: "part", Body: known}},
	})
	unknown, _ := NewReplayableBody("text/plain", -1, func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("a")), nil
	})
	closingLimited, err := NewMultipartBody(MultipartOptions{
		Boundary: "closing-limit", MaximumBytes: measured.ContentLength() - 1,
		Parts: []MultipartPart{{Name: "part", Body: unknown}},
	})
	if err != nil {
		t.Fatalf("construct closing-limited multipart: %v", err)
	}
	opened, _ := closingLimited.Open()
	_, err = io.ReadAll(opened)
	_ = opened.Close()
	if !errors.Is(err, ErrMultipartLimit) {
		t.Fatalf("closing limit error = %v", err)
	}
}

type multipartProbeErrorReader struct{ err error }

func (reader multipartProbeErrorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type multipartErrorWriter struct{ err error }

func (writer multipartErrorWriter) Write([]byte) (int, error) { return 0, writer.err }

type multipartCallErrorWriter struct {
	calls  int
	failAt int
	err    error
}

func (writer *multipartCallErrorWriter) Write(buffer []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failAt {
		return 0, writer.err
	}
	return len(buffer), nil
}
