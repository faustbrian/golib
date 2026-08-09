package search_test

import (
	"encoding/json"
	"errors"
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
	_, err = search.NewResult([]search.Hit{{Index: "idx", ID: "id", Score: &nan, Source: json.RawMessage(`{}`)}}, search.Total{}, nil, nil, search.Diagnostics{}, "")
	if !errors.Is(err, search.ErrInvalidResult) {
		t.Fatalf("NewResult() error = %v, want ErrInvalidResult", err)
	}
}
