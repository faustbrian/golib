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
