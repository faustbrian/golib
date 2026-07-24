package validation_test

import (
	"reflect"
	"slices"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
	"github.com/faustbrian/golib/pkg/validation/structplan"
)

type callerOwnedValue struct {
	Name    string         `validate:"required,max=16"`
	Numbers []int          `validate:"max=4"`
	Mapping map[string]int `validate:"max=4"`
	Pointer *int           `validate:"required"`
}

func TestCollectionRulesDoNotMutateAndRejectBeforeHostileTraversal(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxCollectionSize = 2
	ctx, err := validation.NewContext(limits)
	if err != nil {
		t.Fatal(err)
	}
	values := []int{3, 2, 1}
	original := slices.Clone(values)
	calls := 0
	validator := validation.ValidatorFunc[int](func(ctx validation.Context, _ int) validation.Report {
		calls++
		return validation.NewReport(ctx.Limits())
	})
	report := rules.Items(validation.CollectAll, validator).Validate(ctx, values)
	if !report.HasCode("collection_limit") || calls != 0 {
		t.Fatalf("report=%v calls=%d", report, calls)
	}
	if !slices.Equal(values, original) {
		t.Fatalf("caller slice mutated: %v", values)
	}
}

func TestScalarAllocationBudget(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	validator := rules.Range(1, 10)
	allocations := testing.AllocsPerRun(1000, func() {
		if !validator.Validate(ctx, 5).Empty() {
			panic("unexpected invalid value")
		}
	})
	if allocations > 2 {
		t.Fatalf("scalar allocations = %.2f, budget = 2", allocations)
	}
}

func TestTypedReflectiveAndCollectionValidationNeverMutatesCallerData(t *testing.T) {
	number := 3
	value := callerOwnedValue{Name: "valid", Numbers: []int{3, 2, 1},
		Mapping: map[string]int{"b": 2, "a": 1}, Pointer: &number}
	originalNumber := number
	original := callerOwnedValue{Name: value.Name, Numbers: slices.Clone(value.Numbers),
		Mapping: map[string]int{"b": 2, "a": 1}, Pointer: &originalNumber}
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	item := rules.Range(0, 10)
	_ = rules.Items(validation.CollectAll, item).Validate(ctx, value.Numbers)
	_ = rules.Keys[string, int](validation.CollectAll, rules.Identifier()).
		Validate(ctx, value.Mapping)
	_ = rules.Values[string, int](validation.CollectAll, item).
		Validate(ctx, value.Mapping)
	builder := structplan.New[callerOwnedValue](ctx.Limits())
	if err := structplan.Add(builder, "name",
		func(value callerOwnedValue) string { return value.Name },
		rules.RuneLength(1, 16)); err != nil {
		t.Fatal(err)
	}
	plan, err := builder.Compile()
	if err != nil {
		t.Fatal(err)
	}
	_ = plan.Validate(ctx, value)
	tagPlan, err := structplan.CompileTags[callerOwnedValue](ctx.Limits())
	if err != nil {
		t.Fatal(err)
	}
	_ = tagPlan.Validate(ctx, value)
	_ = validation.Present(value).IsEmpty()
	_ = validation.Present(value).IsZero()
	if !reflect.DeepEqual(value, original) {
		t.Fatalf("caller data mutated: got=%#v want=%#v", value, original)
	}
}
