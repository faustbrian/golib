package tenancyjsonrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenantjsonrpc "github.com/faustbrian/golib/pkg/tenancy/jsonrpc"
)

func TestJSONRPCExtractAndAcceptRequireExplicitTrust(t *testing.T) {
	t.Parallel()

	type key struct{}
	trustedContext := context.WithValue(context.Background(), key{}, true)
	codec, err := tenantjsonrpc.New(tenantjsonrpc.Options{
		Trust: func(ctx context.Context) bool {
			trusted, _ := ctx.Value(key{}).(bool)
			return trusted
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	scope, err := codec.Extract(trustedContext, []byte(`{"tenant_id":"tenant-a","trace":"safe"}`))
	if err != nil || !scope.TenantID().Equal(tenancy.MustTenantID("tenant-a")) {
		t.Fatalf("Extract() = %#v, %v", scope, err)
	}
	accepted, err := codec.Accept(trustedContext, []byte(`{"tenant_id":"tenant-a"}`))
	if err != nil || accepted.Value(key{}) != true {
		t.Fatalf("Accept() = %v, value %v", err, accepted.Value(key{}))
	}
	if err := tenancy.AssertTenant(accepted, tenancy.MustTenantID("tenant-a")); err != nil {
		t.Fatalf("accepted tenant error = %v", err)
	}
	if _, err := codec.Extract(context.Background(), []byte(`{"tenant_id":"tenant-a"}`)); !errors.Is(err, tenancy.ErrTenantMetadataUntrusted) {
		t.Fatalf("Extract(untrusted) error = %v", err)
	}
}

func TestJSONRPCRejectsDuplicateConflictingMalformedAndOversizedMetadata(t *testing.T) {
	t.Parallel()

	codec, _ := tenantjsonrpc.New(tenantjsonrpc.Options{Trust: func(context.Context) bool { return true }})
	tests := map[string]struct {
		metadata string
		want     error
	}{
		"missing":     {`{"trace":"safe"}`, tenancy.ErrTenantMetadataMissing},
		"duplicate":   {`{"tenant_id":"tenant-a","tenant_id":"tenant-a"}`, tenancy.ErrTenantMetadataDuplicate},
		"conflicting": {`{"tenant_id":"tenant-a","tenant_id":"tenant-b"}`, tenancy.ErrTenantMetadataConflicting},
		"wrong type":  {`{"tenant_id":42}`, tenantjsonrpc.ErrInvalidMetadata},
		"malformed":   {`{"tenant_id":`, tenantjsonrpc.ErrInvalidMetadata},
		"trailing":    {`{"tenant_id":"tenant-a"} []`, tenantjsonrpc.ErrInvalidMetadata},
		"bad tenant":  {`{"tenant_id":"bad tenant"}`, tenancy.ErrInvalidTenantID},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := codec.Extract(context.Background(), []byte(test.metadata)); !errors.Is(err, test.want) {
				t.Fatalf("Extract() error = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := codec.Extract(context.Background(), []byte(strings.Repeat(" ", tenantjsonrpc.DefaultMaxMetadataBytes+1))); !errors.Is(err, tenantjsonrpc.ErrOversizedMetadata) {
		t.Fatalf("Extract(oversized) error = %v", err)
	}
}

func TestJSONRPCInjectionPreservesMetadataAndRefusesOverwrite(t *testing.T) {
	t.Parallel()

	codec, _ := tenantjsonrpc.New(tenantjsonrpc.Options{Trust: func(context.Context) bool { return true }})
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	encoded, err := codec.Inject([]byte(`{"trace":"safe"}`), scope)
	if err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if values["tenant_id"] != "tenant-a" || values["trace"] != "safe" {
		t.Fatalf("injected metadata = %#v", values)
	}
	if _, err := codec.Inject(encoded, scope); !errors.Is(err, tenancy.ErrTenantMetadataOverwrite) {
		t.Fatalf("Inject(overwrite) error = %v", err)
	}
}

func TestJSONRPCValidatesOptionsAndContext(t *testing.T) {
	t.Parallel()

	if _, err := tenantjsonrpc.New(tenantjsonrpc.Options{}); !errors.Is(err, tenantjsonrpc.ErrInvalidOptions) {
		t.Fatalf("New(no trust) error = %v", err)
	}
	if _, err := tenantjsonrpc.New(tenantjsonrpc.Options{MaxMetadataBytes: tenantjsonrpc.MaximumMetadataBytes + 1, Trust: func(context.Context) bool { return true }}); !errors.Is(err, tenantjsonrpc.ErrInvalidOptions) {
		t.Fatalf("New(oversized limit) error = %v", err)
	}
	codec, _ := tenantjsonrpc.New(tenantjsonrpc.Options{Trust: func(context.Context) bool { return true }})
	if _, err := codec.Extract(nil, []byte(`{"tenant_id":"tenant-a"}`)); !errors.Is(err, tenantjsonrpc.ErrInvalidContext) {
		t.Fatalf("Extract(nil context) error = %v", err)
	}
	var nilCodec *tenantjsonrpc.Codec
	if _, err := nilCodec.Extract(context.Background(), []byte(`{}`)); !errors.Is(err, tenantjsonrpc.ErrInvalidOptions) {
		t.Fatalf("nil Extract() error = %v", err)
	}
}
