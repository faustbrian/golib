package openrpc_test

import (
	"bytes"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
)

func FuzzAcceptedDocumentsRoundTripDeterministically(f *testing.F) {
	f.Add([]byte(`{"openrpc":"1.4.1","info":{"title":"Fuzz","version":"1"},"methods":[]}`))
	f.Add([]byte("{\n  \"openrpc\": \"1.4.9\", \"info\": {\"title\": \"Fuzz\", \"version\": \"1\"}, \"methods\": [], \"x-value\": 1\n}"))
	f.Fuzz(func(t *testing.T, input []byte) {
		original := append([]byte(nil), input...)
		options := parse.DefaultOptions()
		options.JSON = jsonvalue.Policy{MaxBytes: 1 << 20, MaxDepth: 64, MaxTokens: 100_000}
		options.UnknownFields = parse.PreserveUnknownFields
		result, err := parse.Decode(input, options)
		if err != nil {
			return
		}
		if len(input) != 0 {
			input[0] ^= 0xff
		}
		if !bytes.Equal(result.PreservingJSON(), original) {
			t.Fatal("accepted source was not owned or preserved")
		}
		canonical, err := openrpc.MarshalCanonical(result.Document())
		if err != nil {
			t.Fatal(err)
		}
		reparsed, err := parse.Decode(canonical, options)
		if err != nil {
			t.Fatal(err)
		}
		again, err := openrpc.MarshalCanonical(reparsed.Document())
		if err != nil || !bytes.Equal(canonical, again) {
			t.Fatalf("canonical round trip changed: %v", err)
		}
	})
}
