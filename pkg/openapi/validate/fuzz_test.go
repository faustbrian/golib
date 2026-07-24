package validate_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func FuzzDocumentValidationDeterminism(f *testing.F) {
	for _, seed := range []string{
		`{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}}`,
		`{"openapi":"3.1.2","paths":{"/items":{"get":{"responses":{}}}}}`,
		`{"openapi":"3.0.4","info":null,"components":{}}`,
		`{"swagger":"2.0","info":{"title":"API","version":"1"},"paths":{}}`,
	} {
		f.Add([]byte(seed))
	}
	validator := validate.NewValidator()
	f.Fuzz(func(t *testing.T, raw []byte) {
		limits := parse.DefaultLimits()
		limits.MaxBytes = 32 << 10
		limits.MaxTokens = 4_096
		limits.MaxDepth = 64
		limits.MaxObjectMembers = 1_024
		limits.MaxArrayItems = 1_024
		limits.MaxScalarBytes = 8 << 10
		limits.MaxTotalValues = 2_048
		document, err := openapi.ParseJSON(
			context.Background(), bytes.NewReader(raw), limits,
		)
		if err != nil {
			return
		}
		options := validate.DefaultOptions()
		options.MaxDiagnostics = 64
		options.MaxDocumentNodes = 2_048
		options.MaxDocumentDepth = 64
		options.MaxReferences = 1_024
		options.ReferenceLimits = reference.Limits{
			MaxTraversalDepth: 64, MaxTraversalNodes: 2_048,
			MaxReferenceDepth: 64,
		}
		first, firstErr := validator.DocumentWithOptions(
			context.Background(), document, options,
		)
		second, secondErr := validator.DocumentWithOptions(
			context.Background(), document, options,
		)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("validation errors differ: %v and %v", firstErr, secondErr)
		}
		if firstErr == nil && !reflect.DeepEqual(first.Diagnostics(), second.Diagnostics()) {
			t.Fatal("validation diagnostics are nondeterministic")
		}
	})
}
