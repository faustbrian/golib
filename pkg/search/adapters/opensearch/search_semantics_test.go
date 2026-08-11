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

func TestEncodeBoolPreservesZeroMinimumShouldMatchSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       search.BoolQuery
		wantPresent bool
	}{
		{
			name:  "empty bool",
			query: search.BoolQuery{},
		},
		{
			name: "must only",
			query: search.BoolQuery{
				Must: []search.Query{search.MatchAllQuery{}},
			},
		},
		{
			name:        "should only",
			query:       search.BoolQuery{Should: []search.Query{search.MatchAllQuery{}}},
			wantPresent: true,
		},
		{
			name: "should and must not",
			query: search.BoolQuery{
				Should:  []search.Query{search.MatchAllQuery{}},
				MustNot: []search.Query{search.TermQuery{Field: "hidden", Value: search.BoolValue(true)}},
			},
			wantPresent: true,
		},
		{
			name: "must and should",
			query: search.BoolQuery{
				Must:   []search.Query{search.MatchAllQuery{}},
				Should: []search.Query{search.MatchAllQuery{}},
			},
		},
		{
			name: "filter and should",
			query: search.BoolQuery{
				Filter: []search.Query{search.MatchAllQuery{}},
				Should: []search.Query{search.MatchAllQuery{}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := encodeQuery(test.query, nil)
			if err != nil {
				t.Fatal(err)
			}
			outer, ok := encoded.(map[string]any)
			if !ok {
				t.Fatalf("encoded query type = %T", encoded)
			}
			boolBody, ok := outer["bool"].(map[string]any)
			if !ok {
				t.Fatalf("bool query = %#v", outer["bool"])
			}
			minimum, present := boolBody["minimum_should_match"]
			if present != test.wantPresent {
				t.Fatalf("minimum_should_match presence = %v, want %v: %#v", present, test.wantPresent, boolBody)
			}
			if present && minimum != 0 {
				t.Fatalf("minimum_should_match = %#v, want 0", minimum)
			}
		})
	}
}

