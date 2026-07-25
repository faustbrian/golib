package media_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/media"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestSelectSerializationDataTypePrefersRuntimeData(t *testing.T) {
	t.Parallel()

	data, err := jsonvalue.Number("1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := media.SelectSerializationDataType(
		context.Background(),
		reference.Resource{Root: mustMediaValue(t, `{}`)},
		mustMediaValue(t, `{
			"oneOf":[{"type":"string"},{"type":"number"}]
		}`),
		media.SerializationTypeOptions{Data: data},
	)
	if err != nil || got != media.SerializationDataTypeNumber {
		t.Fatalf("runtime serialization type = %q, %v", got, err)
	}
}

func TestSelectSerializationDataTypeFollowsRefAndAllOf(t *testing.T) {
	t.Parallel()

	root := mustMediaValue(t, `{
		"$defs":{"Text":{"type":"string"}},
		"schema":{"allOf":[
			{"$ref":"#/$defs/Text"},
			{"type":["string","null"]}
		]}
	}`)
	schema, _ := root.Lookup("schema")
	got, err := media.SelectSerializationDataType(
		context.Background(), reference.Resource{Root: root}, schema,
		media.SerializationTypeOptions{},
	)
	if err != nil || got != media.SerializationDataTypeString {
		t.Fatalf("schema serialization type = %q, %v", got, err)
	}
}

func TestSelectSerializationDataTypeDoesNotInferAmbiguousApplicators(t *testing.T) {
	t.Parallel()

	got, err := media.SelectSerializationDataType(
		context.Background(),
		reference.Resource{Root: mustMediaValue(t, `{}`)},
		mustMediaValue(t, `{
			"anyOf":[{"type":"number"},{"maximum":100}]
		}`),
		media.SerializationTypeOptions{},
	)
	if err != nil || got != media.SerializationDataTypeAny {
		t.Fatalf("ambiguous applicator type = %q, %v", got, err)
	}
}

func TestSelectSerializationDataTypeReportsAmbiguousTypeLists(t *testing.T) {
	t.Parallel()

	_, err := media.SelectSerializationDataType(
		context.Background(),
		reference.Resource{Root: mustMediaValue(t, `{}`)},
		mustMediaValue(t, `{"type":["string","number"]}`),
		media.SerializationTypeOptions{},
	)
	if !errors.Is(err, media.ErrAmbiguousSerializationDataType) {
		t.Fatalf("ambiguous serialization type error = %v", err)
	}
	got, err := media.SelectSerializationDataType(
		context.Background(),
		reference.Resource{Root: mustMediaValue(t, `{}`)},
		mustMediaValue(t, `{"type":["integer","number"]}`),
		media.SerializationTypeOptions{},
	)
	if err != nil || got != media.SerializationDataTypeNumber {
		t.Fatalf("numeric serialization type = %q, %v", got, err)
	}
}

func TestSelectSerializationDataTypeCoversEveryRuntimeAndSchemaType(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		raw  string
		want media.SerializationDataType
	}{
		{name: "null", raw: "null", want: media.SerializationDataTypeNull},
		{name: "boolean", raw: "true", want: media.SerializationDataTypeBoolean},
		{name: "number", raw: "1", want: media.SerializationDataTypeNumber},
		{name: "string", raw: `"text"`, want: media.SerializationDataTypeString},
		{name: "array", raw: `[]`, want: media.SerializationDataTypeArray},
		{name: "object", raw: `{}`, want: media.SerializationDataTypeObject},
	} {
		t.Run("runtime "+test.name, func(t *testing.T) {
			t.Parallel()
			got, err := media.SelectSerializationDataType(
				context.Background(),
				reference.Resource{Root: mustMediaValue(t, `{}`)},
				mustMediaValue(t, `{}`),
				media.SerializationTypeOptions{Data: mustMediaValue(t, test.raw)},
			)
			if err != nil || got != test.want {
				t.Fatalf("runtime type = %q, %v; want %q", got, err, test.want)
			}
		})
		t.Run("schema "+test.name, func(t *testing.T) {
			t.Parallel()
			typeName := test.name
			if typeName == "number" {
				typeName = "integer"
			}
			got, err := media.SelectSerializationDataType(
				context.Background(),
				reference.Resource{Root: mustMediaValue(t, `{}`)},
				mustMediaValue(t, `{"type":"`+typeName+`"}`),
				media.SerializationTypeOptions{},
			)
			if err != nil || got != test.want {
				t.Fatalf("schema type = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	got, err := media.SelectSerializationDataType(
		context.Background(),
		reference.Resource{Root: mustMediaValue(t, `{}`)},
		mustMediaValue(t, `false`), media.SerializationTypeOptions{},
	)
	if err != nil || got != media.SerializationDataTypeAny {
		t.Fatalf("boolean schema type = %q, %v", got, err)
	}
}

func TestSelectSerializationDataTypeResolvesExternalAnchorsAndCycles(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/openapi.json",
		Root:         mustMediaValue(t, `{}`),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root: mustMediaValue(t, `{
				"$defs":{"Flag":{"$anchor":"kind","type":"boolean"}}
			}`),
		}, nil
	})
	got, err := media.SelectSerializationDataType(
		context.Background(), base,
		mustMediaValue(t, `{"$ref":"types.json#kind"}`),
		media.SerializationTypeOptions{Resolver: resolver},
	)
	if err != nil || got != media.SerializationDataTypeBoolean {
		t.Fatalf("external anchor type = %q, %v", got, err)
	}

	root := mustMediaValue(t, `{
		"schema":{"$ref":"#/schema","type":"string"}
	}`)
	schema, _ := root.Lookup("schema")
	got, err = media.SelectSerializationDataType(
		context.Background(), reference.Resource{Root: root}, schema,
		media.SerializationTypeOptions{},
	)
	if err != nil || got != media.SerializationDataTypeString {
		t.Fatalf("cyclic schema type = %q, %v", got, err)
	}
}

