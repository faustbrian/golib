package opensearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

type failingBasicCredentials struct{}

func (failingBasicCredentials) Credentials(context.Context) (BasicCredentials, error) {
	return BasicCredentials{}, errors.New("private provider failure")
}

func TestRemainingTransportOutcomes(t *testing.T) {
	base := Config{
		Endpoints: []string{"https://host"}, Transport: internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return internalResponse(200, `{}`), nil }),
		TransportOwnership: TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 1024,
	}
	if _, err := New(Config{
		Endpoints: base.Endpoints, Transport: base.Transport, TransportOwnership: TransportBorrowed,
		TLS: &tls.Config{}, RequestTimeout: time.Second, MaximumResponseBytes: 1024,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal(err)
	}

	resilience, _ := newResilienceController(ResilienceConfig{})
	telemetry, _ := newTelemetry(nil)
	endpoint, _ := url.Parse("https://host")
	tests := []struct {
		name string
		next http.RoundTripper
	}{
		{"response and error", internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return internalResponse(200, `{}`), errors.New("late") })},
		{"read failure", internalRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(errorReader{})}, nil
		})},
		{"success", internalRoundTripFunc(func(*http.Request) (*http.Response, error) { return internalResponse(200, `{}`), nil })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := &poolTransport{endpoints: []*url.URL{endpoint}, next: test.next, maximumResponseBytes: 1024, resilience: resilience, telemetry: telemetry}
			request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			response, err := pool.Perform(request)
			if test.name == "success" && (err != nil || response == nil) {
				t.Fatal(response, err)
			}
			if test.name != "success" && err == nil {
				t.Fatal("failure accepted")
			}
		})
	}
	credentialPool := &poolTransport{endpoints: []*url.URL{endpoint}, next: base.Transport, basic: failingBasicCredentials{}, maximumResponseBytes: 1024, resilience: resilience, telemetry: telemetry}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	if _, err := credentialPool.Stream(request); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
}

func TestRemainingDiscoveryHealthAndInfoOutcomes(t *testing.T) {
	closed := internalClient(t, routeBody(`{}`, 200), nil, nil)
	closed.discovery = DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: []string{".example"}}
	_ = closed.Close()
	if _, err := closed.Discover(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}

	client := internalClient(t, routeBody(`{}`, 200), nil, nil)
	client.discovery = DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: []string{".example"}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}
	for _, address := range []string{"/host.example:9200", "inet/host.example:9200"} {
		endpoint, err := client.discoveredEndpoint(address)
		if address[0] == '/' && err == nil {
			t.Fatal("empty publish-address prefix accepted")
		}
		if address[0] != '/' && (err != nil || endpoint.Host == "") {
			t.Fatal(endpoint, err)
		}
	}
	if client.discoveryAllows("10.1.0.1") {
		t.Fatal("out-of-range address accepted")
	}

	failed := internalClient(t, routeBody(`{}`, 500), nil, nil)
	if _, err := failed.Capacity(t.Context()); err == nil {
		t.Fatal("capacity status accepted")
	}
	oversized := internalClient(t, routeBody(`too-large`, 500), nil, nil)
	oversized.maximumResponseBytes = 1
	if _, err := oversized.Info(t.Context()); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
}

func TestRemainingLifecycleAndReconciliationOutcomes(t *testing.T) {
	authorize := LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })
	sequence := 0
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		sequence++
		if sequence == 1 {
			return internalResponse(200, `{"count":1,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}
		return internalResponse(500, `{}`), nil
	}, nil, authorize)
	if _, err := client.VerifyIndex(t.Context(), "t", "source", "target", "definition"); err == nil {
		t.Fatal("target count failure accepted")
	}
	sequence = 0
	client = internalClient(t, func(*http.Request) (*http.Response, error) {
		sequence++
		count := 1
		if sequence == 2 {
			count = 2
		}
		return internalResponse(200, strings.Replace(`{"count":COUNT,"_shards":{"total":1,"successful":1,"failed":0}}`, "COUNT", string(rune('0'+count)), 1)), nil
	}, nil, authorize)
	report, err := client.VerifyIndex(t.Context(), "t", "source", "target", "definition")
	if err != nil || report.Drift != 1 || report.Verified {
		t.Fatal(report, err)
	}

	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	step := 0
	reconcile := internalClient(t, func(request *http.Request) (*http.Response, error) {
		step++
		if step == 1 {
			return internalResponse(201, `{"pit_id":"pit"}`), nil
		}
		return internalResponse(200, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"sort":["id"]}]}}`), nil
	}, resolver, nil)
	if _, err := reconcile.Read(t.Context(), "t", "events", "", 1); err == nil {
		t.Fatal("versionless reconciliation record accepted")
	}
}

