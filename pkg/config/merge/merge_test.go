package merge_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/config/merge"
)

func TestTrees(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		lower map[string]any
		upper map[string]any
		want  map[string]any
	}{
		"objects merge recursively": {
			lower: map[string]any{"server": map[string]any{"host": "localhost", "port": int64(80)}},
			upper: map[string]any{"server": map[string]any{"port": int64(443)}},
			want:  map[string]any{"server": map[string]any{"host": "localhost", "port": int64(443)}},
		},
		"slices replace": {
			lower: map[string]any{"hosts": []any{"one", "two"}},
			upper: map[string]any{"hosts": []any{"three"}},
			want:  map[string]any{"hosts": []any{"three"}},
		},
		"explicit null replaces": {
			lower: map[string]any{"token": "present"},
			upper: map[string]any{"token": nil},
			want:  map[string]any{"token": nil},
		},
		"deletion removes object key": {
			lower: map[string]any{"keep": true, "remove": "value"},
			upper: map[string]any{"remove": merge.Delete{}},
			want:  map[string]any{"keep": true},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := merge.Trees(test.lower, test.upper)
			if err != nil {
				t.Fatalf("Trees() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Trees() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestTreesRejectsTypeChanges(t *testing.T) {
	t.Parallel()

	_, err := merge.Trees(
		map[string]any{"server": map[string]any{"port": int64(80)}},
		map[string]any{"server": "localhost"},
	)
	if err == nil {
		t.Fatal("Trees() error = nil, want type conflict")
	}

	var conflict *merge.TypeConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Trees() error type = %T, want *merge.TypeConflictError", err)
	}
	if conflict.Path != "server" {
		t.Fatalf("TypeConflictError.Path = %q, want %q", conflict.Path, "server")
	}
	if got := err.Error(); !strings.Contains(got, `config merge at "server"`) ||
		!strings.Contains(got, "object with string") {
		t.Fatalf("TypeConflictError.Error() = %q", got)
	}
}

func TestTreesReportsTheLexicallyFirstConflict(t *testing.T) {
	t.Parallel()

	_, err := merge.Trees(
		map[string]any{"z": "lower", "a": "lower"},
		map[string]any{"z": []any{}, "a": []any{}},
	)

	var conflict *merge.TypeConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected TypeConflictError, got %T: %v", err, err)
	}
	if conflict.Path != "a" {
		t.Fatalf("conflict path = %q, want a", conflict.Path)
	}
}

func TestTreesDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	lower := map[string]any{"nested": map[string]any{"value": "lower"}}
	upper := map[string]any{"other": []any{"upper"}}

	got, err := merge.Trees(lower, upper)
	if err != nil {
		t.Fatalf("Trees() error = %v", err)
	}

	got["nested"].(map[string]any)["value"] = "changed"
	got["other"].([]any)[0] = "changed"

	if lower["nested"].(map[string]any)["value"] != "lower" {
		t.Fatal("Trees() mutated lower input")
	}
	if upper["other"].([]any)[0] != "upper" {
		t.Fatal("Trees() mutated upper input")
	}
}

func TestTreesReplacesInheritedNullWithTypedValue(t *testing.T) {
	t.Parallel()

	got, err := merge.Trees(
		map[string]any{"value": nil},
		map[string]any{"value": "present"},
	)
	if err != nil {
		t.Fatalf("Trees() error = %v", err)
	}
	if got["value"] != "present" {
		t.Fatalf("Trees() value = %#v", got["value"])
	}
}

func TestTreesReportsNestedObjectAndSliceKinds(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		lower string
		upper any
		want  string
	}{
		"object": {lower: "string", upper: map[string]any{}, want: "object"},
		"slice":  {lower: "string", upper: []string{}, want: "slice"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := merge.Trees(
				map[string]any{"nested": map[string]any{"value": test.lower}},
				map[string]any{"nested": map[string]any{"value": test.upper}},
			)
			var conflict *merge.TypeConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("Trees() error = %T %v", err, err)
			}
			if conflict.Path != "nested.value" || conflict.Lower != "string" || conflict.Upper != test.want {
				t.Fatalf("conflict = %#v", conflict)
			}
		})
	}
}

func TestTreesCoversEveryRepresentativeKindPair(t *testing.T) {
	t.Parallel()

	type representative struct {
		name  string
		value any
	}
	values := []representative{
		{name: "null", value: nil},
		{name: "bool", value: true},
		{name: "string", value: "value"},
		{name: "int64", value: int64(1)},
		{name: "uint64", value: uint64(1)},
		{name: "float64", value: float64(1)},
		{name: "object", value: map[string]any{"lower": true}},
		{name: "slice", value: []any{"lower"}},
	}
	upperValues := append(
		append([]representative(nil), values...),
		representative{name: "delete", value: merge.Delete{}},
	)

	for _, lower := range values {
		for _, upper := range upperValues {
			name := lower.name + "_then_" + upper.name
			lower := lower
			upper := upper
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := merge.Trees(
					map[string]any{"value": lower.value},
					map[string]any{"value": upper.value},
				)
				if upper.name == "delete" {
					if err != nil {
						t.Fatalf("Trees() error = %v", err)
					}
					if _, exists := got["value"]; exists {
						t.Fatalf("Trees() retained deleted value: %#v", got)
					}
					return
				}

				compatible := lower.name == "null" || upper.name == "null" || lower.name == upper.name
				if !compatible {
					var conflict *merge.TypeConflictError
					if !errors.As(err, &conflict) {
						t.Fatalf("Trees() error = %T %v, want TypeConflictError", err, err)
					}
					if conflict.Path != "value" || conflict.Lower != lower.name || conflict.Upper != upper.name {
						t.Fatalf("TypeConflictError = %#v", conflict)
					}
					return
				}
				if err != nil {
					t.Fatalf("Trees() error = %v", err)
				}

				want := upper.value
				if lower.name == "object" && upper.name == "object" {
					want = map[string]any{"lower": true}
				}
				if !reflect.DeepEqual(got["value"], want) {
					t.Fatalf("Trees() value = %#v, want %#v", got["value"], want)
				}
			})
		}
	}
}
