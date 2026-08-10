package confluent

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

// ErrInvalidFrame marks malformed framing, invalid IDs, or exceeded frame bounds.
var ErrInvalidFrame = errors.New("confluent schema registry: invalid frame")

// ClassicFramer implements Confluent wire version 0 for Avro and JSON Schema:
// one zero magic byte, one big-endian uint32 schema ID, then payload. Protobuf
// message-index framing is intentionally a separate contract.
type ClassicFramer struct {
	scope      string
	maxPayload int
}

// ProtobufFramer implements Confluent wire version 0 including the Protobuf
// message-index vector. Message indexes select one nested descriptor and are
// deliberately exposed rather than hidden in the value codec.
type ProtobufFramer struct {
	scope      string
	maxPayload int
	maxIndexes int
}

// NewClassicFramer constructs a bounded Confluent Avro or JSON Schema framer.
func NewClassicFramer(scope string, maxPayloadBytes int) (*ClassicFramer, error) {
	if scope == "" || maxPayloadBytes <= 0 {
		return nil, fmt.Errorf("invalid Confluent framer config")
	}
	return &ClassicFramer{scope: scope, maxPayload: maxPayloadBytes}, nil
}

// NewProtobufFramer constructs a bounded Confluent Protobuf framer.
func NewProtobufFramer(scope string, maxPayloadBytes, maxMessageIndexes int) (*ProtobufFramer, error) {
	if scope == "" || maxPayloadBytes <= 0 || maxMessageIndexes <= 0 {
		return nil, fmt.Errorf("invalid Confluent Protobuf framer config")
	}
	return &ProtobufFramer{scope: scope, maxPayload: maxPayloadBytes, maxIndexes: maxMessageIndexes}, nil
}

// Frame writes a version-0 frame for an explicit scoped schema ID.
func (framer *ClassicFramer) Frame(ctx context.Context, id schemaregistry.ProviderID, payload []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id.Provider != ProviderName || id.Scope != framer.scope || len(payload) > framer.maxPayload {
		return nil, fmt.Errorf("%w: ID scope or payload", ErrInvalidFrame)
	}
	value, err := strconv.ParseUint(id.Value, 10, 32)
	if err != nil || value == 0 {
		return nil, fmt.Errorf("%w: schema ID", ErrInvalidFrame)
	}
	framed := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(framed[1:5], uint32(value))
	copy(framed[5:], payload)
	return framed, nil
}

// Unframe parses a bounded version-0 frame and returns an owned payload copy.
func (framer *ClassicFramer) Unframe(ctx context.Context, framed []byte) (schemaregistry.ProviderID, []byte, error) {
	if err := ctx.Err(); err != nil {
		return schemaregistry.ProviderID{}, nil, err
	}
	if len(framed) < 5 || framed[0] != 0 {
		return schemaregistry.ProviderID{}, nil, ErrInvalidFrame
	}
	payload := framed[5:]
	if len(payload) > framer.maxPayload {
		return schemaregistry.ProviderID{}, nil, ErrInvalidFrame
	}
	value := binary.BigEndian.Uint32(framed[1:5])
	if value == 0 {
		return schemaregistry.ProviderID{}, nil, ErrInvalidFrame
	}
	return schemaregistry.ProviderID{
		Provider: ProviderName,
		Scope:    framer.scope,
		Value:    strconv.FormatUint(uint64(value), 10),
	}, append([]byte(nil), payload...), nil
}

// FrameMessage frames one Protobuf payload and its descriptor index path.
func (framer *ProtobufFramer) FrameMessage(
	ctx context.Context,
	id schemaregistry.ProviderID,
	messageIndexes []int,
	payload []byte,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id.Provider != ProviderName || id.Scope != framer.scope || len(payload) > framer.maxPayload ||
		len(messageIndexes) == 0 || len(messageIndexes) > framer.maxIndexes {
		return nil, fmt.Errorf("%w: ID scope, indexes, or payload", ErrInvalidFrame)
	}
	value, err := strconv.ParseUint(id.Value, 10, 32)
	if err != nil || value == 0 {
		return nil, fmt.Errorf("%w: schema ID", ErrInvalidFrame)
	}
	for _, index := range messageIndexes {
		if index < 0 {
			return nil, fmt.Errorf("%w: negative message index", ErrInvalidFrame)
		}
	}
	framed := make([]byte, 5)
	binary.BigEndian.PutUint32(framed[1:5], uint32(value))
	if len(messageIndexes) == 1 && messageIndexes[0] == 0 {
		framed = append(framed, 0)
	} else {
		framed = appendSignedVarint(framed, len(messageIndexes))
		for _, index := range messageIndexes {
			framed = appendSignedVarint(framed, index)
		}
	}
	return append(framed, payload...), nil
}

// UnframeMessage parses one bounded Confluent Protobuf frame.
func (framer *ProtobufFramer) UnframeMessage(
	ctx context.Context,
	framed []byte,
) (schemaregistry.ProviderID, []int, []byte, error) {
	if err := ctx.Err(); err != nil {
		return schemaregistry.ProviderID{}, nil, nil, err
	}
	if len(framed) < 6 || framed[0] != 0 {
		return schemaregistry.ProviderID{}, nil, nil, ErrInvalidFrame
	}
	value := binary.BigEndian.Uint32(framed[1:5])
	if value == 0 {
		return schemaregistry.ProviderID{}, nil, nil, ErrInvalidFrame
	}
	offset := 5
	length, consumed, ok := readSignedVarint(framed[offset:])
	if !ok {
		return schemaregistry.ProviderID{}, nil, nil, ErrInvalidFrame
	}
	offset += consumed
	indexes := []int{0}
	if length != 0 {
		if length < 1 || length > framer.maxIndexes {
			return schemaregistry.ProviderID{}, nil, nil, ErrInvalidFrame
		}
		indexes = make([]int, length)
		for index := range indexes {
			value, consumed, ok := readSignedVarint(framed[offset:])
			if !ok || value < 0 {
				return schemaregistry.ProviderID{}, nil, nil, ErrInvalidFrame
			}
			indexes[index] = value
			offset += consumed
		}
	}
	payload := framed[offset:]
	if len(payload) > framer.maxPayload {
		return schemaregistry.ProviderID{}, nil, nil, ErrInvalidFrame
	}
	return schemaregistry.ProviderID{
		Provider: ProviderName, Scope: framer.scope, Value: strconv.FormatUint(uint64(value), 10),
	}, indexes, append([]byte(nil), payload...), nil
}

func appendSignedVarint(destination []byte, value int) []byte {
	var encoded [binary.MaxVarintLen64]byte
	count := binary.PutUvarint(encoded[:], uint64(value)<<1)
	return append(destination, encoded[:count]...)
}

func readSignedVarint(source []byte) (int, int, bool) {
	encoded, count := binary.Uvarint(source)
	if count <= 0 {
		return 0, 0, false
	}
	decoded := int64(encoded>>1) ^ -int64(encoded&1)
	return int(decoded), count, true
}
