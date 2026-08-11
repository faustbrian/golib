package searchtest

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

var errConformanceFixture = errors.New("conformance fixture failure")

type conformanceFaultAdapter struct {
	base         *Fake
	capabilities func(context.Context) (search.Capabilities, error)
	write        func(context.Context, search.WriteOperation, search.RefreshPolicy) (search.ItemOutcome, error)
	bulk         func(context.Context, search.BulkRequest) (search.BulkResult, error)
	search       func(context.Context, search.Request) (search.Result, error)
}

func (adapter *conformanceFaultAdapter) Capabilities(ctx context.Context) (search.Capabilities, error) {
	if adapter.capabilities != nil {
		return adapter.capabilities(ctx)
	}
	return adapter.base.Capabilities(ctx)
}

func (adapter *conformanceFaultAdapter) Write(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
	if adapter.write != nil {
		return adapter.write(ctx, operation, refresh)
	}
	return adapter.base.Write(ctx, operation, refresh)
}

func (adapter *conformanceFaultAdapter) Bulk(ctx context.Context, request search.BulkRequest) (search.BulkResult, error) {
	if adapter.bulk != nil {
		return adapter.bulk(ctx, request)
	}
	return adapter.base.Bulk(ctx, request)
}

func (adapter *conformanceFaultAdapter) Search(ctx context.Context, request search.Request) (search.Result, error) {
	if adapter.search != nil {
		return adapter.search(ctx, request)
	}
	return adapter.base.Search(ctx, request)
}

