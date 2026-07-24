package httpclient

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidRange indicates invalid request or response range policy.
	ErrInvalidRange = errors.New("invalid HTTP range policy")
	// ErrRangeMismatch indicates a response does not describe the requested range.
	ErrRangeMismatch = errors.New("HTTP range response mismatch")
	// ErrRangeValidatorMismatch indicates representation identity changed.
	ErrRangeValidatorMismatch = errors.New("HTTP range validator mismatch")
	// ErrRangeRestartRequired indicates the server returned a full representation.
	ErrRangeRestartRequired = errors.New("HTTP range restart required")
)

// RangeValidator identifies one representation for safe continuation. ETag
// must be strong. ETag and LastModified are mutually exclusive.
type RangeValidator struct {
	ETag         string
	LastModified time.Time
}

// RangeOptions configures an immutable range request clone. Length zero means
// continue through the end of the representation.
type RangeOptions struct {
	Offset    int64
	Length    int64
	Validator RangeValidator
}

// RangeResponseOptions describes the request a response must satisfy.
type RangeResponseOptions struct {
	Offset       int64
	Length       int64
	Validator    RangeValidator
	AllowRestart bool
}

// RangeDisposition tells a download controller whether to append, restart, or
// finalize an already complete partial file.
type RangeDisposition uint8

const (
	// RangeContinue indicates a validated 206 response can be appended.
	RangeContinue RangeDisposition = iota
	// RangeRestart indicates a 200 response must replace partial content.
	RangeRestart
	// RangeComplete indicates a 416 response proves offset equals total length.
	RangeComplete
)

// RangeMetadata describes parsed representation byte positions. Total is -1
// when the server did not disclose complete length.
type RangeMetadata struct {
	Start int64
	End   int64
	Total int64
}

// RangeError reports range protocol failure without rendering validators or
// response header values.
type RangeError struct {
	Operation string
	Cause     error
}

// Error implements error.
func (err *RangeError) Error() string { return "HTTP range " + err.Operation + " failed" }

// Unwrap returns the stable range failure.
func (err *RangeError) Unwrap() error { return err.Cause }

// WithRange returns an independent GET or HEAD request with Range and optional
// If-Range headers. It never consumes or aliases a request body.
func WithRange(request *http.Request, options RangeOptions) (*http.Request, error) {
	if request == nil || request.URL == nil ||
		(request.Method != http.MethodGet && request.Method != http.MethodHead) ||
		request.Body != nil && request.Body != http.NoBody {
		return nil, &RangeError{Operation: "request", Cause: ErrInvalidRange}
	}
	if err := validateRangePolicy(options.Offset, options.Length, options.Validator); err != nil {
		return nil, err
	}
	cloned := request.Clone(request.Context())
	value := "bytes=" + strconv.FormatInt(options.Offset, 10) + "-"
	if options.Length > 0 {
		end := options.Offset + options.Length - 1
		value += strconv.FormatInt(end, 10)
	}
	cloned.Header.Set("Range", value)
	cloned.Header.Del("If-Range")
	if options.Validator.ETag != "" {
		cloned.Header.Set("If-Range", options.Validator.ETag)
	} else if !options.Validator.LastModified.IsZero() {
		cloned.Header.Set("If-Range", options.Validator.LastModified.UTC().Format(http.TimeFormat))
	}

	return cloned, nil
}

