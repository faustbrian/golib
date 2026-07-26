package eventtest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/eventtest"
)

func TestExpectedEventMatchesIdentityAndPayloadWithoutLeakingValues(t *testing.T) {
	t.Parallel()

	event := decodedEvent(t, "account.opened", accountOpened{Owner: "Ada"})
	expected := eventtest.ExpectedEvent{
		Name:    "account.opened",
		Version: 1,
		Value: func(value any) error {
			opened, ok := value.(accountOpened)
			if !ok || opened.Owner != "Ada" {
				return errors.New("owner mismatch")
			}

			return nil
		},
	}
	if err := eventtest.MatchEvent(event, expected); err != nil {
		t.Fatal(err)
	}

	for name, testCase := range map[string]struct {
		event    eventsourcing.DecodedEvent
		expected eventtest.ExpectedEvent
	}{
		"zero event": {
			expected: expected,
		},
		"invalid expected name": {
			event: event,
			expected: eventtest.ExpectedEvent{
				Version: 1,
			},
		},
		"name": {
			event: event,
			expected: eventtest.ExpectedEvent{
				Name:    "account.closed",
				Version: 1,
			},
		},
		"version": {
			event: event,
			expected: eventtest.ExpectedEvent{
				Name:    "account.opened",
				Version: 2,
			},
		},
		"payload": {
			event: event,
			expected: eventtest.ExpectedEvent{
				Name:    "account.opened",
				Version: 1,
				Value: func(any) error {
					return errors.New("secret owner Ada")
				},
			},
		},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.MatchEvent(testCase.event, testCase.expected)
			if err == nil {
				t.Fatal("MatchEvent() unexpectedly succeeded")
			}
			if name == "payload" &&
				contains(err.Error(), "secret owner Ada") {
				t.Fatalf("MatchEvent() leaked matcher diagnostic: %v", err)
			}
		})
	}
}

func TestMetadataMatchIsExactAndRedacted(t *testing.T) {
	t.Parallel()

	actual := map[string]string{"tenant": "private-a", "source": "test"}
	expected := map[string]string{"source": "test", "tenant": "private-a"}
	if err := eventtest.MatchMetadata(actual, expected); err != nil {
		t.Fatal(err)
	}

	for name, value := range map[string]map[string]string{
		"missing":     {"source": "test", "tenant": "private-a", "trace": "secret"},
		"extra":       {"source": "test"},
		"replacement": {"source": "test", "trace": "secret"},
		"value":       {"source": "other", "tenant": "private-a"},
	} {
		value := value
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.MatchMetadata(actual, value)
			if err == nil {
				t.Fatal("MatchMetadata() unexpectedly succeeded")
			}
			if contains(err.Error(), "private-a") ||
				contains(err.Error(), "secret") ||
				contains(err.Error(), "other") {
				t.Fatalf("MatchMetadata() leaked a value: %v", err)
			}
		})
	}
}

func TestPayloadRoundTripChecksIdentityAndValue(t *testing.T) {
	t.Parallel()

	codec := testCodec(t)
	event := decodedEvent(t, "account.opened", accountOpened{Owner: "Ada"})
	if err := eventtest.CheckPayloadRoundTrip(
		codec,
		event,
		func(original, decoded any) bool {
			return original == decoded
		},
	); err != nil {
		t.Fatal(err)
	}

	if err := eventtest.CheckPayloadRoundTrip(
		codec,
		event,
		func(any, any) bool { return false },
	); !errors.Is(err, eventtest.ErrConformance) {
		t.Fatalf("CheckPayloadRoundTrip() error = %v", err)
	}
}

