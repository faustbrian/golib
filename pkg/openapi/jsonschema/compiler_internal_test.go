package jsonschema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	canonical "github.com/faustbrian/golib/pkg/json-schema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestCompilerRejectsInvalidConfigurationAndInputs(t *testing.T) {
	t.Parallel()

	if _, err := NewCompiler("relative"); !errors.Is(err, ErrUnsupportedDialect) {
		t.Fatalf("relative dialect error = %v", err)
	}
	if _, err := NewCompiler(DialectOAS31, nil); err == nil {
		t.Fatal("nil option was accepted")
	}
	if _, err := NewCompiler(DialectOAS31, WithResourceLoader(nil)); err == nil {
		t.Fatal("nil loader was accepted")
	}
	for _, limits := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		if _, err := NewCompiler(
			DialectOAS31, WithTraversalLimits(limits[0], limits[1]),
		); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("traversal limits %v error = %v", limits, err)
		}
	}
	if _, err := NewCompiler(
		DialectOAS31, WithTraversalLimits(1, 1),
	); err != nil {
		t.Fatalf("exact minimum traversal limits error = %v", err)
	}
	if _, err := NewCompiler(DialectOAS31, WithResourceLoader(staticLoader{})); err != nil {
		t.Fatalf("concrete loader error = %v", err)
	}
	for _, identifier := range []string{
		"relative/schema.json",
		"https://schemas.example.test/root.json#fragment",
		"https://[::1",
	} {
		if _, err := NewCompiler(
			DialectOAS31,
			WithBaseURI(identifier),
		); !errors.Is(err, canonical.ErrInvalidSchema) {
			t.Fatalf("base URI %q error = %v", identifier, err)
		}
	}
	if _, err := NewCompiler(DialectOAS31, WithVocabulary("relative", nil)); !errors.Is(err, canonical.ErrInvalidSchema) {
		t.Fatalf("relative vocabulary error = %v", err)
	}
	var nilKeyword canonical.KeywordCompilerFunc
	if _, err := NewCompiler(DialectOAS31, WithVocabulary(
		"https://schemas.example.test/vocabulary",
		map[string]canonical.KeywordCompiler{"keyword": nilKeyword},
	)); !errors.Is(err, canonical.ErrInvalidSchema) {
		t.Fatalf("nil keyword compiler error = %v", err)
	}
	validKeyword := annotationCompiler{}
	for _, options := range [][]Option{
		{
			WithVocabulary("https://schemas.example.test/one", nil),
			WithVocabulary("https://schemas.example.test/one", nil),
		},
		{
			WithVocabulary(
				"https://schemas.example.test/one",
				map[string]canonical.KeywordCompiler{"keyword": validKeyword},
			),
			WithVocabulary(
				"https://schemas.example.test/two",
				map[string]canonical.KeywordCompiler{"keyword": validKeyword},
			),
		},
		{
			WithVocabulary(
				"https://schemas.example.test/one",
				map[string]canonical.KeywordCompiler{"": validKeyword},
			),
		},
	} {
		if _, err := NewCompiler(DialectOAS31, options...); !errors.Is(err, canonical.ErrInvalidSchema) {
			t.Fatalf("invalid vocabulary registration error = %v", err)
		}
	}
	var nilLoader ResourceLoaderFunc
	if _, err := NewCompiler(
		DialectOAS31, WithResourceLoader(nilLoader),
	); err == nil {
		t.Fatal("typed nil loader was accepted")
	}
	compiler, err := NewCompiler(DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := compiler.Compile(nil, jsonvalue.Null()); err == nil {
		t.Fatal("nil context was accepted")
	}
	var nilCompiler *Compiler
	if _, err := nilCompiler.Compile(context.Background(), jsonvalue.Null()); err == nil {
		t.Fatal("nil compiler was accepted")
	}
	invalidSchema, err := jsonvalue.Object([]jsonvalue.Member{
		{Name: "$schema", Value: jsonvalue.Boolean(true)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(context.Background(), invalidSchema); err == nil {
		t.Fatal("non-string $schema was accepted")
	}
	relativeSchema, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "$schema", Value: mustString(t, "relative")},
	})
	if _, err := compiler.Compile(context.Background(), relativeSchema); !errors.Is(err, ErrUnsupportedDialect) {
		t.Fatalf("relative $schema error = %v", err)
	}
	if _, err := compiler.Compile(context.Background(), jsonvalue.Null()); err == nil {
		t.Fatal("non-schema scalar was accepted")
	}
	if _, err := compiler.Compile(context.Background(), jsonvalue.Value{}); err == nil {
		t.Fatal("invalid semantic value was accepted")
	}
}

