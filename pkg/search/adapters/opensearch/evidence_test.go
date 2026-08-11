package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

const evidenceInfoResponse = `{"name":"node","cluster_name":"cluster","cluster_uuid":"uuid","version":{"number":"3.8.0"}}`

type evidenceRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip evidenceRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestEvidenceStressConcurrentInfoHasNoLostOutcomes(t *testing.T) {
	const workers = 16
	const callsPerWorker = 64
	var calls atomic.Int64
	transport := evidenceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return evidenceResponse(http.StatusOK, io.NopCloser(strings.NewReader(evidenceInfoResponse))), nil
	})
	client := evidenceClient(t, transport, nil, nil, adapter.ResilienceConfig{MaximumInFlight: workers})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	start := make(chan struct{})
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range callsPerWorker {
				info, err := client.Info(ctx)
				if err != nil || info.Version != "3.8.0" {
					errors <- fmt.Errorf("Info() = %#v/%v", info, err)
					return
				}
			}
		}()
	}
	close(start)
	waitForEvidenceWorkers(t, ctx, &wait)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if calls.Load() != workers*callsPerWorker {
		t.Fatalf("transport calls = %d, want %d", calls.Load(), workers*callsPerWorker)
	}
	if snapshot := client.ResilienceSnapshot(); snapshot.InFlight != 0 || snapshot.Rejections != 0 {
		t.Fatalf("resilience resources not released: %#v", snapshot)
	}
}

func TestEvidenceLeakCancellationReleasesAdmission(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int64
	transport := evidenceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return evidenceResponse(http.StatusOK, io.NopCloser(strings.NewReader(evidenceInfoResponse))), nil
	})
	client := evidenceClient(t, transport, nil, nil, adapter.ResilienceConfig{MaximumInFlight: 1})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := client.Info(ctx)
		result <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("transport was not reached before the cancellation bound")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled Info() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled Info() did not release within one second")
	}
	if snapshot := client.ResilienceSnapshot(); snapshot.InFlight != 0 {
		t.Fatalf("cancelled operation retained admission: %#v", snapshot)
	}
	reuseCtx, reuseCancel := context.WithTimeout(t.Context(), time.Second)
	defer reuseCancel()
	if info, err := client.Info(reuseCtx); err != nil || info.Version != "3.8.0" {
		t.Fatalf("released admission was not reusable: %#v/%v", info, err)
	}
}

func TestEvidenceLeakMalformedResponseBodyIsClosed(t *testing.T) {
	body := &evidenceObservedBody{Reader: strings.NewReader(`{"name":`)}
	transport := evidenceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return evidenceResponse(http.StatusOK, body), nil
	})
	client := evidenceClient(t, transport, nil, nil, adapter.ResilienceConfig{})
	if _, err := client.Info(t.Context()); err == nil {
		t.Fatal("malformed response was accepted")
	}
	if body.closed.Load() != 1 {
		t.Fatalf("response body close count = %d, want 1", body.closed.Load())
	}
}

