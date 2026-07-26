package observe_test

import (
	"context"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/diff"
	"github.com/faustbrian/golib/pkg/openrpc/discovery"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/observe"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

type recorder struct{ events []observe.Event }

func (recorder *recorder) Observe(_ context.Context, event observe.Event) {
	recorder.events = append(recorder.events, event)
}

func TestOperationsReportOnlyBoundedMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	observer := &recorder{}
	input := []byte(`{"openrpc":"1.4.1","info":{"title":"Observed","version":"1"},"methods":[]}`)
	parsed, err := observe.Parse(ctx, input, parse.DefaultOptions(), observer)
	if err != nil {
		t.Fatal(err)
	}
	document := parsed.Document()
	if report := observe.Validate(ctx, document, validate.DefaultOptions(), observer); !report.Valid() {
		t.Fatal(report.Diagnostics())
	}

	root, err := jsonvalue.Parse([]byte(`{"value":true}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observe.Resolve(
		ctx, resolver, root, "https://example.com/openrpc.json",
		[]string{"#/value"}, observer,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := observe.Bundle(
		ctx, resolver, root, "https://example.com/openrpc.json", observer,
	); err != nil {
		t.Fatal(err)
	}
	if report := observe.Diff(
		ctx, document, document, diff.DefaultOptions(), observer,
	); report.Err() != nil {
		t.Fatal(report.Err())
	}
	service, err := discovery.NewService(discovery.Static(document), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observe.Discover(ctx, service, observer); err != nil {
		t.Fatal(err)
	}

	want := []observe.Phase{
		observe.PhaseParse, observe.PhaseValidate, observe.PhaseResolve,
		observe.PhaseBundle, observe.PhaseDiff, observe.PhaseDiscover,
	}
	if len(observer.events) != len(want) {
		t.Fatalf("events = %#v", observer.events)
	}
	for index, event := range observer.events {
		if event.Phase != want[index] || event.Outcome != observe.OutcomeSuccess ||
			event.Duration < 0 || event.DiagnosticCount != 0 {
			t.Fatalf("event %d = %#v", index, event)
		}
	}
	if observer.events[2].ReferenceCount != 1 || observer.events[3].ReferenceCount != 0 {
		t.Fatalf("reference events = %#v", observer.events[2:4])
	}
}

type panickingObserver struct{}

func (panickingObserver) Observe(context.Context, observe.Event) { panic("observer detail") }

func TestOperationsContainObserversAndClassifyFailures(t *testing.T) {
	t.Parallel()

	input := []byte(`{"openrpc":"1.4.1","info":{"title":"Observed","version":"1"},"methods":[]}`)
	if _, err := observe.Parse(context.Background(), input, parse.DefaultOptions(), panickingObserver{}); err != nil {
		t.Fatal(err)
	}
	called := false
	if _, err := observe.Parse(
		context.Background(), input, parse.DefaultOptions(),
		observe.ObserverFunc(func(context.Context, observe.Event) { called = true }),
	); err != nil || !called {
		t.Fatalf("ObserverFunc called = %t, error = %v", called, err)
	}
	observer := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := observe.Parse(ctx, input, parse.DefaultOptions(), observer); !errors.Is(err, context.Canceled) {
		t.Fatalf("parse error = %v", err)
	}
	if len(observer.events) != 1 || observer.events[0].Outcome != observe.OutcomeCanceled {
		t.Fatalf("events = %#v", observer.events)
	}
	var invalidContext context.Context
	if _, err := observe.Parse(invalidContext, input, parse.DefaultOptions(), observer); !errors.Is(err, observe.ErrInvalidContext) {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestValidateReportsInvalidDiagnosticsWithoutPayloads(t *testing.T) {
	t.Parallel()

	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Invalid", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	method, err := openrpc.NewMethod(openrpc.MethodInput{Name: "duplicate", Params: []openrpc.ContentDescriptorOrReference{}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version, Info: &info,
		Methods: []openrpc.MethodOrReference{
			openrpc.MethodValue(method), openrpc.MethodValue(method),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	observer := &recorder{}
	report := observe.Validate(
		context.Background(), document, validate.DefaultOptions(), observer,
	)
	if report.Valid() || len(observer.events) != 1 ||
		observer.events[0].Outcome != observe.OutcomeInvalid ||
		observer.events[0].DiagnosticCount != len(report.Diagnostics()) {
		t.Fatalf("report = %#v, events = %#v", report.Diagnostics(), observer.events)
	}
}

func TestOperationsReportInvalidFailureAndCanceledOutcomes(t *testing.T) {
	t.Parallel()

	observer := &recorder{}
	invalid := []byte(`{"openrpc":"1.4.1"}`)
	if _, err := observe.Parse(context.Background(), invalid, parse.DefaultOptions(), observer); err == nil {
		t.Fatal("invalid parse succeeded")
	}
	if observer.events[len(observer.events)-1].Outcome != observe.OutcomeInvalid {
		t.Fatalf("parse event = %#v", observer.events[len(observer.events)-1])
	}

	document := openrpc.Document{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if report := observe.Validate(ctx, document, validate.DefaultOptions(), observer); report.Valid() {
		t.Fatal("canceled validation reported valid")
	}
	if observer.events[len(observer.events)-1].Outcome != observe.OutcomeCanceled {
		t.Fatalf("validate event = %#v", observer.events[len(observer.events)-1])
	}
	beforeEvents := len(observer.events)
	var invalidContext context.Context
	observe.Validate(invalidContext, document, validate.DefaultOptions(), observer)
	if len(observer.events) != beforeEvents {
		t.Fatal("nil-context validation emitted an event")
	}

	root, err := jsonvalue.Parse([]byte(`{"value":true}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observe.Resolve(context.Background(), resolver, root,
		"https://example.com/openrpc.json", []string{"#/missing"}, observer); err == nil {
		t.Fatal("missing reference resolved")
	}
	if observer.events[len(observer.events)-1].Outcome != observe.OutcomeFailure {
		t.Fatalf("resolve event = %#v", observer.events[len(observer.events)-1])
	}
	if _, err := observe.Resolve(invalidContext, resolver, root,
		"https://example.com/openrpc.json", nil, observer); !errors.Is(err, observe.ErrInvalidContext) {
		t.Fatalf("nil-context resolve error = %v", err)
	}

	if _, err := observe.Bundle(context.Background(), nil, root,
		"https://example.com/openrpc.json", observer); err == nil {
		t.Fatal("invalid bundle succeeded")
	}
	if observer.events[len(observer.events)-1].Outcome != observe.OutcomeFailure {
		t.Fatalf("bundle event = %#v", observer.events[len(observer.events)-1])
	}
	if _, err := observe.Bundle(invalidContext, resolver, root,
		"https://example.com/openrpc.json", observer); !errors.Is(err, observe.ErrInvalidContext) {
		t.Fatalf("nil-context bundle error = %v", err)
	}

	failing, err := discovery.NewService(discovery.ProviderFunc(
		func(context.Context) (openrpc.Document, error) {
			return openrpc.Document{}, context.DeadlineExceeded
		},
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	discoveryContext, cancelDiscovery := context.WithCancel(context.Background())
	cancelDiscovery()
	if _, err := observe.Discover(discoveryContext, failing, observer); !errors.Is(err, context.Canceled) {
		t.Fatalf("discovery error = %v", err)
	}
	if observer.events[len(observer.events)-1].Outcome != observe.OutcomeCanceled {
		t.Fatalf("discover event = %#v", observer.events[len(observer.events)-1])
	}
	if _, err := observe.Discover(invalidContext, failing, observer); !errors.Is(err, observe.ErrInvalidContext) {
		t.Fatalf("nil-context discovery error = %v", err)
	}

	if report := observe.Diff(invalidContext, document, document, diff.DefaultOptions(), observer); report.Err() == nil {
		t.Fatal("nil-context diff succeeded")
	}
	observe.Parse(context.Background(), invalid, parse.DefaultOptions(), nil) //nolint:errcheck
}
