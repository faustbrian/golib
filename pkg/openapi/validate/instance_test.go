package validate_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestSchemaInstanceEnforcesSwaggerReadOnlyDirection(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{},"definitions":{"Pet":{"type":"object","properties":{
			"id":{"type":"integer","readOnly":true},
			"name":{"type":"string"},
			"owners":{"type":"array","items":{"type":"object","properties":{
				"token":{"type":"string","readOnly":true}
			}}}
		}}}
	}`)
	schema := definitionSchema(t, document.Raw(), "Pet")
	instance := mustJSONValue(t, `{
		"id":1,"name":"Miso","owners":[{"token":"secret"}]
	}`)

	request, err := validate.SchemaInstance(
		context.Background(), document, schema, instance,
		validate.InstanceOptions{Direction: validate.DirectionRequest},
	)
	if err != nil {
		t.Fatal(err)
	}
	if request.Valid() {
		t.Fatalf("request containing read-only property was accepted: %#v",
			request.Diagnostics())
	}
	want := map[string]bool{"/id": false, "/owners/0/token": false}
	for _, diagnostic := range request.Diagnostics() {
		if diagnostic.Code == "openapi.schema.read-only.request" {
			if _, exists := want[diagnostic.InstanceLocation]; exists {
				want[diagnostic.InstanceLocation] = true
			}
		}
	}
	for pointer, found := range want {
		if !found {
			t.Fatalf("missing read-only request diagnostic at %s: %#v",
				pointer, request.Diagnostics())
		}
	}

	response, err := validate.SchemaInstance(
		context.Background(), document, schema, instance,
		validate.InstanceOptions{Direction: validate.DirectionResponse},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Valid() {
		t.Fatalf("response containing read-only property was rejected: %#v",
			response.Diagnostics())
	}
}

func TestSchemaInstanceValidatesSwaggerDiscriminatorValues(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{},"definitions":{
			"Pet":{"type":"object","discriminator":"kind",
				"required":["kind"],"properties":{"kind":{"type":"string"}}},
			"Cat":{"allOf":[{"$ref":"#/definitions/Pet"},{"type":"object"}]}
		}
	}`)
	schema := definitionSchema(t, document.Raw(), "Pet")
	for _, test := range []struct {
		value string
		valid bool
	}{
		{value: `{"kind":"Pet"}`, valid: true},
		{value: `{"kind":"Cat"}`, valid: true},
		{value: `{"kind":"Dog"}`, valid: false},
	} {
		instance := mustJSONValue(t, test.value)
		report, err := validate.SchemaInstance(
			context.Background(), document, schema, instance,
			validate.InstanceOptions{SchemaName: "Pet"},
		)
		if err != nil {
			t.Fatal(err)
		}
		if report.Valid() != test.valid {
			t.Fatalf("SchemaInstance(%s) valid = %t, want %t: %#v",
				test.value, report.Valid(), test.valid, report.Diagnostics())
		}
	}
}

func TestSchemaInstanceValidatesOpenAPIDiscriminatorDescendants(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{},"components":{"schemas":{
					"Pet":{"type":"object","required":["kind"],
						"properties":{"kind":{"type":"string"}},
						"discriminator":{"propertyName":"kind","mapping":{
							"canine":"Dog"
						}}},
					"Cat":{"allOf":[{"$ref":"#/components/schemas/Pet"}]},
					"Dog":{"allOf":[{"$ref":"#/components/schemas/Pet"}]}
				}}
			}`)
			components, _ := document.Raw().Lookup("components")
			schemas, _ := components.Lookup("schemas")
			schema, _ := schemas.Lookup("Pet")
			for _, test := range []struct {
				value string
				valid bool
			}{
				{value: `{"kind":"Cat"}`, valid: true},
				{value: `{"kind":"canine"}`, valid: true},
				{value: `{"kind":"Missing"}`},
			} {
				report, err := validate.SchemaInstance(
					context.Background(), document, schema,
					mustJSONValue(t, test.value), validate.InstanceOptions{},
				)
				if err != nil {
					t.Fatal(err)
				}
				if report.Valid() != test.valid {
					t.Fatalf("SchemaInstance(%s) valid = %t, want %t: %#v",
						test.value, report.Valid(), test.valid, report.Diagnostics())
				}
			}
		})
	}
}

func TestSchemaInstanceRunsSchemaEvaluation(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Count":{"type":"integer"}}}
	}`)
	components, _ := document.Raw().Lookup("components")
	schemas, _ := components.Lookup("schemas")
	schema, _ := schemas.Lookup("Count")
	report, err := validate.SchemaInstance(
		context.Background(), document, schema,
		mustJSONValue(t, `"wrong"`), validate.InstanceOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid() || report.Diagnostics()[0].Code != "openapi.schema.instance" {
		t.Fatalf("invalid schema instance result: %#v", report.Diagnostics())
	}
}

