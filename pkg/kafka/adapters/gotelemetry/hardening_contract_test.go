package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestAdapterProductionStartsNoGoroutines(t *testing.T) {
	t.Parallel()

	files := []string{"instrumentation.go", "propagation.go"}
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if _, startsGoroutine := node.(*ast.GoStmt); startsGoroutine {
				t.Errorf("%s starts an adapter-owned goroutine", name)
			}

			return true
		})
	}
}

func TestEveryNamedRootObservationHasAnAdapterMapping(t *testing.T) {
	t.Parallel()

	for value := 1; value <= 255; value++ {
		kind := kafka.ObservationKind(value)
		if kind.String() == "unknown" {
			continue
		}
		if operation := messagingOperation(kafka.Observation{Kind: kind}); operation.spanName == "" {
			t.Fatalf("named root observation %d/%s has no adapter mapping", value, kind)
		}
	}
}

func TestEveryObservationCaptureIsPayloadFreeAndAllowlistClosed(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{Runtime: testRuntime{
		tracerProvider: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(spans),
		),
		meterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observationType := reflect.TypeOf(kafka.Observation{})
	allowedFields := map[string]struct{}{
		"Kind": {}, "StartedAt": {}, "Duration": {}, "ClientID": {},
		"GroupID": {}, "BrokerID": {}, "BrokerKnown": {},
		"AuthenticationMethod": {}, "APIKey": {}, "APIKeyKnown": {},
		"RequestBytes": {}, "ResponseBytes": {}, "QueueDuration": {},
		"ThrottleDuration": {}, "ThrottledAfterResponse": {}, "Topic": {},
		"Partition": {}, "PartitionKnown": {}, "Offset": {},
		"OffsetKnown": {}, "Timestamp": {}, "RecordCount": {},
		"PartitionCount": {}, "BrokerCount": {}, "TopicCount": {},
		"GroupCount": {}, "GroupMemberCount": {}, "ProcessedCount": {},
		"CommittedCount": {}, "RecordBytes": {}, "ReplayProcessed": {},
		"ReplaySkipped": {}, "ReplayFailed": {}, "ReplayRemaining": {},
		"DependencyHealthy": {}, "Ready": {}, "ConsecutiveFailures": {},
		"ConsecutiveSuccesses": {}, "Succeeded": {}, "Truncated": {},
		"Category": {},
	}
	for index := 0; index < observationType.NumField(); index++ {
		field := observationType.Field(index).Name
		if _, allowed := allowedFields[field]; !allowed {
			t.Fatalf("observation field %q lacks a privacy review", field)
		}
	}
	if observationType.NumField() != len(allowedFields) {
		t.Fatalf("observation fields = %d, privacy inventory = %d", observationType.NumField(), len(allowedFields))
	}
	for value := 1; value <= 255; value++ {
		kind := kafka.ObservationKind(value)
		if kind.String() == "unknown" {
			continue
		}
		observation := validContractObservation(kind, value)
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("Observer(%s) error = %v", kind, err)
		}
	}
	identityObservations := []kafka.Observation{
		{
			Kind: kafka.ObservationProduceRecord, StartedAt: time.Unix(300, 0),
			Duration: time.Millisecond, RecordCount: 1, Succeeded: true,
			ClientID: "unallowlisted-client-sentinel",
			Topic:    "unallowlisted-topic-sentinel",
		},
		{
			Kind: kafka.ObservationConsumeRecord, StartedAt: time.Unix(301, 0),
			Duration: time.Millisecond, RecordCount: 1, Succeeded: true,
			ClientID: "unallowlisted-client-sentinel",
			Topic:    "unallowlisted-topic-sentinel",
			GroupID:  "unallowlisted-group-sentinel",
		},
	}
	for _, observation := range identityObservations {
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("identity Observer(%s) error = %v", observation.Kind, err)
		}
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	captured := fmt.Sprint(spans.Ended(), metrics)
	for _, forbidden := range []string{
		"unallowlisted-client-sentinel",
		"unallowlisted-topic-sentinel",
		"unallowlisted-group-sentinel",
		"record-key-sentinel",
		"record-value-sentinel",
		"record-header-sentinel",
		"credential-sentinel",
		"broker-endpoint-sentinel",
		"application-error-sentinel",
		"panic-value-sentinel",
	} {
		if strings.Contains(captured, forbidden) {
			t.Fatalf("captured telemetry contains forbidden value %q", forbidden)
		}
	}
}

