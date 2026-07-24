package jsonvalue_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func FuzzValueMarshalJSONWithLimits(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte("plain"),
		[]byte("<script>"),
		[]byte("\x00\n"),
		[]byte("snowman ☃"),
		{0xff},
	} {
		f.Add(seed, uint16(64), uint8(4), uint16(8))
	}
	f.Fuzz(func(
		t *testing.T,
		raw []byte,
		byteLimit uint16,
		depthLimit uint8,
		nodeLimit uint16,
	) {
		if !utf8.Valid(raw) {
			return
		}
		text, err := jsonvalue.String(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		array, err := jsonvalue.Array([]jsonvalue.Value{text, jsonvalue.Null()})
		if err != nil {
			t.Fatal(err)
		}
		value, err := jsonvalue.Object([]jsonvalue.Member{{Name: string(raw), Value: array}})
		if err != nil {
			t.Fatal(err)
		}
		limits := jsonvalue.MarshalLimits{
			MaxBytes: int(byteLimit) + 1,
			MaxDepth: int(depthLimit) + 1,
			MaxNodes: int(nodeLimit) + 1,
		}
		first, firstErr := value.MarshalJSONWithLimits(limits)
		second, secondErr := value.MarshalJSONWithLimits(limits)
		if firstErr != nil {
			if !errors.Is(firstErr, jsonvalue.ErrMarshalLimit) ||
				!errors.Is(secondErr, jsonvalue.ErrMarshalLimit) {
				t.Fatalf("unstable limit errors = %v, %v", firstErr, secondErr)
			}
			return
		}
		if secondErr != nil || !bytes.Equal(first, second) {
			t.Fatalf("unstable output = %q, %v; %q, %v", first, firstErr, second, secondErr)
		}
		if len(first) > limits.MaxBytes || !json.Valid(first) {
			t.Fatalf("invalid bounded output = %q", first)
		}
	})
}
