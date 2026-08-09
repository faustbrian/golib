package tenancy_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestPropagationDefensiveInputsAndCustomField(t *testing.T) {
	t.Parallel()

	if _, err := tenancy.NewPropagationCodec(tenancy.PropagationOptions{Field: "bad field"}); !errors.Is(err, tenancy.ErrInvalidPropagation) {
		t.Fatalf("NewPropagationCodec() error = %v", err)
	}
	codec, _ := tenancy.NewPropagationCodec(tenancy.PropagationOptions{Field: "x_tenant"})
	var nilCarrier tenancy.MapCarrier
	if _, err := codec.Extract(nilCarrier, true); !errors.Is(err, tenancy.ErrInvalidPropagation) {
		t.Fatalf("Extract(nil carrier) error = %v", err)
	}
	if err := codec.Inject(nilCarrier, tenancy.Scope{}); !errors.Is(err, tenancy.ErrInvalidPropagation) {
		t.Fatalf("Inject(nil carrier) error = %v", err)
	}
	var nilCodec *tenancy.PropagationCodec
	if _, err := nilCodec.Extract(tenancy.MapCarrier{}, true); !errors.Is(err, tenancy.ErrInvalidPropagation) {
		t.Fatalf("nil Extract() error = %v", err)
	}
	if err := nilCodec.Inject(tenancy.MapCarrier{}, tenancy.Scope{}); !errors.Is(err, tenancy.ErrInvalidPropagation) {
		t.Fatalf("nil Inject() error = %v", err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	carrier := &structCarrier{values: map[string][]string{}}
	if err := codec.Inject(carrier, scope); err != nil {
		t.Fatalf("Inject(custom carrier) error = %v", err)
	}
	if carrier.values["x_tenant"][0] != "tenant-a" {
		t.Fatalf("custom carrier = %#v", carrier.values)
	}
	if _, err := codec.Extract(valueCarrier{}, true); !errors.Is(err, tenancy.ErrTenantMetadataMissing) {
		t.Fatalf("Extract(value carrier) error = %v", err)
	}
	if err := tenancy.RunScoped(nil, scope, func(context.Context) error { return nil }); !errors.Is(err, tenancy.ErrInvalidContext) {
		t.Fatalf("RunScoped(nil context) error = %v", err)
	}
}

func TestIntegrationDefensivePaths(t *testing.T) {
	t.Parallel()

	if _, err := tenancy.NewIntegration(tenancy.BoundaryQueue, tenancy.PropagationOptions{Field: "bad field"}); !errors.Is(err, tenancy.ErrInvalidIntegration) {
		t.Fatalf("NewIntegration(bad field) error = %v", err)
	}
	integration, _ := tenancy.NewIntegration(tenancy.BoundaryQueue, tenancy.PropagationOptions{})
	if integration.Boundary() != tenancy.BoundaryQueue {
		t.Fatalf("Boundary() = %q", integration.Boundary())
	}
	var nilIntegration *tenancy.Integration
	if nilIntegration.Boundary() != "" {
		t.Fatalf("nil Boundary() = %q", nilIntegration.Boundary())
	}
	if _, err := nilIntegration.Receive(context.Background(), tenancy.MapCarrier{}, true); !errors.Is(err, tenancy.ErrInvalidIntegration) {
		t.Fatalf("nil Receive() error = %v", err)
	}
	encoder, _ := tenancy.NewNamespaceEncoder(make([]byte, 32))
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	if _, err := integration.Key(encoder, scope, ""); !errors.Is(err, tenancy.ErrInvalidNamespaceInput) {
		t.Fatalf("Key(empty) error = %v", err)
	}
	if _, err := nilIntegration.Key(encoder, scope, "key"); !errors.Is(err, tenancy.ErrInvalidIntegration) {
		t.Fatalf("nil Key() error = %v", err)
	}
	if _, err := integration.Key(nil, scope, "key"); !errors.Is(err, tenancy.ErrInvalidNamespaceInput) {
		t.Fatalf("Key(nil encoder) error = %v", err)
	}
}

func TestAdministrativeIterationRejectsInvalidPagesAndSourceFailures(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	audit := func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return nil }
	options := tenancy.IterationOptions{PageSize: 2, MaxTenants: 2, Audit: audit}
	want := errors.New("source failed")
	if _, err := tenancy.IterateTenants(context.Background(), system, pagerFunc(func(context.Context, string, int) (tenancy.TenantPage, error) {
		return tenancy.TenantPage{}, want
	}), options, func(context.Context, tenancy.TenantID) error { return nil }); !errors.Is(err, want) {
		t.Fatalf("source error = %v", err)
	}
	pages := []tenancy.TenantPage{
		{Tenants: tenantIDs("tenant-a", "tenant-b", "tenant-c")},
		{Tenants: make([]tenancy.TenantID, 1)},
		{Tenants: tenantIDs("tenant-a", "tenant-a")},
		{NextCursor: strings.Repeat("x", 257)},
	}
	for index, page := range pages {
		page := page
		_, err := tenancy.IterateTenants(context.Background(), system, pagerFunc(func(context.Context, string, int) (tenancy.TenantPage, error) {
			return page, nil
		}), options, func(context.Context, tenancy.TenantID) error { return nil })
		if !errors.Is(err, tenancy.ErrInvalidIteration) {
			t.Fatalf("invalid page %d error = %v", index, err)
		}
	}
	tenantScope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	scoped, _ := tenancy.WithScope(context.Background(), tenantScope)
	if _, err := tenancy.IterateTenants(scoped, system, pagerFunc(func(context.Context, string, int) (tenancy.TenantPage, error) {
		return tenancy.TenantPage{}, nil
	}), options, func(context.Context, tenancy.TenantID) error { return nil }); !errors.Is(err, tenancy.ErrInvalidIteration) {
		t.Fatalf("scoped context error = %v", err)
	}
}

func TestAdministrativeIterationResumesAtNextPageAndRejectsBadOffset(t *testing.T) {
	t.Parallel()

	system := systemScope(t)
	audit := func(context.Context, tenancy.AdministrativeReason, tenancy.TenantID) error { return nil }
	source := &tenantPagerStub{pages: map[string]tenancy.TenantPage{
		"":     {Tenants: tenantIDs("tenant-a"), NextCursor: "next"},
		"next": {Tenants: tenantIDs("tenant-b")},
	}}
	result, err := tenancy.IterateTenants(context.Background(), system, source, tenancy.IterationOptions{
		PageSize: 1, MaxTenants: 1, Audit: audit,
	}, func(context.Context, tenancy.TenantID) error { return nil })
	if err != nil || result.Resume.Cursor != "next" || result.Resume.Offset != 0 {
		t.Fatalf("next-page result = %#v, %v", result, err)
	}
	if _, err := tenancy.IterateTenants(context.Background(), system, source, tenancy.IterationOptions{
		PageSize: 1, MaxTenants: 1, Resume: tenancy.ResumeToken{Offset: 2}, Audit: audit,
	}, func(context.Context, tenancy.TenantID) error { return nil }); !errors.Is(err, tenancy.ErrInvalidIteration) {
		t.Fatalf("bad offset error = %v", err)
	}
}

func TestGroupRaceBoundariesAndWaitCancellation(t *testing.T) {
	t.Parallel()

	if _, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1025}); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("NewGroup(large) error = %v", err)
	}
	var nilGroup *tenancy.Group
	if err := nilGroup.Submit(context.Background(), tenancy.Scope{}, func(context.Context) error { return nil }); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("nil Submit() error = %v", err)
	}
	group, _ := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1})
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	if err := group.Submit(nil, scope, func(context.Context) error { return nil }); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("Submit(nil context) error = %v", err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	_ = group.Submit(context.Background(), scope, func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	<-started
	timed, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := group.Close(timed); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close(timeout) error = %v", err)
	}
	close(release)
	if err := group.Close(context.Background()); err != nil {
		t.Fatalf("Close(after release) error = %v", err)
	}
	if err := group.Shutdown(nil); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("Shutdown(nil) error = %v", err)
	}

	parent, cancelParent := context.WithCancel(context.Background())
	cancelledGroup, _ := tenancy.NewGroup(parent, tenancy.GroupOptions{MaxConcurrent: 1})
	blockedRelease := make(chan struct{})
	_ = cancelledGroup.Submit(context.Background(), scope, func(context.Context) error {
		<-blockedRelease
		return nil
	})
	reached := make(chan struct{})
	submitResult := make(chan error, 1)
	go func() {
		submitResult <- cancelledGroup.Submit(&doneSignalingContext{Context: context.Background(), reached: reached}, scope, func(context.Context) error { return nil })
	}()
	<-reached
	cancelParent()
	if err := <-submitResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("Submit(cancelled group) error = %v", err)
	}
	close(blockedRelease)
	_ = cancelledGroup.Close(context.Background())

	raceGroup, _ := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1})
	raceRelease := make(chan struct{})
	_ = raceGroup.Submit(context.Background(), scope, func(context.Context) error {
		<-raceRelease
		return nil
	})
	raceReached := make(chan struct{})
	raceResult := make(chan error, 1)
	go func() {
		raceResult <- raceGroup.Submit(&doneSignalingContext{Context: context.Background(), reached: raceReached}, scope, func(context.Context) error { return nil })
	}()
	<-raceReached
	closeContext, closeCancel := context.WithCancel(context.Background())
	closeCancel()
	_ = raceGroup.Close(closeContext)
	close(raceRelease)
	if err := <-raceResult; !errors.Is(err, tenancy.ErrGroupClosed) {
		t.Fatalf("Submit(close race) error = %v", err)
	}
	_ = raceGroup.Close(context.Background())
}

type structCarrier struct {
	values map[string][]string
}

func (carrier *structCarrier) Values(field string) []string { return carrier.values[field] }
func (carrier *structCarrier) Set(field, value string)      { carrier.values[field] = []string{value} }

type valueCarrier struct{}

func (valueCarrier) Values(string) []string { return nil }
func (valueCarrier) Set(string, string)     {}

type doneSignalingContext struct {
	context.Context
	reached chan struct{}
	once    sync.Once
}

func (ctx *doneSignalingContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.reached) })
	return ctx.Context.Done()
}

type pagerFunc func(context.Context, string, int) (tenancy.TenantPage, error)

func (pager pagerFunc) ListTenants(ctx context.Context, cursor string, limit int) (tenancy.TenantPage, error) {
	return pager(ctx, cursor, limit)
}
