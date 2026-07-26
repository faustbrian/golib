package reference

import (
	"context"
	"errors"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestDereferenceReferenceObjectRegistryCoversEveryLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dialect   specversion.Dialect
		tokens    []string
		reference bool
		registry  string
	}{
		{name: "root", dialect: specversion.DialectOAS32},
		{name: "swagger component parameter", dialect: specversion.DialectSwagger20,
			tokens: []string{"parameters", "Shared"}, reference: true, registry: "parameters"},
		{name: "swagger operation parameter", dialect: specversion.DialectSwagger20,
			tokens: []string{"paths", "/pets", "get", "parameters", "0"}, reference: true, registry: "parameters"},
		{name: "swagger response", dialect: specversion.DialectSwagger20,
			tokens: []string{"paths", "/pets", "get", "responses", "200"}, reference: true, registry: "responses"},
		{name: "swagger ordinary object", dialect: specversion.DialectSwagger20,
			tokens: []string{"paths", "/pets", "get"}},
		{name: "oas component example", dialect: specversion.DialectOAS32,
			tokens: []string{"components", "examples", "Shared"}, reference: true, registry: "examples"},
		{name: "oas schema keyword", dialect: specversion.DialectOAS32,
			tokens: []string{"components", "schemas", "Pet"}, registry: "schemas"},
		{name: "oas path", dialect: specversion.DialectOAS32,
			tokens: []string{"paths", "/pets"}, reference: true, registry: "pathItems"},
		{name: "oas operation parameter", dialect: specversion.DialectOAS32,
			tokens: []string{"paths", "/pets", "get", "parameters", "0"}, reference: true, registry: "parameters"},
		{name: "oas response", dialect: specversion.DialectOAS32,
			tokens: []string{"paths", "/pets", "get", "responses", "200"}, reference: true, registry: "responses"},
		{name: "oas request body", dialect: specversion.DialectOAS32,
			tokens: []string{"paths", "/pets", "post", "requestBody"}, reference: true, registry: "requestBodies"},
		{name: "oas32 media type", dialect: specversion.DialectOAS32,
			tokens: []string{"paths", "/pets", "post", "requestBody", "content", "application/json"}, reference: true, registry: "mediaTypes"},
		{name: "oas31 media type", dialect: specversion.DialectOAS31,
			tokens: []string{"paths", "/pets", "post", "requestBody", "content", "application/json"}},
		{name: "oas header", dialect: specversion.DialectOAS32,
			tokens: []string{"paths", "/pets", "get", "responses", "200", "headers", "Trace"}, reference: true, registry: "headers"},
		{name: "oas ordinary object", dialect: specversion.DialectOAS32,
			tokens: []string{"paths", "/pets", "get"}},
		{name: "oas non-component registry name", dialect: specversion.DialectOAS32,
			tokens: []string{"ordinary", "securitySchemes", "value"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dereferencer := objectDereferencer{dialect: test.dialect}
			if got := dereferencer.isReferenceObject(test.tokens); got != test.reference {
				t.Fatalf("isReferenceObject() = %t, want %t", got, test.reference)
			}
			if got := dereferencer.referenceObjectRegistry(test.tokens); got != test.registry {
				t.Fatalf("referenceObjectRegistry() = %q, want %q", got, test.registry)
			}
		})
	}
}

