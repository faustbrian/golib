package jsonschema_test

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"

	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestCompilerRejectsWideSchemaBeforeCopyingChildren(t *testing.T) {
	members := make([]jsonvalue.Member, 4096)
	for index := range members {
		members[index] = jsonvalue.Member{
			Name: "x-wide-" + strconv.Itoa(index), Value: jsonvalue.Null(),
		}
	}
	wide, _ := jsonvalue.Object(members)
	compiler, err := openapischema.NewCompiler(
		openapischema.DialectOAS30,
		openapischema.WithTraversalLimits(1, 2),
	)
	if err != nil {
		t.Fatal(err)
	}
	const repetitions = 16
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for range repetitions {
		if _, err := compiler.Compile(
			context.Background(), wide,
		); !errors.Is(err, openapischema.ErrLimitExceeded) {
			t.Fatalf("wide schema error = %v", err)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions
	if allocated > 64<<10 {
		t.Fatalf("wide rejected schema allocated %d bytes per operation", allocated)
	}
}

func TestCompilerAppliesOpenAPIDialectAndPreservesAnnotations(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	schema := mustValue(t, `{
		"type":"string",
		"discriminator":{"propertyName":"kind"}
	}`)
	compiled, err := compiler.Compile(context.Background(), schema)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := compiled.Validate(context.Background(), []byte(`"value"`))
	if err != nil {
		t.Fatal(err)
	}
	if !valid.Valid {
		t.Fatal("string instance did not satisfy the schema")
	}
	invalid, err := compiled.Validate(context.Background(), []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Valid {
		t.Fatal("null unexpectedly satisfied a string schema")
	}

	annotations, err := compiled.CollectAnnotations(
		context.Background(),
		[]byte(`"value"`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 ||
		annotations[0].KeywordLocation != "/discriminator" {
		t.Fatalf("unexpected annotations: %#v", annotations)
	}
}

func TestCompilerValidatesVersionSpecificOpenAPIVocabulary(t *testing.T) {
	t.Parallel()

	invalid31 := mustValue(t, `{"discriminator":{}}`)
	compiler31, err := openapischema.NewCompiler(openapischema.DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler31.Compile(context.Background(), invalid31); err == nil {
		t.Fatal("OAS 3.1 discriminator without propertyName was accepted")
	}

	compiler32, err := openapischema.NewCompiler(openapischema.DialectOAS32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler32.Compile(context.Background(), invalid31); err != nil {
		t.Fatalf("OAS 3.2 discriminator without propertyName was rejected: %v", err)
	}
}

func TestCompilerEvaluatesOpenAPI30SchemaObjects(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{
			"type":"number",
			"minimum":1,
			"exclusiveMinimum":true,
			"nullable":true,
			"example":2
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		instance string
		valid    bool
	}{
		{instance: `null`, valid: true},
		{instance: `2`, valid: true},
		{instance: `1`, valid: false},
		{instance: `"2"`, valid: false},
	} {
		result, validateErr := compiled.Validate(
			context.Background(),
			[]byte(test.instance),
		)
		if validateErr != nil {
			t.Fatal(validateErr)
		}
		if result.Valid != test.valid {
			t.Fatalf("instance %s validity = %t", test.instance, result.Valid)
		}
	}
	annotations, err := compiled.CollectAnnotations(
		context.Background(),
		[]byte(`2`),
	)
	if err != nil {
		t.Fatal(err)
	}
	foundNullable := false
	foundExample := false
	for _, annotation := range annotations {
		switch annotation.KeywordLocation {
		case "/nullable":
			foundNullable = true
		case "/example":
			foundExample = true
		}
	}
	if !foundNullable || !foundExample {
		t.Fatalf("OpenAPI 3.0 annotations = %#v", annotations)
	}
}

func TestCompilerAcceptsOpenAPI30SchemaDocumentationFields(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), mustValue(t, `{
		"type":"object",
		"nullable":false,
		"discriminator":{"propertyName":"kind"},
		"readOnly":true,
		"writeOnly":false,
		"xml":{"name":"Root"},
		"externalDocs":{"url":"https://example.test/schema"},
		"example":{"kind":"example"},
		"deprecated":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("documented schema did not validate: %#v", result)
	}
}

func TestCompilerUsesECMAScriptPatternsForOpenAPI30(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"type":"string","pattern":"^(?=open)openapi$"}`),
	)
	if err != nil {
		t.Fatalf("ECMAScript lookahead was rejected: %v", err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`"openapi"`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("ECMAScript pattern did not match")
	}
	if _, err = compiler.Compile(
		context.Background(),
		mustValue(t, `{"type":"string","pattern":"(?"}`),
	); err == nil {
		t.Fatal("invalid ECMAScript pattern was accepted")
	}
}

func TestCompilerIgnoresUnknownFormatsWithoutIgnoringTypes(t *testing.T) {
	t.Parallel()

	for _, dialect := range []openapischema.Dialect{
		openapischema.DialectOAS30,
		openapischema.DialectOAS31,
		openapischema.DialectOAS32,
	} {
		compiler, err := openapischema.NewCompiler(dialect)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := compiler.Compile(
			context.Background(),
			mustValue(t, `{"type":"string","format":"vendor-token"}`),
		)
		if err != nil {
			t.Fatalf("dialect %s rejected an unknown format: %v", dialect, err)
		}

		valid, err := compiled.Validate(context.Background(), []byte(`"value"`))
		if err != nil {
			t.Fatal(err)
		}
		if !valid.Valid {
			t.Fatalf("dialect %s rejected the underlying string type", dialect)
		}

		invalid, err := compiled.Validate(context.Background(), []byte(`1`))
		if err != nil {
			t.Fatal(err)
		}
		if invalid.Valid {
			t.Fatalf("dialect %s ignored the underlying string type", dialect)
		}
	}
}

func TestCompilerTreatsUnknownOASKeywordsAsAnnotations(t *testing.T) {
	t.Parallel()

	for _, dialect := range []openapischema.Dialect{
		openapischema.DialectOAS31,
		openapischema.DialectOAS32,
	} {
		compiler, err := openapischema.NewCompiler(dialect)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := compiler.Compile(
			context.Background(),
			mustValue(t, `{
				"type":"string",
				"vendorKeyword":{"policy":"preserved"}
			}`),
		)
		if err != nil {
			t.Fatalf("dialect %s rejected an unknown keyword: %v", dialect, err)
		}

		valid, err := compiled.Validate(context.Background(), []byte(`"value"`))
		if err != nil {
			t.Fatal(err)
		}
		if !valid.Valid {
			t.Fatalf("dialect %s rejected the underlying string type", dialect)
		}

		invalid, err := compiled.Validate(context.Background(), []byte(`1`))
		if err != nil {
			t.Fatal(err)
		}
		if invalid.Valid {
			t.Fatalf("dialect %s ignored the underlying string type", dialect)
		}
	}
}

func TestCompilerSupportsDynamicReferencesInOASDialects(t *testing.T) {
	t.Parallel()

	for _, dialect := range []openapischema.Dialect{
		openapischema.DialectOAS31,
		openapischema.DialectOAS32,
	} {
		compiler, err := openapischema.NewCompiler(dialect)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := compiler.Compile(
			context.Background(),
			mustValue(t, `{
				"$defs":{"node":{
					"$dynamicAnchor":"node",
					"type":"object",
					"required":["value"],
					"properties":{
						"value":{"type":"string"},
						"next":{"$dynamicRef":"#node"}
					}
				}},
				"$ref":"#/$defs/node"
			}`),
		)
		if err != nil {
			t.Fatalf("dialect %s rejected dynamic references: %v", dialect, err)
		}

		valid, err := compiled.Validate(
			context.Background(),
			[]byte(`{"value":"root","next":{"value":"leaf"}}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		if !valid.Valid {
			t.Fatalf("dialect %s rejected a valid dynamic instance", dialect)
		}

		invalid, err := compiled.Validate(
			context.Background(),
			[]byte(`{"value":"root","next":{"value":1}}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		if invalid.Valid {
			t.Fatalf("dialect %s ignored the dynamic target", dialect)
		}
	}
}

func TestCompilerResolvesRelativeReferencesAgainstExplicitBaseURI(t *testing.T) {
	t.Parallel()

	loaded := ""
	compiler, err := openapischema.NewCompiler(
		openapischema.DialectOAS31,
		openapischema.WithBaseURI(
			"https://api.example.test/schemas/root.json",
		),
		openapischema.WithResourceLoader(openapischema.ResourceLoaderFunc(func(
			_ context.Context,
			identifier string,
		) ([]byte, error) {
			loaded = identifier
			return []byte(`{"type":"string"}`), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"$ref":"child.json"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != "https://api.example.test/schemas/child.json" {
		t.Fatalf("loaded identifier = %q", loaded)
	}
	result, err := compiled.Validate(context.Background(), []byte(`1`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("relative reference did not apply the loaded string schema")
	}
	loaded = ""
	if _, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{
			"$id":"https://schemas.example.test/overridden/root.json",
			"$ref":"child.json"
		}`),
	); err != nil {
		t.Fatal(err)
	}
	if loaded != "https://schemas.example.test/overridden/child.json" {
		t.Fatalf("$id override loaded identifier = %q", loaded)
	}
	loaded = ""
	if _, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"$id":"nested/root.json","$ref":"child.json"}`),
	); err != nil {
		t.Fatal(err)
	}
	if loaded != "https://api.example.test/schemas/nested/child.json" {
		t.Fatalf("relative $id loaded identifier = %q", loaded)
	}
}

func TestDocumentCompilerUsesOpenAPI32SelfAsSchemaBase(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	loaded := ""
	compiler, err := openapischema.NewCompilerForDocument(
		dialectDocument{
			version: version,
			raw: mustValue(t, `{
				"openapi":"3.2.0","$self":"descriptions/root.json"
			}`),
		},
		openapischema.WithBaseURI("https://api.example.test/retrieved.json"),
		openapischema.WithResourceLoader(openapischema.ResourceLoaderFunc(func(
			_ context.Context,
			identifier string,
		) ([]byte, error) {
			loaded = identifier
			return []byte(`{"type":"string"}`), nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"$ref":"child.json"}`),
	); err != nil {
		t.Fatal(err)
	}
	if loaded != "https://api.example.test/descriptions/child.json" {
		t.Fatalf("$self loaded identifier = %q", loaded)
	}
}

func TestCompilerEvaluatesBooleanAndEmptySchemas(t *testing.T) {
	t.Parallel()

	for _, dialect := range []openapischema.Dialect{
		openapischema.DialectOAS31,
		openapischema.DialectOAS32,
	} {
		compiler, err := openapischema.NewCompiler(dialect)
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name   string
			schema string
			valid  bool
		}{
			{name: "true", schema: `true`, valid: true},
			{name: "false", schema: `false`, valid: false},
			{name: "empty object", schema: `{}`, valid: true},
		} {
			t.Run(string(dialect)+"/"+test.name, func(t *testing.T) {
				compiled, compileErr := compiler.Compile(
					context.Background(),
					mustValue(t, test.schema),
				)
				if compileErr != nil {
					t.Fatal(compileErr)
				}
				for _, instance := range []string{
					`null`, `true`, `1`, `"value"`, `[]`, `{}`,
				} {
					result, validateErr := compiled.Validate(
						context.Background(),
						[]byte(instance),
					)
					if validateErr != nil {
						t.Fatal(validateErr)
					}
					if result.Valid != test.valid {
						t.Fatalf("instance %s validity = %t", instance, result.Valid)
					}
				}
			})
		}
	}
}

func TestCompilerEvaluatesOpenAPIAlternativeSchemas(t *testing.T) {
	t.Parallel()

	for _, dialect := range []openapischema.Dialect{
		openapischema.DialectOAS30,
		openapischema.DialectOAS31,
		openapischema.DialectOAS32,
	} {
		compiler, err := openapischema.NewCompiler(dialect)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := compiler.Compile(
			context.Background(),
			mustValue(t, `{"oneOf":[{"type":"string"},{"type":"integer"}]}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			instance string
			valid    bool
		}{
			{instance: `"value"`, valid: true},
			{instance: `1`, valid: true},
			{instance: `true`, valid: false},
		} {
			result, validateErr := compiled.Validate(
				context.Background(), []byte(test.instance),
			)
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			if result.Valid != test.valid {
				t.Fatalf("dialect %s instance %s validity = %t",
					dialect, test.instance, result.Valid)
			}
		}
	}
}

func TestCompilerEvaluatesSwagger20SchemaObjects(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectSwagger20)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{
			"type":"number",
			"minimum":1,
			"exclusiveMinimum":true,
			"example":2
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		instance string
		valid    bool
	}{
		{instance: `2`, valid: true},
		{instance: `1`, valid: false},
		{instance: `"2"`, valid: false},
	} {
		result, validateErr := compiled.Validate(
			context.Background(),
			[]byte(test.instance),
		)
		if validateErr != nil {
			t.Fatal(validateErr)
		}
		if result.Valid != test.valid {
			t.Fatalf("instance %s validity = %t", test.instance, result.Valid)
		}
	}
	annotations, err := compiled.CollectAnnotations(
		context.Background(),
		[]byte(`2`),
	)
	if err != nil {
		t.Fatal(err)
	}
	foundExample := false
	for _, annotation := range annotations {
		if annotation.KeywordLocation == "/example" {
			foundExample = true
		}
	}
	if !foundExample {
		t.Fatalf("Swagger annotations = %#v", annotations)
	}
}

func TestCompilerRejectsInvalidSwagger20SchemaObjects(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectSwagger20)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`true`,
		`{"oneOf":[{"type":"string"}]}`,
		`{"type":"integer","default":1.5}`,
		`{"$schema":"http://json-schema.org/draft-04/schema#"}`,
	} {
		if _, compileErr := compiler.Compile(
			context.Background(),
			mustValue(t, raw),
		); compileErr == nil {
			t.Fatalf("invalid Swagger 2.0 Schema Object %s was accepted", raw)
		}
	}
}

func TestSwagger20DefaultsConformToDeclaredTypes(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectSwagger20)
	if err != nil {
		t.Fatal(err)
	}
	valid := []string{
		`{"type":"array","default":[]}`,
		`{"type":"boolean","default":false}`,
		`{"type":"integer","default":1e3}`,
		`{"type":"number","default":1.5}`,
		`{"type":"object","default":{}}`,
		`{"type":"string","default":"value"}`,
		`{"type":["string","null"],"default":null}`,
	}
	for _, raw := range valid {
		if _, compileErr := compiler.Compile(
			context.Background(),
			mustValue(t, raw),
		); compileErr != nil {
			t.Fatalf("valid default %s was rejected: %v", raw, compileErr)
		}
	}
	invalid := []string{
		`{"type":"array","default":{}}`,
		`{"type":"boolean","default":0}`,
		`{"type":"integer","default":1.5}`,
		`{"type":"number","default":"1"}`,
		`{"type":"object","default":[]}`,
		`{"type":"string","default":1}`,
		`{"type":"null","default":"null"}`,
	}
	for _, raw := range invalid {
		if _, compileErr := compiler.Compile(
			context.Background(),
			mustValue(t, raw),
		); compileErr == nil {
			t.Fatalf("invalid default %s was accepted", raw)
		}
	}
}

func TestValidateSwagger20SchemaReportsNestedDefaultLocations(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectSwagger20)
	if err != nil {
		t.Fatal(err)
	}
	output, err := compiler.ValidateSchema(
		context.Background(),
		mustValue(t, `{
			"allOf":[{"type":"string","default":1}],
			"items":[{"type":"boolean","default":0}],
			"additionalProperties":{"type":"object","default":[]},
			"properties":{"a/b":{"type":"array","default":{}}}
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/allOf/0/default":              false,
		"/items/0/default":              false,
		"/additionalProperties/default": false,
		"/properties/a~1b/default":      false,
	}
	for _, unit := range output.Errors {
		if _, exists := want[unit.InstanceLocation]; exists {
			want[unit.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Fatalf("missing error at %s: %#v", pointer, output.Errors)
		}
	}
}

func TestCompilerRejectsInvalidOpenAPI30SchemaObjects(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`true`,
		`{"type":["string","null"]}`,
		`{"const":"fixed"}`,
		`{"$schema":"http://json-schema.org/draft-04/schema#"}`,
		`{"type":"array"}`,
		`{"type":"integer","default":1.5}`,
	} {
		if _, compileErr := compiler.Compile(
			context.Background(),
			mustValue(t, raw),
		); compileErr == nil {
			t.Fatalf("invalid OpenAPI 3.0 Schema Object %s was accepted", raw)
		}
	}
}

func TestValidateOpenAPI30SchemaReportsProseConstraintLocations(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	output, err := compiler.ValidateSchema(
		context.Background(),
		mustValue(t, `{
			"type":"object",
			"properties":{
				"values":{"type":"array"},
				"count":{"type":"integer","default":1.5}
			}
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/properties/values/items":  false,
		"/properties/count/default": false,
	}
	for _, unit := range output.Errors {
		if _, exists := want[unit.InstanceLocation]; exists {
			want[unit.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Fatalf("missing error at %s: %#v", pointer, output.Errors)
		}
	}
}

func TestOpenAPI30NullableRetainsOtherConstraints(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"type":"string","nullable":true,"enum":["kept"]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("nullable bypassed the sibling enum constraint")
	}
}

func TestOpenAPI30DefaultsConformToDeclaredTypes(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	valid := []string{
		`{"type":"array","items":{},"default":[]}`,
		`{"type":"boolean","default":false}`,
		`{"type":"integer","default":1e3}`,
		`{"type":"number","default":1.5}`,
		`{"type":"object","default":{}}`,
		`{"type":"string","default":"value"}`,
		`{"type":"string","nullable":true,"default":null}`,
	}
	for _, raw := range valid {
		if _, compileErr := compiler.Compile(
			context.Background(),
			mustValue(t, raw),
		); compileErr != nil {
			t.Fatalf("valid default %s was rejected: %v", raw, compileErr)
		}
	}
	invalid := []string{
		`{"type":"array","items":{},"default":{}}`,
		`{"type":"boolean","default":0}`,
		`{"type":"integer","default":1.5}`,
		`{"type":"number","default":"1"}`,
		`{"type":"object","default":[]}`,
		`{"type":"string","default":1}`,
		`{"type":"string","default":null}`,
	}
	for _, raw := range invalid {
		if _, compileErr := compiler.Compile(
			context.Background(),
			mustValue(t, raw),
		); compileErr == nil {
			t.Fatalf("invalid default %s was accepted", raw)
		}
	}
}

func TestOpenAPI30NullableAppliesAtNestedSchemaLocations(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS30)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{
			"type":"object",
			"properties":{
				"direct":{"type":"string","nullable":true},
				"all":{"allOf":[{"type":"string","nullable":true}]},
				"any":{"anyOf":[{"type":"string","nullable":true}]},
				"one":{"oneOf":[{"type":"string","nullable":true}]},
				"array":{
					"type":"array",
					"items":{"type":"string","nullable":true}
				},
				"negated":{"not":{"type":"string","nullable":true}},
				"map":{
					"type":"object",
					"additionalProperties":{"type":"string","nullable":true}
				}
			},
			"x-uninterpreted":{"type":"string","nullable":true}
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`{
		"direct":null,
		"all":null,
		"any":null,
		"one":null,
		"array":[null],
		"negated":2,
		"map":{"additional":null}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("nested nullable instance was rejected")
	}
}

func TestValidateSchemaReturnsStandardOutputLocations(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	output, err := compiler.ValidateSchema(
		context.Background(),
		mustValue(t, `{"discriminator":{}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.Valid || len(output.Errors) == 0 {
		t.Fatalf("invalid Schema Object output = %#v", output)
	}
	found := false
	for _, unit := range output.Errors {
		if unit.InstanceLocation == "/discriminator" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing discriminator instance location: %#v", output.Errors)
	}
}

func TestSchemaDeclarationOverridesCompilerDefault(t *testing.T) {
	t.Parallel()

	compiler, err := openapischema.NewCompiler(openapischema.DialectOAS32)
	if err != nil {
		t.Fatal(err)
	}
	schema := mustValue(t, `{
		"$schema":"https://spec.openapis.org/oas/3.1/dialect/2024-11-10",
		"discriminator":{}
	}`)
	if _, err := compiler.Compile(context.Background(), schema); err == nil {
		t.Fatal("per-schema OAS 3.1 declaration did not override OAS 3.2")
	}
}

func TestDocumentCompilerUsesNormativeBaseDialect(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	document := dialectDocument{
		version: version,
		raw:     mustValue(t, `{"openapi":"3.2.0"}`),
	}
	compiler, err := openapischema.NewCompilerForDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"xml":{"nodeType":"element"}}`),
	); err != nil {
		t.Fatalf("OAS 3.2 document did not default to its normative dialect: %v", err)
	}

	document.raw = mustValue(t, `{
		"openapi":"3.2.0",
		"jsonSchemaDialect":"https://spec.openapis.org/oas/3.2/dialect/2025-09-17"
	}`)
	compiler, err = openapischema.NewCompilerForDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"xml":{"nodeType":"element"}}`),
	); err != nil {
		t.Fatalf("explicit OAS 3.2 dialect was not honored: %v", err)
	}
}

func TestDocumentCompilerSelectsOpenAPI30SchemaSubset(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("3.0.4")
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := openapischema.NewCompilerForDocument(dialectDocument{
		version: version,
		raw:     mustValue(t, `{"openapi":"3.0.4"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"type":"integer","nullable":true}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("OpenAPI 3.0 document compiler did not apply nullable")
	}
}

func TestDocumentCompilerSelectsSwagger20SchemaSubset(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("2.0")
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := openapischema.NewCompilerForDocument(dialectDocument{
		version: version,
		raw:     mustValue(t, `{"swagger":"2.0"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"type":"string","minLength":2}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`"x"`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("Swagger document compiler ignored the string constraint")
	}
}

func TestDocumentCompilerUsesExplicitLoadedDialect(t *testing.T) {
	t.Parallel()

	const identifier = "https://schemas.example.test/openapi-dialect"
	version, err := specversion.Parse("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	document := dialectDocument{
		version: version,
		raw: mustValue(t, `{
			"openapi":"3.1.2",
			"jsonSchemaDialect":"https://schemas.example.test/openapi-dialect"
		}`),
	}
	loader := openapischema.ResourceLoaderFunc(func(
		_ context.Context,
		requested string,
	) ([]byte, error) {
		if requested != identifier {
			t.Fatalf("unexpected resource request %q", requested)
		}
		return []byte(`{
			"$id":"https://schemas.example.test/openapi-dialect",
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"$vocabulary":{
				"https://json-schema.org/draft/2020-12/vocab/core":true,
				"https://json-schema.org/draft/2020-12/vocab/validation":true
			},
			"type":["object","boolean"]
		}`), nil
	})
	compiler, err := openapischema.NewCompilerForDocument(
		document,
		openapischema.WithResourceLoader(loader),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(
		context.Background(),
		mustValue(t, `{"type":"string"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`1`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("custom dialect did not retain its validation vocabulary")
	}
}

func TestLegacyCompilersApplyRulesToLoadedSchemaResources(t *testing.T) {
	t.Parallel()

	loader := openapischema.ResourceLoaderFunc(func(
		_ context.Context,
		identifier string,
	) ([]byte, error) {
		switch identifier {
		case "https://schemas.example.test/oas30":
			return []byte(`{"type":"string","nullable":true}`), nil
		case "https://schemas.example.test/swagger":
			return []byte(`{"oneOf":[{"type":"string"}]}`), nil
		case "https://schemas.example.test/malformed":
			return []byte(`{`), nil
		case "https://schemas.example.test/oas30-semantic":
			return []byte(`{"type":"array"}`), nil
		default:
			return nil, errors.New("unexpected schema resource")
		}
	})
	oas30, err := openapischema.NewCompiler(
		openapischema.DialectOAS30,
		openapischema.WithResourceLoader(loader),
	)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := oas30.Compile(
		context.Background(),
		mustValue(t, `{"allOf":[{"$ref":"https://schemas.example.test/oas30"}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiled.Validate(context.Background(), []byte(`null`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatal("loaded OpenAPI 3.0 nullable schema rejected null")
	}

	swagger, err := openapischema.NewCompiler(
		openapischema.DialectSwagger20,
		openapischema.WithResourceLoader(loader),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := swagger.Compile(
		context.Background(),
		mustValue(t, `{"$ref":"https://schemas.example.test/swagger"}`),
	); err == nil {
		t.Fatal("loaded Swagger schema bypassed the Schema Object subset")
	}
	for _, identifier := range []string{
		"https://schemas.example.test/malformed",
		"https://schemas.example.test/oas30-semantic",
	} {
		if _, err := oas30.Compile(
			context.Background(),
			mustValue(t, `{"allOf":[{"$ref":"`+identifier+`"}]}`),
		); err == nil {
			t.Fatalf("invalid loaded schema %s was accepted", identifier)
		}
	}
}

type dialectDocument struct {
	version specversion.Version
	raw     jsonvalue.Value
}

func (document dialectDocument) Raw() jsonvalue.Value {
	return document.raw
}

func (document dialectDocument) SpecificationVersion() specversion.Version {
	return document.version
}

func mustValue(t *testing.T, raw string) jsonvalue.Value {
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
