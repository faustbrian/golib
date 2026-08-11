package opensearch_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestDirectIndexDeletionCannotBypassCleanupEligibilityGuard(t *testing.T) {
	t.Parallel()

	requests := 0
	client := newCutoverClient(t, nil, nil, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
	}))
	if err := client.DeleteIndex(context.Background(), "tenant", "events-v1"); !errors.Is(err, adapter.ErrLifecycleCleanupGuardRequired) {
		t.Fatalf("DeleteIndex() error = %v, want ErrLifecycleCleanupGuardRequired", err)
	}
	if requests != 0 {
		t.Fatalf("unguarded direct deletion dispatched %d requests", requests)
	}
}

func TestCleanupIndexHoldsEligibilityGuardAcrossDeletion(t *testing.T) {
	t.Parallel()

	held := false
	request := search.LifecycleCleanupRequest{
		MigrationID: "migration-1", Tenant: "tenant", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}
	client := newCleanupClient(t,
		adapter.LifecycleCleanupGuardFunc(func(_ context.Context, got search.LifecycleCleanupRequest, operation func() error) error {
			if got != request {
				t.Fatalf("cleanup request = %#v, want %#v", got, request)
			}
			held = true
			defer func() { held = false }()
			return operation()
		}),
		roundTripFunc(func(httpRequest *http.Request) (*http.Response, error) {
			if !held || httpRequest.Method != http.MethodDelete || httpRequest.URL.Path != "/events-v1" {
				t.Fatalf("cleanup request escaped guard: held=%t %s %s", held, httpRequest.Method, httpRequest.URL.Path)
			}
			return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
		}),
	)
	if err := client.CleanupIndex(t.Context(), request); err != nil {
		t.Fatalf("CleanupIndex() = %v", err)
	}
	if held {
		t.Fatal("cleanup eligibility guard remained held")
	}
}

func TestAliasMutationCannotBypassActiveCleanupExclusion(t *testing.T) {
	t.Parallel()

	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	cleanupDone := make(chan error, 1)
	aliasRequests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost && request.URL.Path == "/_aliases" {
			aliasRequests++
		}
		return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
	})
	cleanupClient := newCleanupClient(t,
		adapter.LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
			close(cleanupEntered)
			<-releaseCleanup
			return operation()
		}),
		transport,
	)
	competingClient, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport,
		TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = competingClient.Close() })
	request := search.LifecycleCleanupRequest{
		MigrationID: "migration-1", Tenant: "tenant", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}
	go func() { cleanupDone <- cleanupClient.CleanupIndex(t.Context(), request) }()
	<-cleanupEntered

	err = competingClient.AddAlias(t.Context(), "tenant", "events-retained", "events-v1", false)
	if err == nil || aliasRequests != 0 {
		t.Fatalf("unguarded alias mutation during cleanup = %v/requests=%d, want rejection before transport", err, aliasRequests)
	}
	close(releaseCleanup)
	if err := <-cleanupDone; err != nil {
		t.Fatalf("CleanupIndex() = %v", err)
	}
}

func TestSharedMutationGuardSerializesAliasMutationAfterCleanup(t *testing.T) {
	t.Parallel()

	guard := newSerialLifecycleMutationGuard()
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	aliasTransport := make(chan struct{}, 1)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost && request.URL.Path == "/_aliases" {
			aliasTransport <- struct{}{}
		}
		return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
	})
	cleanupClient := newCleanupClientWithMutationGuard(t, guard,
		adapter.LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
			close(cleanupEntered)
			<-releaseCleanup
			return operation()
		}),
		transport,
	)
	competingClient := newCutoverClientWithMutationGuard(t, guard, transport)
	request := search.LifecycleCleanupRequest{
		MigrationID: "migration-1", Tenant: "tenant", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- cleanupClient.CleanupIndex(t.Context(), request) }()
	cleanupAttempt := <-guard.attempts
	if cleanupAttempt.Operation != adapter.OperationDeleteIndex {
		t.Fatalf("cleanup mutation operation = %q", cleanupAttempt.Operation)
	}
	<-cleanupEntered
	aliasDone := make(chan error, 1)
	go func() {
		aliasDone <- competingClient.AddAlias(t.Context(), "tenant", "events-retained", "events-v1", false)
	}()
	aliasAttempt := <-guard.attempts
	if aliasAttempt.Operation != adapter.OperationSwapAlias {
		t.Fatalf("alias mutation operation = %q", aliasAttempt.Operation)
	}
	select {
	case <-aliasTransport:
		t.Fatal("alias mutation reached transport while cleanup exclusion was active")
	default:
	}
	close(releaseCleanup)
	if err := <-cleanupDone; err != nil {
		t.Fatalf("CleanupIndex() = %v", err)
	}
	if err := <-aliasDone; err != nil {
		t.Fatalf("AddAlias() = %v", err)
	}
	select {
	case <-aliasTransport:
	default:
		t.Fatal("alias mutation did not run after cleanup exclusion released")
	}
}