func TestDereferenceInternalLimitsAndOverlayFamilies(t *testing.T) {
	t.Parallel()

	options := DefaultDereferenceOptions()
	minimum := options
	minimum.ReferenceLimits = Limits{
		MaxTraversalDepth: 1, MaxTraversalNodes: 1, MaxReferenceDepth: 1,
	}
	minimum.MaxReferences = 1
	minimum.MaxNodes = 1
	minimum.MaxDepth = 1
	if err := minimum.validate(); err != nil {
		t.Fatalf("minimum options error = %v", err)
	}
	for _, mutate := range []func(*DereferenceOptions){
		func(options *DereferenceOptions) { options.MaxReferences = 0 },
		func(options *DereferenceOptions) { options.MaxNodes = 0 },
		func(options *DereferenceOptions) { options.MaxDepth = 0 },
	} {
		invalid := options
		mutate(&invalid)
		if err := invalid.validate(); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("invalid options error = %v", err)
		}
	}

	array, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Boolean(true)})
	dereferencer := objectDereferencer{
		ctx: context.Background(),
		options: DereferenceOptions{
			MaxDepth: 1, MaxNodes: 10, MaxReferences: 10,
			ReferenceLimits: DefaultLimits(),
		},
	}
	if _, err := dereferencer.rewrite(
		Resource{}, array, nil, "", 1, map[string]bool{},
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("array child limit error = %v", err)
	}
	if _, err := dereferencer.rewrite(
		Resource{}, jsonvalue.Null(), nil, "", 2, map[string]bool{},
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("scalar depth error = %v", err)
	}
	nodeLimit := dereferencer
	nodeLimit.options.MaxDepth = 2
	nodeLimit.options.MaxNodes = 1
	nodeLimit.nodes = 1
	if _, err := nodeLimit.rewrite(
		Resource{}, jsonvalue.Null(), nil, "", 1, map[string]bool{},
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("scalar node error = %v", err)
	}
	canceled := dereferencer
	canceled.ctx = &cancelAfterFirstReferenceCheck{Context: context.Background()}
	canceled.options.MaxDepth = 2
	canceled.nodes = 0
	if _, err := canceled.rewrite(
		Resource{}, array, nil, "", 1, map[string]bool{},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("array child cancellation = %v", err)
	}
	exact := dereferencer
	exact.nodes = 0
	exact.options.MaxNodes = 1
	if got, err := exact.rewrite(
		Resource{}, jsonvalue.Null(), nil, "", 1, map[string]bool{},
	); err != nil || got.Kind() != jsonvalue.NullKind {
		t.Fatalf("exact rewrite limits = %#v, %v", got, err)
	}

	stringValue, _ := jsonvalue.String("description")
	target, _ := jsonvalue.Object(nil)
	referenceObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "$ref", Value: stringValue},
		{Name: "description", Value: stringValue},
	})
	dereferencer.dialect = specversion.DialectOAS31
	result, err := dereferencer.applyReferenceOverlays(
		target, referenceObject, "parameters", "/parameter",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result.Lookup("description"); !exists {
		t.Fatal("parameter description overlay was discarded")
	}

	pathItem, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "$ref", Value: stringValue},
		{Name: "get", Value: target},
	})
	if !dereferencer.pathItemReferenceHasMeaningfulSiblings(
		[]string{"components", "pathItems", "Shared"}, pathItem,
	) {
		t.Fatal("Path Item operation sibling was not treated as meaningful")
	}
}

func TestDereferenceTraversalAdvancesArrayAndObjectDepth(t *testing.T) {
	t.Parallel()

	leafArray, _ := jsonvalue.Array([]jsonvalue.Value{jsonvalue.Null()})
	nestedArray, _ := jsonvalue.Array([]jsonvalue.Value{leafArray})
	nestedObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "nested", Value: leafArray},
	})
	for _, value := range []jsonvalue.Value{nestedArray, nestedObject} {
		dereferencer := objectDereferencer{
			ctx: context.Background(),
			options: DereferenceOptions{
				MaxDepth:        1,
				MaxNodes:        10,
				MaxReferences:   10,
				ReferenceLimits: DefaultLimits(),
			},
		}
		if _, err := dereferencer.rewrite(
			Resource{}, value, nil, "", 0, map[string]bool{},
		); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("nested traversal error = %v", err)
		}
	}
}

func TestDereferenceReferenceRecursionIncrementsDepth(t *testing.T) {
	t.Parallel()

	text, _ := jsonvalue.String("#/Target")
	target, _ := jsonvalue.Object(nil)
	root, _ := jsonvalue.Object([]jsonvalue.Member{{Name: "Target", Value: target}})
	referenceObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "$ref", Value: text},
	})
	dereferencer := objectDereferencer{
		ctx: context.Background(), dialect: specversion.DialectOAS32,
		options: DereferenceOptions{
			ReferenceLimits: DefaultLimits(), MaxReferences: 1,
			MaxNodes: 10, MaxDepth: 1,
		},
	}
	_, err := dereferencer.replaceReference(
		Resource{Root: root}, referenceObject, text, nil, "", 1,
		map[string]bool{},
	)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("recursive depth error = %v", err)
	}
}

func TestDereferenceTokenHelpersUseMinimumTokenCounts(t *testing.T) {
	t.Parallel()

	if !lastParentIs([]string{"parent", "child"}, "parent") ||
		lastParentIs([]string{"child"}, "parent") {
		t.Fatal("lastParentIs minimum token contract failed")
	}
	if !lastTokenIs([]string{"token"}, "token") || lastTokenIs(nil, "token") {
		t.Fatal("lastTokenIs minimum token contract failed")
	}
}

func TestDereferenceInjectsFinalDocumentDecodeFailure(t *testing.T) {
	t.Parallel()

	root, err := parse.JSON(
		context.Background(),
		strings.NewReader(`{
			"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
		}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("final decode")
	calls := 0
	decode := func(value jsonvalue.Value) (openapi.Document, error) {
		calls++
		if calls == 2 {
			return nil, want
		}
		return openapi.Decode(value)
	}
	if _, err := dereferenceObjects(
		context.Background(), Resource{Root: root}, nil,
		DefaultDereferenceOptions(), decode,
	); !errors.Is(err, want) {
		t.Fatalf("final decode error = %v", err)
	}
}
