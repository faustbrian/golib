package jsonvalue_test

import (
	"bytes"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func FuzzParseStrictJSON(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`null`),
		[]byte(`{"a":1,"b":[true,false]}`),
		[]byte(`{"duplicate":1,"duplicate":2}`),
		[]byte(`1e999999`),
		{'{', '"', 0xff, '"', ':', '1', '}'},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		policy := jsonvalue.Policy{MaxBytes: 4 << 10, MaxDepth: 32, MaxTokens: 512}
		value, err := jsonvalue.Parse(input, policy)
		if err != nil {
			return
		}
		if !bytes.Equal(value.Bytes(), input) {
			t.Fatal("accepted value did not preserve exact bytes")
		}
		encoded, err := value.MarshalJSON()
		if err != nil || !bytes.Equal(encoded, input) {
			t.Fatalf("MarshalJSON() = %q, %v", encoded, err)
		}
		if _, err := jsonvalue.Parse(encoded, policy); err != nil {
			t.Fatalf("accepted value failed to parse again: %v", err)
		}
	})
}
