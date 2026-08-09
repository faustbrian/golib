package search_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestRequestFingerprintIsDeterministicAndBindsBehavior(t *testing.T) {
	t.Parallel()

	request := search.Request{
		Tenant: "tenant-a", Index: "events",
		Query:        search.BoolQuery{Must: []search.Query{search.TermQuery{Field: "status", Value: search.StringValue("delivered")}}},
		Sort:         []search.Sort{{Field: "created_at", Direction: search.Descending}, {Field: "_id", Direction: search.Ascending}},
		Page:         search.CursorPage{Size: 25, KeepAlive: time.Minute},
		Aggregations: map[string]search.Aggregation{"carriers": search.TermsAggregation{Field: "carrier", Size: 10}},
	}
	first, err := search.RequestFingerprint(request, search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	second, err := search.RequestFingerprint(request, search.DefaultLimits())
	if err != nil || first != second {
		t.Fatalf("fingerprints = %q/%q, %v", first, second, err)
	}

	changed := request
	changed.Query = search.TermQuery{Field: "status", Value: search.StringValue("pending")}
	other, err := search.RequestFingerprint(changed, search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("behaviorally different requests shared a fingerprint")
	}

	continued := request
	continued.Page = search.CursorPage{Size: 25, KeepAlive: time.Minute, Cursor: "opaque-continuation"}
	continuation, err := search.RequestFingerprint(continued, search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if continuation != first {
		t.Fatal("cursor token changed the bound query fingerprint")
	}
}

func TestRequestFingerprintRejectsUnboundedUnvalidatedInput(t *testing.T) {
	t.Parallel()
	request := search.Request{Tenant: "tenant", Index: "events", Query: search.FullTextQuery{Fields: []string{"message"}, Text: strings.Repeat("x", search.DefaultLimits().MaxQueryBytes+1)}, Page: search.OffsetPage{Size: 1}}
	if _, err := search.RequestFingerprint(request, search.DefaultLimits()); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatalf("RequestFingerprint() error = %v, want ErrInvalidQuery", err)
	}
}
