package httpclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDecodeResponseUsesBoundedCallerCodecAndCloses(t *testing.T) {
	t.Parallel()

	var closed atomic.Int64
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/vnd.widget; version=1"}},
		Body:       &responseTestBody{Reader: strings.NewReader("widget"), closed: &closed},
	}
	decoded, err := DecodeResponse(response, DecodeOptions{
		MaximumBodyBytes: 16, ExpectedMediaTypes: []string{"application/vnd.widget"},
	}, func(reader io.Reader) (string, error) {
		content, err := io.ReadAll(reader)
		return strings.ToUpper(string(content)), err
	})
	if err != nil {
		t.Fatalf("decode custom response: %v", err)
	}
	if decoded != "WIDGET" || closed.Load() != 1 {
		t.Fatalf("decoded = %q, closes = %d", decoded, closed.Load())
	}
}

func TestDecodeResponseRejectsTrailingLimitAndCodecFailure(t *testing.T) {
	t.Parallel()

	readFour := func(reader io.Reader) (string, error) {
		buffer := make([]byte, 4)
		_, err := io.ReadFull(reader, buffer)
		return string(buffer), err
	}
	codecFailure := errors.New("codec-secret")
	for _, test := range []struct {
		name    string
		body    string
		maximum int64
		decode  DecodeFunc[string]
		want    error
	}{
		{name: "trailing", body: "dataextra", maximum: 16, decode: readFour, want: ErrTrailingResponseData},
		{name: "limit", body: "dataextra", maximum: 4, decode: readFour, want: ErrResponseBodyLimit},
		{name: "codec", body: "data", maximum: 16, decode: func(io.Reader) (string, error) {
			return "", codecFailure
		}, want: codecFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/octet-stream"}},
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}
			_, err := DecodeResponse(response, DecodeOptions{
				MaximumBodyBytes:   test.maximum,
				ExpectedMediaTypes: []string{"application/octet-stream"},
			}, test.decode)
			if !errors.Is(err, test.want) {
				t.Fatalf("decode error = %v, want %v", err, test.want)
			}
			if test.want == codecFailure {
				var decodeError *ResponseDecodeError
				if !errors.As(err, &decodeError) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("codec error = %v", err)
				}
			}
		})
	}
}

func TestDecodeResponsePolicyEmptyAndTrailingOptIn(t *testing.T) {
	t.Parallel()

	decode := DecodeFunc[string](func(reader io.Reader) (string, error) {
		content, err := io.ReadAll(reader)
		return string(content), err
	})
	if _, err := DecodeResponse[string](nil, DecodeOptions{}, decode); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("nil response error = %v", err)
	}
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}
	if _, err := DecodeResponse(response, DecodeOptions{}, decode); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("missing media policy error = %v", err)
	}
	response.Body = http.NoBody
	if _, err := DecodeResponse[string](response, DecodeOptions{
		ExpectedMediaTypes: []string{"application/octet-stream"},
	}, nil); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("nil codec error = %v", err)
	}

	empty := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/octet-stream"}},
		Body:       http.NoBody,
	}
	if _, err := DecodeResponse(empty, DecodeOptions{
		ExpectedMediaTypes: []string{"application/octet-stream"},
	}, decode); !errors.Is(err, ErrEmptyResponseBody) {
		t.Fatalf("empty codec error = %v", err)
	}
	empty.Body = http.NoBody
	if value, err := DecodeResponse(empty, DecodeOptions{
		ExpectedMediaTypes: []string{"application/octet-stream"}, AllowEmpty: true,
	}, decode); err != nil || value != "" {
		t.Fatalf("allowed empty = %q, %v", value, err)
	}

	trailing := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/octet-stream"}},
		Body:       io.NopCloser(strings.NewReader("dataextra")),
	}
	value, err := DecodeResponse(trailing, DecodeOptions{
		MaximumBodyBytes: 16, ExpectedMediaTypes: []string{"application/octet-stream"},
		AllowTrailingData: true,
	}, func(reader io.Reader) (string, error) {
		buffer := make([]byte, 4)
		_, readErr := io.ReadFull(reader, buffer)
		return string(buffer), readErr
	})
	if err != nil || value != "data" {
		t.Fatalf("allowed trailing = %q, %v", value, err)
	}
}

