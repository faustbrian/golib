package jsonschema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

type valueKind uint8

const (
	kindNull valueKind = iota
	kindBoolean
	kindNumber
	kindString
	kindArray
	kindObject
)

type jsonValue struct {
	kind    valueKind
	boolean bool
	number  string
	text    string
	array   []*jsonValue
	object  map[string]*jsonValue
}

type jsonParser struct {
	ctx     context.Context
	decoder jsonTokenDecoder
	limits  Limits
	values  int
}

type jsonTokenDecoder interface {
	Token() (json.Token, error)
	More() bool
	InputOffset() int64
}

func decodeJSON(ctx context.Context, raw []byte, limits Limits) (*jsonValue, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrInvalidJSON)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) > limits.MaxInputBytes {
		return nil, &JSONError{
			Kind:  ErrLimitExceeded,
			Cause: fmt.Errorf("input is %d bytes; limit is %d", len(raw), limits.MaxInputBytes),
		}
	}
	if !utf8.Valid(raw) {
		return nil, &JSONError{Kind: ErrInvalidJSON, Cause: errors.New("invalid UTF-8")}
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	parser := jsonParser{ctx: ctx, decoder: decoder, limits: limits}

	value, err := parser.value(1)
	if err != nil {
		return nil, err
	}

	_, err = decoder.Token()
	if !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON value")
		}

		return nil, parser.wrap(ErrInvalidJSON, err)
	}

	return value, nil
}

func (parser *jsonParser) value(depth int) (*jsonValue, error) {
	if err := parser.ctx.Err(); err != nil {
		return nil, err
	}
	if depth > parser.limits.MaxNestingDepth {
		return nil, parser.wrap(
			ErrLimitExceeded,
			fmt.Errorf("JSON nesting exceeds %d", parser.limits.MaxNestingDepth),
		)
	}

	parser.values++
	if parser.values > parser.limits.MaxTotalValues {
		return nil, parser.wrap(
			ErrLimitExceeded,
			fmt.Errorf("JSON values exceed %d", parser.limits.MaxTotalValues),
		)
	}

	token, err := parser.decoder.Token()
	if err != nil {
		return nil, parser.wrap(ErrInvalidJSON, err)
	}

	switch token := token.(type) {
	case nil:
		return &jsonValue{kind: kindNull}, nil
	case bool:
		return &jsonValue{kind: kindBoolean, boolean: token}, nil
	case string:
		return &jsonValue{kind: kindString, text: token}, nil
	case json.Number:
		if len(token.String()) > parser.limits.MaxNumberBytes {
			return nil, parser.wrap(
				ErrLimitExceeded,
				fmt.Errorf("number exceeds %d bytes", parser.limits.MaxNumberBytes),
			)
		}

		return &jsonValue{kind: kindNumber, number: token.String()}, nil
	case json.Delim:
		switch token {
		case '{':
			return parser.object(depth)
		case '[':
			return parser.array(depth)
		default:
			return nil, parser.wrap(ErrInvalidJSON, fmt.Errorf("unexpected delimiter %q", token))
		}
	default:
		return nil, parser.wrap(ErrInvalidJSON, fmt.Errorf("unexpected token %T", token))
	}
}

func (parser *jsonParser) object(depth int) (*jsonValue, error) {
	object := make(map[string]*jsonValue)

	for parser.decoder.More() {
		if len(object) >= parser.limits.MaxObjectMembers {
			return nil, parser.wrap(
				ErrLimitExceeded,
				fmt.Errorf("object members exceed %d", parser.limits.MaxObjectMembers),
			)
		}

		token, err := parser.decoder.Token()
		if err != nil {
			return nil, parser.wrap(ErrInvalidJSON, err)
		}
		name, ok := token.(string)
		if !ok {
			return nil, parser.wrap(ErrInvalidJSON, errors.New("object key is not a string"))
		}
		if _, duplicate := object[name]; duplicate {
			return nil, parser.wrap(
				ErrInvalidJSON,
				fmt.Errorf("duplicate object member %q", name),
			)
		}

		value, err := parser.value(depth + 1)
		if err != nil {
			return nil, err
		}
		object[name] = value
	}

	if _, err := parser.decoder.Token(); err != nil {
		return nil, parser.wrap(ErrInvalidJSON, err)
	}

	return &jsonValue{kind: kindObject, object: object}, nil
}

func (parser *jsonParser) array(depth int) (*jsonValue, error) {
	array := make([]*jsonValue, 0)

	for parser.decoder.More() {
		if len(array) >= parser.limits.MaxArrayItems {
			return nil, parser.wrap(
				ErrLimitExceeded,
				fmt.Errorf("array items exceed %d", parser.limits.MaxArrayItems),
			)
		}

		value, err := parser.value(depth + 1)
		if err != nil {
			return nil, err
		}
		array = append(array, value)
	}

	if _, err := parser.decoder.Token(); err != nil {
		return nil, parser.wrap(ErrInvalidJSON, err)
	}

	return &jsonValue{kind: kindArray, array: array}, nil
}

func (parser *jsonParser) wrap(kind error, cause error) error {
	return &JSONError{
		Offset: parser.decoder.InputOffset(),
		Kind:   kind,
		Cause:  cause,
	}
}
