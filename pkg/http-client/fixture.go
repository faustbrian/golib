package httpclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// FixtureSchemaVersion is the current persisted fixture schema.
	FixtureSchemaVersion       = 1
	defaultFixtureMaximumBody  = 1 << 20
	maximumFixtureBody         = 64 << 20
	maximumFixtureInteractions = 100_000
)

var (
	// ErrInvalidFixture indicates malformed fixture data or policy.
	ErrInvalidFixture = errors.New("invalid HTTP fixture")
	// ErrFixtureUnmatched indicates that the next interaction did not match.
	ErrFixtureUnmatched = errors.New("HTTP fixture interaction unmatched")
	// ErrFixtureUnused indicates strict verification found remaining work.
	ErrFixtureUnused = errors.New("HTTP fixture interactions unused")
	// ErrFixtureBodyLimit indicates a configured capture bound was exceeded.
	ErrFixtureBodyLimit = errors.New("HTTP fixture body limit exceeded")
	// ErrFixtureTimeout indicates a scripted timeout.
	ErrFixtureTimeout = errors.New("HTTP fixture timeout")
	// ErrFixtureTransport indicates a scripted generic transport failure.
	ErrFixtureTransport = errors.New("HTTP fixture transport failure")
	// ErrFixtureMalformedResponse indicates a scripted malformed response.
	ErrFixtureMalformedResponse = errors.New("HTTP fixture malformed response")
)

// FixtureFailure is a stable persisted pre-response failure category.
type FixtureFailure string

const (
	// FixtureFailureTimeout replays a net.Error timeout.
	FixtureFailureTimeout FixtureFailure = "timeout"
	// FixtureFailureCanceled replays context cancellation.
	FixtureFailureCanceled FixtureFailure = "canceled"
	// FixtureFailureTransport replays a generic transport failure.
	FixtureFailureTransport FixtureFailure = "transport"
	// FixtureFailureMalformedResponse replays malformed wire behavior.
	FixtureFailureMalformedResponse FixtureFailure = "malformed_response"
)

// FixtureBodyFailure is a stable persisted response-read failure category.
type FixtureBodyFailure string

const (
	// FixtureBodyFailureUnexpectedEOF returns partial bytes then io.ErrUnexpectedEOF.
	FixtureBodyFailureUnexpectedEOF FixtureBodyFailure = "unexpected_eof"
)

// Fixture is one versioned deterministic HTTP interaction sequence.
type Fixture struct {
	SchemaVersion int                  `json:"schema_version"`
	RecordedAt    time.Time            `json:"recorded_at"`
	ExpiresAt     time.Time            `json:"expires_at,omitempty"`
	Match         FixtureMatchPolicy   `json:"match"`
	Interactions  []FixtureInteraction `json:"interactions"`
}

// FixtureMatchPolicy persists deterministic sanitized matching behavior.
type FixtureMatchPolicy struct {
	Headers                 []string `json:"headers,omitempty"`
	RedactedQueryParameters []string `json:"redacted_query_parameters,omitempty"`
}

// FixtureInteraction pairs one canonical request with one replay response.
type FixtureInteraction struct {
	Request  FixtureRequest  `json:"request"`
	Response FixtureResponse `json:"response"`
}

// FixtureRequest contains bounded match material.
type FixtureRequest struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Header http.Header `json:"header,omitempty"`
	Body   []byte      `json:"body,omitempty"`
	// BodySHA256 matches a body without persisting its contents.
	BodySHA256 string `json:"body_sha256,omitempty"`
}

// FixtureResponse contains a bounded response snapshot.
type FixtureResponse struct {
	StatusCode    int                `json:"status_code,omitempty"`
	Header        http.Header        `json:"header,omitempty"`
	Body          []byte             `json:"body,omitempty"`
	Trailer       http.Header        `json:"trailer,omitempty"`
	ContentLength *int64             `json:"content_length,omitempty"`
	Failure       FixtureFailure     `json:"failure,omitempty"`
	BodyFailure   FixtureBodyFailure `json:"body_failure,omitempty"`
}

// FixtureReplayError is a stable secret-safe scripted transport failure.
type FixtureReplayError struct{ Kind FixtureFailure }

// Error implements error without rendering request or recorded error data.
func (*FixtureReplayError) Error() string { return "HTTP fixture replay failed" }

// Unwrap returns the stable failure category.
func (err *FixtureReplayError) Unwrap() error {
	switch err.Kind {
	case FixtureFailureTimeout:
		return ErrFixtureTimeout
	case FixtureFailureCanceled:
		return context.Canceled
	case FixtureFailureTransport:
		return ErrFixtureTransport
	case FixtureFailureMalformedResponse:
		return ErrFixtureMalformedResponse
	default:
		return ErrInvalidFixture
	}
}

