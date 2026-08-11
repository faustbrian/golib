package opensearch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestSearchFailsClosedBeforeResolutionWithoutAuthorization(t *testing.T) {
	t.Parallel()

	resolverCalls, transportCalls := 0, 0
	client := authorizationClient(t, nil, &resolverCalls, &transportCalls)
	capabilities, err := client.Capabilities(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.RawExtensions {
		t.Fatal("raw extensions advertised without an authorizer")
	}
	_, err = client.Search(t.Context(), authorizationRequest(search.MatchAllQuery{}, search.Projection{}))
	if !errors.Is(err, adapter.ErrSearchDenied) {
		t.Fatalf("Search() error = %v, want ErrSearchDenied", err)
	}
	if resolverCalls != 0 || transportCalls != 0 {
		t.Fatalf("denied search reached resolver/transport: %d/%d", resolverCalls, transportCalls)
	}
}

func TestSearchAuthorizationReceivesExactRawQueryAndSourceScope(t *testing.T) {
	t.Parallel()

	denied := errors.New("policy denied")
	var got adapter.SearchAuthorization
	resolverCalls, transportCalls := 0, 0
	client := authorizationClient(t, adapter.SearchAuthorizerFunc(func(_ context.Context, authorization adapter.SearchAuthorization) error {
		got = authorization
		return denied
	}), &resolverCalls, &transportCalls)
	raw := search.RawExtensionQuery{Adapter: "opensearch", Payload: json.RawMessage(`{"script":{"source":"forbidden"}}`)}
	request := authorizationRequest(raw, search.Projection{Includes: []string{"public"}, Excludes: []string{"private"}})
	_, err := client.Search(t.Context(), request)
	if !errors.Is(err, adapter.ErrSearchDenied) {
		t.Fatalf("Search() error = %v, want ErrSearchDenied", err)
	}
	if got.Tenant != request.Tenant || got.Index != request.Index || got.FullSource ||
		len(got.Projection.Includes) != 1 || got.Projection.Includes[0] != "public" ||
		len(got.Projection.Excludes) != 1 || got.Projection.Excludes[0] != "private" {
		t.Fatalf("authorization = %#v", got)
	}
	gotRaw, ok := got.Query.(search.RawExtensionQuery)
	if !ok || gotRaw.Adapter != raw.Adapter || string(gotRaw.Payload) != string(raw.Payload) {
		t.Fatalf("authorized raw query = %#v", got.Query)
	}
	if resolverCalls != 0 || transportCalls != 0 {
		t.Fatalf("denied search reached resolver/transport: %d/%d", resolverCalls, transportCalls)
	}
}

func TestSearchAuthorizationDistinguishesFullSourceAndExplicitProjection(t *testing.T) {
	t.Parallel()

	var authorizations []adapter.SearchAuthorization
	resolverCalls, transportCalls := 0, 0
	client := authorizationClient(t, adapter.SearchAuthorizerFunc(func(_ context.Context, authorization adapter.SearchAuthorization) error {
		authorizations = append(authorizations, authorization)
		return nil
	}), &resolverCalls, &transportCalls)
	for _, projection := range []search.Projection{{}, {Includes: []string{"public"}}} {
		if _, err := client.Search(t.Context(), authorizationRequest(search.MatchAllQuery{}, projection)); err != nil {
			t.Fatal(err)
		}
	}
	if len(authorizations) != 2 || !authorizations[0].FullSource || authorizations[1].FullSource {
		t.Fatalf("authorizations = %#v", authorizations)
	}
	if resolverCalls != 2 || transportCalls != 2 {
		t.Fatalf("allowed searches resolver/transport = %d/%d", resolverCalls, transportCalls)
	}
}

func TestSearchAuthorizationReceivesBoundedPaginationIntent(t *testing.T) {
	t.Parallel()

	var got []adapter.SearchPaginationAuthorization
	resolverCalls, transportCalls := 0, 0
	client := authorizationClient(t, adapter.SearchAuthorizerFunc(func(_ context.Context, authorization adapter.SearchAuthorization) error {
		got = append(got, authorization.Pagination)
		return nil
	}), &resolverCalls, &transportCalls)

	offset := authorizationRequest(search.MatchAllQuery{}, search.Projection{})
	offset.Page = search.OffsetPage{Size: 7, Offset: 21}
	if _, err := client.Search(t.Context(), offset); err != nil {
		t.Fatal(err)
	}
	cursor := authorizationRequest(search.MatchAllQuery{}, search.Projection{})
	cursor.Page = search.CursorPage{Size: 9, Cursor: "opaque-sensitive-token", KeepAlive: 2 * time.Minute}
	if _, err := client.Search(t.Context(), cursor); err == nil {
		t.Fatal("invalid continuation unexpectedly reached a successful search")
	}

	want := []adapter.SearchPaginationAuthorization{
		{Kind: adapter.SearchPaginationOffset, Size: 7, Offset: 21},
		{Kind: adapter.SearchPaginationCursor, Size: 9, KeepAlive: 2 * time.Minute, Continuation: true},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("pagination authorizations = %#v, want %#v", got, want)
	}
}

func TestSearchAuthorizationCannotMutateExecutedIntent(t *testing.T) {
	t.Parallel()

	var executed map[string]any
	client := authorizationMutationClient(t,
		adapter.SearchAuthorizerFunc(func(_ context.Context, authorization adapter.SearchAuthorization) error {
			query := authorization.Query.(search.BoolQuery)
			query.Must[0] = search.MatchAllQuery{}
			raw := query.Filter[0].(search.RawExtensionQuery)
			raw.Payload[2] = 'x'
			fullText := query.Should[0].(search.FullTextQuery)
			fullText.Fields[0] = "private"
			ranges := authorization.Aggregations["prices"].(search.RangeAggregation)
			ranges.Buckets[0].Key = "mutated"
			return nil
		}),
		func(body []byte) {
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.UseNumber()
			if err := decoder.Decode(&executed); err != nil {
				t.Fatal(err)
			}
		},
	)
	one, err := search.NumberValue("1")
	if err != nil {
		t.Fatal(err)
	}
	request := authorizationRequest(search.BoolQuery{
		Must:   []search.Query{search.TermQuery{Field: "scope", Value: search.StringValue("public")}},
		Filter: []search.Query{search.RawExtensionQuery{Adapter: "opensearch", Payload: json.RawMessage(`{"term":{"status":"active"}}`)}},
		Should: []search.Query{search.FullTextQuery{Fields: []string{"title"}, Text: "query"}},
	}, search.Projection{})
	request.Aggregations = map[string]search.Aggregation{
		"prices": search.RangeAggregation{Field: "price", Buckets: []search.RangeBucket{{Key: "low", To: &one}}},
	}
	if _, err := client.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(executed)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"scope"`, `"status"`, `"active"`, `"title"`, `"low"`} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("executed body lost authorized intent %s: %s", want, body)
		}
	}
	for _, denied := range []string{`"private"`, `"mutated"`, `"xerm"`} {
		if bytes.Contains(body, []byte(denied)) {
			t.Fatalf("authorizer mutation reached execution %s: %s", denied, body)
		}
	}
}

func TestCallerCannotMutateSearchIntentAfterAuthorizationSnapshot(t *testing.T) {
	t.Parallel()

	entered, release := make(chan struct{}), make(chan struct{})
	var executed map[string]any
	client := authorizationMutationClient(t,
		adapter.SearchAuthorizerFunc(func(context.Context, adapter.SearchAuthorization) error {
			close(entered)
			<-release
			return nil
		}),
		func(body []byte) {
			if err := json.Unmarshal(body, &executed); err != nil {
				t.Fatal(err)
			}
		},
	)
	raw := json.RawMessage(`{"term":{"status":"active"}}`)
	request := authorizationRequest(search.RawExtensionQuery{Adapter: "opensearch", Payload: raw}, search.Projection{Includes: []string{"public"}})
	result := make(chan error, 1)
	go func() {
		_, err := client.Search(t.Context(), request)
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("search authorizer was not reached")
	}
	copy(raw, `{"term":{"status":"denied"}}`)
	request.Projection.Includes[0] = "private"
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(executed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"active"`)) || !bytes.Contains(body, []byte(`"public"`)) ||
		bytes.Contains(body, []byte(`"denied"`)) || bytes.Contains(body, []byte(`"private"`)) {
		t.Fatalf("caller mutation reached executed search: %s", body)
	}
}

