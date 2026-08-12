package valuecheck

import (
	"errors"
	"reflect"
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
	hiddenCycle := &value{}
	hiddenCycle.hidden = hiddenCycle
	shared := []int{1, 2}
	input := map[string]any{
		"nil":     nil,
		"pointer": (*int)(nil),
		"first":   shared,
		"second":  shared,
		"struct":  value{Visible: []int{3}, hidden: hiddenCycle},
		"array":   [1]int{4},
	}
	if err := Validate(input); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestWalkEnforcesDepthAtEveryReflectionEdge(t *testing.T) {
	t.Parallel()

	if err := walk(reflect.ValueOf(1), map[identity]struct{}{}, MaxDepth); err != nil {
		t.Fatalf("walk() at maximum depth error = %v", err)
	}
	if !errors.Is(walk(reflect.ValueOf(1), map[identity]struct{}{}, MaxDepth+1), ErrDepth) {
		t.Fatal("walk() beyond maximum depth was not rejected")
	}

	integer := 1
	type mapKey struct{ Child *int }
	for name, test := range map[string]struct {
		value reflect.Value
		depth int
	}{
		"pointer":   {value: reflect.ValueOf(&integer), depth: MaxDepth},
		"map key":   {value: reflect.ValueOf(map[mapKey]int{{Child: &integer}: 1}), depth: MaxDepth - 1},
		"map value": {value: reflect.ValueOf(map[int][]int{1: {1}}), depth: MaxDepth - 1},
		"struct":    {value: reflect.ValueOf(struct{ Value int }{Value: 1}), depth: MaxDepth},
	} {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(walk(test.value, map[identity]struct{}{}, test.depth), ErrDepth) {
				t.Fatal("walk() nested value beyond maximum depth was not rejected")
			}
		})
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

func TestValidateContinuesAfterUnexportedStructField(t *testing.T) {
	t.Parallel()

	type node struct {
		hidden int
		Next   *node
	}
	cyclic := &node{}
	cyclic.hidden = 1
	cyclic.Next = cyclic
	if !errors.Is(Validate(cyclic), ErrCycle) {
		t.Fatal("Validate() skipped exported field after an unexported field")
	}
}
