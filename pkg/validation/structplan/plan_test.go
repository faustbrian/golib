package structplan_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
	"github.com/faustbrian/golib/pkg/validation/structplan"
)

type account struct {
	Name  string
	Email string
}

type panickingValidator struct{}

func (panickingValidator) Validate(validation.Context, string) validation.Report {
	panic("password=secret")
}

func TestTypedPlanUsesExplicitAccessorsAndIsImmutable(t *testing.T) {
	limits := validation.DefaultLimits()
	builder := structplan.New[account](limits)
	if err := structplan.Add(builder, "name", func(value account) string { return value.Name }, rules.RuneLength(2, 20)); err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if err := structplan.Add(builder, "email", func(value account) string { return value.Email }, rules.Email()); err != nil {
		t.Fatal(err)
	}

	report := plan.Validate(contextFor(t), account{Name: "x", Email: "bad"})
	if report.Len() != 1 || report.Violations()[0].Path().String() != "name" {
		t.Fatalf("immutable plan report = %#v", report.Violations())
	}
	updated, err := builder.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if report := updated.Validate(contextFor(t), account{Name: "x", Email: "bad"}); report.Len() != 2 {
		t.Fatalf("updated plan = %#v", report.Violations())
	}
}

func TestTypedPlanRejectsDuplicatesAndFieldLimits(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxStructFields = 1
	builder := structplan.New[account](limits)
	if err := structplan.Add(builder, "name", func(value account) string { return value.Name }, rules.Prefix("a")); err != nil {
		t.Fatal(err)
	}
	if err := structplan.Add(builder, "name", func(value account) string { return value.Name }, rules.Prefix("b")); !errors.Is(err, structplan.ErrDuplicateField) {
		t.Fatalf("duplicate error = %v", err)
	}
	if err := structplan.Add(builder, "email", func(value account) string { return value.Email }, rules.Email()); !errors.Is(err, validation.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestTypedPlanRejectsMalformedFieldsAndContainsExtensionPanics(t *testing.T) {
	limits := validation.DefaultLimits()
	if err := structplan.Add[account, string](nil, "name",
		func(value account) string { return value.Name }, rules.Prefix("a")); !errors.Is(err, structplan.ErrInvalidPlan) {
		t.Fatalf("nil builder error = %v", err)
	}
	builder := structplan.New[account](limits)
	if err := structplan.Add(builder, "name", nil, rules.Prefix("a")); !errors.Is(err, structplan.ErrInvalidPlan) {
		t.Fatalf("nil accessor error = %v", err)
	}
	if err := structplan.Add[account, string](builder, "name",
		func(value account) string { return value.Name }, nil); !errors.Is(err, structplan.ErrInvalidPlan) {
		t.Fatalf("nil validator error = %v", err)
	}
	if err := structplan.Add(builder, "", func(value account) string {
		return value.Name
	}, rules.Prefix("a")); !errors.Is(err, structplan.ErrInvalidPlan) {
		t.Fatalf("empty field error = %v", err)
	}
	if err := structplan.Add(builder, strings.Repeat("x", limits.MaxPathLength+1),
		func(value account) string { return value.Name }, rules.Prefix("a")); !errors.Is(err, validation.ErrLimitExceeded) {
		t.Fatalf("field path error = %v", err)
	}

	if err := structplan.Add(builder, "password", func(account) string {
		panic("password=secret")
	}, rules.Prefix("a")); err != nil {
		t.Fatal(err)
	}
	if err := structplan.Add(builder, "token", func(value account) string {
		return value.Name
	}, panickingValidator{}); err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Compile()
	if err != nil {
		t.Fatal(err)
	}
	report := plan.Validate(contextFor(t), account{Name: "secret"})
	if report.Len() != 2 || !report.HasCode("validator_panic") ||
		strings.Contains(report.String(), "secret") {
		t.Fatalf("panic report = %#v", report.Violations())
	}
	violations := report.Violations()
	if violations[0].Path().String() != "password" ||
		violations[1].Path().String() != "token" {
		t.Fatalf("panic paths = %#v", violations)
	}
	if _, err := structplan.CompileCached[tagged](nil); !errors.Is(err, structplan.ErrInvalidPlan) {
		t.Fatalf("nil cache error = %v", err)
	}
}

type tagged struct {
	Name  string `validate:"required,min=2,max=10"`
	Email string `validate:"required,email"`
}

func TestTagPlanCompilesStrictGrammarAndValidates(t *testing.T) {
	plan, err := structplan.CompileTags[tagged](validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	report := plan.Validate(contextFor(t), tagged{Name: "x", Email: "bad"})
	violations := report.Violations()
	if len(violations) != 2 || violations[0].Path().String() != "Name" ||
		violations[1].Path().String() != "Email" {
		t.Fatalf("tag report = %#v", violations)
	}
}

func TestTagPlanRejectsUnknownDuplicateMalformedAndCycles(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"unknown", func() error {
			type value struct {
				X string `validate:"wat"`
			}
			_, err := structplan.CompileTags[value](validation.DefaultLimits())
			return err
		}, structplan.ErrUnknownRule},
		{"duplicate", func() error {
			type value struct {
				X string `validate:"required,required"`
			}
			_, err := structplan.CompileTags[value](validation.DefaultLimits())
			return err
		}, structplan.ErrDuplicateRule},
		{"malformed", func() error {
			type value struct {
				X string `validate:"min=no"`
			}
			_, err := structplan.CompileTags[value](validation.DefaultLimits())
			return err
		}, structplan.ErrInvalidTag},
		{"cycle", func() error {
			type node struct{ Next *node }
			_, err := structplan.CompileTags[node](validation.DefaultLimits())
			return err
		}, structplan.ErrCycle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestTagPlanCacheIsBoundedAndConcurrent(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxCompiledPlans = 1
	cache := structplan.NewCache(limits)
	var wait sync.WaitGroup
	plans := make(chan *structplan.TagPlan[tagged], 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			plan, err := structplan.CompileCached[tagged](cache)
			if err != nil {
				t.Errorf("CompileCached() error = %v", err)
				return
			}
			plans <- plan
		}()
	}
	wait.Wait()
	close(plans)
	var first *structplan.TagPlan[tagged]
	for plan := range plans {
		if first == nil {
			first = plan
		}
		if plan != first {
			t.Fatal("cache returned distinct immutable plans")
		}
	}
	if cache.Len() != 1 {
		t.Fatalf("cache length = %d", cache.Len())
	}
	cache.Clear()
	if cache.Len() != 0 {
		t.Fatalf("cache length after clear = %d", cache.Len())
	}
}

func TestTagPlanCacheClearIsRaceSafeAndDoesNotInvalidatePlans(t *testing.T) {
	cache := structplan.NewCache(validation.DefaultLimits())
	retained, err := structplan.CompileCached[tagged](cache)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextFor(t)
	var wait sync.WaitGroup
	for worker := range 16 {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := range 100 {
				if (worker+iteration)%3 == 0 {
					cache.Clear()
					continue
				}
				plan, compileErr := structplan.CompileCached[tagged](cache)
				if compileErr != nil {
					t.Errorf("CompileCached() error = %v", compileErr)
					return
				}
				_ = plan.Validate(ctx, tagged{Name: "valid", Email: "a@example.com"})
				_ = cache.Len()
			}
		}()
	}
	wait.Wait()
	cache.Clear()
	if report := retained.Validate(ctx, tagged{
		Name: "valid", Email: "a@example.com",
	}); !report.Empty() {
		t.Fatalf("retained plan after clear = %#v", report.Violations())
	}
}

func contextFor(t *testing.T) validation.Context {
	t.Helper()
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}
