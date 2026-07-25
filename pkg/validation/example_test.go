package validation_test

import (
	"context"
	"errors"
	"fmt"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
	"github.com/faustbrian/golib/pkg/validation/structplan"
	"github.com/faustbrian/golib/pkg/validation/validationjsonapi"
)

func ExampleValidator() {
	ctx, _ := validation.NewContext(validation.DefaultLimits())
	validator := validation.All(validation.CollectAll,
		rules.RuneLength(3, 20), rules.Prefix("usr_"))
	report := validator.Validate(ctx.WithPath(validation.Field("username")), "x")
	for _, violation := range report.Violations() {
		fmt.Println(violation.Path(), violation.Code())
	}
	// Output:
	// username rune_length
	// username prefix
}

func ExampleValue() {
	ctx, _ := validation.NewContext(validation.DefaultLimits())
	report := rules.Required[string]().Validate(ctx, validation.Missing[string]())
	fmt.Println(report.HasCode("required"))
	// Output: true
}

func ExampleBuilder() {
	type User struct{ Name string }
	builder := structplan.New[User](validation.DefaultLimits())
	_ = structplan.Add(builder, "name", func(user User) string { return user.Name },
		rules.RuneLength(2, 20))
	plan, _ := builder.Compile()
	ctx, _ := validation.NewContext(validation.DefaultLimits())
	report := plan.Validate(ctx, User{Name: "x"})
	fmt.Println(report.Violations()[0].Path())
	// Output: name
}

func ExampleAsyncAll() {
	ctx, _ := validation.NewContext(validation.DefaultLimits())
	check := validation.AsyncValidatorFunc[string](func(
		_ context.Context, ctx validation.Context, _ string,
	) validation.Report {
		return validation.NewReport(ctx.Limits())
	})
	report := validation.AsyncAll(context.Background(), ctx, "user", check)
	fmt.Println(report.Empty())
	// Output: true
}

func ExampleInvalidError() {
	ctx, _ := validation.NewContext(validation.DefaultLimits())
	err := rules.Email().Validate(ctx, "invalid").Err()
	fmt.Println(errors.Is(err, validation.ErrInvalid))
	// Output: true
}

func ExampleErrors() {
	report := validation.NewReport(validation.DefaultLimits()).Add(
		validation.NewViolation(validation.RootPath().Append(validation.Field("name")),
			"required", validation.Error, nil, nil))
	document := validationjsonapi.Errors(report)
	fmt.Println(document.Errors[0].Source.Pointer, document.Errors[0].Code)
	// Output: /name required
}
