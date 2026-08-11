package gotelemetry

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func FuzzAttributePolicyValidate(f *testing.F) {
	f.Add("client", "orders", "workers", uint8(1), false)
	f.Add("", ".", "\x00", uint8(0), false)
	f.Add(string([]byte{0xff}), "orders/created", " group", uint8(130), true)

	f.Fuzz(func(
		t *testing.T,
		clientID string,
		topic string,
		groupID string,
		count uint8,
		duplicate bool,
	) {
		clientID = clientID[:min(len(clientID), maxIdentityLength)]
		topic = topic[:min(len(topic), maxTopicLength)]
		groupID = groupID[:min(len(groupID), maxIdentityLength)]
		listSize := int(count) % 131
		clientIDs := fuzzIdentities(clientID, listSize, duplicate)
		topics := fuzzIdentities(topic, listSize, duplicate)
		consumerGroups := fuzzIdentities(groupID, listSize, duplicate)
		policy := AttributePolicy{
			AllowedClientIDs:      clientIDs,
			AllowedTopics:         topics,
			AllowedConsumerGroups: consumerGroups,
		}
		err := policy.Validate()
		if err != nil && !errors.Is(err, ErrInvalidAttributePolicy) {
			t.Fatalf("Validate() error = %v", err)
		}
	})
}

func FuzzHostileProviders(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint8(0), uint8(kafka.ObservationProducerShutdown), int64(time.Millisecond), false, []byte("client"))
	f.Add(uint8(3), uint8(1), uint8(2), uint8(kafka.ObservationProduceRecord), int64(0), false, []byte{0xff, 0})
	f.Add(uint8(5), uint8(9), uint8(9), uint8(255), int64(-1), true, []byte("oversized"))
	f.Fuzz(func(
		t *testing.T,
		mode uint8,
		nilPosition uint8,
		panicPosition uint8,
		kind uint8,
		duration int64,
		cancel bool,
		identity []byte,
	) {
		var instrumentation *Instrumentation
		var err error
		defer func() {
			if panicValue := recover(); panicValue != nil {
				t.Fatalf("provider mode %d panic = %v", mode, panicValue)
			}
		}()

		base := metricnoop.NewMeterProvider().Meter("fuzz")
		instrumentNames := []string{
			"messaging.client.operation.duration",
			"messaging.process.duration",
			"messaging.client.consumed.messages",
			"kafka.client.operations",
			"kafka.client.operation.duration",
			"kafka.client.request.size",
			"kafka.client.request.queue.duration",
			"kafka.client.throttle.duration",
		}
		nameAt := func(position uint8) string {
			if position%uint8(len(instrumentNames)+1) == 0 {
				return ""
			}

			return instrumentNames[int(position-1)%len(instrumentNames)]
		}
		meter := hardeningMeter{
			Meter:     base,
			nilName:   nameAt(nilPosition),
			panicName: nameAt(panicPosition),
		}
		switch mode % 6 {
		case 0:
			instrumentation, err = New(Config{Runtime: panickingRuntime{}})
		case 1:
			instrumentation, err = New(Config{Runtime: hardeningRuntime{
				tracerProvider: hardeningTracerProvider{},
				meterProvider:  metricnoop.NewMeterProvider(),
			}})
		case 2:
			instrumentation, err = New(Config{Runtime: hardeningRuntime{
				tracerProvider: tracenoop.NewTracerProvider(),
				meterProvider:  hardeningMeterProvider{},
			}})
		case 3:
			meter.panicOnAdd = true
			instrumentation, err = New(Config{Runtime: hardeningRuntime{
				tracerProvider: tracenoop.NewTracerProvider(),
				meterProvider: hardeningMeterProvider{
					MeterProvider: metricnoop.NewMeterProvider(),
					meter:         meter,
				},
			}})
		case 4:
			instrumentation, err = New(Config{Runtime: hardeningRuntime{
				tracerProvider: hardeningTracerProvider{
					TracerProvider: tracenoop.NewTracerProvider(),
				},
				meterProvider: hardeningMeterProvider{
					MeterProvider: metricnoop.NewMeterProvider(),
					meter:         meter,
				},
			}})
		case 5:
			instrumentation, err = New(Config{Runtime: hardeningRuntime{
				tracerProvider: tracenoop.NewTracerProvider(),
				meterProvider: hardeningMeterProvider{
					MeterProvider: metricnoop.NewMeterProvider(),
					meter:         meter,
				},
			}})
		}
		if err == nil {
			ctx := context.Background()
			if cancel {
				canceled, cancelContext := context.WithCancel(ctx)
				cancelContext()
				ctx = canceled
			}
			observation := validContractObservation(kafka.ObservationKind(kind), 0)
			observation.Duration = time.Duration(duration)
			observation.ClientID = string(identity)
			err = instrumentation.Observer()(ctx, observation)
		}
		if err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, ErrInvalidObservation) &&
			!errors.Is(err, ErrProviderPanic) &&
			!errors.Is(err, ErrInstrumentCreation) &&
			!errors.Is(err, ErrRuntimeRequired) {
			t.Fatalf(
				"provider mode %d nil %d panic %d error = %v",
				mode,
				nilPosition,
				panicPosition,
				err,
			)
		}
	})
}