func TestEvidenceLeakRepeatedNetworkCallsBoundRetainedResources(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, evidenceInfoResponse)
	}))
	transport := server.Client().Transport
	baselineGoroutines := runtime.NumGoroutine()
	baselineDescriptors := evidenceOpenDescriptorCount(t)
	runtime.GC()
	var baselineMemory runtime.MemStats
	runtime.ReadMemStats(&baselineMemory)

	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Resilience: adapter.ResilienceConfig{MaximumInFlight: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 512 {
		if info, err := client.Info(t.Context()); err != nil || info.Version != "3.8.0" {
			t.Fatalf("repeated Info() = %#v/%v", info, err)
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	server.CloseClientConnections()
	runtime.GC()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && runtime.NumGoroutine() > baselineGoroutines+2 {
		time.Sleep(10 * time.Millisecond)
		runtime.GC()
	}
	if retained := runtime.NumGoroutine() - baselineGoroutines; retained > 2 {
		t.Fatalf("retained goroutines = %d, want at most 2", retained)
	}
	if baselineDescriptors >= 0 {
		if retained := evidenceOpenDescriptorCount(t) - baselineDescriptors; retained > 2 {
			t.Fatalf("retained file descriptors = %d, want at most 2", retained)
		}
	}
	var finalMemory runtime.MemStats
	runtime.ReadMemStats(&finalMemory)
	if finalMemory.HeapAlloc > baselineMemory.HeapAlloc+4<<20 {
		t.Fatalf("retained heap growth = %d bytes, want at most %d", finalMemory.HeapAlloc-baselineMemory.HeapAlloc, 4<<20)
	}
}

func evidenceOpenDescriptorCount(t *testing.T) int {
	t.Helper()
	directory, err := os.Open("/dev/fd")
	if errors.Is(err, os.ErrNotExist) {
		return -1
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	entries, err := directory.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestEvidenceFaultMalformedBulkIsAmbiguousAndAttributed(t *testing.T) {
	body := &evidenceObservedBody{Reader: strings.NewReader(`{"took":1,"errors":false,"items":[`)}
	transport := evidenceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return evidenceResponse(http.StatusOK, body), nil
	})
	client := evidenceSearchClient(t, transport, adapter.ResilienceConfig{})
	limits := search.DefaultLimits()
	operations := make([]search.WriteOperation, 2)
	for index := range operations {
		document, err := search.NewDocument("tenant", "documents", fmt.Sprintf("id-%d", index), 1,
			json.RawMessage(`{"value":"safe"}`), limits)
		if err != nil {
			t.Fatal(err)
		}
		operations[index] = search.IndexDocument(document)
	}
	request := search.BulkRequest{Operations: operations, Refresh: search.RefreshWaitFor}
	result, err := client.Bulk(t.Context(), request)
	var failure *adapter.Failure
	if !errors.As(err, &failure) || failure.Category != adapter.FailureMalformed || failure.OutcomeKnown {
		t.Fatalf("malformed bulk error = %#v", err)
	}
	if validationErr := result.ValidateRequest(request); validationErr != nil {
		t.Fatalf("unknown outcomes lost attribution: %v", validationErr)
	}
	for _, outcome := range result.Items() {
		if outcome.State != search.OutcomeUnknown || !outcome.Retryable {
			t.Fatalf("malformed bulk outcome = %#v", outcome)
		}
	}
	if body.closed.Load() != 1 {
		t.Fatalf("bulk response body close count = %d, want 1", body.closed.Load())
	}
}

func TestEvidenceSoakSyntheticTransportBounded(t *testing.T) {
	configured := strings.TrimSpace(os.Getenv("SEARCH_SOAK_DURATION"))
	if configured == "" {
		t.Skip("SEARCH_SOAK_DURATION is not configured")
	}
	duration, err := time.ParseDuration(configured)
	if err != nil || duration <= 0 || duration > 5*time.Minute {
		t.Fatal("SEARCH_SOAK_DURATION must be greater than zero and at most 5m")
	}
	var calls atomic.Int64
	transport := evidenceRoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return evidenceResponse(http.StatusOK, io.NopCloser(strings.NewReader(evidenceInfoResponse))), nil
	})
	client := evidenceClient(t, transport, nil, nil, adapter.ResilienceConfig{MaximumInFlight: 8})
	deadline := time.Now().Add(duration)
	ctx, cancel := context.WithDeadline(t.Context(), deadline.Add(time.Second))
	defer cancel()
	errors := make(chan error, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for time.Now().Before(deadline) {
				if _, infoErr := client.Info(ctx); infoErr != nil {
					errors <- infoErr
					return
				}
			}
		}()
	}
	waitForEvidenceWorkers(t, ctx, &wait)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if calls.Load() == 0 || client.ResilienceSnapshot().InFlight != 0 {
		t.Fatalf("bounded soak did not complete cleanly: calls=%d snapshot=%#v", calls.Load(), client.ResilienceSnapshot())
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
		t.Fatalf("concurrent adapter workload exceeded its bound: %v", ctx.Err())
	}
}

type evidenceObservedBody struct {
	io.Reader
	closed atomic.Int64
}

func (body *evidenceObservedBody) Close() error {
	body.closed.Add(1)
	return nil
}

func evidenceResponse(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}
}

func evidenceClient(t *testing.T, transport http.RoundTripper, searchConfig *adapter.SearchConfig, lifecycle *adapter.LifecycleConfig, resilience adapter.ResilienceConfig) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"},
		Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Search: searchConfig, Lifecycle: lifecycle, Resilience: resilience,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func evidenceSearchClient(t *testing.T, transport http.RoundTripper, resilience adapter.ResilienceConfig) *adapter.Client {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	return evidenceClient(t, transport, &adapter.SearchConfig{
		Limits: search.DefaultLimits(), CursorCodec: codec, Clock: time.Now, WriteGuard: allowWriteAuthorization(),
		Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
			return adapter.IndexTarget{Name: "documents-v1", PhysicalName: "documents-v1", Fingerprint: "mapping-v1"}, nil
		}),
	}, nil, resilience)
}
