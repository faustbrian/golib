package otlp

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestHTTPCollectorInteroperability(t *testing.T) {
	t.Parallel()

	var traces atomic.Int64
	var metrics atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("authorization") != "test-token" {
			t.Errorf("authorization = %q, want test-token", request.Header.Get("authorization"))
		}
		body := readOTLPBody(t, request)
		writer.Header().Set("content-type", "application/x-protobuf")
		switch request.URL.Path {
		case "/custom/v1/traces":
			message := &collectortrace.ExportTraceServiceRequest{}
			if err := proto.Unmarshal(body, message); err != nil {
				t.Errorf("unmarshal traces: %v", err)
			}
			traces.Add(countSpans(message))
			response, _ := proto.Marshal(&collectortrace.ExportTraceServiceResponse{})
			_, _ = writer.Write(response)
		case "/custom/v1/metrics":
			message := &collectormetric.ExportMetricsServiceRequest{}
			if err := proto.Unmarshal(body, message); err != nil {
				t.Errorf("unmarshal metrics: %v", err)
			}
			metrics.Add(countMetrics(message))
			response, _ := proto.Marshal(&collectormetric.ExportMetricsServiceResponse{})
			_, _ = writer.Write(response)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	config := integrationConfig(ProtocolHTTPProtobuf, strings.TrimPrefix(server.URL, "http://"))
	config.URLPath = "/custom/v1/traces"
	exportTrace(t, config)
	config.URLPath = "/custom/v1/metrics"
	exportMetric(t, config)
	if traces.Load() != 1 {
		t.Fatalf("exported spans = %d, want 1", traces.Load())
	}
	if metrics.Load() == 0 {
		t.Fatal("exported metrics = 0, want at least 1")
	}
}

func TestGRPCCollectorInteroperabilityAndRetry(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := grpc.NewServer()
	traceService := &traceCollector{failuresRemaining: 2}
	metricService := &metricCollector{}
	collectortrace.RegisterTraceServiceServer(server, traceService)
	collectormetric.RegisterMetricsServiceServer(server, metricService)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	config := integrationConfig(ProtocolGRPC, listener.Addr().String())
	exportTrace(t, config)
	exportMetric(t, config)
	if traceService.attempts.Load() != 3 || traceService.spans.Load() != 1 {
		t.Fatalf("trace attempts/spans = %d/%d, want 3/1", traceService.attempts.Load(), traceService.spans.Load())
	}
	if metricService.metrics.Load() == 0 {
		t.Fatal("gRPC exported metrics = 0, want at least 1")
	}
}

func TestHTTPExporterFailureModes(t *testing.T) {
	t.Parallel()

	t.Run("rate limited then available", func(t *testing.T) {
		var attempts atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			attempt := attempts.Add(1)
			if attempt < 3 {
				writer.Header().Set("Retry-After", "0")
				http.Error(writer, "rate limited", http.StatusTooManyRequests)
				return
			}
			response, _ := proto.Marshal(&collectortrace.ExportTraceServiceResponse{})
			_, _ = writer.Write(response)
		}))
		defer server.Close()
		config := integrationConfig(ProtocolHTTPProtobuf, strings.TrimPrefix(server.URL, "http://"))
		exportTrace(t, config)
		if attempts.Load() != 3 {
			t.Fatalf("attempts = %d, want 3", attempts.Load())
		}
	})

	t.Run("slow endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			time.Sleep(50 * time.Millisecond)
		}))
		defer server.Close()
		config := integrationConfig(ProtocolHTTPProtobuf, strings.TrimPrefix(server.URL, "http://"))
		config.Retry.Enabled = false
		config.Timeout = time.Millisecond
		if err := tryExportTrace(config); err == nil {
			t.Fatal("trace export error = nil, want timeout")
		}
	})

	t.Run("malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("content-type", "application/x-protobuf")
			_, _ = writer.Write([]byte{0xff})
		}))
		defer server.Close()
		config := integrationConfig(ProtocolHTTPProtobuf, strings.TrimPrefix(server.URL, "http://"))
		config.Retry.Enabled = false
		if err := tryExportTrace(config); err == nil {
			t.Fatal("trace export error = nil, want malformed response error")
		}
	})
}

