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
	const fingerprintV1 = "e3011f8dec45f845ddbf8dca2a0d6c63650cacff9d40e17831a528a765f19594"
	if first.Fingerprint() != fingerprintV1 {
		t.Fatalf("fingerprint v1 = %s, want stable known-answer %s", first.Fingerprint(), fingerprintV1)
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
	for _, name := range []string{"", "UPPERCASE", "_hidden", "has space", "wild*card", "../escape", "index\x00name", "index\nname", "index\x7fname"} {
		if _, err := search.NewIndexDefinition(name, json.RawMessage(`{}`), json.RawMessage(`{}`), limits); !errors.Is(err, search.ErrInvalidIndexDefinition) {
			t.Fatalf("NewIndexDefinition(%q) error = %v", name, err)
		}
	}
	limits.MaxSourceBytes = 2
	if _, err := search.NewIndexDefinition("valid-index", json.RawMessage(`{"x":1}`), json.RawMessage(`{}`), limits); !errors.Is(err, search.ErrSchemaLimit) {
		t.Fatalf("NewIndexDefinition() error = %v", err)
	}
}

func TestIndexDefinitionRejectsHostileJSONStructureAcrossSettingsAndMappings(t *testing.T) {
	t.Parallel()

	defaultLimits := search.DefaultLimits()
	mappingExplosion := jsonObjectWithFields(defaultLimits.MaxJSONNodes + 1)
	tests := []struct {
		name      string
		configure func(*search.Limits)
		settings  json.RawMessage
		mappings  json.RawMessage
		want      error
	}{
		{"settings depth", func(limits *search.Limits) { limits.MaxJSONDepth = 2 }, json.RawMessage(`{"outer":{"too":{}}}`), json.RawMessage(`{}`), search.ErrJSONDepthLimit},
		{"mappings depth", func(limits *search.Limits) { limits.MaxJSONDepth = 2 }, json.RawMessage(`{}`), json.RawMessage(`{"outer":{"too":{}}}`), search.ErrJSONDepthLimit},
		{"mapping explosion within byte limit", func(*search.Limits) {}, json.RawMessage(`{}`), mappingExplosion, search.ErrJSONNodeLimit},
		{"combined node count", func(limits *search.Limits) { limits.MaxJSONNodes = 3 }, json.RawMessage(`{"first":1,"second":2}`), json.RawMessage(`{"third":3,"fourth":4}`), search.ErrJSONNodeLimit},
		{"duplicate settings key", func(*search.Limits) {}, json.RawMessage(`{"same":1,"same":2}`), json.RawMessage(`{}`), search.ErrDuplicateJSONKey},
		{"duplicate mappings key", func(*search.Limits) {}, json.RawMessage(`{}`), json.RawMessage(`{"same":1,"s\u0061me":2}`), search.ErrDuplicateJSONKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := search.DefaultLimits()
			test.configure(&limits)
			if len(test.settings)+len(test.mappings) > limits.MaxSourceBytes {
				t.Fatalf("test definition length = %d, exceeds byte limit %d", len(test.settings)+len(test.mappings), limits.MaxSourceBytes)
			}
			if _, err := search.NewIndexDefinition("index-v1", test.settings, test.mappings, limits); !errors.Is(err, search.ErrInvalidIndexDefinition) || !errors.Is(err, test.want) {
				t.Fatalf("NewIndexDefinition() error = %v, want ErrInvalidIndexDefinition and %v", err, test.want)
			}
		})
	}
}

func TestIndexDefinitionAcceptsExactJSONLimitsAndUnicode(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxJSONDepth = 2
	limits.MaxJSONNodes = 4
	definition, err := search.NewIndexDefinition("haku-ä", json.RawMessage(`{"label":"Helsinki 🧭"}`), json.RawMessage(`{"fields":["nimi","sijainti"]}`), limits)
	if err != nil {
		t.Fatalf("NewIndexDefinition() error = %v", err)
	}
	if definition.Name() != "haku-ä" {
		t.Fatalf("Name() = %q", definition.Name())
	}
}

func TestIndexDefinitionRejectsMissingJSONResourceLimits(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxJSONDepth = 0
	if _, err := search.NewIndexDefinition("index-v1", json.RawMessage(`{}`), json.RawMessage(`{}`), limits); !errors.Is(err, search.ErrInvalidLimits) {
		t.Fatalf("NewIndexDefinition() error = %v, want ErrInvalidLimits", err)
	}
}

func TestIndexDefinitionRejectsInvalidUTF8WithoutCanonicalReplacement(t *testing.T) {
	t.Parallel()

	invalid := string([]byte{0xff})
	invalidJSON := json.RawMessage([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})
	for _, test := range []struct {
		name               string
		settings, mappings json.RawMessage
	}{
		{invalid, json.RawMessage(`{}`), json.RawMessage(`{}`)},
		{"events-v1", invalidJSON, json.RawMessage(`{}`)},
		{"events-v1", json.RawMessage(`{}`), invalidJSON},
	} {
		if _, err := search.NewIndexDefinition(test.name, test.settings, test.mappings, search.DefaultLimits()); err == nil {
			t.Fatalf("NewIndexDefinition(%q) accepted invalid UTF-8", test.name)
		}
	}
}