// Timeout reports only explicit timeout fixtures.
func (err *FixtureReplayError) Timeout() bool { return err.Kind == FixtureFailureTimeout }

// Temporary returns false because fixtures do not imply retry safety.
func (*FixtureReplayError) Temporary() bool { return false }

// FixtureError identifies only the interaction index and stable cause.
type FixtureError struct {
	Interaction int
	Cause       error
}

// Error implements error without rendering request or fixture data.
func (*FixtureError) Error() string { return "HTTP fixture operation failed" }

// Unwrap returns the stable fixture cause.
func (err *FixtureError) Unwrap() error { return err.Cause }

// ReplayOptions configures deterministic bounded request matching.
type ReplayOptions struct {
	MatchHeaders     []string
	MaximumBodyBytes int64
}

// ReplayTransport replays one immutable ordered fixture safely across callers.
type ReplayTransport struct {
	interactions  []FixtureInteraction
	matchHeaders  []string
	maximumBody   int64
	redactedQuery map[string]struct{}
	mu            sync.Mutex
	next          int
}

// NewReplayTransport validates and snapshots a replay fixture.
func NewReplayTransport(fixture Fixture, options ReplayOptions) (*ReplayTransport, error) {
	maximumBody := options.MaximumBodyBytes
	if maximumBody == 0 {
		maximumBody = defaultFixtureMaximumBody
	}
	if maximumBody < 1 || maximumBody > maximumFixtureBody ||
		fixture.SchemaVersion != FixtureSchemaVersion ||
		len(fixture.Interactions) > maximumFixtureInteractions {
		return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	configuredHeaders := options.MatchHeaders
	if len(fixture.Match.Headers) > 0 {
		configuredHeaders = fixture.Match.Headers
	}
	matchHeaders, err := canonicalFixtureHeaderNames(configuredHeaders)
	if err != nil {
		return nil, err
	}
	if len(options.MatchHeaders) > 0 && len(fixture.Match.Headers) > 0 {
		override, overrideErr := canonicalFixtureHeaderNames(options.MatchHeaders)
		if overrideErr != nil || !equalStrings(override, matchHeaders) {
			return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
	}
	redactedQuery, err := canonicalFixtureQueryNames(fixture.Match.RedactedQueryParameters)
	if err != nil {
		return nil, err
	}
	interactions := make([]FixtureInteraction, len(fixture.Interactions))
	for index, interaction := range fixture.Interactions {
		canonical, canonicalErr := canonicalFixtureInteraction(interaction, maximumBody, redactedQuery)
		if canonicalErr != nil {
			return nil, &FixtureError{Interaction: index, Cause: canonicalErr}
		}
		interactions[index] = canonical
	}
	return &ReplayTransport{
		interactions: interactions, matchHeaders: matchHeaders, maximumBody: maximumBody,
		redactedQuery: redactedQuery,
	}, nil
}

// RoundTrip returns the next response only when request matches exactly.
func (replay *ReplayTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, &FixtureError{Interaction: -1, Cause: ErrFixtureUnmatched}
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	actual, err := captureFixtureRequest(
		request, replay.matchHeaders, replay.redactedQuery, replay.maximumBody,
	)
	if err != nil {
		return nil, err
	}
	replay.mu.Lock()
	defer replay.mu.Unlock()
	if replay.next >= len(replay.interactions) ||
		!fixtureRequestsEqual(actual, replay.interactions[replay.next].Request, replay.matchHeaders) {
		return nil, &FixtureError{Interaction: replay.next, Cause: ErrFixtureUnmatched}
	}
	interaction := replay.interactions[replay.next]
	replay.next++
	if interaction.Response.Failure != "" {
		return nil, &FixtureError{
			Interaction: replay.next - 1,
			Cause:       &FixtureReplayError{Kind: interaction.Response.Failure},
		}
	}
	return fixtureHTTPResponse(request, interaction.Response), nil
}

// Verify fails when one or more ordered interactions were not consumed.
func (replay *ReplayTransport) Verify() error {
	replay.mu.Lock()
	defer replay.mu.Unlock()
	if replay.next != len(replay.interactions) {
		return &FixtureError{Interaction: replay.next, Cause: ErrFixtureUnused}
	}
	return nil
}

func canonicalFixtureInteraction(
	interaction FixtureInteraction,
	maximumBody int64,
	redactedQuery map[string]struct{},
) (FixtureInteraction, error) {
	if interaction.Request.Method == "" || !validHTTPToken(interaction.Request.Method) ||
		int64(len(interaction.Request.Body)) > maximumBody ||
		int64(len(interaction.Response.Body)) > maximumBody ||
		interaction.Request.BodySHA256 != "" && len(interaction.Request.Body) > 0 ||
		interaction.Request.BodySHA256 != "" && !validFixtureDigest(interaction.Request.BodySHA256) {
		return FixtureInteraction{}, ErrInvalidFixture
	}
	if !validFixtureResponsePolicy(interaction.Response) {
		return FixtureInteraction{}, ErrInvalidFixture
	}
	canonicalURL, err := canonicalFixtureURL(interaction.Request.URL, redactedQuery)
	if err != nil {
		return FixtureInteraction{}, err
	}
	requestHeader, err := canonicalFixtureHeader(interaction.Request.Header, false)
	if err != nil {
		return FixtureInteraction{}, err
	}
	responseHeader, err := canonicalFixtureHeader(interaction.Response.Header, false)
	if err != nil {
		return FixtureInteraction{}, err
	}
	trailer, err := canonicalFixtureHeader(interaction.Response.Trailer, true)
	if err != nil {
		return FixtureInteraction{}, err
	}
	return FixtureInteraction{
		Request: FixtureRequest{
			Method: interaction.Request.Method, URL: canonicalURL,
			Header: requestHeader, Body: append([]byte(nil), interaction.Request.Body...),
			BodySHA256: interaction.Request.BodySHA256,
		},
		Response: FixtureResponse{
			StatusCode: interaction.Response.StatusCode,
			Header:     responseHeader, Body: append([]byte(nil), interaction.Response.Body...),
			Trailer: trailer, ContentLength: cloneInt64(interaction.Response.ContentLength),
			Failure: interaction.Response.Failure, BodyFailure: interaction.Response.BodyFailure,
		},
	}, nil
}

func captureFixtureRequest(
	request *http.Request,
	matchHeaders []string,
	redactedQuery map[string]struct{},
	maximumBody int64,
) (FixtureRequest, error) {
	canonicalURL, err := canonicalFixtureURL(request.URL.String(), redactedQuery)
	if err != nil {
		return FixtureRequest{}, &FixtureError{Interaction: -1, Cause: ErrFixtureUnmatched}
	}
	body, err := readFixtureBody(request.Body, request.ContentLength, maximumBody)
	if err != nil {
		return FixtureRequest{}, err
	}
	header := make(http.Header, len(matchHeaders))
	for _, name := range matchHeaders {
		values := append([]string(nil), request.Header.Values(name)...)
		sort.Strings(values)
		if len(values) > 0 {
			header[name] = values
		}
	}
	return FixtureRequest{
		Method: request.Method, URL: canonicalURL,
		Header: header, Body: body,
	}, nil
}

func readFixtureBody(body io.ReadCloser, contentLength int64, maximum int64) ([]byte, error) {
	if body == nil || body == http.NoBody {
		return nil, nil
	}
	if contentLength > maximum {
		_ = body.Close()
		return nil, &FixtureError{Interaction: -1, Cause: ErrFixtureBodyLimit}
	}
	content, readErr := io.ReadAll(io.LimitReader(body, maximum+1))
	closeErr := body.Close()
	if readErr != nil || closeErr != nil {
		return nil, &FixtureError{Interaction: -1, Cause: errors.Join(ErrInvalidFixture, readErr, closeErr)}
	}
	if int64(len(content)) > maximum {
		return nil, &FixtureError{Interaction: -1, Cause: ErrFixtureBodyLimit}
	}
	return content, nil
}

func fixtureRequestsEqual(actual FixtureRequest, expected FixtureRequest, headers []string) bool {
	if actual.Method != expected.Method || actual.URL != expected.URL {
		return false
	}
	if expected.BodySHA256 != "" {
		digest := sha256.Sum256(actual.Body)
		if hex.EncodeToString(digest[:]) != expected.BodySHA256 {
			return false
		}
	} else if !bytes.Equal(actual.Body, expected.Body) {
		return false
	}
	for _, name := range headers {
		actualValues := append([]string(nil), actual.Header.Values(name)...)
		expectedValues := append([]string(nil), expected.Header.Values(name)...)
		sort.Strings(actualValues)
		sort.Strings(expectedValues)
		if !equalStrings(actualValues, expectedValues) {
			return false
		}
	}
	return true
}

func fixtureHTTPResponse(request *http.Request, fixture FixtureResponse) *http.Response {
	body := append([]byte(nil), fixture.Body...)
	contentLength := int64(len(body))
	if fixture.ContentLength != nil {
		contentLength = *fixture.ContentLength
	}
	responseBody := io.ReadCloser(io.NopCloser(bytes.NewReader(body)))
	if fixture.BodyFailure == FixtureBodyFailureUnexpectedEOF {
		responseBody = &fixtureUnexpectedEOFBody{reader: bytes.NewReader(body)}
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", fixture.StatusCode, http.StatusText(fixture.StatusCode)),
		StatusCode:    fixture.StatusCode,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        fixture.Header.Clone(),
		Body:          responseBody,
		ContentLength: contentLength,
		Trailer:       fixture.Trailer.Clone(),
		Request:       request,
	}
}

// NewScriptedTransport constructs a current-schema ordered replay fixture.
func NewScriptedTransport(
	interactions []FixtureInteraction,
	options ReplayOptions,
) (*ReplayTransport, error) {
	return NewReplayTransport(Fixture{
		SchemaVersion: FixtureSchemaVersion,
		Interactions:  append([]FixtureInteraction(nil), interactions...),
	}, options)
}

func validFixtureResponsePolicy(response FixtureResponse) bool {
	if response.Failure != "" {
		switch response.Failure {
		case FixtureFailureTimeout, FixtureFailureCanceled, FixtureFailureTransport,
			FixtureFailureMalformedResponse:
			return response.StatusCode == 0 && len(response.Header) == 0 &&
				len(response.Body) == 0 && len(response.Trailer) == 0 &&
				response.ContentLength == nil && response.BodyFailure == ""
		default:
			return false
		}
	}
	if response.StatusCode < 100 || response.StatusCode > 599 {
		return false
	}
	if response.ContentLength != nil && *response.ContentLength < -1 {
		return false
	}
	return response.BodyFailure == "" ||
		response.BodyFailure == FixtureBodyFailureUnexpectedEOF
}

type fixtureUnexpectedEOFBody struct{ reader *bytes.Reader }

func (body *fixtureUnexpectedEOFBody) Read(buffer []byte) (int, error) {
	count, err := body.reader.Read(buffer)
	if body.reader.Len() == 0 && (err == nil || errors.Is(err, io.EOF)) {
		return count, io.ErrUnexpectedEOF
	}
	return count, err
}

func (*fixtureUnexpectedEOFBody) Close() error { return nil }

func canonicalFixtureURL(raw string, redactedQuery map[string]struct{}) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return "", ErrInvalidFixture
	}
	origin, err := canonicalOrigin(parsed)
	if err != nil {
		return "", ErrInvalidFixture
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", ErrInvalidFixture
	}
	for key := range query {
		sort.Strings(query[key])
		if _, redact := redactedQuery[strings.ToLower(key)]; redact {
			for index := range query[key] {
				query[key][index] = "[REDACTED]"
			}
		}
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := origin + path
	if encoded := query.Encode(); encoded != "" {
		canonical += "?" + encoded
	}
	return canonical, nil
}

func canonicalFixtureQueryNames(names []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || len(name) > 256 || !validPolicyScopeValue(name) {
			return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
		result[name] = struct{}{}
	}
	return result, nil
}

func validFixtureDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func canonicalFixtureHeaderNames(names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		canonical, err := validateHeaderName(name)
		if err != nil || sensitiveFixtureHeader(canonical) {
			return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	sort.Strings(result)
	return result, nil
}

func canonicalFixtureTrailerNames(names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		canonical, err := validateTrailerName(name)
		if err != nil || sensitiveFixtureHeader(canonical) {
			return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	sort.Strings(result)
	return result, nil
}

func canonicalFixtureHeader(header http.Header, trailer bool) (http.Header, error) {
	result := make(http.Header, len(header))
	for name, values := range header {
		if sensitiveFixtureHeader(http.CanonicalHeaderKey(name)) {
			return nil, ErrInvalidFixture
		}
		var canonical string
		var err error
		if trailer {
			canonical, err = validateTrailer(name, values)
		} else {
			canonical, err = validateHeader(name, values)
		}
		if err != nil {
			return nil, ErrInvalidFixture
		}
		result[canonical] = append([]string(nil), values...)
	}
	return result, nil
}

func sensitiveFixtureHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie",
		"X-Api-Key", "X-Auth-Token", "X-Access-Token":
		return true
	default:
		return false
	}
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

var _ http.RoundTripper = (*ReplayTransport)(nil)
