package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

var (
	// ErrInvalidResponsePolicy indicates invalid decoding configuration or state.
	ErrInvalidResponsePolicy = errors.New("invalid HTTP response policy")
	// ErrResponseBodyLimit indicates that a response exceeded its finite bound.
	ErrResponseBodyLimit = errors.New("HTTP response body limit reached")
	// ErrUnexpectedContentType indicates an incompatible response media type.
	ErrUnexpectedContentType = errors.New("unexpected HTTP response content type")
	// ErrTrailingResponseData indicates more than one encoded document.
	ErrTrailingResponseData = errors.New("trailing HTTP response data")
	// ErrEmptyResponseBody indicates a required representation was absent.
	ErrEmptyResponseBody = errors.New("empty HTTP response body")
	// ErrResponseLength indicates a declared response length mismatch.
	ErrResponseLength = errors.New("HTTP response content length mismatch")
	// ErrResponseDecoderPanic indicates that a caller decoder panicked.
	ErrResponseDecoderPanic = errors.New("HTTP response decoder panicked")
)

const (
	defaultMaximumDecodeBytes = 8 << 20
	maximumDecodeBytes        = 1 << 30
)

// DecodeOptions configures bounded response decoding. JSON decoding rejects
// trailing documents unless AllowTrailingData is explicit.
type DecodeOptions struct {
	MaximumBodyBytes   int64
	ExpectedMediaTypes []string
	AllowEmpty         bool
	AllowTrailingData  bool
}

// DecodeFunc decodes one complete typed representation from a bounded response
// stream. It must consume the complete representation so trailing-data policy
// can inspect any unread bytes.
type DecodeFunc[T any] func(io.Reader) (T, error)

// ResponseLimitError reports a finite response bound without response data.
type ResponseLimitError struct{ Limit int64 }

// Error implements error.
func (*ResponseLimitError) Error() string { return "HTTP response body limit reached" }

// Unwrap returns the stable limit sentinel.
func (*ResponseLimitError) Unwrap() error { return ErrResponseBodyLimit }

// ResponseLengthError reports declared and observed response byte counts.
type ResponseLengthError struct {
	Expected int64
	Actual   int64
}

// Error implements error without rendering response-derived values.
func (*ResponseLengthError) Error() string {
	return "HTTP response content length mismatch"
}

// Unwrap returns the stable response-length sentinel.
func (*ResponseLengthError) Unwrap() error {
	return ErrResponseLength
}

// UnexpectedContentTypeError reports parsed media-type mismatch. The fields
// contain media types only, never body, URL, query, or credential data.
type UnexpectedContentTypeError struct {
	Actual   string
	Expected []string
}

// Error implements error.
func (*UnexpectedContentTypeError) Error() string {
	return "unexpected HTTP response content type"
}

// Unwrap returns the stable content-type sentinel.
func (*UnexpectedContentTypeError) Unwrap() error { return ErrUnexpectedContentType }

// ResponseDecodeError reports codec or reader failure without rendering its
// cause, which may contain response data.
type ResponseDecodeError struct{ Cause error }

// Error implements error.
func (*ResponseDecodeError) Error() string { return "HTTP response decode failed" }

// Unwrap returns the decoding failure.
func (err *ResponseDecodeError) Unwrap() error { return err.Cause }

// ResponseBodyError reports response body lifecycle failure without rendering
// the underlying error.
type ResponseBodyError struct {
	Operation string
	Cause     error
}

// Error implements error.
func (err *ResponseBodyError) Error() string {
	return "HTTP response body " + err.Operation + " failed"
}

// Unwrap returns the body lifecycle failure.
func (err *ResponseBodyError) Unwrap() error { return err.Cause }

