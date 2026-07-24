package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecorderSanitizesBeforeFixtureStorageAndPreservesLiveExchange(t *testing.T) {
	clock := &fixtureTestClock{now: time.Unix(1_700_000_000, 0).UTC()}
	recorder, err := NewRecorderTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil || string(body) != "request secret body" {
			t.Fatalf("forwarded request body = %q, %v", body, readErr)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"Set-Cookie":   []string{"session=live-secret"},
				"X-Request-ID": []string{"volatile-1"},
			},
			Body: io.NopCloser(strings.NewReader(`{"token":"live-secret","value":1}`)),
			Trailer: http.Header{
				"Set-Cookie": []string{"trailer-secret"},
				"X-Checksum": []string{"stable-checksum"},
				"X-Private":  []string{"omitted"},
			},
			Request: request,
		}, nil
	}), RecorderOptions{
		MatchHeaders:            []string{"X-Mode"},
		RedactedQueryParameters: []string{"customer"},
		ResponseHeaders:         []string{"Content-Type", "X-Request-ID"},
		ResponseTrailers:        []string{"X-Checksum", "X-Private", "x-checksum"},
		VolatileHeaders:         []string{"X-Request-ID"},
		MaximumBodyBytes:        1 << 10,
		TTL:                     24 * time.Hour,
		Clock:                   clock,
		ResponseBodyRedactor: FixtureBodyRedactorFunc(func(content []byte) ([]byte, error) {
			return []byte(strings.ReplaceAll(string(content), "live-secret", "[REDACTED]")), nil
		}),
	})
	if err != nil {
		t.Fatalf("construct recorder: %v", err)
	}
	request, _ := http.NewRequest(
		http.MethodPost,
		"https://api.example.test/widgets?token=live-secret&customer=private&safe=visible",
		strings.NewReader("request secret body"),
	)
	request.Header.Set("Authorization", "Bearer live-secret")
	request.Header.Set("Cookie", "session=live-secret")
	request.Header.Set("X-Mode", "create")
	response, err := recorder.RoundTrip(request)
	if err != nil {
		t.Fatalf("record request: %v", err)
	}
	liveBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read live response: %v", err)
	}
	_ = response.Body.Close()
	if string(liveBody) != `{"token":"live-secret","value":1}` ||
		response.Header.Get("Set-Cookie") != "session=live-secret" {
		t.Fatalf("live response was sanitized: %q, %#v", liveBody, response.Header)
	}

	fixture := recorder.Fixture()
	if fixture.SchemaVersion != FixtureSchemaVersion || !fixture.RecordedAt.Equal(clock.now) ||
		!fixture.ExpiresAt.Equal(clock.now.Add(24*time.Hour)) || len(fixture.Interactions) != 1 {
		t.Fatalf("recorded fixture metadata = %#v", fixture)
	}
	interaction := fixture.Interactions[0]
	if strings.Contains(interaction.Request.URL, "live-secret") ||
		strings.Contains(interaction.Request.URL, "private") ||
		!strings.Contains(interaction.Request.URL, "safe=visible") ||
		len(interaction.Request.Body) != 0 || interaction.Request.BodySHA256 == "" ||
		interaction.Request.Header.Get("Authorization") != "" ||
		interaction.Response.Header.Get("Set-Cookie") != "" ||
		interaction.Response.Header.Get("X-Request-ID") != "" ||
		interaction.Response.Trailer.Get("X-Checksum") != "stable-checksum" ||
		interaction.Response.Trailer.Get("X-Private") != "omitted" ||
		interaction.Response.Trailer.Get("Set-Cookie") != "" ||
		string(interaction.Response.Body) != `{"token":"[REDACTED]","value":1}` {
		t.Fatalf("unsafe recorded interaction = %#v", interaction)
	}
	fixture.Interactions[0].Response.Body[0] = 'x'
	if recorder.Fixture().Interactions[0].Response.Body[0] == 'x' {
		t.Fatal("fixture snapshot mutated recorder state")
	}

	replay, err := NewReplayTransport(recorder.Fixture(), ReplayOptions{})
	if err != nil {
		t.Fatalf("construct recorded replay: %v", err)
	}
	replayRequest, _ := http.NewRequest(
		http.MethodPost,
		"https://api.example.test/widgets?customer=different&safe=visible&token=different",
		strings.NewReader("request secret body"),
	)
	replayRequest.Header.Set("X-Mode", "create")
	replayed, err := replay.RoundTrip(replayRequest)
	if err != nil {
		t.Fatalf("replay recorded request: %v", err)
	}
	replayedBody, _ := io.ReadAll(replayed.Body)
	_ = replayed.Body.Close()
	if string(replayedBody) != `{"token":"[REDACTED]","value":1}` {
		t.Fatalf("replayed sanitized body = %q", replayedBody)
	}
}