func TestPayloadRoundTripReportsBoundaryFailures(t *testing.T) {
	t.Parallel()

	event := decodedEvent(t, "account.opened", accountOpened{Owner: "Ada"})
	encoded := mustEncoded(t, "account.opened", []byte(`{"owner":"Ada"}`))
	boundaryFailure := errors.New("codec boundary failed")
	cases := map[string]struct {
		codec eventtestCodec
		equal func(any, any) bool
		want  error
	}{
		"encode": {
			codec: eventtestCodec{
				encode: func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error) {
					return eventsourcing.EncodedEvent{}, boundaryFailure
				},
			},
			equal: func(any, any) bool { return true },
			want:  boundaryFailure,
		},
		"encoded identity": {
			codec: eventtestCodec{
				encode: func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error) {
					return mustEncoded(t, "account.closed", []byte(`{}`)), nil
				},
			},
			equal: func(any, any) bool { return true },
			want:  eventtest.ErrConformance,
		},
		"decode": {
			codec: eventtestCodec{
				encode: func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error) {
					return encoded, nil
				},
				decode: func(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error) {
					return eventsourcing.DecodedEvent{}, boundaryFailure
				},
			},
			equal: func(any, any) bool { return true },
			want:  boundaryFailure,
		},
		"decoded identity": {
			codec: eventtestCodec{
				encode: func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error) {
					return encoded, nil
				},
				decode: func(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error) {
					return decodedEvent(t, "account.closed", accountClosed{}), nil
				},
			},
			equal: func(any, any) bool { return true },
			want:  eventtest.ErrConformance,
		},
	}
	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckPayloadRoundTrip(
				testCase.codec,
				event,
				testCase.equal,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("CheckPayloadRoundTrip() error = %v", err)
			}
		})
	}

	if err := eventtest.CheckPayloadRoundTrip(
		nil,
		event,
		func(any, any) bool { return true },
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("CheckPayloadRoundTrip(nil) error = %v", err)
	}
}

func TestUpcastOutputChecksLogicalEventsAndMetadata(t *testing.T) {
	t.Parallel()

	chain, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	input, err := eventsourcing.NewUpcastEvent(
		mustEncoded(t, "account.opened", []byte(`{"owner":"Ada"}`)),
		map[string]string{"source": "history"},
	)
	if err != nil {
		t.Fatal(err)
	}
	expected := []eventtest.ExpectedUpcastEvent{{
		Name:     "account.opened",
		Version:  1,
		Metadata: map[string]string{"source": "history"},
		Payload: func(payload []byte) error {
			if string(payload) != `{"owner":"Ada"}` {
				return errors.New("payload mismatch")
			}

			return nil
		},
	}}
	if err := eventtest.CheckUpcast(chain, input, expected); err != nil {
		t.Fatal(err)
	}

	expected[0].Name = "account.closed"
	if err := eventtest.CheckUpcast(
		chain,
		input,
		expected,
	); !errors.Is(err, eventtest.ErrConformance) {
		t.Fatalf("CheckUpcast() error = %v", err)
	}
}

func TestUpcastOutputReportsEveryMismatch(t *testing.T) {
	t.Parallel()

	input, err := eventsourcing.NewUpcastEvent(
		mustEncoded(t, "account.opened", []byte(`{}`)),
		map[string]string{"source": "history"},
	)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]eventtest.ExpectedUpcastEvent{
		"length": {},
		"invalid expected": {{
			Version: 1,
		}},
		"version": {{
			Name:    "account.opened",
			Version: 2,
		}},
		"metadata": {{
			Name:     "account.opened",
			Version:  1,
			Metadata: map[string]string{"source": "other"},
		}},
		"payload": {{
			Name:     "account.opened",
			Version:  1,
			Metadata: map[string]string{"source": "history"},
			Payload: func([]byte) error {
				return errors.New("private payload")
			},
		}},
	}
	for name, expected := range cases {
		expected := expected
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := eventtest.CheckUpcast(chain, input, expected)
			if !errors.Is(err, eventtest.ErrConformance) &&
				!errors.Is(err, eventsourcing.ErrInvalidArgument) {
				t.Fatalf("CheckUpcast() error = %v", err)
			}
			if err == nil || contains(err.Error(), "private payload") {
				t.Fatalf("CheckUpcast() diagnostic = %v", err)
			}
		})
	}

	upcastFailure := errors.New("upcast failed")
	rule, err := eventsourcing.NewUpcastRule(
		"account.opened",
		1,
		func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return nil, upcastFailure
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	failing, err := eventsourcing.NewUpcasterChain(rule)
	if err != nil {
		t.Fatal(err)
	}
	if err := eventtest.CheckUpcast(
		failing,
		input,
		nil,
	); !errors.Is(err, upcastFailure) {
		t.Fatalf("CheckUpcast(failing) error = %v", err)
	}
	if err := eventtest.CheckUpcast(
		nil,
		input,
		nil,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("CheckUpcast(nil) error = %v", err)
	}
}

