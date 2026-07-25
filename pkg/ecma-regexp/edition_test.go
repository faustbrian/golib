package ecmascript_test

import (
	"errors"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestEditionIsExplicitAndClosed(t *testing.T) {
	t.Parallel()

	if got := ecmascript.Edition2025.String(); got != "ECMAScript 2025" {
		t.Fatalf("Edition2025.String() = %q", got)
	}

	if _, err := ecmascript.ParseEdition("ECMAScript 2026"); !errors.Is(err, ecmascript.ErrUnsupportedEdition) {
		t.Fatalf("ParseEdition(future) error = %v, want ErrUnsupportedEdition", err)
	}
}

func TestFlagsRejectDuplicatesConflictsAndUnknownFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags string
		err   error
	}{
		{name: "duplicate", flags: "gg", err: ecmascript.ErrDuplicateFlag},
		{name: "unicode mode conflict", flags: "uv", err: ecmascript.ErrConflictingFlags},
		{name: "unknown", flags: "x", err: ecmascript.ErrUnsupportedFlag},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := ecmascript.ParseFlags(test.flags); !errors.Is(err, test.err) {
				t.Fatalf("ParseFlags(%q) error = %v, want %v", test.flags, err, test.err)
			}
		})
	}
}

func TestFlagsExposeECMAScriptModes(t *testing.T) {
	t.Parallel()

	flags, err := ecmascript.ParseFlags("dgimsuy")
	if err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	if !flags.HasIndices() || !flags.Global() || !flags.IgnoreCase() ||
		!flags.Multiline() || !flags.DotAll() || !flags.Unicode() ||
		flags.UnicodeSets() || !flags.Sticky() {
		t.Fatalf("ParseFlags() omitted a mode: %+v", flags)
	}

	unicodeSets, err := ecmascript.ParseFlags("v")
	if err != nil || !unicodeSets.UnicodeSets() {
		t.Fatalf("ParseFlags(v) = %+v, %v", unicodeSets, err)
	}
}