func TestCursorSearchRejectsPartialPagesAndClosesOwnedPIT(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		continuing bool
		response   string
	}{
		{
			name:     "timed out",
			response: `{"took":1,"timed_out":true,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		{
			name:       "failed shard while continuing",
			continuing: true,
			response:   `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":0,"skipped":0,"failed":1,"failures":[{"reason":{"type":"query_shard_exception"}}]},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deleted := 0
			resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
			client := internalClient(t, func(request *http.Request) (*http.Response, error) {
				switch {
				case request.Method == http.MethodPost && request.URL.Path == "/events-v1/_search/point_in_time":
					return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
				case request.Method == http.MethodPost && request.URL.Path == "/_search":
					return internalResponse(http.StatusOK, test.response), nil
				case request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time":
					deleted++
					return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit","successful":true}]}`), nil
				default:
					return internalResponse(http.StatusNotFound, `{}`), nil
				}
			}, resolver, nil)

			request := search.Request{
				Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
				Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
				Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
			}
			if test.continuing {
				fingerprint, err := search.RequestFingerprint(request, client.search.Limits)
				if err != nil {
					t.Fatal(err)
				}
				binding := search.CursorBinding{
					Tenant: request.Tenant, Index: request.Index,
					QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint",
				}
				cursor, err := client.search.CursorCodec.Encode(binding, search.CursorState{
					PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"previous"`)},
					ExpiresAt: time.Now().Add(time.Minute),
				})
				if err != nil {
					t.Fatal(err)
				}
				request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: cursor}
			}

			if _, err := client.Search(t.Context(), request); !errors.Is(err, ErrPartialResults) {
				t.Fatalf("Search() error = %v, want ErrPartialResults", err)
			}
			if deleted != 1 {
				t.Fatalf("PIT deletes = %d, want 1", deleted)
			}
		})
	}
}

func TestCursorSearchClosesPITWhenAFullPageIsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "empty hit id",
			response: `{"took":1,"pit_id":"rotated","_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		{
			name:     "empty hit index",
			response: `{"took":1,"pit_id":"rotated","_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		{
			name:     "invalid result diagnostics",
			response: `{"took":1,"pit_id":"rotated","_shards":{"total":1,"successful":0,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		{
			name:     "trailing response data",
			response: `{"took":1,"pit_id":"rotated","_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}} {}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deleted := []string{}
			resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
			client := internalClient(t, func(request *http.Request) (*http.Response, error) {
				switch request.Method {
				case http.MethodPost:
					if request.URL.Path == "/events-v1/_search/point_in_time" {
						return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
					}
					return internalResponse(http.StatusOK, test.response), nil
				case http.MethodDelete:
					var deletion struct {
						PITID string `json:"pit_id"`
					}
					if err := json.NewDecoder(request.Body).Decode(&deletion); err != nil {
						t.Fatal(err)
					}
					deleted = append(deleted, deletion.PITID)
					return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"rotated","successful":true}]}`), nil
				default:
					return internalResponse(http.StatusNotFound, `{}`), nil
				}
			}, resolver, nil)

			request := search.Request{
				Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
				Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
				Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
			}
			_, err := client.Search(t.Context(), request)
			failure := new(Failure)
			if !errors.As(err, &failure) || failure.Category != FailureMalformed {
				t.Fatalf("Search() error = %v, want malformed failure", err)
			}
			if len(deleted) != 1 || deleted[0] != "rotated" {
				t.Fatalf("deleted PITs = %v, want [rotated]", deleted)
			}
		})
	}
}

func TestCursorSearchRejectsExhaustedTraversalBeforeDispatch(t *testing.T) {
	t.Parallel()

	requests := 0
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		return internalResponse(http.StatusOK, `{"took":1,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`), nil
	}, resolver, nil)
	client.search.Limits.MaxPages = 1
	client.search.Limits.MaxPageItems = 1

	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	fingerprint, err := search.RequestFingerprint(request, client.search.Limits)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := client.search.CursorCodec.Encode(search.CursorBinding{
		Tenant: request.Tenant, Index: request.Index,
		QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint",
	}, search.CursorState{
		PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"id"`)},
		Page: 1, Items: 1, Bytes: 1, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	request.Page = search.CursorPage{Size: 1, Cursor: cursor, KeepAlive: time.Minute}

	if _, err := client.Search(t.Context(), request); !errors.Is(err, search.ErrPageLimit) {
		t.Fatalf("Search() error = %v, want ErrPageLimit", err)
	}
	if requests != 0 {
		t.Fatalf("backend requests = %d, want 0", requests)
	}
}

func TestCursorExpiryStartsWhenPITIsCreated(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	now := started
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/events-v1/_search/point_in_time" {
			return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
		}
		now = now.Add(30 * time.Second)
		return internalResponse(http.StatusOK, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"a","_version":1,"_source":{},"sort":["a"]}]}}`), nil
	}, resolver, nil)
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client.search.CursorCodec = codec
	client.search.Clock = func() time.Time { return now }
	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}

	result, err := client.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := search.RequestFingerprint(request, client.search.Limits)
	if err != nil {
		t.Fatal(err)
	}
	state, err := codec.Decode(result.NextCursor(), search.CursorBinding{
		Tenant: request.Tenant, Index: request.Index,
		QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint",
	}, client.search.Limits)
	if err != nil {
		t.Fatal(err)
	}
	want := started.Add(time.Minute)
	if !state.ExpiresAt.Equal(want) {
		t.Fatalf("cursor expiry = %s, want PIT creation deadline %s", state.ExpiresAt, want)
	}
}

func TestDuplicateCreatedPointInTimeDoesNotDeleteExistingCursor(t *testing.T) {
	t.Parallel()

	creates, searches, deletes := 0, 0, 0
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/events-v1/_search/point_in_time":
			creates++
			return internalResponse(http.StatusCreated, `{"pit_id":"pit-a"}`), nil
		case "/_search":
			searches++
			if searches == 1 {
				return internalResponse(http.StatusOK, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"a","_version":1,"_source":{},"sort":["a"]}]}}`), nil
			}
			return internalResponse(http.StatusOK, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`), nil
		case "/_search/point_in_time":
			deletes++
			return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit-a","successful":true}]}`), nil
		default:
			t.Fatalf("unexpected request %s", request.URL.Path)
			return nil, nil
		}
	}, internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}, nil)
	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	first, err := client.Search(t.Context(), request)
	if err != nil || first.NextCursor() == "" {
		t.Fatalf("first Search() = %#v/%v", first, err)
	}
	if _, err := client.Search(t.Context(), request); err == nil || deletes != 0 {
		t.Fatalf("duplicate Search() error/deletes = %v/%d, want rejection without delete", err, deletes)
	}
	request.Page = search.CursorPage{Size: 1, Cursor: first.NextCursor(), KeepAlive: time.Minute}
	if _, err := client.Search(t.Context(), request); err != nil {
		t.Fatalf("original cursor Search() = %v", err)
	}
	if creates != 2 || searches != 2 || deletes != 1 || client.PointInTimeSnapshot().Open != 0 {
		t.Fatalf("PIT operations create/search/delete/snapshot = %d/%d/%d/%#v", creates, searches, deletes, client.PointInTimeSnapshot())
	}
}

