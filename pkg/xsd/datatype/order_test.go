package datatype_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestOrderedValuePublicBoundaries(t *testing.T) {
	t.Parallel()

	if _, comparable := datatype.CompareOrdered("string", "a", "b"); comparable {
		t.Fatal("CompareOrdered(string) was comparable")
	}
	for _, test := range []struct {
		kind, lexical string
	}{
		{kind: "duration", lexical: "invalid"},
		{kind: "date", lexical: "invalid"},
	} {
		if value, ok := datatype.CanonicalOrderedValue(test.kind, test.lexical); ok {
			t.Fatalf("CanonicalOrderedValue(%s, %q) = %q, true", test.kind, test.lexical, value)
		}
	}
	if value, ok := datatype.CanonicalOrderedValue("duration", "-P1Y2M3DT4H5M6.5S"); !ok || value != "-14:-547813/2" {
		t.Fatalf("CanonicalOrderedValue(negative duration) = %q, %t", value, ok)
	}
}