func TestRecorderStoresOnlyStableTransportFailureCategory(t *testing.T) {
	liveFailure := fixtureTimeoutError{cause: errors.New("live private timeout detail")}
	recorder, err := NewRecorderTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, liveFailure
	}), RecorderOptions{})
	if err != nil {
		t.Fatalf("construct failure recorder: %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/private?token=secret", nil)
	if _, err := recorder.RoundTrip(request); !errors.Is(err, liveFailure.cause) {
		t.Fatalf("live transport error = %v", err)
	}
	fixture := recorder.Fixture()
	if len(fixture.Interactions) != 1 ||
		fixture.Interactions[0].Response.Failure != FixtureFailureTimeout ||
		strings.Contains(fixture.Interactions[0].Request.URL, "secret") {
		t.Fatalf("recorded failure fixture = %#v", fixture)
	}
	replay, err := NewReplayTransport(fixture, ReplayOptions{})
	if err != nil {
		t.Fatalf("construct failure replay: %v", err)
	}
	replayRequest, _ := http.NewRequest(http.MethodGet, "https://example.test/private?token=different", nil)
	if _, err := replay.RoundTrip(replayRequest); !errors.Is(err, ErrFixtureTimeout) ||
		strings.Contains(err.Error(), "private") {
		t.Fatalf("replayed failure = %v", err)
	}
}

