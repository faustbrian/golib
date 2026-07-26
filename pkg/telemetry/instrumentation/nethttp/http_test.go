package nethttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	telemetrypropagation "github.com/faustbrian/golib/pkg/telemetry/propagation"
	"github.com/faustbrian/golib/pkg/telemetry/testtelemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestHandlerRecordsOnlyBoundedServerAttributes(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	policy, err := telemetrypropagation.New(telemetrypropagation.DefaultConfig())
	if err != nil {
		t.Fatalf("propagation.New() error = %v", err)
	}
	handler, err := NewHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
	}), ServerConfig{
		Operation:      "users.show",
		Route:          "/users/{user}",
		TracerProvider: harness.TracerProvider(),
		MeterProvider:  harness.MeterProvider(),
		Propagator:     policy,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest("CUSTOM-secret", "https://attacker.example/users/secret-id?token=secret", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	spans := harness.Spans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name != "users.show" || span.Parent.SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("span name/parent = %q/%s, want fixed operation and remote parent", span.Name, span.Parent.SpanID())
	}
	attributes := attributeMap(span.Attributes)
	if attributes["http.request.method"] != "_OTHER" || attributes["http.route"] != "/users/{user}" {
		t.Fatalf("span attributes = %+v, want normalized method and route template", attributes)
	}
	assertNoSecret(t, attributes, "secret", "attacker.example", "token")
}

func TestTransportInjectsContextWithoutRecordingTargetData(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	policyConfig := telemetrypropagation.DefaultConfig()
	policyConfig.BaggageEnabled = true
	policyConfig.TrustedBaggageKeys = []string{"tenant.tier"}
	policy, err := telemetrypropagation.New(policyConfig)
	if err != nil {
		t.Fatalf("propagation.New() error = %v", err)
	}
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("traceparent") == "stale" || request.Header.Get("traceparent") == "" {
			t.Fatalf("traceparent = %q, want injected context", request.Header.Get("traceparent"))
		}
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("secret response body")),
			Request:    request,
		}, nil
	})
	transport, err := NewTransport(base, ClientConfig{
		Operation:      "payments.request",
		TracerProvider: harness.TracerProvider(),
		MeterProvider:  harness.MeterProvider(),
		Propagator:     policy,
	})
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), "CUSTOM-secret", "https://api.example/users/secret-id?token=secret", nil)
	request.Header.Set("traceparent", "stale")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	_ = response.Body.Close()

	spans := harness.Spans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	span := spans[0]
	attributes := attributeMap(span.Attributes)
	if span.Name != "payments.request" || span.Status.Code != codes.Error {
		t.Fatalf("span name/status = %q/%v, want fixed operation/error", span.Name, span.Status.Code)
	}
	if attributes["http.request.method"] != "_OTHER" || attributes["http.response.status_code"] != int64(502) {
		t.Fatalf("span attributes = %+v, want normalized method and status", attributes)
	}
	assertNoSecret(t, attributes, "secret", "api.example", "token")
}

func TestTransportDoesNotRecordTransportErrors(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	want := errors.New("credential secret failed for https://api.example/private")
	transport, err := NewTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, want
	}), ClientConfig{Operation: "payments.request", TracerProvider: harness.TracerProvider()})
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://api.example/private", nil)
	response, roundTripErr := transport.RoundTrip(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(roundTripErr, want) {
		t.Fatalf("RoundTrip() error = %v, want %v", roundTripErr, want)
	}
	span := harness.Spans()[0]
	if strings.Contains(span.Status.Description, "secret") || len(span.Events) != 0 {
		t.Fatalf("span leaked transport error: status=%q events=%+v", span.Status.Description, span.Events)
	}
}

func TestConfigurationRequiresFixedOperation(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ServerConfig{}); err == nil {
		t.Fatal("NewHandler() error = nil, want operation validation error")
	}
	if _, err := NewTransport(nil, ClientConfig{}); err == nil {
		t.Fatal("NewTransport() error = nil, want operation validation error")
	}
}

