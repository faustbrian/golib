package validationobserve_test

import (
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/validationobserve"
)

type recorder struct {
	observations []validationobserve.Observation
}

func TestObserveSanitizesUnboundedOrUnsafeCustomLabels(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits(),
		validation.WithOperation("unsafe\noperation"))
	if err != nil {
		t.Fatal(err)
	}
	report := validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		validation.RootPath(), strings.Repeat("secret", 100), validation.Error,
		nil, nil))
	recorder := &recorder{}
	validationobserve.Report(ctx, report, recorder)
	got := recorder.observations[0]
	if got.Code != "invalid_violation" || got.Operation != "invalid_operation" {
		t.Fatalf("observation = %#v", got)
	}
}

func TestObserveAllowsEmptyOperationButNotEmptyCode(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		validation.RootPath(), "", validation.Error, nil, nil))
	recorder := &recorder{}
	validationobserve.Report(ctx, report, recorder)
	if recorder.observations[0].Code != "invalid_violation" ||
		recorder.observations[0].Operation != "" {
		t.Fatalf("observation = %#v", recorder.observations[0])
	}
}

func TestObserveAcceptsOnlyExactOperationLabelCharacterClasses(t *testing.T) {
	valid := []string{"a", "z", "A", "Z", "0", "9", "_", "-", ".", ":", "Az0_-."}
	invalid := []string{"`", "{", "@", "[", "/", "!"}
	for _, operation := range append(valid, invalid...) {
		t.Run(operation, func(t *testing.T) {
			ctx, err := validation.NewContext(
				validation.DefaultLimits(),
				validation.WithOperation(operation),
			)
			if err != nil {
				t.Fatal(err)
			}
			report := validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
				validation.RootPath(), "code", validation.Error, nil, nil,
			))
			recorder := &recorder{}
			validationobserve.Report(ctx, report, recorder)
			want := operation
			if strings.Contains("`{@[/!", operation) {
				want = "invalid_operation"
			}
			if got := recorder.observations[0].Operation; got != want {
				t.Fatalf("operation = %q, want %q", got, want)
			}
		})
	}
}

func (recorder *recorder) Observe(observation validationobserve.Observation) {
	recorder.observations = append(recorder.observations, observation)
}

func TestObserveEmitsOnlyBoundedNonSensitiveLabels(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits(), validation.WithOperation("create"))
	if err != nil {
		t.Fatal(err)
	}
	report := validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		validation.RootPath().Append(validation.Field("secret-token")), "required",
		validation.Error, map[string]string{"value": "password"}, nil,
	))
	recorder := &recorder{}
	validationobserve.Report(ctx, report, recorder)
	if len(recorder.observations) != 1 {
		t.Fatalf("observations = %#v", recorder.observations)
	}
	got := recorder.observations[0]
	if got.Code != "required" || got.Operation != "create" || got.Severity != "error" {
		t.Fatalf("observation = %#v", got)
	}
	warning := validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		validation.RootPath(), "deprecated", validation.Warning, nil, nil))
	validationobserve.Report(ctx, warning, recorder)
	if recorder.observations[1].Severity != "warning" {
		t.Fatalf("warning = %#v", recorder.observations[1])
	}
}
