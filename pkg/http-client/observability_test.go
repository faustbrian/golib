package httpclient

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTelemetryModelsOneOperationAndNumberedRetryAttempts(t *testing.T) {
	observer := &telemetryTestObserver{}
	propagator := &telemetryTestPropagator{t: t, requireAttemptContext: true}
	retry, err := NewRetryMiddleware(RetryOptions{
		Name: "retry", MaximumAttempts: 2,
		Clock:  telemetryTestClock{},
		Jitter: RetryJitterFunc(func(time.Duration) time.Duration { return 0 }),
	})
	if err != nil {
		t.Fatalf("construct retry middleware: %v", err)
	}
	attempts := 0
	client, err := New(Config{
		OperationIdentityGenerator: IdentifierGeneratorFunc(func(context.Context) (GeneratedIdentifier, error) {
			return GeneratedIdentifier{Value: "operation-1", EntropyBits: 128}, nil
		}),
		Telemetry: &TelemetryOptions{
			Observer: observer, Propagator: propagator,
			BaggageAllowlist: []string{"safe-key"},
		},
		Middleware: []Middleware{retry},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if request.Header.Get("X-Request-ID") != "operation-1" {
				t.Fatalf("correlation ID = %q", request.Header.Get("X-Request-ID"))
			}
			if request.Header.Get("traceparent") != "00-test-trace-01" {
				t.Fatalf("traceparent = %q", request.Header.Get("traceparent"))
			}
			if values := request.Header.Values("baggage"); len(values) != 1 || values[0] != "safe-key=visible" {
				t.Fatalf("filtered baggage = %#v", values)
			}
			status := http.StatusServiceUnavailable
			if attempts == 2 {
				status = http.StatusNoContent
			}
			return &http.Response{
				StatusCode: status, Header: make(http.Header), Body: http.NoBody,
				Request: request,
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("construct telemetry client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/widgets?secret=query", nil)
	request.Header.Set("baggage", "safe-key=visible, tenant=secret")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute telemetry request: %v", err)
	}
	_ = response.Body.Close()
	if request.Header.Get("X-Request-ID") != "" || request.Header.Get("traceparent") != "" ||
		request.Header.Get("baggage") != "safe-key=visible, tenant=secret" {
		t.Fatalf("caller headers mutated: %#v", request.Header)
	}

	events := observer.snapshot()
	want := []struct {
		phase   TelemetryPhase
		scope   TelemetryScope
		attempt int
		outcome TelemetryOutcome
		class   string
	}{
		{TelemetryStart, TelemetryOperation, 0, "", ""},
		{TelemetryStart, TelemetryAttempt, 1, "", ""},
		{TelemetryFinish, TelemetryAttempt, 1, TelemetryOutcomeHTTPError, "5xx"},
		{TelemetryStart, TelemetryAttempt, 2, "", ""},
		{TelemetryFinish, TelemetryAttempt, 2, TelemetryOutcomeSuccess, "2xx"},
		{TelemetryFinish, TelemetryOperation, 0, TelemetryOutcomeSuccess, "2xx"},
	}
	if len(events) != len(want) {
		t.Fatalf("telemetry events = %#v", events)
	}
	for index, expected := range want {
		event := events[index]
		if event.Phase != expected.phase || event.Scope != expected.scope ||
			event.Attempt != expected.attempt || event.Outcome != expected.outcome ||
			event.StatusClass != expected.class || event.Method != http.MethodGet ||
			event.Profile != PolicyProfileInteractiveV1 || event.OperationID != "operation-1" {
			t.Fatalf("event %d = %#v", index, event)
		}
	}
}

func TestTelemetryObserverAndPropagatorCanBeAdoptedIndependently(t *testing.T) {
	observer := &telemetryTestObserver{}
	observerClient, err := New(Config{
		Telemetry: &TelemetryOptions{Observer: observer},
		Transport: telemetryNoContentTransport(),
	})
	if err != nil {
		t.Fatalf("construct observer-only client: %v", err)
	}
	t.Cleanup(func() { _ = observerClient.Close() })
	response, err := observerClient.Do(mustTLSRequest(t, "https://example.test"))
	if err != nil {
		t.Fatalf("observer-only request: %v", err)
	}
	_ = response.Body.Close()
	if len(observer.snapshot()) != 4 {
		t.Fatalf("observer-only events = %#v", observer.snapshot())
	}

	propagator := &telemetryTestPropagator{t: t, requireAttemptContext: false}
	propagatorClient, err := New(Config{
		Telemetry: &TelemetryOptions{Propagator: propagator},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("traceparent") == "" {
				t.Fatal("propagator-only request has no trace context")
			}
			return telemetryNoContentResponse(request), nil
		}),
	})
	if err != nil {
		t.Fatalf("construct propagator-only client: %v", err)
	}
	t.Cleanup(func() { _ = propagatorClient.Close() })
	response, err = propagatorClient.Do(mustTLSRequest(t, "https://example.test"))
	if err != nil {
		t.Fatalf("propagator-only request: %v", err)
	}
	_ = response.Body.Close()
}

