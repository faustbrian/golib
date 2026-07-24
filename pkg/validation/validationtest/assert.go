// Package validationtest provides reusable report assertions and conformance
// helpers for consumers.
package validationtest

import validation "github.com/faustbrian/golib/pkg/validation"

// TestingT is the subset of testing.TB used by these helpers.
type TestingT interface {
	Helper()
	Fatalf(format string, arguments ...any)
}

// Case is a reusable typed validation fixture.
type Case[T any] struct {
	Name  string
	Value T
	Valid bool
	Codes []string
}

// RequireValid fails the test when report contains any violation.
func RequireValid(t TestingT, report validation.Report) {
	t.Helper()
	if !report.Empty() {
		t.Fatalf("expected valid report, got %v", report)
	}
}

// RequireCode fails the test unless report contains code.
func RequireCode(t TestingT, report validation.Report, code string) {
	t.Helper()
	if !report.HasCode(code) {
		t.Fatalf("expected violation code %q, got %v", code, report)
	}
}

// RejectMutations proves that every supplied defect is rejected.
func RejectMutations[T any](t TestingT, ctx validation.Context,
	validator validation.Validator[T], mutations []T,
) {
	t.Helper()
	for index, mutation := range mutations {
		if report := validator.Validate(ctx, mutation); report.Err() == nil {
			t.Fatalf("mutation %d was accepted", index)
		}
	}
}

// Conformance runs reusable fixtures against a validator.
func Conformance[T any](t TestingT, ctx validation.Context,
	validator validation.Validator[T], cases []Case[T],
) {
	t.Helper()
	for _, current := range cases {
		report := validator.Validate(ctx, current.Value)
		if current.Valid {
			if !report.Empty() {
				t.Fatalf("case %q: expected valid report, got %v",
					current.Name, report)
			}
			continue
		}
		if report.Err() == nil {
			t.Fatalf("case %q: invalid value was accepted", current.Name)
		}
		for _, code := range current.Codes {
			if !report.HasCode(code) {
				t.Fatalf("case %q: expected code %q, got %v",
					current.Name, code, report)
			}
		}
	}
}
