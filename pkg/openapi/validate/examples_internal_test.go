package validate

import (
	"context"
	"errors"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

type internalExampleCodec struct {
	encoded   []byte
	encodeErr error
	decoded   jsonvalue.Value
	decodeErr error
}

func (codec *internalExampleCodec) Encode(
	context.Context,
	jsonvalue.Value,
) ([]byte, error) {
	return codec.encoded, codec.encodeErr
}

func (codec *internalExampleCodec) Decode(
	context.Context,
	[]byte,
) (jsonvalue.Value, error) {
	return codec.decoded, codec.decodeErr
}

func TestExampleCollectionsCoverReferencedAndUnresolvedObjects(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{
		"components":{"examples":{"Shared":{"value":"value"}}}
	}`)
	owner := testValidationValue(t, `{
		"examples":{
			"shared":{"$ref":"#/components/examples/Shared"},
			"missing":{"$ref":"#/missing"}
		}
	}`)
	warnings := appendAmbiguousLegacyExampleWarnings(
		context.Background(), nil, root, owner, "/owner", "3.1.2",
	)
	if len(warnings) != 1 || warnings[0].InstanceLocation !=
		"/owner/examples/shared/$ref" {
		t.Fatalf("ambiguous warnings = %#v", warnings)
	}

	parameters := parameterExamples(
		context.Background(), root, owner, "/owner", "3.1.2",
	)
	if len(parameters) != 1 || parameters[0].pointer !=
		"/owner/examples/shared/$ref" {
		t.Fatalf("parameter examples = %#v", parameters)
	}

	serialized := serializedExamples(
		context.Background(), root, owner, "/owner", "3.1.2",
	)
	if len(serialized) != 1 || serialized[0].pointer !=
		"/owner/examples/shared/$ref" {
		t.Fatalf("serialized examples = %#v", serialized)
	}
}

func TestParameterExampleSerializationSkipsUnusableSchemas(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2",
			"components":{"parameters":{
				"unresolved":{"name":"value","in":"query","schema":{"$ref":"#/missing"}},
				"shapeless":{"name":"value","in":"query","schema":{}},
				"nameless":{"in":"query","schema":{"type":"string"}}
			}}
		}`),
	}
	if diagnostics := validateParameterExampleSerializations(
		context.Background(), document, DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("unusable parameter diagnostics = %#v", diagnostics)
	}
}

func TestSerializedParameterExampleAcceptsExactEncoding(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.2.0",
			"components":{"parameters":{"value":{
				"name":"value","in":"query","schema":{"type":"string"},
				"examples":{"valid":{"serializedValue":"value=value"}}
			}}}
		}`),
	}
	if diagnostics := validateParameterExampleSerializations(
		context.Background(), document, DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("valid serialized parameter diagnostics = %#v", diagnostics)
	}
}

func TestSerializedJSONExamplesCoverReferencePointersAndScalarValues(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{
		"components":{"examples":{"Shared":{
			"externalValue":"shared.json",
			"serializedValue":"{}"
		}}}
	}`)
	mediaType := mediaTypeLocation{
		name:    "application/json",
		pointer: "/content/application~1json",
		value: testValidationValue(t, `{
			"examples":{
				"shared":{"$ref":"#/components/examples/Shared"},
				"missing":{"$ref":"#/missing"},
				"scalar":{"serializedValue":true}
			}
		}`),
	}
	options := DefaultOptions()
	options.ReferenceResourceURI = "https://example.test/openapi.json"
	options.ExternalExampleResolver = ExternalExampleResolverFunc(func(
		context.Context, string,
	) (ExternalExampleResource, error) {
		return ExternalExampleResource{Data: []byte(`{}`)}, nil
	})
	diagnostics := validateSerializedJSONExamples(
		context.Background(), reference.Resource{Root: root}, nil,
		mediaType, "3.2.0", options,
	)
	if len(diagnostics) != 2 {
		t.Fatalf("serialized JSON diagnostics = %#v", diagnostics)
	}
	if diagnostics[0].InstanceLocation !=
		"/content/application~1json/examples/shared/$ref" ||
		diagnostics[1].InstanceLocation !=
			"/content/application~1json/examples/scalar/serializedValue" {
		t.Fatalf("serialized JSON pointers = %#v", diagnostics)
	}
}

