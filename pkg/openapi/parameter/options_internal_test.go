package parameter

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestStyleAllowedRejectsUnknownLocations(t *testing.T) {
	t.Parallel()

	if styleAllowed(Location("unknown"), Form, specversion.DialectOAS32) {
		t.Fatal("unknown parameter location accepted a style")
	}
}

func TestStyleAllowedAcceptsEveryPathStyle(t *testing.T) {
	t.Parallel()

	for _, style := range []Style{Matrix, Label, Simple} {
		if !styleAllowed(Path, style, specversion.DialectOAS32) {
			t.Fatalf("path style %q rejected", style)
		}
	}
	if styleAllowed(Path, Form, specversion.DialectOAS32) {
		t.Fatal("path accepted form style")
	}
}