func fuzzIdentities(base string, count int, duplicate bool) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = base + "-" + strconv.Itoa(index)
	}
	if duplicate && len(values) > 1 {
		values[len(values)-1] = values[0]
	}

	return values
}

func FuzzObserverValidation(f *testing.F) {
	f.Add(uint8(kafka.ObservationProduceRecord), int64(time.Millisecond), 1, 0, true, uint8(0), []byte("client|topic|group"))
	f.Add(uint8(255), int64(-1), -1, -1, false, uint8(255), []byte{0xff, 0, 1})

	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	f.Fuzz(func(
		t *testing.T,
		kind uint8,
		duration int64,
		recordCount int,
		processedCount int,
		succeeded bool,
		category uint8,
		metadata []byte,
	) {
		if len(metadata) > 1024 {
			t.Skip()
		}
		padded := make([]byte, 160)
		copy(padded, metadata)
		integer := func(offset int) int64 {
			return int64(binary.LittleEndian.Uint64(padded[offset : offset+8]))
		}
		observation := kafka.Observation{
			Kind:                   kafka.ObservationKind(kind),
			StartedAt:              time.Unix(integer(0), integer(8)),
			Duration:               time.Duration(duration),
			ClientID:               string(metadata[:min(len(metadata), 32)]),
			GroupID:                string(metadata[min(len(metadata), 32):min(len(metadata), 64)]),
			Topic:                  string(metadata[min(len(metadata), 64):]),
			BrokerID:               int32(integer(16)),
			BrokerKnown:            padded[120]&1 != 0,
			AuthenticationMethod:   kafka.AuthenticationMethod(padded[158]),
			APIKey:                 int16(integer(24)),
			APIKeyKnown:            padded[121]&1 != 0,
			RequestBytes:           integer(32),
			ResponseBytes:          integer(40),
			QueueDuration:          time.Duration(integer(48)),
			ThrottleDuration:       time.Duration(integer(56)),
			ThrottledAfterResponse: padded[122]&1 != 0,
			Partition:              int32(integer(104)),
			PartitionKnown:         padded[123]&1 != 0,
			Offset:                 integer(112),
			OffsetKnown:            padded[124]&1 != 0,
			Timestamp:              time.Unix(integer(128), integer(136)),
			RecordCount:            recordCount,
			PartitionCount:         int(int16(binary.LittleEndian.Uint16(padded[144:146]))),
			BrokerCount:            int(int16(binary.LittleEndian.Uint16(padded[146:148]))),
			TopicCount:             int(int16(binary.LittleEndian.Uint16(padded[148:150]))),
			GroupCount:             int(int16(binary.LittleEndian.Uint16(padded[150:152]))),
			GroupMemberCount:       int(int16(binary.LittleEndian.Uint16(padded[152:154]))),
			ProcessedCount:         processedCount,
			CommittedCount:         int(int16(binary.LittleEndian.Uint16(padded[154:156]))),
			RecordBytes:            integer(64),
			ReplayProcessed:        integer(72),
			ReplaySkipped:          integer(80),
			ReplayFailed:           integer(88),
			ReplayRemaining:        integer(96),
			DependencyHealthy:      padded[125]&1 != 0,
			Ready:                  padded[126]&1 != 0,
			ConsecutiveFailures:    int(int8(padded[156])),
			ConsecutiveSuccesses:   int(int8(padded[157])),
			Succeeded:              succeeded,
			Truncated:              padded[127]&1 != 0,
			Category:               kafka.ErrorCategory(category),
		}
		err := observer(context.Background(), observation)
		if err != nil && !errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("Observer() error = %v", err)
		}
	})
}

