package validate

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"sync"
	"testing"

	canonical "github.com/faustbrian/golib/pkg/json-schema"
	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestBoundDocumentRejectsWideValuesBeforeCopyingChildren(t *testing.T) {
	members := make([]jsonvalue.Member, 4096)
	for index := range members {
		members[index] = jsonvalue.Member{
			Name: "x-wide-" + strconv.Itoa(index), Value: jsonvalue.Null(),
		}
	}
	wide, _ := jsonvalue.Object(members)

	const repetitions = 16
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for range repetitions {
		if err := boundDocument(
			context.Background(), wide, 1, 2,
		); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("wide document error = %v", err)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions
	if allocated > 64<<10 {
		t.Fatalf("wide rejected document allocated %d bytes per operation", allocated)
	}
}

func TestDocumentChildrenFitExactBudgets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		children  int
		queued    int
		depth     int
		remaining int
		want      bool
	}{
		{name: "leaf at depth limit", depth: 3, remaining: 4, want: true},
		{name: "exact remaining nodes", children: 2, queued: 2, depth: 2, remaining: 4, want: true},
		{name: "node overflow", children: 3, queued: 2, depth: 2, remaining: 4},
		{name: "queue exhausted", children: 1, queued: 4, depth: 2, remaining: 4},
		{name: "queue overflow", children: 1, queued: 5, depth: 2, remaining: 4},
		{name: "exact depth", children: 1, depth: 3, remaining: 4},
	} {
		if got := documentChildrenFit(
			test.children, test.queued, test.depth, test.remaining, 3,
		); got != test.want {
			t.Fatalf("%s fit = %t, want %t", test.name, got, test.want)
		}
	}
}

type contextCanceledAfterFirstCheck struct {
	context.Context
	checks int
}

func (ctx *contextCanceledAfterFirstCheck) Err() error {
	ctx.checks++
	if ctx.checks == 1 {
		return nil
	}
	return context.Canceled
}

func TestValidatorCacheSharesConcurrentCompilation(t *testing.T) {
	t.Parallel()

	const workers = 8
	validator := NewValidator()
	schemas := make(chan *canonical.Schema, workers)
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			schema, err := validator.documentSchema(
				context.Background(), specversion.DialectOAS32,
			)
			if err != nil {
				errorsFound <- err
				return
			}
			schemas <- schema
		}()
	}
	wait.Wait()
	close(schemas)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	var first *canonical.Schema
	for schema := range schemas {
		if first == nil {
			first = schema
			continue
		}
		if schema != first {
			t.Fatal("concurrent callers received different compiled schemas")
		}
	}
}

func TestValidatorCacheObservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewValidator().documentSchema(
		ctx, specversion.DialectOAS31,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Validator.documentSchema() error = %v", err)
	}
}

func TestValidatorOwnsDocumentSchemaLimits(t *testing.T) {
	t.Parallel()

	limits := canonical.DefaultLimits()
	limits.MaxEvaluationOps = 7
	validator, err := NewValidatorWithDocumentSchemaLimits(limits)
	if err != nil || validator.documentLimits != limits {
		t.Fatalf("custom validator = %#v, %v", validator, err)
	}
	invalid := limits
	invalid.MaxEvaluationOps = 0
	if _, err := NewValidatorWithDocumentSchemaLimits(invalid); err == nil {
		t.Fatal("invalid document schema limits were accepted")
	}
	zero := &Validator{}
	if _, err := zero.documentSchema(
		context.Background(), specversion.DialectOAS31,
	); err != nil {
		t.Fatalf("zero-value validator schema error = %v", err)
	}
}

func TestValidatorCacheRetriesFailedCompilationAndWaitsCancellably(t *testing.T) {
	t.Parallel()

	const unknownDialect specversion.Dialect = "unknown"
	validator := NewValidator()
	for range 2 {
		if _, err := validator.documentSchema(
			context.Background(), unknownDialect,
		); err == nil {
			t.Fatal("unknown dialect compiled")
		}
	}
	validator.mutex.Lock()
	_, retained := validator.entries[unknownDialect]
	validator.mutex.Unlock()
	if retained {
		t.Fatal("failed compilation remained cached")
	}

	ready := make(chan struct{})
	validator = &Validator{entries: map[specversion.Dialect]*documentSchemaEntry{
		specversion.DialectOAS31: {ready: ready},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := validator.documentSchema(
		ctx, specversion.DialectOAS31,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting compilation error = %v", err)
	}

	want := errors.New("cached failure")
	close(ready)
	validator.entries[specversion.DialectOAS31].err = want
	if _, err := validator.documentSchema(
		context.Background(), specversion.DialectOAS31,
	); !errors.Is(err, want) {
		t.Fatalf("cached compilation error = %v", err)
	}
}

func TestValidatorCacheCancellationWhileWaiting(t *testing.T) {
	t.Parallel()

	ready := make(chan struct{})
	validator := &Validator{entries: map[specversion.Dialect]*documentSchemaEntry{
		specversion.DialectOAS31: {ready: ready},
	}}
	validator.mutex.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := validator.documentSchema(ctx, specversion.DialectOAS31)
		result <- err
	}()
	cancel()
	validator.mutex.Unlock()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting cancellation error = %v", err)
	}
}

