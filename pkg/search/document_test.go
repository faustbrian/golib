package search_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/search"
)

func TestDocumentValidationDefinesTheOwnedBoundedWriteContract(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	source := json.RawMessage(`{"name":"Helsinki"}`)
	document, err := search.NewDocument("tenant-a", "tracking-events", "event-1", 7, source, limits)
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	source[2] = 'X'
	if document.Tenant != "tenant-a" || document.Index != "tracking-events" || document.ID != "event-1" || document.Version != 7 || string(document.Source) != `{"name":"Helsinki"}` {
		t.Fatalf("NewDocument() = %#v", document)
	}
}

func TestDocumentRejectsHostileJSONStructure(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	tooManyFields := jsonObjectWithFields(limits.MaxJSONNodes + 1)
	for _, test := range []struct {
		name      string
		configure func(*search.Limits)
		source    json.RawMessage
		want      error
	}{
		{"excessive depth", func(limits *search.Limits) { limits.MaxJSONDepth = 2 }, json.RawMessage(`{"outer":{"too":{"deep":true}}}`), search.ErrJSONDepthLimit},
		{"high field count within byte limit", func(*search.Limits) {}, tooManyFields, search.ErrJSONNodeLimit},
		{"duplicate object keys", func(*search.Limits) {}, json.RawMessage(`{"same":1,"s\u0061me":2}`), search.ErrDuplicateJSONKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			configured := limits
			test.configure(&configured)
			if len(test.source) > configured.MaxSourceBytes {
				t.Fatalf("test source length = %d, exceeds byte limit %d", len(test.source), configured.MaxSourceBytes)
			}
			if _, err := search.NewDocument("tenant", "index", "id", 1, test.source, configured); !errors.Is(err, search.ErrInvalidSource) || !errors.Is(err, test.want) {
				t.Fatalf("NewDocument() error = %v, want ErrInvalidSource and %v", err, test.want)
			}
		})
	}
}

func jsonObjectWithFields(count int) json.RawMessage {
	var object strings.Builder
	object.Grow(count * 12)
	object.WriteByte('{')
	for index := range count {
		if index != 0 {
			object.WriteByte(',')
		}
		object.WriteByte('"')
		object.WriteString(strconv.Itoa(index))
		object.WriteString(`":null`)
	}
	object.WriteByte('}')
	return json.RawMessage(object.String())
}

func TestDocumentAcceptsExactJSONLimitsAndPreservesUnicode(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxJSONDepth = 2
	limits.MaxJSONNodes = 4
	source := json.RawMessage(`{"näme":"Helsinki 🧭","values":[1,2]}`)
	document, err := search.NewDocument("vuokralainen", "haku-ä", "tunnus", 1, source, limits)
	if err != nil {
		t.Fatalf("NewDocument() error = %v", err)
	}
	if string(document.Source) != string(source) {
		t.Fatalf("NewDocument() source = %s, want %s", document.Source, source)
	}
}

func TestDocumentRejectsMissingJSONResourceLimits(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	limits.MaxJSONNodes = 0
	if _, err := search.NewDocument("tenant", "index", "id", 1, json.RawMessage(`{}`), limits); !errors.Is(err, search.ErrInvalidLimits) {
		t.Fatalf("NewDocument() error = %v, want ErrInvalidLimits", err)
	}
}

func TestDocumentValidationRejectsEveryUnsafeBoundary(t *testing.T) {
	t.Parallel()

	limits := search.DefaultLimits()
	tests := []struct {
		name              string
		tenant, index, id string
		version           uint64
		source            json.RawMessage
		want              error
	}{
		{"tenant required", "", "idx", "id", 1, json.RawMessage(`{}`), search.ErrTenantRequired},
		{"index required", "t", "", "id", 1, json.RawMessage(`{}`), search.ErrIndexRequired},
		{"id required", "t", "idx", "", 1, json.RawMessage(`{}`), search.ErrIDRequired},
		{"version required", "t", "idx", "id", 0, json.RawMessage(`{}`), search.ErrVersionRequired},
		{"source required", "t", "idx", "id", 1, nil, search.ErrSourceRequired},
		{"source object", "t", "idx", "id", 1, json.RawMessage(`[]`), search.ErrInvalidSource},
		{"source valid JSON", "t", "idx", "id", 1, json.RawMessage(`{"x"`), search.ErrInvalidSource},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := search.NewDocument(test.tenant, test.index, test.id, test.version, test.source, limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewDocument() error = %v, want %v", err, test.want)
			}
		})
	}
}
