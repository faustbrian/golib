package glue

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

var (
	// ErrInvalidFrame marks malformed framing, invalid IDs, or exceeded frame bounds.
	ErrInvalidFrame = errors.New("AWS Glue schema registry: invalid frame")
	// ErrCompressionUnsupported marks a valid Glue compression byte this framer does not implement.
	ErrCompressionUnsupported = errors.New("AWS Glue schema registry: compressed frame unsupported")
)

// UncompressedFramer implements AWS Glue header version 3 with compression byte
// 0 and a 16-byte schema-version UUID. ZLIB byte 5 is rejected explicitly.
type UncompressedFramer struct {
	scope      string
	maxPayload int
}

// NewUncompressedFramer constructs a bounded AWS Glue uncompressed framer.
func NewUncompressedFramer(scope string, maxPayloadBytes int) (*UncompressedFramer, error) {
	if scope == "" || maxPayloadBytes <= 0 {
		return nil, fmt.Errorf("invalid AWS Glue framer config")
	}
	return &UncompressedFramer{scope: scope, maxPayload: maxPayloadBytes}, nil
}

// Frame writes an AWS Glue header-version-3 frame with compression byte zero.
func (framer *UncompressedFramer) Frame(ctx context.Context, id schemaregistry.ProviderID, payload []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id.Provider != ProviderName || id.Scope != framer.scope || len(payload) > framer.maxPayload {
		return nil, fmt.Errorf("%w: ID scope or payload", ErrInvalidFrame)
	}
	uuid, err := decodeUUID(id.Value)
	if err != nil {
		return nil, err
	}
	framed := make([]byte, 18+len(payload))
	framed[0] = 3
	copy(framed[2:18], uuid[:])
	copy(framed[18:], payload)
	return framed, nil
}

// Unframe parses an uncompressed AWS Glue frame and returns an owned payload copy.
func (framer *UncompressedFramer) Unframe(ctx context.Context, framed []byte) (schemaregistry.ProviderID, []byte, error) {
	if err := ctx.Err(); err != nil {
		return schemaregistry.ProviderID{}, nil, err
	}
	if len(framed) < 18 || framed[0] != 3 {
		return schemaregistry.ProviderID{}, nil, ErrInvalidFrame
	}
	if framed[1] == 5 {
		return schemaregistry.ProviderID{}, nil, ErrCompressionUnsupported
	}
	if framed[1] != 0 {
		return schemaregistry.ProviderID{}, nil, ErrInvalidFrame
	}
	payload := framed[18:]
	if len(payload) > framer.maxPayload {
		return schemaregistry.ProviderID{}, nil, ErrInvalidFrame
	}
	value := encodeUUID(framed[2:18])
	return schemaregistry.ProviderID{Provider: ProviderName, Scope: framer.scope, Value: value}, append([]byte(nil), payload...), nil
}

func validUUID(value string) bool {
	_, err := decodeUUID(value)
	return err == nil
}

func decodeUUID(value string) ([16]byte, error) {
	var uuid [16]byte
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return uuid, fmt.Errorf("%w: schema version UUID", ErrInvalidFrame)
	}
	_, err := hex.Decode(uuid[:], []byte(strings.ReplaceAll(value, "-", "")))
	if err != nil {
		return uuid, fmt.Errorf("%w: schema version UUID", ErrInvalidFrame)
	}
	return uuid, nil
}

func encodeUUID(value []byte) string {
	hexValue := hex.EncodeToString(value)
	return hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:]
}
