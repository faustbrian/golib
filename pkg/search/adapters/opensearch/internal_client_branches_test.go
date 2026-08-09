package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

type internalResolver struct {
	target IndexTarget
	err    error
}

func (r internalResolver) Resolve(context.Context, string, string, IndexAccess) (IndexTarget, error) {
	return r.target, r.err
}

func internalClient(t *testing.T, handler func(*http.Request) (*http.Response, error), resolver IndexResolver, authorize LifecycleAuthorizer) *Client {
	t.Helper()
	config := Config{Endpoints: []string{"https://search.example"}, Transport: internalRoundTripFunc(handler), TransportOwnership: TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 1 << 20}
	if resolver != nil {
		codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
		if err != nil {
			t.Fatal(err)
		}
		config.Search = &SearchConfig{Limits: search.DefaultLimits(), CursorCodec: codec, Resolver: resolver, Clock: time.Now}
	}
	if authorize != nil {
		config.Lifecycle = &LifecycleConfig{Authorizer: authorize}
	}
	client, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func routeBody(body string, status int) func(*http.Request) (*http.Response, error) {
	return func(*http.Request) (*http.Response, error) { return internalResponse(status, body), nil }
}

func TestInternalClientCapabilitiesWriteAndBulkFailures(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, routeBody(`{}`, 200), nil, nil)
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := client.Capabilities(nil); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // Explicit nil-context contract.
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Capabilities(ctx); err == nil {
		t.Fatal("cancel accepted")
	}
	if _, err := client.Capabilities(t.Context()); !errors.Is(err, ErrSearchDisabled) {
		t.Fatal(err)
	}
	closed := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	_ = closed.Close()
	if _, err := closed.Capabilities(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	client = internalClient(t, routeBody(`{}`, 200), resolver, nil)
	if _, err := client.Capabilities(t.Context()); err != nil {
		t.Fatal(err)
	}

	doc, _ := search.NewDocument("t", "events", "id", 1, json.RawMessage(`{}`), search.DefaultLimits())
	operation := search.IndexDocument(doc)
	request := search.BulkRequest{Operations: []search.WriteOperation{operation}, Refresh: search.RefreshNone}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := client.Write(nil, operation, search.RefreshNone); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // Explicit nil-context contract.
		t.Fatal(err)
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := client.Bulk(nil, request); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // Explicit nil-context contract.
		t.Fatal(err)
	}
	disabled := internalClient(t, routeBody(`{}`, 200), nil, nil)
	if _, err := disabled.Write(t.Context(), operation, search.RefreshNone); !errors.Is(err, ErrSearchDisabled) {
		t.Fatal(err)
	}
	if _, err := disabled.Bulk(t.Context(), request); !errors.Is(err, ErrSearchDisabled) {
		t.Fatal(err)
	}
	if _, err := client.Write(t.Context(), search.WriteOperation{}, search.RefreshNone); err == nil {
		t.Fatal("invalid write accepted")
	}
	if _, err := client.Bulk(t.Context(), search.BulkRequest{}); err == nil {
		t.Fatal("invalid bulk accepted")
	}
	badResolver := internalClient(t, routeBody(`{}`, 200), internalResolver{err: errors.New("private")}, nil)
	if _, err := badResolver.Write(t.Context(), operation, search.RefreshNone); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if _, err := badResolver.Bulk(t.Context(), request); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	badTarget := internalClient(t, routeBody(`{}`, 200), internalResolver{target: IndexTarget{Name: "bad/name", Fingerprint: "f"}}, nil)
	if _, err := badTarget.Write(t.Context(), operation, search.RefreshNone); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if _, err := badTarget.Bulk(t.Context(), request); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}

	for _, body := range []string{"not-json", `{"_id":"other"}`} {
		malformed := internalClient(t, routeBody(body, 200), resolver, nil)
		outcome, err := malformed.Write(t.Context(), operation, search.RefreshNone)
		if err == nil || outcome.State != search.OutcomeUnknown {
			t.Fatalf("body=%q outcome=%#v err=%v", body, outcome, err)
		}
	}
	conflict := internalClient(t, routeBody(`{"_id":"id","error":{"type":"version_conflict_engine_exception"}}`, 409), resolver, nil)
	if outcome, err := conflict.Write(t.Context(), operation, search.RefreshNone); err == nil || outcome.State != search.OutcomeVersionConflict {
		t.Fatal(outcome, err)
	}
	deleteOp := search.DeleteDocument("t", "events", "id", 2)
	notFound := internalClient(t, routeBody(`{"_id":"id","result":"not_found"}`, 404), resolver, nil)
	if outcome, err := notFound.Write(t.Context(), deleteOp, search.RefreshWaitFor); err != nil || outcome.State != search.OutcomeNotFound {
		t.Fatal(outcome, err)
	}

	transportFailureClient := internalClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("network") }, resolver, nil)
	if outcome, err := transportFailureClient.Write(t.Context(), operation, search.RefreshImmediate); err == nil || outcome.State != search.OutcomeUnknown {
		t.Fatal(outcome, err)
	}
	if result, err := transportFailureClient.Bulk(t.Context(), request); err == nil || !result.HasUnknown() {
		t.Fatal(err)
	}
	for _, body := range []string{"not-json", `{"took":1,"items":[]}`} {
		malformed := internalClient(t, routeBody(body, 200), resolver, nil)
		result, err := malformed.Bulk(t.Context(), request)
		if err == nil || !result.HasUnknown() {
			t.Fatalf("body=%q err=%v", body, err)
		}
	}
	deleteRequest := search.BulkRequest{Operations: []search.WriteOperation{deleteOp}, Refresh: search.RefreshImmediate}
	bulkDelete := internalClient(t, routeBody(`{"took":1,"errors":false,"items":[{"delete":{"_id":"id","_version":2,"status":200,"result":"deleted"}}]}`, 200), resolver, nil)
	if _, err := bulkDelete.Bulk(t.Context(), deleteRequest); err != nil {
		t.Fatal(err)
	}
}

