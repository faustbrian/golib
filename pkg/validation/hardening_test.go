package validation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestContextRejectsEveryInvalidBoundAndOversizedMetadata(t *testing.T) {
	base := DefaultLimits()
	mutations := []func(*Limits){
		func(l *Limits) { l.MaxDepth = 0 },
		func(l *Limits) { l.MaxCollectionSize = 0 },
		func(l *Limits) { l.MaxStringLength = 0 },
		func(l *Limits) { l.MaxViolations = 0 },
		func(l *Limits) { l.MaxPathLength = 0 },
		func(l *Limits) { l.MaxMetadataEntries = 0 },
		func(l *Limits) { l.MaxMetadataKeyLength = 0 },
		func(l *Limits) { l.MaxMetadataValueLength = 0 },
		func(l *Limits) { l.MaxRegexPatternLength = 0 },
		func(l *Limits) { l.MaxCustomConcurrency = 0 },
		func(l *Limits) { l.MaxStructFields = 0 },
		func(l *Limits) { l.MaxTagLength = 0 },
		func(l *Limits) { l.MaxCompiledPlans = 0 },
	}
	for index, mutate := range mutations {
		limits := base
		mutate(&limits)
		if _, err := NewContext(limits); !errors.Is(err, ErrInvalidLimit) {
			t.Errorf("mutation %d error = %v", index, err)
		}
	}
	limits := base
	limits.MaxMetadataKeyLength = 1
	if _, err := NewContext(limits, nil, WithMetadata("long", "x")); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("key limit error = %v", err)
	}
	limits = base
	limits.MaxMetadataValueLength = 1
	if _, err := NewContext(limits, WithMetadata("x", "long")); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("value limit error = %v", err)
	}
	if _, err := NewContext(limits, WithLocale("long")); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("locale limit error = %v", err)
	}
	if _, err := NewContext(limits, WithOperation("long")); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("operation limit error = %v", err)
	}
	ctx, err := NewContext(base, WithLocale("fi"), WithOperation("update"))
	if err != nil || ctx.Locale() != "fi" || ctx.Operation() != "update" {
		t.Fatalf("context = %#v, %v", ctx, err)
	}
}

func TestCoreAccessorsAndSafeFormatting(t *testing.T) {
	segment := Field("name")
	if segment.Kind() != FieldSegment || segment.Value() != "name" {
		t.Fatalf("segment = %#v", segment)
	}
	path := RootPath().Append(Index(0)).Append(Field("name"))
	if path.String() != "[0].name" {
		t.Fatalf("path = %q", path.String())
	}
	cause := errors.New("safe")
	violation := NewViolation(RootPath(), "code", Warning,
		map[string]string{"a": "b"}, cause)
	parameters := violation.Parameters()
	parameters["a"] = "changed"
	if violation.Code() != "code" || violation.Severity() != Warning ||
		!errors.Is(violation.Cause(), cause) || violation.Parameters()["a"] != "b" ||
		violation.String() != "code" {
		t.Fatalf("violation = %#v", violation)
	}
	report := NewReport(DefaultLimits()).Add(violation)
	if report.Err() != nil || report.HasCode("missing") {
		t.Fatalf("warning report = %v, err=%v", report, report.Err())
	}
}

func TestReportMergePropagatesTruncationAndInvalidErrorMessage(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxViolations = 1
	first := NewReport(limits).Add(NewViolation(RootPath(), "first", Error, nil, nil))
	second := NewReport(limits).
		Add(NewViolation(RootPath(), "second", Error, nil, nil)).
		Add(NewViolation(RootPath(), "third", Error, nil, nil))
	merged := first.Merge(second)
	if !merged.Truncated() || merged.Len() != 1 {
		t.Fatalf("merged = %#v", merged)
	}
	err := merged.Err()
	if err == nil {
		t.Fatal("merged error = nil")
	}
	if got := err.Error(); got != merged.String() {
		t.Fatalf("error = %q, report = %q", got, merged.String())
	}
}

func TestTruncatedReportRemainsInvalidWhenDroppedViolationIsBlocking(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxViolations = 1
	report := NewReport(limits).
		Add(NewViolation(RootPath(), "warning", Warning, nil, nil)).
		Add(NewViolation(RootPath(), "blocked", Error, nil, nil))
	if !report.Truncated() || !report.HasErrors() || !errors.Is(report.Err(), ErrInvalid) {
		t.Fatalf("report truncated=%v hasErrors=%v err=%v",
			report.Truncated(), report.HasErrors(), report.Err())
	}
}

