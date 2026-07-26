package ecmascript

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrDuplicateFlag    = errors.New("duplicate regular expression flag")
	ErrConflictingFlags = errors.New("conflicting regular expression flags")
	ErrUnsupportedFlag  = errors.New("unsupported regular expression flag")
)

// Flags is an immutable set of ECMA-262 RegExp flags.
type Flags struct {
	bits uint16
}

const (
	flagHasIndices uint16 = 1 << iota
	flagGlobal
	flagIgnoreCase
	flagMultiline
	flagDotAll
	flagUnicode
	flagUnicodeSets
	flagSticky
)

// ParseFlags parses the exact ECMA-262 2025 flag alphabet. The u and v modes
// are mutually exclusive by grammar.
func ParseFlags(source string) (Flags, error) {
	var flags Flags
	for _, char := range source {
		bit, ok := flagBit(char)
		if !ok {
			return Flags{}, fmt.Errorf("%w: %q", ErrUnsupportedFlag, char)
		}
		if flags.bits&bit != 0 {
			return Flags{}, fmt.Errorf("%w: %q", ErrDuplicateFlag, char)
		}
		flags.bits |= bit
	}
	if flags.Unicode() && flags.UnicodeSets() {
		return Flags{}, fmt.Errorf("%w: u and v", ErrConflictingFlags)
	}

	return flags, nil
}

func flagBit(char rune) (uint16, bool) {
	switch char {
	case 'd':
		return flagHasIndices, true
	case 'g':
		return flagGlobal, true
	case 'i':
		return flagIgnoreCase, true
	case 'm':
		return flagMultiline, true
	case 's':
		return flagDotAll, true
	case 'u':
		return flagUnicode, true
	case 'v':
		return flagUnicodeSets, true
	case 'y':
		return flagSticky, true
	default:
		return 0, false
	}
}

func (f Flags) HasIndices() bool { return f.bits&flagHasIndices != 0 }
func (f Flags) Global() bool     { return f.bits&flagGlobal != 0 }
func (f Flags) IgnoreCase() bool { return f.bits&flagIgnoreCase != 0 }
func (f Flags) Multiline() bool  { return f.bits&flagMultiline != 0 }
func (f Flags) DotAll() bool     { return f.bits&flagDotAll != 0 }
func (f Flags) Unicode() bool    { return f.bits&flagUnicode != 0 }
func (f Flags) UnicodeSets() bool {
	return f.bits&flagUnicodeSets != 0
}
func (f Flags) Sticky() bool { return f.bits&flagSticky != 0 }

func (f Flags) String() string {
	var result strings.Builder
	for _, flag := range "dgimsuvy" {
		bit, _ := flagBit(flag)
		if f.bits&bit != 0 {
			result.WriteRune(flag)
		}
	}

	return result.String()
}
