package httpclient

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

var (
	// ErrInvalidStatusPolicy indicates invalid response classification policy.
	ErrInvalidStatusPolicy = errors.New("invalid HTTP status policy")
	// ErrHTTPStatus indicates a response status rejected by caller policy.
	ErrHTTPStatus = errors.New("HTTP response status rejected")
)

const (
	defaultMaximumStatusDrainBytes = 64 << 10
	maximumStatusExcerptBytes      = 1 << 20
	maximumStatusDrainBytes        = 16 << 20
)

// StatusSnapshot supplies bounded redacted response state to vendor mapping.
type StatusSnapshot struct {
	StatusCode int
	Header     http.Header
	Excerpt    []byte
	RequestID  string
}

// ExcerptRedactor returns an independent safe excerpt. It must not retain its
// input, which can contain sensitive response data.
type ExcerptRedactor func([]byte) ([]byte, error)

// VendorErrorMapper maps safe bounded response state to a stable vendor code
// and optional cause.
type VendorErrorMapper func(StatusSnapshot) (string, error)

// StatusOptions configures independent status classification.
type StatusOptions struct {
	Accept              func(int) bool
	MaximumExcerptBytes int64
	MaximumDrainBytes   int64
	RedactExcerpt       ExcerptRedactor
	Retryable           func(int, http.Header) bool
	MapVendorError      VendorErrorMapper
	RequestIDHeaders    []string
}

// HTTPStatusError preserves safe structured response state. Error text never
// renders headers, excerpts, vendor messages, request IDs, or causes.
type HTTPStatusError struct {
	StatusCode int
	Header     http.Header
	VendorCode string
	Excerpt    []byte
	RequestID  string
	Retryable  bool
	Cause      error
}

// Error implements error.
func (*HTTPStatusError) Error() string { return "HTTP response status rejected" }

// Unwrap preserves the stable category and optional vendor cause.
func (err *HTTPStatusError) Unwrap() []error {
	causes := []error{ErrHTTPStatus}
	if err.Cause != nil {
		causes = append(causes, err.Cause)
	}
	return causes
}

// ClassifyResponse returns nil for an accepted response without touching its
// body. A rejected response is boundedly consumed and always closed.
func ClassifyResponse(response *http.Response, options StatusOptions) (resultErr error) {
	if response == nil || response.Body == nil || response.Header == nil {
		return fmt.Errorf("%w: response state is invalid", ErrInvalidStatusPolicy)
	}
	accept := options.Accept
	if accept == nil {
		accept = func(status int) bool { return status >= 200 && status < 300 }
	}
	if accept(response.StatusCode) {
		return nil
	}
	body := response.Body
	defer func() {
		if closeErr := body.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, &ResponseBodyError{Operation: "close", Cause: closeErr})
		}
	}()
	if options.MaximumExcerptBytes < 0 || options.MaximumExcerptBytes > maximumStatusExcerptBytes ||
		options.MaximumDrainBytes < 0 || options.MaximumDrainBytes > maximumStatusDrainBytes ||
		options.MaximumExcerptBytes > 0 && options.RedactExcerpt == nil {
		return fmt.Errorf("%w: body bounds or redactor are invalid", ErrInvalidStatusPolicy)
	}
	drain := options.MaximumDrainBytes
	if drain == 0 {
		drain = defaultMaximumStatusDrainBytes
	}
	var excerpt []byte
	if options.MaximumExcerptBytes > 0 {
		content, readErr := io.ReadAll(io.LimitReader(body, options.MaximumExcerptBytes))
		excerpt = append([]byte(nil), content...)
		if readErr != nil {
			resultErr = errors.Join(resultErr, &ResponseBodyError{Operation: "excerpt read", Cause: readErr})
		}
		redacted, redactErr := options.RedactExcerpt(append([]byte(nil), excerpt...))
		if redactErr != nil {
			resultErr = errors.Join(resultErr, &ResponseBodyError{Operation: "excerpt redaction", Cause: redactErr})
			excerpt = nil
		} else if int64(len(redacted)) > options.MaximumExcerptBytes {
			resultErr = errors.Join(resultErr, &ResponseBodyError{Operation: "excerpt redaction", Cause: ErrInvalidStatusPolicy})
			excerpt = nil
		} else {
			excerpt = append([]byte(nil), redacted...)
		}
	}
	if _, drainErr := io.Copy(io.Discard, io.LimitReader(body, drain+1)); drainErr != nil {
		resultErr = errors.Join(resultErr, &ResponseBodyError{Operation: "drain", Cause: drainErr})
	}
	requestIDHeaders := options.RequestIDHeaders
	if len(requestIDHeaders) == 0 {
		requestIDHeaders = []string{"X-Request-ID", "X-Correlation-ID"}
	}
	requestID := ""
	for _, name := range requestIDHeaders {
		if value := response.Header.Get(name); value != "" {
			requestID = value
			break
		}
	}
	snapshot := StatusSnapshot{
		StatusCode: response.StatusCode, Header: response.Header.Clone(),
		Excerpt: append([]byte(nil), excerpt...), RequestID: requestID,
	}
	statusError := &HTTPStatusError{
		StatusCode: response.StatusCode, Header: response.Header.Clone(),
		Excerpt: append([]byte(nil), excerpt...), RequestID: requestID,
	}
	if options.Retryable != nil {
		statusError.Retryable = options.Retryable(response.StatusCode, response.Header.Clone())
	}
	if options.MapVendorError != nil {
		statusError.VendorCode, statusError.Cause = options.MapVendorError(snapshot)
	}

	return errors.Join(statusError, resultErr)
}
