package validate_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentReportsStableStructuralDiagnostics(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{"openapi":"3.2.0"}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid() {
		t.Fatal("incomplete document was reported as valid")
	}
	diagnostics := report.Diagnostics()
	if len(diagnostics) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %#v", len(diagnostics), diagnostics)
	}
	wantCodes := map[string]bool{
		"openapi.document.required":    false,
		"openapi.root.surface.missing": false,
	}
	for _, diagnostic := range diagnostics {
		if _, exists := wantCodes[diagnostic.Code]; !exists {
			t.Fatalf("unexpected code %q", diagnostic.Code)
		}
		wantCodes[diagnostic.Code] = true
		if diagnostic.Source != validate.SourceDocument {
			t.Fatalf("unexpected source %q", diagnostic.Source)
		}
	}
	for code, seen := range wantCodes {
		if !seen {
			t.Fatalf("missing diagnostic %q", code)
		}
	}
	diagnostics[0].Message = "mutated"
	if report.Diagnostics()[0].Message == "mutated" {
		t.Fatal("report exposed mutable diagnostic storage")
	}
}

func TestDocumentAcceptsMinimalDescriptionsByVersion(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"openapi":"3.0.4","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{}}`,
	} {
		document := mustDocument(t, raw)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Valid() {
			t.Fatalf("minimal document was rejected: %#v", report.Diagnostics())
		}
	}
}

func TestDocumentRejectsNilContextAndDocument(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2",
		"info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := validate.Document(nil, document); err == nil {
		t.Fatal("nil context was accepted")
	}
	if _, err := validate.Document(context.Background(), nil); err == nil {
		t.Fatal("nil document was accepted")
	}
}

func TestDocumentValidationBoundsConstructedDocuments(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"x-nested":{"one":{"two":{"three":true}}}
	}`)
	for name, mutate := range map[string]func(*validate.Options){
		"nodes": func(options *validate.Options) {
			options.MaxDocumentNodes = 4
		},
		"depth": func(options *validate.Options) {
			options.MaxDocumentDepth = 3
		},
	} {
		t.Run(name, func(t *testing.T) {
			options := validate.DefaultOptions()
			mutate(&options)
			if _, err := validate.DocumentWithOptions(
				context.Background(), document, options,
			); !errors.Is(err, validate.ErrLimitExceeded) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestDocumentValidationAcceptsExactDocumentBounds(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	options := validate.DefaultOptions()
	options.MaxDocumentNodes = 6
	options.MaxDocumentDepth = 3
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("document was rejected: %#v", report.Diagnostics())
	}
}

func TestDocumentValidationRejectsEveryInvalidOptionBoundary(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	tests := map[string]func(*validate.Options){
		"document nodes": func(options *validate.Options) {
			options.MaxDocumentNodes = -1
		},
		"document depth": func(options *validate.Options) {
			options.MaxDocumentDepth = -1
		},
		"references": func(options *validate.Options) {
			options.MaxReferences = -1
		},
		"external example bytes": func(options *validate.Options) {
			options.MaxExternalExampleBytes = -1
		},
		"reference traversal depth": func(options *validate.Options) {
			options.ReferenceLimits.MaxTraversalDepth = -1
		},
		"reference traversal nodes": func(options *validate.Options) {
			options.ReferenceLimits.MaxTraversalNodes = -1
		},
		"reference depth": func(options *validate.Options) {
			options.ReferenceLimits.MaxReferenceDepth = -1
		},
		"reference resource fragment": func(options *validate.Options) {
			options.ReferenceResourceURI = "https://api.example.test/openapi.json#fragment"
		},
	}
	for name, invalidate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			options := validate.DefaultOptions()
			invalidate(&options)
			_, err := validate.DocumentWithOptions(
				context.Background(), document, options,
			)
			if err == nil || err.Error() != "validate OpenAPI document: invalid options" {
				t.Fatalf("invalid options error = %v", err)
			}
		})
	}
}

func TestValidatorOwnsReusableSchemaState(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{}
	}`)
	validator := validate.NewValidator()
	for range 2 {
		report, err := validator.Document(context.Background(), document)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Valid() {
			t.Fatalf("document was rejected: %#v", report.Diagnostics())
		}
	}
	var nilValidator *validate.Validator
	if _, err := nilValidator.Document(context.Background(), document); err == nil {
		t.Fatal("nil validator was accepted")
	}
	var zero validate.Validator
	if _, err := zero.Document(context.Background(), document); err != nil {
		t.Fatalf("zero-value validator failed: %v", err)
	}
}

func mustDocument(t *testing.T, raw string) openapi.Document {
	t.Helper()
	document, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(raw),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