func newConformanceFaultAdapter(t *testing.T) *conformanceFaultAdapter {
	t.Helper()
	base, err := NewFake(search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return &conformanceFaultAdapter{base: base}
}

func conformanceConfig(adapter ConformanceAdapter) ConformanceConfig {
	return ConformanceConfig{
		Adapter: adapter, Limits: search.DefaultLimits(), TenantA: "tenant-a", TenantB: "tenant-b",
		LogicalIndex: "documents", Refresh: search.RefreshWaitFor,
	}
}

func TestConformanceRejectsInvalidConfigurationAndCapabilities(t *testing.T) {
	t.Parallel()

	valid := conformanceConfig(newConformanceFaultAdapter(t))
	tests := []ConformanceConfig{
		{},
		func() ConformanceConfig { value := valid; value.Limits = search.Limits{}; return value }(),
		func() ConformanceConfig { value := valid; value.TenantA = ""; return value }(),
		func() ConformanceConfig { value := valid; value.TenantB = ""; return value }(),
		func() ConformanceConfig { value := valid; value.TenantB = value.TenantA; return value }(),
		func() ConformanceConfig { value := valid; value.LogicalIndex = ""; return value }(),
		func() ConformanceConfig { value := valid; value.Refresh = search.RefreshNone; return value }(),
	}
	for _, config := range tests {
		if err := RunConformance(t.Context(), config); err == nil {
			t.Fatalf("RunConformance(%#v) accepted invalid configuration", config)
		}
	}

	failed := newConformanceFaultAdapter(t)
	failed.capabilities = func(context.Context) (search.Capabilities, error) {
		return search.Capabilities{}, errConformanceFixture
	}
	if err := RunConformance(t.Context(), conformanceConfig(failed)); !errors.Is(err, errConformanceFixture) {
		t.Fatalf("capability failure = %v", err)
	}

	missing := newConformanceFaultAdapter(t)
	missing.capabilities = func(context.Context) (search.Capabilities, error) {
		return search.Capabilities{}, nil
	}
	if err := RunConformance(t.Context(), conformanceConfig(missing)); err == nil {
		t.Fatal("missing shared capabilities accepted")
	}
}

func TestQueryAndTenantConformanceRejectEveryWriteAndSearchDivergence(t *testing.T) {
	t.Parallel()

	for failedWrite := 1; failedWrite <= 12; failedWrite++ {
		adapter := newConformanceFaultAdapter(t)
		writes := 0
		adapter.write = func(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
			writes++
			if writes == failedWrite {
				return search.ItemOutcome{}, errConformanceFixture
			}
			return adapter.base.Write(ctx, operation, refresh)
		}
		if err := runQueryConformance(t.Context(), conformanceConfig(adapter)); err == nil {
			t.Fatalf("query write %d failure = %v", failedWrite, err)
		}
	}
	for failedSearch := 1; failedSearch <= 12; failedSearch++ {
		adapter := newConformanceFaultAdapter(t)
		searches := 0
		adapter.search = func(ctx context.Context, request search.Request) (search.Result, error) {
			searches++
			if searches == failedSearch {
				return search.Result{}, errConformanceFixture
			}
			return adapter.base.Search(ctx, request)
		}
		if err := runQueryConformance(t.Context(), conformanceConfig(adapter)); !errors.Is(err, errConformanceFixture) {
			t.Fatalf("query search %d failure = %v", failedSearch, err)
		}
	}

	for failedWrite := 1; failedWrite <= 2; failedWrite++ {
		adapter := newConformanceFaultAdapter(t)
		writes := 0
		adapter.write = func(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
			writes++
			if writes == failedWrite {
				return search.ItemOutcome{}, errConformanceFixture
			}
			return adapter.base.Write(ctx, operation, refresh)
		}
		if err := runTenantConformance(t.Context(), conformanceConfig(adapter)); err == nil {
			t.Fatalf("tenant write %d failure = %v", failedWrite, err)
		}
	}
	for failedSearch := 1; failedSearch <= 3; failedSearch++ {
		adapter := newConformanceFaultAdapter(t)
		searches := 0
		adapter.search = func(ctx context.Context, request search.Request) (search.Result, error) {
			searches++
			if searches == failedSearch {
				return search.Result{}, errConformanceFixture
			}
			return adapter.base.Search(ctx, request)
		}
		if err := runTenantConformance(t.Context(), conformanceConfig(adapter)); !errors.Is(err, errConformanceFixture) {
			t.Fatalf("tenant search %d failure = %v", failedSearch, err)
		}
	}
}

func TestQueryConformanceExercisesMatchAllAndDescendingOrdering(t *testing.T) {
	t.Parallel()

	adapter := newConformanceFaultAdapter(t)
	adapter.search = func(ctx context.Context, request search.Request) (search.Result, error) {
		if _, matchAll := request.Query.(search.MatchAllQuery); matchAll {
			return search.NewResult(nil, search.Total{Relation: search.TotalExact}, nil, nil,
				search.Diagnostics{Backend: "fault"}, "")
		}
		if len(request.Sort) == 1 && request.Sort[0].Direction == search.Descending {
			return search.NewResult(nil, search.Total{Relation: search.TotalExact}, nil, nil,
				search.Diagnostics{Backend: "fault"}, "")
		}
		return adapter.base.Search(ctx, request)
	}
	if err := runQueryConformance(t.Context(), conformanceConfig(adapter)); err == nil {
		t.Fatal("match-all and descending-sort divergence escaped conformance")
	}
}

func TestVersionAndBulkConformanceRejectMalformedAdapterOutcomes(t *testing.T) {
	t.Parallel()

	for failedWrite := 1; failedWrite <= 3; failedWrite++ {
		adapter := newConformanceFaultAdapter(t)
		writes := 0
		adapter.write = func(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
			writes++
			if writes == failedWrite {
				return search.ItemOutcome{State: search.OutcomeFailed}, errConformanceFixture
			}
			return adapter.base.Write(ctx, operation, refresh)
		}
		if err := runVersionConformance(t.Context(), conformanceConfig(adapter)); err == nil {
			t.Fatalf("version write %d failure accepted", failedWrite)
		}
	}
	invalidVersion := conformanceConfig(newConformanceFaultAdapter(t))
	invalidVersion.Limits.MaxTenantBytes = 1
	if err := runVersionConformance(t.Context(), invalidVersion); err == nil {
		t.Fatal("invalid version fixture accepted")
	}
	invalidBulk := conformanceConfig(newConformanceFaultAdapter(t))
	invalidBulk.Limits.MaxTenantBytes = 1
	if err := runBulkConformance(t.Context(), invalidBulk); err == nil {
		t.Fatal("invalid bulk fixture accepted")
	}
	bulkWriteFailure := newConformanceFaultAdapter(t)
	bulkWriteFailure.write = func(context.Context, search.WriteOperation, search.RefreshPolicy) (search.ItemOutcome, error) {
		return search.ItemOutcome{State: search.OutcomeFailed}, errConformanceFixture
	}
	if err := runBulkConformance(t.Context(), conformanceConfig(bulkWriteFailure)); err == nil {
		t.Fatal("failed bulk setup write accepted")
	}

	bulkFailure := newConformanceFaultAdapter(t)
	bulkFailure.bulk = func(context.Context, search.BulkRequest) (search.BulkResult, error) {
		return search.BulkResult{}, errConformanceFixture
	}
	if err := runBulkConformance(t.Context(), conformanceConfig(bulkFailure)); !errors.Is(err, errConformanceFixture) {
		t.Fatalf("bulk failure = %v", err)
	}

	badShape := newConformanceFaultAdapter(t)
	badShape.bulk = func(_ context.Context, request search.BulkRequest) (search.BulkResult, error) {
		items := make([]search.ItemOutcome, len(request.Operations))
		for position, operation := range request.Operations {
			items[position] = search.ItemOutcome{
				Position: position, ID: operation.ID, Action: operation.Action,
				State: search.OutcomeApplied, Version: operation.Version,
			}
		}
		return search.NewBulkResult(items)
	}
	if err := runBulkConformance(t.Context(), conformanceConfig(badShape)); err == nil {
		t.Fatal("malformed bulk shape accepted")
	}

	badItem := newConformanceFaultAdapter(t)
	badItem.bulk = func(_ context.Context, request search.BulkRequest) (search.BulkResult, error) {
		items := []search.ItemOutcome{
			{Position: 0, ID: "wrong", Action: request.Operations[0].Action, State: search.OutcomeApplied, Version: 1},
			{Position: 1, ID: request.Operations[1].ID, Action: request.Operations[1].Action, State: search.OutcomeVersionConflict},
			{Position: 2, ID: request.Operations[2].ID, Action: request.Operations[2].Action, State: search.OutcomeNotFound},
		}
		return search.NewBulkResult(items)
	}
	if err := runBulkConformance(t.Context(), conformanceConfig(badItem)); err == nil {
		t.Fatal("misattributed bulk item accepted")
	}
}

func TestRunConformanceStopsAtEveryFailedStage(t *testing.T) {
	t.Parallel()

	query := newConformanceFaultAdapter(t)
	query.write = func(context.Context, search.WriteOperation, search.RefreshPolicy) (search.ItemOutcome, error) {
		return search.ItemOutcome{}, errConformanceFixture
	}
	if err := RunConformance(t.Context(), conformanceConfig(query)); err == nil {
		t.Fatal("query-stage failure accepted")
	}

	tenant := newConformanceFaultAdapter(t)
	tenant.write = func(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
		if operation.ID == "tenant-shared-id" {
			return search.ItemOutcome{}, errConformanceFixture
		}
		return tenant.base.Write(ctx, operation, refresh)
	}
	if err := RunConformance(t.Context(), conformanceConfig(tenant)); err == nil {
		t.Fatal("tenant-stage failure accepted")
	}

	version := newConformanceFaultAdapter(t)
	version.write = func(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
		if operation.ID == "version-delete" {
			return search.ItemOutcome{}, errConformanceFixture
		}
		return version.base.Write(ctx, operation, refresh)
	}
	if err := RunConformance(t.Context(), conformanceConfig(version)); err == nil {
		t.Fatal("version-stage failure accepted")
	}

	bulk := newConformanceFaultAdapter(t)
	bulk.bulk = func(context.Context, search.BulkRequest) (search.BulkResult, error) {
		return search.BulkResult{}, errConformanceFixture
	}
	if err := RunConformance(t.Context(), conformanceConfig(bulk)); !errors.Is(err, errConformanceFixture) {
		t.Fatalf("bulk-stage failure = %v", err)
	}
}

func TestConformanceHelpersRejectMalformedFixturesAndResults(t *testing.T) {
	t.Parallel()

	adapter := newConformanceFaultAdapter(t)
	config := conformanceConfig(adapter)
	config.Limits.MaxTenantBytes = 1
	if err := writeFixture(t.Context(), config, config.TenantA, fixtureDocument{id: "id", version: 1, source: `{}`}); err == nil {
		t.Fatal("invalid document fixture accepted")
	}

	adapter.write = func(context.Context, search.WriteOperation, search.RefreshPolicy) (search.ItemOutcome, error) {
		return search.ItemOutcome{State: search.OutcomeFailed}, nil
	}
	if err := writeFixture(t.Context(), conformanceConfig(adapter), "tenant-a", fixtureDocument{id: "id", version: 1, source: `{}`}); err == nil {
		t.Fatal("failed write outcome accepted")
	}

	adapter = newConformanceFaultAdapter(t)
	adapter.search = func(context.Context, search.Request) (search.Result, error) {
		return search.Result{}, errConformanceFixture
	}
	if err := expectIDs(t.Context(), conformanceConfig(adapter), "tenant-a", search.MatchAllQuery{}, 1, 0, nil, "failure"); !errors.Is(err, errConformanceFixture) {
		t.Fatalf("search helper failure = %v", err)
	}
	adapter.search = func(context.Context, search.Request) (search.Result, error) {
		return search.NewResult(
			[]search.Hit{{Index: "documents", ID: "wrong", Version: 1}},
			search.Total{Value: 1, Relation: search.TotalExact}, nil, nil,
			search.Diagnostics{Backend: "fault"}, "",
		)
	}
	if err := expectIDs(t.Context(), conformanceConfig(adapter), "tenant-a", search.MatchAllQuery{}, 1, 0, []string{"wanted"}, "mismatch"); err == nil {
		t.Fatal("wrong IDs accepted")
	}
}