func TestDecodeResponseLifecycleBoundaries(t *testing.T) {
	t.Parallel()

	expected := []string{"application/octet-stream"}
	decodeAll := DecodeFunc[string](func(reader io.Reader) (string, error) {
		content, err := io.ReadAll(reader)
		return string(content), err
	})
	closeFailure := errors.New("close-secret")
	closing := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/octet-stream"}},
		Body: &responseTestBody{
			Reader: strings.NewReader("data"), closeErr: closeFailure,
		},
	}
	if _, err := DecodeResponse(closing, DecodeOptions{ExpectedMediaTypes: expected}, decodeAll); !errors.Is(err, closeFailure) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("close failure = %v", err)
	}

	invalid := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}
	if _, err := DecodeResponse(invalid, DecodeOptions{
		MaximumBodyBytes: -1, ExpectedMediaTypes: expected,
	}, decodeAll); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("invalid maximum = %v", err)
	}
	semantic := &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}
	if value, err := DecodeResponse(semantic, DecodeOptions{ExpectedMediaTypes: expected}, decodeAll); err != nil || value != "" {
		t.Fatalf("semantic empty = %q, %v", value, err)
	}
	mismatch := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body:       http.NoBody,
	}
	if _, err := DecodeResponse(mismatch, DecodeOptions{ExpectedMediaTypes: expected}, decodeAll); !errors.Is(err, ErrUnexpectedContentType) {
		t.Fatalf("media mismatch = %v", err)
	}

	for _, allow := range []bool{false, true} {
		empty := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/octet-stream"}},
			Body:       http.NoBody,
		}
		_, err := DecodeResponse(empty, DecodeOptions{
			ExpectedMediaTypes: expected, AllowEmpty: allow,
		}, func(io.Reader) (string, error) { return "", io.EOF })
		if allow && err != nil || !allow && !errors.Is(err, ErrEmptyResponseBody) {
			t.Fatalf("EOF empty allow %t = %v", allow, err)
		}
	}

	overflow := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/octet-stream"}},
		Body:       io.NopCloser(strings.NewReader("dataextra")),
	}
	if _, err := DecodeResponse(overflow, DecodeOptions{
		MaximumBodyBytes: 4, ExpectedMediaTypes: expected, AllowTrailingData: true,
	}, func(reader io.Reader) (string, error) {
		buffer := make([]byte, 4)
		_, readErr := io.ReadFull(reader, buffer)
		return string(buffer), readErr
	}); !errors.Is(err, ErrResponseBodyLimit) {
		t.Fatalf("allowed trailing overflow = %v", err)
	}
}

func TestDecodeResponseContainsDecoderPanicAndCloses(t *testing.T) {
	t.Parallel()

	var closed atomic.Int64
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/octet-stream"}},
		Body:       &responseTestBody{Reader: strings.NewReader("secret"), closed: &closed},
	}
	_, err := DecodeResponse(response, DecodeOptions{
		ExpectedMediaTypes: []string{"application/octet-stream"},
	}, func(io.Reader) (string, error) {
		panic("decoder-secret")
	})
	var decodeError *ResponseDecodeError
	if !errors.As(err, &decodeError) || !errors.Is(err, ErrResponseDecoderPanic) {
		t.Fatalf("decoder panic error = %#v", err)
	}
	if strings.Contains(err.Error(), "secret") || closed.Load() != 1 {
		t.Fatalf("decoder panic rendered secret or leaked body: %q, closes = %d", err, closed.Load())
	}
}