func TestSequentialMediaInstanceAppliesSchemaAndItemSchema(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	mediaType := mustJSONValue(t, `{
		"schema":{"type":"array","minItems":3},
		"itemSchema":{"type":"integer"}
	}`)
	report, err := validate.SequentialMediaInstance(
		context.Background(),
		document,
		mediaType,
		[]jsonvalue.Value{mustJSONValue(t, `1`), mustJSONValue(t, `"wrong"`)},
		validate.InstanceOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"openapi.media-type.schema.instance":      "",
		"openapi.media-type.item-schema.instance": "/1",
	}
	for _, diagnostic := range report.Diagnostics() {
		pointer, exists := want[diagnostic.Code]
		if !exists {
			continue
		}
		if diagnostic.InstanceLocation != pointer ||
			diagnostic.SpecificationSection != "sequential-media-types" {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
		delete(want, diagnostic.Code)
	}
	if len(want) != 0 {
		t.Fatalf("missing diagnostics %v: %#v", want, report.Diagnostics())
	}
}

func TestSequentialMediaInstanceAcceptsConformingOrderedItems(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	report, err := validate.SequentialMediaInstance(
		context.Background(),
		document,
		mustJSONValue(t, `{
			"schema":{"type":"array","prefixItems":[
				{"const":1},{"const":2}
			]},
			"itemSchema":{"type":"integer"}
		}`),
		[]jsonvalue.Value{mustJSONValue(t, `1`), mustJSONValue(t, `2`)},
		validate.InstanceOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("ordered items rejected: %#v", report.Diagnostics())
	}
}

func TestSequentialMediaInstanceValidatesInputsAndBounds(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	legacy := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	mediaType := mustJSONValue(t, `{"itemSchema":{"type":"integer"}}`)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name    string
		ctx     context.Context
		doc     openapi.Document
		media   jsonvalue.Value
		items   []jsonvalue.Value
		options validate.InstanceOptions
		want    string
	}{
		{name: "nil context", doc: document, media: mediaType,
			want: "nil context"},
		{name: "nil document", ctx: context.Background(), media: mediaType,
			want: "nil document"},
		{name: "wrong dialect", ctx: context.Background(), doc: legacy,
			media: mediaType, want: "OpenAPI 3.2 document required"},
		{name: "non-object", ctx: context.Background(), doc: document,
			media: mustJSONValue(t, `true`), want: "media type is not an object"},
		{name: "invalid direction", ctx: context.Background(), doc: document,
			media: mediaType, options: validate.InstanceOptions{Direction: "sideways"},
			want: "invalid direction"},
		{name: "negative limit", ctx: context.Background(), doc: document,
			media: mediaType, options: validate.InstanceOptions{MaxNodes: -1},
			want: "invalid node limit"},
		{name: "item limit", ctx: context.Background(), doc: document,
			media:   mediaType,
			items:   []jsonvalue.Value{mustJSONValue(t, `1`), mustJSONValue(t, `2`)},
			options: validate.InstanceOptions{MaxNodes: 1},
			want:    validate.ErrLimitExceeded.Error()},
		{name: "canceled", ctx: canceled, doc: document, media: mediaType,
			want: context.Canceled.Error()},
		{name: "invalid sequence item", ctx: context.Background(), doc: document,
			media: mustJSONValue(t, `{"schema":{}}`),
			items: []jsonvalue.Value{{}}, want: "array contains zero value"},
		{name: "invalid complete schema", ctx: context.Background(), doc: document,
			media: mustJSONValue(t, `{"schema":1}`), want: "instance schema"},
		{name: "invalid item schema", ctx: context.Background(), doc: document,
			media: mustJSONValue(t, `{"itemSchema":1}`),
			items: []jsonvalue.Value{mustJSONValue(t, `1`)},
			want:  "sequential media item 0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := validate.SequentialMediaInstance(
				test.ctx, test.doc, test.media, test.items, test.options,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestBinaryMediaLengthAppliesMaxLengthToOctets(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{}
			}`)
			report, err := validate.BinaryMediaLength(
				context.Background(), document,
				mustJSONValue(t, `{"type":"string","maxLength":3}`), 4,
			)
			if err != nil {
				t.Fatal(err)
			}
			if report.Valid() || len(report.Diagnostics()) != 1 {
				t.Fatalf("report = %#v", report.Diagnostics())
			}
			diagnostic := report.Diagnostics()[0]
			if diagnostic.Code != "openapi.schema.binary.max-length" ||
				diagnostic.KeywordLocation != "/maxLength" ||
				diagnostic.SpecificationVersion != version {
				t.Fatalf("diagnostic = %#v", diagnostic)
			}

			withinLimit, withinErr := validate.BinaryMediaLength(
				context.Background(), document,
				mustJSONValue(t, `{"maxLength":3}`), 3,
			)
			if withinErr != nil {
				t.Fatal(withinErr)
			}
			if !withinLimit.Valid() {
				t.Fatalf("boundary length rejected: %#v", withinLimit.Diagnostics())
			}
		})
	}
}

func TestBinaryMediaLengthValidatesInputsAndExactNumbers(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"One":{"maxLength":1}}}
	}`)
	legacy := mustDocument(t, `{
		"swagger":"2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name   string
		ctx    context.Context
		doc    openapi.Document
		schema jsonvalue.Value
		want   string
	}{
		{name: "nil context", doc: document, schema: mustJSONValue(t, `{}`),
			want: "nil context"},
		{name: "nil document", ctx: context.Background(),
			schema: mustJSONValue(t, `{}`), want: "nil document"},
		{name: "legacy dialect", ctx: context.Background(), doc: legacy,
			schema: mustJSONValue(t, `{}`), want: "OpenAPI 3 document required"},
		{name: "invalid schema", ctx: context.Background(), doc: document,
			schema: mustJSONValue(t, `1`), want: "not an object or boolean"},
		{name: "canceled", ctx: canceled, doc: document,
			schema: mustJSONValue(t, `{}`), want: context.Canceled.Error()},
		{name: "unresolved", ctx: context.Background(), doc: document,
			schema: mustJSONValue(t, `{"$ref":"#/components/schemas/Missing"}`),
			want:   "unresolved schema reference"},
		{name: "wrong type", ctx: context.Background(), doc: document,
			schema: mustJSONValue(t, `{"maxLength":"1"}`),
			want:   "non-negative integer"},
		{name: "negative", ctx: context.Background(), doc: document,
			schema: mustJSONValue(t, `{"maxLength":-1}`),
			want:   "non-negative integer"},
		{name: "fractional", ctx: context.Background(), doc: document,
			schema: mustJSONValue(t, `{"maxLength":1.5}`),
			want:   "non-negative integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := validate.BinaryMediaLength(
				test.ctx, test.doc, test.schema, 0,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
	for _, schema := range []string{
		`true`, `{}`, `{"maxLength":3e0}`,
		`{"maxLength":18446744073709551616}`,
	} {
		report, err := validate.BinaryMediaLength(
			context.Background(), document, mustJSONValue(t, schema), 3,
		)
		if err != nil {
			t.Fatalf("schema %s: %v", schema, err)
		}
		if !report.Valid() {
			t.Fatalf("schema %s rejected length: %#v", schema, report.Diagnostics())
		}
	}
	referenced, err := validate.BinaryMediaLength(
		context.Background(), document,
		mustJSONValue(t, `{"$ref":"#/components/schemas/One"}`), 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if referenced.Valid() {
		t.Fatalf("referenced maxLength was not applied")
	}
}

func TestBinaryMediaLengthExaminesRefsAndAllOfSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{
			"Limit":{"maxLength":4},
			"Cycle":{"maxLength":3,"allOf":[
				{"$ref":"#/components/schemas/Cycle"}
			]}
		}}
	}`)
	for _, schema := range []string{
		`{"allOf":[{"maxLength":6},{"$ref":"#/components/schemas/Limit"}]}`,
		`{"$ref":"#/components/schemas/Cycle"}`,
	} {
		report, err := validate.BinaryMediaLength(
			context.Background(), document, mustJSONValue(t, schema), 5,
		)
		if err != nil {
			t.Fatal(err)
		}
		if report.Valid() {
			t.Fatalf("schema %s did not apply its reachable maxLength", schema)
		}
	}
}

func TestBinaryMediaLengthResolvesAuthorizedExternalAllOfSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	requested := ""
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		ctx context.Context,
		identifier string,
	) (reference.Resource, error) {
		if err := ctx.Err(); err != nil {
			return reference.Resource{}, err
		}
		requested = identifier
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         mustJSONValue(t, `{"maxLength":2}`),
		}, nil
	})
	report, err := validate.BinaryMediaLengthWithOptions(
		context.Background(), document,
		mustJSONValue(t, `{"allOf":[{"$ref":"limits.json"}]}`),
		3,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid() || requested != "https://api.example.test/limits.json" {
		t.Fatalf("report = %#v, requested = %q", report.Diagnostics(), requested)
	}
}

