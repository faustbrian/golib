package gotelemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	telemetry "github.com/faustbrian/golib/pkg/telemetry"
	"go.opentelemetry.io/otel/trace"
)

func TestInstrumentHTTPClientInjectsTraceAndPreservesClientPolicy(t *testing.T) {
	runtime := testRuntime(t)
	base := &recordingTransport{}
	redirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	original := &http.Client{Transport: base, Timeout: time.Second, CheckRedirect: redirect}
	client, err := InstrumentHTTPClient(runtime, original, "webhook.deliver")
	if err != nil {
		t.Fatalf("InstrumentHTTPClient() error = %v", err)
	}
	if client == original || original.Transport != base || client.Timeout != original.Timeout || client.CheckRedirect == nil {
		t.Fatal("InstrumentHTTPClient() did not clone and preserve client policy")
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled,
	})
	request, _ := http.NewRequestWithContext(trace.ContextWithSpanContext(context.Background(), spanContext), http.MethodPost, "https://example.com/hook", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = response.Body.Close()
	if !strings.HasPrefix(base.traceparent, "00-") {
		t.Fatalf("traceparent = %q", base.traceparent)
	}
}

func TestInstrumentHTTPClientRejectsUnsafeConfiguration(t *testing.T) {
	runtime := testRuntime(t)
	base := &http.Client{Transport: &recordingTransport{}}
	for name, test := range map[string]struct {
		runtime *telemetry.Runtime
		client  *http.Client
		op      string
	}{
		"runtime":   {client: base, op: "webhook.deliver"},
		"client":    {runtime: runtime, op: "webhook.deliver"},
		"transport": {runtime: runtime, client: &http.Client{}, op: "webhook.deliver"},
		"operation": {runtime: runtime, client: base, op: "bad operation"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := InstrumentHTTPClient(test.runtime, test.client, test.op); err == nil {
				t.Fatal("InstrumentHTTPClient() accepted unsafe configuration")
			}
		})
	}
}

func testRuntime(t *testing.T) *telemetry.Runtime {
	t.Helper()
	config := telemetry.DefaultConfig("webhook-test", "v1")
	config.Traces.Enabled = false
	config.Metrics.Enabled = false
	runtime, err := telemetry.Init(context.Background(), config)
	if err != nil {
		t.Fatalf("telemetry.Init() error = %v", err)
	}
	t.Cleanup(func() {
		if err := runtime.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	})
	return runtime
}

type recordingTransport struct {
	traceparent string
}

func (t *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("request required")
	}
	t.traceparent = request.Header.Get("Traceparent")
	return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
}