// DecodeResponse decodes one bounded representation with a caller-selected
// codec and always closes the response body. ExpectedMediaTypes is required;
// status classification remains a separate caller policy.
func DecodeResponse[T any](
	response *http.Response,
	options DecodeOptions,
	decode DecodeFunc[T],
) (value T, resultErr error) {
	if response == nil || response.Body == nil {
		return value, fmt.Errorf("%w: response or body is nil", ErrInvalidResponsePolicy)
	}
	body := response.Body
	defer func() {
		if closeErr := body.Close(); closeErr != nil {
			wrapped := &ResponseBodyError{Operation: "close", Cause: closeErr}
			resultErr = errors.Join(resultErr, wrapped)
		}
	}()
	if nilLike(decode) {
		return value, fmt.Errorf("%w: response decoder is nil", ErrInvalidResponsePolicy)
	}
	maximum, expected, err := resolveCustomDecodeOptions(options)
	if err != nil {
		return value, err
	}
	if responseSemanticallyEmpty(response) {
		return value, nil
	}
	actual, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if parseErr != nil || !acceptedMediaType(actual, expected) {
		return value, &UnexpectedContentTypeError{
			Actual: strings.ToLower(actual), Expected: append([]string(nil), expected...),
		}
	}

	reader := &boundedResponseReader{
		reader: body, remaining: maximum, limit: maximum,
		expected: expectedResponseLength(response),
	}
	value, err = invokeResponseDecoder(decode, reader)
	if err != nil {
		if errors.Is(err, io.EOF) && reader.read == 0 {
			if options.AllowEmpty {
				return value, nil
			}
			return value, ErrEmptyResponseBody
		}
		return value, wrapResponseDecodeError(err)
	}
	if reader.read == 0 {
		if options.AllowEmpty {
			return value, nil
		}
		return value, ErrEmptyResponseBody
	}
	if options.AllowTrailingData {
		if _, copyErr := io.Copy(io.Discard, reader); copyErr != nil {
			return value, wrapResponseDecodeError(copyErr)
		}
		return value, nil
	}
	var trailing [1]byte
	count, trailingErr := reader.Read(trailing[:])
	if count > 0 {
		return value, ErrTrailingResponseData
	}
	if trailingErr != nil && !errors.Is(trailingErr, io.EOF) {
		return value, wrapResponseDecodeError(trailingErr)
	}

	return value, nil
}

func invokeResponseDecoder[T any](decode DecodeFunc[T], reader io.Reader) (value T, err error) {
	defer func() {
		if recover() != nil {
			err = ErrResponseDecoderPanic
		}
	}()

	return decode(reader)
}

// DecodeJSONResponse decodes one bounded JSON document and always closes the
// response body. Status classification remains a separate caller policy.
func DecodeJSONResponse[T any](
	response *http.Response,
	options DecodeOptions,
) (value T, resultErr error) {
	if response == nil || response.Body == nil {
		return value, fmt.Errorf("%w: response or body is nil", ErrInvalidResponsePolicy)
	}
	body := response.Body
	defer func() {
		if closeErr := body.Close(); closeErr != nil {
			wrapped := &ResponseBodyError{Operation: "close", Cause: closeErr}
			resultErr = errors.Join(resultErr, wrapped)
		}
	}()
	maximum, expected, err := resolveDecodeOptions(options)
	if err != nil {
		return value, err
	}
	if responseSemanticallyEmpty(response) {
		return value, nil
	}
	actual, _, parseErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if parseErr != nil || !acceptedMediaType(actual, expected) {
		return value, &UnexpectedContentTypeError{
			Actual: strings.ToLower(actual), Expected: append([]string(nil), expected...),
		}
	}
	reader := &boundedResponseReader{
		reader: body, remaining: maximum, limit: maximum,
		expected: expectedResponseLength(response),
	}
	decoder := json.NewDecoder(reader)
	if decodeErr := decoder.Decode(&value); decodeErr != nil {
		if errors.Is(decodeErr, io.EOF) {
			if options.AllowEmpty {
				return value, nil
			}
			return value, ErrEmptyResponseBody
		}
		return value, wrapResponseDecodeError(decodeErr)
	}
	if options.AllowTrailingData {
		if _, copyErr := io.Copy(io.Discard, reader); copyErr != nil {
			return value, wrapResponseDecodeError(copyErr)
		}
		return value, nil
	}
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	if trailingErr == nil {
		return value, ErrTrailingResponseData
	}
	if !errors.Is(trailingErr, io.EOF) {
		return value, wrapResponseDecodeError(trailingErr)
	}

	return value, nil
}

