package outbox_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
)

func TestEnvelopeBuilderBuildsDeterministicEnvelope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 9, 30, 0, 123456789, time.FixedZone("EEST", 3*60*60))
	builder, err := outbox.NewEnvelopeBuilder(
		outbox.WithClock(func() time.Time { return now }),
		outbox.WithIDGenerator(func() (string, error) { return "evt-123", nil }),
	)
	if err != nil {
		t.Fatalf("create builder: %v", err)
	}

	payload := []byte(`{"order_id":42}`)
	metadata := map[string]string{"trace_id": "trace-1", "content_type": "application/json"}
	envelope, err := builder.Build(outbox.NewEnvelopeParams{
		Topic:          "orders.created",
		Payload:        payload,
		PayloadVersion: 2,
		Metadata:       metadata,
		OrderingKey:    "customer-7",
		IdempotencyKey: "create-order-42",
	})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}

	wantTime := now.UTC().Truncate(time.Microsecond)
	if envelope.ID != "evt-123" || envelope.Topic != "orders.created" {
		t.Fatalf("unexpected identity: %#v", envelope)
	}
	if !envelope.CreatedAt.Equal(wantTime) || !envelope.AvailableAt.Equal(wantTime) {
		t.Fatalf("timestamps = %s/%s, want %s", envelope.CreatedAt, envelope.AvailableAt, wantTime)
	}
	if envelope.Attempts != 0 || envelope.PayloadVersion != 2 {
		t.Fatalf("attempts/version = %d/%d, want 0/2", envelope.Attempts, envelope.PayloadVersion)
	}

	payload[0] = 'X'
	metadata["trace_id"] = "changed"
	if bytes.Equal(envelope.Payload, payload) || envelope.Metadata["trace_id"] != "trace-1" {
		t.Fatal("envelope retained caller-owned payload or metadata")
	}

	first := envelope.CanonicalJSON()
	second := envelope.CanonicalJSON()
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical encoding changed: %q != %q", first, second)
	}
	wantJSON := `{"id":"evt-123","topic":"orders.created","payload":"eyJvcmRlcl9pZCI6NDJ9","payload_version":2,"metadata":{"content_type":"application/json","trace_id":"trace-1"},"ordering_key":"customer-7","idempotency_key":"create-order-42","attempts":0,"available_at":"2026-07-15T06:30:00.123456Z","created_at":"2026-07-15T06:30:00.123456Z"}`
	if string(first) != wantJSON {
		t.Fatalf("canonical JSON = %s, want %s", first, wantJSON)
	}
}

func TestEnvelopeBuilderOptionsRejectInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		option outbox.EnvelopeBuilderOption
		want   error
	}{
		"nil option":       {option: nil},
		"nil clock":        {option: outbox.WithClock(nil)},
		"nil ID generator": {option: outbox.WithIDGenerator(nil)},
		"invalid limits": {
			option: outbox.WithLimits(outbox.Limits{}),
			want:   outbox.ErrInvalidLimits,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := outbox.NewEnvelopeBuilder(test.option)
			if err == nil {
				t.Fatal("expected configuration error")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEnvelopeBuilderUsesExplicitAvailability(t *testing.T) {
	t.Parallel()

	availableAt := time.Date(2026, time.July, 16, 12, 0, 0, 999, time.FixedZone("EEST", 3*60*60))
	builder, err := outbox.NewEnvelopeBuilder(
		outbox.WithClock(func() time.Time { return time.Unix(1, 0) }),
		outbox.WithIDGenerator(func() (string, error) { return "evt-1", nil }),
	)
	if err != nil {
		t.Fatalf("create builder: %v", err)
	}

	envelope, err := builder.Build(outbox.NewEnvelopeParams{Topic: "topic", AvailableAt: availableAt})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if want := availableAt.UTC().Truncate(time.Microsecond); !envelope.AvailableAt.Equal(want) {
		t.Fatalf("available at = %s, want %s", envelope.AvailableAt, want)
	}
}

func TestEnvelopeBuilderRejectsEmptyGeneratedID(t *testing.T) {
	t.Parallel()

	builder, err := outbox.NewEnvelopeBuilder(outbox.WithIDGenerator(func() (string, error) { return "", nil }))
	if err != nil {
		t.Fatalf("create builder: %v", err)
	}

	_, err = builder.Build(outbox.NewEnvelopeParams{Topic: "topic"})
	if err == nil {
		t.Fatal("expected empty ID error")
	}
}

func TestEnvelopeValidateForInsertRejectsOversizedID(t *testing.T) {
	t.Parallel()

	limits := outbox.DefaultLimits()
	envelope := outbox.Envelope{ID: string(bytes.Repeat([]byte{'x'}, limits.MaxIDBytes+1)), Topic: "topic"}
	if err := envelope.ValidateForInsert(limits); !errors.Is(err, outbox.ErrIDTooLarge) {
		t.Fatalf("error = %v, want %v", err, outbox.ErrIDTooLarge)
	}
}

func TestEnvelopeValidateForInsertRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	valid := outbox.Limits{
		MaxIDBytes: 1, MaxTopicBytes: 1, MaxPayloadBytes: 1,
		MaxMetadataEntries: 1, MaxMetadataBytes: 1,
		MaxOrderingKeyBytes: 1, MaxIdempotencyKeyBytes: 1,
	}
	mutations := []func(*outbox.Limits){
		func(limits *outbox.Limits) { limits.MaxIDBytes = 0 },
		func(limits *outbox.Limits) { limits.MaxTopicBytes = 0 },
		func(limits *outbox.Limits) { limits.MaxPayloadBytes = 0 },
		func(limits *outbox.Limits) { limits.MaxMetadataEntries = 0 },
		func(limits *outbox.Limits) { limits.MaxMetadataBytes = 0 },
		func(limits *outbox.Limits) { limits.MaxOrderingKeyBytes = 0 },
		func(limits *outbox.Limits) { limits.MaxIdempotencyKeyBytes = 0 },
	}
	for index, mutate := range mutations {
		limits := valid
		mutate(&limits)
		if err := limits.Validate(); !errors.Is(err, outbox.ErrInvalidLimits) {
			t.Fatalf("invalid limit %d error = %v", index, err)
		}
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("minimum valid limits error = %v", err)
	}
}

func TestEnvelopeExactSizeAndTimestampBoundaries(t *testing.T) {
	t.Parallel()

	limits := outbox.Limits{
		MaxIDBytes: 2, MaxTopicBytes: 2, MaxPayloadBytes: 2,
		MaxMetadataEntries: 2, MaxMetadataBytes: 4,
		MaxOrderingKeyBytes: 2, MaxIdempotencyKeyBytes: 2,
	}
	valid := outbox.Envelope{
		ID: "12", Topic: "12", Payload: []byte("12"), PayloadVersion: 1,
		Metadata:    map[string]string{"a": "b", "c": "d"},
		OrderingKey: "12", IdempotencyKey: "12",
		AvailableAt: time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	}
	if err := valid.ValidateForInsert(limits); err != nil {
		t.Fatalf("exact boundary envelope error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*outbox.Envelope)
		want   error
	}{
		{name: "id", mutate: func(value *outbox.Envelope) { value.ID = "123" }, want: outbox.ErrIDTooLarge},
		{name: "topic", mutate: func(value *outbox.Envelope) { value.Topic = "123" }, want: outbox.ErrTopicTooLarge},
		{name: "payload", mutate: func(value *outbox.Envelope) { value.Payload = []byte("123") }, want: outbox.ErrPayloadTooLarge},
		{name: "metadata entries", mutate: func(value *outbox.Envelope) { value.Metadata["e"] = "" }, want: outbox.ErrMetadataEntriesTooLarge},
		{name: "metadata bytes", mutate: func(value *outbox.Envelope) { value.Metadata["c"] = "de" }, want: outbox.ErrMetadataTooLarge},
		{name: "ordering key", mutate: func(value *outbox.Envelope) { value.OrderingKey = "123" }, want: outbox.ErrOrderingKeyTooLarge},
		{name: "idempotency key", mutate: func(value *outbox.Envelope) { value.IdempotencyKey = "123" }, want: outbox.ErrIdempotencyKeyTooLarge},
		{name: "negative year", mutate: func(value *outbox.Envelope) { value.AvailableAt = time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC) }, want: outbox.ErrTimestampOutOfRange},
		{name: "extended year", mutate: func(value *outbox.Envelope) { value.CreatedAt = time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC) }, want: outbox.ErrTimestampOutOfRange},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			value.Metadata = map[string]string{"a": "b", "c": "d"}
			test.mutate(&value)
			if err := value.ValidateForInsert(limits); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEnvelopeBuilderRejectsTooManyMetadataEntries(t *testing.T) {
	t.Parallel()

	builder, err := outbox.NewEnvelopeBuilder(
		outbox.WithIDGenerator(func() (string, error) { return "evt-1", nil }),
	)
	if err != nil {
		t.Fatalf("create builder: %v", err)
	}
	metadata := make(map[string]string, 65)
	for index := 0; index < 65; index++ {
		metadata[fmt.Sprintf("k%d", index)] = ""
	}
	if _, err := builder.Build(outbox.NewEnvelopeParams{
		Topic: "topic", Metadata: metadata,
	}); !errors.Is(err, outbox.ErrMetadataEntriesTooLarge) {
		t.Fatalf("error = %v, want %v", err, outbox.ErrMetadataEntriesTooLarge)
	}
}

func TestEnvelopeValidateForInsertRejectsInvalidPersistenceState(t *testing.T) {
	t.Parallel()

	now := time.Now()
	valid := outbox.Envelope{
		ID: "evt-1", Topic: "topic", PayloadVersion: 1,
		AvailableAt: now, CreatedAt: now,
	}
	tests := map[string]struct {
		envelope outbox.Envelope
		want     error
	}{
		"payload version": {envelope: func() outbox.Envelope { value := valid; value.PayloadVersion = 0; return value }(), want: outbox.ErrPayloadVersionRequired},
		"attempts":        {envelope: func() outbox.Envelope { value := valid; value.Attempts = 1; return value }(), want: outbox.ErrAttemptsInvalid},
		"availability":    {envelope: func() outbox.Envelope { value := valid; value.AvailableAt = time.Time{}; return value }(), want: outbox.ErrAvailableAtRequired},
		"creation":        {envelope: func() outbox.Envelope { value := valid; value.CreatedAt = time.Time{}; return value }(), want: outbox.ErrCreatedAtRequired},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := test.envelope.ValidateForInsert(outbox.DefaultLimits()); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEnvelopeCanonicalJSONRemainsValidForExtendedYear(t *testing.T) {
	t.Parallel()

	envelope := outbox.Envelope{
		ID: "evt-1", Topic: "topic", PayloadVersion: 1,
		AvailableAt: time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if encoded := envelope.CanonicalJSON(); !json.Valid(encoded) {
		t.Fatalf("canonical JSON is invalid: %q", encoded)
	}
}

func TestEnvelopeValidateForInsertRejectsExtendedYear(t *testing.T) {
	t.Parallel()

	envelope := outbox.Envelope{
		ID: "evt-1", Topic: "topic", PayloadVersion: 1,
		AvailableAt: time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:   time.Now(),
	}
	if err := envelope.ValidateForInsert(outbox.DefaultLimits()); !errors.Is(err, outbox.ErrTimestampOutOfRange) {
		t.Fatalf("error = %v, want %v", err, outbox.ErrTimestampOutOfRange)
	}
}

func TestEnvelopeBuilderDefaultsPayloadVersion(t *testing.T) {
	t.Parallel()

	builder, err := outbox.NewEnvelopeBuilder(
		outbox.WithClock(func() time.Time { return time.Unix(1, 0) }),
		outbox.WithIDGenerator(func() (string, error) { return "evt-1", nil }),
	)
	if err != nil {
		t.Fatalf("create builder: %v", err)
	}

	envelope, err := builder.Build(outbox.NewEnvelopeParams{Topic: "orders.created", Payload: []byte("{}")})
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	if envelope.PayloadVersion != 1 {
		t.Fatalf("payload version = %d, want 1", envelope.PayloadVersion)
	}
}

func TestEnvelopeBuilderRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		limits outbox.Limits
		params outbox.NewEnvelopeParams
		want   error
	}{
		"empty topic": {
			params: outbox.NewEnvelopeParams{Payload: []byte("x")},
			want:   outbox.ErrTopicRequired,
		},
		"payload too large": {
			limits: outbox.Limits{MaxIDBytes: 10, MaxTopicBytes: 10, MaxPayloadBytes: 1, MaxMetadataEntries: 10, MaxMetadataBytes: 10, MaxOrderingKeyBytes: 10, MaxIdempotencyKeyBytes: 10},
			params: outbox.NewEnvelopeParams{Topic: "topic", Payload: []byte("xx")},
			want:   outbox.ErrPayloadTooLarge,
		},
		"metadata too large": {
			limits: outbox.Limits{MaxIDBytes: 10, MaxTopicBytes: 10, MaxPayloadBytes: 10, MaxMetadataEntries: 10, MaxMetadataBytes: 3, MaxOrderingKeyBytes: 10, MaxIdempotencyKeyBytes: 10},
			params: outbox.NewEnvelopeParams{Topic: "topic", Metadata: map[string]string{"ab": "cd"}},
			want:   outbox.ErrMetadataTooLarge,
		},
		"topic too large": {
			limits: outbox.Limits{MaxIDBytes: 10, MaxTopicBytes: 1, MaxPayloadBytes: 10, MaxMetadataEntries: 10, MaxMetadataBytes: 10, MaxOrderingKeyBytes: 10, MaxIdempotencyKeyBytes: 10},
			params: outbox.NewEnvelopeParams{Topic: "xx"},
			want:   outbox.ErrTopicTooLarge,
		},
		"ordering key too large": {
			limits: outbox.Limits{MaxIDBytes: 10, MaxTopicBytes: 10, MaxPayloadBytes: 10, MaxMetadataEntries: 10, MaxMetadataBytes: 10, MaxOrderingKeyBytes: 1, MaxIdempotencyKeyBytes: 10},
			params: outbox.NewEnvelopeParams{Topic: "topic", OrderingKey: "xx"},
			want:   outbox.ErrOrderingKeyTooLarge,
		},
		"idempotency key too large": {
			limits: outbox.Limits{MaxIDBytes: 10, MaxTopicBytes: 10, MaxPayloadBytes: 10, MaxMetadataEntries: 10, MaxMetadataBytes: 10, MaxOrderingKeyBytes: 10, MaxIdempotencyKeyBytes: 1},
			params: outbox.NewEnvelopeParams{Topic: "topic", IdempotencyKey: "xx"},
			want:   outbox.ErrIdempotencyKeyTooLarge,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			options := []outbox.EnvelopeBuilderOption{
				outbox.WithClock(func() time.Time { return time.Unix(1, 0) }),
				outbox.WithIDGenerator(func() (string, error) { return "evt-1", nil }),
			}
			if test.limits.MaxTopicBytes != 0 {
				options = append(options, outbox.WithLimits(test.limits))
			}
			builder, err := outbox.NewEnvelopeBuilder(options...)
			if err != nil {
				t.Fatalf("create builder: %v", err)
			}

			_, err = builder.Build(test.params)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEnvelopeBuilderPreservesGeneratorFailure(t *testing.T) {
	t.Parallel()

	generatorErr := errors.New("entropy unavailable")
	builder, err := outbox.NewEnvelopeBuilder(
		outbox.WithIDGenerator(func() (string, error) { return "", generatorErr }),
	)
	if err != nil {
		t.Fatalf("create builder: %v", err)
	}

	_, err = builder.Build(outbox.NewEnvelopeParams{Topic: "orders.created"})
	if !errors.Is(err, generatorErr) {
		t.Fatalf("error = %v, want wrapped %v", err, generatorErr)
	}
}
