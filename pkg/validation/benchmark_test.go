package validation_test

import (
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
	"github.com/faustbrian/golib/pkg/validation/structplan"
)

type benchmarkUser struct {
	Name string `validate:"required,min=3,max=40"`
}

func BenchmarkValidation(b *testing.B) {
	ctx, err := validation.NewContext(validation.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	scalar := rules.Range(1, 10)
	item := validation.All(validation.CollectAll, rules.GreaterThan(0), rules.LessThan(10))
	collection := rules.Items(validation.CollectAll, item)
	values := []int{1, 2, 3, 4, 5}
	hostile := make([]int, ctx.Limits().MaxCollectionSize+1)
	hostileString := strings.Repeat("x", ctx.Limits().MaxStringLength+1)
	hostilePath := validation.RootPath().Append(
		validation.Field(strings.Repeat("x", 1<<20)),
	)
	hostilePathViolation := validation.NewViolation(hostilePath, "required",
		validation.Error, nil, nil)
	builder := structplan.New[benchmarkUser](ctx.Limits())
	if err := structplan.Add(builder, "Name",
		func(value benchmarkUser) string { return value.Name },
		rules.RuneLength(3, 40)); err != nil {
		b.Fatal(err)
	}
	typedPlan, err := builder.Compile()
	if err != nil {
		b.Fatal(err)
	}
	tagPlan, err := structplan.CompileTags[benchmarkUser](ctx.Limits())
	if err != nil {
		b.Fatal(err)
	}
	user := benchmarkUser{Name: "valid-user"}
	benchmarks := []struct {
		name string
		run  func()
	}{
		{"scalar", func() { _ = scalar.Validate(ctx, 5) }},
		{"collection", func() { _ = collection.Validate(ctx, values) }},
		{"collect_all", func() { _ = item.Validate(ctx, -1) }},
		{"hostile_bounded", func() { _ = collection.Validate(ctx, hostile) }},
		{"hostile_string_bounded", func() {
			_ = rules.Identifier().Validate(ctx, hostileString)
		}},
		{"hostile_path_bounded", func() {
			_ = validation.NewReport(ctx.Limits()).Add(hostilePathViolation)
		}},
		{"typed_plan", func() { _ = typedPlan.Validate(ctx, user) }},
		{"reflective_plan", func() { _ = tagPlan.Validate(ctx, user) }},
	}
	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				benchmark.run()
			}
		})
	}
}
