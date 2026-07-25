package httpclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDecodeJSONResponseStreamsBoundedStrictDocumentAndCloses(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name string `json:"name"`
	}
	var closed atomic.Int64
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/problem+json; charset=utf-8"}},
		Body:       &responseTestBody{Reader: strings.NewReader(`{"name":"widget"}`), closed: &closed},
	}
	decoded, err := DecodeJSONResponse[payload](response, DecodeOptions{MaximumBodyBytes: 64})
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Name != "widget" || closed.Load() != 1 {
		t.Fatalf("decoded = %#v, closes = %d", decoded, closed.Load())
	}
}

func TestDecodeJSONResponseRejectsMediaTypeLimitAndTrailingData(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		contentType string
		body        string
		maximum     int64
		want        error
	}{
		{
			name: "media type", contentType: "text/plain", body: `{}`,
			maximum: 64, want: ErrUnexpectedContentType,
		},
		{
			name: "limit", contentType: "application/json", body: `{"value":"too large"}`,
			maximum: 4, want: ErrResponseBodyLimit,
		},
		{
			name: "trailing", contentType: "application/json", body: `{} {}`,
			maximum: 64, want: ErrTrailingResponseData,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var closed atomic.Int64
			response := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {test.contentType}},
				Body: &responseTestBody{
					Reader: strings.NewReader(test.body), closed: &closed,
				},
			}
			_, err := DecodeJSONResponse[map[string]any](response, DecodeOptions{
				MaximumBodyBytes: test.maximum,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("decode error = %v, want %v", err, test.want)
			}
			if closed.Load() != 1 {
				t.Fatalf("body closes = %d", closed.Load())
			}
		})
	}
}

func TestDecodeJSONResponseEmptyAndMalformedSemantics(t *testing.T) {
	t.Parallel()

	for _, status := range []int{
		http.StatusNoContent,
		http.StatusResetContent,
		http.StatusNotModified,
	} {
		response := &http.Response{
			StatusCode: status, Header: make(http.Header), Body: http.NoBody,
		}
		if _, err := DecodeJSONResponse[map[string]any](response, DecodeOptions{}); err != nil {
			t.Fatalf("status %d empty decode: %v", status, err)
		}
	}

	empty := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       http.NoBody,
	}
	if _, err := DecodeJSONResponse[map[string]any](empty, DecodeOptions{}); !errors.Is(err, ErrEmptyResponseBody) {
		t.Fatalf("empty response error = %v", err)
	}
	empty.Body = http.NoBody
	if _, err := DecodeJSONResponse[map[string]any](empty, DecodeOptions{AllowEmpty: true}); err != nil {
		t.Fatalf("allowed empty response: %v", err)
	}

	malformed := errors.New("payload-secret")
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       &responseTestBody{Reader: &responseErrorReader{err: malformed}},
	}
	_, err := DecodeJSONResponse[map[string]any](response, DecodeOptions{})
	var decodeError *ResponseDecodeError
	if !errors.As(err, &decodeError) || !errors.Is(err, malformed) ||
		strings.Contains(err.Error(), malformed.Error()) {
		t.Fatalf("malformed response error = %#v", err)
	}
}

func TestDecodeJSONResponsePreservesCloseFailure(t *testing.T) {
	t.Parallel()

	closeFailure := errors.New("close-secret")
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body: &responseTestBody{
			Reader: strings.NewReader(`{}`), closeErr: closeFailure,
		},
	}
	_, err := DecodeJSONResponse[map[string]any](response, DecodeOptions{})
	var bodyError *ResponseBodyError
	if !errors.As(err, &bodyError) || !errors.Is(err, closeFailure) ||
		strings.Contains(err.Error(), closeFailure.Error()) {
		t.Fatalf("close error = %#v", err)
	}
}

func TestDecodeJSONResponsePolicyAndReaderBoundaries(t *testing.T) {
	t.Parallel()

	if (&ResponseLimitError{}).Error() == "" || (&UnexpectedContentTypeError{}).Error() == "" {
		t.Fatal("typed response errors rendered empty text")
	}
	if _, err := DecodeJSONResponse[any](nil, DecodeOptions{}); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("nil response error = %v", err)
	}
	if _, err := DecodeJSONResponse[any](&http.Response{}, DecodeOptions{}); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("nil body error = %v", err)
	}

	var closed atomic.Int64
	invalidOptions := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: &responseTestBody{Reader: strings.NewReader(`{}`), closed: &closed},
	}
	if _, err := DecodeJSONResponse[any](invalidOptions, DecodeOptions{MaximumBodyBytes: -1}); !errors.Is(err, ErrInvalidResponsePolicy) || closed.Load() != 1 {
		t.Fatalf("invalid maximum error = %v, closes = %d", err, closed.Load())
	}
	invalidMedia := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody,
	}
	if _, err := DecodeJSONResponse[any](invalidMedia, DecodeOptions{
		ExpectedMediaTypes: []string{"not a media type"},
	}); !errors.Is(err, ErrInvalidResponsePolicy) {
		t.Fatalf("invalid expected media type error = %v", err)
	}

	request, _ := http.NewRequest(http.MethodHead, "https://example.test", nil)
	head := &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody,
		Request: request,
	}
	if _, err := DecodeJSONResponse[any](head, DecodeOptions{}); err != nil {
		t.Fatalf("HEAD decode error = %v", err)
	}

	allowedTrailing := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{} ignored`)),
	}
	if _, err := DecodeJSONResponse[map[string]any](allowedTrailing, DecodeOptions{
		MaximumBodyBytes: 64, AllowTrailingData: true,
	}); err != nil {
		t.Fatalf("allowed trailing data error = %v", err)
	}
	malformedTrailing := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{} invalid`)),
	}
	if _, err := DecodeJSONResponse[map[string]any](malformedTrailing, DecodeOptions{
		MaximumBodyBytes: 64,
	}); err == nil {
		t.Fatal("malformed trailing data succeeded")
	}
	overflowTrailing := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{}12345`)),
	}
	if _, err := DecodeJSONResponse[map[string]any](overflowTrailing, DecodeOptions{
		MaximumBodyBytes: 2, AllowTrailingData: true,
	}); !errors.Is(err, ErrResponseBodyLimit) {
		t.Fatalf("allowed trailing limit error = %v", err)
	}

	reader := &boundedResponseReader{
		reader: strings.NewReader("x"), remaining: 1, limit: 1, expected: 1,
	}
	if count, err := reader.Read(nil); count != 0 || err != nil {
		t.Fatalf("zero read = %d, %v", count, err)
	}
	buffer := make([]byte, 8)
	if count, err := reader.Read(buffer); count != 1 || err != nil {
		t.Fatalf("bounded read = %d, %v", count, err)
	}
	if count, err := reader.Read(buffer); count != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("bounded EOF = %d, %v", count, err)
	}
}

type responseTestBody struct {
	io.Reader
	closed   *atomic.Int64
	closeErr error
}

func (body *responseTestBody) Close() error {
	if body.closed != nil {
		body.closed.Add(1)
	}
	return body.closeErr
}

type responseErrorReader struct{ err error }

func (reader *responseErrorReader) Read([]byte) (int, error) { return 0, reader.err }
