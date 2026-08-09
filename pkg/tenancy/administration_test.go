package tenancy_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestAdministrativeIterationIsBoundedAuditedAndTenantIsolated(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	source := &tenantPagerStub{pages: map[string]tenancy.TenantPage{
		"":       {Tenants: tenantIDs("tenant-a", "tenant-b"), NextCursor: "page-2"},
		"page-2": {Tenants: tenantIDs("tenant-c")},
	}}
	var audited []string
	var operated []string
	result, err := tenancy.IterateTenants(context.Background(), system, source, tenancy.IterationOptions{
		PageSize:   2,
		MaxTenants: 3,
		Audit: func(ctx context.Context, reason tenancy.AdministrativeReason, tenant tenancy.TenantID) error {
			if _, err := tenancy.RequireSystem(ctx); err != nil {
				t.Fatalf("audit context error = %v", err)
			}
			if reason != system.AdministrativeReason() {
				t.Fatalf("audit reason = %#v", reason)
			}
			audited = append(audited, tenant.Value())
			return nil
		},
	}, func(ctx context.Context, tenant tenancy.TenantID) error {
		if err := tenancy.AssertTenant(ctx, tenant); err != nil {
			t.Fatalf("operation context error = %v", err)
		}
		operated = append(operated, tenant.Value())
		return nil
	})
	if err != nil {
		t.Fatalf("IterateTenants() error = %v", err)
	}
	if result.Processed != 3 || !result.Complete || result.Resume != (tenancy.ResumeToken{}) {
		t.Fatalf("result = %#v", result)
	}
	want := []string{"tenant-a", "tenant-b", "tenant-c"}
	if !reflect.DeepEqual(audited, want) || !reflect.DeepEqual(operated, want) {
		t.Fatalf("audited = %#v, operated = %#v", audited, operated)
	}
}

func TestAdministrativeIterationReturnsPreciseResumeToken(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	source := &tenantPagerStub{pages: map[string]tenancy.TenantPage{
		"": {Tenants: tenantIDs("tenant-a", "tenant-b", "tenant-c"), NextCursor: "next"},
	}}
	options := tenancy.IterationOptions{
		PageSize: 3, MaxTenants: 2,
		Audit: func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return nil },
	}
	result, err := tenancy.IterateTenants(context.Background(), system, source, options, func(context.Context, tenancy.TenantID) error { return nil })
	if err != nil {
		t.Fatalf("IterateTenants() error = %v", err)
	}
	if result.Complete || result.Processed != 2 || result.Resume != (tenancy.ResumeToken{Cursor: "", Offset: 2}) {
		t.Fatalf("bounded result = %#v", result)
	}

	options.MaxTenants = 3
	options.Resume = result.Resume
	var resumed []string
	resumedResult, err := tenancy.IterateTenants(context.Background(), system, source, options, func(_ context.Context, tenant tenancy.TenantID) error {
		resumed = append(resumed, tenant.Value())
		return nil
	})
	if err != nil || !resumedResult.Complete || !reflect.DeepEqual(resumed, []string{"tenant-c"}) {
		t.Fatalf("resumed = %#v, result %#v, error %v", resumed, resumedResult, err)
	}
}

