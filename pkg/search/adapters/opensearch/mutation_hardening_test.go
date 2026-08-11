package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestSearchAcceptsCursorWithExactlyOneMillisecondRemaining(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	codec, err := search.NewCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"),
		func() time.Time { return now },
		4096,
	)
	if err != nil {
		t.Fatal(err)
	}
	resolver := internalResolver{target: IndexTarget{
		Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition-v1",
	}}
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodDelete {
			return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit","successful":true}]}`), nil
		}
		return internalResponse(http.StatusOK, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`), nil
	}, resolver, nil)
	client.pits.close()
	client.search.CursorCodec = codec
	client.pits = newPointInTimeTracker(codec, client.search.MaximumOpenPointInTimes)

	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	fingerprint, err := search.RequestFingerprint(request, client.search.Limits)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := codec.Encode(search.CursorBinding{
		Tenant: request.Tenant, Index: request.Index,
		QueryFingerprint: fingerprint, IndexFingerprint: resolver.target.Fingerprint,
	}, search.CursorState{
		PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"id"`)},
		ExpiresAt: now.Add(time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Page = search.CursorPage{Size: 1, Cursor: cursor, KeepAlive: time.Minute}
	result, err := client.Search(t.Context(), request)
	if err != nil || len(result.Hits()) != 0 || result.NextCursor() != "" {
		t.Fatalf("Search() = %#v/%v, want an accepted terminal cursor page", result, err)
	}
}

func TestProjectionValidationRejectsEmptyAndNestedDisclosureEdges(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		projection search.Projection
		want       bool
	}{
		{name: "empty root discloses nothing", source: `{}`, projection: search.Projection{Includes: []string{"public"}}, want: true},
		{name: "excluded empty object", source: `{"private":{}}`, projection: search.Projection{Excludes: []string{"private"}}},
		{name: "nested included array", source: `{"public":[{"name":"value"}]}`, projection: search.Projection{Includes: []string{"public.name"}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sourceWithinProjection(json.RawMessage(test.source), test.projection); got != test.want {
				t.Fatalf("sourceWithinProjection(%s, %#v) = %t, want %t", test.source, test.projection, got, test.want)
			}
		})
	}
}

func TestProjectionWildcardMatcherPreservesOrderedSegmentsAndSuffixes(t *testing.T) {
	tests := []struct {
		pattern string
		field   string
		want    bool
	}{
		{pattern: "a**b", field: "axxb", want: true},
		{pattern: "a*x*y", field: "a-x-y", want: true},
		{pattern: "a*x*y", field: "a-y-x", want: false},
		{pattern: "*ab*cd", field: "zzabyycd", want: true},
		{pattern: "*ab*cd", field: "zzabcdyy", want: false},
		{pattern: "a*b", field: "ab", want: true},
		{pattern: "a*b", field: "a", want: false},
		{pattern: "a*", field: "a", want: true},
	}
	for _, test := range tests {
		if got := projectionPatternMatches(test.pattern, test.field); got != test.want {
			t.Fatalf("projectionPatternMatches(%q, %q) = %t, want %t", test.pattern, test.field, got, test.want)
		}
	}
}

func TestCreatePointInTimeAcceptsIdentifierAtQueryByteLimit(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition-v1"}}
	client := internalClient(t, nil, resolver, nil)
	id := strings.Repeat("p", client.search.Limits.MaxQueryBytes)
	client.maximumResponseBytes = int64(len(id) + 1024)
	client.transport.maximumResponseBytes = client.maximumResponseBytes
	client.transport.next = internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return internalResponse(http.StatusCreated, `{"pit_id":"`+id+`"}`), nil
	})
	got, err := client.createPIT(t.Context(), "events-v1", time.Minute)
	if err != nil || got != id {
		t.Fatalf("createPIT() identifier length/error = %d/%v, want %d/nil", len(got), err, len(id))
	}
}

func TestWriteResponseRejectsMismatchedNotFoundTarget(t *testing.T) {
	operation := search.DeleteDocument("tenant", "events", "id", 7)
	_, err := decodeWriteResponse(operation, "events-v1", http.StatusNotFound,
		[]byte(`{"_index":"events-v2","_id":"id","_version":7,"status":404,"result":"not_found"}`))
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("decodeWriteResponse() error = %v, want ErrMalformedResponse", err)
	}
}

