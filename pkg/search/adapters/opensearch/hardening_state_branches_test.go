package opensearch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestPointInTimeTrackerFailsClosedAcrossTerminalStates(t *testing.T) {
	now := time.Now()
	codec, err := search.NewCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"),
		func() time.Time { return now },
		4096,
	)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := now.Add(time.Minute)

	closed := newPointInTimeTracker(codec, 1)
	closed.close()
	if _, err := closed.reserve(expiresAt); !errors.Is(err, ErrClosed) {
		t.Fatalf("reserve after close error = %v, want ErrClosed", err)
	}
	if _, err := closed.acquire("pit", expiresAt); !errors.Is(err, ErrClosed) {
		t.Fatalf("acquire after close error = %v, want ErrClosed", err)
	}

	tracker := newPointInTimeTracker(codec, 2)
	if err := tracker.bind(nil, "pit", expiresAt); err != nil {
		t.Fatalf("nil lease bind error = %v", err)
	}
	lease, err := tracker.reserve(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.bind(lease, "pit-old", expiresAt); err != nil {
		t.Fatal(err)
	}
	if err := tracker.bind(lease, "pit-new", expiresAt); err != nil {
		t.Fatalf("PIT identifier renewal error = %v", err)
	}
	tracker.release(lease)
	if err := tracker.bind(lease, "pit-revived", expiresAt); !errors.Is(err, ErrClosed) {
		t.Fatalf("released lease bind error = %v, want ErrClosed", err)
	}

	capacity := newPointInTimeTracker(codec, 1)
	held, err := capacity.reserve(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capacity.acquire("other-pit", expiresAt); !errors.Is(err, ErrPointInTimeCapacity) || !errors.Is(err, ErrBackpressure) {
		t.Fatalf("capacity acquire error = %v", err)
	}
	capacity.release(held)

	corrupted := newPointInTimeTracker(codec, 1)
	inactive, err := corrupted.acquire("inactive-pit", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	corrupted.mu.Lock()
	inactive.active = false
	corrupted.mu.Unlock()
	if _, err := corrupted.acquire("inactive-pit", expiresAt); !errors.Is(err, ErrClosed) {
		t.Fatalf("inactive lease acquire error = %v, want ErrClosed", err)
	}

	var nilTracker *pointInTimeTracker
	if snapshot := nilTracker.snapshot(); snapshot != (PointInTimeSnapshot{}) {
		t.Fatalf("nil tracker snapshot = %#v", snapshot)
	}
	tracker.yield(nil)
}

func TestPointInTimeTrackerRejectsInvalidRotationAndReapsExpiry(t *testing.T) {
	now := time.Now()
	codec, err := search.NewCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"),
		func() time.Time { return now },
		4096,
	)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := now.Add(time.Minute)
	tracker := newPointInTimeTracker(codec, 2)
	first, err := tracker.reserve(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.bind(first, "pit-a", expiresAt); err != nil {
		t.Fatal(err)
	}
	tracker.yield(first)
	if tracker.rotate(first, "pit-a", "pit-c") {
		t.Fatal("rotation accepted a lease that was not acquired")
	}

	acquired, err := tracker.acquire("pit-a", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tracker.reserve(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.bind(second, "pit-b", expiresAt); err != nil {
		t.Fatal(err)
	}
	if tracker.rotate(acquired, "pit-a", "pit-b") {
		t.Fatal("rotation stole another live PIT identifier")
	}
	tracker.yield(acquired)
	tracker.yield(second)

	now = now.Add(2 * time.Minute)
	if snapshot := tracker.snapshot(); snapshot.Open != 0 {
		t.Fatalf("expired tracker snapshot = %#v, want no open PITs", snapshot)
	}
}

func TestPointInTimeTrackerReapToleratesConcurrentRelease(t *testing.T) {
	now := time.Now()
	var tracker *pointInTimeTracker
	var lease *pointInTimeLease
	releaseOnClock := false
	codec, err := search.NewCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"),
		func() time.Time {
			if releaseOnClock {
				releaseOnClock = false
				tracker.release(lease)
			}
			return now
		},
		4096,
	)
	if err != nil {
		t.Fatal(err)
	}
	tracker = newPointInTimeTracker(codec, 1)
	lease, err = tracker.reserve(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	releaseOnClock = true
	if snapshot := tracker.snapshot(); snapshot.Open != 0 {
		t.Fatalf("concurrently released tracker snapshot = %#v, want no open PITs", snapshot)
	}
}

func TestCleanupIndexRejectsInvalidBindingAndMissingEligibilityGuard(t *testing.T) {
	authorize := LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })
	client := internalClient(t, routeBody(`{"acknowledged":true}`, http.StatusOK), nil, authorize)
	invalidUTF8 := string([]byte{0xff})
	for name, mutate := range map[string]func(*search.LifecycleCleanupRequest){
		"missing active fingerprint": func(request *search.LifecycleCleanupRequest) { request.ActiveFingerprint = "" },
		"oversized migration ID": func(request *search.LifecycleCleanupRequest) {
			request.MigrationID = strings.Repeat("m", search.DefaultLimits().MaxIDBytes+1)
		},
		"invalid UTF-8 migration ID": func(request *search.LifecycleCleanupRequest) { request.MigrationID = invalidUTF8 },
	} {
		request := cleanupBranchRequest()
		mutate(&request)
		if err := client.CleanupIndex(t.Context(), request); !errors.Is(err, ErrUnsafeIndexTarget) {
			t.Fatalf("%s cleanup binding error = %v, want ErrUnsafeIndexTarget", name, err)
		}
	}

	request := cleanupBranchRequest()
	client.lifecycle.CleanupGuard = nil
	if err := client.CleanupIndex(t.Context(), request); !errors.Is(err, ErrLifecycleCleanupGuardRequired) {
		t.Fatalf("unguarded cleanup error = %v, want ErrLifecycleCleanupGuardRequired", err)
	}
}

func TestCleanupIndexRejectsMissingRepeatedAndDivergentGuardOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		response  func(*http.Request) (*http.Response, error)
		guard     LifecycleCleanupGuard
		wantKnown bool
	}{
		{
			name:     "missing callback",
			response: routeBody(`{"acknowledged":true}`, http.StatusOK),
			guard: LifecycleCleanupGuardFunc(func(context.Context, search.LifecycleCleanupRequest, func() error) error {
				return nil
			}),
			wantKnown: true,
		},
		{
			name:     "repeated callback",
			response: routeBody(`{"acknowledged":true}`, http.StatusOK),
			guard: LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
				if err := operation(); err != nil {
					return err
				}
				return operation()
			}),
			wantKnown: true,
		},
		{
			name: "operation and guard disagree",
			response: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network unavailable")
			},
			guard: LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
				_ = operation()
				return errors.New("eligibility lease lost")
			}),
			wantKnown: false,
		},
		{
			name:     "guard rejects completed deletion",
			response: routeBody(`{"acknowledged":true}`, http.StatusOK),
			guard: LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
				if err := operation(); err != nil {
					return err
				}
				return errors.New("eligibility lease lost")
			}),
			wantKnown: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := internalClient(t, test.response, nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))
			client.lifecycle.CleanupGuard = test.guard
			err := client.CleanupIndex(t.Context(), cleanupBranchRequest())
			var failure *Failure
			if !errors.Is(err, ErrLifecycleCleanupGuardRejected) || !errors.As(err, &failure) || failure.OutcomeKnown != test.wantKnown {
				t.Fatalf("CleanupIndex() error = %#v, want guard rejection with known=%t", err, test.wantKnown)
			}
		})
	}
}