func TestAdministrativeIterationStopsAtAuditOperationAndCancellation(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	source := &tenantPagerStub{pages: map[string]tenancy.TenantPage{
		"": {Tenants: tenantIDs("tenant-a", "tenant-b")},
	}}
	want := errors.New("audit unavailable")
	result, err := tenancy.IterateTenants(context.Background(), system, source, tenancy.IterationOptions{
		PageSize: 2, MaxTenants: 2,
		Audit: func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return want },
	}, func(context.Context, tenancy.TenantID) error { return nil })
	if !errors.Is(err, want) || result.Resume.Offset != 0 {
		t.Fatalf("audit failure = %#v, %v", result, err)
	}

	want = errors.New("operation failed")
	options := tenancy.IterationOptions{
		PageSize: 2, MaxTenants: 2,
		Audit: func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return nil },
	}
	result, err = tenancy.IterateTenants(context.Background(), system, source, options, func(context.Context, tenancy.TenantID) error { return want })
	if !errors.Is(err, want) || result.Resume.Offset != 0 {
		t.Fatalf("operation failure = %#v, %v", result, err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = tenancy.IterateTenants(cancelled, system, source, options, func(context.Context, tenancy.TenantID) error { return nil })
	if !errors.Is(err, context.Canceled) || result.Resume != (tenancy.ResumeToken{}) {
		t.Fatalf("cancelled = %#v, %v", result, err)
	}

	duringAudit, cancelDuringAudit := context.WithCancel(context.Background())
	options.Audit = func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error {
		cancelDuringAudit()
		return nil
	}
	result, err = tenancy.IterateTenants(duringAudit, system, source, options, func(context.Context, tenancy.TenantID) error { return nil })
	if !errors.Is(err, context.Canceled) || result.Resume.Offset != 0 {
		t.Fatalf("cancelled during audit = %#v, %v", result, err)
	}
}

func TestAdministrativeResumeRepeatsFailedTenantWithCompleteAttribution(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	source := &tenantPagerStub{pages: map[string]tenancy.TenantPage{
		"snapshot-v1": {Tenants: tenantIDs("tenant-a", "tenant-b")},
	}}
	var auditTenants []string
	var operationTenants []string
	audit := func(ctx context.Context, reason tenancy.AdministrativeReason, tenant tenancy.TenantID) error {
		resolved, err := tenancy.RequireSystem(ctx)
		if err != nil || !resolved.Equal(system) || reason != system.AdministrativeReason() {
			return errors.New("administrative attribution was not preserved")
		}
		auditTenants = append(auditTenants, tenant.Value())
		return nil
	}
	failure := errors.New("import checkpoint failed")
	options := tenancy.IterationOptions{
		PageSize: 2, MaxTenants: 2, Resume: tenancy.ResumeToken{Cursor: "snapshot-v1"}, Audit: audit,
	}
	result, err := tenancy.IterateTenants(context.Background(), system, source, options,
		func(ctx context.Context, tenant tenancy.TenantID) error {
			if err := tenancy.AssertTenant(ctx, tenant); err != nil {
				return err
			}
			if ctxScope, _ := tenancy.RequireScope(ctx); ctxScope.AdministrativeReason() != (tenancy.AdministrativeReason{}) {
				return errors.New("system reason promoted into tenant operation")
			}
			operationTenants = append(operationTenants, tenant.Value())
			if tenant.Equal(tenancy.MustTenantID("tenant-b")) {
				return failure
			}
			return nil
		})
	if !errors.Is(err, failure) || result.Processed != 1 ||
		result.Resume != (tenancy.ResumeToken{Cursor: "snapshot-v1", Offset: 1}) {
		t.Fatalf("failed import result = %#v, %v", result, err)
	}
	options.Resume = result.Resume
	resumed, err := tenancy.IterateTenants(context.Background(), system, source, options,
		func(ctx context.Context, tenant tenancy.TenantID) error {
			if err := tenancy.AssertTenant(ctx, tenant); err != nil {
				return err
			}
			operationTenants = append(operationTenants, tenant.Value())
			return nil
		})
	if err != nil || !resumed.Complete || resumed.Processed != 1 {
		t.Fatalf("resumed import result = %#v, %v", resumed, err)
	}
	if !reflect.DeepEqual(auditTenants, []string{"tenant-a", "tenant-b", "tenant-b"}) ||
		!reflect.DeepEqual(operationTenants, []string{"tenant-a", "tenant-b", "tenant-b"}) {
		t.Fatalf("audit = %#v, operations = %#v", auditTenants, operationTenants)
	}
}

func TestAdministrativeIterationValidatesEveryBoundary(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	source := &tenantPagerStub{pages: map[string]tenancy.TenantPage{"": {}}}
	audit := func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return nil }
	operation := func(context.Context, tenancy.TenantID) error { return nil }
	tests := map[string]struct {
		ctx       context.Context
		scope     tenancy.Scope
		source    tenancy.TenantPager
		options   tenancy.IterationOptions
		operation func(context.Context, tenancy.TenantID) error
	}{
		"context":   {nil, system, source, tenancy.IterationOptions{PageSize: 1, MaxTenants: 1, Audit: audit}, operation},
		"scope":     {context.Background(), tenancy.Scope{}, source, tenancy.IterationOptions{PageSize: 1, MaxTenants: 1, Audit: audit}, operation},
		"source":    {context.Background(), system, nil, tenancy.IterationOptions{PageSize: 1, MaxTenants: 1, Audit: audit}, operation},
		"page size": {context.Background(), system, source, tenancy.IterationOptions{MaxTenants: 1, Audit: audit}, operation},
		"maximum":   {context.Background(), system, source, tenancy.IterationOptions{PageSize: 1, Audit: audit}, operation},
		"audit":     {context.Background(), system, source, tenancy.IterationOptions{PageSize: 1, MaxTenants: 1}, operation},
		"operation": {context.Background(), system, source, tenancy.IterationOptions{PageSize: 1, MaxTenants: 1, Audit: audit}, nil},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := tenancy.IterateTenants(test.ctx, test.scope, test.source, test.options, test.operation); !errors.Is(err, tenancy.ErrInvalidIteration) {
				t.Fatalf("IterateTenants() error = %v", err)
			}
		})
	}
}

