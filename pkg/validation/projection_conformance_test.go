package validation_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/validationhttp"
	"github.com/faustbrian/golib/pkg/validation/validationjsonapi"
	"github.com/faustbrian/golib/pkg/validation/validationrpc"
)

func TestTransportProjectionsPreserveConformanceAndEscapeLocations(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxViolations = 2
	unsafeLocation := "<token>/~\n"
	report := validation.NewReport(limits).
		Add(validation.NewViolation(
			validation.RootPath().Append(validation.Field(unsafeLocation)),
			"deprecated", validation.Warning,
			map[string]string{"minimum": "1"}, nil)).
		Add(validation.NewViolation(
			validation.RootPath().Append(validation.Field("items")).
				Append(validation.Index(2)),
			"required", validation.Error, nil, nil)).
		Add(validation.NewViolation(validation.RootPath(), "omitted",
			validation.Error, nil, nil))

	rpc := validationrpc.InvalidParams(report)
	if rpc.Code != -32602 || !rpc.Data.Truncated || !rpc.Data.HasErrors ||
		len(rpc.Data.Violations) != 2 ||
		rpc.Data.Violations[0].Path != unsafeLocation ||
		rpc.Data.Violations[0].Code != "deprecated" ||
		rpc.Data.Violations[0].Severity != "warning" ||
		rpc.Data.Violations[0].Parameters["minimum"] != "1" ||
		rpc.Data.Violations[1].Path != "items[2]" ||
		rpc.Data.Violations[1].Code != "required" ||
		rpc.Data.Violations[1].Severity != "error" {
		t.Fatalf("JSON-RPC projection = %#v", rpc)
	}

	jsonAPI := validationjsonapi.Errors(report)
	if !jsonAPI.Meta.Truncated || !jsonAPI.Meta.HasErrors ||
		len(jsonAPI.Errors) != 2 ||
		jsonAPI.Errors[0].Source.Pointer != "/<token>~1~0\n" ||
		jsonAPI.Errors[0].Code != "deprecated" ||
		jsonAPI.Errors[0].Status != "200" ||
		jsonAPI.Errors[0].Meta.Severity != "warning" ||
		jsonAPI.Errors[0].Meta.Parameters["minimum"] != "1" ||
		jsonAPI.Errors[1].Source.Pointer != "/items/2" ||
		jsonAPI.Errors[1].Code != "required" ||
		jsonAPI.Errors[1].Status != "422" ||
		jsonAPI.Errors[1].Meta.Severity != "error" {
		t.Fatalf("JSON:API projection = %#v", jsonAPI)
	}

	problem := validationhttp.FromReport(report)
	if problem.Status != http.StatusUnprocessableEntity || !problem.Truncated ||
		len(problem.Errors) != 2 || problem.Errors[0].Path != unsafeLocation ||
		problem.Errors[0].Code != "deprecated" ||
		problem.Errors[0].Severity != "warning" ||
		problem.Errors[0].Parameters["minimum"] != "1" ||
		problem.Errors[1].Path != "items[2]" ||
		problem.Errors[1].Code != "required" ||
		problem.Errors[1].Severity != "error" {
		t.Fatalf("HTTP projection = %#v", problem)
	}

	for name, projected := range map[string]any{
		"rpc": rpc, "jsonapi": jsonAPI, "http": problem,
	} {
		encoded, err := json.Marshal(projected)
		if err != nil {
			t.Fatalf("%s marshal error = %v", name, err)
		}
		if strings.Contains(string(encoded), "<token>") ||
			strings.Contains(string(encoded), "\n") {
			t.Fatalf("%s did not JSON-escape hostile location: %q", name, encoded)
		}
	}
}