func TestInternalClientSearchFailureBranches(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	base := search.Request{Tenant: "t", Index: "events", Query: search.MatchAllQuery{}, Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}}, Page: search.OffsetPage{Size: 1}}
	disabled := internalClient(t, routeBody(`{}`, 200), nil, nil)
	if _, err := disabled.Search(t.Context(), base); !errors.Is(err, ErrSearchDisabled) {
		t.Fatal(err)
	}
	client := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	invalid := base
	invalid.Query = nil
	if _, err := client.Search(t.Context(), invalid); err == nil {
		t.Fatal("invalid query accepted")
	}
	badResolver := internalClient(t, routeBody(`{}`, 200), internalResolver{err: errors.New("x")}, nil)
	if _, err := badResolver.Search(t.Context(), base); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	badTarget := internalClient(t, routeBody(`{}`, 200), internalResolver{target: IndexTarget{Name: "bad/name", Fingerprint: "f"}}, nil)
	if _, err := badTarget.Search(t.Context(), base); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	locale := base
	locale.Query = search.FullTextQuery{Fields: []string{"name"}, Text: "x", Locale: "fi"}
	if _, err := client.Search(t.Context(), locale); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	statusClient := internalClient(t, routeBody(`{"error":{"type":"rejected_execution_exception"}}`, 503), resolver, nil)
	if _, err := statusClient.Search(t.Context(), base); err == nil {
		t.Fatal("status accepted")
	}
	malformed := internalClient(t, routeBody(`not-json`, 200), resolver, nil)
	if _, err := malformed.Search(t.Context(), base); err == nil {
		t.Fatal("malformed accepted")
	}
	tooLarge := internalClient(t, routeBody(`{"took":0,"_shards":{"total":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`, 200), resolver, nil)
	tooLarge.search.Limits.MaxResultBytes = 1
	if _, err := tooLarge.Search(t.Context(), base); err == nil {
		t.Fatal("large result accepted")
	}
	invalidHit := internalClient(t, routeBody(`{"took":1,"_shards":{"total":1,"successful":1},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"","_id":""}]}}`, 200), resolver, nil)
	if _, err := invalidHit.Search(t.Context(), base); err == nil {
		t.Fatal("invalid result accepted")
	}

	cursorRequest := base
	cursorRequest.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: "invalid"}
	if _, err := client.Search(t.Context(), cursorRequest); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	pitMalformed := internalClient(t, routeBody(`{}`, 200), resolver, nil)
	cursorRequest.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute}
	if _, err := pitMalformed.Search(t.Context(), cursorRequest); err == nil {
		t.Fatal("malformed PIT accepted")
	}
	pitFailed := internalClient(t, routeBody(`{}`, 500), resolver, nil)
	if _, err := pitFailed.Search(t.Context(), cursorRequest); err == nil {
		t.Fatal("PIT status accepted")
	}
	paths := 0
	pitStatus := internalClient(t, func(request *http.Request) (*http.Response, error) {
		paths++
		if paths == 1 {
			return internalResponse(201, `{"pit_id":"pit"}`), nil
		}
		return internalResponse(500, `{}`), nil
	}, resolver, nil)
	if _, err := pitStatus.Search(t.Context(), cursorRequest); err == nil {
		t.Fatal("search status accepted")
	}
}

