package validation_test

import (
	"errors"
	"fmt"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
)

type panickingStringValidator struct{}

func (panickingStringValidator) Validate(validation.Context, string) validation.Report {
	panic("secret-token")
}

func TestContextIsImmutableAndBounded(t *testing.T) {
	limits := validation.DefaultLimits()
	ctx, err := validation.NewContext(limits,
		validation.WithLocale("fi-FI"),
		validation.WithOperation("create"),
		validation.WithMetadata("tenant", "north"),
	)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}

	copied := ctx.WithPath(validation.Field("profile"))
	if got, want := ctx.Path().String(), ""; got != want {
		t.Fatalf("original path = %q, want %q", got, want)
	}
	if got, want := copied.Path().String(), "profile"; got != want {
		t.Fatalf("copy path = %q, want %q", got, want)
	}
	if got, ok := copied.Metadata("tenant"); !ok || got != "north" {
		t.Fatalf("Metadata(tenant) = %q, %v", got, ok)
	}

	tiny := limits
	tiny.MaxMetadataEntries = 1
	_, err = validation.NewContext(tiny,
		validation.WithMetadata("a", "b"),
		validation.WithMetadata("c", "d"),
	)
	if !errors.Is(err, validation.ErrLimitExceeded) {
		t.Fatalf("NewContext() error = %v, want ErrLimitExceeded", err)
	}
}

func TestPathPreservesTypedSegmentsAndEscapesPointers(t *testing.T) {
	path := validation.RootPath().
		Append(validation.Field("items")).
		Append(validation.Index(2)).
		Append(validation.Key("a/b~c")).
		Append(validation.Item())

	if got, want := path.String(), "items[2][a/b~c][]"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got, want := path.JSONPointer(), "/items/2/a~1b~0c/-"; got != want {
		t.Fatalf("JSONPointer() = %q, want %q", got, want)
	}
	segments := path.Segments()
	segments[0] = validation.Field("changed")
	if got := path.String(); got != "items[2][a/b~c][]" {
		t.Fatalf("Segments mutated path: %q", got)
	}
}

func TestReportOrdersDeduplicatesBoundsAndDoesNotLeakValues(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxViolations = 2
	password := validation.NewViolation(
		validation.RootPath().Append(validation.Field("password")),
		"required",
		validation.Error,
		map[string]string{"minimum": "12"},
		errors.New("safe cause"),
	)
	report := validation.NewReport(limits).
		Add(password).
		Add(password).
		Add(validation.NewViolation(validation.RootPath(), "other", validation.Warning, nil, nil)).
		Add(validation.NewViolation(validation.RootPath(), "ignored", validation.Error, nil, nil))

	if got, want := report.Len(), 2; got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}
	if !report.Truncated() {
		t.Fatal("Truncated() = false, want true")
	}
	if !errors.Is(report.Err(), validation.ErrInvalid) {
		t.Fatalf("Err() = %v, want ErrInvalid", report.Err())
	}
	var invalid *validation.InvalidError
	if !errors.As(report.Err(), &invalid) || invalid.Report().Len() != 2 {
		t.Fatalf("Err() does not expose InvalidError report: %v", report.Err())
	}
	if got := fmt.Sprint(report); got != "validation failed with 2 violations (truncated)" {
		t.Fatalf("String() = %q", got)
	}
	if got := fmt.Sprint(password); got != "password: required" {
		t.Fatalf("Violation String() = %q", got)
	}
}

func TestValueDistinguishesMissingNullPresentEmptyAndZero(t *testing.T) {
	tests := []struct {
		name    string
		value   validation.Value[string]
		state   validation.Presence
		empty   bool
		zero    bool
		present bool
	}{
		{"missing", validation.Missing[string](), validation.MissingState, false, false, false},
		{"null", validation.Null[string](), validation.NullState, false, false, false},
		{"empty", validation.Present(""), validation.PresentState, true, true, true},
		{"value", validation.Present("0"), validation.PresentState, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value.Presence() != tt.state || tt.value.IsEmpty() != tt.empty ||
				tt.value.IsZero() != tt.zero || tt.value.IsPresent() != tt.present {
				t.Fatalf("state mismatch: %#v", tt.value)
			}
		})
	}
}

func TestFunctionValidatorUsesContextPathAndPanicPolicy(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	ctx = ctx.WithPath(validation.Field("name"))
	validator := validation.ValidatorFunc[string](func(ctx validation.Context, _ string) validation.Report {
		return validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
			ctx.Path(), "bad_name", validation.Error, nil, nil,
		))
	})
	if got := validator.Validate(ctx, "unsafe").Violations()[0].Path().String(); got != "name" {
		t.Fatalf("violation path = %q, want name", got)
	}

	isolated := validation.IsolatePanics[string](panickingStringValidator{})
	report := isolated.Validate(ctx, "secret-input")
	if !report.HasCode("validator_panic") || fmt.Sprint(report) != "validation failed with 1 violation" {
		t.Fatalf("isolated report = %v", report)
	}
}
