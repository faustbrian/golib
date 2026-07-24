package jsonschema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// ValidateValue validates a caller-provided value after bounded JSON
// encoding. Integer types and json.Number retain exact decimal semantics.
func (schema *Schema) ValidateValue(ctx context.Context, value any) (Result, error) {
	raw, err := schema.encodeValue(ctx, value)
	if err != nil {
		return Result{}, err
	}
	return schema.Validate(ctx, raw)
}

// ValidateValueOutput validates a caller-provided value and returns the
// selected standard output form.
func (schema *Schema) ValidateValueOutput(
	ctx context.Context,
	value any,
	format OutputFormat,
) (OutputUnit, error) {
	raw, err := schema.encodeValue(ctx, value)
	if err != nil {
		return OutputUnit{}, err
	}
	return schema.ValidateOutput(ctx, raw, format)
}

func (schema *Schema) encodeValue(ctx context.Context, value any) ([]byte, error) {
	if schema == nil || schema.plan == nil {
		return nil, fmt.Errorf("%w: nil compiled schema", ErrInvalidSchema)
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrInvalidJSON)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buffer := &limitedJSONBuffer{limit: schema.limits.MaxInputBytes + 1}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := callJSONEncode(ctx, encoder, value); err != nil {
		return nil, fmt.Errorf("%w: encode value: %w", ErrInvalidJSON, err)
	}
	raw := buffer.Bytes()
	if raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	return append([]byte(nil), raw...), nil
}

type limitedJSONBuffer struct {
	bytes.Buffer
	limit int
}

func (buffer *limitedJSONBuffer) Write(value []byte) (int, error) {
	if len(value) > buffer.limit-buffer.Len() {
		return 0, &LimitError{Resource: "input bytes", Limit: buffer.limit - 1}
	}
	return buffer.Buffer.Write(value)
}
