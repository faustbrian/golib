package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReplayTransportMatchesCanonicalRequestsAndClonesResponses(t *testing.T) {
	fixture := Fixture{
		SchemaVersion: FixtureSchemaVersion,
		RecordedAt:    time.Unix(1_700_000_000, 0).UTC(),
		Interactions: []FixtureInteraction{{
			Request: FixtureRequest{
				Method: http.MethodPost,
				URL:    "https://API.example.test:443/widgets?b=2&a=3&a=1",
				Header: http.Header{"X-Mode": []string{"second", "first"}},
				Body:   []byte("request-body"),
			},
			Response: FixtureResponse{
				StatusCode: http.StatusCreated,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       []byte(`{"id":1}`),
				Trailer:    http.Header{"X-Checksum": []string{"valid"}},
			},
		}},
	}
	replay, err := NewReplayTransport(fixture, ReplayOptions{
		MatchHeaders:     []string{"X-Mode"},
		MaximumBodyBytes: 64,
	})
	if err != nil {
		t.Fatalf("construct replay transport: %v", err)
	}
	fixture.Interactions[0].Response.Body[0] = 'x'
	request, _ := http.NewRequest(
		http.MethodPost,
		"https://api.example.test/widgets?a=1&a=3&b=2",
		strings.NewReader("request-body"),
	)
	request.Header.Add("X-Mode", "first")
	request.Header.Add("X-Mode", "second")
	request.Header.Set("Authorization", "Bearer live-secret")
	response, err := replay.RoundTrip(request)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read replay body: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated || string(content) != `{"id":1}` ||
		response.Request != request || response.ContentLength != int64(len(content)) ||
		response.Trailer.Get("X-Checksum") != "valid" {
		t.Fatalf("replay response = %#v, %q", response, content)
	}
	response.Header.Set("X-Mutated", "true")
	if fixture.Interactions[0].Response.Header.Get("X-Mutated") != "" {
		t.Fatal("replay response aliased fixture headers")
	}
	if err := replay.Verify(); err != nil {
		t.Fatalf("verify replay: %v", err)
	}
}

func TestReplayTransportRejectsUnmatchedAndUnusedInteractionsSafely(t *testing.T) {
	fixture := Fixture{
		SchemaVersion: FixtureSchemaVersion,
		Interactions: []FixtureInteraction{
			{
				Request:  FixtureRequest{Method: http.MethodGet, URL: "https://example.test/first"},
				Response: FixtureResponse{StatusCode: http.StatusNoContent},
			},
			{
				Request:  FixtureRequest{Method: http.MethodGet, URL: "https://example.test/second"},
				Response: FixtureResponse{StatusCode: http.StatusNoContent},
			},
		},
	}
	replay, err := NewReplayTransport(fixture, ReplayOptions{})
	if err != nil {
		t.Fatalf("construct replay transport: %v", err)
	}
	unmatched, _ := http.NewRequest(http.MethodGet, "https://example.test/private?token=secret", nil)
	if _, err := replay.RoundTrip(unmatched); !errors.Is(err, ErrFixtureUnmatched) ||
		strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unmatched error = %v", err)
	}
	first, _ := http.NewRequest(http.MethodGet, "https://example.test/first", nil)
	response, err := replay.RoundTrip(first)
	if err != nil {
		t.Fatalf("replay first interaction: %v", err)
	}
	_ = response.Body.Close()
	if err := replay.Verify(); !errors.Is(err, ErrFixtureUnused) || strings.Contains(err.Error(), "second") {
		t.Fatalf("unused error = %v", err)
	}
	second, _ := http.NewRequest(http.MethodGet, "https://example.test/second", nil)
	response, err = replay.RoundTrip(second)
	if err != nil {
		t.Fatalf("replay second interaction: %v", err)
	}
	_ = response.Body.Close()
	if _, err := replay.RoundTrip(second); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("exhausted replay error = %v", err)
	}
	if err := replay.Verify(); err != nil {
		t.Fatalf("verify consumed replay: %v", err)
	}
}

