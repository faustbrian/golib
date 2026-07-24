package validationservice_test

import (
	"context"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/validationservice"
)

func TestHookAndChainRemainTransportNeutral(t *testing.T) {
	limits := validation.DefaultLimits()
	first := validationservice.Hook[string](func(_ context.Context, ctx validation.Context, _ string) validation.Report {
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "first", validation.Warning, nil, nil),
		)
	})
	second := validationservice.Hook[string](func(_ context.Context, ctx validation.Context, _ string) validation.Report {
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "second", validation.Error, nil, nil),
		)
	})
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	report := validationservice.Chain(validation.CollectAll, first, second).
		Validate(context.Background(), ctx, "input")
	if report.Len() != 2 || !report.HasCode("second") {
		t.Fatalf("report = %#v", report.Violations())
	}
}

func TestChainSkipsNilAndShortCircuits(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	fail := validationservice.Hook[int](func(_ context.Context, ctx validation.Context, _ int) validation.Report {
		calls++
		return validation.NewReport(ctx.Limits()).Add(
			validation.NewViolation(ctx.Path(), "stop", validation.Error, nil, nil))
	})
	after := validationservice.Hook[int](func(_ context.Context, ctx validation.Context, _ int) validation.Report {
		calls++
		return validation.NewReport(ctx.Limits())
	})
	var nilHook validationservice.Validator[int]
	validationservice.Chain(validation.ShortCircuit, nilHook, fail, after).
		Validate(context.Background(), ctx, 1)
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}