func TestInternalLifecycleTemplateAndOperationalFailures(t *testing.T) {
	authorize := LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil })
	client := internalClient(t, routeBody(`{}`, 200), nil, authorize)
	definition, _ := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err := client.CreateIndex(t.Context(), "", definition); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if _, _, err := client.Reindex(t.Context(), "", "a", "b", ""); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if _, err := client.VerifyIndex(t.Context(), "", "a", "b"); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if _, err := client.ResolveAlias(t.Context(), "", "alias"); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if err := client.SwapAlias(t.Context(), "t", "alias", "same", "same"); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if err := client.AddAlias(t.Context(), "", "alias", "index", false); err == nil {
		t.Fatal("alias accepted")
	}
	if err := client.DeleteIndex(t.Context(), "", "index"); !errors.Is(err, ErrUnsafeIndexTarget) {
		t.Fatal(err)
	}
	if err := client.PutIndexTemplate(t.Context(), "t", "bad/name", []string{"x-*"}, 0, definition); err == nil {
		t.Fatal("template accepted")
	}
	if err := client.DeleteIndexTemplate(t.Context(), "t", "bad/name"); err == nil {
		t.Fatal("template delete accepted")
	}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if err := client.authorizeLifecycle(nil, "t"); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // nil-context validation is the contract under test.
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.authorizeLifecycle(ctx, "t"); err == nil {
		t.Fatal("cancel auth accepted")
	}
	disabled := internalClient(t, routeBody(`{}`, 200), nil, nil)
	if err := disabled.authorizeLifecycle(t.Context(), "t"); !errors.Is(err, ErrLifecycleDisabled) {
		t.Fatal(err)
	}
	denied := internalClient(t, routeBody(`{}`, 200), nil, LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return errors.New("private") }))
	if err := denied.authorizeLifecycle(t.Context(), "t", "x"); !errors.Is(err, ErrLifecycleDenied) {
		t.Fatal(err)
	}
	for _, call := range []func() error{
		func() error { return denied.CreateIndex(t.Context(), "t", definition) },
		func() error { _, _, err := denied.Reindex(t.Context(), "t", "a", "b", ""); return err },
		func() error { _, err := denied.VerifyIndex(t.Context(), "t", "a", "b"); return err },
		func() error { _, err := denied.ResolveAlias(t.Context(), "t", "alias"); return err },
		func() error { return denied.SwapAlias(t.Context(), "t", "alias", "a", "b") },
		func() error { return denied.AddAlias(t.Context(), "t", "alias", "a", false) },
		func() error { return denied.DeleteIndex(t.Context(), "t", "a") },
		func() error {
			return denied.PutIndexTemplate(t.Context(), "t", "template", []string{"events-*"}, 0, definition)
		},
		func() error { return denied.DeleteIndexTemplate(t.Context(), "t", "template") },
	} {
		if !errors.Is(call(), ErrLifecycleDenied) {
			t.Fatal("denied lifecycle call accepted")
		}
	}
	failed := internalClient(t, routeBody(`{}`, 500), nil, authorize)
	for _, call := range []func() error{
		func() error { return failed.CreateIndex(t.Context(), "t", definition) },
		func() error { _, _, err := failed.Reindex(t.Context(), "t", "a", "b", ""); return err },
		func() error { _, _, err := failed.Reindex(t.Context(), "t", "a", "b", "task"); return err },
		func() error { _, err := failed.VerifyIndex(t.Context(), "t", "a", "b"); return err },
		func() error { _, err := failed.ResolveAlias(t.Context(), "t", "alias"); return err },
		func() error { return failed.SwapAlias(t.Context(), "t", "alias", "a", "b") },
		func() error { return failed.AddAlias(t.Context(), "t", "alias", "a", false) },
		func() error { return failed.DeleteIndex(t.Context(), "t", "a") },
		func() error {
			return failed.PutIndexTemplate(t.Context(), "t", "template", []string{"events-*"}, 0, definition)
		},
		func() error { return failed.DeleteIndexTemplate(t.Context(), "t", "template") },
	} {
		if call() == nil {
			t.Fatal("failed lifecycle response accepted")
		}
	}

	for _, body := range []string{"not-json", `{"acknowledged":false}`} {
		bad := internalClient(t, routeBody(body, 200), nil, authorize)
		if err := bad.CreateIndex(t.Context(), "t", definition); err == nil {
			t.Fatal("create response accepted")
		}
		if err := bad.SwapAlias(t.Context(), "t", "alias", "a", "b"); err == nil {
			t.Fatal("swap accepted")
		}
		if err := bad.DeleteIndex(t.Context(), "t", "index"); err == nil {
			t.Fatal("delete accepted")
		}
		if err := bad.PutIndexTemplate(t.Context(), "t", "template", []string{"events-*"}, 0, definition); err == nil {
			t.Fatal("template response accepted")
		}
	}
	if _, _, err := client.Reindex(t.Context(), "t", "a", "b", strings.Repeat("x", 513)); !errors.Is(err, ErrLifecycleRejected) {
		t.Fatal(err)
	}
	for _, body := range []string{"not-json", `{"task":""}`} {
		bad := internalClient(t, routeBody(body, 200), nil, authorize)
		if _, _, err := bad.Reindex(t.Context(), "t", "a", "b", ""); err == nil {
			t.Fatal("reindex start accepted")
		}
	}
	for _, body := range []string{"not-json", `{"completed":true}`, `{"completed":false}`} {
		bad := internalClient(t, routeBody(body, 200), nil, authorize)
		_, done, err := bad.Reindex(t.Context(), "t", "a", "b", "task")
		if body == `{"completed":false}` {
			if err != nil || done {
				t.Fatal(done, err)
			}
		} else if err == nil {
			t.Fatal("reindex task accepted")
		}
	}
	for _, body := range []string{"not-json", `{"count":1,"_shards":{"total":1,"successful":0,"failed":1}}`} {
		bad := internalClient(t, routeBody(body, 200), nil, authorize)
		if _, err := bad.countIndex(t.Context(), "a"); err == nil {
			t.Fatal("count accepted")
		}
	}
	for _, body := range []string{"not-json", `{}`, `{"bad/name":{"aliases":{"alias":{}}}}`, `{"index":{"aliases":{}}}`} {
		bad := internalClient(t, routeBody(body, 200), nil, authorize)
		if _, err := bad.ResolveAlias(t.Context(), "t", "alias"); err == nil {
			t.Fatalf("alias body %q accepted", body)
		}
	}
	if err := client.PutIndexTemplate(t.Context(), "t", "template", []string{"bad pattern"}, 0, definition); err == nil {
		t.Fatal("bad pattern accepted")
	}
	emptyTemplate := internalClient(t, routeBody("", 404), nil, authorize)
	if err := emptyTemplate.DeleteIndexTemplate(t.Context(), "t", "template"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"a/b", "a\\b", "a?b", "a#b", "a\x00b", "a\rb", "a\nb"} {
		if !containsUnsafePath(value) {
			t.Fatalf("unsafe path %q", value)
		}
	}
	versions := SupportedOpenSearchVersions()
	versions[0] = "changed"
	if SupportedOpenSearchVersions()[0] == "changed" {
		t.Fatal("versions alias")
	}
}