func TestCursorContinuationIsSingleConsumerPerClient(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	searches := 0
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/events-v1/_search/point_in_time":
			return internalResponse(http.StatusCreated, `{"pit_id":"pit-a"}`), nil
		case "/_search":
			searches++
			if searches == 1 {
				return internalResponse(http.StatusOK, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"a","_version":1,"_source":{},"sort":["a"]}]}}`), nil
			}
			close(entered)
			<-release
			return internalResponse(http.StatusOK, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`), nil
		case "/_search/point_in_time":
			return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit-a","successful":true}]}`), nil
		default:
			t.Fatalf("unexpected request %s", request.URL.Path)
			return nil, nil
		}
	}, internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}, nil)
	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	first, err := client.Search(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Page = search.CursorPage{Size: 1, Cursor: first.NextCursor(), KeepAlive: time.Minute}
	result := make(chan error, 1)
	go func() { _, searchErr := client.Search(t.Context(), request); result <- searchErr }()
	<-entered
	if _, err := client.Search(t.Context(), request); !errors.Is(err, ErrPointInTimeInUse) {
		t.Fatalf("concurrent continuation = %v, want ErrPointInTimeInUse", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestCursorContinuationUsesOnlyRemainingPointInTimeLifetime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	var requestedKeepAlive string
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, func(request *http.Request) (*http.Response, error) {
		var body struct {
			PIT struct {
				KeepAlive string `json:"keep_alive"`
			} `json:"pit"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requestedKeepAlive = body.PIT.KeepAlive
		return internalResponse(http.StatusOK, `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"a","_version":1,"_source":{},"sort":["a"]}]}}`), nil
	}, resolver, nil)
	codec, err := search.NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return now }, 4096)
	if err != nil {
		t.Fatal(err)
	}
	client.search.CursorCodec = codec
	client.search.Clock = func() time.Time { return now }
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
		QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint",
	}, search.CursorState{
		PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"before"`)},
		ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: cursor}

	if _, err := client.Search(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if requestedKeepAlive != "30000ms" {
		t.Fatalf("continuation keep_alive = %q, want remaining 30000ms", requestedKeepAlive)
	}

	requestedKeepAlive = ""
	now = time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC).Add(time.Minute - 500*time.Microsecond)
	if _, err := client.Search(t.Context(), request); !errors.Is(err, search.ErrCursorExpired) {
		t.Fatalf("sub-millisecond continuation error = %v, want ErrCursorExpired", err)
	}
	if requestedKeepAlive != "" {
		t.Fatalf("expired continuation reached backend with keep_alive %q", requestedKeepAlive)
	}
}

func TestSearchClassifiesResourceNotFoundByPaginationContext(t *testing.T) {
	t.Parallel()

	response := `{"error":{"type":"resource_not_found_exception"},"status":404}`
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	base := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
	}
	t.Run("missing offset target is not an expired PIT", func(t *testing.T) {
		t.Parallel()
		client := internalClient(t, routeBody(response, http.StatusNotFound), resolver, nil)
		request := base
		request.Page = search.OffsetPage{Size: 1}
		_, err := client.Search(t.Context(), request)
		failure := new(Failure)
		if !errors.As(err, &failure) || failure.Category != FailureRejected || errors.Is(err, ErrPITExpired) {
			t.Fatalf("offset Search() error = %#v/%v, want rejected non-PIT failure", failure, err)
		}
	})
	t.Run("missing cursor context is an expired PIT", func(t *testing.T) {
		t.Parallel()
		deleted := false
		client := internalClient(t, func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodDelete {
				deleted = true
				return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit","successful":true}]}`), nil
			}
			return internalResponse(http.StatusNotFound, response), nil
		}, resolver, nil)
		request := base
		request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute}
		fingerprint, err := search.RequestFingerprint(request, client.search.Limits)
		if err != nil {
			t.Fatal(err)
		}
		cursor, err := client.search.CursorCodec.Encode(search.CursorBinding{
			Tenant: request.Tenant, Index: request.Index,
			QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint",
		}, search.CursorState{
			PointInTime: "pit", SortValues: []json.RawMessage{json.RawMessage(`"before"`)},
			ExpiresAt: time.Now().Add(time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
		request.Page = search.CursorPage{Size: 1, KeepAlive: time.Minute, Cursor: cursor}
		_, err = client.Search(t.Context(), request)
		if !errors.Is(err, ErrPITExpired) || !deleted {
			t.Fatalf("cursor Search() error/deleted = %v/%v, want expired PIT cleanup", err, deleted)
		}
	})
}