func TestLifecycleMutationGuardFailsClosedAcrossCallbackOutcomes(t *testing.T) {
	operationFailure := errors.New("operation failed")
	guardFailure := errors.New("private guard failure")
	request := LifecycleMutationRequest{
		Tenant: "tenant", Operation: OperationSwapAlias, Resources: []string{"events-read", "events-v1", "events-v2"},
	}
	client := internalClient(t, routeBody(`{}`, http.StatusOK), nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }))

	client.lifecycle.MutationGuard = nil
	if err := client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return nil }); !errors.Is(err, ErrLifecycleMutationGuardRequired) {
		t.Fatalf("missing mutation guard error = %v, want ErrLifecycleMutationGuardRequired", err)
	}

	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(context.Context, LifecycleMutationRequest, func() error) error {
		return nil
	})
	assertLifecycleMutationFailure(t,
		client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return nil }),
		true,
	)

	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(context.Context, LifecycleMutationRequest, func() error) error {
		return context.Canceled
	})
	if err := client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mutation guard error = %v, want context cancellation", err)
	}

	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, _ LifecycleMutationRequest, operation func() error) error {
		if err := operation(); err != nil {
			return err
		}
		return operation()
	})
	assertLifecycleMutationFailure(t,
		client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return nil }),
		false,
	)

	var lateOperation func() error
	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, _ LifecycleMutationRequest, operation func() error) error {
		lateOperation = operation
		return nil
	})
	assertLifecycleMutationFailure(t,
		client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return nil }),
		true,
	)
	assertLifecycleMutationFailure(t, lateOperation(), true)

	callbackStarted := make(chan struct{})
	callbackDone := make(chan error, 1)
	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, _ LifecycleMutationRequest, operation func() error) error {
		go func() { callbackDone <- operation() }()
		<-callbackStarted
		return nil
	})
	assertLifecycleMutationFailure(t,
		client.withLifecycleMutation(t.Context(), request, func(ctx context.Context) error {
			close(callbackStarted)
			<-ctx.Done()
			return ctx.Err()
		}),
		false,
	)
	if err := <-callbackDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight mutation callback error = %v, want context cancellation", err)
	}

	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, _ LifecycleMutationRequest, operation func() error) error {
		_ = operation()
		return guardFailure
	})
	err := client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return operationFailure })
	assertLifecycleMutationFailure(t, err, false)
	if !errors.Is(err, operationFailure) || errors.Is(err, guardFailure) {
		t.Fatalf("divergent guard error = %v, want operation cause without private guard cause", err)
	}

	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, _ LifecycleMutationRequest, operation func() error) error {
		return operation()
	})
	if err := client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return operationFailure }); !errors.Is(err, operationFailure) || errors.Is(err, ErrLifecycleMutationGuardRejected) {
		t.Fatalf("preserved operation error = %v", err)
	}

	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, _ LifecycleMutationRequest, operation func() error) error {
		if err := operation(); err != nil {
			return err
		}
		return guardFailure
	})
	err = client.withLifecycleMutation(t.Context(), request, func(context.Context) error { return nil })
	assertLifecycleMutationFailure(t, err, true)
	if errors.Is(err, guardFailure) {
		t.Fatalf("successful mutation leaked private guard cause: %v", err)
	}

	resources := []string{"events-read", "events-v1", "events-v2"}
	client.lifecycle.MutationGuard = LifecycleMutationGuardFunc(func(_ context.Context, guarded LifecycleMutationRequest, operation func() error) error {
		guarded.Resources[0] = "mutated"
		return operation()
	})
	if err := client.withLifecycleMutation(t.Context(), LifecycleMutationRequest{
		Tenant: "tenant", Operation: OperationSwapAlias, Resources: resources,
	}, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("valid lifecycle mutation error = %v", err)
	}
	if resources[0] != "events-read" {
		t.Fatalf("mutation guard changed caller resources: %#v", resources)
	}
}