func FuzzTraceContextPropagation(f *testing.F) {
	f.Add("application", []byte("value"))
	f.Add("0", []byte{})
	f.Add("TraceParent", []byte(
		"00-0102030405060708090a0b0c0d0e0f10-1112131415161718-01",
	))
	f.Add("tracestate", []byte("vendor=value"))

	policy, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		f.Fatalf("NewTraceContextPropagation() error = %v", err)
	}
	ctx := trace.ContextWithSpanContext(
		context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SpanID:  trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24},
		}),
	)

	f.Fuzz(func(t *testing.T, key string, value []byte) {
		if len(key) > 256 || len(value) > 16<<10 {
			t.Skip()
		}
		record := kafka.ProducerRecord{
			Topic: "orders.v1",
			Key:   []byte("order-1"),
			Value: []byte("payload"),
			Headers: []kafka.Header{
				{Key: key, Value: value},
			},
		}
		original := retainProducerRecord(record)
		borrowed := kafka.ConsumedRecord{
			Topic: record.Topic, Key: record.Key, Value: record.Value,
			Headers: record.Headers,
		}
		borrowedOriginal := retainConsumedRecordShape(borrowed)
		_, _ = policy.Extract(context.Background(), borrowed)
		if !reflect.DeepEqual(borrowed, borrowedOriginal) {
			t.Fatal("Extract() mutated borrowed record")
		}

		injected, injectErr := policy.Inject(ctx, record)
		if !reflect.DeepEqual(record, original) {
			t.Fatal("Inject() mutated caller record")
		}
		if injectErr != nil {
			return
		}
		if validateErr := injected.Validate(kafka.DefaultMessageLimits()); validateErr != nil {
			t.Fatalf("Inject() returned invalid record: %v", validateErr)
		}
		if bytes.Equal(injected.Key, record.Key) && len(record.Key) != 0 &&
			&injected.Key[0] == &record.Key[0] {
			t.Fatal("Inject() result aliases caller key")
		}
		consumed := kafka.ConsumedRecord{
			Topic: injected.Topic, Key: injected.Key, Value: injected.Value,
			Headers: injected.Headers,
		}
		extracted, extractErr := policy.Extract(context.Background(), consumed)
		if extractErr != nil {
			t.Fatalf("Extract() error = %v", extractErr)
		}
		if !trace.SpanContextFromContext(extracted).IsRemote() {
			t.Fatal("Extract() did not mark the injected span context remote")
		}
	})
}

func retainConsumedRecordShape(record kafka.ConsumedRecord) kafka.ConsumedRecord {
	retained := record
	retained.Key = cloneBytesShape(record.Key)
	retained.Value = cloneBytesShape(record.Value)
	if record.Headers == nil {
		return retained
	}
	retained.Headers = make([]kafka.Header, len(record.Headers))
	for index, header := range record.Headers {
		retained.Headers[index] = kafka.Header{
			Key:   header.Key,
			Value: cloneBytesShape(header.Value),
		}
	}

	return retained
}

func cloneBytesShape(value []byte) []byte {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)

	return cloned
}
