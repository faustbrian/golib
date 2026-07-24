package validationtest_test

import (
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
	"github.com/faustbrian/golib/pkg/validation/validationtest"
)

func TestMutationCasesProveValidatorRejectsDefects(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	validationtest.RequireValid(t, rules.Range(1, 3).Validate(ctx, 2))
	validationtest.RequireCode(t, rules.Range(1, 3).Validate(ctx, 4), "range")
	validationtest.RejectMutations(t, ctx, rules.Range(1, 3), []int{0, 4})
}

type recordingT struct {
	fatal bool
}

func (test *recordingT) Helper() {}

func (test *recordingT) Fatalf(string, ...any) { test.fatal = true }

func TestAssertionFailuresAreReported(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	bad := rules.Range(1, 3).Validate(ctx, 4)
	checks := []func(*recordingT){
		func(test *recordingT) { validationtest.RequireValid(test, bad) },
		func(test *recordingT) { validationtest.RequireCode(test, bad, "missing") },
		func(test *recordingT) { validationtest.RejectMutations(test, ctx, rules.Range(1, 3), []int{2}) },
	}
	for index, check := range checks {
		recorder := &recordingT{}
		check(recorder)
		if !recorder.fatal {
			t.Errorf("check %d did not report failure", index)
		}
	}
}

func TestConformanceRunsReusableCases(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	cases := []validationtest.Case[int]{
		{Name: "inside", Value: 2, Valid: true},
		{Name: "outside", Value: 4, Codes: []string{"range"}},
	}
	recorder := &recordingT{}
	validationtest.Conformance(recorder, ctx, rules.Range(1, 3), cases)
	if recorder.fatal {
		t.Fatal("valid conformance table reported failure")
	}
	cases[0].Valid = false
	validationtest.Conformance(recorder, ctx, rules.Range(1, 3), cases[:1])
	if !recorder.fatal {
		t.Fatal("invalid conformance expectation was not detected")
	}
	for _, failing := range [][]validationtest.Case[int]{
		{{Name: "unexpected invalid", Value: 4, Valid: true}},
		{{Name: "missing code", Value: 4, Codes: []string{"missing"}}},
	} {
		recorder = &recordingT{}
		validationtest.Conformance(recorder, ctx, rules.Range(1, 3), failing)
		if !recorder.fatal {
			t.Fatalf("case %#v did not report failure", failing)
		}
	}
}