func TestRemainingResilienceOutcomes(t *testing.T) {
	now := time.Now()
	newController := func() *resilienceController {
		controller, _ := newResilienceController(ResilienceConfig{MaximumInFlight: 1, MaximumQueued: 1, MaximumQueueWait: time.Second, CircuitFailureThreshold: 1, CircuitOpenDuration: time.Second, Clock: func() time.Time { return now }})
		controller.openUntil = now.Add(-time.Second)
		controller.tokens <- struct{}{}
		return controller
	}
	noQueue := newController()
	noQueue.maximumQueued = 0
	if _, err := noQueue.acquire(t.Context()); !errors.Is(err, ErrBackpressure) || noQueue.halfOpenActive {
		t.Fatal(err)
	}
	cancelled := newController()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { _, err := cancelled.acquire(ctx); result <- err }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for cancelled.snapshot().Queued == 0 {
		select {
		case err := <-result:
			t.Fatalf("queue admission ended early: %v", err)
		case <-deadline.C:
			t.Fatal("queue admission did not become observable")
		case <-poll.C:
		}
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) || cancelled.halfOpenActive {
		t.Fatal(err)
	}
}

func TestRemainingSearchAndExecutionOutcomes(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	if _, err := client.executeContent(t.Context(), OperationInfo, http.MethodPost, "/", []byte(`{}`), "application/json", 200); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.executeWrite(t.Context(), http.MethodPut, "/", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	closed := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	_ = closed.Close()
	if _, _, err := closed.executeWrite(t.Context(), http.MethodPut, "/", nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := encodeQuery(nil, nil); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatal(err)
	}

	request := search.Request{Tenant: "t", Index: "events", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.CursorPage{Size: 1, KeepAlive: time.Minute}}
	steps := 0
	pitClient := internalClient(t, func(request *http.Request) (*http.Response, error) {
		steps++
		if steps == 1 {
			return internalResponse(201, `{"pit_id":"pit"}`), nil
		}
		if request.Method == http.MethodDelete {
			return internalResponse(200, `{"pits":[{"pit_id":"rotated","successful":true}]}`), nil
		}
		return internalResponse(200, `{"took":1,"timed_out":false,"pit_id":"rotated","_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`), nil
	}, resolver, nil)
	if _, err := pitClient.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}

	limited := internalClient(t, routeBody(`{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`, 200), resolver, nil)
	limited.search.Limits.MaxPages = 1
	fingerprint, err := search.RequestFingerprint(request, limited.search.Limits)
	if err != nil {
		t.Fatal(err)
	}
	binding := search.CursorBinding{Tenant: request.Tenant, Index: request.Index, QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint"}
	cursor, err := limited.search.CursorCodec.Encode(binding, search.CursorState{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"previous"`)}, Page: 1, Items: 1, ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: cursor}
	if _, err := limited.Search(t.Context(), request); !errors.Is(err, search.ErrPageLimit) {
		t.Fatal(err)
	}

	for _, state := range []search.CursorState{
		{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"previous"`)}, Items: limited.search.Limits.MaxPages * limited.search.Limits.MaxPageItems, ExpiresAt: time.Now().Add(time.Minute)},
		{PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"previous"`)}, Bytes: limited.search.Limits.MaxResultBytes, ExpiresAt: time.Now().Add(time.Minute)},
	} {
		stateCursor, encodeErr := limited.search.CursorCodec.Encode(binding, state)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: stateCursor}
		if _, searchErr := limited.Search(t.Context(), request); !errors.Is(searchErr, search.ErrPageLimit) {
			t.Fatal(searchErr)
		}
	}

	oversizedSort := strings.Repeat("x", 5000)
	cursorSteps := 0
	tooLargeCursor := internalClient(t, func(request *http.Request) (*http.Response, error) {
		cursorSteps++
		if cursorSteps == 1 {
			return internalResponse(201, `{"pit_id":"pit"}`), nil
		}
		return internalResponse(200, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["`+oversizedSort+`"]}]}}`), nil
	}, resolver, nil)
	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute}
	if _, err := tooLargeCursor.Search(t.Context(), request); !errors.Is(err, search.ErrInvalidCursor) {
		t.Fatal(err)
	}

	invalidResult := internalClient(t, routeBody(`{"took":1,"timed_out":false,"_shards":{"total":1,"successful":0,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`, 200), resolver, nil)
	request.Page = search.OffsetPage{Size: 1}
	if _, err := invalidResult.Search(t.Context(), request); err == nil {
		t.Fatal(err)
	} else if failure := new(Failure); !errors.As(err, &failure) || failure.Category != FailureMalformed {
		t.Fatal(err)
	}
}
