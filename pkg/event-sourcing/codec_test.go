package eventsourcing_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestJSONCodecRoundTripsRegisteredEventDeterministically(t *testing.T) {
	t.Parallel()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2),
	)
	if err != nil {
		t.Fatal(err)
	}
	registeredAt := time.Date(2026, time.July, 25, 9, 17, 42, 123456789, time.FixedZone("EEST", 3*60*60))
	decoded, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "customer.registered",
		Version: 2,
		Value: customerRegistered{
			ID:           9_007_199_254_740_993,
			RegisteredAt: registeredAt,
			Labels:       map[string]string{"z": "last", "a": "first"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := codec.Encode(decoded)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	second, err := codec.Encode(decoded)
	if err != nil {
		t.Fatalf("second Encode() error = %v", err)
	}
	if first.Name().String() != "customer.registered" ||
		first.Version() != 2 ||
		first.ContentType() != eventsourcing.JSONContentType {
		t.Fatalf(
			"encoded identity = (%s, %d, %s)",
			first.Name(),
			first.Version(),
			first.ContentType(),
		)
	}
	if string(first.Payload()) != string(second.Payload()) {
		t.Fatalf("encoding is not deterministic: %q != %q", first.Payload(), second.Payload())
	}

	roundTrip, err := codec.Decode(first)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	actual, ok := roundTrip.Value().(customerRegistered)
	if !ok {
		t.Fatalf("Decode() value type = %T, want customerRegistered", roundTrip.Value())
	}
	if roundTrip.Name().String() != "customer.registered" ||
		roundTrip.Version() != 2 ||
		actual.ID != 9_007_199_254_740_993 ||
		!actual.RegisteredAt.Equal(registeredAt) ||
		actual.Labels["a"] != "first" ||
		actual.Labels["z"] != "last" {
		t.Fatalf("round trip = %#v", roundTrip)
	}
}

func TestJSONCodecSupportsReaderFirstRollingSchemaDeployment(t *testing.T) {
	t.Parallel()

	type registeredV1 struct {
		ID uint64 `json:"id"`
	}
	type registeredV2 struct {
		ID      uint64 `json:"id"`
		Segment string `json:"segment"`
	}

	oldWriter, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[registeredV1]("customer.registered", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	newReader, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[registeredV1]("customer.registered", 1),
		eventsourcing.JSONEvent[registeredV2]("customer.registered", 2),
	)
	if err != nil {
		t.Fatal(err)
	}
	oldEvent, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "customer.registered",
			Version: 1,
			Value:   registeredV1{ID: 42},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	storedByOldWriter, err := oldWriter.Encode(oldEvent)
	if err != nil {
		t.Fatal(err)
	}
	decodedByNewReader, err := newReader.Decode(storedByOldWriter)
	if err != nil {
		t.Fatal(err)
	}
	if decodedByNewReader.Version() != 1 ||
		decodedByNewReader.Value() != (registeredV1{ID: 42}) {
		t.Fatalf("new reader decoded old event = %#v", decodedByNewReader)
	}

	newEvent, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "customer.registered",
			Version: 2,
			Value:   registeredV2{ID: 42, Segment: "business"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	storedByNewWriter, err := newReader.Encode(newEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldWriter.Decode(storedByNewWriter); !errors.Is(
		err,
		eventsourcing.ErrIncompatibleVersion,
	) {
		t.Fatalf(
			"old reader Decode(new schema) error = %v, want ErrIncompatibleVersion",
			err,
		)
	}
}

func TestJSONCodecUsesExplicitAliasesWithoutChangingStoredHistory(t *testing.T) {
	t.Parallel()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2),
		eventsourcing.JSONAlias(
			"customer.signed-up",
			2,
			"customer.registered",
			2,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "customer.signed-up",
		Version:     2,
		ContentType: eventsourcing.JSONContentType,
		Payload: []byte(
			`{"id":9007199254740993,"registered_at":"2026-07-25T06:17:42.123456789Z","labels":{}}`,
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := codec.Decode(stored)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Name().String() != "customer.registered" || decoded.Version() != 2 {
		t.Fatalf("decoded identity = (%s, %d)", decoded.Name(), decoded.Version())
	}
	if stored.Name().String() != "customer.signed-up" {
		t.Fatalf("stored event identity changed to %s", stored.Name())
	}
	aliasValue := lifecycleEvent(t, "customer.signed-up", 2)
	if _, err := codec.Encode(aliasValue); !errors.Is(err, eventsourcing.ErrUnknownEvent) {
		t.Fatalf("Encode(alias) error = %v, want ErrUnknownEvent", err)
	}
}

func TestJSONCodecRejectsUnknownTypeAndRegistrationConflicts(t *testing.T) {
	t.Parallel()

	duplicate := eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2)
	if _, err := eventsourcing.NewJSONCodec(duplicate, duplicate); !errors.Is(
		err,
		eventsourcing.ErrDuplicateRegistration,
	) {
		t.Fatalf("NewJSONCodec() error = %v, want ErrDuplicateRegistration", err)
	}

	codec, err := eventsourcing.NewJSONCodec(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	unknown := lifecycleEvent(t, "customer.deleted", 1)
	if _, err := codec.Encode(unknown); !errors.Is(err, eventsourcing.ErrUnknownEvent) {
		t.Fatalf("Encode() error = %v, want ErrUnknownEvent", err)
	}
	incompatible := lifecycleEvent(t, "customer.registered", 3)
	if _, err := codec.Encode(incompatible); !errors.Is(
		err,
		eventsourcing.ErrIncompatibleVersion,
	) {
		t.Fatalf("Encode() error = %v, want ErrIncompatibleVersion", err)
	} else if errors.Is(err, eventsourcing.ErrUnknownEvent) {
		t.Fatalf("Encode() error = %v, must not report ErrUnknownEvent", err)
	}
	wrongType, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "customer.registered",
		Version: 2,
		Value:   accountOpened{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Encode(wrongType); !errors.Is(err, eventsourcing.ErrEventTypeMismatch) {
		t.Fatalf("Encode() error = %v, want ErrEventTypeMismatch", err)
	}
}

func TestJSONCodecStrictDecodingRejectsUnknownAndTrailingData(t *testing.T) {
	t.Parallel()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2),
		eventsourcing.WithJSONStrictDecoding(),
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string][]byte{
		"unknown field": []byte(
			`{"id":9,"registered_at":"2026-07-25T06:17:42Z","labels":{},"secret":"no"}`,
		),
		"trailing value": []byte(
			`{"id":9,"registered_at":"2026-07-25T06:17:42Z","labels":{}} {}`,
		),
		"invalid JSON":       []byte(`{"id":`),
		"missing object key": []byte(`{`),
		"invalid object key": []byte(`{"unterminated`),
		"unterminated object": []byte(
			`{"id":9`,
		),
		"duplicate key": []byte(
			`{"id":9,"id":10,"registered_at":"2026-07-25T06:17:42Z","labels":{}}`,
		),
		"nested duplicate key": []byte(
			`{"id":9,"registered_at":"2026-07-25T06:17:42Z","labels":{"a":"x","a":"y"}}`,
		),
		"invalid UTF-8": {
			'{', '"', 'i', 'd', '"', ':', '"', 0xff, '"', '}',
		},
		"excessive nesting": []byte(
			strings.Repeat("[", eventsourcing.MaxJSONDepth+1) +
				"0" +
				strings.Repeat("]", eventsourcing.MaxJSONDepth+1),
		),
		"excessive container entries": []byte(
			"[" +
				strings.Repeat("0,", eventsourcing.MaxJSONContainerEntries) +
				"0]",
		),
		"excessive object entries": oversizedJSONObject(),
	}
	for name, payload := range tests {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			encoded, buildErr := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
				Name:        "customer.registered",
				Version:     2,
				ContentType: eventsourcing.JSONContentType,
				Payload:     payload,
			})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if _, decodeErr := codec.Decode(encoded); !errors.Is(
				decodeErr,
				eventsourcing.ErrMalformedEvent,
			) {
				t.Fatalf("Decode() error = %v, want ErrMalformedEvent", decodeErr)
			}
		})
	}
}

func TestJSONCodecRejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()

	event := eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2)
	tests := map[string][]eventsourcing.JSONCodecOption{
		"no declarations": nil,
		"nil option": {
			nil,
		},
		"zero registration": {
			eventsourcing.JSONRegistration{},
		},
		"invalid event name": {
			eventsourcing.JSONEvent[customerRegistered]("CustomerRegistered", 2),
		},
		"zero event version": {
			eventsourcing.JSONEvent[customerRegistered]("customer.registered", 0),
		},
		"alias duplicates event": {
			event,
			eventsourcing.JSONAlias(
				"customer.registered",
				2,
				"customer.renamed",
				2,
			),
		},
		"alias targets itself": {
			event,
			eventsourcing.JSONAlias(
				"customer.renamed",
				2,
				"customer.renamed",
				2,
			),
		},
		"alias changes schema version": {
			event,
			eventsourcing.JSONAlias(
				"customer.signed-up",
				1,
				"customer.registered",
				2,
			),
		},
		"invalid alias name": {
			event,
			eventsourcing.JSONAlias(
				"CustomerSignedUp",
				2,
				"customer.registered",
				2,
			),
		},
		"invalid alias target": {
			event,
			eventsourcing.JSONAlias(
				"customer.signed-up",
				2,
				"CustomerRegistered",
				2,
			),
		},
		"alias target missing": {
			event,
			eventsourcing.JSONAlias(
				"customer.signed-up",
				2,
				"customer.missing",
				2,
			),
		},
		"strict option duplicated": {
			event,
			eventsourcing.WithJSONStrictDecoding(),
			eventsourcing.WithJSONStrictDecoding(),
		},
	}
	for name, declarations := range tests {
		declarations := declarations
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := eventsourcing.NewJSONCodec(declarations...); err == nil {
				t.Fatal("NewJSONCodec() error = nil, want declaration error")
			}
		})
	}
}