func TestReplayTransportPreservesCaseSensitiveExtensionMethods(t *testing.T) {
	replay, err := NewReplayTransport(Fixture{
		SchemaVersion: FixtureSchemaVersion,
		Interactions: []FixtureInteraction{{
			Request:  FixtureRequest{Method: "Custom", URL: "https://example.test/"},
			Response: FixtureResponse{StatusCode: http.StatusNoContent},
		}},
	}, ReplayOptions{})
	if err != nil {
		t.Fatalf("construct extension-method replay: %v", err)
	}
	request, _ := http.NewRequest("CUSTOM", "https://example.test/", nil)
	if _, err := replay.RoundTrip(request); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("case-insensitive extension method matched: %v", err)
	}
}

func TestReplayTransportRejectsMalformedFixturesAndBoundaries(t *testing.T) {
	valid := Fixture{
		SchemaVersion: FixtureSchemaVersion,
		Interactions: []FixtureInteraction{{
			Request:  FixtureRequest{Method: http.MethodGet, URL: "https://example.test"},
			Response: FixtureResponse{StatusCode: http.StatusNoContent},
		}},
	}
	tooMany := valid
	tooMany.Interactions = make([]FixtureInteraction, maximumFixtureInteractions+1)
	invalidFixtures := []struct {
		fixture Fixture
		options ReplayOptions
	}{
		{Fixture{}, ReplayOptions{}},
		{valid, ReplayOptions{MaximumBodyBytes: -1}},
		{valid, ReplayOptions{MaximumBodyBytes: maximumFixtureBody + 1}},
		{tooMany, ReplayOptions{}},
		{valid, ReplayOptions{MatchHeaders: []string{"bad header"}}},
		{valid, ReplayOptions{MatchHeaders: []string{"Authorization"}}},
		{fixtureWithRequest(valid, FixtureRequest{Method: "", URL: "https://example.test"}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: "bad method", URL: "https://example.test"}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "://bad"}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "/relative"}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "https://user@example.test/"}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "https://example.test/#fragment"}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "https://example.test/?bad;query"}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "https://example.test/", Body: []byte("long")}), ReplayOptions{MaximumBodyBytes: 2}},
		{fixtureWithResponse(valid, FixtureResponse{StatusCode: 99}), ReplayOptions{}},
		{fixtureWithResponse(valid, FixtureResponse{StatusCode: 600}), ReplayOptions{}},
		{fixtureWithResponse(valid, FixtureResponse{StatusCode: 200, Body: []byte("long")}), ReplayOptions{MaximumBodyBytes: 2}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "https://example.test/", Header: http.Header{"Authorization": []string{"secret"}}}), ReplayOptions{}},
		{fixtureWithRequest(valid, FixtureRequest{Method: http.MethodGet, URL: "https://example.test/", Header: http.Header{"Bad Header": []string{"value"}}}), ReplayOptions{}},
		{fixtureWithResponse(valid, FixtureResponse{StatusCode: 200, Header: http.Header{"Set-Cookie": []string{"secret"}}}), ReplayOptions{}},
		{fixtureWithResponse(valid, FixtureResponse{StatusCode: 200, Trailer: http.Header{"Content-Length": []string{"1"}}}), ReplayOptions{}},
	}
	for index, test := range invalidFixtures {
		if _, err := NewReplayTransport(test.fixture, test.options); !errors.Is(err, ErrInvalidFixture) {
			t.Fatalf("invalid fixture %d error = %v", index, err)
		}
	}
	replay, err := NewReplayTransport(valid, ReplayOptions{
		MatchHeaders: []string{"X-Match", "x-match"}, MaximumBodyBytes: 2,
	})
	if err != nil {
		t.Fatalf("construct boundary replay: %v", err)
	}
	if _, err := replay.RoundTrip(nil); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("nil request error = %v", err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	canceled, _ := http.NewRequestWithContext(canceledContext, http.MethodGet, "https://example.test", nil)
	if _, err := replay.RoundTrip(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v", err)
	}
	invalidURL, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	invalidURL.URL.Scheme = ""
	if _, err := replay.RoundTrip(invalidURL); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("invalid request URL error = %v", err)
	}
	declared, _ := http.NewRequest(http.MethodGet, "https://example.test", strings.NewReader("long"))
	if _, err := replay.RoundTrip(declared); !errors.Is(err, ErrFixtureBodyLimit) {
		t.Fatalf("declared body limit error = %v", err)
	}
	chunked, _ := http.NewRequest(http.MethodGet, "https://example.test", io.NopCloser(strings.NewReader("long")))
	chunked.ContentLength = -1
	if _, err := replay.RoundTrip(chunked); !errors.Is(err, ErrFixtureBodyLimit) {
		t.Fatalf("stream body limit error = %v", err)
	}
	readFailure := errors.New("private read failure")
	failed, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	failed.Body = fixtureFailureBody{readErr: readFailure, closeErr: readFailure}
	failed.ContentLength = -1
	if _, err := replay.RoundTrip(failed); !errors.Is(err, ErrInvalidFixture) || !errors.Is(err, readFailure) {
		t.Fatalf("body failure error = %v", err)
	}
}