func TestCursorSearchEnforcesCumulativeBoundsAfterResponse(t *testing.T) {
	t.Parallel()

	response := `{"took":1,"timed_out":false,"pit_id":"rotated","_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":2,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"a","_version":1,"_source":{},"sort":["a"]},{"_index":"events-v1","_id":"b","_version":1,"_source":{},"sort":["b"]}]}}`
	for _, test := range []struct {
		name          string
		pageSize      int
		cursorState   func(search.Limits) search.CursorState
		wantPageLimit bool
		wantCursor    bool
		wantRequests  int
	}{
		{
			name:     "last page is allowed for a short result",
			pageSize: 3,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Page: limits.MaxPages - 1}
			},
			wantRequests: 2,
		},
		{
			name:     "exact item budget is allowed for a short result",
			pageSize: 3,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Items: limits.MaxPages*limits.MaxPageItems - 2}
			},
			wantRequests: 2,
		},
		{
			name:     "item budget overflow is rejected",
			pageSize: 3,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Items: limits.MaxPages*limits.MaxPageItems - 1}
			},
			wantPageLimit: true,
			wantRequests:  2,
		},
		{
			name:     "exact byte budget is allowed for a short result",
			pageSize: 3,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Bytes: limits.MaxResultBytes - int64(len(response))}
			},
			wantRequests: 2,
		},
		{
			name:     "byte budget overflow is rejected",
			pageSize: 3,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Bytes: limits.MaxResultBytes - int64(len(response)) + 1}
			},
			wantPageLimit: true,
			wantRequests:  2,
		},
		{
			name:     "continuation at the page limit is rejected",
			pageSize: 2,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Page: limits.MaxPages - 1}
			},
			wantPageLimit: true,
			wantRequests:  2,
		},
		{
			name:     "continuation at the item limit is rejected",
			pageSize: 2,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Items: limits.MaxPages*limits.MaxPageItems - 2}
			},
			wantPageLimit: true,
			wantRequests:  2,
		},
		{
			name:     "continuation at the byte limit is rejected",
			pageSize: 2,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{Bytes: limits.MaxResultBytes - int64(len(response))}
			},
			wantPageLimit: true,
			wantRequests:  2,
		},
		{
			name:     "continuation below every limit is allowed",
			pageSize: 2,
			cursorState: func(limits search.Limits) search.CursorState {
				return search.CursorState{
					Page:  limits.MaxPages - 2,
					Items: limits.MaxPages*limits.MaxPageItems - 3,
					Bytes: limits.MaxResultBytes - int64(len(response)) - 1,
				}
			},
			wantCursor:   true,
			wantRequests: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			requests := 0
			resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
			client := internalClient(t, func(request *http.Request) (*http.Response, error) {
				requests++
				if request.Method == http.MethodDelete {
					return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"rotated","successful":true}]}`), nil
				}
				return internalResponse(http.StatusOK, response), nil
			}, resolver, nil)
			client.search.Limits.MaxPages = 3
			client.search.Limits.MaxPageItems = 3
			client.search.Limits.MaxResultBytes = int64(len(response)) + 100

			request := search.Request{
				Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
				Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
				Page: search.CursorPage{Size: test.pageSize, KeepAlive: time.Minute},
			}
			fingerprint, err := search.RequestFingerprint(request, client.search.Limits)
			if err != nil {
				t.Fatal(err)
			}
			state := test.cursorState(client.search.Limits)
			state.PointInTime = "pit"
			state.SortValues = []json.RawMessage{json.RawMessage(`"previous"`)}
			state.ExpiresAt = time.Now().Add(time.Minute)
			cursor, err := client.search.CursorCodec.Encode(search.CursorBinding{
				Tenant: request.Tenant, Index: request.Index,
				QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint",
			}, state)
			if err != nil {
				t.Fatal(err)
			}
			request.Page = search.CursorPage{Size: test.pageSize, Cursor: cursor, KeepAlive: time.Minute}

			result, err := client.Search(t.Context(), request)
			if test.wantPageLimit {
				if !errors.Is(err, search.ErrPageLimit) {
					t.Fatalf("Search() error = %v, want ErrPageLimit", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !test.wantPageLimit && (result.NextCursor() != "") != test.wantCursor {
				t.Fatalf("Search() next cursor present = %t, want %t", result.NextCursor() != "", test.wantCursor)
			}
			if requests != test.wantRequests {
				t.Fatalf("backend requests = %d, want %d", requests, test.wantRequests)
			}
			if test.wantCursor {
				next, decodeErr := client.search.CursorCodec.Decode(result.NextCursor(), search.CursorBinding{
					Tenant: request.Tenant, Index: request.Index,
					QueryFingerprint: fingerprint, IndexFingerprint: "fingerprint",
				}, client.search.Limits)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if next.Page != client.search.Limits.MaxPages-1 ||
					next.Items != client.search.Limits.MaxPages*client.search.Limits.MaxPageItems-1 ||
					next.Bytes != client.search.Limits.MaxResultBytes-1 {
					t.Fatalf("next cursor state = %#v, want every cumulative value one below its limit", next)
				}
			}
		})
	}
}

func TestDeletePITValidatesSuccessfulResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid", body: `{"pits":[{"pit_id":"pit","successful":true}]}`},
		{name: "malformed", body: `not-json`, wantErr: true},
		{name: "missing outcome", body: `{}`, wantErr: true},
		{name: "unsuccessful", body: `{"pits":[{"pit_id":"pit","successful":false}]}`, wantErr: true},
		{name: "different pit", body: `{"pits":[{"pit_id":"other","successful":true}]}`, wantErr: true},
		{name: "multiple outcomes", body: `{"pits":[{"pit_id":"pit","successful":true},{"pit_id":"other","successful":true}]}`, wantErr: true},
		{name: "trailing json", body: `{"pits":[{"pit_id":"pit","successful":true}]} {}`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := internalClient(t, routeBody(test.body, http.StatusOK), internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}, nil)
			err := client.deletePIT(context.Background(), "pit")
			if !test.wantErr {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			failure := new(Failure)
			if !errors.As(err, &failure) || failure.Operation != OperationDeletePIT || failure.Category != FailureMalformed {
				t.Fatalf("deletePIT() error = %v, want malformed delete failure", err)
			}
		})
	}
}

func TestPITMutationsClassifyAmbiguousOutcomes(t *testing.T) {
	t.Parallel()

	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	searchResponse := `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`

	tests := []struct {
		name         string
		operation    Operation
		wantRequests int
		handler      func(*http.Request) (*http.Response, error)
	}{
		{
			name: "create transport failure", operation: OperationCreatePIT, wantRequests: 1,
			handler: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("response lost")
			},
		},
		{
			name: "create malformed success", operation: OperationCreatePIT, wantRequests: 1,
			handler: func(*http.Request) (*http.Response, error) {
				return internalResponse(http.StatusCreated, `{}`), nil
			},
		},
		{
			name: "delete transport failure", operation: OperationDeletePIT, wantRequests: 3,
			handler: func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/events-v1/_search/point_in_time":
					return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
				case "/_search":
					return internalResponse(http.StatusOK, searchResponse), nil
				default:
					return nil, errors.New("response lost")
				}
			},
		},
		{
			name: "delete malformed success", operation: OperationDeletePIT, wantRequests: 3,
			handler: func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/events-v1/_search/point_in_time":
					return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
				case "/_search":
					return internalResponse(http.StatusOK, searchResponse), nil
				default:
					return internalResponse(http.StatusOK, `{}`), nil
				}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requests := 0
			client := internalClient(t, func(request *http.Request) (*http.Response, error) {
				requests++
				return test.handler(request)
			}, resolver, nil)

			_, err := client.Search(t.Context(), request)
			failure := new(Failure)
			if !errors.As(err, &failure) || failure.Operation != test.operation || failure.OutcomeKnown {
				t.Fatalf("Search() error = %#v / %v, want unknown %s outcome", failure, err, test.operation)
			}
			if requests != test.wantRequests {
				t.Fatalf("backend requests = %d, want %d", requests, test.wantRequests)
			}
		})
	}
}

func TestSearchRejectsMissingRequiredResponseMetadata(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"took":        `{"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"timed out":   `{"took":1,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"shards":      `{"took":1,"timed_out":false,"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"shard total": `{"took":1,"timed_out":false,"_shards":{"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"successful":  `{"took":1,"timed_out":false,"_shards":{"total":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"skipped":     `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"failed":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"failed":      `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0},"hits":{"total":{"value":0,"relation":"eq"},"hits":[]}}`,
		"total value": `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"relation":"eq"},"hits":[]}}`,
		"hit list":    `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":0,"relation":"eq"}}}`,
	}
	request := search.Request{Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}}
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	for field, response := range responses {
		field, response := field, response
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			client := internalClient(t, routeBody(response, http.StatusOK), resolver, nil)

			_, err := client.Search(t.Context(), request)
			failure := new(Failure)
			if !errors.As(err, &failure) || failure.Category != FailureMalformed {
				t.Fatalf("Search() error = %#v / %v, want malformed response for missing %s", failure, err, field)
			}
		})
	}
}

