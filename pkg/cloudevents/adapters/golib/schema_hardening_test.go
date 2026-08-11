package golib_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	registryjsonschema "github.com/faustbrian/golib/pkg/schema-registry/formats/jsonschema"
)

type schemaHardeningResolverFunc func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error)

func (fn schemaHardeningResolverFunc) Resolve(
	ctx context.Context,
	lookup schemaregistry.Lookup,
) (schemaregistry.ResolveResult, error) {
	return fn(ctx, lookup)
}

type schemaHardeningClock struct{ now time.Time }

func (clock schemaHardeningClock) Now() time.Time { return clock.now }

type schemaHardeningCanonicalizer struct{}

func (schemaHardeningCanonicalizer) Canonicalize(
	_ context.Context,
	definition schemaregistry.Definition,
) ([]byte, error) {
	return append([]byte(nil), definition.Content...), nil
}

func TestRegistryJSONSchemaValidatorRejectsUnmappedEventSchemaWithoutResolution(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			calls.Add(1)
			return schemaregistry.ResolveResult{}, errors.New("must not resolve")
		},
	))
	validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache: cache,
		SchemaLookups: map[string]schemaregistry.Lookup{
			"https://schemas.example/orders": schemaHardeningLookup(),
		},
		Adapter:            newSchemaHardeningAdapter(t),
		AvailabilityPolicy: schemaregistry.FailClosed,
		Timeout:            time.Second,
	})

	err := validator.Validate(
		context.Background(),
		"http://169.254.169.254/latest/meta-data/iam/security-credentials/",
		"application/json",
		[]byte(`{"id":1}`),
	)
	if !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("unmapped dataschema error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("resolver calls = %d, want 0", calls.Load())
	}
}

func TestRegistryJSONSchemaValidatorRequiresBoundedExplicitConfiguration(t *testing.T) {
	t.Parallel()

	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaregistry.ResolveResult{}, errors.New("must not resolve")
		},
	))
	adapter := newSchemaHardeningAdapter(t)
	lookup := schemaHardeningLookup()
	valid := golib.RegistryJSONSchemaConfig{
		Cache:              cache,
		SchemaLookups:      map[string]schemaregistry.Lookup{"schema": lookup},
		Adapter:            adapter,
		AvailabilityPolicy: schemaregistry.FailClosed,
		Timeout:            time.Second,
	}

	tests := map[string]golib.RegistryJSONSchemaConfig{
		"nil cache":             {SchemaLookups: valid.SchemaLookups, Adapter: adapter, AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second},
		"empty mappings":        {Cache: cache, Adapter: adapter, AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second},
		"empty mapping URI":     {Cache: cache, SchemaLookups: map[string]schemaregistry.Lookup{"": lookup}, Adapter: adapter, AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second},
		"empty mapping lookup":  {Cache: cache, SchemaLookups: map[string]schemaregistry.Lookup{"schema": {}}, Adapter: adapter, AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second},
		"nil adapter":           {Cache: cache, SchemaLookups: valid.SchemaLookups, AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second},
		"implicit availability": {Cache: cache, SchemaLookups: valid.SchemaLookups, Adapter: adapter, Timeout: time.Second},
		"unknown availability":  {Cache: cache, SchemaLookups: valid.SchemaLookups, Adapter: adapter, AvailabilityPolicy: schemaregistry.AvailabilityPolicy("unknown"), Timeout: time.Second},
		"zero timeout":          {Cache: cache, SchemaLookups: valid.SchemaLookups, Adapter: adapter, AvailabilityPolicy: schemaregistry.FailClosed},
		"negative timeout":      {Cache: cache, SchemaLookups: valid.SchemaLookups, Adapter: adapter, AvailabilityPolicy: schemaregistry.FailClosed, Timeout: -time.Second},
	}
	for name, validator := range tests {
		validator := validator
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := golib.NewRegistryJSONSchemaValidator(validator); !errors.Is(err, golib.ErrSchemaMapping) {
				t.Fatalf("configuration error = %v", err)
			}
		})
	}
	if err := (golib.RegistryJSONSchemaValidator{}).Validate(context.Background(), "schema", "application/json", []byte(`{}`)); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("zero-value validator error = %v", err)
	}
}

