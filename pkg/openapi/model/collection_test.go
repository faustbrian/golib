package model_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/model"
)

func TestListOwnsInputAndReturnedSlices(t *testing.T) {
	t.Parallel()

	input := []string{"one", "two"}
	list := model.NewList(input)
	input[0] = "changed"
	if list.Len() != 2 {
		t.Fatalf("Len() = %d", list.Len())
	}
	if value, ok := list.At(0); !ok || value != "one" {
		t.Fatalf("At(0) = %q, %t", value, ok)
	}
	for _, index := range []int{-1, 2} {
		if value, ok := list.At(index); ok || value != "" {
			t.Fatalf("At(%d) = %q, %t", index, value, ok)
		}
	}
	values := list.Values()
	values[0] = "changed"
	if value, _ := list.At(0); value != "one" {
		t.Fatal("Values exposed list storage")
	}
}

func TestMapOwnsEntriesAndPreservesInsertionOrder(t *testing.T) {
	t.Parallel()

	input := []model.Entry[int]{{Name: "second", Value: 2}, {Name: "first", Value: 1}}
	ordered, err := model.NewMap(input)
	if err != nil {
		t.Fatal(err)
	}
	input[0].Name = "changed"
	if ordered.Len() != 2 {
		t.Fatalf("Len() = %d", ordered.Len())
	}
	if names := ordered.Names(); names[0] != "second" || names[1] != "first" {
		t.Fatalf("Names() = %#v", names)
	}
	entries := ordered.Entries()
	entries[0].Name = "changed"
	if names := ordered.Names(); names[0] != "second" {
		t.Fatal("Entries exposed map storage")
	}
	if value, ok := ordered.Lookup("first"); !ok || value != 1 {
		t.Fatalf("Lookup(first) = %d, %t", value, ok)
	}
	if value, ok := ordered.Lookup("missing"); ok || value != 0 {
		t.Fatalf("Lookup(missing) = %d, %t", value, ok)
	}
}

func TestMapRejectsAmbiguousNames(t *testing.T) {
	t.Parallel()

	for _, entries := range [][]model.Entry[int]{
		{{Name: "duplicate"}, {Name: "duplicate"}},
		{{Name: string([]byte{0xff})}},
	} {
		if _, err := model.NewMap(entries); !errors.Is(err, model.ErrInvalidCollection) {
			t.Fatalf("NewMap() error = %v", err)
		}
	}
}