func TestSearchRejectsResponseOutsideRequestedShapeAndLimits(t *testing.T) {
	t.Parallel()

	longID := strings.Repeat("i", search.DefaultLimits().MaxIDBytes+1)
	longSort := strings.Repeat("s", search.DefaultLimits().MaxQueryBytes+1)
	tests := map[string]struct {
		response       string
		prepareClient  func(*Client)
		prepareRequest func(*search.Request)
	}{
		"invalid UTF-8 response": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"` + string([]byte{0xff}) + `","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		"oversized hit id": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"` + longID + `","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		"unsafe hit index": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"other/tenant","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		"oversized source": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{"value":"too large"},"sort":["id"]}]}}`,
			prepareClient: func(client *Client) {
				client.search.Limits.MaxSourceBytes = 8
			},
		},
		"non-object source": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":[],"sort":["id"]}]}}`,
		},
		"missing requested sort value": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{}}]}}`,
		},
		"oversized sort value": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["` + longSort + `"]}]}}`,
		},
		"unrequested highlight": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"],"highlight":{"secret":["value"]}}]}}`,
		},
		"unrequested highlight with equal field count": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"],"highlight":{"secret":[]}}]}}`,
			prepareRequest: func(request *search.Request) {
				request.Highlights = map[string]search.Highlight{"allowed": {FragmentSize: 8, MaxFragments: 1}}
			},
		},
		"oversized highlight fragment": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"],"highlight":{"allowed":["too-large"]}}]}}`,
			prepareClient: func(client *Client) {
				client.search.Limits.MaxSourceBytes = 8
			},
			prepareRequest: func(request *search.Request) {
				request.Highlights = map[string]search.Highlight{"allowed": {FragmentSize: 8, MaxFragments: 1}}
			},
		},
		"unrequested aggregation": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]},"aggregations":{"secret":{"value":1}}}`,
		},
		"unrequested aggregation with equal key count": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]},"aggregations":{"secret":{"value":1}}}`,
			prepareRequest: func(request *search.Request) {
				request.Aggregations = map[string]search.Aggregation{"allowed": search.TermsAggregation{Field: "kind", Size: 1}}
			},
		},
		"unrequested suggestion": {
			response: `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]},"suggest":{"secret":[]}}`,
		},
		"unexpected point in time on offset page": {
			response: `{"took":1,"timed_out":false,"pit_id":"other-tenant-pit","_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
	}
	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 1},
	}
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := internalClient(t, routeBody(test.response, http.StatusOK), resolver, nil)
			client.maximumResponseBytes = max(client.maximumResponseBytes, int64(len(test.response))+1)
			if test.prepareClient != nil {
				test.prepareClient(client)
			}
			preparedRequest := request
			if test.prepareRequest != nil {
				test.prepareRequest(&preparedRequest)
			}

			_, err := client.Search(t.Context(), preparedRequest)
			failure := new(Failure)
			if !errors.As(err, &failure) || failure.Category != FailureMalformed {
				t.Fatalf("Search() error = %#v / %v, want malformed response", failure, err)
			}
		})
	}
}

func TestCursorSearchRejectsUnsafeRotatedPointInTimeIDs(t *testing.T) {
	t.Parallel()

	const maximumPITBytes = 4096
	for _, test := range []struct {
		name, pointInTime string
		wantMalformed     bool
	}{
		{name: "exact byte limit", pointInTime: strings.Repeat("p", maximumPITBytes)},
		{name: "control characters", pointInTime: "other\ntenant", wantMalformed: true},
		{name: "oversized", pointInTime: strings.Repeat("p", maximumPITBytes+1), wantMalformed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			searchBody, err := json.Marshal(map[string]any{
				"took": 1, "timed_out": false, "pit_id": test.pointInTime,
				"_shards": map[string]any{"total": 1, "successful": 1, "skipped": 0, "failed": 0},
				"hits":    map[string]any{"total": map[string]any{"value": 0, "relation": "eq"}, "hits": []any{}},
			})
			if err != nil {
				t.Fatal(err)
			}
			deleted := ""
			resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
			client := internalClient(t, func(request *http.Request) (*http.Response, error) {
				switch {
				case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/events-v1/"):
					return internalResponse(http.StatusCreated, `{"pit_id":"initial"}`), nil
				case request.Method == http.MethodDelete:
					var deletion struct {
						PITID string `json:"pit_id"`
					}
					if decodeErr := json.NewDecoder(request.Body).Decode(&deletion); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					deleted = deletion.PITID
					body, marshalErr := json.Marshal(map[string]any{
						"pits": []any{map[string]any{"pit_id": deletion.PITID, "successful": true}},
					})
					if marshalErr != nil {
						t.Fatal(marshalErr)
					}
					return internalResponse(http.StatusOK, string(body)), nil
				default:
					return internalResponse(http.StatusOK, string(searchBody)), nil
				}
			}, resolver, nil)
			client.search.Limits.MaxQueryBytes = maximumPITBytes
			client.maximumResponseBytes = int64(len(searchBody) + 1)
			request := search.Request{
				Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
				Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
				Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
			}

			_, err = client.Search(t.Context(), request)
			failure := new(Failure)
			if test.wantMalformed && (!errors.As(err, &failure) || failure.Category != FailureMalformed || deleted != "initial") {
				t.Fatalf("Search() error/deleted PIT = %v/%q", err, deleted)
			}
			if !test.wantMalformed && (err != nil || deleted != test.pointInTime) {
				t.Fatalf("Search() error/deleted PIT = %v/%q", err, deleted)
			}
		})
	}
}

func TestSearchRejectsProjectionDisclosureAndPhysicalIndexMismatch(t *testing.T) {
	t.Parallel()

	resolver := internalResolver{target: IndexTarget{Name: "tenant-a-events-v2", PhysicalName: "tenant-a-events-v2", Fingerprint: "fingerprint"}}
	request := search.Request{
		Tenant: "tenant-a", Index: "events", Query: search.MatchAllQuery{},
		Sort:       []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page:       search.OffsetPage{Size: 1},
		Projection: search.Projection{Includes: []string{"public"}, Excludes: []string{"secret"}},
	}
	for name, response := range map[string]string{
		"excluded field":         `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"tenant-a-events-v2","_id":"id","_version":1,"_source":{"public":"ok","secret":"leak"},"sort":["id"]}]}}`,
		"foreign physical index": `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"tenant-b-events-v2","_id":"id","_version":1,"_source":{"public":"ok"},"sort":["id"]}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := internalClient(t, routeBody(response, http.StatusOK), resolver, nil)
			_, err := client.Search(t.Context(), request)
			failure := new(Failure)
			if !errors.As(err, &failure) || failure.Category != FailureMalformed {
				t.Fatalf("Search() error = %#v / %v, want malformed response", failure, err)
			}
		})
	}
}