func assertLifecycleMutationFailure(t *testing.T, err error, outcomeKnown bool) {
	t.Helper()
	var failure *Failure
	if !errors.Is(err, ErrLifecycleMutationGuardRejected) || !errors.As(err, &failure) ||
		failure.Operation != OperationSwapAlias || failure.Category != FailureRejected || failure.OutcomeKnown != outcomeKnown {
		t.Fatalf("lifecycle mutation error = %#v, want guard rejection with known=%t", err, outcomeKnown)
	}
}

func TestLifecycleAuthorizationObservesCancellationAfterPolicyCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	client := internalClient(t, routeBody(`{}`, http.StatusOK), nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error {
		cancel()
		return nil
	}))
	if err := client.authorizeLifecycle(ctx, "tenant", "events"); !errors.Is(err, context.Canceled) {
		t.Fatalf("authorizeLifecycle() error = %v, want context cancellation", err)
	}
}

func TestSearchAuthorizationAndResolutionObserveCancellationAtBothBoundaries(t *testing.T) {
	request := search.Request{Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}}
	preCancelled, cancelPre := context.WithCancel(t.Context())
	cancelPre()
	client := internalClient(t, routeBody(`{}`, http.StatusOK), internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition"}}, nil)
	if err := client.authorizeSearch(preCancelled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled authorization error = %v", err)
	}
	if _, err := client.resolveIndexTarget(preCancelled, OperationSearch, "tenant", "events", IndexRead); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled resolution error = %v", err)
	}

	authorizeCtx, cancelAuthorize := context.WithCancel(t.Context())
	client.search.Authorizer = SearchAuthorizerFunc(func(context.Context, SearchAuthorization) error {
		cancelAuthorize()
		return nil
	})
	if err := client.authorizeSearch(authorizeCtx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("authorization callback cancellation error = %v", err)
	}

	resolveCtx, cancelResolve := context.WithCancel(t.Context())
	client.search.Resolver = IndexResolverFunc(func(context.Context, string, string, IndexAccess) (IndexTarget, error) {
		cancelResolve()
		return IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition"}, nil
	})
	if _, err := client.resolveIndexTarget(resolveCtx, OperationSearch, "tenant", "events", IndexRead); !errors.Is(err, context.Canceled) {
		t.Fatalf("resolver callback cancellation error = %v", err)
	}
}