type traceCollector struct {
	collectortrace.UnimplementedTraceServiceServer
	failuresRemaining int64
	attempts          atomic.Int64
	spans             atomic.Int64
}

func (collector *traceCollector) Export(
	ctx context.Context,
	request *collectortrace.ExportTraceServiceRequest,
) (*collectortrace.ExportTraceServiceResponse, error) {
	collector.attempts.Add(1)
	if values := metadata.ValueFromIncomingContext(ctx, "authorization"); len(values) != 1 || values[0] != "test-token" {
		return nil, status.Error(codes.Unauthenticated, "missing test token")
	}
	if atomic.AddInt64(&collector.failuresRemaining, -1) >= 0 {
		return nil, status.Error(codes.Unavailable, "collector unavailable")
	}
	collector.spans.Add(countSpans(request))
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

type metricCollector struct {
	collectormetric.UnimplementedMetricsServiceServer
	metrics atomic.Int64
}

func (collector *metricCollector) Export(
	ctx context.Context,
	request *collectormetric.ExportMetricsServiceRequest,
) (*collectormetric.ExportMetricsServiceResponse, error) {
	if values := metadata.ValueFromIncomingContext(ctx, "authorization"); len(values) != 1 || values[0] != "test-token" {
		return nil, status.Error(codes.Unauthenticated, "missing test token")
	}
	collector.metrics.Add(countMetrics(request))
	return &collectormetric.ExportMetricsServiceResponse{}, nil
}

func integrationConfig(protocol Protocol, endpoint string) Config {
	return Config{
		Protocol:    protocol,
		Endpoint:    endpoint,
		Headers:     map[string]string{"authorization": "test-token"},
		Compression: CompressionGZIP,
		TLS:         TLSConfig{Insecure: true},
		Retry: RetryConfig{
			Enabled:         true,
			InitialInterval: time.Millisecond,
			MaxInterval:     2 * time.Millisecond,
			MaxElapsedTime:  time.Second,
		},
		Timeout: time.Second,
	}
}

func exportTrace(t *testing.T, config Config) {
	t.Helper()
	if err := tryExportTrace(config); err != nil {
		t.Fatalf("export trace: %v", err)
	}
}

func tryExportTrace(config Config) error {
	exporter, err := NewTraceExporter(context.Background(), config)
	if err != nil {
		return err
	}
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder), trace.WithSampler(trace.AlwaysSample()))
	_, span := provider.Tracer("integration").Start(context.Background(), "operation")
	span.End()
	exportErr := exporter.ExportSpans(context.Background(), recorder.Ended())
	return errors.Join(exportErr, provider.Shutdown(context.Background()), exporter.Shutdown(context.Background()))
}

func exportMetric(t *testing.T, config Config) {
	t.Helper()
	exporter, err := NewMetricExporter(context.Background(), config)
	if err != nil {
		t.Fatalf("NewMetricExporter() error = %v", err)
	}
	reader := metric.NewPeriodicReader(exporter, metric.WithInterval(time.Hour))
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	counter, err := provider.Meter("integration").Int64Counter("integration.operations")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}
	counter.Add(context.Background(), 1)
	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func readOTLPBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	var reader io.Reader = request.Body
	if request.Header.Get("content-encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(request.Body)
		if err != nil {
			t.Fatalf("gzip.NewReader() error = %v", err)
		}
		defer func() { _ = gzipReader.Close() }()
		reader = gzipReader
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return body
}

func countSpans(request *collectortrace.ExportTraceServiceRequest) int64 {
	var count int64
	for _, resourceSpans := range request.ResourceSpans {
		for _, scopeSpans := range resourceSpans.ScopeSpans {
			count += int64(len(scopeSpans.Spans))
		}
	}
	return count
}

func countMetrics(request *collectormetric.ExportMetricsServiceRequest) int64 {
	var count int64
	for _, resourceMetrics := range request.ResourceMetrics {
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
			count += int64(len(scopeMetrics.Metrics))
		}
	}
	return count
}
