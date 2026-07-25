package apiqueryvalidation_test

import (
	"context"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
	"github.com/faustbrian/golib/pkg/api-query/apiqueryvalidation"
	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestReportPreservesStructuredSafeQueryViolations(t *testing.T) {
	t.Parallel()

	schema, err := apiquery.NewSchema(apiquery.SchemaConfig{
		Resource: "orders", Revision: "v1",
		Fields: []apiquery.FieldDefinition{{Name: "id", Type: apiquery.TypeString}},
	})
	if err != nil {
		t.Fatalf("NewSchema() error = %v", err)
	}
	_, compileErr := apiquery.Compile(context.Background(), schema, apiquery.Request{
		Fields: apiquery.Present([]string{"unknown", "id", "id"}),
	}, apiquery.CompileOptions{})
	report := apiqueryvalidation.Report(compileErr, validation.DefaultLimits())
	if report.Len() != 2 || !report.HasCode("invalid_element") || !report.HasCode("conflict") {
		t.Fatalf("Report() = %#v", report.Violations())
	}
	violations := report.Violations()
	if violations[0].Path().String() != "fields[0]" || violations[0].Cause() != nil ||
		len(violations[0].Parameters()) != 0 {
		t.Fatalf("first violation = %#v, want safe structured path", violations[0])
	}
	if violations[1].Path().String() != "fields[2]" {
		t.Fatalf("second path = %q", violations[1].Path().String())
	}
}
