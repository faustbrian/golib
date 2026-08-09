package tenancyjsonrpc_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenancyjsonrpc "github.com/faustbrian/golib/pkg/tenancy/jsonrpc"
)

func FuzzJSONRPCMetadata(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"tenant_id":"tenant-a"}`),
		[]byte(`{"tenant_id":"tenant-a","tenant_id":"tenant-b"}`),
		[]byte(`{"tenant_id":42}`),
		[]byte(`{`),
		{},
	} {
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, metadata []byte, trusted bool) {
		codec, err := tenancyjsonrpc.New(tenancyjsonrpc.Options{
			Trust: func(context.Context) bool { return trusted },
		})
		if err != nil {
			t.Fatal(err)
		}
		scope, extractErr := codec.Extract(context.Background(), metadata)
		if extractErr == nil && (!trusted || !scope.Valid() || scope.Kind() != tenancy.ScopeTenant) {
			t.Fatalf("accepted hostile metadata: trusted=%t scope=%#v", trusted, scope)
		}
	})
}