// ValidateRangeResponse validates protocol metadata without consuming or
// closing response body. The caller retains response ownership on every exit.
func ValidateRangeResponse(
	response *http.Response,
	options RangeResponseOptions,
) (RangeMetadata, RangeDisposition, error) {
	if response == nil || response.Header == nil {
		return RangeMetadata{}, RangeContinue, &RangeError{Operation: "response", Cause: ErrInvalidRange}
	}
	if err := validateRangePolicy(options.Offset, options.Length, options.Validator); err != nil {
		return RangeMetadata{}, RangeContinue, err
	}
	switch response.StatusCode {
	case http.StatusPartialContent:
		metadata, ok := parseContentRange(response.Header.Get("Content-Range"), false)
		if !ok || metadata.Start != options.Offset ||
			options.Length > 0 && metadata.End != options.Offset+options.Length-1 {
			return RangeMetadata{}, RangeContinue, &RangeError{Operation: "response", Cause: ErrRangeMismatch}
		}
		length := metadata.End - metadata.Start + 1
		if response.ContentLength >= 0 && response.ContentLength != length {
			return RangeMetadata{}, RangeContinue, &RangeError{Operation: "response length", Cause: ErrRangeMismatch}
		}
		if !rangeValidatorMatches(response.Header, options.Validator) {
			return RangeMetadata{}, RangeContinue, &RangeError{Operation: "response validator", Cause: ErrRangeValidatorMismatch}
		}
		return metadata, RangeContinue, nil
	case http.StatusOK:
		if !options.AllowRestart {
			return RangeMetadata{}, RangeRestart, &RangeError{Operation: "response", Cause: ErrRangeRestartRequired}
		}
		return RangeMetadata{Start: 0, End: max(response.ContentLength-1, -1), Total: response.ContentLength}, RangeRestart, nil
	case http.StatusRequestedRangeNotSatisfiable:
		metadata, ok := parseContentRange(response.Header.Get("Content-Range"), true)
		if !ok || metadata.Total != options.Offset {
			return RangeMetadata{}, RangeContinue, &RangeError{Operation: "response", Cause: ErrRangeMismatch}
		}
		return metadata, RangeComplete, nil
	default:
		return RangeMetadata{}, RangeContinue, &RangeError{Operation: "response status", Cause: ErrRangeMismatch}
	}
}

func validateRangePolicy(offset int64, length int64, validator RangeValidator) error {
	if offset < 0 || length < 0 || validator.ETag != "" && !validator.LastModified.IsZero() ||
		validator.ETag != "" && !validStrongETag(validator.ETag) ||
		length > 0 && offset+length-1 < offset {
		return &RangeError{Operation: "policy", Cause: ErrInvalidRange}
	}
	return nil
}

func validStrongETag(value string) bool {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for index := 1; index < len(value)-1; index++ {
		if value[index] < 0x21 || value[index] == 0x7f || value[index] == '"' {
			return false
		}
	}
	return true
}

func rangeValidatorMatches(header http.Header, validator RangeValidator) bool {
	if validator.ETag != "" {
		return header.Get("ETag") == validator.ETag
	}
	if validator.LastModified.IsZero() {
		return true
	}
	modified, err := http.ParseTime(header.Get("Last-Modified"))
	return err == nil && modified.Equal(validator.LastModified.Truncate(time.Second))
}

func parseContentRange(value string, unsatisfied bool) (RangeMetadata, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return RangeMetadata{}, false
	}
	rangePart, totalPart, found := strings.Cut(strings.TrimPrefix(value, "bytes "), "/")
	if !found {
		return RangeMetadata{}, false
	}
	total := int64(-1)
	if totalPart != "*" {
		parsed, err := strconv.ParseInt(totalPart, 10, 64)
		if err != nil || parsed < 0 {
			return RangeMetadata{}, false
		}
		total = parsed
	}
	if unsatisfied {
		return RangeMetadata{Start: -1, End: -1, Total: total}, rangePart == "*" && total >= 0
	}
	startPart, endPart, found := strings.Cut(rangePart, "-")
	if !found {
		return RangeMetadata{}, false
	}
	start, startErr := strconv.ParseInt(startPart, 10, 64)
	end, endErr := strconv.ParseInt(endPart, 10, 64)
	if startErr != nil || endErr != nil || start < 0 || end < start || total >= 0 && end >= total {
		return RangeMetadata{}, false
	}
	return RangeMetadata{Start: start, End: end, Total: total}, true
}
