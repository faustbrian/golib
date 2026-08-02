package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestRawCarrierReturnsOneExcessValueSentinel(t *testing.T) {
	t.Parallel()

	raw := make([]json.RawMessage, maxMetadataValues+2)
	for index := range raw {
		raw[index] = json.RawMessage("\"value\"")
	}
	values := (rawCarrier{"field": raw}).Values("field")
	if len(values) != maxMetadataValues+1 {
		t.Fatalf("raw carrier values = %d, want %d", len(values), maxMetadataValues+1)
	}
}
