package confluent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type canonicalizerFunction func(context.Context, schemaregistry.Definition) ([]byte, error)

func (function canonicalizerFunction) Canonicalize(ctx context.Context, definition schemaregistry.Definition) ([]byte, error) {
	return function(ctx, definition)
}

func TestHTTPBoundaryClassificationAndRetries(t *testing.T) {
	t.Parallel()

	transportError := errors.New("transport")
	readError := errors.New("read")
	closeError := errors.New("close")
	cases := []struct {
		name      string
		transport http.RoundTripper
		want      error
	}{
		{"transport", roundTripperFunction(func(*http.Request) (*http.Response, error) { return nil, transportError }), schemaregistry.ErrUnavailable},
		{"read", roundTripperFunction(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: errorReadCloser{readErr: readError}}, nil
		}), schemaregistry.ErrUnavailable},
		{"close", roundTripperFunction(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: errorReadCloser{readErr: io.EOF, closeErr: closeError}}, nil
		}), schemaregistry.ErrUnavailable},
		{"large", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(200, strings.Repeat("x", 129)), nil }), schemaregistry.ErrLimitExceeded},
		{"throttled", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(429, ""), nil }), schemaregistry.ErrUnavailable},
		{"server", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(500, ""), nil }), schemaregistry.ErrUnavailable},
		{"unauthorized", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(401, ""), nil }), schemaregistry.ErrUnauthorized},
		{"forbidden", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(403, ""), nil }), schemaregistry.ErrUnauthorized},
		{"not-found", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(404, ""), nil }), schemaregistry.ErrNotFound},
		{"conflict", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(409, ""), nil }), schemaregistry.ErrIncompatible},
		{"rejected", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(400, ""), nil }), schemaregistry.ErrRejected},
		{"malformed", roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(200, "{"), nil }), ErrInvalidResponse},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := internalProvider(t, test.transport)
			var target any
			if err := provider.doJSON(context.Background(), http.MethodGet, "/schemas", nil, &target); !errors.Is(err, test.want) {
				t.Fatalf("doJSON() error = %v, want %v", err, test.want)
			}
		})
	}

	credentialError := errors.New("credentials")
	provider := internalProvider(t, roundTripperFunction(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport called after credential failure")
		return nil, nil
	}))
	provider.credentials = credentialFunction(func(context.Context) (string, error) { return "", credentialError })
	if err := provider.doJSON(context.Background(), http.MethodGet, "/schemas", nil, nil); !errors.Is(err, credentialError) {
		t.Fatalf("doJSON(credentials) error = %v", err)
	}

	var attempts int
	provider = internalProvider(t, roundTripperFunction(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, transportError
		}
		if request.URL.Path == "/base/empty" {
			return response(200, ""), nil
		}
		if request.Header.Get("Authorization") != "Bearer token" || request.Header.Get("Content-Type") != mediaType ||
			request.Header.Get("Accept") != mediaType || request.URL.RawQuery != "verbose=true" || request.URL.EscapedPath() != "/base/a%2Fb" {
			t.Fatalf("request = %+v", request)
		}
		return response(200, "{}"), nil
	}))
	provider.maxAttempts = 2
	provider.credentials = credentialFunction(func(context.Context) (string, error) { return "Bearer token", nil })
	var decoded map[string]any
	if err := provider.doJSON(context.Background(), http.MethodPost, "/a%2Fb?verbose=true", []byte("{}"), &decoded); err != nil || attempts != 2 {
		t.Fatalf("doJSON(retry) = (%v, %d attempts)", err, attempts)
	}
	if err := provider.doJSON(context.Background(), http.MethodGet, "/empty", nil, nil); err != nil {
		t.Fatalf("doJSON(nil target) error = %v", err)
	}

	provider.retryDelay = time.Millisecond
	if err := provider.waitRetry(context.Background()); err != nil {
		t.Fatalf("waitRetry(timer) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := provider.waitRetry(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitRetry(canceled) error = %v", err)
	}
	provider.retryDelay = 0
	if err := provider.waitRetry(context.Background()); err != nil {
		t.Fatalf("waitRetry(zero) error = %v", err)
	}
	attempts = 0
	provider = internalProvider(t, roundTripperFunction(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, transportError
	}))
	provider.maxAttempts = 2
	if err := provider.doJSON(context.Background(), http.MethodGet, "/schemas", nil, nil); !errors.Is(err, schemaregistry.ErrUnavailable) || attempts != 2 {
		t.Fatalf("doJSON(exact retries) = (%v, %d attempts)", err, attempts)
	}
	provider = internalProvider(t, sequentialTransport(response(200, "{}")))
	provider.maxResponse = 2
	var exactResponse map[string]any
	if err := provider.doJSON(context.Background(), http.MethodGet, "/schemas", nil, &exactResponse); err != nil {
		t.Fatalf("doJSON(exact response limit) error = %v", err)
	}
	provider = internalProvider(t, sequentialTransport(response(300, "")))
	if err := provider.doJSON(context.Background(), http.MethodGet, "/schemas", nil, nil); !errors.Is(err, schemaregistry.ErrRejected) {
		t.Fatalf("doJSON(status 300) error = %v", err)
	}
	attempts = 0
	provider = internalProvider(t, roundTripperFunction(func(*http.Request) (*http.Response, error) {
		attempts++
		return response(500, ""), nil
	}))
	provider.maxAttempts = 2
	if err := provider.doJSON(context.Background(), http.MethodGet, "/schemas", nil, nil); !errors.Is(err, schemaregistry.ErrUnavailable) || attempts != 2 {
		t.Fatalf("doJSON(exact status retries) = (%v, %d attempts)", err, attempts)
	}

	provider.slots <- struct{}{}
	timed, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if err := provider.doJSON(timed, http.MethodGet, "/schemas", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("doJSON(saturated) error = %v", err)
	}
	<-provider.slots
}