func TestTelemetryStripsTrustBoundaryHeadersAndFiltersBaggage(t *testing.T) {
	var captured []http.Header
	client, err := New(Config{
		Telemetry: &TelemetryOptions{
			Observer:         &telemetryTestObserver{},
			BaggageAllowlist: []string{"safe"},
			SensitiveHeaders: []string{"X-Vendor-Secret"},
		},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			captured = append(captured, request.Header.Clone())
			if len(captured) == 1 {
				return &http.Response{
					StatusCode: http.StatusFound, Body: http.NoBody, Request: request,
					Header: http.Header{"Location": []string{"https://other.test/final"}},
				}, nil
			}
			return telemetryNoContentResponse(request), nil
		}),
	})
	if err != nil {
		t.Fatalf("construct boundary client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request := mustTLSRequest(t, "https://first.test/start")
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Cookie", "session=secret")
	request.Header.Set("Proxy-Authorization", "Basic secret")
	request.Header.Set("X-Vendor-Secret", "secret")
	request.Header.Set("Traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	request.Header.Add("Baggage", "safe=kept, malformed")
	request.Header.Add("Baggage", "tenant=secret")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute redirect: %v", err)
	}
	_ = response.Body.Close()
	if len(captured) != 2 || captured[0].Get("Baggage") != "safe=kept" {
		t.Fatalf("captured headers = %#v", captured)
	}
	for _, name := range []string{
		"Authorization", "Cookie", "Proxy-Authorization", "X-Vendor-Secret",
		"Traceparent", "Tracestate", "Baggage",
	} {
		if captured[1].Get(name) != "" {
			t.Fatalf("redirect retained %s: %#v", name, captured[1])
		}
	}
	if request.Header.Get("X-Vendor-Secret") != "secret" ||
		request.Header.Values("Baggage")[1] != "tenant=secret" {
		t.Fatalf("caller headers mutated: %#v", request.Header)
	}
}

func TestTelemetryOutcomeAndCacheCategoriesAreBounded(t *testing.T) {
	request := mustTLSRequest(t, "https://example.test/private/path?secret=query")
	cacheResponse := func(status int, provenance CacheProvenance) *http.Response {
		return withCacheMetadata(&http.Response{
			StatusCode: status, Header: make(http.Header), Body: http.NoBody,
			Request: request,
		}, CacheMetadata{Provenance: provenance})
	}
	transportFailure := newTransportError(request, errors.New("private transport detail"))
	for _, test := range []struct {
		name    string
		result  *http.Response
		failure error
		outcome TelemetryOutcome
		class   string
		cache   TelemetryCacheOutcome
	}{
		{"success", cacheResponse(204, CacheMiss), nil, TelemetryOutcomeSuccess, "2xx", TelemetryCacheMiss},
		{"HTTP", cacheResponse(503, CacheHit), nil, TelemetryOutcomeHTTPError, "5xx", TelemetryCacheHit},
		{"revalidated", cacheResponse(200, CacheRevalidated), nil, TelemetryOutcomeSuccess, "2xx", TelemetryCacheRevalidated},
		{"stale", cacheResponse(200, CacheStale), nil, TelemetryOutcomeSuccess, "2xx", TelemetryCacheStale},
		{"unknown cache", cacheResponse(200, CacheProvenance(99)), nil, TelemetryOutcomeSuccess, "2xx", TelemetryCacheNone},
		{"canceled", nil, context.Canceled, TelemetryOutcomeCanceled, "", TelemetryCacheNone},
		{"deadline", nil, context.DeadlineExceeded, TelemetryOutcomeCanceled, "", TelemetryCacheNone},
		{"rate capacity", nil, ErrRateLimitCapacity, TelemetryOutcomeRateLimited, "", TelemetryCacheNone},
		{"rate wait", nil, ErrRateLimitWaitExceeded, TelemetryOutcomeRateLimited, "", TelemetryCacheNone},
		{"circuit", nil, ErrCircuitRejected, TelemetryOutcomeCircuitOpen, "", TelemetryCacheNone},
		{"retry", nil, ErrRetryExhausted, TelemetryOutcomeRetryFailure, "", TelemetryCacheNone},
		{"transport", nil, transportFailure, TelemetryOutcomeTransport, "", TelemetryCacheNone},
		{"failure", nil, errors.New("private vendor detail"), TelemetryOutcomeFailure, "", TelemetryCacheNone},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := finishTelemetryEvent(TelemetryEvent{Cache: TelemetryCacheNone}, test.result, test.failure)
			if event.Phase != TelemetryFinish || event.Outcome != test.outcome ||
				event.StatusClass != test.class || event.Cache != test.cache {
				t.Fatalf("event = %#v", event)
			}
		})
	}
	if statusClass(0) != "invalid" || statusClass(600) != "invalid" {
		t.Fatal("invalid status escaped bounded classification")
	}
}

