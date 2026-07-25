package discovery_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/discovery"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
)

func FuzzDiscoverySnapshotsAreDeterministic(f *testing.F) {
	f.Add([]byte(`{"openrpc":"1.4.1","info":{"title":"Fuzz","version":"1"},"methods":[]}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		options := openrpcparse.DefaultOptions()
		options.JSON = jsonvalue.Policy{MaxBytes: 1 << 20, MaxDepth: 64, MaxTokens: 100_000}
		parsed, err := openrpcparse.Decode(input, options)
		if err != nil {
			return
		}
		service, err := discovery.NewService(discovery.Static(parsed.Document()), nil)
		if err != nil {
			t.Fatal(err)
		}
		first, err := service.Discover(context.Background())
		if err != nil {
			return
		}
		second, err := service.Discover(context.Background())
		if err != nil || first.Revision() != second.Revision() || !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatalf("discovery was not deterministic: %v", err)
		}
	})
}