func TestCompilerErrorsDoNotExposeIdentifiersOrLoaderDetails(t *testing.T) {
	t.Parallel()

	const secret = "secret-token"
	assertRedactedCompilerError(t, func() error {
		_, err := NewCompiler(Dialect(secret))
		return err
	}())
	assertRedactedCompilerError(t, func() error {
		_, err := NewCompiler(
			DialectOAS31,
			WithBaseURI("https://user:"+secret+"@example.test/schema#fragment"),
		)
		return err
	}())
	assertRedactedCompilerError(t, func() error {
		_, err := NewCompiler(
			DialectOAS31,
			WithVocabulary(secret, nil),
		)
		return err
	}())

	compiler, err := NewCompiler(DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	schema := mustInternalValue(t, `{"$schema":"`+secret+`"}`)
	_, err = compiler.Compile(context.Background(), schema)
	assertRedactedCompilerError(t, err)

	external := Dialect("https://schemas.example.test/" + secret)
	compiler, err = NewCompiler(
		external,
		WithResourceLoader(rawLoader{err: errors.New(secret)}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = compiler.Compile(context.Background(), mustInternalValue(t, `{}`))
	assertRedactedCompilerError(t, err)
}

func assertRedactedCompilerError(t *testing.T, err error) {
	t.Helper()
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unredacted compiler error = %v", err)
	}
}

func TestSchemaChildrenFitExactBudgets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		children int
		visited  int
		queued   int
		depth    int
		want     bool
	}{
		{name: "leaf at depth limit", visited: 6, depth: 3, want: true},
		{name: "exact remaining nodes", children: 2, visited: 2, queued: 2, depth: 2, want: true},
		{name: "node overflow", children: 3, visited: 2, queued: 2, depth: 2},
		{name: "visited exhausted", children: 1, visited: 6, depth: 2},
		{name: "queue exhausted", children: 1, visited: 4, queued: 2, depth: 2},
		{name: "exact depth", children: 1, visited: 1, depth: 3},
	} {
		if got := schemaChildrenFit(
			test.children, test.visited, test.queued, test.depth, 6, 3,
		); got != test.want {
			t.Fatalf("%s fit = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestBoundSchemaValueTracksExactNodesAndDepth(t *testing.T) {
	t.Parallel()

	twoChildren := mustInternalValue(t, `{"first":null,"second":null}`)
	if err := boundSchemaValue(
		context.Background(), twoChildren, 2, 2,
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("node overflow error = %v", err)
	}
	for _, value := range []jsonvalue.Value{
		mustInternalValue(t, `[[null]]`),
		mustInternalValue(t, `{"nested":{"leaf":null}}`),
	} {
		if err := boundSchemaValue(
			context.Background(), value, 3, 1,
		); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("nested depth error = %v", err)
		}
	}
}

func TestCompilerPropagatesPreparationAndTranslationFailures(t *testing.T) {
	value := mustInternalValue(t, `{
		"$schema":"https://schemas.example.test/unavailable"
	}`)
	want := errors.New("dialect unavailable")
	compiler, err := NewCompiler(
		DialectOAS31,
		WithResourceLoader(rawLoader{err: want}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = compiler.ValidateSchema(
		context.Background(), value,
	); !errors.Is(err, want) {
		t.Fatalf("preparation error = %v", err)
	}

	compiler, err = NewCompiler(DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = compiler.engine(context.Background(), DialectOAS31); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = compiler.ValidateSchema(
		canceled, mustInternalValue(t, `{}`),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("validation cancellation error = %v", err)
	}

	compiler = &Compiler{
		defaultDialect: DialectOAS31,
		baseURI:        string([]byte{0xff}),
		engines:        make(map[Dialect]*dialectEngine),
	}
	if _, err = compiler.Compile(
		context.Background(), mustInternalValue(t, `{}`),
	); err == nil {
		t.Fatal("invalid internal base URI reached compilation")
	}
}

func TestDocumentCompilerRejectsUnsupportedDocumentState(t *testing.T) {
	t.Parallel()

	if _, err := NewCompilerForDocument(nil); !errors.Is(err, ErrUnsupportedDialect) {
		t.Fatalf("nil document error = %v", err)
	}
	document := internalDocument{version: specversion.Version{}, raw: jsonvalue.Null()}
	if _, err := NewCompilerForDocument(document); !errors.Is(err, ErrUnsupportedDialect) {
		t.Fatalf("unknown document error = %v", err)
	}
	oas31, _ := specversion.Parse("3.1.2")
	raw, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "jsonSchemaDialect", Value: jsonvalue.Boolean(true)},
	})
	document = internalDocument{version: oas31, raw: raw}
	if _, err := NewCompilerForDocument(document); !errors.Is(err, canonical.ErrInvalidSchema) {
		t.Fatalf("invalid declaration error = %v", err)
	}
	want := errors.New("option failure")
	empty, err := jsonvalue.Object(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCompilerForDocument(
		internalDocument{version: oas31, raw: empty},
		func(*Compiler) error { return want },
	); !errors.Is(err, want) {
		t.Fatalf("option error = %v", err)
	}
}

func TestDocumentSelfSelectionCoversDefensiveURIStates(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		base    string
		self    jsonvalue.Value
		want    string
		include bool
	}{
		{name: "missing"},
		{name: "non-string", self: jsonvalue.Boolean(true), include: true},
		{name: "empty", self: mustString(t, ""), include: true},
		{name: "invalid reference", self: mustString(t, "%zz"), include: true},
		{name: "invalid base", base: "%",
			self: mustString(t, "child"), include: true,
			want: "%"},
		{name: "relative without base", self: mustString(t, "child"), include: true},
		{name: "fragment", base: "https://example.test/root",
			self: mustString(t, "#fragment"), include: true,
			want: "https://example.test/root"},
		{name: "resolved", base: "https://example.test/root/openapi.json",
			self: mustString(t, "../canonical.json"), include: true,
			want: "https://example.test/canonical.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler := &Compiler{baseURI: test.base}
			var members []jsonvalue.Member
			if test.include {
				members = append(members, jsonvalue.Member{Name: "$self", Value: test.self})
			}
			root, err := jsonvalue.Object(members)
			if err != nil {
				t.Fatal(err)
			}
			compiler.applyDocumentSelf(root)
			want := test.want
			if want == "" {
				want = test.base
			}
			if compiler.baseURI != want {
				t.Fatalf("base URI = %q, want %q", compiler.baseURI, want)
			}
		})
	}
}

func TestCompilerRegistersOwnedCustomVocabulary(t *testing.T) {
	t.Parallel()

	const (
		dialect    = "https://schemas.example.test/dialect"
		vocabulary = "https://schemas.example.test/vocabulary"
	)
	keywords := map[string]canonical.KeywordCompiler{"x-note": annotationCompiler{}}
	compiler, err := NewCompiler(
		Dialect(dialect),
		WithResourceLoader(ResourceLoaderFunc(func(context.Context, string) ([]byte, error) {
			return []byte(`{
				"$id":"https://schemas.example.test/dialect",
				"$schema":"https://json-schema.org/draft/2020-12/schema",
				"$vocabulary":{
					"https://json-schema.org/draft/2020-12/vocab/core":true,
					"https://json-schema.org/draft/2020-12/vocab/applicator":true,
					"https://json-schema.org/draft/2020-12/vocab/validation":true,
					"https://schemas.example.test/vocabulary":true
				},
				"type":["object","boolean"]
			}`), nil
		})),
		WithVocabulary(vocabulary, keywords),
	)
	if err != nil {
		t.Fatal(err)
	}
	delete(keywords, "x-note")
	value, err := parse.JSON(
		context.Background(),
		strings.NewReader(`{"x-note":{"kept":true}}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	annotations, err := schema.CollectAnnotations(context.Background(), []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 || annotations[0].KeywordLocation != "/x-note" {
		t.Fatalf("annotations = %#v", annotations)
	}
}

func TestResourceInventoryAndInvalidCanonicalValue(t *testing.T) {
	t.Parallel()

	for _, identifier := range []string{
		string(DialectSwagger20),
		"http://swagger.io/v2/schema.json#",
		"http://json-schema.org/draft-04/schema",
		"http://json-schema.org/draft-04/schema#",
		string(DialectOAS30),
		string(DialectOAS31),
		string(DialectOAS31Snapshot),
		"https://spec.openapis.org/oas/3.1/meta/2024-11-10",
		string(DialectOAS32),
		"https://spec.openapis.org/oas/3.2/meta/2025-09-17",
	} {
		if resource, ok := embeddedResource(identifier); !ok || resource == "" {
			t.Fatalf("embeddedResource(%q) = %q, %t", identifier, resource, ok)
		}
	}
	if resource, ok := embeddedResource("https://example.test/unknown"); ok || resource != "" {
		t.Fatalf("unknown embedded resource = %q, %t", resource, ok)
	}
}

func TestOpenAPI30NullableTransformHandlesNonSchemaValues(t *testing.T) {
	t.Parallel()

	for _, value := range []jsonvalue.Value{jsonvalue.Null(), jsonvalue.Boolean(true)} {
		transformed, err := applyOpenAPI30Nullable(value)
		if err != nil {
			t.Fatal(err)
		}
		if transformed.Kind() != value.Kind() {
			t.Fatalf("transformed kind = %d, want %d", transformed.Kind(), value.Kind())
		}
	}
	reference := mustInternalValue(t, `{"$ref":"#/components/schemas/Value"}`)
	transformed, err := applyOpenAPI30Nullable(reference)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transformed.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"$ref":"#/components/schemas/Value"}` {
		t.Fatalf("transformed reference = %s", raw)
	}
	for _, transform := range []func(jsonvalue.Value) (jsonvalue.Value, error){
		transformOpenAPI30SchemaArray,
		transformOpenAPI30SchemaMap,
	} {
		transformed, err = transform(jsonvalue.Null())
		if err != nil || transformed.Kind() != jsonvalue.NullKind {
			t.Fatalf("non-container transform = %#v, %v", transformed, err)
		}
	}
	if _, err := schemaForCompilation(jsonvalue.Value{}, DialectOAS30, ""); err == nil {
		t.Fatal("invalid semantic value was serialized")
	}
}

func TestLegacySchemaHelpersCoverMalformedAndTypedValues(t *testing.T) {
	t.Parallel()

	if errors := swagger20SchemaErrors(jsonvalue.Null(), ""); len(errors) != 0 {
		t.Fatalf("scalar Swagger errors = %#v", errors)
	}
	if errors := swagger20SchemaErrors(mustInternalValue(t, `{"$ref":"#/x"}`), ""); len(errors) != 0 {
		t.Fatalf("reference Swagger errors = %#v", errors)
	}
	_ = swagger20SchemaErrors(mustInternalValue(t, `{
		"items":true,"additionalProperties":false,"allOf":true,"properties":true
	}`), "")
	if !swagger20DefaultMatches(jsonvalue.Boolean(true), jsonvalue.Null()) {
		t.Fatal("unknown Swagger type representation was rejected")
	}
	if swagger20DefaultMatches(
		mustInternalValue(t, `["integer","string"]`),
		jsonvalue.Boolean(true),
	) {
		t.Fatal("mismatched Swagger union default was accepted")
	}
	if errors := openAPI30SchemaErrors(jsonvalue.Null(), ""); len(errors) != 0 {
		t.Fatalf("scalar OpenAPI errors = %#v", errors)
	}
	if errors := openAPI30SchemaErrors(mustInternalValue(t, `{"$ref":"#/x"}`), ""); len(errors) != 0 {
		t.Fatalf("reference OpenAPI errors = %#v", errors)
	}
	_ = openAPI30SchemaErrors(mustInternalValue(t, `{
		"allOf":true,"oneOf":true,"anyOf":true,"properties":true
	}`), "")

	values := map[string]jsonvalue.Value{
		"array":   mustInternalValue(t, `[]`),
		"boolean": jsonvalue.Boolean(true),
		"integer": mustInternalValue(t, `1`),
		"number":  mustInternalValue(t, `1.5`),
		"null":    jsonvalue.Null(),
		"object":  mustInternalValue(t, `{}`),
		"string":  mustString(t, "value"),
		"unknown": jsonvalue.Boolean(true),
	}
	for typeName, value := range values {
		if !schemaValueMatchesType(typeName, value) {
			t.Errorf("%s did not match %#v", typeName, value)
		}
	}
	if schemaValueMatchesType("integer", mustString(t, "1")) ||
		schemaValueMatchesType("null", jsonvalue.Boolean(false)) {
		t.Fatal("mismatched primitive type was accepted")
	}
}

func TestSchemaCompilationHelpersCoverDialectAndBaseURIStates(t *testing.T) {
	t.Parallel()

	if _, err := withDialect(jsonvalue.Value{}, DialectOAS31); err == nil {
		t.Fatal("invalid value acquired a dialect")
	}
	invalidUTF8 := Dialect(string([]byte{0xff}))
	if _, err := withDialect(mustInternalValue(t, `{}`), invalidUTF8); err == nil {
		t.Fatal("invalid UTF-8 dialect was accepted")
	}
	if _, err := schemaForCompilation(
		mustInternalValue(t, `{}`), invalidUTF8, "",
	); err == nil {
		t.Fatal("invalid UTF-8 compilation dialect was accepted")
	}
	declared := mustInternalValue(t, `{"$schema":"https://example.test"}`)
	if raw, err := withDialect(declared, DialectOAS31); err != nil || len(raw) == 0 {
		t.Fatalf("declared dialect = %s, %v", raw, err)
	}
	for _, test := range []struct {
		value jsonvalue.Value
		base  string
	}{
		{value: jsonvalue.Null(), base: "https://example.test/root"},
		{value: mustInternalValue(t, `{}`)},
		{value: mustInternalValue(t, `{"$id":true}`), base: "https://example.test/root"},
		{value: mustInternalValue(t, `{"$id":"https://other.test/root"}`), base: "https://example.test/root"},
		{value: mustInternalValue(t, `{"$id":"%zz"}`), base: "https://example.test/root"},
	} {
		if _, err := withSchemaBaseURI(test.value, test.base); err != nil {
			t.Fatalf("withSchemaBaseURI(%#v, %q): %v", test.value, test.base, err)
		}
	}
	if _, err := withSchemaBaseURI(
		mustInternalValue(t, `{"$id":"child"}`), ":",
	); err == nil {
		t.Fatal("invalid schema base URI was accepted")
	}
	if _, err := withSchemaBaseURI(
		mustInternalValue(t, `{}`), string([]byte{0xff}),
	); err == nil {
		t.Fatal("invalid UTF-8 schema base URI was accepted")
	}
	if _, err := schemaForCompilation(
		mustInternalValue(t, `{"$id":"child"}`), DialectOAS31, ":",
	); err == nil {
		t.Fatal("invalid compilation base URI was accepted")
	}
}

func TestSchemaValueFactoriesPropagateImmutableConstructionFailures(t *testing.T) {
	want := errors.New("immutable construction failure")
	failString := func(string) (jsonvalue.Value, error) {
		return jsonvalue.Value{}, want
	}
	failArray := func([]jsonvalue.Value) (jsonvalue.Value, error) {
		return jsonvalue.Value{}, want
	}
	failObject := func([]jsonvalue.Member) (jsonvalue.Value, error) {
		return jsonvalue.Value{}, want
	}
	if _, err := schemaForCompilationUsing(
		mustInternalValue(t, `{}`), DialectOAS30, "",
		func(jsonvalue.Value) (jsonvalue.Value, error) {
			return jsonvalue.Value{}, want
		},
	); !errors.Is(err, want) {
		t.Fatalf("nullable translation error = %v", err)
	}

	factory := immutableValueFactory()
	factory.objectValue = failObject
	if _, err := withDialectUsing(
		mustInternalValue(t, `{}`), DialectOAS31, factory,
	); !errors.Is(err, want) {
		t.Fatalf("dialect object error = %v", err)
	}

	factory = immutableValueFactory()
	factory.stringValue = failString
	if _, err := withSchemaBaseURIUsing(
		mustInternalValue(t, `{"$id":"child"}`),
		"https://example.test/root", factory,
	); !errors.Is(err, want) {
		t.Fatalf("base URI string error = %v", err)
	}

	nullable := mustInternalValue(t, `{"type":"string","nullable":true}`)
	if _, err := applyOpenAPI30NullableUsing(
		nullable, factory,
	); !errors.Is(err, want) {
		t.Fatalf("nullable null-name error = %v", err)
	}

	factory = immutableValueFactory()
	stringCalls := 0
	factory.stringValue = func(value string) (jsonvalue.Value, error) {
		stringCalls++
		if stringCalls == 2 {
			return jsonvalue.Value{}, want
		}
		return jsonvalue.String(value)
	}
	if _, err := applyOpenAPI30NullableUsing(
		nullable, factory,
	); !errors.Is(err, want) {
		t.Fatalf("nullable type-name error = %v", err)
	}

	factory = immutableValueFactory()
	factory.arrayValue = failArray
	if _, err := applyOpenAPI30NullableUsing(
		nullable, factory,
	); !errors.Is(err, want) {
		t.Fatalf("nullable type-array error = %v", err)
	}

	factory = immutableValueFactory()
	factory.objectValue = failObject
	if _, err := applyOpenAPI30NullableUsing(
		mustInternalValue(t, `{}`), factory,
	); !errors.Is(err, want) {
		t.Fatalf("nullable object error = %v", err)
	}
	if _, err := applyOpenAPI30NullableUsing(
		mustInternalValue(t, `{"items":{}}`), factory,
	); !errors.Is(err, want) {
		t.Fatalf("nullable child error = %v", err)
	}

	factory = immutableValueFactory()
	factory.arrayValue = failArray
	if _, err := transformOpenAPI30SchemaArrayUsing(
		mustInternalValue(t, `[]`), factory,
	); !errors.Is(err, want) {
		t.Fatalf("schema array error = %v", err)
	}
	factory = immutableValueFactory()
	factory.objectValue = failObject
	if _, err := transformOpenAPI30SchemaArrayUsing(
		mustInternalValue(t, `[{}]`), factory,
	); !errors.Is(err, want) {
		t.Fatalf("schema array child error = %v", err)
	}
	factory = immutableValueFactory()
	factory.objectValue = failObject
	if _, err := transformOpenAPI30SchemaMapUsing(
		mustInternalValue(t, `{}`), factory,
	); !errors.Is(err, want) {
		t.Fatalf("schema map error = %v", err)
	}
	if _, err := transformOpenAPI30SchemaMapUsing(
		mustInternalValue(t, `{"value":{}}`), factory,
	); !errors.Is(err, want) {
		t.Fatalf("schema map child error = %v", err)
	}
}

func TestCanonicalAnnotationPropagatesEncodingFailures(t *testing.T) {
	want := errors.New("annotation encoding failure")
	compiler := annotationCompiler{marshal: func(
		canonical.Value,
	) (json.RawMessage, error) {
		return nil, want
	}}
	if _, err := compiler.Compile(
		context.Background(), canonical.Draft202012, canonical.Value{},
	); !errors.Is(err, want) {
		t.Fatalf("annotation error = %v", err)
	}
	if _, err := marshalCanonicalValueUsing(
		canonical.Value{},
		canonicalAccess{
			kind: func(canonical.Value) canonical.ValueKind { return 255 },
		},
	); err == nil {
		t.Fatal("unsupported canonical value kind was encoded")
	}
	for _, test := range []struct {
		name   string
		access canonicalAccess
	}{
		{
			name: "array child",
			access: canonicalAccess{
				kind:   funcSequenceKind(canonical.ArrayKind, 255),
				length: func(canonical.Value) int { return 1 },
				index: func(canonical.Value, int) (canonical.Value, bool) {
					return canonical.Value{}, true
				},
			},
		},
		{
			name: "object child",
			access: canonicalAccess{
				kind:  funcSequenceKind(canonical.ObjectKind, 255),
				names: func(canonical.Value) []string { return []string{"value"} },
				lookup: func(canonical.Value, string) (canonical.Value, bool) {
					return canonical.Value{}, true
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := marshalCanonicalValueUsing(
				canonical.Value{}, test.access,
			); err == nil {
				t.Fatal("invalid canonical child was encoded")
			}
		})
	}
}

func funcSequenceKind(kinds ...canonical.ValueKind) func(canonical.Value) canonical.ValueKind {
	index := 0
	return func(canonical.Value) canonical.ValueKind {
		kind := kinds[index]
		if index < len(kinds)-1 {
			index++
		}
		return kind
	}
}

func TestCompilerReusesPinnedDialectEngine(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler(DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	firstEngine, firstMeta, err := compiler.engine(context.Background(), DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	secondEngine, secondMeta, err := compiler.engine(context.Background(), DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	if firstEngine != secondEngine || firstMeta != secondMeta {
		t.Fatal("compiler rebuilt a pinned dialect engine")
	}
}

func TestCompilerHandlesExternalDialectEngineOwnership(t *testing.T) {
	const dialect = Dialect("https://schemas.example.test/external-dialect")
	metaSchema := []byte(`{
		"$id":"https://schemas.example.test/external-dialect",
		"$schema":"https://json-schema.org/draft/2020-12/schema",
		"type":["object","boolean"]
	}`)

	want := errors.New("load failure")
	compiler, err := NewCompiler(DialectOAS31, WithResourceLoader(rawLoader{err: want}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = compiler.engine(context.Background(), dialect); !errors.Is(err, want) {
		t.Fatalf("external load error = %v", err)
	}

	loader := &countingLoader{raw: metaSchema}
	compiler, err = NewCompiler(DialectOAS31, WithResourceLoader(loader))
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := compiler.engine(context.Background(), dialect)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := compiler.engine(context.Background(), dialect)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || loader.Count() != 2 {
		t.Fatalf("non-default engine was cached: engines %p/%p, loads %d", first, second, loader.Count())
	}

	compiler, err = NewCompiler(dialect, WithResourceLoader(rawLoader{raw: []byte(`{`)}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = compiler.newEngine(context.Background(), dialect); err == nil {
		t.Fatal("malformed external dialect compiled")
	}
}

func TestCompilerPropagatesOwnedEngineDependencyFailures(t *testing.T) {
	want := errors.New("owned dependency failure")

	compiler, err := NewCompiler(DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	compiler.constructEngine = func(...canonical.Option) (*canonical.Compiler, error) {
		return nil, want
	}
	if _, _, err = compiler.newEngine(
		context.Background(), DialectOAS31,
	); !errors.Is(err, want) {
		t.Fatalf("construct error = %v", err)
	}

	compiler, _ = NewCompiler(DialectOAS31)
	compiler.readResource = func(string) ([]byte, error) { return nil, want }
	if _, _, err = compiler.newEngine(
		context.Background(), DialectOAS31,
	); !errors.Is(err, want) {
		t.Fatalf("read error = %v", err)
	}

	compiler, _ = NewCompiler(DialectOAS31)
	compiler.compileSchema = func(
		*canonical.Compiler, context.Context, []byte,
	) (*canonical.Schema, error) {
		return nil, want
	}
	if _, _, err = compiler.newEngine(
		context.Background(), DialectOAS31,
	); !errors.Is(err, want) {
		t.Fatalf("compile error = %v", err)
	}

	compiler, _ = NewCompiler(DialectOAS31)
	if _, _, err = compiler.engine(context.Background(), DialectOAS31); err != nil {
		t.Fatal(err)
	}
	compiler.validateSchema = func(
		*canonical.Schema,
		context.Context,
		[]byte,
		canonical.OutputFormat,
	) (canonical.OutputUnit, error) {
		return canonical.OutputUnit{}, want
	}
	if _, err = compiler.ValidateSchema(
		context.Background(), mustInternalValue(t, `{}`),
	); !errors.Is(err, want) {
		t.Fatalf("validation error = %v", err)
	}
}

func TestCompilerBuildsOneConcurrentDefaultDialectEngine(t *testing.T) {
	const dialect = Dialect("https://schemas.example.test/concurrent-dialect")
	loader := &blockingLoader{
		raw: []byte(`{
			"$id":"https://schemas.example.test/concurrent-dialect",
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":["object","boolean"]
		}`),
		entered: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	compiler, err := NewCompiler(dialect, WithResourceLoader(loader))
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		engine *canonical.Compiler
		err    error
	}
	results := make(chan result, 2)
	build := func() {
		engine, _, err := compiler.engine(context.Background(), dialect)
		results <- result{engine: engine, err: err}
	}
	go build()
	select {
	case <-loader.entered:
	case <-time.After(time.Second):
		t.Fatal("engine construction did not start")
	}
	go build()
	duplicateWork := false
	select {
	case <-loader.entered:
		duplicateWork = true
	case <-time.After(100 * time.Millisecond):
	}
	close(loader.release)
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent engines failed: %v, %v", first.err, second.err)
	}
	if first.engine != second.engine {
		t.Fatalf("different engines were published: %p, %p", first.engine, second.engine)
	}
	if duplicateWork {
		t.Fatal("concurrent callers built the same dialect engine twice")
	}
}

func TestCompilerRetriesFailedDefaultDialectEngine(t *testing.T) {
	const dialect = Dialect("https://schemas.example.test/retry-dialect")
	loader := &retryLoader{
		raw: []byte(`{
			"$id":"https://schemas.example.test/retry-dialect",
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":["object","boolean"]
		}`),
	}
	compiler, err := NewCompiler(dialect, WithResourceLoader(loader))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := compiler.engine(context.Background(), dialect); err == nil {
		t.Fatal("first dialect load error = nil")
	}
	if _, _, err := compiler.engine(context.Background(), dialect); err != nil {
		t.Fatalf("retried dialect load error = %v", err)
	}
	if loader.Count() != 2 {
		t.Fatalf("dialect load attempts = %d, want 2", loader.Count())
	}
}

func TestCompilerCancelsWhileWaitingForDialectEngine(t *testing.T) {
	const dialect = Dialect("https://schemas.example.test/canceled-dialect")
	loader := &blockingLoader{
		raw: []byte(`{
			"$id":"https://schemas.example.test/canceled-dialect",
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"type":["object","boolean"]
		}`),
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	compiler, err := NewCompiler(dialect, WithResourceLoader(loader))
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() {
		_, _, err := compiler.engine(context.Background(), dialect)
		completed <- err
	}()
	<-loader.entered
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := compiler.engine(canceled, dialect); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting engine error = %v", err)
	}
	close(loader.release)
	if err := <-completed; err != nil {
		t.Fatalf("building engine error = %v", err)
	}
}

type staticLoader struct{}

func (staticLoader) Load(context.Context, string) ([]byte, error) {
	return nil, canonical.ErrResourceUnavailable
}

type rawLoader struct {
	raw []byte
	err error
}

type countingLoader struct {
	raw   []byte
	mu    sync.Mutex
	count int
}

type retryLoader struct {
	raw   []byte
	mu    sync.Mutex
	count int
}

func (loader *retryLoader) Load(context.Context, string) ([]byte, error) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	loader.count++
	if loader.count == 1 {
		return nil, errors.New("transient load failure")
	}
	return loader.raw, nil
}

func (loader *retryLoader) Count() int {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	return loader.count
}

func (loader *countingLoader) Load(context.Context, string) ([]byte, error) {
	loader.mu.Lock()
	loader.count++
	loader.mu.Unlock()
	return loader.raw, nil
}

func (loader *countingLoader) Count() int {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	return loader.count
}

type blockingLoader struct {
	raw     []byte
	entered chan struct{}
	release chan struct{}
}

func (loader *blockingLoader) Load(context.Context, string) ([]byte, error) {
	loader.entered <- struct{}{}
	<-loader.release
	return loader.raw, nil
}

func (loader rawLoader) Load(context.Context, string) ([]byte, error) {
	return loader.raw, loader.err
}

type internalDocument struct {
	version specversion.Version
	raw     jsonvalue.Value
}

func (document internalDocument) Raw() jsonvalue.Value {
	return document.raw
}

func (document internalDocument) SpecificationVersion() specversion.Version {
	return document.version
}

func mustString(t *testing.T, value string) jsonvalue.Value {
	t.Helper()
	result, err := jsonvalue.String(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustInternalValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(),
		strings.NewReader(raw),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCanonicalAnnotationEncodingCoversEveryValueKind(t *testing.T) {
	t.Parallel()

	compiler, err := NewCompiler(DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	value, err := parse.JSON(
		context.Background(),
		strings.NewReader(`{
			"type":"null",
			"example":{"a":true,"z":[null,false,1,"text"]}
		}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	annotations, err := schema.CollectAnnotations(context.Background(), []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 {
		t.Fatalf("annotations = %#v", annotations)
	}
	raw, err := canonicalJSON(annotations[0].Annotation)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte(`{"a":true,"z":[null,false,1,"text"]}`)) {
		t.Fatalf("annotation = %s", raw)
	}
}

func TestResourceLoaderCoversFallbackAndLegacyDialectPaths(t *testing.T) {
	t.Parallel()

	loader := &resourceLoader{}
	if _, err := loader.Load(
		context.Background(), "https://example.test/missing",
	); !errors.Is(err, canonical.ErrResourceUnavailable) {
		t.Fatalf("missing resource error = %v", err)
	}
	want := errors.New("fallback")
	loader.fallback = rawLoader{err: want}
	if _, err := loader.Load(
		context.Background(), "https://example.test/failure",
	); !errors.Is(err, want) {
		t.Fatalf("fallback error = %v", err)
	}
	loader.fallback = rawLoader{raw: []byte(`{"type":"string"}`)}
	raw, err := loader.Load(
		context.Background(), "https://example.test/schema",
	)
	if err != nil || string(raw) != `{"type":"string"}` {
		t.Fatalf("unvalidated fallback = %s, %v", raw, err)
	}

	compiler, err := NewCompiler(DialectSwagger20)
	if err != nil {
		t.Fatal(err)
	}
	_, metaSchema, err := compiler.engine(context.Background(), DialectSwagger20)
	if err != nil {
		t.Fatal(err)
	}
	loader = &resourceLoader{
		fallback:   rawLoader{raw: []byte(`{"type":"string"}`)},
		dialect:    DialectSwagger20,
		metaSchema: metaSchema,
	}
	if raw, err = loader.Load(
		context.Background(), "https://example.test/swagger-schema",
	); err != nil || string(raw) != `{"type":"string"}` {
		t.Fatalf("Swagger fallback = %s, %v", raw, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.prepareLegacyResource(
		canceled, []byte(`{"type":"string"}`),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy validation cancellation error = %v", err)
	}
	want = errors.New("validation failure")
	loader.validate = func(
		*canonical.Schema,
		context.Context,
		[]byte,
		canonical.OutputFormat,
	) (canonical.OutputUnit, error) {
		return canonical.OutputUnit{}, want
	}
	if _, err := loader.prepareLegacyResource(
		context.Background(), []byte(`{"type":"string"}`),
	); !errors.Is(err, want) {
		t.Fatalf("legacy validation error = %v", err)
	}
}

func canonicalJSON(value any) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buffer.Bytes()), nil
}
