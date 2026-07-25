//go:build publicinterop

package openapi_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	canonical "github.com/faustbrian/golib/pkg/json-schema"
	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestPinnedPublicDescriptions(t *testing.T) {
	for _, fixture := range []struct {
		name string
		path string
		yaml bool
		want map[string]int
	}{
		{
			name: "Swagger Petstore",
			path: "specification/independent/swagger-petstore/openapi.yaml",
			yaml: true,
			want: map[string]int{
				"warning:openapi.xml.array-name.missing": 2,
				"warning:openapi.xml.non-property":       7,
			},
		},
		{
			name: "GitHub REST API",
			path: "specification/independent/github-rest-api/api.github.com.2022-11-28.json",
			want: map[string]int{
				"error:openapi.path.template.ambiguous":            2,
				"error:openapi.schema.discriminator.not-required":  5,
				"warning:openapi.example.schema":                   595,
				"warning:openapi.header.content-type.ignored":      1,
				"warning:openapi.operation-id.nonportable":         1468,
				"warning:openapi.operation.deprecated":             37,
				"warning:openapi.operation.request-body.undefined": 21,
				"warning:openapi.parameter.deprecated":             2,
				"warning:openapi.responses.success.missing":        8,
			},
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			file, err := os.Open(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			var document openapi.Document
			if fixture.yaml {
				document, err = openapi.ParseYAML(ctx, file, parse.DefaultLimits())
			} else {
				document, err = openapi.ParseJSON(ctx, file, parse.DefaultLimits())
			}
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				t.Fatal(err)
			}
			documentLimits := canonical.DefaultLimits()
			documentLimits.MaxEvaluationOps = 20_000_000
			validator, err := validate.NewValidatorWithDocumentSchemaLimits(documentLimits)
			if err != nil {
				t.Fatal(err)
			}
			validationOptions := validate.DefaultOptions()
			validationOptions.MaxReferences = 1_000_000
			validationOptions.ReferenceLimits.MaxTraversalNodes = 1_000_000
			report, err := validator.DocumentWithOptions(ctx, document, validationOptions)
			if err != nil {
				t.Fatal(err)
			}
			counts := make(map[string]int)
			for _, diagnostic := range report.Diagnostics() {
				counts[string(diagnostic.Severity)+":"+diagnostic.Code]++
			}
			if len(counts) != len(fixture.want) {
				t.Fatalf("diagnostic classes = %#v", counts)
			}
			for key, want := range fixture.want {
				if counts[key] != want {
					t.Errorf("%s = %d, want %d", key, counts[key], want)
				}
			}

			var output bytes.Buffer
			options := serialize.DefaultOptions()
			if err := serialize.JSON(ctx, &output, document, options); err != nil {
				t.Fatal(err)
			}
			roundTrip, err := openapi.ParseJSON(ctx, bytes.NewReader(output.Bytes()), parse.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			if roundTrip.SpecificationVersion() != document.SpecificationVersion() {
				t.Fatalf("round-trip version = %s", roundTrip.SpecificationVersion())
			}
		})
	}
}
