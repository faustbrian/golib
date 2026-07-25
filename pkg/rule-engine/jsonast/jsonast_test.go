package jsonast_test

import (
	"os"
	"testing"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	"github.com/faustbrian/golib/pkg/rule-engine/jsonast"
)

func TestFixtures(t *testing.T) {
	t.Parallel()

	valid, err := os.ReadFile("testdata/location-routing.json")
	if err != nil {
		t.Fatal(err)
	}
	set, diagnostics, err := jsonast.Parse(valid, ruleengine.DefaultLimits())
	if err != nil || len(diagnostics) != 0 || set.ID != "location-routing" {
		t.Fatalf("Parse(valid) = %#v, %#v, %v", set, diagnostics, err)
	}
	canonical, err := jsonast.Marshal(set)
	if err != nil || len(canonical) == 0 {
		t.Fatalf("Marshal() = %q, %v", canonical, err)
	}

	invalid, err := os.ReadFile("testdata/invalid-unknown-operator.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, diagnostics, err := jsonast.Parse(invalid, ruleengine.DefaultLimits()); err == nil || len(diagnostics) == 0 {
		t.Fatalf("Parse(invalid) diagnostics = %#v, error = %v", diagnostics, err)
	}
}
