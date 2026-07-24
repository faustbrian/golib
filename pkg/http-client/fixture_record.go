package httpclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

var defaultFixtureRedactedQueryParameters = []string{
	"access_token", "api_key", "apikey", "key", "password", "secret",
	"sig", "signature", "token",
}

// FixtureBodyRedactor sanitizes one bounded response body before storage.
type FixtureBodyRedactor interface {
	RedactFixtureBody([]byte) ([]byte, error)
}

// FixtureBodyRedactorFunc adapts a body-redaction function.
type FixtureBodyRedactorFunc func([]byte) ([]byte, error)

// RedactFixtureBody implements FixtureBodyRedactor.
func (function FixtureBodyRedactorFunc) RedactFixtureBody(content []byte) ([]byte, error) {
	return function(content)
}

// RecorderOptions configures bounded sanitized fixture capture.
type RecorderOptions struct {
	MatchHeaders            []string
	RedactedQueryParameters []string
	ResponseHeaders         []string
	ResponseTrailers        []string
	VolatileHeaders         []string
	SensitiveHeaders        []string
	MaximumBodyBytes        int64
	TTL                     time.Duration
	Clock                   RetryClock
	ResponseBodyRedactor    FixtureBodyRedactor
}

// RecorderTransport records sanitized successful exchanges around base.
type RecorderTransport struct {
	base                http.RoundTripper
	matchHeaders        []string
	redactedQuery       map[string]struct{}
	responseHeaders     []string
	responseTrailers    []string
	volatileHeaders     map[string]struct{}
	maximumBody         int64
	redactor            FixtureBodyRedactor
	maximumInteractions int
	mu                  sync.Mutex
	fixture             Fixture
}