func TestSearchReturnsLogicalIndexWithoutPhysicalTopology(t *testing.T) {
	t.Parallel()

	response := `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"tenant-a-events-v2","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`
	client := internalClient(t, routeBody(response, http.StatusOK), internalResolver{
		target: IndexTarget{Name: "tenant-a-events-v2", PhysicalName: "tenant-a-events-v2", Fingerprint: "fingerprint"},
	}, nil)
	result, err := client.Search(t.Context(), search.Request{
		Tenant: "tenant-a", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 1},
	})
	if err != nil || len(result.Hits()) != 1 || result.Hits()[0].Index != "events" {
		t.Fatalf("Search() = %#v/%v, want logical index", result.Hits(), err)
	}
}

func TestCursorSearchRejectsOversizedCreatedPointInTimeBeforeSearch(t *testing.T) {
	t.Parallel()

	requests := 0
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		requests++
		return internalResponse(http.StatusCreated, `{"pit_id":"`+strings.Repeat("p", search.DefaultLimits().MaxQueryBytes+1)+`"}`), nil
	}, internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}, nil)
	client.maximumResponseBytes = int64(search.DefaultLimits().MaxQueryBytes + 1024)
	_, err := client.Search(t.Context(), search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	})
	failure := new(Failure)
	if !errors.As(err, &failure) || failure.Operation != OperationCreatePIT || failure.Category != FailureMalformed || failure.OutcomeKnown || requests != 1 {
		t.Fatalf("Search() error/requests = %#v/%d, want bounded create-PIT failure", failure, requests)
	}
}

