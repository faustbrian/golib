package jsonschema

import (
	"testing"
	"time"
)

func TestECMARegexpAdapterPreservesPatternIdentity(t *testing.T) {
	t.Parallel()

	compiled, err := ecmaRegexpEngine(time.Second)(`^\cc$`)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.String() != `^\cc$` || !compiled.MatchString("\x03") {
		t.Fatalf("compiled regexp = %q", compiled.String())
	}
	if _, err := ecmaRegexpEngine(time.Second)(`[`); err == nil {
		t.Fatal("invalid pattern compiled")
	}
}

func TestValidationBoundaryRepanicsUnexpectedFailures(t *testing.T) {
	t.Parallel()

	defer func() {
		if recovered := recover(); recovered != "unexpected" {
			t.Fatalf("recovered panic = %#v", recovered)
		}
	}()
	_ = runValidation(func() error { panic("unexpected") })
}