func TestValidatorCacheSelectObservesCancellation(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := &contextCanceledAfterFirstCheck{Context: canceled}
	validator := &Validator{entries: map[specversion.Dialect]*documentSchemaEntry{
		specversion.DialectOAS31: {ready: make(chan struct{})},
	}}
	if _, err := validator.documentSchema(
		ctx, specversion.DialectOAS31,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("select cancellation error = %v", err)
	}
}

func TestDocumentSchemaResourcesAndLoader(t *testing.T) {
	t.Parallel()
	const unknownDialect specversion.Dialect = "unknown"

	for _, dialect := range []specversion.Dialect{
		specversion.DialectSwagger20,
		specversion.DialectOAS30,
		specversion.DialectOAS31,
		specversion.DialectOAS32,
	} {
		resource, schemaDialect, err := schemaResource(dialect)
		if err != nil || resource == "" || schemaDialect == "" {
			t.Fatalf("schemaResource(%q) = %q, %q, %v", dialect, resource, schemaDialect, err)
		}
	}
	if _, _, err := schemaResource(unknownDialect); err == nil {
		t.Fatal("unknown dialect resource was accepted")
	}

	loader := pinnedSchemaLoader{}
	for _, identifier := range []string{
		"http://json-schema.org/draft-04/schema",
		"http://json-schema.org/draft-04/schema#",
	} {
		raw, err := loader.Load(context.Background(), identifier)
		if err != nil || len(raw) == 0 {
			t.Fatalf("load(%q) = %d bytes, %v", identifier, len(raw), err)
		}
	}
	if _, err := loader.Load(
		context.Background(), "https://example.test/schema",
	); !errors.Is(err, canonical.ErrResourceUnavailable) {
		t.Fatalf("unknown resource error = %v", err)
	}
}