func TestBinaryMediaLengthBoundsSchemaTraversal(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceLimits.MaxTraversalNodes = 1
	_, err := validate.BinaryMediaLengthWithOptions(
		context.Background(), document,
		mustJSONValue(t, `{"allOf":[{"maxLength":1}]}`), 0, options,
	)
	if !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("traversal limit error = %v", err)
	}
	options = validate.DefaultOptions()
	options.ReferenceLimits.MaxTraversalNodes = -1
	_, err = validate.BinaryMediaLengthWithOptions(
		context.Background(), document, mustJSONValue(t, `{}`), 0, options,
	)
	if err == nil || !strings.Contains(err.Error(), "invalid reference options") {
		t.Fatalf("invalid options error = %v", err)
	}
	options = validate.DefaultOptions()
	options.ReferenceLimits.MaxTraversalDepth = 1
	_, err = validate.BinaryMediaLengthWithOptions(
		context.Background(), document,
		mustJSONValue(t, `{"allOf":[{"maxLength":1}]}`), 0, options,
	)
	if !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("traversal depth error = %v", err)
	}
}

func TestBinaryMediaLengthRejectsMalformedReachableSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	for _, schema := range []string{
		`{"allOf":{}}`,
		`{"allOf":[1]}`,
	} {
		_, err := validate.BinaryMediaLength(
			context.Background(), document, mustJSONValue(t, schema), 0,
		)
		if err == nil {
			t.Fatalf("malformed reachable schema %s was accepted", schema)
		}
	}
}