func TestInternalHealthCapacityAndReconciliationFailures(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	for _, body := range []string{"not-json", `{"cluster_name":"","status":"green"}`, `{"cluster_name":"x","status":"bad"}`, `{"cluster_name":"x","status":"green","number_of_nodes":-1}`} {
		client := internalClient(t, routeBody(body, 200), resolver, nil)
		if _, err := client.Health(t.Context()); err == nil {
			t.Fatalf("health %q accepted", body)
		}
	}
	status := internalClient(t, routeBody(`{}`, 500), resolver, nil)
	if _, err := status.Health(t.Context()); err == nil {
		t.Fatal("health status accepted")
	}
	for _, body := range []string{"not-json", `{"_nodes":{"total":0}}`} {
		client := internalClient(t, routeBody(body, 200), resolver, nil)
		if _, err := client.Capacity(t.Context()); err == nil {
			t.Fatalf("capacity %q accepted", body)
		}
	}
	cluster := `{"_nodes":{"total":1,"successful":1},"indices":{"count":1,"shards":{"total":1,"primaries":1}},"nodes":{"count":{"total":1,"data":1}}}`
	sequence := 0
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		sequence++
		if sequence == 1 {
			return internalResponse(200, cluster), nil
		}
		return internalResponse(200, `not-json`), nil
	}, resolver, nil)
	if _, err := client.Capacity(t.Context()); err == nil {
		t.Fatal("node capacity accepted")
	}
	sequence = 0
	secondStatus := internalClient(t, func(*http.Request) (*http.Response, error) {
		sequence++
		if sequence == 1 {
			return internalResponse(200, cluster), nil
		}
		return internalResponse(500, `{}`), nil
	}, resolver, nil)
	if _, err := secondStatus.Capacity(t.Context()); err == nil {
		t.Fatal("node status accepted")
	}

	partialBody := `{"took":1,"timed_out":true,"_shards":{"total":1,"successful":1},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`
	sequence = 0
	partial := internalClient(t, func(request *http.Request) (*http.Response, error) {
		sequence++
		if sequence == 1 {
			return internalResponse(201, `{"pit_id":"pit"}`), nil
		}
		if request.Method == http.MethodDelete {
			return internalResponse(200, `{}`), nil
		}
		return internalResponse(200, partialBody), nil
	}, resolver, nil)
	if _, err := partial.Read(t.Context(), "t", "events", "", 1); !errors.Is(err, ErrPartialResults) {
		t.Fatal(err)
	}
	if _, err := status.Read(t.Context(), "t", "events", "", 1); err == nil {
		t.Fatal("read status accepted")
	}
	malformedHitBody := `{"took":1,"_shards":{"total":1,"successful":1},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":0,"_source":{}}]}}`
	sequence = 0
	malformedHit := internalClient(t, func(request *http.Request) (*http.Response, error) {
		sequence++
		if sequence == 1 {
			return internalResponse(201, `{"pit_id":"pit"}`), nil
		}
		if request.Method == http.MethodDelete {
			return internalResponse(200, `{}`), nil
		}
		return internalResponse(200, malformedHitBody), nil
	}, resolver, nil)
	if _, err := malformedHit.Read(t.Context(), "t", "events", "", 1); err == nil {
		t.Fatal("malformed reconciliation hit accepted")
	}
}

