package media_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/media"
)

func TestMultipartFormDataEncodeWritesDeterministicBoundedParts(t *testing.T) {
	t.Parallel()

	body, contentType, err := media.MultipartFormDataEncode(
		[]media.MultipartPart{
			{
				Name:        "profile",
				ContentType: "application/json",
				Headers: []media.MultipartHeader{
					{Name: "X-Trace", Value: "first"},
				},
				Data: []byte(`{"name":"Ada"}`),
			},
			{Name: "tag", Data: []byte("one")},
			{Name: "tag", Data: []byte("two")},
		},
		"openapi-boundary", 5, 2_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "multipart/form-data; boundary=openapi-boundary" {
		t.Fatalf("content type = %q", contentType)
	}
	want := "--openapi-boundary\r\n" +
		"Content-Disposition: form-data; name=\"profile\"\r\n" +
		"Content-Type: application/json\r\n" +
		"X-Trace: first\r\n\r\n" +
		`{"name":"Ada"}` + "\r\n" +
		"--openapi-boundary\r\n" +
		"Content-Disposition: form-data; name=\"tag\"\r\n\r\n" +
		"one\r\n" +
		"--openapi-boundary\r\n" +
		"Content-Disposition: form-data; name=\"tag\"\r\n\r\n" +
		"two\r\n" +
		"--openapi-boundary--\r\n"
	if string(body) != want {
		t.Fatalf("multipart body = %q, want %q", body, want)
	}
}

func TestMultipartMixedEncodeWritesNamedParts(t *testing.T) {
	t.Parallel()

	body, contentType, err := media.MultipartMixedEncode(
		[]media.MultipartPart{
			{Name: "metadata", ContentType: "application/json", Data: []byte(`{}`)},
			{Name: "document", ContentType: "text/plain", Data: []byte("content")},
		},
		"mixed-boundary", 2, 1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "multipart/mixed; boundary=mixed-boundary" {
		t.Fatalf("content type = %q", contentType)
	}
	want := "--mixed-boundary\r\n" +
		"Content-Disposition: form-data; name=\"metadata\"\r\n" +
		"Content-Type: application/json\r\n\r\n{}\r\n" +
		"--mixed-boundary\r\n" +
		"Content-Disposition: form-data; name=\"document\"\r\n" +
		"Content-Type: text/plain\r\n\r\ncontent\r\n" +
		"--mixed-boundary--\r\n"
	if string(body) != want {
		t.Fatalf("multipart body = %q, want %q", body, want)
	}
}

func TestMultipartFormDataEncodeRejectsInvalidInputsAndBounds(t *testing.T) {
	t.Parallel()

	valid := []media.MultipartPart{{Name: "field", Data: []byte("value")}}
	for _, test := range []struct {
		name      string
		parts     []media.MultipartPart
		boundary  string
		maxParts  int
		maxBytes  int
		wantError error
	}{
		{name: "empty boundary", parts: valid, maxParts: 1, maxBytes: 100,
			wantError: media.ErrInvalidMultipartEncoding},
		{name: "invalid boundary", parts: valid, boundary: "bad\n", maxParts: 1,
			maxBytes: 100, wantError: media.ErrInvalidMultipartEncoding},
		{name: "invalid part maximum", boundary: "x", maxBytes: 100,
			wantError: media.ErrInvalidMultipartEncoding},
		{name: "invalid byte maximum", boundary: "x", maxParts: 1,
			wantError: media.ErrInvalidMultipartEncoding},
		{name: "part limit", parts: valid, boundary: "x", maxParts: 1,
			maxBytes: 100, wantError: nil},
		{name: "too many parts", parts: append(valid, valid...), boundary: "x",
			maxParts: 1, maxBytes: 1_000, wantError: media.ErrMultipartEncodingLimit},
		{name: "byte limit", parts: valid, boundary: "x", maxParts: 1,
			maxBytes: 20, wantError: media.ErrMultipartEncodingLimit},
		{name: "invalid name", parts: []media.MultipartPart{{Name: "bad\n"}},
			boundary: "x", maxParts: 1, maxBytes: 100,
			wantError: media.ErrInvalidMultipartEncoding},
		{name: "invalid content type", parts: []media.MultipartPart{{
			Name: "field", ContentType: "bad",
		}}, boundary: "x", maxParts: 1, maxBytes: 100,
			wantError: media.ErrInvalidMultipartEncoding},
		{name: "reserved header", parts: []media.MultipartPart{{
			Name: "field", Headers: []media.MultipartHeader{{
				Name: "content-type", Value: "text/plain",
			}},
		}}, boundary: "x", maxParts: 1, maxBytes: 100,
			wantError: media.ErrInvalidMultipartEncoding},
		{name: "invalid header", parts: []media.MultipartPart{{
			Name: "field", Headers: []media.MultipartHeader{{
				Name: "X-Test", Value: "bad\r\nvalue",
			}},
		}}, boundary: "x", maxParts: 1, maxBytes: 100,
			wantError: media.ErrInvalidMultipartEncoding},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := media.MultipartFormDataEncode(
				test.parts, test.boundary, test.maxParts, test.maxBytes,
			)
			if test.wantError == nil {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func TestMultipartFormDataEncodeEnforcesEveryOutputBoundary(t *testing.T) {
	t.Parallel()

	parts := []media.MultipartPart{{
		Name: "field", Data: []byte("a body long enough to cross each boundary"),
	}}
	body, _, err := media.MultipartFormDataEncode(parts, "x", 1, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	if exact, _, err := media.MultipartFormDataEncode(
		parts, "x", 1, len(body),
	); err != nil || string(exact) != string(body) {
		t.Fatalf("exact multipart byte limit = %q, %v", exact, err)
	}
	for maximum := 1; maximum < len(body); maximum++ {
		_, _, err = media.MultipartFormDataEncode(parts, "x", 1, maximum)
		if !errors.Is(err, media.ErrMultipartEncodingLimit) {
			t.Fatalf("maximum %d error = %v", maximum, err)
		}
	}
}