func TestBinaryMediaLengthHonorsOpenAPI30ReferenceSiblingRules(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Limit":{"maxLength":10}}}
	}`)
	report, err := validate.BinaryMediaLength(
		context.Background(), document,
		mustJSONValue(t, `{
			"$ref":"#/components/schemas/Limit","maxLength":1
		}`), 5,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid() {
		t.Fatalf("OpenAPI 3.0 Reference Object sibling applied: %#v",
			report.Diagnostics())
	}
}

func TestBinaryMediaLengthPropagatesCancellationAfterResolution(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{}
	}`)
	ctx, cancel := context.WithCancel(context.Background())
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		cancel()
		return reference.Resource{
			RetrievalURI: "https://api.example.test/limit.json",
			Root:         mustJSONValue(t, `{"maxLength":1}`),
		}, nil
	})
	_, err := validate.BinaryMediaLengthWithOptions(
		ctx, document, mustJSONValue(t, `{"$ref":"limit.json"}`), 0, options,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestMultipartBinaryMediaLengthsPartitionsNamedAndPositionalSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{
			"File":{"maxLength":2},
			"Prefix":{"maxLength":1},
			"Sequence":{
				"type":"object",
				"properties":{"file":{"$ref":"#/components/schemas/File"}},
				"prefixItems":[{"$ref":"#/components/schemas/Prefix"}],
				"allOf":[{"items":{"maxLength":3}}]
			}
		}}
	}`)
	mediaType := mustJSONValue(t, `{
		"schema":{"$ref":"#/components/schemas/Sequence"},
		"itemSchema":{"maxLength":4}
	}`)
	report, err := validate.MultipartBinaryMediaLengths(
		context.Background(),
		document,
		mediaType,
		[]validate.NamedBinaryMediaPart{
			{Name: "file", Octets: 3},
			{Name: "file", Octets: 2},
			{Name: "missing", Octets: 100},
		},
		[]validate.PositionalBinaryMediaPart{
			{Index: 0, Octets: 2},
			{Index: 1, Octets: 1},
			{Index: 2, Octets: 4},
		},
		validate.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/file": false, "/0": false, "/2": false}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.schema.binary.max-length" {
			continue
		}
		if _, tracked := want[diagnostic.InstanceLocation]; tracked {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestMultipartBinaryMediaLengthsValidatesInputs(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	legacy := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},"paths":{}
	}`)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	invalidOptions := validate.DefaultOptions()
	invalidOptions.ReferenceLimits.MaxTraversalNodes = -1
	for _, test := range []struct {
		name       string
		ctx        context.Context
		document   openapi.Document
		mediaType  jsonvalue.Value
		named      []validate.NamedBinaryMediaPart
		positional []validate.PositionalBinaryMediaPart
		options    validate.Options
		want       string
	}{
		{name: "nil context", document: document, mediaType: mustJSONValue(t, `{}`),
			options: validate.DefaultOptions(), want: "nil context"},
		{name: "nil document", ctx: context.Background(),
			mediaType: mustJSONValue(t, `{}`), options: validate.DefaultOptions(),
			want: "nil document"},
		{name: "legacy", ctx: context.Background(), document: legacy,
			mediaType: mustJSONValue(t, `{}`), options: validate.DefaultOptions(),
			want: "OpenAPI 3.2"},
		{name: "media type", ctx: context.Background(), document: document,
			mediaType: mustJSONValue(t, `1`), options: validate.DefaultOptions(),
			want: "not an object"},
		{name: "options", ctx: context.Background(), document: document,
			mediaType: mustJSONValue(t, `{}`), options: invalidOptions,
			want: "invalid reference options"},
		{name: "canceled", ctx: canceled, document: document,
			mediaType: mustJSONValue(t, `{}`), options: validate.DefaultOptions(),
			want: context.Canceled.Error()},
		{name: "empty name", ctx: context.Background(), document: document,
			mediaType: mustJSONValue(t, `{}`), options: validate.DefaultOptions(),
			named: []validate.NamedBinaryMediaPart{{}}, want: "empty part name"},
		{name: "negative index", ctx: context.Background(), document: document,
			mediaType: mustJSONValue(t, `{}`), options: validate.DefaultOptions(),
			positional: []validate.PositionalBinaryMediaPart{{Index: -1}},
			want:       "negative part index"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := validate.MultipartBinaryMediaLengths(
				test.ctx,
				test.document,
				test.mediaType,
				test.named,
				test.positional,
				test.options,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMultipartBinaryMediaLengthsRejectsMalformedSubschemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{},
		"components":{"schemas":{"Bad":1}}
	}`)
	for _, mediaType := range []string{
		`{"schema":{"properties":{"file":{"maxLength":"bad"}}}}`,
		`{"schema":1}`,
		`{"schema":{"prefixItems":{}}}`,
		`{"schema":{"allOf":{}}}`,
		`{"schema":{"allOf":[1]}}`,
		`{"schema":{"$ref":"#/missing"}}`,
		`{"schema":{"$ref":"#/components/schemas/Bad"}}`,
		`{"schema":{"prefixItems":[1]}}`,
	} {
		_, err := validate.MultipartBinaryMediaLengths(
			context.Background(),
			document,
			mustJSONValue(t, mediaType),
			[]validate.NamedBinaryMediaPart{{Name: "file"}},
			[]validate.PositionalBinaryMediaPart{{Index: 0}},
			validate.DefaultOptions(),
		)
		if err == nil {
			t.Errorf("malformed media type accepted: %s", mediaType)
		}
	}
	withoutSchema, err := validate.MultipartBinaryMediaLengths(
		context.Background(),
		document,
		mustJSONValue(t, `{"itemSchema":{"maxLength":1}}`),
		nil,
		[]validate.PositionalBinaryMediaPart{{Index: 0, Octets: 1}},
		validate.DefaultOptions(),
	)
	if err != nil || !withoutSchema.Valid() {
		t.Fatalf("itemSchema-only report = %#v, %v", withoutSchema.Diagnostics(), err)
	}
	booleanSchema, err := validate.MultipartBinaryMediaLengths(
		context.Background(), document, mustJSONValue(t, `{"schema":true}`),
		nil, []validate.PositionalBinaryMediaPart{{Index: 0}},
		validate.DefaultOptions(),
	)
	if err != nil || !booleanSchema.Valid() {
		t.Fatalf("boolean schema report = %#v, %v", booleanSchema.Diagnostics(), err)
	}
	depthOptions := validate.DefaultOptions()
	depthOptions.ReferenceLimits.MaxTraversalDepth = 1
	_, err = validate.MultipartBinaryMediaLengths(
		context.Background(), document,
		mustJSONValue(t, `{"schema":{"allOf":[{}]}}`), nil,
		[]validate.PositionalBinaryMediaPart{{Index: 0}}, depthOptions,
	)
	if !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("depth error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancelOptions := validate.DefaultOptions()
	cancelOptions.ReferenceResourceURI = "https://api.example.test/openapi.json"
	cancelOptions.ReferenceResolver = reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		cancel()
		return reference.Resource{
			RetrievalURI: "https://api.example.test/schema.json",
			Root:         mustJSONValue(t, `{}`),
		}, nil
	})
	_, err = validate.MultipartBinaryMediaLengths(
		canceled, document, mustJSONValue(t, `{"schema":{"$ref":"schema.json"}}`),
		nil, []validate.PositionalBinaryMediaPart{{Index: 0}}, cancelOptions,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestMultipartBinaryMediaLengthsHonorsExactTraversalLimits(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	mediaType := mustJSONValue(t, `{
		"schema":{"allOf":[{"prefixItems":[{"maxLength":1}]}]}
	}`)
	part := []validate.PositionalBinaryMediaPart{{Index: 0, Octets: 2}}
	exact := validate.DefaultOptions()
	exact.ReferenceLimits.MaxTraversalDepth = 2
	exact.ReferenceLimits.MaxTraversalNodes = 2
	report, err := validate.MultipartBinaryMediaLengths(
		context.Background(), document, mediaType, nil, part, exact,
	)
	if err != nil {
		t.Fatalf("exact traversal limits: %v", err)
	}
	if report.Valid() {
		t.Fatalf("nested maxLength was not applied: %#v", report.Diagnostics())
	}

	for _, limited := range []validate.Options{
		func() validate.Options {
			options := exact
			options.ReferenceLimits.MaxTraversalDepth = 1
			return options
		}(),
		func() validate.Options {
			options := exact
			options.ReferenceLimits.MaxTraversalNodes = 1
			return options
		}(),
	} {
		_, err := validate.MultipartBinaryMediaLengths(
			context.Background(), document, mediaType, nil, part, limited,
		)
		if !errors.Is(err, validate.ErrLimitExceeded) {
			t.Errorf("limit error = %v", err)
		}
	}
}

func TestMultipartBinaryMediaLengthsResolvesReferencesWithinExactLimits(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceLimits.MaxTraversalDepth = 2
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		if identifier != "https://api.example.test/part.json" {
			return reference.Resource{}, fmt.Errorf("unexpected identifier %q", identifier)
		}
		return reference.Resource{
			RetrievalURI: identifier,
			Root: mustJSONValue(t, `{
				"allOf":[{"prefixItems":[{"maxLength":1}]}]
			}`),
		}, nil
	})
	tooShallow := options
	tooShallow.ReferenceLimits.MaxTraversalDepth = 2
	_, err := validate.MultipartBinaryMediaLengths(
		context.Background(), document,
		mustJSONValue(t, `{"schema":{"$ref":"part.json"}}`),
		nil, []validate.PositionalBinaryMediaPart{{Index: 0, Octets: 2}},
		tooShallow,
	)
	if !errors.Is(err, validate.ErrLimitExceeded) {
		t.Fatalf("reference traversal depth error = %v", err)
	}
	options.ReferenceLimits.MaxTraversalDepth = 3
	report, err := validate.MultipartBinaryMediaLengths(
		context.Background(), document,
		mustJSONValue(t, `{"schema":{"$ref":"part.json"}}`),
		nil, []validate.PositionalBinaryMediaPart{{Index: 0, Octets: 2}},
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid() {
		t.Fatalf("referenced maxLength was not applied: %#v", report.Diagnostics())
	}
}

func TestMultipartBinaryMediaLengthsTracksReferencesByResource(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		var schema string
		switch identifier {
		case "https://api.example.test/a/schema.json",
			"https://api.example.test/b/schema.json":
			schema = `{"$ref":"part.json"}`
		case "https://api.example.test/a/part.json":
			schema = `{"prefixItems":[{"maxLength":10}]}`
		case "https://api.example.test/b/part.json":
			schema = `{"prefixItems":[{"maxLength":1}]}`
		default:
			return reference.Resource{}, fmt.Errorf(
				"unexpected identifier %q", identifier,
			)
		}
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         mustJSONValue(t, schema),
		}, nil
	})
	report, err := validate.MultipartBinaryMediaLengths(
		context.Background(), document,
		mustJSONValue(t, `{"schema":{"allOf":[
			{"$ref":"a/schema.json"},{"$ref":"b/schema.json"}
		]}}`),
		nil, []validate.PositionalBinaryMediaPart{{Index: 0, Octets: 2}},
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid() {
		t.Fatalf("second resource constraint was skipped: %#v", report.Diagnostics())
	}
}

func TestMultipartBinaryMediaLengthsPartitionsPrefixBoundary(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	mediaType := mustJSONValue(t, `{
		"schema":{"prefixItems":[{"maxLength":1}],"items":{"maxLength":3}}
	}`)
	report, err := validate.MultipartBinaryMediaLengths(
		context.Background(), document, mediaType, nil,
		[]validate.PositionalBinaryMediaPart{
			{Index: 0, Octets: 2},
			{Index: 1, Octets: 4},
			{Index: 2, Octets: 2},
		},
		validate.DefaultOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/0": false, "/1": false}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.schema.binary.max-length" {
			continue
		}
		if diagnostic.InstanceLocation == "/2" {
			t.Errorf("items boundary rejected valid part: %#v", diagnostic)
		}
		if _, tracked := want[diagnostic.InstanceLocation]; tracked {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestMultipartBinaryMediaLengthsAcceptsZeroReferenceLimits(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{}
	}`)
	options := validate.DefaultOptions()
	options.ReferenceLimits.MaxTraversalNodes = 0
	report, err := validate.MultipartBinaryMediaLengths(
		context.Background(), document, mustJSONValue(t, `{}`), nil, nil, options,
	)
	if err != nil || !report.Valid() {
		t.Fatalf("zero reference limit report = %#v, %v",
			report.Diagnostics(), err)
	}
}

