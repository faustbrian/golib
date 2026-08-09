package jsonapi

import (
	"bytes"
	"errors"
	"testing"
)

func TestDecodersIgnoreNonCompliantMembers(t *testing.T) {
	t.Parallel()

	core := map[string]string{
		"top level":      `{"unknown":true,"data":null}`,
		"jsonapi object": `{"jsonapi":{"version":"1.1","unknown":true},"data":null}`,
		"resource":       `{"data":{"type":"articles","id":"1","unknown":true}}`,
		"relationship":   `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":null,"unknown":true}}}}`,
		"identifier":     `{"data":{"type":"articles","id":"1","relationships":{"author":{"data":{"type":"people","id":"9","unknown":true}}}}}`,
		"link object":    `{"data":null,"links":{"self":{"href":"/articles","unknown":true}}}`,
		"error":          `{"errors":[{"status":"400","unknown":true}]}`,
		"error source":   `{"errors":[{"source":{"pointer":"/data","unknown":true}}]}`,
	}
	for name, payload := range core {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			document, err := Unmarshal([]byte(payload))
			if err != nil {
				t.Fatalf("Unmarshal() rejected a non-compliant member: %v", err)
			}
			encoded, err := Marshal(document)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if bytes.Contains(encoded, []byte(`"unknown"`)) {
				t.Fatalf("non-compliant member was retained: %s", encoded)
			}
		})
	}

	atomic := map[string]string{
		"top level": `{"atomic:operations":[{"op":"remove","ref":{"type":"articles","id":"1"}}],"unknown":true}`,
		"operation": `{"atomic:operations":[{"op":"remove","ref":{"type":"articles","id":"1"},"unknown":true}]}`,
		"reference": `{"atomic:operations":[{"op":"remove","ref":{"type":"articles","id":"1","unknown":true}}]}`,
		"result":    `{"atomic:results":[{"meta":{},"unknown":true}]}`,
	}
	for name, payload := range atomic {
		t.Run("atomic "+name, func(t *testing.T) {
			t.Parallel()
			document, err := UnmarshalAtomic([]byte(payload))
			if err != nil {
				t.Fatalf("UnmarshalAtomic() rejected a non-compliant member: %v", err)
			}
			encoded, err := MarshalAtomic(document)
			if err != nil {
				t.Fatalf("MarshalAtomic() error = %v", err)
			}
			if bytes.Contains(encoded, []byte(`"unknown"`)) {
				t.Fatalf("non-compliant member was retained: %s", encoded)
			}
		})
	}
}

func TestIgnoredMembersDoNotQualifyOtherwiseInvalidObjects(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		payload string
		path    string
	}{
		"relationship": {
			payload: `{"data":{"type":"articles","id":"1","relationships":{"author":{"unknown":true}}}}`,
			path:    "/data/relationships/author",
		},
		"error": {
			payload: `{"errors":[{"unknown":true}]}`,
			path:    "/errors/0",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Unmarshal([]byte(test.payload))
			var validationError *ValidationError
			if !errors.As(err, &validationError) ||
				!hasViolation(validationError, test.path, "required") {
				t.Fatalf("ignored member qualified an invalid object: %T %#v", err, validationError)
			}
		})
	}
}
