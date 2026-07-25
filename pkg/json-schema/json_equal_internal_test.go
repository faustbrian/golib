package jsonschema

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
)

func TestExactNumberComparisonSignEdges(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		left  string
		right string
		want  int
	}{
		{left: "-1", right: "1", want: -1},
		{left: "1", right: "-1", want: 1},
	} {
		if actual := compareNumber(test.left, test.right); actual != test.want {
			t.Errorf("compare %s and %s: got %d, want %d", test.left, test.right, actual, test.want)
		}
	}
	if equalJSON(&jsonValue{kind: 255}, &jsonValue{kind: 255}) {
		t.Fatal("unknown JSON value kinds must not compare equal")
	}
}

func TestZeroDivisorIsNotAMultiple(t *testing.T) {
	t.Parallel()

	if numberIsMultiple("1", "0") {
		t.Fatal("zero must not be accepted as a divisor")
	}
}

func TestCanonicalJSONHashAndCollisionFallback(t *testing.T) {
	t.Parallel()

	negative := &jsonValue{kind: kindNumber, number: "-1"}
	positive := &jsonValue{kind: kindNumber, number: "1"}
	if canonicalJSONHash(negative) == canonicalJSONHash(positive) {
		t.Fatal("number sign was omitted from the canonical hash")
	}
	for _, value := range []*jsonValue{
		{kind: kindNull},
		{kind: kindBoolean, boolean: false},
		{kind: kindBoolean, boolean: true},
		{kind: kindString, text: "value"},
		{kind: kindArray, array: []*jsonValue{{kind: kindNull}}},
		{
			kind: kindObject,
			object: map[string]*jsonValue{
				"value": {kind: kindBoolean, boolean: true},
			},
		},
	} {
		_ = canonicalJSONHash(value)
	}

	values := []*jsonValue{
		{kind: kindString, text: "a"},
		{kind: kindString, text: "b"},
		{kind: kindString, text: "c"},
	}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	state.limits.MaxUniqueComparisons = 2
	_, err := uniqueJSONWithHash(
		values,
		&state,
		func(*jsonValue) [sha256.Size]byte { return [sha256.Size]byte{} },
	)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want collision work limit", err)
	}

	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	unique, err := uniqueJSONWithHash(
		[]*jsonValue{
			{kind: kindString, text: "same"},
			{kind: kindString, text: "same"},
		},
		&state,
		func(*jsonValue) [sha256.Size]byte { return [sha256.Size]byte{} },
	)
	if err != nil || unique {
		t.Fatalf("got unique=%t err=%v, want duplicate", unique, err)
	}

	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	state.limits.MaxUniqueComparisons = 0
	_, err = uniqueJSONWithHash(values[:2], &state, canonicalJSONHash)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want distinct-item work limit", err)
	}
}

func TestUniqueJSONSelectsAlgorithmAtExactThreshold(t *testing.T) {
	t.Parallel()

	values := make([]*jsonValue, 17)
	for index := range values {
		values[index] = &jsonValue{kind: kindNumber, number: fmt.Sprint(index)}
	}

	limits := DefaultLimits()
	limits.MaxUniqueComparisons = 16
	state := evaluationState{ctx: context.Background(), limits: limits}
	if _, err := uniqueJSON(values[:16], &state); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("16 values used unexpected algorithm: %v", err)
	}
	state = evaluationState{ctx: context.Background(), limits: limits}
	if unique, err := uniqueJSON(values, &state); err != nil || !unique {
		t.Fatalf("17 values: unique=%t err=%v", unique, err)
	}
}

func TestHashedUniqueComparisonAccounting(t *testing.T) {
	t.Parallel()

	values := []*jsonValue{
		{kind: kindString, text: "a"},
		{kind: kindString, text: "b"},
		{kind: kindString, text: "c"},
	}
	state := evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	unique, err := uniqueJSONWithHash(values, &state, canonicalJSONHash)
	if err != nil || !unique || state.uniqueComparisons != 2 {
		t.Fatalf("distinct: unique=%t comparisons=%d err=%v",
			unique, state.uniqueComparisons, err)
	}

	state = evaluationState{ctx: context.Background(), limits: DefaultLimits()}
	unique, err = uniqueJSONWithHash(
		values,
		&state,
		func(*jsonValue) [sha256.Size]byte { return [sha256.Size]byte{} },
	)
	if err != nil || !unique || state.uniqueComparisons != 3 {
		t.Fatalf("collisions: unique=%t comparisons=%d err=%v",
			unique, state.uniqueComparisons, err)
	}
}