func TestProjectionPatternMatcherImplementsOnlyOpenSearchStarWildcards(t *testing.T) {
	tests := []struct {
		pattern string
		field   string
		want    bool
	}{
		{pattern: "public", field: "private", want: false},
		{pattern: "public*", field: "private", want: false},
		{pattern: "a**b", field: "ab", want: true},
		{pattern: "a*x*b", field: "a-middle-b", want: false},
		{pattern: "a*x*b", field: "a-x-b", want: true},
	}
	for _, test := range tests {
		if got := projectionPatternMatches(test.pattern, test.field); got != test.want {
			t.Fatalf("projectionPatternMatches(%q, %q) = %t, want %t", test.pattern, test.field, got, test.want)
		}
	}
}

func TestSearchRejectsUnrepresentableCursorDeadlineBeforeCreatingPointInTime(t *testing.T) {
	requests := 0
	codec, err := search.NewCursorCodec(
		[]byte("0123456789abcdef0123456789abcdef"),
		func() time.Time { return time.Unix(1<<63-1, 0) },
		4096,
	)
	if err != nil {
		t.Fatal(err)
	}
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
	}, internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition"}}, nil)
	client.search.CursorCodec = codec
	_, err = client.Search(t.Context(), search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Millisecond},
	})
	if !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatalf("Search() error = %v, want ErrInvalidCursor", err)
	}
	if requests != 0 {
		t.Fatalf("unrepresentable deadline created %d PITs", requests)
	}
}

func TestSearchFailsClosedWhenPointInTimeTrackerClosesDuringCreation(t *testing.T) {
	var client *Client
	deleted := 0
	client = internalClient(t, func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost && request.URL.Path == "/events-v1/_search/point_in_time" {
			client.pits.close()
			return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
		}
		if request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time" {
			deleted++
			return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit","successful":true}]}`), nil
		}
		return internalResponse(http.StatusOK, `{}`), nil
	}, internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition"}}, nil)
	_, err := client.Search(t.Context(), search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Search() error = %v, want ErrClosed", err)
	}
	if deleted != 1 {
		t.Fatalf("created PIT cleanup requests = %d, want 1", deleted)
	}
}

func TestSearchRejectsRotatedPointInTimeIdentifierOwnedByAnotherCursor(t *testing.T) {
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/events-v1/_search/point_in_time":
			return internalResponse(http.StatusCreated, `{"pit_id":"pit-a"}`), nil
		case request.Method == http.MethodPost && request.URL.Path == "/_search":
			return internalResponse(http.StatusOK, `{"pit_id":"pit-b","took":1,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`), nil
		case request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time":
			return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit-a","successful":true}]}`), nil
		default:
			return internalResponse(http.StatusNotFound, `{}`), nil
		}
	}, internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition"}}, nil)
	expiresAt, err := client.search.CursorCodec.Deadline(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	other, err := client.pits.reserve(expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.pits.bind(other, "pit-b", expiresAt); err != nil {
		t.Fatal(err)
	}
	client.pits.yield(other)

	_, err = client.Search(t.Context(), search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	})
	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("Search() error = %v, want ErrMalformedResponse", err)
	}
}

func TestNilClientPointInTimeSnapshotIsEmpty(t *testing.T) {
	var client *Client
	if snapshot := client.PointInTimeSnapshot(); snapshot != (PointInTimeSnapshot{}) {
		t.Fatalf("nil client snapshot = %#v", snapshot)
	}
}

func cleanupBranchRequest() search.LifecycleCleanupRequest {
	return search.LifecycleCleanupRequest{
		MigrationID: "migration-1", Tenant: "tenant", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}
}