func TestSelectSerializationDataTypeKeepsAnchorIdentitiesDistinct(t *testing.T) {
	t.Parallel()

	root := mustMediaValue(t, `{
		"$defs":{
			"Text":{"$anchor":"text","type":"string"},
			"Count":{"$anchor":"count","type":"number"}
		},
		"schema":{"allOf":[{"$ref":"#text"},{"$ref":"#count"}]}
	}`)
	schema, _ := root.Lookup("schema")
	_, err := media.SelectSerializationDataType(
		context.Background(), reference.Resource{Root: root}, schema,
		media.SerializationTypeOptions{},
	)
	if !errors.Is(err, media.ErrInvalidSerializationDataType) {
		t.Fatalf("distinct anchor constraint error = %v", err)
	}
}

func TestSelectSerializationDataTypeAcceptsExactMinimumLimits(t *testing.T) {
	t.Parallel()

	got, err := media.SelectSerializationDataType(
		context.Background(),
		reference.Resource{Root: mustMediaValue(t, `{}`)},
		mustMediaValue(t, `{"type":"string"}`),
		media.SerializationTypeOptions{
			MaxSchemas: 1,
			ReferenceLimits: reference.Limits{
				MaxTraversalDepth: 1,
				MaxTraversalNodes: 1,
				MaxReferenceDepth: 1,
			},
		},
	)
	if err != nil || got != media.SerializationDataTypeString {
		t.Fatalf("exact minimum limits = %q, %v", got, err)
	}
}

func TestSelectSerializationDataTypeRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	validResource := reference.Resource{Root: mustMediaValue(t, `{}`)}
	validSchema := mustMediaValue(t, `{}`)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name     string
		ctx      context.Context
		resource reference.Resource
		schema   jsonvalue.Value
		options  media.SerializationTypeOptions
		want     error
	}{
		{name: "nil context", resource: validResource, schema: validSchema,
			want: media.ErrInvalidSerializationDataType},
		{name: "canceled context", ctx: canceled, resource: validResource,
			schema: validSchema, want: context.Canceled},
		{name: "invalid resource", ctx: context.Background(), schema: validSchema,
			want: media.ErrInvalidSerializationDataType},
		{name: "invalid schema", ctx: context.Background(), resource: validResource,
			schema: mustMediaValue(t, `1`),
			want:   media.ErrInvalidSerializationDataType},
		{name: "negative schema limit", ctx: context.Background(),
			resource: validResource, schema: validSchema,
			options: media.SerializationTypeOptions{MaxSchemas: -1},
			want:    media.ErrInvalidSerializationDataType},
		{name: "invalid reference limits", ctx: context.Background(),
			resource: validResource, schema: validSchema,
			options: media.SerializationTypeOptions{
				ReferenceLimits: reference.Limits{MaxTraversalDepth: 1},
			}, want: media.ErrInvalidSerializationDataType},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := media.SelectSerializationDataType(
				test.ctx, test.resource, test.schema, test.options,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSelectSerializationDataTypeRejectsInvalidSchemaTraversal(t *testing.T) {
	t.Parallel()

	root := mustMediaValue(t, `{
		"notSchema":"text",
		"badRef":{"$ref":1},
		"limited":{"allOf":[{"type":"string"}]}
	}`)
	for _, test := range []struct {
		name    string
		schema  string
		options media.SerializationTypeOptions
		want    error
	}{
		{name: "non-schema target", schema: `{"$ref":"#/notSchema"}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "non-string ref", schema: `{"$ref":1}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "missing ref", schema: `{"$ref":"#/missing"}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "schema limit", schema: `{"allOf":[{"type":"string"}]}`,
			options: media.SerializationTypeOptions{MaxSchemas: 1},
			want:    media.ErrSerializationDataTypeLimit},
		{name: "invalid allOf", schema: `{"allOf":{}}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "invalid allOf child", schema: `{"allOf":[1]}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "invalid type", schema: `{"type":1}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "empty type", schema: `{"type":[]}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "non-string type", schema: `{"type":["string",1]}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "unknown type", schema: `{"type":"bytes"}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "unknown array type", schema: `{"type":["bytes"]}`,
			want: media.ErrInvalidSerializationDataType},
		{name: "incompatible allOf",
			schema: `{"allOf":[{"type":"string"},{"type":"number"}]}`,
			want:   media.ErrInvalidSerializationDataType},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := media.SelectSerializationDataType(
				context.Background(), reference.Resource{Root: root},
				mustMediaValue(t, test.schema), test.options,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSelectSerializationDataTypeHonorsCancellationDuringResolution(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		cancel()
		return reference.Resource{
			RetrievalURI: identifier,
			Root:         mustMediaValue(t, `{"type":"string"}`),
		}, nil
	})
	_, err := media.SelectSerializationDataType(
		ctx,
		reference.Resource{
			RetrievalURI: "https://api.example.test/openapi.json",
			Root:         mustMediaValue(t, `{}`),
		},
		mustMediaValue(t, `{"$ref":"types.json"}`),
		media.SerializationTypeOptions{Resolver: resolver},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}