func TestSchemaInstanceResolvesRootInternalSchemaReference(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Count":{"type":"integer"}}}
	}`)
	report, err := validate.SchemaInstance(
		context.Background(), document,
		mustJSONValue(t, `{"$ref":"#/components/schemas/Count"}`),
		mustJSONValue(t, `"wrong"`), validate.InstanceOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid() || report.Diagnostics()[0].Code != "openapi.schema.instance" {
		t.Fatalf("invalid referenced schema result: %#v", report.Diagnostics())
	}
}

func TestOpenAPIDiscriminatorAddsSemanticInstanceDiagnostics(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.0.4", "3.1.1", "3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{}
			}`)
			schema := mustJSONValue(t, `{
				"type":"object","required":["id"],"properties":{
					"id":{"type":"integer"},"kind":{"type":"string"}
				},"discriminator":{"propertyName":"kind","mapping":{"cat":"Cat"}}
			}`)
			for _, test := range []struct {
				value string
				code  string
			}{
				{
					value: `{"id":1,"kind":"cat"}`,
					code:  "openapi.schema.discriminator.value",
				},
				{
					value: `{"id":"wrong","kind":"cat"}`,
					code:  "openapi.schema.instance",
				},
			} {
				report, err := validate.SchemaInstance(
					context.Background(), document, schema,
					mustJSONValue(t, test.value), validate.InstanceOptions{},
				)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, diagnostic := range report.Diagnostics() {
					if diagnostic.Code == test.code {
						found = true
					}
				}
				if report.Valid() || !found {
					t.Fatalf("SchemaInstance(%s) diagnostics = %#v, want %s",
						test.value, report.Diagnostics(), test.code)
				}
			}
		})
	}
}

