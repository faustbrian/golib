package validation_test

import (
	"fmt"
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
)

func FuzzPathAndReportSafety(f *testing.F) {
	f.Add("field", "code", "secret")
	f.Add("a/b~c", "required", "password-token")
	f.Fuzz(func(t *testing.T, field, code, rejected string) {
		secret := "[[REJECTED:" + rejected + "]]"
		if code == "" || strings.Contains(code, secret) {
			code = "invalid"
		}
		path := validation.RootPath().Append(validation.Field(field))
		_ = path.JSONPointer()
		flat := validation.RootPath().Append(validation.Field("prefix." + field))
		nested := validation.RootPath().Append(validation.Field("prefix")).
			Append(validation.Field(field))
		limits := validation.DefaultLimits()
		structural := validation.NewReport(limits).
			Add(validation.NewViolation(flat,
				"path", validation.Error, nil, nil)).
			Add(validation.NewViolation(nested,
				"path", validation.Error, nil, nil))
		if len(flat.String()) <= limits.MaxPathLength &&
			len(nested.String()) <= limits.MaxPathLength && structural.Len() != 2 {
			t.Fatal("distinct typed paths were deduplicated")
		}
		if (len(flat.String()) > limits.MaxPathLength ||
			len(nested.String()) > limits.MaxPathLength) &&
			!structural.HasCode("path_limit") {
			t.Fatal("oversized path did not produce path_limit")
		}
		violation := validation.NewViolation(
			validation.RootPath().Append(validation.Field("field")),
			code, validation.Error, nil, nil,
		)
		report := validation.NewReport(validation.DefaultLimits()).Add(violation)
		if strings.Contains(fmt.Sprint(violation), secret) ||
			strings.Contains(fmt.Sprint(report), secret) {
			t.Fatal("rejected value leaked through default formatting")
		}
	})
}