func TestSearchAcceptsExactResponseShapeLimits(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	id := strings.Repeat("i", limits.MaxIDBytes)
	source := json.RawMessage(`{"value":"` + strings.Repeat("s", limits.MaxSourceBytes-len(`{"value":""}`)) + `"}`)
	sortValue := strings.Repeat("o", limits.MaxQueryBytes-2)
	fragment := strings.Repeat("h", limits.MaxSourceBytes)
	body, err := json.Marshal(map[string]any{
		"took": 1, "timed_out": false,
		"_shards": map[string]any{"total": 1, "successful": 1, "skipped": 0, "failed": 0},
		"hits": map[string]any{
			"total": map[string]any{"value": 1, "relation": "eq"},
			"hits": []any{map[string]any{
				"_index": "events-v1", "_id": id, "_version": 1, "_source": source,
				"sort": []any{sortValue}, "highlight": map[string]any{"allowed": []any{fragment}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, routeBody(string(body), http.StatusOK), resolver, nil)
	client.maximumResponseBytes = int64(len(body) + 1)
	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: 1},
		Highlights: map[string]search.Highlight{
			"allowed": {FragmentSize: limits.MaxSourceBytes, MaxFragments: 1},
		},
	}

	result, err := client.Search(t.Context(), request)
	hits := result.Hits()
	if err != nil || len(hits) != 1 || hits[0].ID != id || len(hits[0].Source) != limits.MaxSourceBytes ||
		len(hits[0].SortValues) != 1 || len(hits[0].SortValues[0]) != limits.MaxQueryBytes ||
		len(hits[0].Highlights["allowed"]) != 1 || len(hits[0].Highlights["allowed"][0]) != limits.MaxSourceBytes {
		t.Fatalf("Search() exact-limit hit/error = %#v/%v", hits, err)
	}
}

func TestSearchAcceptsHitWithoutSource(t *testing.T) {
	t.Parallel()

	response := `{"took":1,"timed_out":false,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"sort":["id"]}]}}`
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, routeBody(response, http.StatusOK), resolver, nil)
	request := search.Request{
		Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 1},
	}

	result, err := client.Search(t.Context(), request)
	hits := result.Hits()
	if err != nil || len(hits) != 1 || len(hits[0].Source) != 0 {
		t.Fatalf("Search() source-less hit/error = %#v/%v", hits, err)
	}
}

func TestSearchBoundsNetworkReadByResultLimit(t *testing.T) {
	t.Parallel()

	body := &byteCountingReadCloser{Reader: strings.NewReader(strings.Repeat("x", 128))}
	resolver := internalResolver{target: IndexTarget{Name: "events-v1", PhysicalName: "events-v1", Fingerprint: "fingerprint"}}
	client := internalClient(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	}, resolver, nil)
	client.search.Limits.MaxResultBytes = 8
	request := search.Request{Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{}, Page: search.OffsetPage{Size: 1}}

	if _, err := client.Search(t.Context(), request); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Search() error = %v, want ErrResponseTooLarge", err)
	}
	if body.bytes > client.search.Limits.MaxResultBytes+1 {
		t.Fatalf("response bytes read = %d, want at most %d", body.bytes, client.search.Limits.MaxResultBytes+1)
	}
}

type byteCountingReadCloser struct {
	io.Reader
	bytes int64
}

func (reader *byteCountingReadCloser) Read(buffer []byte) (int, error) {
	count, err := reader.Reader.Read(buffer)
	reader.bytes += int64(count)
	return count, err
}

func (*byteCountingReadCloser) Close() error { return nil }
