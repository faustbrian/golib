package valuecheck

import (
	"errors"
	"testing"
)

func TestValidateRejectsCyclesAndDepth(t *testing.T) {
	t.Parallel()

	cyclicMap := map[string]any{}
	cyclicMap["self"] = cyclicMap
	if !errors.Is(Validate(cyclicMap), ErrCycle) {
		t.Fatal("Validate() map cycle was not rejected")
	}
	cyclicSlice := make([]any, 1)
	cyclicSlice[0] = cyclicSlice
	if !errors.Is(Validate(cyclicSlice), ErrCycle) {
		t.Fatal("Validate() slice cycle was not rejected")
	}
	type node struct{ Next *node }
	cyclicPointer := &node{}
	cyclicPointer.Next = cyclicPointer
	if !errors.Is(Validate(cyclicPointer), ErrCycle) {
		t.Fatal("Validate() pointer cycle was not rejected")
	}

	deep := any("leaf")
	for range MaxDepth + 1 {
		deep = []any{deep}
	}
	if !errors.Is(Validate(deep), ErrDepth) {
		t.Fatal("Validate() excessive depth was not rejected")
	}
}

func TestValidateAcceptsAcyclicValues(t *testing.T) {
	t.Parallel()
	if err := Validate(nil); err != nil {
		t.Fatalf("Validate(nil) error = %v", err)
	}

	type value struct {
		Visible []int
		hidden  *value
	}
	shared := []int{1, 2}
	input := map[string]any{
		"nil":     nil,
		"pointer": (*int)(nil),
		"first":   shared,
		"second":  shared,
		"struct":  value{Visible: []int{3}, hidden: &value{}},
		"array":   [1]int{4},
	}
	if err := Validate(input); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsCycleInMapKey(t *testing.T) {
	t.Parallel()

	type key struct{ Next *key }
	cyclic := &key{}
	cyclic.Next = cyclic
	if !errors.Is(Validate(map[*key]int{cyclic: 1}), ErrCycle) {
		t.Fatal("Validate() map-key cycle was not rejected")
	}
}
