package decode

import (
	"fmt"
	"reflect"
	"testing"
)

func TestFlattenErrorsPreservesWrappedStructuredFailures(t *testing.T) {
	t.Parallel()

	first := &FieldError{Path: "first"}
	second := &FieldError{Path: "second"}
	got := flattenErrors([]error{
		fmt.Errorf("wrapped field: %w", first),
		fmt.Errorf("wrapped aggregate: %w", &Errors{Fields: []*FieldError{second}}),
	})
	want := []*FieldError{first, second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flattenErrors() = %#v, want %#v", got, want)
	}
}
