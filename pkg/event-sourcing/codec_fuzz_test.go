package eventsourcing_test

import (
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func FuzzJSONCodecDecode(f *testing.F) {
	f.Add([]byte(`{"id":9007199254740993,"registered_at":"2026-07-25T06:17:42Z","labels":{}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"id":`))
	f.Add([]byte(`{"unknown":"field"}`))

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[customerRegistered]("customer.registered", 2),
		eventsourcing.WithJSONStrictDecoding(),
	)
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) == 0 || len(payload) > eventsourcing.MaxPayloadBytes {
			return
		}
		encoded, buildErr := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
			Name:        "customer.registered",
			Version:     2,
			ContentType: eventsourcing.JSONContentType,
			Payload:     payload,
		})
		if buildErr != nil {
			t.Fatal(buildErr)
		}

		_, decodeErr := codec.Decode(encoded)
		if decodeErr != nil && !errors.Is(decodeErr, eventsourcing.ErrMalformedEvent) {
			t.Fatalf("Decode() error = %v, want ErrMalformedEvent", decodeErr)
		}
	})
}
