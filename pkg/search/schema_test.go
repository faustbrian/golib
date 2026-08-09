package search_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func TestIndexDefinitionFingerprintIsCanonicalAndCompatibilityIsExplicit(t *testing.T) {
	t.Parallel()

	first, err := search.NewIndexDefinition(
		"locations-v3",
		json.RawMessage(`{"index":{"number_of_shards":2,"refresh_interval":"1s"}}`),
		json.RawMessage(`{"properties":{"name":{"type":"text"},"country":{"type":"keyword"}}}`),
		search.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	equivalent, err := search.NewIndexDefinition(
		"locations-v3-copy",
		json.RawMessage("{\n  \"index\": {\"refresh_interval\": \"1s\", \"number_of_shards\": 2}\n}"),
		json.RawMessage(`{"properties":{"country":{"type":"keyword"},"name":{"type":"text"}}}`),
		search.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() != equivalent.Fingerprint() {
		t.Fatalf("equivalent fingerprints differ: %s != %s", first.Fingerprint(), equivalent.Fingerprint())
	}
	if compatibility := search.CompareDefinitions(first, equivalent); compatibility.Kind != search.Compatible {
		t.Fatalf("CompareDefinitions() = %#v", compatibility)
	}

	changed, err := search.NewIndexDefinition("locations-v4", first.Settings(), json.RawMessage(`{"properties":{"name":{"type":"keyword"}}}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if compatibility := search.CompareDefinitions(first, changed); compatibility.Kind != search.ReindexRequired || len(compatibility.Reasons) == 0 {
		t.Fatalf("CompareDefinitions() = %#v", compatibility)
	}
}

func TestIndexDefinitionRejectsUnsafeNamesAndUnboundedSchema(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	for _, name := range []string{"", "UPPERCASE", "_hidden", "has space", "wild*card", "../escape"} {
		if _, err := search.NewIndexDefinition(name, json.RawMessage(`{}`), json.RawMessage(`{}`), limits); !errors.Is(err, search.ErrInvalidIndexDefinition) {
			t.Fatalf("NewIndexDefinition(%q) error = %v", name, err)
		}
	}
	limits.MaxSourceBytes = 2
	if _, err := search.NewIndexDefinition("valid-index", json.RawMessage(`{"x":1}`), json.RawMessage(`{}`), limits); !errors.Is(err, search.ErrSchemaLimit) {
		t.Fatalf("NewIndexDefinition() error = %v", err)
	}
}