func TestReplayTransportHeaderMismatchDoesNotConsumeInteraction(t *testing.T) {
	replay, err := NewReplayTransport(Fixture{
		SchemaVersion: FixtureSchemaVersion,
		Interactions: []FixtureInteraction{{
			Request: FixtureRequest{
				Method: http.MethodGet, URL: "https://example.test/a%2Fb",
				Header: http.Header{"X-Match": []string{"one", "two"}},
			},
			Response: FixtureResponse{StatusCode: http.StatusNoContent},
		}},
	}, ReplayOptions{MatchHeaders: []string{"X-Match"}})
	if err != nil {
		t.Fatalf("construct header replay: %v", err)
	}
	wrongLength, _ := http.NewRequest(http.MethodGet, "https://example.test/a%2Fb", nil)
	wrongLength.Header.Set("X-Match", "one")
	if _, err := replay.RoundTrip(wrongLength); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("header length mismatch = %v", err)
	}
	wrongValue, _ := http.NewRequest(http.MethodGet, "https://example.test/a%2Fb", nil)
	wrongValue.Header.Add("X-Match", "one")
	wrongValue.Header.Add("X-Match", "wrong")
	if _, err := replay.RoundTrip(wrongValue); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("header value mismatch = %v", err)
	}
}

func TestReplayTransportScriptsBoundedFailureAndTruncationFixtures(t *testing.T) {
	declaredLength := int64(10)
	replay, err := NewScriptedTransport([]FixtureInteraction{
		{
			Request:  FixtureRequest{Method: http.MethodGet, URL: "https://example.test/timeout"},
			Response: FixtureResponse{Failure: FixtureFailureTimeout},
		},
		{
			Request:  FixtureRequest{Method: http.MethodGet, URL: "https://example.test/canceled"},
			Response: FixtureResponse{Failure: FixtureFailureCanceled},
		},
		{
			Request:  FixtureRequest{Method: http.MethodGet, URL: "https://example.test/transport"},
			Response: FixtureResponse{Failure: FixtureFailureTransport},
		},
		{
			Request:  FixtureRequest{Method: http.MethodGet, URL: "https://example.test/malformed"},
			Response: FixtureResponse{Failure: FixtureFailureMalformedResponse},
		},
		{
			Request: FixtureRequest{Method: http.MethodGet, URL: "https://example.test/truncated"},
			Response: FixtureResponse{
				StatusCode: http.StatusOK, Body: []byte("short"),
				ContentLength: &declaredLength,
				BodyFailure:   FixtureBodyFailureUnexpectedEOF,
			},
		},
	}, ReplayOptions{})
	if err != nil {
		t.Fatalf("construct scripted transport: %v", err)
	}
	for _, test := range []struct {
		path string
		want error
	}{
		{"timeout", ErrFixtureTimeout},
		{"canceled", context.Canceled},
		{"transport", ErrFixtureTransport},
		{"malformed", ErrFixtureMalformedResponse},
	} {
		request, _ := http.NewRequest(http.MethodGet, "https://example.test/"+test.path, nil)
		_, err := replay.RoundTrip(request)
		if !errors.Is(err, test.want) || strings.Contains(err.Error(), test.path) {
			t.Fatalf("%s fixture error = %v", test.path, err)
		}
		if test.path == "timeout" {
			var networkError net.Error
			if !errors.As(err, &networkError) || !networkError.Timeout() {
				t.Fatalf("timeout fixture is not net.Error: %v", err)
			}
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/truncated", nil)
	response, err := replay.RoundTrip(request)
	if err != nil {
		t.Fatalf("replay truncated response: %v", err)
	}
	content, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(content) != "short" || !errors.Is(err, io.ErrUnexpectedEOF) ||
		response.ContentLength != declaredLength {
		t.Fatalf("truncated response = %q, %d, %v", content, response.ContentLength, err)
	}
	if err := replay.Verify(); err != nil {
		t.Fatalf("verify scripted transport: %v", err)
	}
}

func TestReplayTransportValidatesPersistedMatchAndFailurePolicy(t *testing.T) {
	base := Fixture{
		SchemaVersion: FixtureSchemaVersion,
		Match: FixtureMatchPolicy{
			Headers: []string{"X-Match"}, RedactedQueryParameters: []string{"token"},
		},
		Interactions: []FixtureInteraction{{
			Request: FixtureRequest{
				Method: http.MethodPost, URL: "https://example.test/?token=[REDACTED]",
				BodySHA256: emptyFixtureBodyDigest,
			},
			Response: FixtureResponse{StatusCode: http.StatusNoContent},
		}},
	}
	if _, err := NewReplayTransport(base, ReplayOptions{MatchHeaders: []string{"x-match"}}); err != nil {
		t.Fatalf("matching persisted override: %v", err)
	}
	for _, test := range []Fixture{
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Match.Headers = []string{"bad header"}
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Match.RedactedQueryParameters = []string{""}
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Match.RedactedQueryParameters = []string{"bad\nname"}
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Interactions[0].Request.BodySHA256 = "short"
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Interactions[0].Request.BodySHA256 = strings.ToUpper(emptyFixtureBodyDigest)
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Interactions[0].Response.Failure = FixtureFailure("unknown")
			fixture.Interactions[0].Response.StatusCode = 0
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Interactions[0].Response.Failure = FixtureFailureTimeout
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			value := int64(-2)
			fixture.Interactions[0].Response.ContentLength = &value
			return fixture
		}(),
		func() Fixture {
			fixture := cloneFixture(base)
			fixture.Interactions[0].Response.BodyFailure = FixtureBodyFailure("unknown")
			return fixture
		}(),
	} {
		if _, err := NewReplayTransport(test, ReplayOptions{}); !errors.Is(err, ErrInvalidFixture) {
			t.Fatalf("invalid persisted fixture %#v error = %v", test, err)
		}
	}
	if _, err := NewReplayTransport(base, ReplayOptions{MatchHeaders: []string{"X-Other"}}); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("mismatched persisted override = %v", err)
	}

	digestReplay, _ := NewReplayTransport(base, ReplayOptions{})
	nonempty, _ := http.NewRequest(http.MethodPost, "https://example.test/?token=different", strings.NewReader("different"))
	if _, err := digestReplay.RoundTrip(nonempty); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("digest mismatch = %v", err)
	}
	raw := cloneFixture(base)
	raw.Interactions[0].Request.BodySHA256 = ""
	raw.Interactions[0].Request.Body = []byte("expected")
	rawReplay, _ := NewReplayTransport(raw, ReplayOptions{})
	actual, _ := http.NewRequest(http.MethodPost, "https://example.test/?token=different", strings.NewReader("actual"))
	if _, err := rawReplay.RoundTrip(actual); !errors.Is(err, ErrFixtureUnmatched) {
		t.Fatalf("raw body mismatch = %v", err)
	}

	failure := &FixtureReplayError{Kind: FixtureFailure("unknown")}
	if failure.Error() == "" || !errors.Is(failure, ErrInvalidFixture) || failure.Temporary() {
		t.Fatalf("unknown fixture failure = %v", failure)
	}
	body := &fixtureUnexpectedEOFBody{reader: bytes.NewReader([]byte("four"))}
	buffer := make([]byte, 2)
	if count, err := body.Read(buffer); count != 2 || err != nil {
		t.Fatalf("incremental truncation read = %d, %v", count, err)
	}
	_ = body.Close()
}

func fixtureWithRequest(fixture Fixture, request FixtureRequest) Fixture {
	fixture.Interactions = append([]FixtureInteraction(nil), fixture.Interactions...)
	fixture.Interactions[0].Request = request
	return fixture
}

func fixtureWithResponse(fixture Fixture, response FixtureResponse) Fixture {
	fixture.Interactions = append([]FixtureInteraction(nil), fixture.Interactions...)
	fixture.Interactions[0].Response = response
	return fixture
}

type fixtureFailureBody struct {
	readErr  error
	closeErr error
}

func (body fixtureFailureBody) Read([]byte) (int, error) { return 0, body.readErr }
func (body fixtureFailureBody) Close() error             { return body.closeErr }