func authorizationRequest(query search.Query, projection search.Projection) search.Request {
	return search.Request{
		Tenant: "tenant-a", Index: "events", Query: query, Projection: projection,
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: 10},
	}
}

func allowSearchAuthorization() adapter.SearchAuthorizer {
	return adapter.SearchAuthorizerFunc(func(context.Context, adapter.SearchAuthorization) error { return nil })
}

func allowWriteAuthorization() adapter.WriteGuard {
	return adapter.WriteGuardFunc(func(context.Context, adapter.WriteAuthorization) error { return nil })
}

func authorizationClient(t *testing.T, authorizer adapter.SearchAuthorizer, resolverCalls, transportCalls *int) *adapter.Client {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			*transportCalls++
			body := `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: codec, Clock: time.Now, Authorizer: authorizer,
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				*resolverCalls++
				return adapter.IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition-v1"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func authorizationMutationClient(t *testing.T, authorizer adapter.SearchAuthorizer, inspect func([]byte)) *adapter.Client {
	t.Helper()
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), time.Now, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			inspect(body)
			response := `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`
			if bytes.Contains(body, []byte(`"aggs"`)) {
				response = `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]},"aggregations":{"prices":{"buckets":[]}}}`
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: codec, Clock: time.Now, Authorizer: authorizer,
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "definition-v1"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
