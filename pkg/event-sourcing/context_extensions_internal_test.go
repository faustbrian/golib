package eventsourcing

import (
	"context"
	"errors"
	"testing"
)

type contextExtensionCodec struct {
	encodeCalls int
	decodeCalls int
}

func (codec *contextExtensionCodec) Encode(
	DecodedEvent,
) (EncodedEvent, error) {
	codec.encodeCalls++

	return EncodedEvent{}, nil
}

func (codec *contextExtensionCodec) Decode(
	EncodedEvent,
) (DecodedEvent, error) {
	codec.decodeCalls++

	return DecodedEvent{}, nil
}

type contextExtensionUpcaster func(
	UpcastEvent,
) ([]UpcastEvent, error)

func (upcaster contextExtensionUpcaster) Upcast(
	event UpcastEvent,
) ([]UpcastEvent, error) {
	return upcaster(event)
}

func TestContextExtensionsPreserveFallbackAndCancellation(t *testing.T) {
	t.Parallel()

	codec := &contextExtensionCodec{}
	if _, err := encodePayload(
		context.Background(),
		codec,
		DecodedEvent{},
	); err != nil || codec.encodeCalls != 1 {
		t.Fatalf("encodePayload() calls = %d, error = %v", codec.encodeCalls, err)
	}
	if _, err := decodePayload(
		context.Background(),
		codec,
		EncodedEvent{},
	); err != nil || codec.decodeCalls != 1 {
		t.Fatalf("decodePayload() calls = %d, error = %v", codec.decodeCalls, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := encodePayload(
		cancelled,
		codec,
		DecodedEvent{},
	); !errors.Is(err, context.Canceled) || codec.encodeCalls != 1 {
		t.Fatalf("cancelled encode = %v, calls = %d", err, codec.encodeCalls)
	}
	if _, err := decodePayload(
		cancelled,
		codec,
		EncodedEvent{},
	); !errors.Is(err, context.Canceled) || codec.decodeCalls != 1 {
		t.Fatalf("cancelled decode = %v, calls = %d", err, codec.decodeCalls)
	}

	upcastCalls := 0
	upcaster := contextExtensionUpcaster(func(
		event UpcastEvent,
	) ([]UpcastEvent, error) {
		upcastCalls++

		return []UpcastEvent{event}, nil
	})
	event := UpcastEvent{
		event: EncodedEvent{
			name:        EventName{value: "account.opened"},
			version:     1,
			contentType: JSONContentType,
			payload:     []byte(`{}`),
		},
	}
	output, err := upcastWithContext(context.Background(), upcaster, event)
	if err != nil || len(output) != 1 || upcastCalls != 1 {
		t.Fatalf("upcast fallback = %#v, %v; calls = %d", output, err, upcastCalls)
	}
	if _, err := upcastWithContext(
		cancelled,
		upcaster,
		event,
	); !errors.Is(err, context.Canceled) || upcastCalls != 1 {
		t.Fatalf("cancelled upcast = %v, calls = %d", err, upcastCalls)
	}
}

func TestUpcasterChainContextValidatesContext(t *testing.T) {
	t.Parallel()

	chain, err := NewUpcasterChain()
	if err != nil {
		t.Fatal(err)
	}
	event := UpcastEvent{
		event: EncodedEvent{
			name:        EventName{value: "account.opened"},
			version:     1,
			contentType: JSONContentType,
			payload:     []byte(`{}`),
		},
	}
	if _, err := chain.UpcastContext(contextExtensionNilContext(), event); !errors.Is(
		err,
		ErrInvalidArgument,
	) {
		t.Fatalf("UpcastContext(nil) error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chain.UpcastContext(cancelled, event); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("UpcastContext(cancelled) error = %v", err)
	}
	output, err := chain.UpcastContext(context.Background(), event)
	if err != nil || len(output) != 1 {
		t.Fatalf("UpcastContext() = %#v, %v", output, err)
	}
}

func contextExtensionNilContext() context.Context {
	return nil
}
