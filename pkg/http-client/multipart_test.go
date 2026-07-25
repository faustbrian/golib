package httpclient

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMultipartBodyIsDeterministicReplayableAndLengthExact(t *testing.T) {
	t.Parallel()

	metadata, _ := NewBytesBody("application/json", []byte(`{"name":"widget"}`))
	file, _ := NewBytesBody("text/plain", []byte("file content"))
	body, err := NewMultipartBody(MultipartOptions{
		Boundary: "deterministic-boundary", MaximumBytes: 1024,
		Parts: []MultipartPart{
			{Name: "metadata", Body: metadata, Header: http.Header{"X-Part": {"one"}}},
			{Name: "attachment", FileName: "widget.txt", Body: file},
		},
	})
	if err != nil {
		t.Fatalf("construct multipart body: %v", err)
	}
	if !body.Replayable() || body.ContentLength() <= 0 {
		t.Fatalf("multipart replay = %t, length = %d", body.Replayable(), body.ContentLength())
	}
	mediaType, parameters, err := mime.ParseMediaType(body.ContentType())
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] != "deterministic-boundary" {
		t.Fatalf("content type = %q, %v", body.ContentType(), err)
	}
	first := readMultipartBody(t, body)
	second := readMultipartBody(t, body)
	if !bytes.Equal(first, second) || int64(len(first)) != body.ContentLength() {
		t.Fatalf("multipart lengths = %d, %d, declared %d", len(first), len(second), body.ContentLength())
	}
	reader := multipart.NewReader(bytes.NewReader(first), parameters["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatalf("read metadata part: %v", err)
	}
	metadataContent, _ := io.ReadAll(part)
	if part.FormName() != "metadata" || part.FileName() != "" || part.Header.Get("X-Part") != "one" ||
		part.Header.Get("Content-Type") != "application/json" || string(metadataContent) != `{"name":"widget"}` {
		t.Fatalf("metadata part = %#v, %q", part, metadataContent)
	}
	part, err = reader.NextPart()
	if err != nil {
		t.Fatalf("read file part: %v", err)
	}
	fileContent, _ := io.ReadAll(part)
	if part.FormName() != "attachment" || part.FileName() != "widget.txt" || string(fileContent) != "file content" {
		t.Fatalf("file part = %#v, %q", part, fileContent)
	}
}

func TestMultipartBodyPreservesOneShotStreamingAndOwnership(t *testing.T) {
	t.Parallel()

	var closed atomic.Int64
	stream, _ := NewStreamingBody("application/octet-stream", -1, &responseTestBody{
		Reader: strings.NewReader("stream"), closed: &closed,
	})
	body, err := NewMultipartBody(MultipartOptions{
		Boundary: "stream-boundary", MaximumBytes: 1024,
		Parts: []MultipartPart{{Name: "stream", FileName: "stream.bin", Body: stream}},
	})
	if err != nil {
		t.Fatalf("construct streaming multipart: %v", err)
	}
	if body.Replayable() || body.ContentLength() != -1 {
		t.Fatalf("stream multipart replay = %t, length = %d", body.Replayable(), body.ContentLength())
	}
	content := readMultipartBody(t, body)
	if !bytes.Contains(content, []byte("stream")) || closed.Load() != 1 {
		t.Fatalf("stream content = %q, closes = %d", content, closed.Load())
	}
	if _, err := body.Open(); !errors.Is(err, ErrBodyConsumed) {
		t.Fatalf("second open error = %v", err)
	}
}

func TestMultipartBodyEnforcesTotalAndPartLengths(t *testing.T) {
	t.Parallel()

	known, _ := NewBytesBody("text/plain", []byte("content"))
	if _, err := NewMultipartBody(MultipartOptions{
		Boundary: "small", MaximumBytes: 8,
		Parts: []MultipartPart{{Name: "part", Body: known}},
	}); !errors.Is(err, ErrMultipartLimit) {
		t.Fatalf("known limit error = %v", err)
	}

	for _, test := range []struct {
		name   string
		length int64
		body   string
		want   error
	}{
		{name: "short", length: 5, body: "four", want: ErrMultipartPartLength},
		{name: "long", length: 3, body: "four", want: ErrMultipartPartLength},
	} {
		t.Run(test.name, func(t *testing.T) {
			partBody, _ := NewReplayableBody("text/plain", test.length, func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(test.body)), nil
			})
			body, err := NewMultipartBody(MultipartOptions{
				Boundary: "length", MaximumBytes: 1024,
				Parts: []MultipartPart{{Name: "part", Body: partBody}},
			})
			if err != nil {
				t.Fatalf("construct multipart: %v", err)
			}
			opened, err := body.Open()
			if err != nil {
				t.Fatalf("open multipart: %v", err)
			}
			_, err = io.ReadAll(opened)
			_ = opened.Close()
			if !errors.Is(err, test.want) {
				t.Fatalf("multipart read error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestMultipartBodyRejectsInvalidMetadataAndClosesOpenedParts(t *testing.T) {
	t.Parallel()

	valid, _ := NewBytesBody("text/plain", []byte("valid"))
	for _, options := range []MultipartOptions{
		{},
		{Boundary: "boundary", Parts: []MultipartPart{{Name: "", Body: valid}}},
		{Boundary: "boundary", Parts: []MultipartPart{{Name: "bad\r\nname", Body: valid}}},
		{
			Boundary: "boundary",
			Parts: []MultipartPart{{
				Name:   "part",
				Body:   valid,
				Header: http.Header{"Content-Disposition": {"bad"}},
			}},
		},
	} {
		if _, err := NewMultipartBody(options); !errors.Is(err, ErrInvalidMultipart) {
			t.Fatalf("invalid multipart options %#v error = %v", options, err)
		}
	}

	var closed atomic.Int64
	first, _ := NewReplayableBody("text/plain", 1, func() (io.ReadCloser, error) {
		return &responseTestBody{Reader: strings.NewReader("a"), closed: &closed}, nil
	})
	failure := errors.New("part-secret")
	second, _ := NewReplayableBody("text/plain", 1, func() (io.ReadCloser, error) {
		return nil, failure
	})
	body, err := NewMultipartBody(MultipartOptions{
		Boundary: "open-failure", MaximumBytes: 1024,
		Parts: []MultipartPart{{Name: "first", Body: first}, {Name: "second", Body: second}},
	})
	if err != nil {
		t.Fatalf("construct multipart: %v", err)
	}
	_, err = body.Open()
	var multipartError *MultipartError
	if !errors.As(err, &multipartError) || !errors.Is(err, failure) || closed.Load() != 1 ||
		strings.Contains(err.Error(), failure.Error()) {
		t.Fatalf("multipart open error = %#v, closes = %d", err, closed.Load())
	}
}

func readMultipartBody(t *testing.T, body RequestBody) []byte {
	t.Helper()
	opened, err := body.Open()
	if err != nil {
		t.Fatalf("open multipart: %v", err)
	}
	content, err := io.ReadAll(opened)
	if err != nil {
		t.Fatalf("read multipart: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return content
}