func TestFramingBoundaryFailures(t *testing.T) {
	t.Parallel()

	for _, config := range []struct {
		scope   string
		payload int
	}{{"", 1}, {"scope", 0}} {
		if _, err := NewClassicFramer(config.scope, config.payload); err == nil {
			t.Fatal("NewClassicFramer(invalid) error = nil")
		}
	}
	if _, err := NewProtobufFramer("", 1, 1); err == nil {
		t.Fatal("NewProtobufFramer(empty scope) error = nil")
	}
	if _, err := NewProtobufFramer("scope", 0, 1); err == nil {
		t.Fatal("NewProtobufFramer(zero payload) error = nil")
	}
	if _, err := NewProtobufFramer("scope", 1, 0); err == nil {
		t.Fatal("NewProtobufFramer(zero indexes) error = nil")
	}
	classic, err := NewClassicFramer("scope", 1)
	if err != nil {
		t.Fatalf("NewClassicFramer() error = %v", err)
	}
	protobuf, err := NewProtobufFramer("scope", 1, 2)
	if err != nil {
		t.Fatalf("NewProtobufFramer() error = %v", err)
	}
	id := schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: "1"}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := classic.Frame(canceled, id, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Frame(canceled) error = %v", err)
	}
	if _, _, err := classic.Unframe(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Unframe(canceled) error = %v", err)
	}
	badIDs := []schemaregistry.ProviderID{{}, {Provider: ProviderName, Scope: "scope", Value: "0"}, {Provider: ProviderName, Scope: "scope", Value: "x"}}
	for _, badID := range badIDs {
		if _, err := classic.Frame(context.Background(), badID, nil); !errors.Is(err, ErrInvalidFrame) {
			t.Fatalf("Frame(%+v) error = %v", badID, err)
		}
	}
	if _, err := classic.Frame(context.Background(), id, []byte{1, 2}); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("Frame(large) error = %v", err)
	}
	classicFrame, err := classic.Frame(context.Background(), id, []byte{1})
	if err != nil {
		t.Fatalf("Frame(exact payload) error = %v", err)
	}
	if gotID, payload, err := classic.Unframe(context.Background(), classicFrame); err != nil || gotID != id || len(payload) != 1 || payload[0] != 1 {
		t.Fatalf("Unframe(exact payload) = (%+v, %v, %v)", gotID, payload, err)
	}
	emptyClassicFrame, err := classic.Frame(context.Background(), id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotID, payload, err := classic.Unframe(context.Background(), emptyClassicFrame); err != nil || gotID != id || len(payload) != 0 {
		t.Fatalf("Unframe(empty payload) = (%+v, %v, %v)", gotID, payload, err)
	}
	for _, frame := range [][]byte{nil, {1, 0, 0, 0, 1}, {0, 0, 0, 0, 0}, {0, 0, 0, 0, 1, 1, 2}} {
		if _, _, err := classic.Unframe(context.Background(), frame); !errors.Is(err, ErrInvalidFrame) {
			t.Fatalf("Unframe(%v) error = %v", frame, err)
		}
	}
	if _, err := protobuf.FrameMessage(canceled, id, []int{0}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("FrameMessage(canceled) error = %v", err)
	}
	for _, indexes := range [][]int{nil, {0, 1, 2}, {-1}} {
		if _, err := protobuf.FrameMessage(context.Background(), id, indexes, nil); !errors.Is(err, ErrInvalidFrame) {
			t.Fatalf("FrameMessage(%v) error = %v", indexes, err)
		}
	}
	if _, err := protobuf.FrameMessage(context.Background(), id, []int{0}, []byte{1, 2}); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("FrameMessage(large) error = %v", err)
	}
	protobufFrame, err := protobuf.FrameMessage(context.Background(), id, []int{1, 0}, []byte{1})
	if err != nil {
		t.Fatalf("FrameMessage(exact limits) error = %v", err)
	}
	gotID, indexes, payload, err := protobuf.UnframeMessage(context.Background(), protobufFrame)
	if err != nil || gotID != id || len(indexes) != 2 || indexes[0] != 1 || indexes[1] != 0 || len(payload) != 1 || payload[0] != 1 {
		t.Fatalf("UnframeMessage(exact limits) = (%+v, %v, %v, %v)", gotID, indexes, payload, err)
	}
	defaultIndexFrame, err := protobuf.FrameMessage(context.Background(), id, []int{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gotID, indexes, payload, err = protobuf.UnframeMessage(context.Background(), defaultIndexFrame)
	if err != nil || gotID != id || len(indexes) != 1 || indexes[0] != 0 || len(payload) != 0 {
		t.Fatalf("UnframeMessage(default index) = (%+v, %v, %v, %v)", gotID, indexes, payload, err)
	}
	oneIndexFrame, err := protobuf.FrameMessage(context.Background(), id, []int{1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gotID, indexes, payload, err = protobuf.UnframeMessage(context.Background(), oneIndexFrame)
	if err != nil || gotID != id || len(indexes) != 1 || indexes[0] != 1 || len(payload) != 0 {
		t.Fatalf("UnframeMessage(one index) = (%+v, %v, %v, %v)", gotID, indexes, payload, err)
	}
	if _, err := protobuf.FrameMessage(context.Background(), badIDs[0], []int{0}, nil); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("FrameMessage(bad scope) error = %v", err)
	}
	if _, err := protobuf.FrameMessage(context.Background(), badIDs[1], []int{0}, nil); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("FrameMessage(zero ID) error = %v", err)
	}
	if _, _, _, err := protobuf.UnframeMessage(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("UnframeMessage(canceled) error = %v", err)
	}
	for _, frame := range [][]byte{nil, {1, 0, 0, 0, 1, 0}, {0, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 1, 0x80}, {0, 0, 0, 0, 1, 6}, {0, 0, 0, 0, 1, 4, 0}, {0, 0, 0, 0, 1, 4, 1, 0}, {0, 0, 0, 0, 1, 0, 1, 2}} {
		if _, _, _, err := protobuf.UnframeMessage(context.Background(), frame); !errors.Is(err, ErrInvalidFrame) {
			t.Fatalf("UnframeMessage(%v) error = %v", frame, err)
		}
	}
	if _, _, ok := readSignedVarint([]byte{0x80}); ok {
		t.Fatal("readSignedVarint(truncated) ok = true")
	}
	if got, _, ok := readSignedVarint([]byte{1}); !ok || got != -1 {
		t.Fatalf("readSignedVarint(negative) = (%d, %v)", got, ok)
	}
}

func mustInternalSchema(t *testing.T) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte("string"),
	}, canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
		return definition.Content, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func sequentialTransport(responses ...*http.Response) http.RoundTripper {
	index := 0
	return roundTripperFunction(func(*http.Request) (*http.Response, error) {
		if index >= len(responses) {
			return nil, errors.New("unexpected request")
		}
		result := responses[index]
		index++
		return result, nil
	})
}

