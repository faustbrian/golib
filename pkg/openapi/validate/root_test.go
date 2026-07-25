package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesRootURIAndSurfaceConstraints(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"$self":"https://[::1",
		"jsonSchemaDialect":"relative/dialect",
		"info":{"title":"API","version":"1"}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.root.surface.missing":             false,
		"openapi.root.self.invalid":                false,
		"openapi.root.schema-dialect.non-absolute": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("missing root diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

func TestOpenAPI31RequiresAnAPISurface(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.0", "3.1.1", "3.1.2"} {
		document := mustDocument(t, `{
			"openapi":"`+version+`",
			"info":{"title":"API","version":"1"}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.root.surface.missing" {
				found = true
			}
		}
		if !found {
			t.Errorf("version %s accepted a root without an API surface", version)
		}
	}
}

func TestDocumentAcceptsRelativeSelfAndAbsoluteSchemaDialect(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"$self":"descriptions/pets.yaml",
		"jsonSchemaDialect":"https://schemas.example.test/dialect",
		"info":{"title":"API","version":"1"},
		"components":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.root.surface.missing" ||
			diagnostic.Code == "openapi.root.self.invalid" ||
			diagnostic.Code == "openapi.root.schema-dialect.non-absolute" {
			t.Fatalf("valid root metadata rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentAcceptsEmptySelfURIReference(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","$self":"",
		"info":{"title":"API","version":"1"},"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.root.self.invalid" {
			t.Fatalf("empty URI reference rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentValidatesOpenAPI31SchemaDialectURI(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"jsonSchemaDialect":"relative",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.root.schema-dialect.non-absolute" {
			return
		}
	}
	t.Fatalf("missing schema dialect diagnostic: %#v", report.Diagnostics())
}

func TestDocumentRecommendsTheStandardEntryDocumentName(t *testing.T) {
	t.Parallel()

	versions := []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	}
	for _, version := range versions {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{}
			}`)
			options := validate.DefaultOptions()
			options.ReferenceResourceURI = "https://api.example.test/service.json"
			report, err := validate.DocumentWithOptions(
				context.Background(), document, options,
			)
			if err != nil {
				t.Fatal(err)
			}

			var found []validate.Diagnostic
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.document.entry-name.nonstandard" {
					found = append(found, diagnostic)
				}
			}
			if len(found) != 1 || found[0].InstanceLocation != "" ||
				found[0].Severity != validate.SeverityWarning ||
				found[0].SpecificationSection != "openapi-description-structure" {
				t.Fatalf("entry-document name diagnostics = %#v", found)
			}
		})
	}
}

func TestDocumentAcceptsStandardEntryDocumentNames(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	for _, resourceURI := range []string{
		"https://api.example.test/openapi.json",
		"https://api.example.test/descriptions/openapi.yaml?revision=1",
	} {
		options := validate.DefaultOptions()
		options.ReferenceResourceURI = resourceURI
		report, err := validate.DocumentWithOptions(
			context.Background(), document, options,
		)
		if err != nil {
			t.Fatal(err)
		}
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code == "openapi.document.entry-name.nonstandard" {
				t.Errorf("standard entry name rejected: %#v", diagnostic)
			}
		}
	}
}

func TestSwaggerDoesNotApplyTheOpenAPIEntryNameRecommendation(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/swagger.json"
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.document.entry-name.nonstandard" {
			t.Fatalf("Swagger received an OpenAPI naming recommendation: %#v", diagnostic)
		}
	}
}