func TestMergePreservesDroppedBlockingState(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxViolations = 1
	other := NewReport(limits).
		Add(NewViolation(RootPath(), "warning", Warning, nil, nil)).
		Add(NewViolation(RootPath(), "blocked", Error, nil, nil))
	merged := NewReport(limits).Merge(other)
	if !merged.Truncated() || !merged.HasErrors() ||
		!errors.Is(merged.Err(), ErrInvalid) {
		t.Fatalf("merged truncated=%v hasErrors=%v err=%v",
			merged.Truncated(), merged.HasErrors(), merged.Err())
	}
}

func TestReportReplacesOversizedPathsWithBoundedViolation(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxPathLength = 3
	report := NewReport(limits).Add(NewViolation(
		RootPath().Append(Field("secret-value")), "required", Error, nil, nil,
	))
	violations := report.Violations()
	if len(violations) != 1 || violations[0].Code() != "path_limit" ||
		violations[0].Path().String() != "" {
		t.Fatalf("violations = %#v", violations)
	}
}

func TestPathLengthBudgetMatchesStableRenderingAtBoundaries(t *testing.T) {
	paths := []Path{
		RootPath().Append(Field("a")).Append(Field("b")),
		RootPath().Append(Index(1)),
		RootPath().Append(Key("x")),
		RootPath().Append(Item()),
	}
	for index, path := range paths {
		length := len(path.String())
		if path.exceedsRenderedLength(length) ||
			!path.exceedsRenderedLength(length-1) {
			t.Errorf("path %d rendering = %q length=%d", index, path, length)
		}
	}
}

func TestReportRejectsUnboundedOrUnsafeCustomDiagnostics(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxMetadataEntries = 1
	limits.MaxMetadataKeyLength = 4
	limits.MaxMetadataValueLength = 4
	cases := []Violation{
		NewViolation(RootPath(), "bad\ncode", Error, nil, nil),
		NewViolation(RootPath(), "long-code", Error, nil, nil),
		NewViolation(RootPath(), "code", Severity(99), nil, nil),
		NewViolation(RootPath(), "code", Error,
			map[string]string{"a": "safe", "b": "safe"}, nil),
		NewViolation(RootPath(), "code", Error,
			map[string]string{"long-key": "safe"}, nil),
		NewViolation(RootPath(), "code", Error,
			map[string]string{"key": "secret-value"}, nil),
		NewViolation(RootPath(), "code", Error,
			map[string]string{"key": "x\ny"}, nil),
		NewViolation(RootPath(), "code", Error,
			map[string]string{"key": string([]byte{0xff})}, nil),
	}
	for index, violation := range cases {
		report := NewReport(limits).Add(violation)
		retained := report.Violations()
		if !report.HasErrors() || len(retained) != 1 ||
			retained[0].Code() != "invalid_violation" ||
			retained[0].Severity() != Error ||
			len(retained[0].Parameters()) != 0 ||
			retained[0].Path().String() != "" {
			t.Errorf("case %d report = %#v", index, retained)
		}
		if strings.Contains(fmt.Sprint(report), "secret-value") {
			t.Errorf("case %d leaked rejected diagnostic", index)
		}
	}
	valid := NewReport(limits).Add(NewViolation(RootPath(), "code", Warning,
		map[string]string{"key": "safe"}, nil))
	if valid.HasErrors() || !valid.HasCode("code") {
		t.Fatalf("bounded diagnostic = %#v", valid.Violations())
	}
	unsafePath := NewViolation(RootPath().Append(Field("line\nsecret")),
		"code", Error, nil, nil)
	if strings.Contains(unsafePath.String(), "\n") {
		t.Fatalf("unsafe formatted path = %q", unsafePath.String())
	}
}

func TestValueCoversTypedEmptyKindsAndGet(t *testing.T) {
	value := Present(3)
	if got, ok := value.Get(); !ok || got != 3 || value.IsEmpty() {
		t.Fatalf("value = %#v", value)
	}
	var dynamic any
	if !Present(dynamic).IsEmpty() || !Present(dynamic).IsZero() {
		t.Fatal("nil interface was not empty")
	}
	if !Present([]int{}).IsEmpty() || Present([]int{1}).IsEmpty() ||
		!Present([0]int{}).IsEmpty() {
		t.Fatal("collection emptiness mismatch")
	}
}