func TestMessageIDSequenceIsDeterministicBoundedAndConcurrent(t *testing.T) {
	t.Parallel()

	generator, err := eventtest.NewMessageIDSequence("id-1", "id-2")
	if err != nil {
		t.Fatal(err)
	}
	first, err := generator.NewMessageID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.NewMessageID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.String() != "id-1" || second.String() != "id-2" {
		t.Fatalf("IDs = %q, %q", first.String(), second.String())
	}
	if _, err := generator.NewMessageID(
		context.Background(),
	); !errors.Is(err, eventtest.ErrSequenceExhausted) {
		t.Fatalf("exhausted error = %v", err)
	}

	concurrent, err := eventtest.NewMessageIDSequence(
		"concurrent-1",
		"concurrent-2",
	)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	values := make(chan string, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			id, sequenceErr := concurrent.NewMessageID(context.Background())
			if sequenceErr != nil {
				values <- sequenceErr.Error()

				return
			}
			values <- id.String()
		}()
	}
	wait.Wait()
	close(values)
	seen := map[string]bool{}
	for value := range values {
		seen[value] = true
	}
	if !seen["concurrent-1"] || !seen["concurrent-2"] {
		t.Fatalf("concurrent IDs = %v", seen)
	}
}

func TestMessageIDSequenceValidatesInputsAndCancellation(t *testing.T) {
	t.Parallel()

	if _, err := eventtest.NewMessageIDSequence(); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewMessageIDSequence() error = %v", err)
	}
	if _, err := eventtest.NewMessageIDSequence(""); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewMessageIDSequence(invalid) error = %v", err)
	}
	if _, err := eventtest.NewMessageIDSequence("duplicate", "duplicate"); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewMessageIDSequence(duplicate) error = %v", err)
	}
	oversized := make([]string, eventsourcing.MaxAppendMessages+1)
	if _, err := eventtest.NewMessageIDSequence(oversized...); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewMessageIDSequence(oversized) error = %v", err)
	}

	generator, err := eventtest.NewMessageIDSequence("id-1")
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	if _, err := generator.NewMessageID(nilContext); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("NewMessageID(nil) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := generator.NewMessageID(ctx); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("NewMessageID(cancelled) error = %v", err)
	}
	var nilGenerator *eventtest.MessageIDSequence
	if _, err := nilGenerator.NewMessageID(
		context.Background(),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil NewMessageID() error = %v", err)
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}

	return false
}

func testCodec(t *testing.T) eventsourcing.PayloadCodec {
	t.Helper()

	codec, err := eventsourcing.NewJSONCodec(
		eventsourcing.JSONEvent[accountOpened]("account.opened", 1),
	)
	if err != nil {
		t.Fatal(err)
	}

	return codec
}

func mustEncoded(
	t *testing.T,
	name string,
	payload []byte,
) eventsourcing.EncodedEvent {
	t.Helper()

	event, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        name,
		Version:     1,
		ContentType: "application/json",
		Payload:     payload,
	})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

type eventtestCodec struct {
	encode func(eventsourcing.DecodedEvent) (eventsourcing.EncodedEvent, error)
	decode func(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error)
}

func (codec eventtestCodec) Encode(
	event eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	return codec.encode(event)
}

func (codec eventtestCodec) Decode(
	event eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	return codec.decode(event)
}