func TestObserverContainsHostileProviderPanicsAndClosesStartedSpan(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	base := metricnoop.NewMeterProvider().Meter("test")
	instrumentation, err := New(Config{Runtime: testRuntime{
		tracerProvider: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(spans),
		),
		meterProvider: hardeningMeterProvider{
			MeterProvider: metricnoop.NewMeterProvider(),
			meter:         hardeningMeter{Meter: base, panicOnAdd: true},
		},
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		err = instrumentation.Observer()(
			context.Background(),
			validLifecycleObservation(),
		)
	}()
	if panicValue != nil {
		t.Fatalf("Observer() panic = %v", panicValue)
	}
	if err == nil || err.Error() != "kafka/gotelemetry: provider panicked" {
		t.Fatalf("Observer() error = %v", err)
	}
	if ended := spans.Ended(); len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
}

func TestObserverContainsHostileSpanEndPanic(t *testing.T) {
	t.Parallel()

	provider := sdktrace.NewTracerProvider()
	instrumentation, err := New(Config{Runtime: testRuntime{
		tracerProvider: hardeningTracerProvider{TracerProvider: provider},
		meterProvider:  metricnoop.NewMeterProvider(),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var panicValue any
	func() {
		defer func() { panicValue = recover() }()
		err = instrumentation.Observer()(
			context.Background(),
			validLifecycleObservation(),
		)
	}()
	if panicValue != nil {
		t.Fatalf("Observer() panic = %v", panicValue)
	}
	if !errors.Is(err, ErrProviderPanic) {
		t.Fatalf("Observer() error = %v", err)
	}
}

func TestNewContainsHostileProviderPanicsAndRejectsNilInstruments(t *testing.T) {
	t.Parallel()

	var panicValue any
	var instrumentation *Instrumentation
	var err error
	func() {
		defer func() { panicValue = recover() }()
		instrumentation, err = New(Config{Runtime: panickingRuntime{}})
	}()
	if panicValue != nil {
		t.Fatalf("New() panic = %v", panicValue)
	}
	if instrumentation != nil || err == nil ||
		err.Error() != "kafka/gotelemetry: provider panicked" {
		t.Fatalf("New() = %#v, %v", instrumentation, err)
	}

	base := metricnoop.NewMeterProvider().Meter("test")
	for _, instrumentName := range []string{
		"messaging.client.operation.duration",
		"messaging.process.duration",
		"messaging.client.consumed.messages",
		"kafka.client.operations",
		"kafka.client.operation.duration",
		"kafka.client.request.size",
		"kafka.client.request.queue.duration",
		"kafka.client.throttle.duration",
	} {
		instrumentation, err = New(Config{Runtime: failingRuntime{
			meter: failingMeterProvider{
				MeterProvider: metricnoop.NewMeterProvider(),
				meter: hardeningMeter{
					Meter: base, nilName: instrumentName,
				},
			},
		}})
		if instrumentation != nil || !errors.Is(err, ErrInstrumentCreation) {
			t.Fatalf("New(nil %s) = %#v, %v", instrumentName, instrumentation, err)
		}
	}
	instrumentation, err = New(Config{Runtime: failingRuntime{
		meter: failingMeterProvider{
			MeterProvider: metricnoop.NewMeterProvider(),
			meter: hardeningMeter{
				Meter:     base,
				nilName:   "messaging.client.operation.duration",
				panicName: "messaging.process.duration",
			},
		},
	}})
	if instrumentation != nil || !errors.Is(err, ErrInstrumentCreation) {
		t.Fatalf("New() after first nil instrument = %#v, %v", instrumentation, err)
	}

	for name, runtime := range map[string]Runtime{
		"nil meter": hardeningRuntime{
			tracerProvider: sdktrace.NewTracerProvider(),
			meterProvider:  hardeningMeterProvider{},
		},
		"nil tracer": hardeningRuntime{
			tracerProvider: hardeningTracerProvider{},
			meterProvider:  metricnoop.NewMeterProvider(),
		},
	} {
		instrumentation, err = New(Config{Runtime: runtime})
		if instrumentation != nil || err == nil {
			t.Fatalf("New(%s) = %#v, %v", name, instrumentation, err)
		}
	}
}

func TestConfigValidateContainsHostileProviderPanic(t *testing.T) {
	t.Parallel()

	var panicValue any
	var err error
	func() {
		defer func() { panicValue = recover() }()
		err = (Config{Runtime: panickingRuntime{}}).Validate()
	}()
	if panicValue != nil {
		t.Fatalf("Config.Validate() panic = %v", panicValue)
	}
	if !errors.Is(err, ErrProviderPanic) {
		t.Fatalf("Config.Validate() error = %v", err)
	}
}

func TestOversizedObservedIdentitiesAreRejectedBeforeLookup(t *testing.T) {
	t.Parallel()

	maximumClientID := strings.Repeat("c", maxIdentityLength)
	maximumTopic := strings.Repeat("t", maxTopicLength)
	maximumGroup := strings.Repeat("g", maxIdentityLength)
	policy := normalizedAttributePolicy{
		clientIDs:      map[string]struct{}{maximumClientID: {}},
		topics:         map[string]struct{}{maximumTopic: {}},
		consumerGroups: map[string]struct{}{maximumGroup: {}},
	}
	if policy.allowsClientID(strings.Repeat("c", maxIdentityLength+1)) ||
		policy.allowsTopic(strings.Repeat("t", maxTopicLength+1)) ||
		policy.allowsConsumerGroup(strings.Repeat("g", maxIdentityLength+1)) {
		t.Fatal("oversized observed identity passed allowlist")
	}
	if !policy.allowsClientID(maximumClientID) ||
		!policy.allowsTopic(maximumTopic) ||
		!policy.allowsConsumerGroup(maximumGroup) {
		t.Fatal("maximum-length observed identity missed exact allowlist")
	}
}

func TestStandardMetricsUseOnlyPinnedConventionDimensions(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(),
			meterProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
			),
		},
		Attributes: AttributePolicy{
			AllowedClientIDs:      []string{"client"},
			AllowedTopics:         []string{"orders"},
			AllowedConsumerGroups: []string{"workers"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observations := []kafka.Observation{
		{
			Kind: kafka.ObservationProduceRecord, StartedAt: time.Unix(1, 0),
			Duration: time.Millisecond, ClientID: "client", Topic: "orders",
			RecordCount: 1, Succeeded: true,
		},
		{
			Kind: kafka.ObservationConsumePoll, StartedAt: time.Unix(2, 0),
			Duration: time.Millisecond, ClientID: "client", Topic: "orders",
			GroupID: "workers", RecordCount: 1, Succeeded: true,
		},
		{
			Kind: kafka.ObservationConsumeRecord, StartedAt: time.Unix(3, 0),
			Duration: time.Millisecond, ClientID: "client", Topic: "orders",
			GroupID: "workers", RecordCount: 1, Succeeded: false,
			Category: kafka.ErrorPermanent,
		},
	}
	for _, observation := range observations {
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("Observer(%s) error = %v", observation.Kind, err)
		}
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	wantKeys := map[string]map[string]struct{}{
		"messaging.client.operation.duration": attributeKeys(
			"messaging.system", "messaging.operation.name",
			"messaging.operation.type", "messaging.consumer.group.name",
			"messaging.destination.name",
		),
		"messaging.client.consumed.messages": attributeKeys(
			"messaging.system", "messaging.operation.name",
			"messaging.consumer.group.name", "messaging.destination.name",
		),
		"messaging.process.duration": attributeKeys(
			"messaging.system", "messaging.operation.name",
			"messaging.consumer.group.name", "messaging.destination.name", "error.type",
		),
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, current := range scope.Metrics {
			for _, set := range metricAttributeSets(t, current.Data) {
				for _, pair := range set.ToSlice() {
					switch string(pair.Key) {
					case "messaging.client.id", "kafka.client.id",
						"kafka.topic", "kafka.consumer.group":
						t.Fatalf("%s identity attribute %q", current.Name, pair.Key)
					}
				}
				allowed, standard := wantKeys[current.Name]
				if !standard {
					continue
				}
				for _, pair := range set.ToSlice() {
					if _, ok := allowed[string(pair.Key)]; !ok {
						t.Fatalf("%s unexpected attribute %q", current.Name, pair.Key)
					}
				}
			}
		}
	}
}

func TestCompletionObservationsDoNotClaimUnprovedMessagingOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		observation      kafka.Observation
		wantSpanName     string
		wantConsumedData bool
	}{
		{
			name: "produce completion",
			observation: kafka.Observation{
				Kind: kafka.ObservationProduceRecord, StartedAt: time.Unix(10, 0),
				Duration: time.Millisecond, RecordCount: 1, Succeeded: true,
			},
			wantSpanName: "kafka producer.publish_completion",
		},
		{
			name: "poll cycle",
			observation: kafka.Observation{
				Kind: kafka.ObservationConsumePoll, StartedAt: time.Unix(11, 0),
				Duration: time.Millisecond, RecordCount: 1, Succeeded: true,
			},
			wantSpanName:     "kafka consumer.poll_cycle",
			wantConsumedData: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			spans := tracetest.NewSpanRecorder()
			reader := sdkmetric.NewManualReader()
			instrumentation, err := New(Config{Runtime: testRuntime{
				tracerProvider: sdktrace.NewTracerProvider(
					sdktrace.WithSpanProcessor(spans),
				),
				meterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
			}})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := instrumentation.Observer()(context.Background(), test.observation); err != nil {
				t.Fatalf("Observer() error = %v", err)
			}
			ended := spans.Ended()
			if len(ended) != 1 || ended[0].Name() != test.wantSpanName ||
				ended[0].SpanKind() != trace.SpanKindInternal {
				t.Fatalf("span = %#v", ended)
			}
			if _, exists := attributeMap(ended[0].Attributes())["messaging.system"]; exists {
				t.Fatal("completion span claims a standard messaging operation")
			}

			var metrics metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &metrics); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			assertMetricAbsent(t, metrics, "messaging.client.operation.duration")
			assertMetricAbsent(t, metrics, "messaging.client.sent.messages")
			if test.wantConsumedData {
				assertIntCounter(
					t,
					metrics,
					"messaging.client.consumed.messages",
					1,
					map[string]any{
						"messaging.system":         "kafka",
						"messaging.operation.name": "poll",
					},
				)
			}
		})
	}
}

