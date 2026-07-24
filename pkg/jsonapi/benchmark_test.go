package jsonapi

import (
	"fmt"
	"net/url"
	"testing"
)

func BenchmarkMarshalSingleResource(b *testing.B) {
	collection := benchmarkDocument(1, false)
	document := Document{Data: ResourceData(collection.Data.many[0])}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(document); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalResourceCollection(b *testing.B) {
	document := benchmarkDocument(100, false)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(document); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalSingleResource(b *testing.B) {
	document := Document{Data: ResourceData(benchmarkDocument(1, false).Data.many[0])}
	payload, err := Marshal(document)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if _, err := Unmarshal(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalResourceCollection(b *testing.B) {
	payload, err := Marshal(benchmarkDocument(100, false))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if _, err := Unmarshal(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalCompoundDocument(b *testing.B) {
	document := benchmarkDocument(100, true)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(document); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalCompoundDocument(b *testing.B) {
	payload, err := Marshal(benchmarkDocument(100, true))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if _, err := Unmarshal(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalAdversarialCompoundDocument(b *testing.B) {
	payload, err := Marshal(benchmarkAdversarialCompoundDocument(1_000))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if _, err := Unmarshal(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalAtomicOperations(b *testing.B) {
	document := benchmarkAtomicDocument(100)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := MarshalAtomic(document); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalAtomicOperations(b *testing.B) {
	payload, err := MarshalAtomic(benchmarkAtomicDocument(100))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		if _, err := UnmarshalAtomic(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseQuery(b *testing.B) {
	values := url.Values{
		"include":          {"author.comments,tags"},
		"fields[articles]": {"title,body,createdAt"},
		"filter[status]":   {"published"},
		"page[after]":      {"opaque-cursor"},
		"page[size]":       {"50"},
		"sort":             {"-createdAt,title"},
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseQuery(values); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNegotiateAccept(b *testing.B) {
	const extension = "https://example.com/extensions/version"
	const profile = "https://example.com/profiles/timestamps"
	negotiator, err := NewNegotiator([]string{extension}, []string{profile})
	if err != nil {
		b.Fatal(err)
	}
	header := "application/json;q=0.2, " + MediaTypeJSONAPI +
		`;ext="` + extension + `";profile="` + profile + `";q=0.9`
	b.ReportAllocs()
	for b.Loop() {
		if _, err := negotiator.NegotiateAccept(header); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCursorPaginationMetadata(b *testing.B) {
	total := int64(100_000)
	estimate := int64(100_500)
	truncated := true
	metadata := CursorPageMeta{
		RangeTruncated: &truncated,
		Total:          &total,
		EstimatedTotal: &CursorEstimatedTotal{BestGuess: &estimate},
	}
	b.ReportAllocs()
	for b.Loop() {
		meta, err := metadata.Meta()
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := ParseCursorPageMeta(meta); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkAtomicDocument(size int) AtomicDocument {
	operations := make([]AtomicOperation, size)
	for index := range operations {
		operations[index] = AtomicOperation{
			Op:   AtomicRemove,
			Href: fmt.Sprintf("/articles/%d", index),
		}
	}
	return AtomicDocument{Operations: operations}
}

func benchmarkDocument(size int, compound bool) Document {
	resources := make([]ResourceObject, size)
	included := make([]ResourceObject, 0, size)
	for index := range resources {
		id := fmt.Sprintf("%d", index)
		resource := ResourceObject{
			Type: "articles",
			ID:   id,
			Attributes: Attributes{
				"title": fmt.Sprintf("Article %d", index),
				"body":  "Representative benchmark content",
			},
		}
		if compound {
			authorID := "author-" + id
			resource.Relationships = Relationships{
				"author": {Data: ToOne(Identifier{Type: "people", ID: authorID})},
			}
			included = append(included, ResourceObject{
				Type:       "people",
				ID:         authorID,
				Attributes: Attributes{"name": "Benchmark Author"},
			})
		}
		resources[index] = resource
	}
	document := Document{Data: ResourceCollection(resources...)}
	if compound {
		document.Included = included
	}
	return document
}

func benchmarkAdversarialCompoundDocument(size int) Document {
	document := benchmarkDocument(size, true)
	for index := range document.Included {
		id := document.Included[index].ID
		next := document.Included[(index+1)%len(document.Included)].ID
		document.Included[index].LID = "local-" + id
		document.Included[index].Relationships = Relationships{
			"next": {Data: ToOne(Identifier{Type: "people", ID: next})},
		}
	}
	return document
}