func TestDocumentValidationInternalFailureStates(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	validDocument := validationDocument{
		raw: testValidationValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{}
		}`),
		version: version,
	}
	var nilValidator *Validator
	if _, err := nilValidator.DocumentWithOptions(
		context.Background(), validDocument, DefaultOptions(),
	); err == nil {
		t.Fatal("nil validator was accepted")
	}

	unknownDocument := validationDocument{
		raw: validDocument.raw,
	}
	if _, err := NewValidator().DocumentWithOptions(
		context.Background(), unknownDocument, DefaultOptions(),
	); err == nil {
		t.Fatal("unknown document dialect was accepted")
	}

	invalidDocument := validationDocument{
		raw: jsonvalue.Value{}, version: version,
	}
	if _, err := NewValidator().DocumentWithOptions(
		context.Background(), invalidDocument, DefaultOptions(),
	); err == nil {
		t.Fatal("invalid immutable document was serialized")
	}
}

func TestDocumentValidationOwnsFallibleDependencies(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2",
			"info":{"title":"API","version":"1"},
			"paths":{},
			"components":{"schemas":{"Value":{"type":"string"}}}
		}`),
	}
	want := errors.New("document dependency failure")
	validator := NewValidator()
	validator.validateOutput = func(
		*canonical.Schema, context.Context, []byte, canonical.OutputFormat,
	) (canonical.OutputUnit, error) {
		return canonical.OutputUnit{}, want
	}
	if _, err := validator.DocumentWithOptions(
		context.Background(), document, DefaultOptions(),
	); !errors.Is(err, want) {
		t.Fatalf("document evaluator error = %v", err)
	}

	options := DefaultOptions()
	options.schemaMarshaller = func(jsonvalue.Value) ([]byte, error) {
		return nil, want
	}
	if _, err := NewValidator().DocumentWithOptions(
		context.Background(), document, options,
	); !errors.Is(err, want) {
		t.Fatalf("Schema Object validation error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	options = DefaultOptions()
	options.schemaMarshaller = func(value jsonvalue.Value) ([]byte, error) {
		cancel()
		return value.MarshalJSON()
	}
	options.schemaValidator = func(
		compiler *openapischema.Compiler,
		ctx context.Context,
		value jsonvalue.Value,
	) (openapischema.OutputUnit, error) {
		return openapischema.OutputUnit{Valid: true}, nil
	}
	if _, err := NewValidator().DocumentWithOptions(
		ctx, document, options,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("final cancellation error = %v", err)
	}
}

func TestDocumentValidationDefaultsAndDiagnosticBounds(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	valid := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2",
			"info":{"title":"API","version":"1"},
			"paths":{}
		}`),
	}
	if _, err := NewValidator().DocumentWithOptions(
		context.Background(), valid, Options{},
	); err == nil {
		t.Fatal("zero diagnostic limit was accepted")
	}
	options := Options{MaxDiagnostics: 1}
	if _, err := NewValidator().DocumentWithOptions(
		context.Background(), valid, options,
	); err != nil {
		t.Fatalf("zero-valued work bounds error = %v", err)
	}
	invalid := validationDocument{
		version: version,
		raw:     testValidationValue(t, `{"openapi":"3.1.2"}`),
	}
	report, err := NewValidator().DocumentWithOptions(
		context.Background(), invalid, options,
	)
	if err != nil || len(report.Diagnostics()) != 1 {
		t.Fatalf("bounded diagnostics = %#v, %v", report.Diagnostics(), err)
	}
}

func TestDocumentSchemaDependencyFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("document schema dependency failure")
	if _, err := newDocumentSchemaUsing(
		context.Background(), specversion.DialectOAS31,
		documentSchemaDependencies{read: func(string) ([]byte, error) {
			return nil, want
		}},
	); !errors.Is(err, want) {
		t.Fatalf("schema read error = %v", err)
	}
	if _, err := newDocumentSchemaUsing(
		context.Background(), specversion.DialectOAS31,
		documentSchemaDependencies{construct: func(
			...canonical.Option,
		) (*canonical.Compiler, error) {
			return nil, want
		}},
	); !errors.Is(err, want) {
		t.Fatalf("compiler construction error = %v", err)
	}
	if _, err := newDocumentSchemaUsing(
		context.Background(), specversion.DialectOAS31,
		documentSchemaDependencies{compile: func(
			*canonical.Compiler, context.Context, []byte,
		) (*canonical.Schema, error) {
			return nil, want
		}},
	); !errors.Is(err, want) {
		t.Fatalf("schema compilation error = %v", err)
	}
	if got := diagnostics(canonical.OutputUnit{
		Valid: false, KeywordLocation: "/type", Error: "invalid",
	}, "3.1.2"); len(got) != 1 || got[0].Code != "openapi.document.type" {
		t.Fatalf("direct output diagnostics = %#v", got)
	}
}

func TestBoundDocumentCoversCancellationAndPendingBudgets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := boundDocument(ctx, testValidationValue(t, `{}`), 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("bound cancellation error = %v", err)
	}
	if err := boundDocument(
		context.Background(), testValidationValue(t, `{}`), 0, 1,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero node budget error = %v", err)
	}
	duringWalk := &contextCanceledAfterFirstCheck{Context: context.Background()}
	if err := boundDocument(
		duringWalk, testValidationValue(t, `{}`), 1, 1,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("walk cancellation error = %v", err)
	}
	for _, value := range []jsonvalue.Value{
		testValidationValue(t, `[1,2]`),
		testValidationValue(t, `{"one":1,"two":2}`),
	} {
		if err := boundDocument(
			context.Background(), value, 2, 3,
		); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("pending node error = %v", err)
		}
	}
	if err := boundDocument(
		context.Background(), testValidationValue(t, `{"nested":{}}`), 3, 1,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("depth error = %v", err)
	}
	if err := boundDocument(
		context.Background(), testValidationValue(t, `[[]]`), 3, 1,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("array depth error = %v", err)
	}
	if err := boundDocument(
		context.Background(), testValidationValue(t, `[[[]]]`), 3, 2,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("nested array depth error = %v", err)
	}
	for _, value := range []jsonvalue.Value{
		testValidationValue(t, `[1,2]`),
		testValidationValue(t, `{"one":1,"two":2}`),
	} {
		if err := boundDocument(context.Background(), value, 3, 2); err != nil {
			t.Fatalf("value at exact document bounds = %v", err)
		}
	}
	if err := boundDocument(
		context.Background(), testValidationValue(t, `{}`), 1, 1,
	); err != nil {
		t.Fatalf("scalar at exact document bounds = %v", err)
	}
}

func TestDocumentDiagnosticHelpersUseExactMatches(t *testing.T) {
	t.Parallel()

	diagnostics := []Diagnostic{{
		Code: "openapi.value", InstanceLocation: "/value",
	}}
	if !hasDiagnosticCodeAt(diagnostics, "/value", "openapi.value") {
		t.Fatal("exact diagnostic was not found")
	}
	if hasDiagnosticCodeAt(diagnostics, "/other", "openapi.value") {
		t.Fatal("diagnostic matched a different pointer")
	}
	if hasDiagnosticCodeAt(diagnostics, "/value", "openapi.other") {
		t.Fatal("diagnostic matched a different code")
	}

	for location, want := range map[string]string{
		"":         "invalid",
		"type":     "invalid",
		"/":        "invalid",
		"/type":    "type",
		"/anyOf/0": "0",
	} {
		if got := keywordName(location); got != want {
			t.Errorf("keywordName(%q) = %q, want %q", location, got, want)
		}
	}
}

type validationDocument struct {
	raw     jsonvalue.Value
	version openapi.Version
}

func (document validationDocument) Raw() jsonvalue.Value {
	return document.raw
}

func (document validationDocument) SpecificationVersion() openapi.Version {
	return document.version
}