func TestAppliedDeleteResultRequiresExactStatusAndResult(t *testing.T) {
	if !validAppliedWriteResult(search.ActionDelete, http.StatusOK, "deleted") {
		t.Fatal("valid delete result rejected")
	}
	if validAppliedWriteResult(search.ActionDelete, http.StatusCreated, "deleted") {
		t.Fatal("created delete result accepted")
	}
	if validAppliedWriteResult(search.ActionDelete, http.StatusOK, "created") {
		t.Fatal("non-delete result accepted")
	}
}

func TestCleanupIndexValidatesEveryMigrationBinding(t *testing.T) {
	client := internalClient(t, routeBody(`{"acknowledged":true}`, http.StatusOK), nil,
		LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
	tests := []struct {
		name   string
		mutate func(*search.LifecycleCleanupRequest)
	}{
		{name: "migration", mutate: func(request *search.LifecycleCleanupRequest) { request.MigrationID = "" }},
		{name: "tenant", mutate: func(request *search.LifecycleCleanupRequest) { request.Tenant = "" }},
		{name: "alias", mutate: func(request *search.LifecycleCleanupRequest) { request.Alias = "bad/alias" }},
		{name: "active index", mutate: func(request *search.LifecycleCleanupRequest) { request.ActiveIndex = "bad/index" }},
		{name: "inactive index", mutate: func(request *search.LifecycleCleanupRequest) { request.InactiveIndex = "bad/index" }},
		{name: "same generation", mutate: func(request *search.LifecycleCleanupRequest) { request.InactiveIndex = request.ActiveIndex }},
		{name: "active fingerprint", mutate: func(request *search.LifecycleCleanupRequest) { request.ActiveFingerprint = "" }},
		{name: "inactive fingerprint", mutate: func(request *search.LifecycleCleanupRequest) { request.InactiveFingerprint = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := cleanupBranchRequest()
			test.mutate(&request)
			if err := client.CleanupIndex(t.Context(), request); !errors.Is(err, ErrUnsafeIndexTarget) {
				t.Fatalf("CleanupIndex() error = %v, want ErrUnsafeIndexTarget", err)
			}
		})
	}
}

func TestCleanupGuardFailurePreservesOutcomeKnowledgeAndOperationCause(t *testing.T) {
	known := lifecycleCleanupGuardFailure(nil)
	if !known.OutcomeKnown || !errors.Is(known, ErrLifecycleCleanupGuardRejected) {
		t.Fatalf("known cleanup guard failure = %#v", known)
	}
	operationErr := errors.New("delete failed")
	unknown := lifecycleCleanupGuardFailure(operationErr)
	if unknown.OutcomeKnown || !errors.Is(unknown, ErrLifecycleCleanupGuardRejected) || !errors.Is(unknown, operationErr) {
		t.Fatalf("unknown cleanup guard failure = %#v", unknown)
	}
}

func TestPointInTimeTrackerAccountsForRebindingAcquisitionAndDefensiveRelease(t *testing.T) {
	now := time.Now()
	codec, err := search.NewCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096,
	)
	if err != nil {
		t.Fatal(err)
	}
	tracker := newPointInTimeTracker(codec, 3)
	expiresAt := now.Add(time.Minute)
	lease, err := tracker.acquire("pit-old", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := tracker.snapshot(); snapshot.Open != 1 {
		t.Fatalf("acquired tracker snapshot = %#v, want one open PIT", snapshot)
	}
	if err := tracker.bind(lease, "pit-new", expiresAt); err != nil {
		t.Fatal(err)
	}
	tracker.yield(lease)
	oldLease, err := tracker.acquire("pit-old", expiresAt)
	if err != nil || oldLease == lease {
		t.Fatalf("old PIT identifier retained original ownership = %#v/%v", oldLease, err)
	}
	tracker.release(oldLease)

	other := &pointInTimeLease{id: "pit-new", expiresAt: expiresAt, active: true}
	tracker.mu.Lock()
	tracker.mu.byID["pit-new"] = other
	tracker.releaseLocked(lease)
	if tracker.mu.byID["pit-new"] != other {
		tracker.mu.Unlock()
		t.Fatal("release deleted another lease's PIT identifier")
	}
	tracker.mu.open = 0
	tracker.releaseLocked(other)
	open := tracker.mu.open
	tracker.mu.Unlock()
	if open != 0 {
		t.Fatalf("defensive release underflowed open PIT count to %d", open)
	}
}