func TestProviderOperationBoundaries(t *testing.T) {
	t.Parallel()

	schema := mustInternalSchema(t)
	noCall := roundTripperFunction(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected transport call")
		return nil, nil
	})
	provider := internalProvider(t, noCall)
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{}); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("Register(invalid subject) error = %v", err)
	}
	if _, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Name: "s"}}); !errors.Is(err, schemaregistry.ErrUnsupportedFormat) {
		t.Fatalf("Register(invalid schema) error = %v", err)
	}
	provider = internalProvider(t, sequentialTransport(response(200, `{"id":7,"version":2}`)))
	registered, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Name: "s"}, Schema: schema})
	if err != nil || registered.Outcome != schemaregistry.RegistrationExisting || registered.ID.Value != "7" || registered.Version.Number != 2 {
		t.Fatalf("Register(existing) = (%+v, %v)", registered, err)
	}
	for _, test := range []struct {
		name      string
		responses []*http.Response
		want      error
	}{
		{"lookup failure", []*http.Response{response(500, "")}, schemaregistry.ErrUnavailable},
		{"unknown create", []*http.Response{response(404, ""), response(500, "")}, schemaregistry.ErrUnknownOutcome},
		{"rejected create", []*http.Response{response(404, ""), response(409, "")}, schemaregistry.ErrIncompatible},
		{"invalid ID", []*http.Response{response(404, ""), response(200, `{"id":0}`)}, ErrInvalidResponse},
		{"invalid existing ID", []*http.Response{response(200, `{"id":0,"version":2}`)}, ErrInvalidResponse},
		{"invalid existing version", []*http.Response{response(200, `{"id":7,"version":0}`)}, ErrInvalidResponse},
		{"trailing JSON", []*http.Response{response(200, `{"id":7,"version":2} {}`)}, ErrInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := internalProvider(t, sequentialTransport(test.responses...))
			result, err := provider.Register(context.Background(), schemaregistry.RegisterRequest{Subject: schemaregistry.Subject{Name: "s"}, Schema: schema})
			if !errors.Is(err, test.want) {
				t.Fatalf("Register() = (%+v, %v), want %v", result, err, test.want)
			}
			if errors.Is(test.want, schemaregistry.ErrUnknownOutcome) && result.Outcome != schemaregistry.RegistrationUnknown {
				t.Fatalf("Register() outcome = %s", result.Outcome)
			}
		})
	}

	provider = internalProvider(t, noCall)
	invalidLookups := []schemaregistry.Lookup{
		schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "other", Scope: "scope", Value: "1"}),
		schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: "0"}),
		schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: "x"}),
		schemaregistry.AtVersion(schemaregistry.Subject{}, schemaregistry.Version{Number: 1}),
		schemaregistry.AtVersion(schemaregistry.Subject{Name: "s"}, schemaregistry.Version{Opaque: "v"}),
		{},
	}
	for _, lookup := range invalidLookups {
		if _, err := provider.Resolve(context.Background(), lookup); err == nil {
			t.Fatalf("Resolve(%+v) error = nil", lookup)
		}
	}
	provider = internalProvider(t, sequentialTransport(response(404, "")))
	if _, err := provider.Resolve(context.Background(), schemaregistry.Latest(schemaregistry.Subject{Name: "s"})); !errors.Is(err, schemaregistry.ErrNotFound) {
		t.Fatalf("Resolve(not found) error = %v", err)
	}
	provider = internalProvider(t, sequentialTransport(response(200, `{"subject":"s","version":3,"id":8,"schema":"string","schemaType":"AVRO"}`)))
	latest, err := provider.Resolve(context.Background(), schemaregistry.Latest(schemaregistry.Subject{Name: "s"}))
	if err != nil || latest.Version.Number != 3 || latest.Subject.Name != "s" || latest.ID.Value != "8" {
		t.Fatalf("Resolve(latest) = (%+v, %v)", latest, err)
	}
	provider = internalProvider(t, sequentialTransport(response(200, `{"subject":"s","version":3,"schema":"string","schemaType":"AVRO"}`)))
	byID, err := provider.Resolve(context.Background(), schemaregistry.ByProviderID(
		schemaregistry.ProviderID{Provider: ProviderName, Scope: "scope", Value: "8"},
	))
	if err != nil || byID.ID.Value != "8" || byID.Subject.Name != "s" || byID.Version.Number != 3 {
		t.Fatalf("Resolve(provider ID coordinates) = (%+v, %v)", byID, err)
	}
	for _, responseBody := range []string{
		`{"subject":"s","version":3,"id":0,"schema":"string","schemaType":"AVRO"}`,
		`{"subject":"other","version":3,"id":8,"schema":"string","schemaType":"AVRO"}`,
		`{"subject":"s","version":2,"id":8,"schema":"string","schemaType":"AVRO"}`,
	} {
		provider = internalProvider(t, sequentialTransport(response(200, responseBody)))
		_, err := provider.Resolve(context.Background(), schemaregistry.AtVersion(
			schemaregistry.Subject{Name: "s"}, schemaregistry.Version{Number: 3},
		))
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("Resolve(incomplete version response) error = %v", err)
		}
	}

	provider = internalProvider(t, noCall)
	if _, err := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{}); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("CheckCompatibility(invalid subject) error = %v", err)
	}
	if _, err := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{Subject: schemaregistry.Subject{Name: "s"}}); !errors.Is(err, schemaregistry.ErrUnsupportedFormat) {
		t.Fatalf("CheckCompatibility(invalid schema) error = %v", err)
	}
	for _, test := range []struct {
		name      string
		responses []*http.Response
		want      error
	}{
		{"config failure", []*http.Response{response(500, "")}, schemaregistry.ErrUnavailable},
		{"invalid mode", []*http.Response{response(200, `{"compatibilityLevel":"INVALID"}`)}, ErrInvalidResponse},
		{"check failure", []*http.Response{response(200, `{"compatibilityLevel":"BACKWARD"}`), response(500, "")}, schemaregistry.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := internalProvider(t, sequentialTransport(test.responses...))
			_, err := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{
				Subject: schemaregistry.Subject{Name: "s"}, Candidate: schema, Mode: schemaregistry.CompatibilityBackward,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("CheckCompatibility() error = %v, want %v", err, test.want)
			}
		})
	}
	provider = internalProvider(t, sequentialTransport(
		response(200, `{"compatibilityLevel":"BACKWARD"}`),
		response(200, `{"is_compatible":false,"messages":["removed field"]}`),
	))
	compatibility, err := provider.CheckCompatibility(context.Background(), schemaregistry.CompatibilityRequest{
		Subject: schemaregistry.Subject{Name: "s"}, Candidate: schema, Mode: schemaregistry.CompatibilityBackward,
	})
	if err != nil || compatibility.Compatible || len(compatibility.Diagnostics) != 1 {
		t.Fatalf("CheckCompatibility(diagnostic) = (%+v, %v)", compatibility, err)
	}

	provider = internalProvider(t, noCall)
	for _, request := range []schemaregistry.ListRequest{{}, {Limit: 1, PageToken: "bad"}, {Limit: 1, PageToken: "-1"}} {
		if _, err := provider.List(context.Background(), request); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
			t.Fatalf("List(%+v) error = %v", request, err)
		}
	}
	provider = internalProvider(t, sequentialTransport(response(500, "")))
	if _, err := provider.List(context.Background(), schemaregistry.ListRequest{Limit: 1}); !errors.Is(err, schemaregistry.ErrUnavailable) {
		t.Fatalf("List(failure) error = %v", err)
	}
	for _, test := range []struct {
		name      string
		response  string
		request   schemaregistry.ListRequest
		wantError error
	}{
		{"oversized response", `["a","b","c"]`, schemaregistry.ListRequest{Limit: 1}, ErrInvalidResponse},
		{"prefix mismatch", `["other"]`, schemaregistry.ListRequest{SubjectPrefix: "wanted", Limit: 1}, ErrInvalidResponse},
		{
			"page token overflow", `["a","b"]`,
			schemaregistry.ListRequest{Limit: 1, PageToken: strconv.Itoa(int(^uint(0) >> 1))},
			schemaregistry.ErrInvalidRequest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := internalProvider(t, sequentialTransport(response(200, test.response)))
			if _, err := provider.List(context.Background(), test.request); !errors.Is(err, test.wantError) {
				t.Fatalf("List() error = %v, want %v", err, test.wantError)
			}
		})
	}
	provider = internalProvider(t, sequentialTransport(response(200, `["a"]`)))
	page, err := provider.List(context.Background(), schemaregistry.ListRequest{Limit: 1, PageToken: "0"})
	if err != nil || len(page.Schemas) != 1 || page.Schemas[0].Subject.Name != "a" || page.NextPageToken != "" {
		t.Fatalf("List(zero offset exact limit) = (%+v, %v)", page, err)
	}
	provider = internalProvider(t, sequentialTransport(response(200, `[]`)))
	page, err = provider.List(context.Background(), schemaregistry.ListRequest{Limit: 1, PageToken: "1"})
	if err != nil || len(page.Schemas) != 0 || page.NextPageToken != "" {
		t.Fatalf("List(end offset) = (%+v, %v)", page, err)
	}
	maximum := int(^uint(0) >> 1)
	provider = internalProvider(t, roundTripperFunction(func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("limit") != strconv.Itoa(maximum) {
			t.Fatalf("maximum list limit = %q", request.URL.Query().Get("limit"))
		}
		return response(200, `[]`), nil
	}))
	if _, err := provider.List(context.Background(), schemaregistry.ListRequest{Limit: maximum}); err != nil {
		t.Fatalf("List(maximum limit) error = %v", err)
	}
	provider = internalProvider(t, sequentialTransport(response(200, `["a","b"]`)))
	page, err = provider.List(context.Background(), schemaregistry.ListRequest{
		Limit: 1, PageToken: strconv.Itoa(maximum - 1),
	})
	if err != nil || page.NextPageToken != strconv.Itoa(maximum) {
		t.Fatalf("List(maximum page token) = (%+v, %v)", page, err)
	}
}