func TestZeroContextFailsClosedAndUsesSafeDefaultLimits(t *testing.T) {
	var ctx Context
	report := NewReport(ctx.Limits()).Add(NewViolation(RootPath(), "required", Error, nil, nil))
	if !errors.Is(report.Err(), ErrInvalid) || ctx.Limits().MaxViolations == 0 {
		t.Fatalf("report=%v limits=%#v", report, ctx.Limits())
	}
	async := AsyncValidatorFunc[int](func(_ context.Context, ctx Context, _ int) Report {
		return NewReport(ctx.Limits()).Add(NewViolation(ctx.Path(), "async", Error, nil, nil))
	})
	if report := AsyncAll(context.Background(), ctx, 1, async); !report.HasCode("async") {
		t.Fatalf("async report = %v", report)
	}
}

func TestValidatorAdaptersContainPanicsWithoutLeakingPayloads(t *testing.T) {
	ctx, err := NewContext(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	validator := ValidatorFunc[string](func(Context, string) Report {
		panic("password=secret-input")
	})
	report := validator.Validate(ctx.WithPath(Field("password")), "secret-input")
	if !report.HasCode("validator_panic") ||
		!errors.Is(report.Violations()[0].Cause(), ErrValidatorPanic) {
		t.Fatalf("synchronous panic report = %#v", report.Violations())
	}
	if strings.Contains(fmt.Sprint(report), "secret-input") ||
		strings.Contains(fmt.Sprint(report), "password=secret-input") {
		t.Fatalf("synchronous panic leaked payload: %v", report)
	}

	async := AsyncValidatorFunc[string](func(context.Context, Context, string) Report {
		panic("token=secret-input")
	})
	report = async.ValidateAsync(context.Background(),
		ctx.WithPath(Field("token")), "secret-input")
	if !report.HasCode("validator_panic") ||
		!errors.Is(report.Violations()[0].Cause(), ErrValidatorPanic) {
		t.Fatalf("asynchronous panic report = %#v", report.Violations())
	}
	if strings.Contains(fmt.Sprint(report), "secret-input") ||
		strings.Contains(fmt.Sprint(report), "token=secret-input") {
		t.Fatalf("asynchronous panic leaked payload: %v", report)
	}
}

func TestReportDedupUsesTypedPathIdentity(t *testing.T) {
	flat := RootPath().Append(Field("a.b"))
	nested := RootPath().Append(Field("a")).Append(Field("b"))
	key := RootPath().Append(Field("items")).Append(Key("[0]"))
	index := RootPath().Append(Field("items")).Append(Index(0))
	report := NewReport(DefaultLimits()).
		Add(NewViolation(flat, "invalid", Error, nil, nil)).
		Add(NewViolation(nested, "invalid", Error, nil, nil)).
		Add(NewViolation(key, "invalid", Error, nil, nil)).
		Add(NewViolation(index, "invalid", Error, nil, nil))
	if report.Len() != 4 {
		t.Fatalf("typed paths collapsed: %#v", report.Violations())
	}
}

func TestReportDedupLengthPrefixesParameterIdentity(t *testing.T) {
	path := RootPath().Append(Field("field"))
	report := NewReport(DefaultLimits()).
		Add(NewViolation(path, "invalid", Error,
			map[string]string{"ab": "c"}, nil)).
		Add(NewViolation(path, "invalid", Error,
			map[string]string{"a": "bc"}, nil))
	if report.Len() != 2 {
		t.Fatalf("parameter identities collapsed: %#v", report.Violations())
	}
}

func TestCompositionNilAndShortCircuitBranches(t *testing.T) {
	ctx, err := NewContext(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	pass := ValidatorFunc[int](func(ctx Context, _ int) Report { return NewReport(ctx.Limits()) })
	fail := ValidatorFunc[int](func(ctx Context, _ int) Report {
		return NewReport(ctx.Limits()).Add(NewViolation(ctx.Path(), "fail", Error, nil, nil))
	})
	if report := All[int](CollectAll, nil, pass).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("All nil = %v", report)
	}
	if report := Any[int](ShortCircuit, nil, fail, pass, fail).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("Any short = %v", report)
	}
	if report := When[int](nil, fail, nil).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("When nil = %v", report)
	}
	if report := Dependent[int](nil, nil).Validate(ctx, 1); !report.Empty() {
		t.Fatalf("Dependent nil = %v", report)
	}
}