func TestExternalAndOwnerExamplesCoverInvalidInputs(t *testing.T) {
	t.Parallel()

	options := DefaultOptions()
	options.ExternalExampleResolver = ExternalExampleResolverFunc(func(
		context.Context, string,
	) (ExternalExampleResource, error) {
		return ExternalExampleResource{}, nil
	})
	for _, external := range []jsonvalue.Value{
		jsonvalue.Boolean(true),
		testValidationValue(t, `"%"`),
	} {
		if diagnostics := validateExternalJSONExample(
			context.Background(), testValidationValue(t, `{}`), external,
			"/externalValue", nil, "3.2.0", options,
		); len(diagnostics) != 0 {
			t.Errorf("invalid external diagnostics = %#v", diagnostics)
		}
	}
	exactLimit := DefaultOptions()
	exactLimit.MaxExternalExampleBytes = 2
	exactLimit.ExternalExampleResolver = ExternalExampleResolverFunc(func(
		context.Context, string,
	) (ExternalExampleResource, error) {
		return ExternalExampleResource{Data: []byte(`{}`)}, nil
	})
	if diagnostics := validateExternalJSONExample(
		context.Background(), testValidationValue(t, `{}`),
		testValidationValue(t, `"example.json"`), "/externalValue", nil,
		"3.2.0", exactLimit,
	); len(diagnostics) != 0 {
		t.Fatalf("exact-limit external diagnostics = %#v", diagnostics)
	}

	version, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"openapi":"3.1.2",
			"components":{"examples":{}}
		}`),
	}
	owner := locatedParameter{
		pointer: "/owner",
		value: testValidationValue(t, `{
			"schema":{"type":"string"},
			"examples":{"missing":{"$ref":"#/missing"}}
		}`),
	}
	compiler, err := openSchemaCompiler(document)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics := validateExampleOwnerSchema(
		context.Background(), document, compiler, owner, DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("unresolved owner diagnostics = %#v", diagnostics)
	}
	if got := appendInvalidExample(
		context.Background(), nil, nil, jsonvalue.Value{}, "/example", "3.1.2",
	); len(got) != 0 {
		t.Fatalf("invalid immutable example diagnostics = %#v", got)
	}
}

func TestMediaTypeExampleCodecHelpersCoverInvalidInputs(t *testing.T) {
	t.Parallel()

	var nilCodec *internalExampleCodec
	if !mediaTypeExampleCodecIsNil(nilCodec) {
		t.Fatal("typed nil codec was accepted")
	}
	codec := &internalExampleCodec{
		encoded: []byte("long"),
		decoded: testValidationValue(t, `2`),
	}
	diagnostics := appendCodecDataExample(
		context.Background(),
		nil,
		codec,
		nil,
		testValidationValue(t, `1`),
		"/data",
		"3.2.0",
		1,
	)
	if len(diagnostics) != 1 ||
		diagnostics[0].Code != "openapi.example.media-serialization-limit" {
		t.Fatalf("data diagnostics = %#v", diagnostics)
	}
	if exact := appendCodecDataExample(
		context.Background(), nil, codec, nil,
		testValidationValue(t, `1`), "/data", "3.2.0", len(codec.encoded),
	); len(exact) != 0 {
		t.Fatalf("exact data diagnostics = %#v", exact)
	}
	for _, serialized := range []jsonvalue.Value{
		testValidationValue(t, `1`),
		testValidationValue(t, `"long"`),
	} {
		diagnostics = appendCodecSerializedExample(
			context.Background(),
			nil,
			codec,
			nil,
			serialized,
			jsonvalue.Value{},
			false,
			"/serialized",
			"3.2.0",
			1,
		)
		if serialized.Kind() == jsonvalue.StringKind &&
			(len(diagnostics) != 1 ||
				diagnostics[0].Code != "openapi.example.serialized-limit") {
			t.Fatalf("serialized diagnostics = %#v", diagnostics)
		}
	}
	if exact := appendCodecSerializedExample(
		context.Background(), nil, codec, nil,
		testValidationValue(t, `"2"`), jsonvalue.Value{}, false,
		"/serialized", "3.2.0", 1,
	); len(exact) != 0 {
		t.Fatalf("exact serialized diagnostics = %#v", exact)
	}
	if got := appendCodecSchemaDiagnostic(
		context.Background(), nil, nil, codec.decoded,
		"code", "message", "/schema", "3.2.0",
	); len(got) != 0 {
		t.Fatalf("nil schema diagnostics = %#v", got)
	}
	if got := appendCodecSchemaDiagnostic(
		context.Background(), nil, new(openapischema.Schema), jsonvalue.Value{},
		"code", "message", "/schema", "3.2.0",
	); len(got) != 0 {
		t.Fatalf("unmarshalable schema diagnostics = %#v", got)
	}
}

func TestMediaTypeExternalCodecExamplesCoverFailures(t *testing.T) {
	t.Parallel()

	codec := &internalExampleCodec{decoded: testValidationValue(t, `1`)}
	base := DefaultOptions()
	base.ReferenceResourceURI = "https://api.example.test/openapi.json"
	for _, test := range []struct {
		name       string
		external   jsonvalue.Value
		maximum    int
		resolveErr error
		data       []byte
		decodeErr  error
		want       string
	}{
		{name: "non-string", external: testValidationValue(t, `1`)},
		{name: "invalid URI", external: testValidationValue(t, `"%zz"`)},
		{name: "unresolved", external: testValidationValue(t, `"value.txt"`),
			resolveErr: errors.New("missing"),
			want:       "openapi.example.external-value.unresolved"},
		{name: "limit", external: testValidationValue(t, `"value.txt"`),
			maximum: 1, data: []byte("12"),
			want: "openapi.example.external-value.limit"},
		{name: "exact", external: testValidationValue(t, `"value.txt"`),
			maximum: 1, data: []byte("1")},
		{name: "invalid", external: testValidationValue(t, `"value.txt"`),
			data: []byte("value"), decodeErr: errors.New("invalid"),
			want: "openapi.example.external-value.invalid"},
	} {
		options := base
		if test.maximum > 0 {
			options.MaxExternalExampleBytes = test.maximum
		}
		options.ExternalExampleResolver = ExternalExampleResolverFunc(func(
			context.Context,
			string,
		) (ExternalExampleResource, error) {
			return ExternalExampleResource{Data: test.data}, test.resolveErr
		})
		codec.decodeErr = test.decodeErr
		diagnostics := appendCodecExternalExample(
			context.Background(), nil, codec, nil, test.external,
			jsonvalue.Value{}, false, "/external", "3.2.0", options,
		)
		if test.want == "" {
			if len(diagnostics) != 0 {
				t.Fatalf("%s diagnostics = %#v", test.name, diagnostics)
			}
			continue
		}
		if len(diagnostics) != 1 || diagnostics[0].Code != test.want {
			t.Fatalf("%s diagnostics = %#v", test.name, diagnostics)
		}
	}
}

func TestSwaggerExampleMediaTypesSkipsOperationsWithoutResponses(t *testing.T) {
	t.Parallel()

	version, err := openapi.ParseVersion("2.0")
	if err != nil {
		t.Fatal(err)
	}
	document := validationDocument{
		version: version,
		raw: testValidationValue(t, `{
			"swagger":"2.0",
			"paths":{"/value":{"get":{"produces":["application/json"]}}}
		}`),
	}
	if diagnostics := validateSwaggerExampleMediaTypes(
		context.Background(), document, DefaultOptions(),
	); len(diagnostics) != 0 {
		t.Fatalf("response-less operation diagnostics = %#v", diagnostics)
	}
}

func openSchemaCompiler(
	document openapi.Document,
) (*openapischema.Compiler, error) {
	return openapischema.NewCompilerForDocument(document)
}