func TestAdministrativeIterationAcceptsExactResourceLimits(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	cursor := string(make([]byte, 256))
	source := &tenantPagerStub{pages: map[string]tenancy.TenantPage{
		"":     {NextCursor: cursor},
		cursor: {},
	}}
	result, err := tenancy.IterateTenants(context.Background(), system, source, tenancy.IterationOptions{
		PageSize:   1000,
		MaxTenants: 1_000_000,
		Audit: func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error {
			return nil
		},
	}, func(context.Context, tenancy.TenantID) error { return nil })
	if err != nil || !result.Complete {
		t.Fatalf("exact-limit iteration = %#v, %v", result, err)
	}
	result, err = tenancy.IterateTenants(context.Background(), system, source, tenancy.IterationOptions{
		PageSize: 1, MaxTenants: 1, Resume: tenancy.ResumeToken{Cursor: cursor},
		Audit: func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error {
			return nil
		},
	}, func(context.Context, tenancy.TenantID) error { return nil })
	if err != nil || !result.Complete {
		t.Fatalf("exact cursor resume = %#v, %v", result, err)
	}
}

func TestAdministrativeIterationRejectsCursorCycles(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	source := tenantPagerFunc(func(_ context.Context, cursor string, _ int) (tenancy.TenantPage, error) {
		switch cursor {
		case "":
			return tenancy.TenantPage{NextCursor: "a"}, nil
		case "a":
			return tenancy.TenantPage{NextCursor: "b"}, nil
		default:
			return tenancy.TenantPage{NextCursor: "a"}, nil
		}
	})

	_, err := tenancy.IterateTenants(ctx, system, source, tenancy.IterationOptions{
		PageSize: 1, MaxTenants: 1,
		Audit: func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return nil },
	}, func(context.Context, tenancy.TenantID) error { return nil })
	if !errors.Is(err, tenancy.ErrInvalidIteration) {
		t.Fatalf("IterateTenants(cursor cycle) error = %v", err)
	}
}

func TestAdministrativeIterationRejectsUnboundedDistinctPages(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	source := tenantPagerFunc(func(_ context.Context, cursor string, _ int) (tenancy.TenantPage, error) {
		calls++
		switch cursor {
		case "":
			return tenancy.TenantPage{NextCursor: "a"}, nil
		case "a":
			return tenancy.TenantPage{NextCursor: "b"}, nil
		default:
			cancel()
			return tenancy.TenantPage{NextCursor: "c"}, nil
		}
	})

	_, err := tenancy.IterateTenants(ctx, system, source, tenancy.IterationOptions{
		PageSize: 1, MaxTenants: 1,
		Audit: func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return nil },
	}, func(context.Context, tenancy.TenantID) error { return nil })
	if !errors.Is(err, tenancy.ErrInvalidIteration) || calls != 2 {
		t.Fatalf("IterateTenants(unbounded pages) calls = %d, error = %v", calls, err)
	}
}

type tenantPagerStub struct {
	pages map[string]tenancy.TenantPage
}

type tenantPagerFunc func(context.Context, string, int) (tenancy.TenantPage, error)

func (pager tenantPagerFunc) ListTenants(ctx context.Context, cursor string, size int) (tenancy.TenantPage, error) {
	return pager(ctx, cursor, size)
}

func (pager *tenantPagerStub) ListTenants(_ context.Context, cursor string, _ int) (tenancy.TenantPage, error) {
	return pager.pages[cursor], nil
}

func tenantIDs(values ...string) []tenancy.TenantID {
	ids := make([]tenancy.TenantID, len(values))
	for index, value := range values {
		ids[index] = tenancy.MustTenantID(value)
	}
	return ids
}

func systemScope(t *testing.T) tenancy.Scope {
	t.Helper()
	reason, err := tenancy.NewAdministrativeReason("operator", "maintenance", "OPS-1")
	if err != nil {
		t.Fatalf("NewAdministrativeReason() error = %v", err)
	}
	scope, err := tenancy.NewSystemScope(tenancy.NewSystemCapability(reason), tenancy.Metadata{})
	if err != nil {
		t.Fatalf("NewSystemScope() error = %v", err)
	}
	return scope
}
