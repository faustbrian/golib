package validationrpc_test

import (
	"encoding/json"
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/validationrpc"
)

func TestInvalidParamsPreservesSafeStableData(t *testing.T) {
	report := sampleReport(t)
	projected := validationrpc.InvalidParams(report)
	if projected.Code != -32602 || projected.Message != "Invalid params" ||
		len(projected.Data.Violations) != 2 {
		t.Fatalf("projection = %#v", projected)
	}
	if got := projected.Data.Violations[0].Path; got != "items[0].name" {
		t.Fatalf("path = %q", got)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "secret-value") || strings.Contains(text, "safe cause") {
		t.Fatalf("projection leaked diagnostic values: %s", text)
	}
}

func TestInvalidParamsProjectsWarningsAndTruncation(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxViolations = 1
	report := validation.NewReport(limits).
		Add(validation.NewViolation(validation.RootPath(), "warning", validation.Warning, nil, nil)).
		Add(validation.NewViolation(validation.RootPath(), "ignored", validation.Error, nil, nil))
	projected := validationrpc.InvalidParams(report)
	if !projected.Data.Truncated || !projected.Data.HasErrors ||
		projected.Data.Violations[0].Severity != "warning" {
		t.Fatalf("projection = %#v", projected)
	}
}

func sampleReport(t *testing.T) validation.Report {
	t.Helper()
	limits := validation.DefaultLimits()
	first := validation.NewViolation(validation.RootPath().
		Append(validation.Field("items")).Append(validation.Index(0)).
		Append(validation.Field("name")), "required", validation.Error,
		map[string]string{"minimum": "1"}, errText("safe cause"))
	second := validation.NewViolation(validation.RootPath().Append(validation.Field("token")),
		"format", validation.Error, nil, nil)
	return validation.NewReport(limits).Add(first).Add(second)
}

type errText string

func (err errText) Error() string { return string(err) }