func TestHandlerValidatesInputAndPreservesPanics(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(nil, ServerConfig{Operation: "server.request"}); err == nil {
		t.Fatal("NewHandler(nil) error = nil, want handler error")
	}
	for _, route := range []string{"relative", "/users?secret=true", "/users#secret", "/" + strings.Repeat("x", 256)} {
		if _, err := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ServerConfig{
			Operation: "server.request",
			Route:     route,
		}); err == nil {
			t.Fatalf("NewHandler() route %q error = nil, want validation error", route)
		}
	}
	harness := testtelemetry.New()
	handler, err := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(5 * time.Millisecond)
		panic("secret panic")
	}), ServerConfig{
		Operation:      "server.request",
		TracerProvider: harness.TracerProvider(),
		MeterProvider:  harness.MeterProvider(),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	defer func() {
		if recovered := recover(); recovered != "secret panic" {
			t.Fatalf("recovered = %v, want original panic", recovered)
		}
		span := harness.Spans()[0]
		if span.Status.Code != codes.Error || strings.Contains(fmt.Sprint(span), "secret") {
			t.Fatalf("panic telemetry is unsafe: %+v", span)
		}
		metrics, err := harness.Metrics(context.Background())
		if err != nil {
			t.Fatalf("Metrics() error = %v", err)
		}
		var duration float64
		for _, scope := range metrics.ScopeMetrics {
			for _, measurement := range scope.Metrics {
				if measurement.Name != "http.server.request.duration" {
					continue
				}
				histogram := measurement.Data.(metricdata.Histogram[float64])
				duration = histogram.DataPoints[0].Sum
			}
		}
		if duration <= 0 {
			t.Fatalf("panic duration = %f, want elapsed handler time", duration)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestHandlerAndTransportUseNoopDefaults(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ServerConfig{
		Operation: "server.request",
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	transport, err := NewTransport(roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	}), ClientConfig{Operation: "client.request"})
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	_ = response.Body.Close()
	response, err = transport.RoundTrip(nil)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("RoundTrip(nil) error = nil, want request error")
	}
	if _, err := NewTransport(nil, ClientConfig{Operation: "client.request"}); err != nil {
		t.Fatalf("NewTransport(nil) error = %v", err)
	}
}

func TestHandlerMarksServerFailures(t *testing.T) {
	t.Parallel()

	harness := testtelemetry.New()
	handler, err := NewHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}), ServerConfig{Operation: "server.request", TracerProvider: harness.TracerProvider()})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if span := harness.Spans()[0]; span.Status.Code != codes.Error {
		t.Fatalf("span status = %v, want error", span.Status.Code)
	}
}

func TestConstructorsReportInstrumentFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument failed")
	histogramProvider := errorMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: errorMeter{
		Meter:        metricnoop.NewMeterProvider().Meter("test"),
		histogramErr: want,
	}}
	counterProvider := errorMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: errorMeter{
		Meter:      metricnoop.NewMeterProvider().Meter("test"),
		counterErr: want,
	}}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if _, err := NewHandler(handler, ServerConfig{Operation: "server.request", MeterProvider: histogramProvider}); !errors.Is(err, want) {
		t.Fatalf("NewHandler() histogram error = %v, want %v", err, want)
	}
	if _, err := NewHandler(handler, ServerConfig{Operation: "server.request", MeterProvider: counterProvider}); !errors.Is(err, want) {
		t.Fatalf("NewHandler() counter error = %v, want %v", err, want)
	}
	if _, err := NewTransport(http.DefaultTransport, ClientConfig{Operation: "client.request", MeterProvider: histogramProvider}); !errors.Is(err, want) {
		t.Fatalf("NewTransport() histogram error = %v, want %v", err, want)
	}
	if _, err := NewTransport(http.DefaultTransport, ClientConfig{Operation: "client.request", MeterProvider: counterProvider}); !errors.Is(err, want) {
		t.Fatalf("NewTransport() counter error = %v, want %v", err, want)
	}
}

func TestHandlerCanExplicitlyTrustInboundBaggage(t *testing.T) {
	t.Parallel()

	policyConfig := telemetrypropagation.DefaultConfig()
	policyConfig.BaggageEnabled = true
	policyConfig.TrustedBaggageKeys = []string{"tenant.tier"}
	policy, err := telemetrypropagation.New(policyConfig)
	if err != nil {
		t.Fatalf("propagation.New() error = %v", err)
	}
	var tier string
	handler, err := NewHandler(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		tier = baggage.FromContext(request.Context()).Member("tenant.tier").Value()
	}), ServerConfig{
		Operation:      "trusted.request",
		TrustedInbound: true,
		Propagator:     policy,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("baggage", "tenant.tier=gold,user.id=secret")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if tier != "gold" {
		t.Fatalf("trusted tenant tier = %q, want gold", tier)
	}
}

func TestTrustedHandlerFallsBackToStandardExtractor(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ServerConfig{
		Operation:      "trusted.request",
		TrustedInbound: true,
		Propagator:     otelpropagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type errorMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider errorMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type errorMeter struct {
	metric.Meter
	histogramErr error
	counterErr   error
}

func (meter errorMeter) Float64Histogram(string, ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if meter.histogramErr != nil {
		return nil, meter.histogramErr
	}
	return meter.Meter.Float64Histogram("ok")
}

func (meter errorMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if meter.counterErr != nil {
		return nil, meter.counterErr
	}
	return meter.Meter.Int64Counter("ok")
}

func attributeMap(attributes []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(attributes))
	for _, item := range attributes {
		result[string(item.Key)] = item.Value.AsInterface()
	}
	return result
}

func assertNoSecret(t *testing.T, attributes map[string]any, needles ...string) {
	t.Helper()
	for key, value := range attributes {
		text := key + "=" + fmt.Sprint(value)
		for _, needle := range needles {
			if strings.Contains(text, needle) {
				t.Fatalf("attribute %q leaked %q", text, needle)
			}
		}
	}
}

var _ otelpropagation.TextMapPropagator = (*telemetrypropagation.Policy)(nil)