func TestRegistryJSONSchemaValidatorUsesCacheIdentityValidation(t *testing.T) {
	t.Parallel()

	adapter := newSchemaHardeningAdapter(t)
	schema := compileSchemaHardeningSchema(t, adapter)
	lookup := schemaHardeningLookup()
	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaregistry.ResolveResult{
				Schema:    schema,
				ID:        schemaregistry.ProviderID{Provider: "other", Value: "wrong"},
				Lifecycle: schemaregistry.LifecycleAvailable,
			}, nil
		},
	))
	validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache: cache, SchemaLookups: map[string]schemaregistry.Lookup{"schema": lookup}, Adapter: adapter,
		AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
	})

	if err := validator.Validate(context.Background(), "schema", "application/json", []byte(`{"id":1}`)); !errors.Is(err, schemaregistry.ErrResolutionMismatch) {
		t.Fatalf("identity mismatch error = %v", err)
	}
}

func TestRegistryJSONSchemaValidatorRejectsResolvedNonJSONSchema(t *testing.T) {
	t.Parallel()

	resolvedSchema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format: schemaregistry.FormatAvro, Content: []byte(`{"type":"record","name":"Event","fields":[]}`),
	}, schemaHardeningCanonicalizer{})
	if err != nil {
		t.Fatal(err)
	}
	lookup := schemaHardeningLookup()
	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaHardeningResult(resolvedSchema), nil
		},
	))
	validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache: cache, SchemaLookups: map[string]schemaregistry.Lookup{"schema": lookup},
		Adapter: newSchemaHardeningAdapter(t), AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
	})

	if err := validator.Validate(context.Background(), "schema", "application/json", []byte(`{"id":1}`)); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("non-JSON resolved schema error = %v", err)
	}
}

func TestRegistryJSONSchemaValidatorBoundsResolutionAndPreservesParentCancellation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		parent  func() (context.Context, context.CancelFunc)
		timeout time.Duration
		want    error
	}{
		{name: "validator timeout", parent: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}, timeout: 20 * time.Millisecond, want: context.DeadlineExceeded},
		{name: "parent cancellation", parent: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}, timeout: time.Second, want: context.Canceled},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			deadlineSeen := make(chan time.Duration, 1)
			cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
				func(ctx context.Context, _ schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
					deadline, ok := ctx.Deadline()
					if !ok {
						deadlineSeen <- 0
					} else {
						deadlineSeen <- time.Until(deadline)
					}
					<-ctx.Done()
					return schemaregistry.ResolveResult{}, ctx.Err()
				},
			))
			validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
				Cache: cache, SchemaLookups: map[string]schemaregistry.Lookup{"schema": schemaHardeningLookup()},
				Adapter: newSchemaHardeningAdapter(t), AvailabilityPolicy: schemaregistry.FailClosed, Timeout: test.timeout,
			})
			ctx, cancel := test.parent()
			defer cancel()
			err := validator.Validate(ctx, "schema", "application/json", []byte(`{"id":1}`))
			if !errors.Is(err, test.want) {
				t.Fatalf("validation error = %v, want %v", err, test.want)
			}
			if test.want == context.DeadlineExceeded {
				seen := <-deadlineSeen
				if seen <= 0 || seen > test.timeout {
					t.Fatalf("derived deadline remaining = %s, want within (0, %s]", seen, test.timeout)
				}
			}
		})
	}
}

