package search_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestResultOwnsHitsHighlightsAggregationsAndDiagnostics(t *testing.T) {
	t.Parallel()

	score := 4.25
	hits := []search.Hit{{
		Index: "locations-v3", ID: "location-1", Version: 7, Score: &score,
		Source:     json.RawMessage(`{"name":"Helsinki"}`),
		SortValues: []json.RawMessage{json.RawMessage(`1200000`), json.RawMessage(`"location-1"`)},
		Highlights: map[string][]string{"name": {"<em>Hel</em>sinki"}},
	}}
	aggregations := map[string]json.RawMessage{"countries": json.RawMessage(`{"buckets":[{"key":"FI","doc_count":1}]}`)}
	result, err := search.NewResult(hits, search.Total{Value: 1, Relation: search.TotalExact}, aggregations, nil, search.Diagnostics{Backend: "opensearch", Took: time.Millisecond}, "next")
	if err != nil {
		t.Fatalf("NewResult() error = %v", err)
	}

	hits[0].Source[2] = 'X'
	hits[0].Highlights["name"][0] = "changed"
	aggregations["countries"][0] = '['
	got := result.Hits()
	got[0].Source[2] = 'Y'
	got[0].SortValues[0][0] = '9'
	got[0].Highlights["name"][0] = "also changed"

	got = result.Hits()
	if string(got[0].Source) != `{"name":"Helsinki"}` || got[0].Highlights["name"][0] != "<em>Hel</em>sinki" || string(result.Aggregations()["countries"]) != `{"buckets":[{"key":"FI","doc_count":1}]}` {
		t.Fatalf("result ownership was not preserved: %#v", result)
	}
	if result.NextCursor() != "next" || result.Total().Value != 1 {
		t.Fatalf("result metadata = %#v", result)
	}
}

func TestResultPreservesPartialFailuresAndRejectsInvalidBackendValues(t *testing.T) {
	t.Parallel()

	diagnostics := search.Diagnostics{
		Backend: "opensearch", Partial: true, TimedOut: true,
		Shards:   search.ShardDiagnostics{Total: 3, Successful: 2, Failed: 1},
		Failures: []search.Failure{{Scope: "shard-2", Code: "timeout", Message: "bounded timeout", Retryable: true}},
	}
	result, err := search.NewResult(nil, search.Total{Value: 10, Relation: search.TotalLowerBound}, nil, nil, diagnostics, "")
	if err != nil {
		t.Fatalf("NewResult() error = %v", err)
	}
	if !result.Diagnostics().Partial || len(result.Diagnostics().Failures) != 1 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics())
	}

	nan := 0.0
	nan /= nan
	_, err = search.NewResult([]search.Hit{{Index: "idx", ID: "id", Version: 1, Score: &nan, Source: json.RawMessage(`{}`)}}, search.Total{Value: 1, Relation: search.TotalExact}, nil, nil, search.Diagnostics{Backend: "test"}, "")
	if !errors.Is(err, search.ErrInvalidResult) {
		t.Fatalf("NewResult() error = %v, want ErrInvalidResult", err)
	}
}

