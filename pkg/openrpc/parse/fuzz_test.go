package parse_test

import (
	"context"
	"reflect"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func FuzzDecodeOpenRPCDocument(f *testing.F) {
	f.Add([]byte(`{"openrpc":"1.4.1","info":{"title":"Fuzz","version":"1"},"methods":[]}`))
	f.Add([]byte(`{"openrpc":"1.4.1","openrpc":"1.4.1"}`))
	f.Add([]byte(`{"openrpc":"1.4.1","info":null,"methods":[]}`))
	f.Add([]byte(`{"openrpc":"1.4.1","info":{"title":"Fuzz","version":"1"},"methods":[{"name":"m","params":[]}]}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		options := parse.DefaultOptions()
		options.JSON = jsonvalue.Policy{MaxBytes: 16 << 10, MaxDepth: 48, MaxTokens: 2_048}
		options.MaxMethods = 64
		options.MaxServerVariables = 64
		parsed, err := parse.Decode(input, options)
		if err != nil {
			return
		}
		canonical, err := openrpc.MarshalCanonical(parsed.Document())
		if err != nil {
			t.Fatalf("accepted document did not serialize: %v", err)
		}
		if _, err := parse.Decode(canonical, options); err != nil {
			t.Fatalf("canonical document failed to parse again: %v", err)
		}
		raw, err := jsonvalue.Parse(canonical, options.JSON)
		if err != nil {
			t.Fatal(err)
		}
		if report := validate.MetaSchema(context.Background(), raw, 100); report.Err() != nil {
			t.Fatalf("meta-schema execution failed: %v", report.Err())
		}
		validationOptions := validate.DefaultOptions()
		first := validate.Document(context.Background(), parsed.Document(), validationOptions)
		second := validate.Document(context.Background(), parsed.Document(), validationOptions)
		if first.Truncated() != second.Truncated() ||
			!reflect.DeepEqual(first.Diagnostics(), second.Diagnostics()) {
			t.Fatal("semantic validation was not deterministic")
		}
	})
}