func TestRecorderRejectsUnsafeConfigurationAndRuntimeBoundaries(t *testing.T) {
	var nilTransport *fixtureNilTransport
	var nilClock *fixtureNilClock
	var nilRedactor *fixtureNilRedactor
	constructors := []func() (*RecorderTransport, error){
		func() (*RecorderTransport, error) { return NewRecorderTransport(nil, RecorderOptions{}) },
		func() (*RecorderTransport, error) { return NewRecorderTransport(nilTransport, RecorderOptions{}) },
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{TTL: -1})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{MaximumBodyBytes: -1})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{MaximumBodyBytes: maximumFixtureBody + 1})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{MatchHeaders: []string{"Authorization"}})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{SensitiveHeaders: []string{"bad header"}})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{RedactedQueryParameters: []string{""}})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{ResponseHeaders: []string{"Set-Cookie"}})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{ResponseTrailers: []string{"Content-Length"}})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{
				MatchHeaders:     []string{"X-Vendor-Token"},
				SensitiveHeaders: []string{"X-Vendor-Token"},
			})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{VolatileHeaders: []string{"bad header"}})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{Clock: nilClock})
		},
		func() (*RecorderTransport, error) {
			return NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{ResponseBodyRedactor: nilRedactor})
		},
	}
	for index, construct := range constructors {
		if _, err := construct(); !errors.Is(err, ErrInvalidFixture) {
			t.Fatalf("invalid recorder %d error = %v", index, err)
		}
	}

	recorder, _ := NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{})
	if _, err := recorder.RoundTrip(nil); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("nil record request = %v", err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	canceled, _ := http.NewRequestWithContext(canceledContext, http.MethodGet, "https://example.test", nil)
	if _, err := recorder.RoundTrip(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled record request = %v", err)
	}
	oversizedRequest, _ := http.NewRequest(
		http.MethodPost, "https://example.test", strings.NewReader("long"),
	)
	limitedBodyRecorder, _ := NewRecorderTransport(
		telemetryNoContentTransport(), RecorderOptions{MaximumBodyBytes: 2},
	)
	if _, err := limitedBodyRecorder.RoundTrip(oversizedRequest); !errors.Is(err, ErrFixtureBodyLimit) {
		t.Fatalf("record request body limit = %v", err)
	}

	boundaryCases := []struct {
		name     string
		response *http.Response
		options  RecorderOptions
		want     error
	}{
		{"nil response", nil, RecorderOptions{}, ErrInvalidFixture},
		{"nil body", &http.Response{StatusCode: 204, Header: make(http.Header)}, RecorderOptions{}, nil},
		{"body limit", &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("long")), ContentLength: 4}, RecorderOptions{MaximumBodyBytes: 2}, ErrFixtureBodyLimit},
		{"body read", &http.Response{StatusCode: 200, Header: make(http.Header), Body: fixtureFailureBody{readErr: errors.New("read"), closeErr: errors.New("close")}, ContentLength: -1}, RecorderOptions{}, ErrInvalidFixture},
		{"invalid status", &http.Response{StatusCode: 0, Header: make(http.Header), Body: http.NoBody}, RecorderOptions{}, ErrInvalidFixture},
		{"redactor error", fixtureBodyResponse("body"), RecorderOptions{ResponseBodyRedactor: FixtureBodyRedactorFunc(func([]byte) ([]byte, error) { return nil, errors.New("redact") })}, ErrInvalidFixture},
		{"redactor overflow", fixtureBodyResponse("body"), RecorderOptions{MaximumBodyBytes: 4, ResponseBodyRedactor: FixtureBodyRedactorFunc(func([]byte) ([]byte, error) { return []byte("large"), nil })}, ErrInvalidFixture},
		{"redactor panic", fixtureBodyResponse("body"), RecorderOptions{ResponseBodyRedactor: FixtureBodyRedactorFunc(func([]byte) ([]byte, error) { panic("redact") })}, ErrInvalidFixture},
	}
	for _, test := range boundaryCases {
		t.Run(test.name, func(t *testing.T) {
			candidate, err := NewRecorderTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if test.response != nil {
					test.response.Request = request
				}
				return test.response, nil
			}), test.options)
			if err != nil {
				t.Fatalf("construct boundary recorder: %v", err)
			}
			request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
			response, err := candidate.RoundTrip(request)
			if test.want == nil {
				if err != nil || response == nil {
					t.Fatalf("boundary response = %#v, %v", response, err)
				}
				_ = response.Body.Close()
			} else if !errors.Is(err, test.want) || response != nil {
				t.Fatalf("boundary response = %#v, %v, want %v", response, err, test.want)
			}
		})
	}

	limited, _ := NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{})
	limited.maximumInteractions = 0
	request, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if _, err := limited.RoundTrip(request); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("interaction limit error = %v", err)
	}
	failureLimited, _ := NewRecorderTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport")
	}), RecorderOptions{})
	failureLimited.maximumInteractions = 0
	request, _ = http.NewRequest(http.MethodGet, "https://example.test", nil)
	if _, err := failureLimited.RoundTrip(request); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("failure interaction limit error = %v", err)
	}

	closeProbe := &fixtureCloseProbe{}
	transportFailure := errors.New("transport failure")
	withResponse, _ := NewRecorderTransport(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 500, Header: make(http.Header), Body: closeProbe, Request: request,
		}, transportFailure
	}), RecorderOptions{})
	request, _ = http.NewRequest(http.MethodGet, "https://example.test", nil)
	if _, err := withResponse.RoundTrip(request); !errors.Is(err, transportFailure) || !closeProbe.closed {
		t.Fatalf("response-plus-error = %v, closed %t", err, closeProbe.closed)
	}
	if classifyFixtureFailure(context.Canceled) != FixtureFailureCanceled ||
		classifyFixtureFailure(context.DeadlineExceeded) != FixtureFailureTimeout ||
		classifyFixtureFailure(errors.New("transport")) != FixtureFailureTransport {
		t.Fatal("fixture failure classification is unstable")
	}
}

func fixtureBodyResponse(content string) *http.Response {
	return &http.Response{
		StatusCode: 200, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(content)), ContentLength: int64(len(content)),
	}
}

type fixtureTestClock struct{ now time.Time }

func (clock *fixtureTestClock) Now() time.Time                      { return clock.now }
func (*fixtureTestClock) Wait(context.Context, time.Duration) error { return nil }

type fixtureTimeoutError struct{ cause error }

func (err fixtureTimeoutError) Error() string { return err.cause.Error() }
func (err fixtureTimeoutError) Unwrap() error { return err.cause }
func (fixtureTimeoutError) Timeout() bool     { return true }
func (fixtureTimeoutError) Temporary() bool   { return false }

type fixtureNilTransport struct{}

func (*fixtureNilTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

type fixtureNilRedactor struct{}

func (*fixtureNilRedactor) RedactFixtureBody([]byte) ([]byte, error) { return nil, nil }

type fixtureCloseProbe struct{ closed bool }

func (*fixtureCloseProbe) Read([]byte) (int, error) { return 0, io.EOF }
func (probe *fixtureCloseProbe) Close() error       { probe.closed = true; return nil }