func TestDeletionAndReferenceCompilationBoundaries(t *testing.T) {
	t.Parallel()

	schema := mustInternalSchema(t)
	noCall := roundTripperFunction(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected transport call")
		return nil, nil
	})
	provider := internalProvider(t, noCall)
	if _, err := provider.Delete(context.Background(), schemaregistry.DeleteRequest{}); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("Delete(invalid) error = %v", err)
	}
	request := schemaregistry.DeleteRequest{
		Subject: schemaregistry.Subject{Name: "s"}, Version: schemaregistry.Version{Number: 1},
		Policy: schemaregistry.DeletionPolicy{Mode: schemaregistry.DeleteSoft, ExpectedFingerprint: schema.Fingerprint()},
	}
	provider = internalProvider(t, sequentialTransport(response(404, "")))
	if _, err := provider.Delete(context.Background(), request); !errors.Is(err, schemaregistry.ErrNotFound) {
		t.Fatalf("Delete(resolve failure) error = %v", err)
	}
	other, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{Format: schemaregistry.FormatAvro, Content: []byte("other")}, canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
		return definition.Content, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	mismatch := request
	mismatch.Policy.ExpectedFingerprint = other.Fingerprint()
	provider = internalProvider(t, sequentialTransport(response(200, `{"subject":"s","version":1,"id":7,"schema":"string","schemaType":"AVRO"}`)))
	if _, err := provider.Delete(context.Background(), mismatch); !errors.Is(err, schemaregistry.ErrConfirmationRequired) {
		t.Fatalf("Delete(mismatch) error = %v", err)
	}
	invalidMode := request
	invalidMode.Policy.Mode = "invalid"
	provider = internalProvider(t, sequentialTransport(response(200, `{"subject":"s","version":1,"id":7,"schema":"string","schemaType":"AVRO"}`)))
	if _, err := provider.Delete(context.Background(), invalidMode); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("Delete(invalid mode) error = %v", err)
	}
	for _, test := range []struct {
		name       string
		deleteBody *http.Response
		want       error
	}{
		{"transport", response(500, ""), schemaregistry.ErrUnavailable},
		{"wrong version", response(200, "2"), ErrInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := internalProvider(t, sequentialTransport(
				response(200, `{"subject":"s","version":1,"id":7,"schema":"string","schemaType":"AVRO"}`), test.deleteBody,
			))
			if _, err := provider.Delete(context.Background(), request); !errors.Is(err, test.want) {
				t.Fatalf("Delete() error = %v, want %v", err, test.want)
			}
		})
	}
	provider = internalProvider(t, sequentialTransport(
		response(200, `{"subject":"s","version":1,"id":7,"schema":"string","schemaType":"AVRO"}`), response(200, "1"),
	))
	deleted, err := provider.Delete(context.Background(), request)
	if err != nil || deleted.Lifecycle != schemaregistry.LifecycleDeleting {
		t.Fatalf("Delete(soft) = (%+v, %v)", deleted, err)
	}

	compile := func(provider *Provider, value registeredSchema, coordinate schemaregistry.ReferenceCoordinate, state map[schemaregistry.ReferenceCoordinate]uint8, schemas, references, depth int) error {
		_, err := provider.compileResponse(context.Background(), value, coordinate, state, &schemas, &references, depth)
		return err
	}
	base := registeredSchema{Schema: "string", SchemaType: "AVRO"}
	provider = internalProvider(t, noCall)
	if err := compile(provider, base, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, provider.referenceLimits.MaxDepth+1); !errors.Is(err, schemaregistry.ErrReferenceLimit) {
		t.Fatalf("compileResponse(depth) error = %v", err)
	}
	if err := compile(provider, base, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, provider.referenceLimits.MaxSchemas, 0, 1); !errors.Is(err, schemaregistry.ErrReferenceLimit) {
		t.Fatalf("compileResponse(schemas) error = %v", err)
	}
	if err := compile(provider, base, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, provider.referenceLimits.MaxSchemas-1, 0, provider.referenceLimits.MaxDepth); err != nil {
		t.Fatalf("compileResponse(exact schema and depth limits) error = %v", err)
	}
	coordinate := referenceCoordinate("s", 1)
	if err := compile(provider, base, coordinate, map[schemaregistry.ReferenceCoordinate]uint8{coordinate: 1}, 0, 0, 1); !errors.Is(err, schemaregistry.ErrReferenceCycle) {
		t.Fatalf("compileResponse(cycle) error = %v", err)
	}
	if err := compile(provider, base, coordinate, map[schemaregistry.ReferenceCoordinate]uint8{coordinate: 2}, 0, 0, 1); err != nil {
		t.Fatalf("compileResponse(shared DAG) error = %v", err)
	}
	invalidReferences := []schemaReference{{}, {Name: "x"}, {Name: "x", Subject: "s"}}
	for _, reference := range invalidReferences {
		value := base
		value.References = []schemaReference{reference}
		if err := compile(provider, value, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, 1); !errors.Is(err, schemaregistry.ErrReferenceLimit) {
			t.Fatalf("compileResponse(%+v) error = %v", reference, err)
		}
	}
	value := base
	value.References = []schemaReference{{Name: "x", Subject: "s", Version: 1}}
	if err := compile(provider, value, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, provider.referenceLimits.MaxReferences, 1); !errors.Is(err, schemaregistry.ErrReferenceLimit) {
		t.Fatalf("compileResponse(reference count) error = %v", err)
	}
	provider = internalProvider(t, sequentialTransport(response(200, `{"schema":"string","schemaType":"AVRO"}`)))
	if err := compile(provider, value, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, provider.referenceLimits.MaxReferences-1, provider.referenceLimits.MaxDepth-1); err != nil {
		t.Fatalf("compileResponse(exact reference and recursive depth limits) error = %v", err)
	}
	provider = internalProvider(t, sequentialTransport(
		response(200, `{"schema":"string","schemaType":"AVRO","references":[{"name":"nested","subject":"nested","version":1}]}`),
		response(200, `{"schema":"string","schemaType":"AVRO"}`),
	))
	provider.referenceLimits.MaxDepth = 2
	if err := compile(provider, value, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, 1); !errors.Is(err, schemaregistry.ErrReferenceLimit) {
		t.Fatalf("compileResponse(nested depth) error = %v", err)
	}
	for _, test := range []struct {
		name     string
		response *http.Response
		want     error
	}{
		{"missing", response(404, ""), schemaregistry.ErrReferenceMissing},
		{"unavailable", response(500, ""), schemaregistry.ErrUnavailable},
	} {
		provider := internalProvider(t, sequentialTransport(test.response))
		if err := compile(provider, value, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, 1); !errors.Is(err, test.want) {
			t.Fatalf("compileResponse(%s) error = %v", test.name, err)
		}
	}
	cycleResponse := `{"schema":"string","schemaType":"AVRO","references":[{"name":"x","subject":"s","version":1}]}`
	provider = internalProvider(t, sequentialTransport(response(200, cycleResponse), response(200, cycleResponse)))
	if err := compile(provider, value, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, 1); !errors.Is(err, schemaregistry.ErrReferenceCycle) {
		t.Fatalf("compileResponse(dependency cycle) error = %v", err)
	}
	badType := base
	badType.SchemaType = "YAML"
	if err := compile(provider, badType, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, 1); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("compileResponse(type) error = %v", err)
	}
	jsonValue := base
	jsonValue.SchemaType = "JSON"
	if err := compile(provider, jsonValue, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, 1); !errors.Is(err, schemaregistry.ErrUnsupportedFormat) {
		t.Fatalf("compileResponse(canonicalizer) error = %v", err)
	}
	canonicalError := errors.New("canonical")
	provider.canonicalizers[schemaregistry.FormatAvro] = canonicalizerFunction(func(context.Context, schemaregistry.Definition) ([]byte, error) { return nil, canonicalError })
	if err := compile(provider, base, schemaregistry.ReferenceCoordinate{}, map[schemaregistry.ReferenceCoordinate]uint8{}, 0, 0, 1); !errors.Is(err, canonicalError) {
		t.Fatalf("compileResponse(canonical error) error = %v", err)
	}

	badReferenceSchema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte("string"),
		References: []schemaregistry.Reference{{Name: "x", Fingerprint: schema.Fingerprint()}},
	}, canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
		return definition.Content, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.requestForSchema(badReferenceSchema); !errors.Is(err, schemaregistry.ErrInvalidRequest) {
		t.Fatalf("requestForSchema(bad reference) error = %v", err)
	}
	validReferenceSchema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte("string"),
		References: []schemaregistry.Reference{{Name: "x", Subject: "s", Version: 1, Fingerprint: schema.Fingerprint()}},
	}, canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
		return definition.Content, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if body, err := provider.requestForSchema(validReferenceSchema); err != nil || !strings.Contains(string(body), `"subject":"s"`) {
		t.Fatalf("requestForSchema(valid reference) = (%s, %v)", body, err)
	}
}

