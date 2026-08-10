package search_test

import (
	"encoding/json"
	"errors"
	"fmt"
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
	deep := strings.Repeat(`{"nested":`, 33) + `true` + strings.Repeat(`}`, 33)
	fields := make([]string, 20_000)
	for index := range fields {
		fields[index] = fmt.Sprintf(`"field%d":null`, index)
	}
	for _, test := range []struct {
		name   string
		source json.RawMessage
	}{
		{"excessive depth", json.RawMessage(deep)},
		{"high field count within byte limit", json.RawMessage(`{` + strings.Join(fields, ",") + `}`)},
		{"duplicate object keys", json.RawMessage(`{"same":1,"same":2}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if len(test.source) > limits.MaxSourceBytes {
				t.Fatalf("test source length = %d, exceeds byte limit %d", len(test.source), limits.MaxSourceBytes)
			}
			if _, err := search.NewDocument("tenant", "index", "id", 1, test.source, limits); !errors.Is(err, search.ErrInvalidSource) {
				t.Fatalf("NewDocument() error = %v, want ErrInvalidSource", err)
			}
		})
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
