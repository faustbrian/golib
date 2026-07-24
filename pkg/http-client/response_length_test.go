package httpclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestResponseDecodersRejectDeclaredLengthMismatch(t *testing.T) {
	t.Parallel()

	short := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 8,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(strings.NewReader(`{}`)),
	}
	_, err := DecodeJSONResponse[map[string]any](short, DecodeOptions{MaximumBodyBytes: 16})
	var lengthError *ResponseLengthError
	if !errors.As(err, &lengthError) || !errors.Is(err, ErrResponseLength) ||
		lengthError.Expected != 8 || lengthError.Actual != 2 {
		t.Fatalf("short length error = %#v", err)
	}

	long := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 4,
		Header:        http.Header{"Content-Type": {"application/octet-stream"}},
		Body:          io.NopCloser(strings.NewReader("dataextra")),
	}
	_, err = DecodeResponse(long, DecodeOptions{
		MaximumBodyBytes: 16, ExpectedMediaTypes: []string{"application/octet-stream"},
	}, func(reader io.Reader) (string, error) {
		content, readErr := io.ReadAll(reader)
		return string(content), readErr
	})
	if !errors.As(err, &lengthError) || lengthError.Expected != 4 || lengthError.Actual != 9 {
		t.Fatalf("long length error = %#v", err)
	}
}

func TestResponseDecodersDistinguishExplicitZeroUnknownAndExactLength(t *testing.T) {
	t.Parallel()

	decode := DecodeFunc[string](func(reader io.Reader) (string, error) {
		content, err := io.ReadAll(reader)
		return string(content), err
	})
	options := DecodeOptions{ExpectedMediaTypes: []string{"text/plain"}}
	explicitZero := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":   {"text/plain"},
			"Content-Length": {"0"},
		},
		Body: io.NopCloser(strings.NewReader("x")),
	}
	if _, err := DecodeResponse(explicitZero, options, decode); !errors.Is(err, ErrResponseLength) {
		t.Fatalf("explicit zero error = %v", err)
	}

	unknown := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("unknown")),
	}
	if value, err := DecodeResponse(unknown, options, decode); err != nil || value != "unknown" {
		t.Fatalf("unknown length = %q, %v", value, err)
	}
	exact := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 5,
		Header:        http.Header{"Content-Type": {"text/plain"}},
		Body:          io.NopCloser(strings.NewReader("exact")),
	}
	if value, err := DecodeResponse(exact, options, decode); err != nil || value != "exact" {
		t.Fatalf("exact length = %q, %v", value, err)
	}
}

func TestResponseLengthPolicySkipsSemanticEmptyAndRendersSafely(t *testing.T) {
	t.Parallel()

	response := &http.Response{
		StatusCode:    http.StatusNoContent,
		ContentLength: 100,
		Header:        make(http.Header),
		Body:          http.NoBody,
	}
	if _, err := DecodeJSONResponse[any](response, DecodeOptions{}); err != nil {
		t.Fatalf("semantic empty length error = %v", err)
	}
	lengthError := &ResponseLengthError{Expected: 10, Actual: 2}
	if lengthError.Error() == "" || !errors.Is(lengthError, ErrResponseLength) ||
		strings.Contains(lengthError.Error(), "10") || strings.Contains(lengthError.Error(), "2") {
		t.Fatalf("rendered length error = %q", lengthError.Error())
	}
}
