package tenancy_test

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"testing/quick"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestPropertyTenantNamespacesNeverAlias(t *testing.T) {
	t.Parallel()
	encoder, err := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	property := func(left, right uint64, logical uint32) bool {
		if left == right {
			right++
		}
		leftScope, _ := tenancy.NewTenantScope(tenancy.MustTenantID(fmt.Sprintf("t-%x", left)), tenancy.Metadata{})
		rightScope, _ := tenancy.NewTenantScope(tenancy.MustTenantID(fmt.Sprintf("t-%x", right)), tenancy.Metadata{})
		key := fmt.Sprintf("object-%x", logical)
		leftKey, leftErr := encoder.Encode(leftScope, tenancy.NamespaceCache, key)
		rightKey, rightErr := encoder.Encode(rightScope, tenancy.NamespaceCache, key)
		return leftErr == nil && rightErr == nil && leftKey != rightKey
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 10_000}); err != nil {
		t.Fatal(err)
	}
}

func TestPropertyConcurrentOperationsCannotObserveAnotherTenant(t *testing.T) {
	t.Parallel()
	encoder, _ := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	tenants := []tenancy.Scope{
		mustTenantScope(t, "tenant-a"),
		mustTenantScope(t, "tenant-b"),
		mustTenantScope(t, "tenant-c"),
	}
	store := make(map[string]string)
	var mutex sync.RWMutex
	var wait sync.WaitGroup
	for tenantIndex, scope := range tenants {
		tenantIndex, scope := tenantIndex, scope
		wait.Add(1)
		go func() {
			defer wait.Done()
			random := rand.New(rand.NewSource(int64(tenantIndex + 1)))
			for operation := range 2_000 {
				logical := fmt.Sprintf("record-%d", random.Intn(64))
				key, err := encoder.Encode(scope, tenancy.NamespaceCache, logical)
				if err != nil {
					t.Errorf("Encode() error = %v", err)
					return
				}
				value := fmt.Sprintf("tenant-%d-operation-%d", tenantIndex, operation)
				mutex.Lock()
				store[key] = value
				mutex.Unlock()
				mutex.RLock()
				observed := store[key]
				mutex.RUnlock()
				if len(observed) < 8 || observed[:8] != fmt.Sprintf("tenant-%d", tenantIndex) {
					t.Errorf("tenant %d observed %q", tenantIndex, observed)
					return
				}
			}
		}()
	}
	wait.Wait()
}

func mustTenantScope(t *testing.T, value string) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.NewTenantScope(tenancy.MustTenantID(value), tenancy.Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func BenchmarkContextScope(b *testing.B) {
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	parent := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		ctx, err := tenancy.WithScope(parent, scope)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := tenancy.RequireTenant(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNamespaceEncoding(b *testing.B) {
	encoder, _ := tenancy.NewNamespaceEncoder([]byte("0123456789abcdef0123456789abcdef"))
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := encoder.Encode(scope, tenancy.NamespaceCache, "orders/42"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTenantAssertion(b *testing.B) {
	id := tenancy.MustTenantID("tenant-a")
	scope, _ := tenancy.NewTenantScope(id, tenancy.Metadata{})
	ctx, _ := tenancy.WithScope(context.Background(), scope)
	b.ReportAllocs()
	for b.Loop() {
		if err := tenancy.AssertTenant(ctx, id); err != nil {
			b.Fatal(err)
		}
	}
}
