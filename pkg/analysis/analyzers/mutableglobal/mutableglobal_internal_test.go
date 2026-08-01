package mutableglobal

import (
	"go/types"
	"testing"
)

func TestHoldsMutableStateRejectsUnsupportedType(t *testing.T) {
	t.Parallel()

	if holdsMutableState(types.NewTuple(), types.Universe.Lookup("error").Type()) {
		t.Fatal("holdsMutableState() accepted a tuple")
	}
}

func TestPackagePatternDistinguishesExactAndTreeMatches(t *testing.T) {
	t.Parallel()

	exact := packagePattern{prefix: "example.com/service"}
	if !exact.matches("example.com/service") {
		t.Fatal("exact pattern rejected its package")
	}
	if exact.matches("example.com/service/child") {
		t.Fatal("exact pattern accepted a child package")
	}
	tree := packagePattern{prefix: "example.com/service", tree: true}
	if !tree.matches("example.com/service/child") {
		t.Fatal("tree pattern rejected a child package")
	}
}