func TestInternalInfoTransportBodyFailures(t *testing.T) {
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
	late := internalClient(t, func(*http.Request) (*http.Response, error) { return internalResponse(200, `{}`), errors.New("late") }, resolver, nil)
	if _, err := late.Info(t.Context()); err == nil {
		t.Fatal("late error accepted")
	}
	readFailure := internalClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(errorReader{})}, nil
	}, resolver, nil)
	if _, err := readFailure.Info(t.Context()); err == nil {
		t.Fatal("read error accepted")
	}
}

func TestInternalDiscoveryFailureBranches(t *testing.T) {
	client := internalClient(t, routeBody(`{}`, 200), nil, nil)
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := client.Discover(nil); !errors.Is(err, ErrContextRequired) { //nolint:staticcheck // nil-context validation is the contract under test.
		t.Fatal(err)
	}
	if _, err := client.Discover(t.Context()); !errors.Is(err, ErrDiscoveryDisabled) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client.discovery = DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: []string{".example"}}
	if _, err := client.Discover(ctx); err == nil {
		t.Fatal("cancel discovery accepted")
	}
	policy := DiscoveryPolicy{MaximumNodes: 1, AllowedDNSSuffixes: []string{".example"}}
	for _, test := range []struct {
		body         string
		status       int
		transportErr error
	}{
		{`{}`, 500, nil}, {`not-json`, 200, nil}, {`{"nodes":{}}`, 200, nil},
		{`{"nodes":{"a":{"roles":["data"],"http":{"publish_address":"other.test:9200"}}}}`, 200, nil},
		{`{"nodes":{"a":{"roles":["data"],"http":{"publish_address":"a.example:9200"}},"b":{"roles":["data"],"http":{"publish_address":"b.example:9200"}}}}`, 200, nil},
		{"", 0, errors.New("network")},
	} {
		discovery := internalClient(t, func(*http.Request) (*http.Response, error) {
			if test.transportErr != nil {
				return nil, test.transportErr
			}
			return internalResponse(test.status, test.body), nil
		}, nil, nil)
		discovery.discovery = policy
		if _, err := discovery.Discover(t.Context()); err == nil {
			t.Fatalf("discovery %#v accepted", test)
		}
	}
	late := internalClient(t, func(*http.Request) (*http.Response, error) { return internalResponse(200, `{}`), errors.New("late") }, nil, nil)
	late.discovery = policy
	if _, err := late.Discover(t.Context()); err == nil {
		t.Fatal("late discovery accepted")
	}
	readFailure := internalClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(errorReader{})}, nil
	}, nil, nil)
	readFailure.discovery = policy
	if _, err := readFailure.Discover(t.Context()); err == nil {
		t.Fatal("read discovery accepted")
	}
	zero := internalClient(t, routeBody(`{"nodes":{"a":{"roles":["cluster_manager"],"http":{"publish_address":"a.example:9200"}}}}`, 200), nil, nil)
	zero.discovery = policy
	if _, err := zero.Discover(t.Context()); err == nil {
		t.Fatal("empty topology accepted")
	}
	excluded := internalClient(t, routeBody(`{"nodes":{"a":{"roles":["cluster_manager"],"http":{"publish_address":"a.example:9200"}},"b":{"roles":["data"]},"c":{"roles":["data"],"http":{"publish_address":"c.example:9200"}}}}`, 200), nil, nil)
	excluded.discovery = policy
	if result, err := excluded.Discover(t.Context()); err != nil || result.Excluded != 2 || result.Discovered != 1 {
		t.Fatal(result, err)
	}
}
