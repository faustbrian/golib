package temporalwire_test

import (
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/temporalwire"
)

func FuzzDocumentJSON(f *testing.F) {
	f.Add([]byte(`{"version":"temporal/v1","kind":"instant-period","value":"[2026-01-01T00:00:00Z,2026-01-02T00:00:00Z)"}`))
	f.Add([]byte(`null`))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		document, err := temporalwire.Unmarshal(input, temporal.Limits{})
		if err != nil {
			return
		}
		encoded, err := temporalwire.Marshal(document, temporal.Limits{})
		if err != nil {
			t.Fatalf("Marshal(): %v", err)
		}
		if _, err := temporalwire.Unmarshal(encoded, temporal.Limits{}); err != nil {
			t.Fatalf("round-trip Unmarshal(): %v", err)
		}
	})
}

func FuzzCollectionDocumentJSON(f *testing.F) {
	f.Add([]byte(`{"version":"temporal/v1","kind":"instant-set","values":["[2026-01-01T00:00:00Z,2026-01-02T00:00:00Z)"]}`))
	f.Add([]byte(`{"version":"temporal/v1","kind":"daily-set","values":[]}`))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		document, err := temporalwire.UnmarshalCollection(input, temporal.Limits{})
		if err != nil {
			return
		}
		encoded, err := temporalwire.MarshalCollection(document, temporal.Limits{})
		if err != nil {
			t.Fatalf("MarshalCollection(): %v", err)
		}
		if _, err := temporalwire.UnmarshalCollection(encoded, temporal.Limits{}); err != nil {
			t.Fatalf("round-trip UnmarshalCollection(): %v", err)
		}
	})
}