func attributeKeys(keys ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		result[key] = struct{}{}
	}

	return result
}

func metricAttributeSets(t *testing.T, data metricdata.Aggregation) []attribute.Set {
	t.Helper()

	switch current := data.(type) {
	case metricdata.Sum[int64]:
		sets := make([]attribute.Set, 0, len(current.DataPoints))
		for _, point := range current.DataPoints {
			sets = append(sets, point.Attributes)
		}

		return sets
	case metricdata.Histogram[float64]:
		sets := make([]attribute.Set, 0, len(current.DataPoints))
		for _, point := range current.DataPoints {
			sets = append(sets, point.Attributes)
		}

		return sets
	default:
		t.Fatalf("unexpected aggregation %T", data)

		return nil
	}
}

type panickingRuntime struct{}

func (panickingRuntime) TracerProvider() trace.TracerProvider {
	panic("credential=provider-panic")
}

func (panickingRuntime) MeterProvider() metric.MeterProvider {
	return metricnoop.NewMeterProvider()
}

type hardeningRuntime struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

func (runtime hardeningRuntime) TracerProvider() trace.TracerProvider {
	return runtime.tracerProvider
}

func (runtime hardeningRuntime) MeterProvider() metric.MeterProvider {
	return runtime.meterProvider
}

type hardeningMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider hardeningMeterProvider) Meter(
	string,
	...metric.MeterOption,
) metric.Meter {
	return provider.meter
}