func TestResultRejectsAmbiguousOrMalformedBackendValues(t *testing.T) {
	t.Parallel()

	validHit := search.Hit{Index: "idx", ID: "id", Version: 1, Source: json.RawMessage(`{}`)}
	validTotal := search.Total{Value: 1, Relation: search.TotalExact}
	validDiagnostics := search.Diagnostics{Backend: "test"}
	tests := []struct {
		name         string
		hits         []search.Hit
		total        search.Total
		aggregations map[string]json.RawMessage
		suggestions  map[string]json.RawMessage
		diagnostics  search.Diagnostics
	}{
		{name: "missing total relation", hits: []search.Hit{validHit}, total: search.Total{Value: 1}, diagnostics: validDiagnostics},
		{name: "missing hit version", hits: []search.Hit{{Index: "idx", ID: "id", Source: json.RawMessage(`{}`)}}, total: validTotal, diagnostics: validDiagnostics},
		{name: "total below returned hits", hits: []search.Hit{validHit}, total: search.Total{Relation: search.TotalExact}, diagnostics: validDiagnostics},
		{name: "malformed aggregation", hits: []search.Hit{validHit}, total: validTotal, aggregations: map[string]json.RawMessage{"counts": json.RawMessage(`{`)}, diagnostics: validDiagnostics},
		{name: "malformed suggestion", hits: []search.Hit{validHit}, total: validTotal, suggestions: map[string]json.RawMessage{"names": json.RawMessage(`{`)}, diagnostics: validDiagnostics},
		{name: "empty aggregation name", hits: []search.Hit{validHit}, total: validTotal, aggregations: map[string]json.RawMessage{"": json.RawMessage(`{}`)}, diagnostics: validDiagnostics},
		{name: "unsafe suggestion name", hits: []search.Hit{validHit}, total: validTotal, suggestions: map[string]json.RawMessage{"names\nraw": json.RawMessage(`{}`)}, diagnostics: validDiagnostics},
		{name: "overlong aggregation name", hits: []search.Hit{validHit}, total: validTotal, aggregations: map[string]json.RawMessage{strings.Repeat("a", search.MaxFieldNameBytes+1): json.RawMessage(`{}`)}, diagnostics: validDiagnostics},
		{name: "invalid UTF-8 hit index", hits: []search.Hit{{Index: string([]byte{0xff}), ID: "id", Version: 1}}, total: validTotal, diagnostics: validDiagnostics},
		{name: "invalid UTF-8 hit ID", hits: []search.Hit{{Index: "idx", ID: string([]byte{0xff}), Version: 1}}, total: validTotal, diagnostics: validDiagnostics},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := search.NewResult(test.hits, test.total, test.aggregations, test.suggestions, test.diagnostics, ""); !errors.Is(err, search.ErrInvalidResult) {
				t.Fatalf("NewResult() error = %v, want ErrInvalidResult", err)
			}
		})
	}

	diagnostics := search.Diagnostics{
		Backend:  "test",
		Partial:  true,
		Failures: []search.Failure{{Scope: "backend", Code: "timeout", Retryable: true}},
	}
	if _, err := search.NewResult(nil, search.Total{Relation: search.TotalExact}, nil, nil, diagnostics, ""); err != nil {
		t.Fatalf("NewResult() rejected non-shard partial diagnostics: %v", err)
	}
}

func TestResultRejectsStructurallyInvalidDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		diagnostics search.Diagnostics
	}{
		{name: "missing backend"},
		{name: "unsafe backend", diagnostics: search.Diagnostics{Backend: "backend\nraw"}},
		{name: "unsafe request ID", diagnostics: search.Diagnostics{Backend: "test", RequestID: "request\x00raw"}},
		{name: "unattributed shard count", diagnostics: search.Diagnostics{Backend: "test", Shards: search.ShardDiagnostics{Successful: 1}}},
		{name: "timeout not partial", diagnostics: search.Diagnostics{Backend: "test", TimedOut: true}},
		{name: "failed shards not partial", diagnostics: search.Diagnostics{Backend: "test", Shards: search.ShardDiagnostics{Total: 1, Failed: 1}}},
		{name: "failure details not partial", diagnostics: search.Diagnostics{Backend: "test", Failures: []search.Failure{{Scope: "backend", Code: "failed"}}}},
		{name: "missing failure scope", diagnostics: search.Diagnostics{Backend: "test", Partial: true, Shards: search.ShardDiagnostics{Total: 1, Failed: 1}, Failures: []search.Failure{{Code: "failed"}}}},
		{name: "missing failure code", diagnostics: search.Diagnostics{Backend: "test", Partial: true, Shards: search.ShardDiagnostics{Total: 1, Failed: 1}, Failures: []search.Failure{{Scope: "shard"}}}},
		{name: "unsafe warning", diagnostics: search.Diagnostics{Backend: "test", Warnings: []string{"warning\x00raw"}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := search.NewResult(nil, search.Total{Relation: search.TotalExact}, nil, nil, test.diagnostics, ""); !errors.Is(err, search.ErrInvalidResult) {
				t.Fatalf("NewResult() error = %v, want ErrInvalidResult", err)
			}
		})
	}
}
