// Package nethttp instruments net/http servers and clients without recording
// raw URLs, hosts, headers, bodies, client addresses, or arbitrary methods.
package nethttp

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	otelpropagation "go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const scopeName = "github.com/faustbrian/golib/pkg/telemetry/instrumentation/nethttp"

var operationPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

// ServerConfig defines fixed, low-cardinality server instrumentation.
type ServerConfig struct {
	Operation      string
	Route          string
	TrustedInbound bool
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     otelpropagation.TextMapPropagator
}

// ClientConfig defines fixed, low-cardinality client instrumentation.
type ClientConfig struct {
	Operation      string
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     otelpropagation.TextMapPropagator
}

type serverHandler struct {
	next       http.Handler
	operation  string
	route      string
	tracer     trace.Tracer
	propagator otelpropagation.TextMapPropagator
	trusted    bool
	duration   metric.Float64Histogram
	requests   metric.Int64Counter
}

// NewHandler wraps handler with bounded tracing, metrics, and extraction.
func NewHandler(handler http.Handler, config ServerConfig) (http.Handler, error) {
	if handler == nil {
		return nil, errors.New("HTTP handler is required")
	}
	if err := validateOperation(config.Operation); err != nil {
		return nil, err
	}
	if len(config.Route) > 256 || strings.ContainsAny(config.Route, "?#") || (config.Route != "" && !strings.HasPrefix(config.Route, "/")) {
		return nil, errors.New("HTTP route must be a fixed path template")
	}
	tracerProvider, meterProvider, propagator := providers(
		config.TracerProvider,
		config.MeterProvider,
		config.Propagator,
	)
	meter := meterProvider.Meter(scopeName)
	duration, err := meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	requests, err := meter.Int64Counter("http.server.request.count", metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	return &serverHandler{
		next:       handler,
		operation:  config.Operation,
		route:      config.Route,
		tracer:     tracerProvider.Tracer(scopeName),
		propagator: propagator,
		trusted:    config.TrustedInbound,
		duration:   duration,
		requests:   requests,
	}, nil
}

func (handler *serverHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	carrier := otelpropagation.HeaderCarrier(request.Header)
	ctx := request.Context()
	if extractor, ok := handler.propagator.(trustedExtractor); handler.trusted && ok {
		ctx = extractor.ExtractTrusted(ctx, carrier)
	} else {
		ctx = handler.propagator.Extract(ctx, carrier)
	}
	attributes := []attribute.KeyValue{attribute.String("http.request.method", normalizeMethod(request.Method))}
	if handler.route != "" {
		attributes = append(attributes, attribute.String("http.route", handler.route))
	}
	ctx, span := handler.tracer.Start(
		ctx,
		handler.operation,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attributes...),
	)
	request = request.WithContext(ctx)

	started := time.Now()
	var captured httpsnoop.Metrics
	defer func() {
		panicValue := recover()
		status := captured.Code
		if status == 0 {
			status = http.StatusInternalServerError
		}
		resultAttributes := append(
			append([]attribute.KeyValue(nil), attributes...),
			attribute.Int("http.response.status_code", status),
		)
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= http.StatusInternalServerError || panicValue != nil {
			span.SetStatus(codes.Error, "HTTP server error")
		}
		duration := captured.Duration
		if duration <= 0 {
			duration = time.Since(started)
		}
		handler.duration.Record(ctx, duration.Seconds(), metric.WithAttributes(resultAttributes...))
		handler.requests.Add(ctx, 1, metric.WithAttributes(resultAttributes...))
		span.End()
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	captured = httpsnoop.CaptureMetrics(handler.next, writer, request)
}

type trustedExtractor interface {
	ExtractTrusted(context.Context, otelpropagation.TextMapCarrier) context.Context
}

// Transport is a privacy-preserving instrumented HTTP round tripper.
type Transport struct {
	base       http.RoundTripper
	operation  string
	tracer     trace.Tracer
	propagator otelpropagation.TextMapPropagator
	duration   metric.Float64Histogram
	requests   metric.Int64Counter
}

// NewTransport wraps base with bounded tracing, metrics, and injection.
func NewTransport(base http.RoundTripper, config ClientConfig) (*Transport, error) {
	if err := validateOperation(config.Operation); err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport
	}
	tracerProvider, meterProvider, propagator := providers(
		config.TracerProvider,
		config.MeterProvider,
		config.Propagator,
	)
	meter := meterProvider.Meter(scopeName)
	duration, err := meter.Float64Histogram("http.client.request.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	requests, err := meter.Int64Counter("http.client.request.count", metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	return &Transport{
		base:       base,
		operation:  config.Operation,
		tracer:     tracerProvider.Tracer(scopeName),
		propagator: propagator,
		duration:   duration,
		requests:   requests,
	}, nil
}

// RoundTrip records only normalized method, bounded status, and duration.
func (transport *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("HTTP request is required")
	}
	method := normalizeMethod(request.Method)
	attributes := []attribute.KeyValue{attribute.String("http.request.method", method)}
	ctx, span := transport.tracer.Start(
		request.Context(),
		transport.operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attributes...),
	)
	defer span.End()

	clone := request.Clone(ctx)
	clone.Header = request.Header.Clone()
	transport.propagator.Inject(ctx, otelpropagation.HeaderCarrier(clone.Header))
	started := time.Now()
	response, err := transport.base.RoundTrip(clone)
	if err != nil {
		span.SetStatus(codes.Error, "HTTP transport failed")
		attributes = append(attributes, attribute.String("error.type", "transport"))
	} else {
		status := response.StatusCode
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		attributes = append(attributes, attribute.Int("http.response.status_code", status))
		if status >= http.StatusBadRequest {
			span.SetStatus(codes.Error, "HTTP request failed")
		}
	}
	duration := time.Since(started).Seconds()
	transport.duration.Record(ctx, duration, metric.WithAttributes(attributes...))
	transport.requests.Add(ctx, 1, metric.WithAttributes(attributes...))
	return response, err
}

func providers(
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	propagator otelpropagation.TextMapPropagator,
) (trace.TracerProvider, metric.MeterProvider, otelpropagation.TextMapPropagator) {
	if tracerProvider == nil {
		tracerProvider = tracenoop.NewTracerProvider()
	}
	if meterProvider == nil {
		meterProvider = metricnoop.NewMeterProvider()
	}
	if propagator == nil {
		propagator = otelpropagation.TraceContext{}
	}
	return tracerProvider, meterProvider, propagator
}

func validateOperation(operation string) error {
	if !operationPattern.MatchString(operation) {
		return errors.New("HTTP operation must be a fixed low-cardinality name")
	}
	return nil
}

func normalizeMethod(method string) string {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut,
		http.MethodTrace:
		return method
	default:
		return "_OTHER"
	}
}
