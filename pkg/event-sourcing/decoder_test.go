package eventsourcing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestEventDecoderExposesOrderedLogicalHistory(t *testing.T) {
	t.Parallel()

	split := mustUpcastRule(
		t,
		"legacy.account-created",
		1,
		func(input eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			metadata := input.Metadata()
			metadata["migrated"] = "true"

			return []eventsourcing.UpcastEvent{
				mustUpcastEvent(
					t,
					"account.opened",
					1,
					input.Event().Payload(),
					metadata,
				),
				mustUpcastEvent(
					t,
					"account.owner-set",
					1,
					input.Event().Payload(),
					metadata,
				),
			}, nil
		},
	)
	upcasters, err := eventsourcing.NewUpcasterChain(split)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := eventsourcing.NewEventDecoder(repositoryCodec(t), upcasters)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "history-1",
			Stream:     stream,
			Event:      mustEncodedEvent(t, "legacy.account-created", 1, []byte(`{"owner":"Ada"}`)),
			Metadata:   map[string]string{"source": "legacy"},
			RecordedAt: repositoryFixedTime(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 7,
	})
	if err != nil {
		t.Fatal(err)
	}

	logical, err := decoder.Decode(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(logical) != 2 ||
		logical[0].Event().Name().String() != "account.opened" ||
		logical[1].Event().Name().String() != "account.owner-set" ||
		logical[0].SegmentIndex() != 0 ||
		logical[1].SegmentIndex() != 1 ||
		logical[0].SegmentCount() != 2 ||
		logical[1].SegmentCount() != 2 ||
		logical[0].SourceMessage().StreamVersion() != 7 ||
		logical[0].Metadata()["source"] != "legacy" ||
		logical[0].Metadata()["migrated"] != "true" {
		t.Fatalf("Decode() = %#v", logical)
	}
	metadata := logical[0].Metadata()
	metadata["source"] = "changed"
	if logical[0].Metadata()["source"] != "legacy" {
		t.Fatal("logical event metadata aliases the caller")
	}
}

func TestEventDecoderValidatesCompositionAndStoredInput(t *testing.T) {
	t.Parallel()

	chain, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	codec := repositoryCodec(t)
	if decoder, err := eventsourcing.NewEventDecoder(nil, chain); decoder != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewEventDecoder(nil codec) = %#v, %v", decoder, err)
	}
	if decoder, err := eventsourcing.NewEventDecoder(codec, nil); decoder != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewEventDecoder(nil upcasters) = %#v, %v", decoder, err)
	}
	decoder, err := eventsourcing.NewEventDecoder(codec, chain)
	if err != nil {
		t.Fatal(err)
	}
	var nilDecoder *eventsourcing.EventDecoder
	if logical, err := nilDecoder.Decode(eventsourcing.Message{}); logical != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Decode() = %#v, %v", logical, err)
	}
	if logical, err := decoder.Decode(eventsourcing.Message{}); logical != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Decode(zero) = %#v, %v", logical, err)
	}
	if logical, err := decoder.DecodeContext(
		decoderNilContext(),
		eventsourcing.Message{},
	); logical != nil || !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("DecodeContext(nil) = %#v, %v", logical, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	stream, err := eventsourcing.NewStreamID("account", "cancelled")
	if err != nil {
		t.Fatal(err)
	}
	if logical, err := decoder.DecodeContext(
		cancelled,
		persistedRepositoryMessage(
			t,
			stream,
			mustEncodedEvent(t, "account.opened", 1, []byte(`{}`)),
		),
	); logical != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeContext(cancelled) = %#v, %v", logical, err)
	}
	if !(eventsourcing.LogicalEvent{}).IsZero() {
		t.Fatal("zero LogicalEvent is assigned")
	}
}

func decoderNilContext() context.Context {
	return nil
}

func TestEventDecoderPreservesDecodeFailuresAndReviewedDrops(t *testing.T) {
	t.Parallel()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}
	message := persistedRepositoryMessage(
		t,
		stream,
		mustEncodedEvent(t, "legacy.account-created", 1, []byte(`{}`)),
	)
	decodeFailure := errors.New("decode failure")
	tests := map[string]struct {
		codec eventsourcing.PayloadCodec
		chain *eventsourcing.UpcasterChain
		want  error
	}{
		"upcast": {
			codec: repositoryCodec(t),
			chain: decoderUpcasterChain(t, func(eventsourcing.UpcastEvent) (
				[]eventsourcing.UpcastEvent,
				error,
			) {
				return nil, decodeFailure
			}),
			want: decodeFailure,
		},
		"codec": {
			codec: decoderCodec{decode: func(eventsourcing.EncodedEvent) (
				eventsourcing.DecodedEvent,
				error,
			) {
				return eventsourcing.DecodedEvent{}, decodeFailure
			}},
			chain: mustEmptyUpcasterChain(t),
			want:  decodeFailure,
		},
		"zero decoded event": {
			codec: decoderCodec{decode: func(eventsourcing.EncodedEvent) (
				eventsourcing.DecodedEvent,
				error,
			) {
				return eventsourcing.DecodedEvent{}, nil
			}},
			chain: mustEmptyUpcasterChain(t),
			want:  eventsourcing.ErrCorruptHistory,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decoder, err := eventsourcing.NewEventDecoder(
				testCase.codec,
				testCase.chain,
			)
			if err != nil {
				t.Fatal(err)
			}
			if logical, err := decoder.Decode(message); logical != nil ||
				!errors.Is(err, testCase.want) {
				t.Fatalf("Decode() = %#v, %v", logical, err)
			}
		})
	}

	policy, err := eventsourcing.NewReviewedDropPolicy(
		"obsolete projection event",
		"maintainer",
		time.Date(2026, time.July, 26, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	drop := mustUpcastRule(
		t,
		"legacy.account-created",
		1,
		func(eventsourcing.UpcastEvent) ([]eventsourcing.UpcastEvent, error) {
			return nil, nil
		},
		eventsourcing.AllowUpcastDrop(policy),
	)
	chain, err := eventsourcing.NewUpcasterChain(drop)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := eventsourcing.NewEventDecoder(repositoryCodec(t), chain)
	if err != nil {
		t.Fatal(err)
	}
	logical, err := decoder.Decode(message)
	if err != nil || len(logical) != 0 {
		t.Fatalf("Decode(drop) = %#v, %v", logical, err)
	}
}

type decoderCodec struct {
	decode func(eventsourcing.EncodedEvent) (eventsourcing.DecodedEvent, error)
}

func (decoderCodec) Encode(
	eventsourcing.DecodedEvent,
) (eventsourcing.EncodedEvent, error) {
	return eventsourcing.EncodedEvent{}, errors.New("encode is not supported")
}

func (codec decoderCodec) Decode(
	event eventsourcing.EncodedEvent,
) (eventsourcing.DecodedEvent, error) {
	return codec.decode(event)
}

func decoderUpcasterChain(
	t *testing.T,
	upcaster eventsourcing.UpcasterFunc,
) *eventsourcing.UpcasterChain {
	t.Helper()

	rule := mustUpcastRule(t, "legacy.account-created", 1, upcaster)
	chain, err := eventsourcing.NewUpcasterChain(rule)
	if err != nil {
		t.Fatal(err)
	}

	return chain
}

func mustEmptyUpcasterChain(t *testing.T) *eventsourcing.UpcasterChain {
	t.Helper()

	chain, err := eventsourcing.NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}

	return chain
}