func TestHTTPRequestConstructionAndCanceledRetry(t *testing.T) {
	t.Parallel()

	provider := internalProvider(t, roundTripperFunction(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}))
	if err := provider.doJSON(context.Background(), http.MethodGet, "/%", nil, nil); err == nil {
		t.Fatal("doJSON(invalid path) error = nil")
	}
	if err := provider.doJSON(context.Background(), "bad\nmethod", "/x", nil, nil); err == nil {
		t.Fatal("doJSON(invalid method) error = nil")
	}

	for _, status := range []int{0, http.StatusServiceUnavailable} {
		ctx, cancel := context.WithCancel(context.Background())
		provider = internalProvider(t, roundTripperFunction(func(*http.Request) (*http.Response, error) {
			cancel()
			if status == 0 {
				return nil, errors.New("transport")
			}
			return response(status, ""), nil
		}))
		provider.maxAttempts = 2
		provider.retryDelay = time.Second
		if err := provider.doJSON(ctx, http.MethodGet, "/x", nil, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("doJSON(canceled retry, status=%d) error = %v", status, err)
		}
	}
}

type roundTripperFunction func(*http.Request) (*http.Response, error)

func (function roundTripperFunction) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type credentialFunction func(context.Context) (string, error)

func (function credentialFunction) Authorization(ctx context.Context) (string, error) {
	return function(ctx)
}

