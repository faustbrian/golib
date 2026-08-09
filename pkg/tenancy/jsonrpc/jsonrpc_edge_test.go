package tenancyjsonrpc_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
	tenantjsonrpc "github.com/faustbrian/golib/pkg/tenancy/jsonrpc"
)

func TestJSONRPCRejectsEveryStructuralAmbiguity(t *testing.T) {
	t.Parallel()

	codec, _ := tenantjsonrpc.New(tenantjsonrpc.Options{Trust: func(context.Context) bool { return true }})
	metadata := []string{
		`[]`,
		`{`,
		`{"`,
		`{"trace":`,
		`{"trace":1,"trace":2,"tenant_id":"tenant-a"}`,
		`{"tenant_id":"tenant-a"`,
	}
	for _, value := range metadata {
		if _, err := codec.Extract(context.Background(), []byte(value)); !errors.Is(err, tenantjsonrpc.ErrInvalidMetadata) {
			t.Fatalf("Extract(%q) error = %v", value, err)
		}
	}
}

func TestJSONRPCInjectionAndAcceptanceFailClosed(t *testing.T) {
	t.Parallel()

	if _, err := tenantjsonrpc.New(tenantjsonrpc.Options{Field: "bad field", Trust: func(context.Context) bool { return true }}); !errors.Is(err, tenantjsonrpc.ErrInvalidOptions) {
		t.Fatalf("New(bad field) error = %v", err)
	}
	if _, err := tenantjsonrpc.New(tenantjsonrpc.Options{MaxMetadataBytes: 1, Trust: func(context.Context) bool { return true }}); !errors.Is(err, tenantjsonrpc.ErrInvalidOptions) {
		t.Fatalf("New(short limit) error = %v", err)
	}
	codec, _ := tenantjsonrpc.New(tenantjsonrpc.Options{Trust: func(context.Context) bool { return true }})
	if _, err := codec.Accept(context.Background(), []byte(`{}`)); !errors.Is(err, tenancy.ErrTenantMetadataMissing) {
		t.Fatalf("Accept(missing) error = %v", err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	if _, err := codec.Inject([]byte(`[]`), scope); !errors.Is(err, tenantjsonrpc.ErrInvalidMetadata) {
		t.Fatalf("Inject(invalid metadata) error = %v", err)
	}
	reason, _ := tenancy.NewAdministrativeReason("operator", "maintenance", "")
	system, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if _, err := codec.Inject([]byte(`{}`), system); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("Inject(system) error = %v", err)
	}
	var nilCodec *tenantjsonrpc.Codec
	if _, err := nilCodec.Inject([]byte(`{}`), scope); !errors.Is(err, tenantjsonrpc.ErrInvalidOptions) {
		t.Fatalf("nil Inject() error = %v", err)
	}
}

func TestJSONRPCAcceptsExactMetadataBounds(t *testing.T) {
	t.Parallel()

	trust := func(context.Context) bool { return true }
	for _, maximum := range []int{2, tenantjsonrpc.MaximumMetadataBytes} {
		if _, err := tenantjsonrpc.New(tenantjsonrpc.Options{MaxMetadataBytes: maximum, Trust: trust}); err != nil {
			t.Fatalf("New(maximum %d) error = %v", maximum, err)
		}
	}

	const prefix = `{"tenant_id":"tenant-a","padding":"`
	const suffix = `"}`
	metadata := []byte(prefix + strings.Repeat("x", tenantjsonrpc.MaximumMetadataBytes-len(prefix)-len(suffix)) + suffix)
	codec, _ := tenantjsonrpc.New(tenantjsonrpc.Options{MaxMetadataBytes: len(metadata), Trust: trust})
	scope, err := codec.Extract(context.Background(), metadata)
	if err != nil || !scope.TenantID().Equal(tenancy.MustTenantID("tenant-a")) {
		t.Fatalf("Extract(exact maximum) = %#v, %v", scope, err)
	}
}
