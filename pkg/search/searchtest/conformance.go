package searchtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/faustbrian/golib/pkg/search"
)

// ConformanceAdapter is the shared indexing and query surface exercised by
// RunConformance. Implementations must be backed by a fresh, isolated index.
type ConformanceAdapter interface {
	search.Indexer
	search.Searcher
}

// ConformanceConfig defines one bounded deterministic shared-semantics run.
// TenantA and TenantB must resolve to isolated storage for LogicalIndex.
type ConformanceConfig struct {
	Adapter      ConformanceAdapter
	Limits       search.Limits
	TenantA      string
	TenantB      string
	LogicalIndex string
	Refresh      search.RefreshPolicy
}

// RunConformance proves only semantics declared by both the in-memory fake and
// production adapters. The caller owns backend setup, teardown, and isolation.
func RunConformance(ctx context.Context, config ConformanceConfig) error {
	if err := validateConformanceConfig(config); err != nil {
		return err
	}

	capabilities, err := config.Adapter.Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("searchtest conformance: capabilities: %w", err)
	}
	if !capabilities.Boolean || !capabilities.Term || !capabilities.Prefix ||
		!capabilities.Exists || !capabilities.Offset ||
		!capabilities.ExternalVersion || !capabilities.BulkPartialOutcomes {
		return errors.New("searchtest conformance: adapter does not declare every shared capability")
	}

	if err := runQueryConformance(ctx, config); err != nil {
		return err
	}
	if err := runTenantConformance(ctx, config); err != nil {
		return err
	}
	if err := runVersionConformance(ctx, config); err != nil {
		return err
	}
	return runBulkConformance(ctx, config)
}

func validateConformanceConfig(config ConformanceConfig) error {
	if config.Adapter == nil || config.Limits.Validate() != nil ||
		config.TenantA == "" || config.TenantB == "" ||
		config.TenantA == config.TenantB || config.LogicalIndex == "" ||
		config.Refresh != search.RefreshWaitFor && config.Refresh != search.RefreshImmediate {
		return errors.New("searchtest conformance: invalid configuration")
	}
	return nil
}

func runQueryConformance(ctx context.Context, config ConformanceConfig) error {
	documents := []fixtureDocument{
		{id: "term-exact", version: 1, source: `{"scenario":"term","keyword":"exact"}`},
		{id: "term-other", version: 1, source: `{"scenario":"term","keyword":"exact-suffix"}`},
		{id: "exists-null", version: 1, source: `{"scenario":"exists","exists_value":null}`},
		{id: "exists-empty", version: 1, source: `{"scenario":"exists","exists_value":[]}`},
		{id: "exists-null-array", version: 1, source: `{"scenario":"exists","exists_value":[null]}`},
		{id: "exists-present", version: 1, source: `{"scenario":"exists","exists_value":[null,"present"]}`},
		{id: "bool-match", version: 1, source: `{"scenario":"bool","keyword":"wanted"}`},
		{id: "bool-miss", version: 1, source: `{"scenario":"bool","keyword":"other"}`},
		{id: "page-a", version: 1, source: `{"scenario":"page"}`},
		{id: "page-b", version: 1, source: `{"scenario":"page"}`},
		{id: "page-c", version: 1, source: `{"scenario":"page"}`},
	}
	for _, fixture := range documents {
		if err := writeFixture(ctx, config, config.TenantA, fixture); err != nil {
			return err
		}
	}

	if err := expectIDs(ctx, config, config.TenantA,
		search.TermQuery{Field: "keyword", Value: search.StringValue("exact")}, 20, 0,
		[]string{"term-exact"}, "exact term"); err != nil {
		return err
	}
	if err := expectIDs(ctx, config, config.TenantA,
		search.PrefixQuery{Field: "keyword", Prefix: "exact"}, 20, 0,
		[]string{"term-exact", "term-other"}, "keyword prefix"); err != nil {
		return err
	}
	if err := expectIDs(ctx, config, config.TenantA,
		search.ExistsQuery{Field: "exists_value"}, 20, 0,
		[]string{"exists-present"}, "indexed-value exists"); err != nil {
		return err
	}
	if err := expectIDs(ctx, config, config.TenantA,
		search.BoolQuery{Should: []search.Query{
			search.TermQuery{Field: "keyword", Value: search.StringValue("wanted")},
		}, MinimumShouldMatch: 0}, 20, 0,
		[]string{"bool-match"}, "should-only bool default"); err != nil {
		return err
	}
	if err := expectIDs(ctx, config, config.TenantA,
		search.TermQuery{Field: "scenario", Value: search.StringValue("page")}, 2, 0,
		[]string{"page-a", "page-b"}, "first offset page"); err != nil {
		return err
	}
	return expectIDs(ctx, config, config.TenantA,
		search.TermQuery{Field: "scenario", Value: search.StringValue("page")}, 2, 2,
		[]string{"page-c"}, "second offset page")
}

