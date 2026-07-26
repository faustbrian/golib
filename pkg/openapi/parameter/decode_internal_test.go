package parameter

import (
	"errors"
	"reflect"
	"testing"
)

func TestSplitEncodedHandlesEmptyAndCaseFoldedDelimiters(t *testing.T) {
	t.Parallel()

	if got, err := splitEncoded("value", "", 1); err != nil || !reflect.DeepEqual(got, []string{"value"}) {
		t.Fatalf("empty delimiter split = %#v", got)
	}
	want := []string{"one", "two", "three"}
	if got, err := splitEncoded("one%2Ftwo%2fthree", "%2f", 3); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("case-folded split = %#v, want %#v", got, want)
	}
	if got, err := splitEncoded("%2fone%2f", "%2f", 3); err != nil || !reflect.DeepEqual(
		got, []string{"", "one", ""},
	) {
		t.Fatalf("endpoint split = %#v", got)
	}
}

func TestSplitEncodedEnforcesPartLimit(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"one%2Ftwo", "one%2Ftwo%2Fthree"} {
		if _, err := splitEncoded(raw, "%2f", 1); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("splitEncoded(%q) error = %v, want item limit", raw, err)
		}
	}
}

func TestMaximumPartsAccountsForObjectPairsWithoutOverflow(t *testing.T) {
	t.Parallel()

	decoder := valueDecoder{maxItems: 3}
	if got := decoder.maximumParts(Array, false); got != 3 {
		t.Fatalf("array maximum = %d, want 3", got)
	}
	if got := decoder.maximumParts(Object, true); got != 3 {
		t.Fatalf("exploded object maximum = %d, want 3", got)
	}
	if got := decoder.maximumParts(Object, false); got != 6 {
		t.Fatalf("object token maximum = %d, want 6", got)
	}

	maximum := int(^uint(0) >> 1)
	decoder.maxItems = maximum / 2
	if got := decoder.maximumParts(Object, false); got != maximum-1 {
		t.Fatalf("boundary object maximum = %d, want %d", got, maximum-1)
	}
	decoder.maxItems++
	if got := decoder.maximumParts(Object, false); got != decoder.maxItems {
		t.Fatalf("saturated object maximum = %d, want %d", got, decoder.maxItems)
	}
}

func TestShapeValuesRemainStable(t *testing.T) {
	t.Parallel()

	if Primitive != 1 || Array != 2 || Object != 3 {
		t.Fatalf("shape values = %d, %d, %d", Primitive, Array, Object)
	}
}

func TestIndexASCIIFoldIncludesTheFinalCandidate(t *testing.T) {
	t.Parallel()

	if got := indexASCIIFold("abC", "c", 2); got != 2 {
		t.Fatalf("final candidate index = %d", got)
	}
}