// NewRecorderTransport creates a bounded recorder with safe persistence
// defaults. Response bodies are omitted unless a redactor is configured.
func NewRecorderTransport(base http.RoundTripper, options RecorderOptions) (*RecorderTransport, error) {
	if nilLike(base) || options.TTL < 0 {
		return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	maximumBody := options.MaximumBodyBytes
	if maximumBody == 0 {
		maximumBody = defaultFixtureMaximumBody
	}
	if maximumBody < 1 || maximumBody > maximumFixtureBody {
		return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	matchHeaders, err := canonicalFixtureHeaderNames(options.MatchHeaders)
	if err != nil {
		return nil, err
	}
	customSensitive := make(map[string]struct{}, len(options.SensitiveHeaders))
	for _, name := range options.SensitiveHeaders {
		canonical, nameErr := validateHeaderName(name)
		if nameErr != nil {
			return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
		customSensitive[canonical] = struct{}{}
	}
	for _, name := range matchHeaders {
		if _, sensitive := customSensitive[name]; sensitive {
			return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
	}
	redactedNames := append([]string(nil), defaultFixtureRedactedQueryParameters...)
	redactedNames = append(redactedNames, options.RedactedQueryParameters...)
	redactedQuery, err := canonicalFixtureQueryNames(redactedNames)
	if err != nil {
		return nil, err
	}
	responseNames := options.ResponseHeaders
	if len(responseNames) == 0 {
		responseNames = []string{"Content-Encoding", "Content-Type"}
	}
	responseHeaders, err := canonicalFixtureHeaderNames(responseNames)
	if err != nil {
		return nil, err
	}
	responseTrailers, err := canonicalFixtureTrailerNames(options.ResponseTrailers)
	if err != nil {
		return nil, err
	}
	volatileNames := append(
		[]string{"Date", "Server", "Traceparent", "Tracestate", "X-Request-ID"},
		options.VolatileHeaders...,
	)
	volatileNames = append(volatileNames, options.SensitiveHeaders...)
	volatileHeaders := make(map[string]struct{}, len(volatileNames))
	for _, name := range volatileNames {
		canonical, nameErr := validateHeaderName(name)
		if nameErr != nil {
			return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
		volatileHeaders[canonical] = struct{}{}
	}
	clock := options.Clock
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	if options.ResponseBodyRedactor != nil && nilLike(options.ResponseBodyRedactor) {
		return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	recordedAt := clock.Now().UTC()
	expiresAt := time.Time{}
	if options.TTL > 0 {
		expiresAt = recordedAt.Add(options.TTL)
	}
	return &RecorderTransport{
		base: base, matchHeaders: matchHeaders, redactedQuery: redactedQuery,
		responseHeaders: responseHeaders, responseTrailers: responseTrailers,
		volatileHeaders: volatileHeaders,
		maximumBody:     maximumBody, redactor: options.ResponseBodyRedactor,
		maximumInteractions: maximumFixtureInteractions,
		fixture: Fixture{
			SchemaVersion: FixtureSchemaVersion, RecordedAt: recordedAt, ExpiresAt: expiresAt,
			Match: FixtureMatchPolicy{
				Headers:                 append([]string(nil), matchHeaders...),
				RedactedQueryParameters: sortedFixtureQueryNames(redactedQuery),
			},
		},
	}, nil
}

// RoundTrip records a sanitized interaction while returning original live
// response bytes and headers to the caller.
func (recorder *RecorderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	originalLength := request.ContentLength
	captured, err := captureFixtureRequest(
		request, recorder.matchHeaders, recorder.redactedQuery, recorder.maximumBody,
	)
	if err != nil {
		return nil, err
	}
	rawRequestBody := append([]byte(nil), captured.Body...)
	request.Body = fixtureRequestBody(rawRequestBody)
	request.ContentLength = originalLength
	digest := sha256.Sum256(rawRequestBody)
	captured.Body = nil
	captured.BodySHA256 = hex.EncodeToString(digest[:])

	response, err := recorder.base.RoundTrip(request)
	if err != nil {
		var closeErr error
		if response != nil && response.Body != nil {
			closeErr = response.Body.Close()
		}
		interaction := FixtureInteraction{
			Request:  captured,
			Response: FixtureResponse{Failure: classifyFixtureFailure(err)},
		}
		if appendErr := recorder.appendInteraction(interaction); appendErr != nil {
			return nil, errors.Join(err, closeErr, appendErr)
		}
		return nil, errors.Join(err, closeErr)
	}
	if response == nil {
		return nil, &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	if response.Body == nil {
		response.Body = http.NoBody
	}
	originalResponseLength := response.ContentLength
	rawResponseBody, err := readFixtureBody(
		response.Body, response.ContentLength, recorder.maximumBody,
	)
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(rawResponseBody))
	response.ContentLength = originalResponseLength

	storedBody, err := recorder.redactResponseBody(rawResponseBody)
	if err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	interaction := FixtureInteraction{
		Request: captured,
		Response: FixtureResponse{
			StatusCode: response.StatusCode,
			Header: selectFixtureHeaders(
				response.Header, recorder.responseHeaders, recorder.volatileHeaders,
			),
			Body: storedBody,
			Trailer: selectFixtureHeaders(
				response.Trailer, recorder.responseTrailers, recorder.volatileHeaders,
			),
		},
	}
	if err := recorder.appendInteraction(interaction); err != nil {
		_ = response.Body.Close()
		return nil, err
	}
	return response, nil
}

// Fixture returns an independently mutable sanitized snapshot.
func (recorder *RecorderTransport) Fixture() Fixture {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return cloneFixture(recorder.fixture)
}

func (recorder *RecorderTransport) appendInteraction(interaction FixtureInteraction) error {
	canonical, err := canonicalFixtureInteraction(
		interaction, recorder.maximumBody, recorder.redactedQuery,
	)
	if err != nil {
		return &FixtureError{Interaction: -1, Cause: err}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.fixture.Interactions) >= recorder.maximumInteractions {
		return &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
	}
	recorder.fixture.Interactions = append(recorder.fixture.Interactions, canonical)
	return nil
}

func classifyFixtureFailure(failure error) FixtureFailure {
	if errors.Is(failure, context.Canceled) {
		return FixtureFailureCanceled
	}
	if errors.Is(failure, context.DeadlineExceeded) {
		return FixtureFailureTimeout
	}
	var networkError net.Error
	if errors.As(failure, &networkError) && networkError.Timeout() {
		return FixtureFailureTimeout
	}
	return FixtureFailureTransport
}

func (recorder *RecorderTransport) redactResponseBody(content []byte) (redacted []byte, err error) {
	if recorder.redactor == nil {
		return nil, nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			redacted = nil
			err = &FixtureError{Interaction: -1, Cause: ErrInvalidFixture}
		}
	}()
	redacted, err = recorder.redactor.RedactFixtureBody(append([]byte(nil), content...))
	if err != nil || int64(len(redacted)) > recorder.maximumBody {
		return nil, &FixtureError{Interaction: -1, Cause: errors.Join(ErrInvalidFixture, err)}
	}
	return append([]byte(nil), redacted...), nil
}

func selectFixtureHeaders(
	header http.Header,
	names []string,
	volatile map[string]struct{},
) http.Header {
	selected := make(http.Header, len(names))
	for _, name := range names {
		if _, omitted := volatile[name]; omitted || sensitiveFixtureHeader(name) {
			continue
		}
		values := append([]string(nil), header.Values(name)...)
		sort.Strings(values)
		if len(values) > 0 {
			selected[name] = values
		}
	}
	return selected
}

func sortedFixtureQueryNames(names map[string]struct{}) []string {
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func fixtureRequestBody(content []byte) io.ReadCloser {
	if len(content) == 0 {
		return http.NoBody
	}
	return io.NopCloser(bytes.NewReader(content))
}

func cloneFixture(fixture Fixture) Fixture {
	clone := Fixture{
		SchemaVersion: fixture.SchemaVersion, RecordedAt: fixture.RecordedAt,
		ExpiresAt: fixture.ExpiresAt,
		Match: FixtureMatchPolicy{
			Headers: append([]string(nil), fixture.Match.Headers...),
			RedactedQueryParameters: append(
				[]string(nil), fixture.Match.RedactedQueryParameters...,
			),
		},
		Interactions: make([]FixtureInteraction, len(fixture.Interactions)),
	}
	for index, interaction := range fixture.Interactions {
		clone.Interactions[index] = FixtureInteraction{
			Request: FixtureRequest{
				Method: interaction.Request.Method, URL: interaction.Request.URL,
				Header:     interaction.Request.Header.Clone(),
				Body:       append([]byte(nil), interaction.Request.Body...),
				BodySHA256: interaction.Request.BodySHA256,
			},
			Response: FixtureResponse{
				StatusCode:    interaction.Response.StatusCode,
				Header:        interaction.Response.Header.Clone(),
				Body:          append([]byte(nil), interaction.Response.Body...),
				Trailer:       interaction.Response.Trailer.Clone(),
				ContentLength: cloneInt64(interaction.Response.ContentLength),
				Failure:       interaction.Response.Failure,
				BodyFailure:   interaction.Response.BodyFailure,
			},
		}
	}
	return clone
}

var _ http.RoundTripper = (*RecorderTransport)(nil)
