package validation

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestContextAcceptsMetadataAtEveryExactLimit(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxMetadataEntries = 2
	limits.MaxMetadataKeyLength = 2
	limits.MaxMetadataValueLength = 2

	ctx, err := NewContext(
		limits,
		WithLocale("fi"),
		WithOperation("up"),
		WithMetadata("aa", "11"),
		WithMetadata("bb", "22"),
	)
	if err != nil {
		t.Fatalf("NewContext() at exact limits: %v", err)
	}
	if ctx.Locale() != "fi" || ctx.Operation() != "up" {
		t.Fatalf("context locale=%q operation=%q", ctx.Locale(), ctx.Operation())
	}
	if value, ok := ctx.Metadata("bb"); !ok || value != "22" {
		t.Fatalf("Metadata(bb) = %q, %v", value, ok)
	}
}

func TestCompositionContinuesPastNilAndStopsAfterDecisiveSuccess(t *testing.T) {
	ctx, err := NewContext(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	fail := ValidatorFunc[int](func(ctx Context, _ int) Report {
		return NewReport(ctx.Limits()).Add(
			NewViolation(ctx.Path(), "failed", Error, nil, nil),
		)
	})
	pass := ValidatorFunc[int](func(ctx Context, _ int) Report {
		return NewReport(ctx.Limits())
	})

	if report := All[int](CollectAll, nil, fail).Validate(ctx, 1); !report.HasCode("failed") {
		t.Fatalf("All(nil, fail) = %#v", report.Violations())
	}
	if report := Any[int](CollectAll, nil, fail).Validate(ctx, 1); !report.HasCode("failed") {
		t.Fatalf("Any(nil, fail) = %#v", report.Violations())
	}
	var trailingCalls atomic.Int32
	trailing := ValidatorFunc[int](func(ctx Context, _ int) Report {
		trailingCalls.Add(1)
		return NewReport(ctx.Limits()).Add(
			NewViolation(ctx.Path(), "trailing", Error, nil, nil),
		)
	})
	if report := Any(ShortCircuit, pass, trailing).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("Any(pass, trailing) = %#v", report.Violations())
	}
	if trailingCalls.Load() != 0 {
		t.Fatalf("trailing validator calls = %d", trailingCalls.Load())
	}
}

func TestAsyncAllRejectsCanceledWorkAndContinuesPastNil(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxCustomConcurrency = 1
	validationContext, err := NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	validator := AsyncValidatorFunc[int](func(
		_ context.Context, ctx Context, _ int,
	) Report {
		calls.Add(1)
		return NewReport(ctx.Limits()).Add(
			NewViolation(ctx.Path(), "executed", Warning, nil, nil),
		)
	})

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if report := AsyncAll(canceled, validationContext, 1, validator); !report.Empty() {
		t.Fatalf("canceled AsyncAll = %#v", report.Violations())
	}
	if calls.Load() != 0 {
		t.Fatalf("canceled validator calls = %d", calls.Load())
	}

	report := AsyncAll(context.Background(), validationContext, 1, nil, validator)
	if calls.Load() != 1 || report.Len() != 1 || !report.HasCode("executed") {
		t.Fatalf("AsyncAll(nil, validator) calls=%d report=%#v",
			calls.Load(), report.Violations())
	}
}

func TestPathRenderingPreservesSegmentsAfterItemsAndMixedBoundaries(t *testing.T) {
	pointer := RootPath().Append(Item()).Append(Field("after"))
	if got := pointer.JSONPointer(); got != "/-/after" {
		t.Fatalf("JSONPointer() = %q", got)
	}

	path := RootPath().Append(Index(0)).Append(Field("name"))
	length := len(path.String())
	if path.exceedsRenderedLength(length) || !path.exceedsRenderedLength(length-1) {
		t.Fatalf("path=%q length=%d", path.String(), length)
	}
}

func TestDiagnosticCodeAcceptsOnlyExactCharacterClasses(t *testing.T) {
	valid := []string{
		"a", "z", "A", "Z", "0", "9", "_", "-", ".", ":", "Az0_-.:",
	}
	for _, value := range valid {
		if !validCode(value, len(value)) {
			t.Errorf("validCode(%q, %d) = false", value, len(value))
		}
	}

	invalid := []string{"", "`", "{", "@", "[", "/", "!", "a!"}
	for _, value := range invalid {
		if validCode(value, 32) {
			t.Errorf("validCode(%q, 32) = true", value)
		}
	}
	if validCode("ab", 1) {
		t.Fatal("validCode accepted a value above the length limit")
	}
}

func TestSafeTextEnforcesExactControlCharacterBoundaries(t *testing.T) {
	for _, value := range []string{" ", "~", "ä"} {
		if !safeText(value) {
			t.Errorf("safeText(%q) = false", value)
		}
	}
	for _, value := range []string{"\x00", "\x1f", "\x7f", string([]byte{0xff})} {
		if safeText(value) {
			t.Errorf("safeText(%q) = true", value)
		}
	}
}