func TestRegistryJSONSchemaValidatorPropagatesCancellationIntoPayloadValidation(t *testing.T) {
	t.Parallel()

	adapter := newSchemaHardeningAdapter(t)
	schema := compileSchemaHardeningSchema(t, adapter)
	lookup := schemaHardeningLookup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			cancel()
			return schemaHardeningResult(schema), nil
		},
	))
	validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache: cache, SchemaLookups: map[string]schemaregistry.Lookup{"schema": lookup}, Adapter: adapter,
		AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
	})

	if err := validator.Validate(ctx, "schema", "application/json", []byte(`{"id":1}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-resolution validation error = %v, want cancellation", err)
	}
}

func TestRegistryJSONSchemaValidatorOwnsImmutableSchemaLookupSnapshot(t *testing.T) {
	t.Parallel()

	adapter := newSchemaHardeningAdapter(t)
	schema := compileSchemaHardeningSchema(t, adapter)
	wantLookup := schemaHardeningLookup()
	source := map[string]schemaregistry.Lookup{"schema": wantLookup}
	var calls atomic.Int32
	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(_ context.Context, lookup schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			calls.Add(1)
			if lookup != wantLookup {
				return schemaregistry.ResolveResult{}, errors.New("unexpected lookup")
			}
			return schemaHardeningResult(schema), nil
		},
	))
	validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache: cache, SchemaLookups: source, Adapter: adapter,
		AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
	})

	source["schema"] = schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "attacker", Value: "target"})
	source["http://169.254.169.254/latest/meta-data"] = wantLookup
	if err := validator.Validate(context.Background(), "schema", "application/json", []byte(`{"id":1}`)); err != nil {
		t.Fatalf("validation from owned snapshot: %v", err)
	}
	if err := validator.Validate(context.Background(), "http://169.254.169.254/latest/meta-data", "application/json", []byte(`{"id":1}`)); !errors.Is(err, golib.ErrSchemaMapping) {
		t.Fatalf("post-construction URI injection error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("resolver calls = %d, want 1", calls.Load())
	}
}

func TestRegistryJSONSchemaValidatorConcurrentValidationDoesNotShareSourceMap(t *testing.T) {
	t.Parallel()

	adapter := newSchemaHardeningAdapter(t)
	schema := compileSchemaHardeningSchema(t, adapter)
	lookup := schemaHardeningLookup()
	source := map[string]schemaregistry.Lookup{"schema": lookup}
	cache := newSchemaHardeningCache(t, schemaHardeningResolverFunc(
		func(context.Context, schemaregistry.Lookup) (schemaregistry.ResolveResult, error) {
			return schemaHardeningResult(schema), nil
		},
	))
	validator := newSchemaHardeningValidator(t, golib.RegistryJSONSchemaConfig{
		Cache: cache, SchemaLookups: source, Adapter: adapter,
		AvailabilityPolicy: schemaregistry.FailClosed, Timeout: time.Second,
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := range 100 {
			source["mutable"] = schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Value: string(rune(index + 1))})
		}
	}()
	results := make(chan error, 100)
	for range 100 {
		go func() {
			results <- validator.Validate(context.Background(), "schema", "application/json", []byte(`{"id":1}`))
		}()
	}
	for range 100 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent validation: %v", err)
		}
	}
	<-done
}

func newSchemaHardeningCache(t *testing.T, resolver schemaregistry.Resolver) *schemaregistry.ResolveCache {
	t.Helper()
	cache, err := schemaregistry.NewResolveCache(resolver, schemaregistry.ResolveCacheConfig{
		MaxEntries: 2, MaxConcurrent: 1, FreshFor: time.Minute, StaleFor: time.Minute,
		NegativeFor: time.Minute, Clock: schemaHardeningClock{now: time.Unix(1, 0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func newSchemaHardeningAdapter(t *testing.T) *registryjsonschema.Adapter {
	t.Helper()
	adapter, err := registryjsonschema.New(registryjsonschema.Config{
		MaxSchemaBytes: 1024, MaxTotalSchemaBytes: 2048, MaxPayloadBytes: 1024, MaxResources: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func compileSchemaHardeningSchema(t *testing.T, adapter *registryjsonschema.Adapter) schemaregistry.Schema {
	t.Helper()
	schema, err := schemaregistry.Compile(context.Background(), schemaregistry.Definition{
		Format:  schemaregistry.FormatJSONSchema,
		Content: []byte(`{"type":"object","required":["id"]}`),
	}, adapter)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func schemaHardeningLookup() schemaregistry.Lookup {
	return schemaregistry.ByProviderID(schemaregistry.ProviderID{Provider: "test", Scope: "local", Value: "orders-v1"})
}

func schemaHardeningResult(schema schemaregistry.Schema) schemaregistry.ResolveResult {
	return schemaregistry.ResolveResult{
		Schema: schema, ID: schemaHardeningLookup().ProviderID(), Lifecycle: schemaregistry.LifecycleAvailable,
	}
}

func newSchemaHardeningValidator(
	t *testing.T,
	config golib.RegistryJSONSchemaConfig,
) golib.RegistryJSONSchemaValidator {
	t.Helper()
	validator, err := golib.NewRegistryJSONSchemaValidator(config)
	if err != nil {
		t.Fatal(err)
	}
	return validator
}