func TestTelemetryPolicyValidationAndObserverIsolation(t *testing.T) {
	var nilObserver *telemetryBoundaryObserver
	var nilPropagator *telemetryTestPropagator
	for _, options := range []*TelemetryOptions{
		{},
		{Observer: nilObserver},
		{Propagator: nilPropagator},
		{Observer: &telemetryTestObserver{}, CorrelationHeader: "bad header"},
		{Observer: &telemetryTestObserver{}, BaggageAllowlist: []string{""}},
		{Observer: &telemetryTestObserver{}, BaggageAllowlist: []string{"bad=name"}},
		{Observer: &telemetryTestObserver{}, BaggageAllowlist: []string{`bad"name`}},
		{Observer: &telemetryTestObserver{}, SensitiveHeaders: []string{"bad header"}},
	} {
		if _, err := New(Config{Telemetry: options}); !errors.Is(err, ErrInvalidTelemetry) {
			t.Fatalf("invalid telemetry %#v error = %v", options, err)
		}
	}
	observer := &telemetryBoundaryObserver{panicStart: true, panicFinish: true}
	client, err := New(Config{
		Telemetry: &TelemetryOptions{Observer: observer},
		Transport: telemetryNoContentTransport(),
	})
	if err != nil {
		t.Fatalf("construct isolated observer client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	response, err := client.Do(mustTLSRequest(t, "https://example.test"))
	if err != nil {
		t.Fatalf("observer panic affected request: %v", err)
	}
	_ = response.Body.Close()
}

func TestSlogTelemetryObserverLogsOnlySafeFixedFields(t *testing.T) {
	var output bytes.Buffer
	observer, err := NewSlogTelemetryObserver(slog.New(slog.NewJSONHandler(
		&output,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)))
	if err != nil {
		t.Fatalf("construct slog observer: %v", err)
	}
	event := TelemetryEvent{
		Phase: TelemetryFinish, Scope: TelemetryOperation,
		OperationID: "operation-1", Method: http.MethodPost,
		Profile: PolicyProfileWebhookDeliveryV1,
		Outcome: TelemetryOutcomeFailure, StatusClass: "5xx",
		Cache: TelemetryCacheMiss,
	}
	ctx := observer.Start(context.Background(), TelemetryEvent{
		Phase: TelemetryStart, Scope: TelemetryOperation,
		OperationID: "operation-1", Method: http.MethodPost,
		Profile: PolicyProfileWebhookDeliveryV1, Cache: TelemetryCacheNone,
	})
	observer.Finish(ctx, event)
	logged := output.String()
	for _, want := range []string{
		`"phase":"start"`, `"phase":"finish"`, `"scope":"operation"`,
		`"operation_id":"operation-1"`, `"outcome":"failure"`,
		`"status_class":"5xx"`, `"cache":"miss"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("slog output missing %s: %s", want, logged)
		}
	}
	for _, secret := range []string{"private/path", "secret=query", "vendor detail", "Authorization"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("slog output contains %q: %s", secret, logged)
		}
	}
	if _, err := NewSlogTelemetryObserver(nil); !errors.Is(err, ErrInvalidTelemetry) {
		t.Fatalf("nil slog logger error = %v", err)
	}
}

func TestW3CTraceContextIsValidatedAndInjectedOnTrustedAttempts(t *testing.T) {
	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ctx, err := WithW3CTraceContext(
		context.Background(),
		traceparent,
		"vendor=value,other=opaque",
	)
	if err != nil {
		t.Fatalf("attach trace context: %v", err)
	}
	client, err := New(Config{
		Telemetry: &TelemetryOptions{Propagator: W3CTraceContextPropagator{}},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Traceparent") != traceparent ||
				request.Header.Get("Tracestate") != "vendor=value,other=opaque" {
				t.Fatalf("W3C headers = %#v", request.Header)
			}
			return telemetryNoContentResponse(request), nil
		}),
	})
	if err != nil {
		t.Fatalf("construct W3C client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute W3C request: %v", err)
	}
	_ = response.Body.Close()
	if request.Header.Get("Traceparent") != "" || request.Header.Get("Tracestate") != "" {
		t.Fatalf("caller trace headers mutated: %#v", request.Header)
	}
	resolved, ok := W3CTraceContextFromContext(ctx)
	if !ok || resolved.Traceparent != traceparent || resolved.Tracestate == "" {
		t.Fatalf("resolved trace context = %#v, %t", resolved, ok)
	}

	for _, invalid := range []struct{ parent, state string }{
		{"", ""},
		{"00-00000000000000000000000000000000-00f067aa0ba902b7-01", ""},
		{"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01", ""},
		{"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", ""},
		{traceparent, "Bad Key=value"},
		{traceparent, "duplicate=one,duplicate=two"},
		{traceparent, strings.Repeat("a", 513)},
		{traceparent, strings.Repeat("a=v,", 32) + "z=v"},
		{traceparent, "=value"},
		{traceparent, strings.Repeat("a", 257) + "=value"},
		{traceparent, "a@b@c=value"},
		{traceparent, strings.Repeat("a", 242) + "@vendor=value"},
		{traceparent, "tenant@" + strings.Repeat("a", 15) + "=value"},
		{traceparent, "bad~=value"},
		{traceparent, "vendor= leading"},
		{traceparent, "vendor=" + strings.Repeat("a", 257)},
		{traceparent, "vendor=bad=value"},
	} {
		if _, err := WithW3CTraceContext(context.Background(), invalid.parent, invalid.state); !errors.Is(err, ErrInvalidTraceContext) {
			t.Fatalf("invalid trace context %#v error = %v", invalid, err)
		}
	}
	var nilContext context.Context
	if _, err := WithW3CTraceContext(nilContext, traceparent, ""); !errors.Is(err, ErrInvalidTraceContext) {
		t.Fatalf("nil trace context error = %v", err)
	}
	if _, ok := W3CTraceContextFromContext(nilContext); ok {
		t.Fatal("nil context returned W3C trace context")
	}
	emptyState, err := WithW3CTraceContext(context.Background(), traceparent, "")
	if err != nil {
		t.Fatalf("attach empty tracestate: %v", err)
	}
	header := http.Header{"Tracestate": []string{"stale=value"}}
	W3CTraceContextPropagator{}.Inject(emptyState, header)
	if header.Get("Traceparent") != traceparent || header.Get("Tracestate") != "" {
		t.Fatalf("empty-state injection = %#v", header)
	}
	untouched := http.Header{}
	W3CTraceContextPropagator{}.Inject(context.Background(), untouched)
	if len(untouched) != 0 {
		t.Fatalf("missing-context injection = %#v", untouched)
	}
	multiTenant, err := WithW3CTraceContext(context.Background(), traceparent, "tenant@vendor=value")
	if err != nil {
		t.Fatalf("attach multi-tenant tracestate: %v", err)
	}
	if resolved, _ := W3CTraceContextFromContext(multiTenant); resolved.Tracestate != "tenant@vendor=value" {
		t.Fatalf("multi-tenant tracestate = %#v", resolved)
	}
}

func TestTelemetryMetricLabelsExcludeHighCardinalityIdentityAndMethods(t *testing.T) {
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace,
	} {
		if got := telemetryMethod(method); got != method {
			t.Fatalf("standard method %q classified as %q", method, got)
		}
	}
	event := TelemetryEvent{
		Phase: TelemetryFinish, Scope: TelemetryOperation,
		OperationID: "tenant-secret-operation", Method: "PRIVATE-tenant-secret",
		Profile: PolicyProfileInteractiveV1, Outcome: TelemetryOutcomeSuccess,
		StatusClass: "2xx", Cache: TelemetryCacheNone,
	}
	labels := event.MetricLabels()
	if labels.Method != "OTHER" || labels.Scope != TelemetryOperation ||
		labels.Profile != PolicyProfileInteractiveV1 ||
		labels.Outcome != TelemetryOutcomeSuccess || labels.StatusClass != "2xx" ||
		labels.Cache != TelemetryCacheNone {
		t.Fatalf("metric labels = %#v", labels)
	}
	if strings.Contains(strings.Join([]string{
		labels.Method, string(labels.Scope), string(labels.Profile),
		string(labels.Outcome), labels.StatusClass, string(labels.Cache),
	}, "|"), "tenant-secret") {
		t.Fatalf("metric labels leaked high-cardinality data: %#v", labels)
	}
}

type telemetryTestObserver struct {
	mu     sync.Mutex
	events []TelemetryEvent
}

func (observer *telemetryTestObserver) Start(ctx context.Context, event TelemetryEvent) context.Context {
	observer.record(event)
	return context.WithValue(ctx, telemetryTestContextKey{}, event.Scope)
}

func (observer *telemetryTestObserver) Finish(ctx context.Context, event TelemetryEvent) {
	if ctx.Value(telemetryTestContextKey{}) != event.Scope {
		panic("telemetry context was not preserved")
	}
	observer.record(event)
}

func (observer *telemetryTestObserver) record(event TelemetryEvent) {
	observer.mu.Lock()
	observer.events = append(observer.events, event)
	observer.mu.Unlock()
}

func (observer *telemetryTestObserver) snapshot() []TelemetryEvent {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]TelemetryEvent(nil), observer.events...)
}

type telemetryTestPropagator struct {
	t                     *testing.T
	requireAttemptContext bool
}

func (propagator *telemetryTestPropagator) Inject(ctx context.Context, header http.Header) {
	propagator.t.Helper()
	if propagator.requireAttemptContext && ctx.Value(telemetryTestContextKey{}) != TelemetryAttempt {
		propagator.t.Fatal("propagator did not receive attempt span context")
	}
	header.Set("traceparent", "00-test-trace-01")
	header.Add("baggage", "injected-secret=secret")
}

type telemetryTestClock struct{}

func (telemetryTestClock) Now() time.Time                            { return time.Unix(1_700_000_000, 0) }
func (telemetryTestClock) Wait(context.Context, time.Duration) error { return nil }

type telemetryTestContextKey struct{}

type telemetryBoundaryObserver struct {
	panicStart  bool
	panicFinish bool
}

func (observer *telemetryBoundaryObserver) Start(ctx context.Context, _ TelemetryEvent) context.Context {
	if observer.panicStart {
		panic("observer start")
	}
	return nil
}

func (observer *telemetryBoundaryObserver) Finish(context.Context, TelemetryEvent) {
	if observer.panicFinish {
		panic("observer finish")
	}
}

func telemetryNoContentTransport() http.RoundTripper {
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return telemetryNoContentResponse(request), nil
	})
}

func telemetryNoContentResponse(request *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusNoContent, Header: make(http.Header),
		Body: http.NoBody, Request: request,
	}
}