type errorReadCloser struct {
	readErr  error
	closeErr error
}

func (closer errorReadCloser) Read([]byte) (int, error) { return 0, closer.readErr }
func (closer errorReadCloser) Close() error             { return closer.closeErr }

func internalConfig(transport http.RoundTripper) Config {
	return Config{
		Endpoint: "https://registry.example/base", Scope: "scope", Transport: transport,
		RequestTimeout: time.Second, MaxResponseBytes: 128, MaxAttempts: 1,
		MaxConcurrent: 1, ReferenceLimits: schemaregistry.GraphLimits{MaxSchemas: 4, MaxDepth: 4, MaxReferences: 4},
		Canonicalizers: map[schemaregistry.Format]schemaregistry.Canonicalizer{
			schemaregistry.FormatAvro: canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
				return definition.Content, nil
			}),
		},
	}
}

func internalProvider(t *testing.T, transport http.RoundTripper) *Provider {
	t.Helper()
	provider, err := New(internalConfig(transport))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestNewAndMappingBoundaries(t *testing.T) {
	t.Parallel()

	transport := roundTripperFunction(func(*http.Request) (*http.Response, error) { return response(200, "{}"), nil })
	endpointPolicy := internalConfig(transport)
	endpointPolicy.Endpoint = "ftp://example.test"
	if _, err := New(endpointPolicy); err == nil || err.Error() != "confluent endpoint must use HTTPS" {
		t.Fatalf("New(unsupported endpoint) error = %v", err)
	}
	for _, endpoint := range []string{"%", "https:///missing", "https://user@example.test", "https://example.test?q=1", "https://example.test#fragment", "ftp://example.test"} {
		config := internalConfig(transport)
		config.Endpoint = endpoint
		if _, err := New(config); err == nil {
			t.Fatalf("New(%q) error = nil", endpoint)
		}
	}
	invalidConfigs := []Config{
		{},
		func() Config { config := internalConfig(transport); config.Scope = ""; return config }(),
		func() Config { config := internalConfig(transport); config.RequestTimeout = 0; return config }(),
		func() Config { config := internalConfig(transport); config.MaxResponseBytes = 0; return config }(),
		func() Config { config := internalConfig(transport); config.MaxAttempts = 0; return config }(),
		func() Config { config := internalConfig(transport); config.MaxConcurrent = 0; return config }(),
		func() Config { config := internalConfig(transport); config.RetryDelay = -1; return config }(),
		func() Config {
			config := internalConfig(transport)
			config.ReferenceLimits.MaxSchemas = 0
			return config
		}(),
		func() Config {
			config := internalConfig(transport)
			config.ReferenceLimits.MaxDepth = 0
			return config
		}(),
		func() Config {
			config := internalConfig(transport)
			config.ReferenceLimits.MaxReferences = 0
			return config
		}(),
		func() Config {
			config := internalConfig(transport)
			config.Canonicalizers["yaml"] = canonicalizerFunction(func(_ context.Context, definition schemaregistry.Definition) ([]byte, error) {
				return definition.Content, nil
			})
			return config
		}(),
	}
	for _, config := range invalidConfigs {
		if _, err := New(config); err == nil {
			t.Fatalf("New(invalid config) error = nil: %+v", config)
		}
	}
	config := internalConfig(transport)
	var nilCanonicalizer *canonicalizerFunction
	config.Canonicalizers[schemaregistry.FormatAvro] = nilCanonicalizer
	if _, err := New(config); err == nil {
		t.Fatal("New(nil canonicalizer) error = nil")
	}
	config = internalConfig(transport)
	config.Endpoint = "http://registry.example"
	config.AllowHTTPForTesting = true
	provider, err := New(config)
	if err != nil || provider.Capabilities().Provider != ProviderName {
		t.Fatalf("New(test HTTP) = (%+v, %v)", provider, err)
	}
	for format, want := range map[schemaregistry.Format]string{
		schemaregistry.FormatAvro: "AVRO", schemaregistry.FormatJSONSchema: "JSON", schemaregistry.FormatProtobuf: "PROTOBUF",
	} {
		got, err := schemaType(format)
		if err != nil || got != want {
			t.Fatalf("schemaType(%s) = (%q, %v)", format, got, err)
		}
	}
	if _, err := schemaType("yaml"); !errors.Is(err, schemaregistry.ErrUnsupportedFormat) {
		t.Fatalf("schemaType(unsupported) error = %v", err)
	}
	for value, want := range map[string]schemaregistry.Format{"": schemaregistry.FormatAvro, "AVRO": schemaregistry.FormatAvro, "JSON": schemaregistry.FormatJSONSchema, "PROTOBUF": schemaregistry.FormatProtobuf} {
		got, err := confluentFormat(value)
		if err != nil || got != want {
			t.Fatalf("confluentFormat(%q) = (%s, %v)", value, got, err)
		}
	}
	if _, err := confluentFormat("YAML"); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("confluentFormat(invalid) error = %v", err)
	}
	modes := map[string]schemaregistry.CompatibilityMode{
		"BACKWARD": schemaregistry.CompatibilityBackward, "BACKWARD_TRANSITIVE": schemaregistry.CompatibilityBackwardTransitive,
		"FORWARD": schemaregistry.CompatibilityForward, "FORWARD_TRANSITIVE": schemaregistry.CompatibilityForwardTransitive,
		"FULL": schemaregistry.CompatibilityFull, "FULL_TRANSITIVE": schemaregistry.CompatibilityFullTransitive, "NONE": schemaregistry.CompatibilityNone,
	}
	for value, want := range modes {
		got, err := compatibilityMode(value)
		if err != nil || got != want {
			t.Fatalf("compatibilityMode(%q) = (%s, %v)", value, got, err)
		}
	}
	if _, err := compatibilityMode("INVALID"); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("compatibilityMode(invalid) error = %v", err)
	}
	if interfaceIsNil(1) || !interfaceIsNil(nilCanonicalizer) {
		t.Fatal("interfaceIsNil() mismatch")
	}
}
