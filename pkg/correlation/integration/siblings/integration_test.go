package siblings_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/correlation/http/requestidbridge"
	correlationlog "github.com/faustbrian/golib/pkg/correlation/log"
	correlationtelemetry "github.com/faustbrian/golib/pkg/correlation/telemetry"
	"github.com/faustbrian/golib/pkg/http-middleware/requestid"
	golog "github.com/faustbrian/golib/pkg/log"
	"github.com/faustbrian/golib/pkg/telemetry/testtelemetry"
	"go.opentelemetry.io/otel/trace"
)

func TestRequestIDMiddlewareBridgeUsesExplicitLookup(t *testing.T) {
	middleware, err := requestid.New(requestid.Policy{
		Generator: func() (string, error) { return "middleware-request", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	values := correlation.Values{CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{})}
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		_, adopted, err := requestidbridge.Adopt(request.Context(), values, func(ctx context.Context) (string, bool) {
			return requestid.FromContext(ctx, requestid.Request)
		}, requestidbridge.Options{Trusted: true})
		if err != nil || adopted.RequestID.String() != "middleware-request" {
			t.Fatalf("Adopt() = %#v, %v", adopted, err)
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestGoLogAndGoTelemetryUseStandardCorrelationAdapters(t *testing.T) {
	values := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("request", correlation.Policy{}),
	}
	attributes, err := correlationlog.Attrs(values, correlation.DisclosurePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	logger, err := golog.New(slog.NewTextHandler(io.Discard, nil), golog.WithAttrs(attributes...))
	if err != nil || logger == nil {
		t.Fatalf("log composition = %v, %v", logger, err)
	}

	harness := testtelemetry.New()
	defer func() { _ = harness.Shutdown(context.Background()) }()
	ctx, parent := harness.TracerProvider().Tracer("integration").Start(context.Background(), "parent")
	link, err := correlationtelemetry.Link(parent.SpanContext(), values, correlation.DisclosurePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	_, child := harness.TracerProvider().Tracer("integration").Start(ctx, "child", trace.WithLinks(link))
	child.End()
	parent.End()
	spans := harness.Spans()
	if len(spans) != 2 || len(spans[0].Links)+len(spans[1].Links) != 1 {
		t.Fatalf("recorded spans = %#v", spans)
	}
}
