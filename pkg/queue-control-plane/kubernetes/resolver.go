package kubernetes

import (
	"context"
	"errors"
	"reflect"
	"strings"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

var (
	// ErrInvalidResolver reports an empty or malformed tenant mapping.
	ErrInvalidResolver = errors.New("kubernetes: invalid tenant resolver")
	// ErrTenantNotConfigured reports a tenant without Kubernetes visibility.
	ErrTenantNotConfigured = errors.New("kubernetes: tenant not configured")
)

// TenantAdapter is the complete bounded Kubernetes surface for one tenant.
type TenantAdapter interface {
	Scaler
	List(context.Context, int64, string) (Page, error)
}

// StaticTenantResolver stores an immutable tenant-to-namespace mapping.
type StaticTenantResolver struct {
	adapters map[string]TenantAdapter
}

// NewStaticTenantResolver copies a fixed tenant adapter mapping.
func NewStaticTenantResolver(configured map[string]TenantAdapter) (*StaticTenantResolver, error) {
	if len(configured) == 0 {
		return nil, ErrInvalidResolver
	}

	adapters := make(map[string]TenantAdapter, len(configured))
	for tenant, adapter := range configured {
		if invalidTenant(tenant) || nilInterface(adapter) {
			return nil, ErrInvalidResolver
		}
		adapters[tenant] = adapter
	}

	return &StaticTenantResolver{adapters: adapters}, nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// ResolveTenant returns the preconfigured scale-only tenant boundary.
func (resolver *StaticTenantResolver) ResolveTenant(_ context.Context, tenant string) (Scaler, error) {
	return resolver.resolve(tenant)
}

// ListTenantWorkloads returns a bounded page through the preconfigured tenant
// boundary.
func (resolver *StaticTenantResolver) ListTenantWorkloads(
	ctx context.Context,
	tenant string,
	limit int64,
	continueToken string,
) (Page, error) {
	adapter, err := resolver.resolve(tenant)
	if err != nil {
		return Page{}, err
	}

	return adapter.List(ctx, limit, continueToken)
}

func (resolver *StaticTenantResolver) resolve(tenant string) (TenantAdapter, error) {
	adapter, exists := resolver.adapters[tenant]
	if !exists {
		return nil, ErrTenantNotConfigured
	}

	return adapter, nil
}

func invalidTenant(tenant string) bool {
	return strings.TrimSpace(tenant) == "" || len(tenant) > controlplane.MaxIdentityBytes
}
