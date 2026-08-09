package tenancyjsonrpc_test

import (
	"context"
	"errors"
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