type hardeningMeter struct {
	metric.Meter
	panicOnAdd bool
	nilName    string
	panicName  string
}

func (meter hardeningMeter) Int64Counter(
	name string,
	options ...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if meter.panicName == name {
		panic("unexpected later instrument construction")
	}
	if meter.nilName == name {
		return nil, nil
	}
	if name != "kafka.client.operations" {
		return meter.Meter.Int64Counter(name, options...)
	}
	counter, err := meter.Meter.Int64Counter(name, options...)
	if err != nil {
		return nil, err
	}

	return hardeningCounter{Int64Counter: counter, panicOnAdd: meter.panicOnAdd}, nil
}

func (meter hardeningMeter) Int64Histogram(
	name string,
	options ...metric.Int64HistogramOption,
) (metric.Int64Histogram, error) {
	if meter.panicName == name {
		panic("unexpected later instrument construction")
	}
	if meter.nilName == name {
		return nil, nil
	}

	return meter.Meter.Int64Histogram(name, options...)
}

func (meter hardeningMeter) Float64Histogram(
	name string,
	options ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if meter.panicName == name {
		panic("unexpected later instrument construction")
	}
	if meter.nilName == name {
		return nil, nil
	}

	return meter.Meter.Float64Histogram(name, options...)
}

type hardeningCounter struct {
	metric.Int64Counter
	panicOnAdd bool
}

type hardeningTracerProvider struct {
	trace.TracerProvider
}

func (provider hardeningTracerProvider) Tracer(
	name string,
	options ...trace.TracerOption,
) trace.Tracer {
	if provider.TracerProvider == nil {
		return nil
	}

	return hardeningTracer{
		Tracer: provider.TracerProvider.Tracer(name, options...),
	}
}

type hardeningTracer struct {
	trace.Tracer
}

func (tracer hardeningTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	spanCtx, span := tracer.Tracer.Start(ctx, name, options...)

	return spanCtx, hardeningSpan{Span: span}
}

type hardeningSpan struct {
	trace.Span
}

func (hardeningSpan) End(...trace.SpanEndOption) {
	panic("panic-value-sentinel")
}

func (counter hardeningCounter) Add(
	ctx context.Context,
	value int64,
	options ...metric.AddOption,
) {
	if counter.panicOnAdd {
		panic("credential=metric-panic")
	}
	counter.Int64Counter.Add(ctx, value, options...)
}