func TestLifecycleMutationWaitsForAsynchronousCallbackBeforeRejectingGuard(t *testing.T) {
	t.Parallel()

	transportEntered := make(chan struct{})
	releaseTransport := make(chan struct{})
	guardReturned := make(chan struct{})
	guard := adapter.LifecycleMutationGuardFunc(func(_ context.Context, _ adapter.LifecycleMutationRequest, operation func() error) error {
		go func() { _ = operation() }()
		<-transportEntered
		close(guardReturned)
		return nil
	})
	client := newCutoverClientWithMutationGuard(t, guard, roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(transportEntered)
		<-releaseTransport
		return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
	}))
	done := make(chan error, 1)
	go func() { done <- client.AddAlias(t.Context(), "tenant", "events-read", "events-v1", true) }()
	<-guardReturned
	select {
	case err := <-done:
		t.Fatalf("AddAlias() returned before its callback completed: %v", err)
	default:
	}
	close(releaseTransport)
	err := <-done
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleMutationGuardRejected) || !errors.As(err, &failure) || failure.OutcomeKnown {
		t.Fatalf("asynchronous lifecycle mutation error = %#v, want unknown guard rejection", err)
	}
}

func TestCleanupIndexRejectsGuardThatReturnsBeforeDeletionCompletes(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	callbackDone := make(chan struct{})
	client := newCleanupClient(t,
		adapter.LifecycleCleanupGuardFunc(func(_ context.Context, _ search.LifecycleCleanupRequest, operation func() error) error {
			go func() {
				defer close(callbackDone)
				_ = operation()
			}()
			<-entered
			return nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			close(entered)
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	)
	request := search.LifecycleCleanupRequest{
		MigrationID: "migration-1", Tenant: "tenant", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}
	done := make(chan error, 1)
	go func() { done <- client.CleanupIndex(t.Context(), request) }()
	<-entered
	err := <-done
	<-callbackDone
	var failure *adapter.Failure
	if !errors.Is(err, adapter.ErrLifecycleCleanupGuardRejected) || !errors.As(err, &failure) || failure.OutcomeKnown {
		t.Fatalf("asynchronous CleanupIndex() error = %#v, want unknown guard rejection", err)
	}
}

func newCleanupClient(t *testing.T, guard adapter.LifecycleCleanupGuard, transport http.RoundTripper) *adapter.Client {
	return newCleanupClientWithMutationGuard(t, allowLifecycleMutationGuard(), guard, transport)
}

func newCleanupClientWithMutationGuard(t *testing.T, mutationGuard adapter.LifecycleMutationGuard, guard adapter.LifecycleCleanupGuard, transport http.RoundTripper) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport,
		TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer:    adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			MutationGuard: mutationGuard,
			CleanupGuard:  guard,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newCutoverClientWithMutationGuard(t *testing.T, guard adapter.LifecycleMutationGuard, transport http.RoundTripper) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport,
		TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer:    adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			MutationGuard: guard,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type serialLifecycleMutationGuard struct {
	token    chan struct{}
	attempts chan adapter.LifecycleMutationRequest
}

func newSerialLifecycleMutationGuard() *serialLifecycleMutationGuard {
	guard := &serialLifecycleMutationGuard{
		token: make(chan struct{}, 1), attempts: make(chan adapter.LifecycleMutationRequest, 2),
	}
	guard.token <- struct{}{}
	return guard
}

func (guard *serialLifecycleMutationGuard) WithLifecycleMutation(ctx context.Context, request adapter.LifecycleMutationRequest, operation func() error) error {
	guard.attempts <- request
	select {
	case <-guard.token:
		defer func() { guard.token <- struct{}{} }()
		return operation()
	case <-ctx.Done():
		return ctx.Err()
	}
}
