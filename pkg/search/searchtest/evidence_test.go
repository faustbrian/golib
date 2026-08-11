package searchtest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	"github.com/faustbrian/golib/pkg/search/searchtest"
)

func TestEvidenceStressFakeConcurrentTenantIsolation(t *testing.T) {
	const workers = 8
	const documentsPerTenant = 32
	limits := search.DefaultLimits()
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	start := make(chan struct{})
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for worker := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			tenant := fmt.Sprintf("tenant-%d", worker)
			operations := make([]search.WriteOperation, documentsPerTenant)
			for index := range documentsPerTenant {
				id := fmt.Sprintf("document-%03d", index)
				document, documentErr := search.NewDocument(tenant, "documents", id, 1,
					json.RawMessage(fmt.Sprintf(`{"owner":%q}`, tenant)), limits)
				if documentErr != nil {
					errors <- documentErr
					return
				}
				operations[index] = search.IndexDocument(document)
			}
			result, bulkErr := fake.Bulk(ctx, search.BulkRequest{Operations: operations, Refresh: search.RefreshWaitFor})
			if bulkErr != nil || result.Partial() {
				errors <- fmt.Errorf("bulk tenant %s: partial=%t error=%v", tenant, result.Partial(), bulkErr)
				return
			}
		}()
	}
	close(start)
	waitForEvidenceWorkers(t, ctx, &wait)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}

	errors = make(chan error, workers)
	for worker := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			tenant := fmt.Sprintf("tenant-%d", worker)
			result, searchErr := fake.Search(ctx, search.Request{
				Tenant: tenant, Index: "documents",
				Query: search.TermQuery{Field: "owner", Value: search.StringValue(tenant)},
				Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
				Page:  search.OffsetPage{Size: documentsPerTenant},
			})
			if searchErr != nil {
				errors <- searchErr
				return
			}
			ids := make([]string, len(result.Hits()))
			for index, hit := range result.Hits() {
				ids[index] = hit.ID
			}
			want := make([]string, documentsPerTenant)
			for index := range documentsPerTenant {
				want[index] = fmt.Sprintf("document-%03d", index)
			}
			if !slices.Equal(ids, want) || result.Total().Value != documentsPerTenant {
				errors <- fmt.Errorf("tenant %s observed IDs %v and total %d", tenant, ids, result.Total().Value)
			}
		}()
	}
	waitForEvidenceWorkers(t, ctx, &wait)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func waitForEvidenceWorkers(t *testing.T, ctx context.Context, wait *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wait.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("concurrent fake workload exceeded its bound: %v", ctx.Err())
	}
}

func TestEvidenceFaultFakeStaleEventCannotResurrectDeletedSource(t *testing.T) {
	limits := search.DefaultLimits()
	fake, err := searchtest.NewFake(limits)
	if err != nil {
		t.Fatal(err)
	}
	document, err := search.NewDocument("tenant", "documents", "id", 1, json.RawMessage(`{"state":"live"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	if outcome, writeErr := fake.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor); writeErr != nil || outcome.State != search.OutcomeApplied {
		t.Fatal(outcome, writeErr)
	}
	if outcome, deleteErr := fake.Write(t.Context(), search.DeleteDocument("tenant", "documents", "id", 3), search.RefreshWaitFor); deleteErr != nil || outcome.State != search.OutcomeApplied {
		t.Fatal(outcome, deleteErr)
	}
	document, err = search.NewDocument("tenant", "documents", "id", 2, json.RawMessage(`{"state":"stale"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	if outcome, staleErr := fake.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor); staleErr != nil || outcome.State != search.OutcomeVersionConflict {
		t.Fatalf("stale event outcome = %#v/%v", outcome, staleErr)
	}
	result, err := fake.Search(t.Context(), search.Request{
		Tenant: "tenant", Index: "documents", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10},
	})
	if err != nil || len(result.Hits()) != 0 {
		t.Fatalf("deleted source was resurrected: hits=%#v error=%v", result.Hits(), err)
	}
}
