package schemaregistry

import (
	"context"
	"fmt"
)

// ValueCodec performs business serialization against an already-resolved
// schema. Implementations must not access a registry.
type ValueCodec interface {
	Encode(context.Context, Schema, any) ([]byte, error)
	Decode(context.Context, Schema, []byte, any) error
}

// Framer owns one explicitly versioned provider wire format. It receives and
// returns opaque provider IDs, never portable fingerprints.
type Framer interface {
	Frame(context.Context, ProviderID, []byte) ([]byte, error)
	Unframe(context.Context, []byte) (ProviderID, []byte, error)
}

// CodecLimits bound both business payloads and complete framed messages.
type CodecLimits struct {
	MaxPayloadBytes int
	MaxFrameBytes   int
}

// WireMessage is parsed framing metadata plus an owned payload copy. Callers
// explicitly resolve ID before invoking Decode.
type WireMessage struct {
	ID      ProviderID
	Payload []byte
}

// CodecIntegration composes a business codec and provider framer without a
// registry dependency or hidden I/O.
type CodecIntegration struct {
	codec  ValueCodec
	framer Framer
	limits CodecLimits
}

// NewCodecIntegration validates explicit codec and framing bounds.
func NewCodecIntegration(
	codec ValueCodec,
	framer Framer,
	limits CodecLimits,
) (*CodecIntegration, error) {
	if interfaceIsNil(codec) || interfaceIsNil(framer) {
		return nil, fmt.Errorf("%w: codec and framer are required", ErrInvalidRequest)
	}
	if limits.MaxPayloadBytes <= 0 || limits.MaxFrameBytes < max(1, limits.MaxPayloadBytes) {
		return nil, fmt.Errorf("%w: codec limits", ErrInvalidRequest)
	}
	return &CodecIntegration{codec: codec, framer: framer, limits: limits}, nil
}

// Encode serializes against an explicit schema, checks payload bounds, and
// frames with an explicit provider ID.
func (integration *CodecIntegration) Encode(
	ctx context.Context,
	schema Schema,
	id ProviderID,
	value any,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if schema.Fingerprint() == (Fingerprint{}) || id.Provider == "" || id.Value == "" {
		return nil, fmt.Errorf("%w: schema and provider ID are required", ErrInvalidRequest)
	}
	payload, err := integration.codec.Encode(ctx, schema, value)
	if err != nil {
		return nil, fmt.Errorf("encode value: %w", err)
	}
	if len(payload) > integration.limits.MaxPayloadBytes {
		return nil, fmt.Errorf("%w: payload bytes", ErrLimitExceeded)
	}
	framed, err := integration.framer.Frame(ctx, id, payload)
	if err != nil {
		return nil, fmt.Errorf("frame payload: %w", err)
	}
	if len(framed) > integration.limits.MaxFrameBytes {
		return nil, fmt.Errorf("%w: frame bytes", ErrLimitExceeded)
	}
	return append([]byte(nil), framed...), nil
}

// Parse validates the complete frame bound before delegating to the explicit
// provider framer. It never resolves the returned ID.
func (integration *CodecIntegration) Parse(
	ctx context.Context,
	framed []byte,
) (WireMessage, error) {
	if err := ctx.Err(); err != nil {
		return WireMessage{}, err
	}
	if len(framed) > integration.limits.MaxFrameBytes {
		return WireMessage{}, fmt.Errorf("%w: frame bytes", ErrLimitExceeded)
	}
	id, payload, err := integration.framer.Unframe(ctx, framed)
	if err != nil {
		return WireMessage{}, fmt.Errorf("unframe payload: %w", err)
	}
	if id.Provider == "" || id.Value == "" {
		return WireMessage{}, fmt.Errorf("%w: provider ID", ErrInvalidSchema)
	}
	if len(payload) > integration.limits.MaxPayloadBytes {
		return WireMessage{}, fmt.Errorf("%w: payload bytes", ErrLimitExceeded)
	}
	return WireMessage{ID: id, Payload: append([]byte(nil), payload...)}, nil
}

// Decode applies an already-resolved schema to one parsed payload. Registry
// access cannot occur through this API.
func (integration *CodecIntegration) Decode(
	ctx context.Context,
	schema Schema,
	message WireMessage,
	target any,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if schema.Fingerprint() == (Fingerprint{}) || message.ID.Provider == "" || message.ID.Value == "" {
		return fmt.Errorf("%w: schema and parsed provider ID are required", ErrInvalidRequest)
	}
	if len(message.Payload) > integration.limits.MaxPayloadBytes {
		return fmt.Errorf("%w: payload bytes", ErrLimitExceeded)
	}
	if err := integration.codec.Decode(ctx, schema, append([]byte(nil), message.Payload...), target); err != nil {
		return fmt.Errorf("decode value: %w", err)
	}
	return nil
}
