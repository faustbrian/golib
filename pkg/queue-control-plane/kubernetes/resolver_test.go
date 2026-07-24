package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

func TestStaticTenantResolverProvidesConfiguredWorkloadBoundary(t *testing.T) {
	t.Parallel()

	adapter := &tenantAdapterStub{page: Page{Items: []Status{{Name: "billing-workers"}}}}
	configured := map[string]TenantAdapter{"tenant-1": adapter}
	resolver, err := NewStaticTenantResolver(configured)
	if err != nil {
		t.Fatalf("NewStaticTenantResolver() error = %v", err)
	}
	delete(configured, "tenant-1")

	scaler, err := resolver.ResolveTenant(context.Background(), "tenant-1")
	if err != nil || scaler != adapter {
		t.Fatalf("ResolveTenant() = (%v, %v)", scaler, err)
	}
	page, err := resolver.ListTenantWorkloads(context.Background(), "tenant-1", 25, "current-page")
	if err != nil || len(page.Items) != 1 || adapter.limit != 25 || adapter.continueToken != "current-page" {
		t.Fatalf("ListTenantWorkloads() = (%#v, %v)", page, err)
	}
	if _, err := resolver.ResolveTenant(context.Background(), "tenant-2"); !errors.Is(err, ErrTenantNotConfigured) {
		t.Fatalf("ResolveTenant(unknown) error = %v", err)
	}
}

func TestStaticTenantResolverRejectsInvalidMappings(t *testing.T) {
	t.Parallel()

	var typedNil *Adapter
	for _, configured := range []map[string]TenantAdapter{
		nil,
		{"": &tenantAdapterStub{}},
		{strings.Repeat("x", controlplane.MaxIdentityBytes+1): &tenantAdapterStub{}},
		{"tenant-1": nil},
		{"tenant-1": typedNil},
	} {
		resolver, err := NewStaticTenantResolver(configured)
		if resolver != nil || !errors.Is(err, ErrInvalidResolver) {
			t.Fatalf("NewStaticTenantResolver() = (%v, %v)", resolver, err)
		}
	}
}

func TestStaticTenantResolverAcceptsValueAdapter(t *testing.T) {
	t.Parallel()

	resolver, err := NewStaticTenantResolver(map[string]TenantAdapter{
		"tenant-1": valueTenantAdapter{},
	})
	if err != nil || resolver == nil {
		t.Fatalf("NewStaticTenantResolver() = (%v, %v)", resolver, err)
	}
}

func TestStaticTenantResolverRejectsUnknownTenantVisibility(t *testing.T) {
	t.Parallel()

	resolver, err := NewStaticTenantResolver(map[string]TenantAdapter{
		"tenant-1": &tenantAdapterStub{},
	})
	if err != nil {
		t.Fatalf("NewStaticTenantResolver() error = %v", err)
	}
	if _, err := resolver.ListTenantWorkloads(context.Background(), "tenant-2", 1, ""); !errors.Is(err, ErrTenantNotConfigured) {
		t.Fatalf("ListTenantWorkloads() error = %v", err)
	}
}

type tenantAdapterStub struct {
	scalerStub
	page          Page
	limit         int64
	continueToken string
}

type valueTenantAdapter struct{}

func (valueTenantAdapter) List(context.Context, int64, string) (Page, error) {
	return Page{}, nil
}

func (valueTenantAdapter) Scale(context.Context, string, uint32) (ScaleResult, error) {
	return ScaleResult{}, nil
}

func (adapter *tenantAdapterStub) List(_ context.Context, limit int64, continueToken string) (Page, error) {
	adapter.limit = limit
	adapter.continueToken = continueToken

	return adapter.page, adapter.err
}