func resolveCustomDecodeOptions(options DecodeOptions) (int64, []string, error) {
	if len(options.ExpectedMediaTypes) == 0 {
		return 0, nil, fmt.Errorf("%w: expected media types are required", ErrInvalidResponsePolicy)
	}
	return resolveDecodeOptions(options)
}

func resolveDecodeOptions(options DecodeOptions) (int64, []string, error) {
	maximum := options.MaximumBodyBytes
	if maximum == 0 {
		maximum = defaultMaximumDecodeBytes
	}
	if maximum < 1 || maximum > maximumDecodeBytes {
		return 0, nil, fmt.Errorf("%w: maximum body is invalid", ErrInvalidResponsePolicy)
	}
	expected := append([]string(nil), options.ExpectedMediaTypes...)
	if len(expected) == 0 {
		expected = []string{"application/json", "application/*+json"}
	}
	for index, mediaType := range expected {
		normalized := strings.ToLower(strings.TrimSpace(mediaType))
		if normalized == "application/*+json" {
			expected[index] = normalized
			continue
		}
		parsed, _, err := mime.ParseMediaType(normalized)
		if err != nil || parsed == "" {
			return 0, nil, fmt.Errorf("%w: expected media type is invalid", ErrInvalidResponsePolicy)
		}
		expected[index] = strings.ToLower(parsed)
	}

	return maximum, expected, nil
}

func acceptedMediaType(actual string, expected []string) bool {
	actual = strings.ToLower(actual)
	for _, candidate := range expected {
		if actual == candidate {
			return true
		}
		if candidate == "application/*+json" && strings.HasPrefix(actual, "application/") &&
			strings.HasSuffix(actual, "+json") {
			return true
		}
	}

	return false
}

func responseSemanticallyEmpty(response *http.Response) bool {
	if response.Request != nil && response.Request.Method == http.MethodHead {
		return true
	}
	return response.StatusCode >= 100 && response.StatusCode < 200 ||
		response.StatusCode == http.StatusNoContent ||
		response.StatusCode == http.StatusResetContent ||
		response.StatusCode == http.StatusNotModified
}

func expectedResponseLength(response *http.Response) int64 {
	if response.ContentLength != 0 || response.Header.Get("Content-Length") != "" {
		return response.ContentLength
	}
	return -1
}

func wrapResponseDecodeError(err error) error {
	if errors.Is(err, ErrResponseBodyLimit) {
		return err
	}
	return &ResponseDecodeError{Cause: err}
}

type boundedResponseReader struct {
	reader    io.Reader
	remaining int64
	limit     int64
	read      int64
	expected  int64
}

func (reader *boundedResponseReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if reader.remaining == 0 {
		var probe [1]byte
		count, err := reader.reader.Read(probe[:])
		if count > 0 {
			return 0, &ResponseLimitError{Limit: reader.limit}
		}
		return 0, reader.validateLength(err)
	}
	if int64(len(buffer)) > reader.remaining {
		buffer = buffer[:reader.remaining]
	}
	count, err := reader.reader.Read(buffer)
	reader.remaining -= int64(count)
	reader.read += int64(count)

	return count, reader.validateLength(err)
}

func (reader *boundedResponseReader) validateLength(err error) error {
	if errors.Is(err, io.EOF) && reader.expected >= 0 && reader.read != reader.expected {
		return &ResponseLengthError{Expected: reader.expected, Actual: reader.read}
	}
	return err
}
