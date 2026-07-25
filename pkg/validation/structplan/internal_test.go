package structplan

import (
	"reflect"
	"testing"
)

func TestFieldByIndexRejectsMalformedCompiledIndexes(t *testing.T) {
	if got := fieldByIndex(reflect.Value{}, []int{0}); got.IsValid() {
		t.Fatal("invalid root produced a field")
	}
	if got := fieldByIndex(reflect.ValueOf(3), []int{0}); got.IsValid() {
		t.Fatal("non-struct root produced a field")
	}
	value := struct{ Pointer *struct{ Name string } }{}
	if got := fieldByIndex(reflect.ValueOf(value), []int{0, 0}); got.IsValid() {
		t.Fatal("nil pointer produced a field")
	}
	name := struct{ Name string }{Name: "ok"}
	withPointer := struct{ Pointer *struct{ Name string } }{Pointer: &name}
	if got := fieldByIndex(reflect.ValueOf(withPointer), []int{0, 0}); got.String() != "ok" {
		t.Fatalf("pointer field = %v", got)
	}
}
