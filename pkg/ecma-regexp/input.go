package ecmascript

import (
	"unicode/utf16"
	"unicode/utf8"
)

// Index maps one ECMAScript UTF-16 boundary to Go byte and rune boundaries.
// Exact is false when a UTF-16 boundary splits a surrogate pair and therefore
// has no exact Go string or rune boundary.
type Index struct {
	UTF16 int
	Rune  int
	Byte  int
	Exact bool
}

// IndexSpan is a half-open range in all supported index coordinate systems.
type IndexSpan struct {
	Start Index
	End   Index
}

type inputView struct {
	units             []uint16
	boundaries        []Index
	codePointBoundary []bool
}

func makeInputView(source string, limits MatchLimits) (*inputView, error) {
	if uint64(len(source)) > limits.InputBytes {
		return nil, &LimitError{Kind: LimitInputBytes, Limit: limits.InputBytes, Used: uint64(len(source))}
	}
	runeCount := utf8.RuneCountInString(source)
	if uint64(runeCount) > limits.InputRunes {
		return nil, &LimitError{Kind: LimitInputRunes, Limit: limits.InputRunes, Used: uint64(runeCount)}
	}

	view := &inputView{
		units:             make([]uint16, 0, runeCount),
		boundaries:        make([]Index, 1, runeCount+1),
		codePointBoundary: make([]bool, 1, runeCount+1),
	}
	view.boundaries[0] = Index{Exact: true}
	view.codePointBoundary[0] = true
	runeOffset := 0
	for byteOffset, char := range source {
		size := 1
		if char != utf8.RuneError || source[byteOffset] >= utf8.RuneSelf {
			_, size = utf8.DecodeRuneInString(source[byteOffset:])
		}
		encoded := utf16.Encode([]rune{char})
		for unitIndex, unit := range encoded {
			view.units = append(view.units, unit)
			exact := unitIndex == len(encoded)-1
			view.codePointBoundary = append(view.codePointBoundary, exact)
			boundaryByte := byteOffset
			boundaryRune := runeOffset
			if exact {
				boundaryByte += size
				boundaryRune++
			}
			view.boundaries = append(view.boundaries, Index{
				UTF16: len(view.units),
				Rune:  boundaryRune,
				Byte:  boundaryByte,
				Exact: exact,
			})
		}
		runeOffset++
	}

	return view, nil
}

func makeUTF16InputView(input UTF16String, limits MatchLimits) (*inputView, error) {
	storageBytes := uint64(len(input.units)) * 2
	if storageBytes > limits.InputBytes {
		return nil, &LimitError{Kind: LimitInputBytes, Limit: limits.InputBytes, Used: storageBytes}
	}

	codePoints := 0
	validScalar := true
	codePointBoundary := make([]bool, len(input.units)+1)
	codePointBoundary[0] = true
	for index := 0; index < len(input.units); {
		unit := input.units[index]
		width := 1
		if isHighSurrogate(unit) && index+1 < len(input.units) && isLowSurrogate(input.units[index+1]) {
			width = 2
		} else if isHighSurrogate(unit) || isLowSurrogate(unit) {
			validScalar = false
		}
		index += width
		codePointBoundary[index] = true
		codePoints++
	}
	if uint64(codePoints) > limits.InputRunes {
		return nil, &LimitError{Kind: LimitInputRunes, Limit: limits.InputRunes, Used: uint64(codePoints)}
	}

	view := &inputView{
		units:             append([]uint16(nil), input.units...),
		boundaries:        make([]Index, len(input.units)+1),
		codePointBoundary: codePointBoundary,
	}
	view.boundaries[0] = Index{Exact: true}
	if !validScalar {
		for index := 1; index < len(view.boundaries); index++ {
			view.boundaries[index] = Index{UTF16: index, Rune: -1, Byte: -1}
		}
		return view, nil
	}

	runeOffset := 0
	byteOffset := 0
	for index := 0; index < len(input.units); {
		unit := input.units[index]
		if isHighSurrogate(unit) {
			view.boundaries[index+1] = Index{UTF16: index + 1, Rune: runeOffset, Byte: byteOffset}
			char := utf16.DecodeRune(rune(unit), rune(input.units[index+1]))
			index += 2
			runeOffset++
			byteOffset += utf8.RuneLen(char)
			view.boundaries[index] = Index{UTF16: index, Rune: runeOffset, Byte: byteOffset, Exact: true}
			continue
		}
		index++
		runeOffset++
		byteOffset += utf8.RuneLen(rune(unit))
		view.boundaries[index] = Index{UTF16: index, Rune: runeOffset, Byte: byteOffset, Exact: true}
	}

	return view, nil
}

func (v *inputView) span(start, end int) IndexSpan {
	return IndexSpan{Start: v.boundaries[start], End: v.boundaries[end]}
}