func runTenantConformance(ctx context.Context, config ConformanceConfig) error {
	fixture := fixtureDocument{id: "tenant-shared-id", version: 1, source: `{"scenario":"tenant","keyword":"tenant-a"}`}
	if err := writeFixture(ctx, config, config.TenantA, fixture); err != nil {
		return err
	}
	fixture.source = `{"scenario":"tenant","keyword":"tenant-b"}`
	if err := writeFixture(ctx, config, config.TenantB, fixture); err != nil {
		return err
	}
	if err := expectIDs(ctx, config, config.TenantA,
		search.TermQuery{Field: "keyword", Value: search.StringValue("tenant-a")}, 10, 0,
		[]string{"tenant-shared-id"}, "tenant A own document"); err != nil {
		return err
	}
	if err := expectIDs(ctx, config, config.TenantA,
		search.TermQuery{Field: "keyword", Value: search.StringValue("tenant-b")}, 10, 0,
		nil, "tenant A isolation"); err != nil {
		return err
	}
	return expectIDs(ctx, config, config.TenantB,
		search.TermQuery{Field: "keyword", Value: search.StringValue("tenant-b")}, 10, 0,
		[]string{"tenant-shared-id"}, "tenant B own document")
}

func runVersionConformance(ctx context.Context, config ConformanceConfig) error {
	fixture := fixtureDocument{id: "version-delete", version: 1, source: `{"scenario":"version"}`}
	if err := writeFixture(ctx, config, config.TenantA, fixture); err != nil {
		return err
	}
	deleted, err := config.Adapter.Write(ctx,
		search.DeleteDocument(config.TenantA, config.LogicalIndex, fixture.id, 3), config.Refresh)
	if err != nil || deleted.State != search.OutcomeApplied {
		return fmt.Errorf("searchtest conformance: delete before stale write: outcome=%#v error=%v", deleted, err)
	}
	document, err := search.NewDocument(config.TenantA, config.LogicalIndex, fixture.id, 2,
		json.RawMessage(fixture.source), config.Limits)
	if err != nil {
		return fmt.Errorf("searchtest conformance: stale document fixture: %w", err)
	}
	stale, err := config.Adapter.Write(ctx, search.IndexDocument(document), config.Refresh)
	if err != nil || stale.State != search.OutcomeVersionConflict {
		return fmt.Errorf("searchtest conformance: stale write after delete: outcome=%#v error=%v", stale, err)
	}
	return nil
}

func runBulkConformance(ctx context.Context, config ConformanceConfig) error {
	if err := writeFixture(ctx, config, config.TenantA, fixtureDocument{
		id: "bulk-conflict", version: 2, source: `{"scenario":"bulk"}`,
	}); err != nil {
		return err
	}
	applied, err := search.NewDocument(config.TenantA, config.LogicalIndex, "bulk-applied", 1,
		json.RawMessage(`{"scenario":"bulk"}`), config.Limits)
	if err != nil {
		return fmt.Errorf("searchtest conformance: bulk applied fixture: %w", err)
	}
	conflict, err := search.NewDocument(config.TenantA, config.LogicalIndex, "bulk-conflict", 1,
		json.RawMessage(`{"scenario":"bulk"}`), config.Limits)
	if err != nil {
		return fmt.Errorf("searchtest conformance: bulk conflict fixture: %w", err)
	}
	operations := []search.WriteOperation{
		search.IndexDocument(applied),
		search.IndexDocument(conflict),
		search.DeleteDocument(config.TenantA, config.LogicalIndex, "bulk-missing", 1),
	}
	result, err := config.Adapter.Bulk(ctx, search.BulkRequest{Operations: operations, Refresh: config.Refresh})
	if err != nil {
		return fmt.Errorf("searchtest conformance: bulk execution: %w", err)
	}
	items := result.Items()
	if len(items) != len(operations) || !result.Partial() {
		return fmt.Errorf("searchtest conformance: bulk shape: items=%#v partial=%t", items, result.Partial())
	}
	wantStates := []search.OutcomeState{search.OutcomeApplied, search.OutcomeVersionConflict, search.OutcomeNotFound}
	for position, item := range items {
		operation := operations[position]
		if item.Position != position || item.ID != operation.ID || item.Action != operation.Action ||
			item.Version != operation.Version || item.State != wantStates[position] {
			return fmt.Errorf("searchtest conformance: bulk item %d: got=%#v operation=%#v state=%s", position, item, operation, wantStates[position])
		}
	}
	return nil
}

type fixtureDocument struct {
	id      string
	version uint64
	source  string
}

func writeFixture(ctx context.Context, config ConformanceConfig, tenant string, fixture fixtureDocument) error {
	document, err := search.NewDocument(tenant, config.LogicalIndex, fixture.id, fixture.version,
		json.RawMessage(fixture.source), config.Limits)
	if err != nil {
		return fmt.Errorf("searchtest conformance: document %s: %w", fixture.id, err)
	}
	outcome, err := config.Adapter.Write(ctx, search.IndexDocument(document), config.Refresh)
	if err != nil || outcome.State != search.OutcomeApplied {
		return fmt.Errorf("searchtest conformance: write %s: outcome=%#v error=%v", fixture.id, outcome, err)
	}
	return nil
}

func expectIDs(ctx context.Context, config ConformanceConfig, tenant string, query search.Query,
	size, offset int, want []string, scenario string,
) error {
	result, err := config.Adapter.Search(ctx, search.Request{
		Tenant: tenant, Index: config.LogicalIndex, Query: query,
		Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}},
		Page: search.OffsetPage{Size: size, Offset: offset},
	})
	if err != nil {
		return fmt.Errorf("searchtest conformance: %s search: %w", scenario, err)
	}
	hits := result.Hits()
	got := make([]string, len(hits))
	for position, hit := range hits {
		got[position] = hit.ID
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("searchtest conformance: %s IDs=%v want=%v", scenario, got, want)
	}
	return nil
}
