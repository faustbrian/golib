package tenancy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestPropagationCodecInjectsAndAcceptsTrustedTenantScope(t *testing.T) {
	t.Parallel()

	codec, err := tenancy.NewPropagationCodec(tenancy.PropagationOptions{})
	if err != nil {
		t.Fatalf("NewPropagationCodec() error = %v", err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	carrier := tenancy.MapCarrier{}
	if err := codec.Inject(carrier, scope); err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if values := carrier.Values(tenancy.DefaultTenantField); len(values) != 1 || values[0] != "tenant-a" {
		t.Fatalf("injected values = %#v", values)
	}

	parent, cancel := context.WithCancel(context.Background())
	ctx, err := codec.Accept(parent, carrier, true)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if err := tenancy.AssertTenant(ctx, tenancy.MustTenantID("tenant-a")); err != nil {
		t.Fatalf("accepted scope error = %v", err)
	}
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("Accept() dropped parent cancellation")
	}
}

func TestPropagationCodecRejectsAmbiguousSpoofedAndMalformedMetadata(t *testing.T) {
	t.Parallel()

	codec, _ := tenancy.NewPropagationCodec(tenancy.PropagationOptions{})
	tests := map[string]struct {
		carrier tenancy.MapCarrier
		trusted bool
		want    error
	}{
		"missing": {tenancy.MapCarrier{}, true, tenancy.ErrTenantMetadataMissing},
		"duplicate": {
			tenancy.MapCarrier{tenancy.DefaultTenantField: []string{"tenant-a", "tenant-a"}},
			true, tenancy.ErrTenantMetadataDuplicate,
		},
		"conflicting": {
			tenancy.MapCarrier{tenancy.DefaultTenantField: []string{"tenant-a", "tenant-b"}},
			true, tenancy.ErrTenantMetadataConflicting,
		},
		"spoofed": {
			tenancy.MapCarrier{tenancy.DefaultTenantField: []string{"tenant-a"}},
			false, tenancy.ErrTenantMetadataUntrusted,
		},
		"malformed": {
			tenancy.MapCarrier{tenancy.DefaultTenantField: []string{"bad tenant"}},
			true, tenancy.ErrInvalidTenantID,
		},
		"oversized": {
			tenancy.MapCarrier{tenancy.DefaultTenantField: []string{
				string(make([]byte, tenancy.MaxTenantIDBytes+1)),
			}},
			true, tenancy.ErrInvalidTenantID,
		},
		"too many": {
			tenancy.MapCarrier{tenancy.DefaultTenantField: []string{
				"tenant-a", "tenant-a", "tenant-a", "tenant-a", "tenant-a",
				"tenant-a", "tenant-a", "tenant-a", "tenant-a",
			}},
			true, tenancy.ErrTenantMetadataOversized,
		},
		"maximum duplicates": {
			tenancy.MapCarrier{tenancy.DefaultTenantField: []string{
				"tenant-a", "tenant-a", "tenant-a", "tenant-a",
				"tenant-a", "tenant-a", "tenant-a", "tenant-a",
			}},
			true, tenancy.ErrTenantMetadataDuplicate,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := codec.Extract(test.carrier, test.trusted); !errors.Is(err, test.want) {
				t.Fatalf("Extract() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPropagationCodecRefusesOverwriteSystemScopeAndContextConflict(t *testing.T) {
	t.Parallel()

	codec, _ := tenancy.NewPropagationCodec(tenancy.PropagationOptions{})
	tenantA, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	tenantB, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-b"), tenancy.Metadata{})
	carrier := tenancy.MapCarrier{tenancy.DefaultTenantField: {"preexisting"}}
	if err := codec.Inject(carrier, tenantA); !errors.Is(err, tenancy.ErrTenantMetadataOverwrite) {
		t.Fatalf("Inject(overwrite) error = %v", err)
	}
	reason, _ := tenancy.NewAdministrativeReason("operator", "maintenance", "")
	system, _ := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if err := codec.Inject(tenancy.MapCarrier{}, system); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("Inject(system) error = %v", err)
	}

	ctxA, _ := tenancy.WithScope(context.Background(), tenantA)
	carrierB := tenancy.MapCarrier{}
	_ = codec.Inject(carrierB, tenantB)
	if _, err := codec.Accept(ctxA, carrierB, true); !errors.Is(err, tenancy.ErrConflictingScope) {
		t.Fatalf("Accept(conflict) error = %v", err)
	}
	if err := codec.InjectFromContext(tenancy.MapCarrier{}, context.Background()); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("InjectFromContext(missing) error = %v", err)
	}
	systemContext, _ := tenancy.WithScope(context.Background(), system)
	if err := codec.InjectFromContext(tenancy.MapCarrier{}, systemContext); !errors.Is(err, tenancy.ErrTenantScopeRequired) {
		t.Fatalf("InjectFromContext(system) error = %v", err)
	}
}

func TestExplicitRunInstallsScopeWithoutLosingCallerContext(t *testing.T) {
	t.Parallel()

	type key struct{}
	parent := context.WithValue(context.Background(), key{}, "value")
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	called := false
	err := tenancy.RunScoped(parent, scope, func(ctx context.Context) error {
		called = true
		if ctx.Value(key{}) != "value" {
			t.Fatal("RunScoped() dropped parent value")
		}
		return tenancy.AssertTenant(ctx, tenancy.MustTenantID("tenant-a"))
	})
	if err != nil || !called {
		t.Fatalf("RunScoped() = %v, called %t", err, called)
	}
	if err := tenancy.RunScoped(parent, scope, nil); !errors.Is(err, tenancy.ErrInvalidOperation) {
		t.Fatalf("RunScoped(nil) error = %v", err)
	}
}
