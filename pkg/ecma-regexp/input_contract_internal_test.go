package ecmascript

import (
	"slices"
	"testing"
)

func TestInputViewPreservesExactLimitBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultMatchOptions().Limits
	limits.InputBytes = 2
	limits.InputRunes = 1
	view, err := makeInputView("é", limits)
	if err != nil {
		t.Fatalf("makeInputView() error = %v", err)
	}
	assertInputBoundaries(t, view, []Index{
		{Exact: true},
		{UTF16: 1, Rune: 1, Byte: 2, Exact: true},
	})

	utf16View, err := makeUTF16InputView(UTF16FromUnits([]uint16{'a'}), limits)
	if err != nil {
		t.Fatalf("makeUTF16InputView() error = %v", err)
	}
	assertInputBoundaries(t, utf16View, []Index{
		{Exact: true},
		{UTF16: 1, Rune: 1, Byte: 1, Exact: true},
	})
}

func TestInputViewMapsUTF8BoundaryWidths(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		input   string
		endByte int
	}{
		{name: "ASCII", input: "a", endByte: 1},
		{name: "multibyte", input: "é", endByte: 2},
		{name: "replacement character", input: "�", endByte: 3},
		{name: "invalid byte", input: string([]byte{0xFF}), endByte: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			view, err := makeInputView(test.input, DefaultMatchOptions().Limits)
			if err != nil {
				t.Fatalf("makeInputView() error = %v", err)
			}
			if got := view.boundaries[1]; got != (Index{UTF16: 1, Rune: 1, Byte: test.endByte, Exact: true}) {
				t.Fatalf("boundary = %+v", got)
			}
		})
	}
}

func TestUTF16InputViewMapsScalarAndUnpairedSurrogateBoundaries(t *testing.T) {
	t.Parallel()

	view, err := makeUTF16InputView(
		UTF16FromUnits([]uint16{'a', 0xD83D, 0xDE00, 'b'}),
		DefaultMatchOptions().Limits,
	)
	if err != nil {
		t.Fatalf("makeUTF16InputView(valid) error = %v", err)
	}
	assertInputBoundaries(t, view, []Index{
		{Exact: true},
		{UTF16: 1, Rune: 1, Byte: 1, Exact: true},
		{UTF16: 2, Rune: 1, Byte: 1},
		{UTF16: 3, Rune: 2, Byte: 5, Exact: true},
		{UTF16: 4, Rune: 3, Byte: 6, Exact: true},
	})
	if got, want := view.codePointBoundary, []bool{true, true, false, true, true}; !slices.Equal(got, want) {
		t.Fatalf("code-point boundaries = %v, want %v", got, want)
	}

	view, err = makeUTF16InputView(
		UTF16FromUnits([]uint16{0xD800, 'a', 0xDC00}),
		DefaultMatchOptions().Limits,
	)
	if err != nil {
		t.Fatalf("makeUTF16InputView(unpaired) error = %v", err)
	}
	assertInputBoundaries(t, view, []Index{
		{Exact: true},
		{UTF16: 1, Rune: -1, Byte: -1},
		{UTF16: 2, Rune: -1, Byte: -1},
		{UTF16: 3, Rune: -1, Byte: -1},
	})
}

func assertInputBoundaries(t *testing.T, view *inputView, want []Index) {
	t.Helper()
	if !slices.Equal(view.boundaries, want) {
		t.Fatalf("boundaries = %+v, want %+v", view.boundaries, want)
	}
}