func TestSchemaInstanceAppliesOpenAPIReadWriteDirection(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{},"components":{"schemas":{"Account":{
			"type":"object","required":["id","secret"],"properties":{
				"id":{"type":"integer","readOnly":true},
				"secret":{"type":"string","writeOnly":true}
			}
		}}}
	}`)
	components, _ := document.Raw().Lookup("components")
	schemas, _ := components.Lookup("schemas")
	schema, _ := schemas.Lookup("Account")

	request, err := validate.SchemaInstance(
		context.Background(), document, schema,
		mustJSONValue(t, `{"secret":"value"}`),
		validate.InstanceOptions{Direction: validate.DirectionRequest},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !request.Valid() {
		t.Fatalf("request incorrectly required read-only property: %#v",
			request.Diagnostics())
	}

	response, err := validate.SchemaInstance(
		context.Background(), document, schema,
		mustJSONValue(t, `{"id":1}`),
		validate.InstanceOptions{Direction: validate.DirectionResponse},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Valid() {
		t.Fatalf("response incorrectly required write-only property: %#v",
			response.Diagnostics())
	}

	requestWithID, err := validate.SchemaInstance(
		context.Background(), document, schema,
		mustJSONValue(t, `{"id":1,"secret":"value"}`),
		validate.InstanceOptions{Direction: validate.DirectionRequest},
	)
	if err != nil {
		t.Fatal(err)
	}
	responseWithSecret, err := validate.SchemaInstance(
		context.Background(), document, schema,
		mustJSONValue(t, `{"id":1,"secret":"value"}`),
		validate.InstanceOptions{Direction: validate.DirectionResponse},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertDirectionWarning(
		t, requestWithID, "openapi.schema.read-only.request", "/id",
	)
	assertDirectionWarning(
		t, responseWithSecret, "openapi.schema.write-only.response", "/secret",
	)
}

func assertDirectionWarning(
	t *testing.T,
	report validate.Report,
	code string,
	pointer string,
) {
	t.Helper()
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == code && diagnostic.InstanceLocation == pointer &&
			diagnostic.Severity == validate.SeverityWarning {
			return
		}
	}
	t.Fatalf("missing direction warning %s at %s: %#v",
		code, pointer, report.Diagnostics())
}

func definitionSchema(
	t *testing.T,
	root jsonvalue.Value,
	name string,
) jsonvalue.Value {
	t.Helper()
	definitions, exists := root.Lookup("definitions")
	if !exists {
		t.Fatal("missing definitions")
	}
	schema, exists := definitions.Lookup(name)
	if !exists {
		t.Fatalf("missing definition %s", name)
	}
	return schema
}

func mustJSONValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
