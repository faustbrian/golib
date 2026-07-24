package validation_test

import (
	"context"
	"errors"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestAllHonorsExecutionMode(t *testing.T) {
	ctx := testContext(t)
	calls := 0
	failing := func(code string) validation.Validator[int] {
		return validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
			calls++
			return validation.NewReport(ctx.Limits()).Add(
				validation.NewViolation(ctx.Path(), code, validation.Error, nil, nil),
			)
		})
	}

	report := validation.All(validation.ShortCircuit, failing("first"), failing("second")).
		Validate(ctx, 1)
	if calls != 1 || report.Len() != 1 || !report.HasCode("first") {
		t.Fatalf("short circuit calls=%d report=%v", calls, report)
	}
	calls = 0
	report = validation.All(validation.CollectAll, failing("first"), failing("second")).
		Validate(ctx, 1)
	if calls != 2 || report.Len() != 2 || !report.HasCode("second") {
		t.Fatalf("collect all calls=%d report=%v", calls, report)
	}
}

func TestAnyNotAndConditionalTruthTables(t *testing.T) {
	ctx := testContext(t)
	pass := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		return validation.NewReport(ctx.Limits())
	})
	fail := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "failed", validation.Error, nil, nil),
		)
	})

	if report := validation.Any(validation.CollectAll, fail, pass).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("Any(fail, pass) = %v", report)
	}
	if report := validation.Any(validation.CollectAll, fail, fail).Validate(ctx, 1); report.Len() != 1 {
		t.Fatalf("Any(fail, fail) = %v, want deduplicated failures", report)
	}
	if report := validation.Not(pass).Validate(ctx, 1); !report.HasCode("not") {
		t.Fatalf("Not(pass) = %v", report)
	}
	if report := validation.Not(fail).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("Not(fail) = %v", report)
	}
	if report := validation.When(func(value int) bool { return value > 0 }, fail, pass).
		Validate(ctx, 1); !report.HasCode("failed") {
		t.Fatalf("When(true) = %v", report)
	}
	if report := validation.When(func(value int) bool { return value > 0 }, fail, pass).
		Validate(ctx, -1); !report.Empty() {
		t.Fatalf("When(false) = %v", report)
	}
}

func TestDependentRunsOnlyAfterPrerequisitePasses(t *testing.T) {
	ctx := testContext(t)
	calls := 0
	pass := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		return validation.NewReport(ctx.Limits())
	})
	fail := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "failed", validation.Error, nil, nil),
		)
	})
	dependent := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		calls++
		return validation.NewReport(ctx.Limits())
	})

	validation.Dependent(fail, dependent).Validate(ctx, 1)
	if calls != 0 {
		t.Fatalf("dependent calls after failure = %d", calls)
	}
	validation.Dependent(pass, dependent).Validate(ctx, 1)
	if calls != 1 {
		t.Fatalf("dependent calls after success = %d", calls)
	}
}

func TestSuccessfulCompositionPreservesAdvisoryFindings(t *testing.T) {
	ctx := testContext(t)
	warning := func(code string) validation.Validator[int] {
		return validation.ValidatorFunc[int](func(ctx validation.Context,
			_ int,
		) validation.Report {
			return validation.NewReport(ctx.Limits()).Add(
				validation.NewViolation(ctx.Path(), code, validation.Warning, nil, nil),
			)
		})
	}
	failure := validation.ValidatorFunc[int](func(ctx validation.Context,
		_ int,
	) validation.Report {
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "failure", validation.Error, nil, nil),
		)
	})

	report := validation.Any(validation.CollectAll,
		failure, warning("first"), warning("second")).Validate(ctx, 1)
	violations := report.Violations()
	if report.HasErrors() || len(violations) != 2 ||
		violations[0].Code() != "first" || violations[1].Code() != "second" {
		t.Fatalf("Any advisories = %#v", violations)
	}

	report = validation.Dependent(warning("prerequisite"), warning("dependent")).
		Validate(ctx, 1)
	violations = report.Violations()
	if report.HasErrors() || len(violations) != 2 ||
		violations[0].Code() != "prerequisite" ||
		violations[1].Code() != "dependent" {
		t.Fatalf("Dependent advisories = %#v", violations)
	}
}

func TestAsyncContractPropagatesCancellation(t *testing.T) {
	validationContext := testContext(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	async := validation.AsyncValidatorFunc[string](func(
		ctx context.Context, validationContext validation.Context, _ string,
	) validation.Report {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("context error = %v", ctx.Err())
		}
		return validation.NewReport(validationContext.Limits())
	})
	if report := async.ValidateAsync(ctx, validationContext, "value"); !report.Empty() {
		t.Fatalf("ValidateAsync() = %v", report)
	}
}

func testContext(t *testing.T) validation.Context {
	t.Helper()
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
