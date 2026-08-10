package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
			resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
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
				Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}},
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
			response: `{"took":1,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		{
			name:     "empty hit index",
			response: `{"took":1,"_shards":{"total":1,"successful":1,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
		{
			name:     "invalid result diagnostics",
			response: `{"took":1,"_shards":{"total":1,"successful":0,"skipped":0,"failed":0},"hits":{"total":{"value":1,"relation":"eq"},"hits":[{"_index":"events-v1","_id":"id","_version":1,"_source":{},"sort":["id"]}]}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deleted := 0
			resolver := internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}
			client := internalClient(t, func(request *http.Request) (*http.Response, error) {
				switch request.Method {
				case http.MethodPost:
					if request.URL.Path == "/events-v1/_search/point_in_time" {
						return internalResponse(http.StatusCreated, `{"pit_id":"pit"}`), nil
					}
					return internalResponse(http.StatusOK, test.response), nil
				case http.MethodDelete:
					deleted++
					return internalResponse(http.StatusOK, `{"pits":[{"pit_id":"pit","successful":true}]}`), nil
				default:
					return internalResponse(http.StatusNotFound, `{}`), nil
				}
			}, resolver, nil)

			request := search.Request{
				Tenant: "tenant", Index: "events", Query: search.MatchAllQuery{},
				Sort: []search.Sort{{Field: "_id", Direction: search.Ascending}},
				Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
			}
			_, err := client.Search(t.Context(), request)
			failure := new(Failure)
			if !errors.As(err, &failure) || failure.Category != FailureMalformed {
				t.Fatalf("Search() error = %v, want malformed failure", err)
			}
			if deleted != 1 {
				t.Fatalf("PIT deletes = %d, want 1", deleted)
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

			client := internalClient(t, routeBody(test.body, http.StatusOK), internalResolver{target: IndexTarget{Name: "events-v1", Fingerprint: "fingerprint"}}, nil)
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
