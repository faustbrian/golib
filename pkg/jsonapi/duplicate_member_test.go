package jsonapi

import (
	"errors"
	"testing"
)

func TestUnmarshalRejectsDuplicateObjectMembers(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		payload string
		path    string
	}{
		"top level": {
			payload: `{"data":null,"data":null}`,
			path:    "/data",
		},
		"resource": {
			payload: `{"data":{"type":"articles","id":"1","id":"2"}}`,
			path:    "/data/id",
		},
		"nested attribute object": {
			payload: `{"data":{"type":"articles","id":"1","attributes":{"settings":{"mode":"a","mode":"b"}}}}`,
			path:    "/data/attributes/settings/mode",
		},
		"meta object": {
			payload: `{"meta":{"requestId":"a","requestId":"b"}}`,
			path:    "/meta/requestId",
		},
		"array item": {
			payload: `{"data":[{"type":"articles","id":"1"},{"type":"articles","id":"2","id":"3"}]}`,
			path:    "/data/1/id",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Unmarshal([]byte(test.payload))
			if err == nil {
				t.Fatal("expected duplicate member error")
			}
			var decodeError *DecodeError
			if !errors.As(err, &decodeError) {
				t.Fatalf("expected DecodeError, got %T: %v", err, err)
			}
			if decodeError.Path != test.path || decodeError.Code != "duplicate-member" {
				t.Fatalf(
					"unexpected error: got path %q code %q, want path %q code duplicate-member",
					decodeError.Path,
					decodeError.Code,
					test.path,
				)
			}
		})
	}
}