func TestJSONCodecDecodeRejectsUnknownIdentityAndContentType(t *testing.T) {
	t.Parallel()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2),
		eventsourcing.JSONAlias(
			"customer.signed-up",
			2,
			"customer.registered",
			2,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		name        string
		version     eventsourcing.SchemaVersion
		contentType string
		want        error
	}{
		"unknown name": {
			name:        "customer.deleted",
			version:     2,
			contentType: eventsourcing.JSONContentType,
			want:        eventsourcing.ErrUnknownEvent,
		},
		"unsupported canonical schema version": {
			name:        "customer.registered",
			version:     3,
			contentType: eventsourcing.JSONContentType,
			want:        eventsourcing.ErrIncompatibleVersion,
		},
		"unsupported alias schema version": {
			name:        "customer.signed-up",
			version:     3,
			contentType: eventsourcing.JSONContentType,
			want:        eventsourcing.ErrIncompatibleVersion,
		},
		"wrong content type": {
			name:        "customer.registered",
			version:     2,
			contentType: "application/msgpack",
			want:        eventsourcing.ErrUnsupportedContentType,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			encoded, buildErr := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
				Name:        test.name,
				Version:     test.version,
				ContentType: test.contentType,
				Payload:     []byte("{}"),
			})
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if _, decodeErr := codec.Decode(encoded); !errors.Is(decodeErr, test.want) {
				t.Fatalf("Decode() error = %v, want %v", decodeErr, test.want)
			} else if errors.Is(test.want, eventsourcing.ErrIncompatibleVersion) &&
				errors.Is(decodeErr, eventsourcing.ErrUnknownEvent) {
				t.Fatalf("Decode() error = %v, must not report ErrUnknownEvent", decodeErr)
			}
		})
	}
	if _, err := codec.Decode(eventsourcing.EncodedEvent{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Decode(zero) error = %v, want ErrInvalidArgument", err)
	}
	if _, err := codec.Encode(eventsourcing.DecodedEvent{}); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Encode(zero) error = %v, want ErrInvalidArgument", err)
	}
}

func TestJSONCodecRedactsMalformedPayloadAndEncodingValue(t *testing.T) {
	t.Parallel()

	type unsupportedEvent struct {
		Callback chan string `json:"callback"`
	}
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2),
		eventsourcing.JSONEvent[unsupportedEvent]("customer.unsupported", 1),
	)
	if err != nil {
		t.Fatal(err)
	}

	secret := "credential-do-not-disclose"
	stored, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "customer.registered",
		Version:     2,
		ContentType: eventsourcing.JSONContentType,
		Payload:     []byte(`{"id":"` + secret + `"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Decode(stored); !errors.Is(err, eventsourcing.ErrMalformedEvent) {
		t.Fatalf("Decode() error = %v, want ErrMalformedEvent", err)
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("Decode() disclosed payload: %q", err)
	}

	decoded, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "customer.unsupported",
		Version: 1,
		Value:   unsupportedEvent{Callback: make(chan string)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Encode(decoded); !errors.Is(err, eventsourcing.ErrMalformedEvent) {
		t.Fatalf("Encode() error = %v, want ErrMalformedEvent", err)
	}
}

func TestJSONCodecRejectsEncodedPayloadOverEnvelopeLimit(t *testing.T) {
	t.Parallel()

	type largeEvent struct {
		Value string `json:"value"`
	}
	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[largeEvent]("payload.large", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := eventsourcing.NewDecodedEvent(eventsourcing.DecodedEventInput{
		Name:    "payload.large",
		Version: 1,
		Value: largeEvent{
			Value: strings.Repeat("x", eventsourcing.MaxPayloadBytes),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := codec.Encode(decoded); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Encode() error = %v, want ErrInvalidArgument", err)
	}
}

type customerRegistered struct {
	ID           int64             `json:"id"`
	RegisteredAt time.Time         `json:"registered_at"`
	Labels       map[string]string `json:"labels"`
}

func oversizedJSONObject() []byte {
	var builder strings.Builder
	builder.WriteByte('{')
	for index := 0; index <= eventsourcing.MaxJSONContainerEntries; index++ {
		if index != 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`"key-`)
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString(`":0`)
	}
	builder.WriteByte('}')

	return []byte(builder.String())
}
