package jsonschema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	jsonschema "github.com/faustbrian/golib/pkg/json-schema"
)

func TestBasicOutputPreservesReferenceEvaluationPath(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$id":"https://example.com/polygon",
		"$defs":{"point":{
			"type":"object",
			"properties":{"x":{"type":"number"},"y":{"type":"number"}},
			"additionalProperties":false,
			"required":["x","y"]
		}},
		"type":"array",
		"items":{"$ref":"#/$defs/point"},
		"minItems":3
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(context.Background(), []byte(`[
		{"x":2.5,"y":1.3},
		{"x":1,"z":6.7}
	]`), jsonschema.OutputBasic)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		keyword  string
		absolute string
		instance string
	}{
		{keyword: "", instance: ""},
		{
			keyword:  "/items/$ref",
			absolute: "https://example.com/polygon#/$defs/point",
			instance: "/1",
		},
		{
			keyword:  "/items/$ref/required",
			absolute: "https://example.com/polygon#/$defs/point/required",
			instance: "/1",
		},
		{
			keyword:  "/items/$ref/additionalProperties",
			absolute: "https://example.com/polygon#/$defs/point/additionalProperties",
			instance: "/1/z",
		},
		{keyword: "/minItems", instance: ""},
	}
	if len(output.Errors) != len(want) {
		t.Fatalf("got %#v, want %d errors", output.Errors, len(want))
	}
	for index, expected := range want {
		actual := output.Errors[index]
		if actual.KeywordLocation != expected.keyword ||
			actual.AbsoluteKeywordLocation != expected.absolute ||
			actual.InstanceLocation != expected.instance {
			t.Errorf("error %d: got %#v, want %#v", index, actual, expected)
		}
	}
	detailed, err := schema.ValidateOutput(
		context.Background(),
		[]byte(`[{"x":2.5,"y":1.3},{"x":1,"z":6.7}]`),
		jsonschema.OutputDetailed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(detailed.Errors) != 2 ||
		detailed.Errors[0].KeywordLocation != "/items/$ref" ||
		len(detailed.Errors[0].Errors) != 2 ||
		detailed.Errors[1].KeywordLocation != "/minItems" {
		t.Fatalf("unexpected detailed hierarchy %#v", detailed.Errors)
	}
}

func TestVerboseOutputIncludesEveryEvaluatedKeyword(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$schema":"https://json-schema.org/draft/2020-12/schema",
		"type":"object",
		"properties":{"validProp":true},
		"additionalProperties":false
	}`))
	if err != nil {
		t.Fatal(err)
	}

	output, err := schema.ValidateOutput(
		context.Background(),
		[]byte(`{"validProp":5,"disallowedProp":"value"}`),
		jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.Valid {
		t.Fatal("verbose output unexpectedly reported a valid instance")
	}
	if len(output.Errors) != 3 {
		t.Fatalf("unexpected verbose hierarchy %#v", output.Errors)
	}
	assertOutputUnit := func(index int, valid bool, keyword string) {
		t.Helper()
		unit := output.Errors[index]
		if unit.Valid != valid || unit.KeywordLocation != keyword ||
			unit.InstanceLocation != "" {
			t.Fatalf("unit %d: got %#v", index, unit)
		}
	}
	assertOutputUnit(0, false, "/additionalProperties")
	assertOutputUnit(1, true, "/properties")
	assertOutputUnit(2, true, "/type")
	additional := output.Errors[0]
	if len(additional.Errors) != 1 ||
		additional.Errors[0].Valid ||
		additional.Errors[0].KeywordLocation != "/additionalProperties" ||
		additional.Errors[0].InstanceLocation != "/disallowedProp" {
		t.Fatalf("unexpected additional-property result %#v", additional)
	}
}

func TestVerboseOutputIncludesSuccessfulAndFailedReferenceApplications(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$id":"https://example.test/list",
		"$defs":{"item":{"type":"integer"}},
		"type":"array",
		"items":{"$ref":"#/$defs/item"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(
		context.Background(), []byte(`[1,"no"]`), jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}

	var items *jsonschema.OutputUnit
	for index := range output.Errors {
		if output.Errors[index].KeywordLocation == "/items" {
			items = &output.Errors[index]
			break
		}
	}
	if items == nil || len(items.Errors) != 2 {
		t.Fatalf("unexpected items result %#v", items)
	}
	for index, valid := range []bool{true, false} {
		unit := items.Errors[index]
		if unit.Valid != valid || unit.KeywordLocation != "/items/$ref" ||
			unit.InstanceLocation != "/"+fmt.Sprint(index) ||
			len(unit.Errors)+len(unit.Annotations) != 1 {
			t.Fatalf("item %d: unexpected result %#v", index, unit)
		}
		children := unit.Annotations
		if !valid {
			children = unit.Errors
		}
		target := children[0]
		if target.Valid != valid ||
			target.AbsoluteKeywordLocation != "https://example.test/list#/$defs/item" {
			t.Fatalf("item %d: unexpected target %#v", index, target)
		}
	}
}

func TestVerboseOutputRetainsEveryApplicatorBranch(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"properties":{"x":{"type":"integer","minimum":0}},
		"anyOf":[{"type":"string"},{"type":"object"}],
		"not":{"required":["blocked"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(
		context.Background(), []byte(`{"x":-1}`), jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}
	if output.Valid {
		t.Fatal("verbose output unexpectedly reported a valid instance")
	}

	byKeyword := make(map[string]jsonschema.OutputUnit)
	for _, unit := range output.Errors {
		byKeyword[unit.KeywordLocation] = unit
	}
	properties := byKeyword["/properties"]
	if properties.Valid || len(properties.Errors) != 2 ||
		properties.Errors[0].Valid || properties.Errors[0].KeywordLocation != "/properties/x/minimum" ||
		!properties.Errors[1].Valid || properties.Errors[1].KeywordLocation != "/properties/x/type" {
		t.Fatalf("unexpected properties result %#v", properties)
	}
	anyOf := byKeyword["/anyOf"]
	if !anyOf.Valid || len(anyOf.Annotations) != 2 ||
		anyOf.Annotations[0].Valid || !anyOf.Annotations[1].Valid {
		t.Fatalf("unexpected anyOf result %#v", anyOf)
	}
	not := byKeyword["/not"]
	if !not.Valid || len(not.Annotations) != 1 || not.Annotations[0].Valid {
		t.Fatalf("unexpected not result %#v", not)
	}
}

func TestVerboseOutputExpandsArrayApplicators(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"prefixItems":[{"type":"integer"}],
		"items":{"type":"string"},
		"contains":{"const":"match"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(
		context.Background(), []byte(`[1,"match",2]`), jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}
	byKeyword := make(map[string]jsonschema.OutputUnit)
	for _, unit := range output.Errors {
		byKeyword[unit.KeywordLocation] = unit
	}
	prefix := byKeyword["/prefixItems"]
	if !prefix.Valid || len(prefix.Annotations) != 0 {
		t.Fatalf("unexpected prefixItems result %#v", prefix)
	}
	items := byKeyword["/items"]
	if items.Valid || len(items.Errors) != 2 ||
		!items.Errors[0].Valid || items.Errors[1].Valid {
		t.Fatalf("unexpected items result %#v", items)
	}
	contains := byKeyword["/contains"]
	if !contains.Valid || len(contains.Annotations) != 0 {
		t.Fatalf("unexpected contains result %#v", contains)
	}
}

func TestVerboseOutputExpandsObjectApplicators(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"properties":{"known":{"type":"integer"}},
		"patternProperties":{"^s":{"type":"string"}},
		"additionalProperties":{"type":"boolean"},
		"propertyNames":{"minLength":2},
		"dependentSchemas":{"known":{"required":["other"]}},
		"unevaluatedProperties":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(
		context.Background(),
		[]byte(`{"known":1,"s1":"ok","x":true}`),
		jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}
	byKeyword := make(map[string]jsonschema.OutputUnit)
	for _, unit := range output.Errors {
		byKeyword[unit.KeywordLocation] = unit
	}
	for _, keyword := range []string{
		"/additionalProperties", "/patternProperties", "/properties",
		"/unevaluatedProperties",
	} {
		if !byKeyword[keyword].Valid {
			t.Fatalf("%s: unexpected result %#v", keyword, byKeyword[keyword])
		}
	}
	propertyNames := byKeyword["/propertyNames"]
	if propertyNames.Valid || len(propertyNames.Errors) != 3 ||
		!propertyNames.Errors[0].Valid || !propertyNames.Errors[1].Valid ||
		propertyNames.Errors[2].Valid {
		t.Fatalf("unexpected propertyNames result %#v", propertyNames)
	}
	dependent := byKeyword["/dependentSchemas"]
	if dependent.Valid || len(dependent.Errors) != 1 ||
		dependent.Errors[0].KeywordLocation != "/dependentSchemas/known/required" {
		t.Fatalf("unexpected dependentSchemas result %#v", dependent)
	}
}

func TestVerboseOutputOmitsInactiveConditionalBranches(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		schema   string
		instance string
		want     []string
	}{
		{name: "no condition", schema: `{"then":false,"else":false}`, instance: `null`},
		{
			name:     "then",
			schema:   `{"if":{"type":"integer"},"then":{"minimum":1},"else":false}`,
			instance: `2`,
			want:     []string{"/if", "/then"},
		},
		{
			name:     "else",
			schema:   `{"if":{"type":"integer"},"then":false,"else":{"minLength":1}}`,
			instance: `"x"`,
			want:     []string{"/else", "/if"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiler, err := jsonschema.NewCompiler()
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(context.Background(), []byte(test.schema))
			if err != nil {
				t.Fatal(err)
			}
			output, err := schema.ValidateOutput(
				context.Background(), []byte(test.instance), jsonschema.OutputVerbose,
			)
			if err != nil {
				t.Fatal(err)
			}
			actual := make([]string, 0, len(output.Annotations))
			for _, unit := range output.Annotations {
				actual = append(actual, unit.KeywordLocation)
			}
			if !slices.Equal(actual, test.want) {
				t.Fatalf("got %v, want %v", actual, test.want)
			}
		})
	}
}

func TestVerboseOutputExpandsFailedApplicatorFamilies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		dialect  jsonschema.Dialect
		schema   string
		instance string
		keyword  string
	}{
		{
			name: "pattern properties", schema: `{"patternProperties":{"^s":{"type":"integer"}}}`,
			instance: `{"s":"no"}`, keyword: "/patternProperties",
		},
		{
			name:     "additional properties",
			schema:   `{"properties":{"fixed":true},"patternProperties":{"^s":true},"additionalProperties":{"type":"string"}}`,
			instance: `{"fixed":1,"s1":2,"extra":3}`, keyword: "/additionalProperties",
		},
		{
			name: "unevaluated properties", schema: `{"unevaluatedProperties":false}`,
			instance: `{"x":1}`, keyword: "/unevaluatedProperties",
		},
		{
			name: "prefix items", schema: `{"prefixItems":[{"type":"string"}]}`,
			instance: `[1]`, keyword: "/prefixItems",
		},
		{
			name: "contains", schema: `{"contains":{"const":"match"}}`,
			instance: `[1,2]`, keyword: "/contains",
		},
		{
			name: "unevaluated items", schema: `{"unevaluatedItems":false}`,
			instance: `[1]`, keyword: "/unevaluatedItems",
		},
		{
			name: "tuple items", dialect: jsonschema.Draft7,
			schema:   `{"items":[{"type":"string"}]}`,
			instance: `[1]`, keyword: "/items",
		},
		{
			name: "legacy homogeneous items", dialect: jsonschema.Draft7,
			schema:   `{"items":{"type":"string"}}`,
			instance: `[1]`, keyword: "/items",
		},
		{
			name: "additional items", dialect: jsonschema.Draft7,
			schema:   `{"items":[true],"additionalItems":{"type":"string"}}`,
			instance: `[null,1]`, keyword: "/additionalItems",
		},
		{
			name: "all of", schema: `{"allOf":[{"type":"string"},{"minLength":2}]}`,
			instance: `1`, keyword: "/allOf",
		},
		{
			name: "one of", schema: `{"oneOf":[true,true]}`,
			instance: `null`, keyword: "/oneOf",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := make([]jsonschema.Option, 0, 1)
			if test.dialect != "" {
				options = append(options, jsonschema.WithDialect(test.dialect))
			}
			compiler, err := jsonschema.NewCompiler(options...)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(context.Background(), []byte(test.schema))
			if err != nil {
				t.Fatal(err)
			}
			output, err := schema.ValidateOutput(
				context.Background(), []byte(test.instance), jsonschema.OutputVerbose,
			)
			if err != nil {
				t.Fatal(err)
			}
			for _, unit := range output.Errors {
				if unit.KeywordLocation == test.keyword {
					if unit.Valid || len(unit.Errors) == 0 {
						t.Fatalf("unexpected result %#v", unit)
					}
					return
				}
			}
			t.Fatalf("missing %s in %#v", test.keyword, output.Errors)
		})
	}
}

func TestVerboseOutputRetainsAnnotationResults(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"title":"root",
		"contentMediaType":"application/json",
		"properties":{"x":{"title":"child"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(
		context.Background(), []byte(`{"x":null}`), jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Annotations) != 2 ||
		output.Annotations[0].KeywordLocation != "/properties" ||
		len(output.Annotations[0].Annotations) != 1 ||
		output.Annotations[1].KeywordLocation != "/title" ||
		output.Annotations[1].Annotation != "root" {
		t.Fatalf("unexpected annotation hierarchy %#v", output.Annotations)
	}

	invalid, err := compiler.Compile(context.Background(), []byte(`{
		"title":"retained for verbose diagnostics",
		"type":"string"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err = invalid.ValidateOutput(
		context.Background(), []byte(`1`), jsonschema.OutputVerbose,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Errors) != 2 || output.Errors[0].Annotation == nil {
		t.Fatalf("failed schema annotation was not retained %#v", output.Errors)
	}
}

func TestBasicOutputIdentifiesFailedValidationKeywords(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		schema   string
		instance string
		options  []jsonschema.Option
		want     []string
	}{
		{
			name:     "exact values",
			schema:   `{"enum":[1,2],"const":2}`,
			instance: `3`,
			want:     []string{"", "/const", "/enum"},
		},
		{
			name:     "numbers",
			schema:   `{"minimum":5,"maximum":1,"multipleOf":2}`,
			instance: `3`,
			want:     []string{"", "/maximum", "/minimum", "/multipleOf"},
		},
		{
			name:     "strings",
			schema:   `{"minLength":5,"maxLength":1,"pattern":"^z","format":"email"}`,
			instance: `"ab"`,
			options:  []jsonschema.Option{jsonschema.WithFormatAssertion()},
			want:     []string{"", "/format", "/maxLength", "/minLength", "/pattern"},
		},
		{
			name:     "arrays",
			schema:   `{"minItems":3,"maxItems":1,"uniqueItems":true,"contains":{"const":9}}`,
			instance: `[1,1]`,
			want:     []string{"", "/contains", "/maxItems", "/minItems", "/uniqueItems"},
		},
		{
			name: "objects",
			schema: `{
				"minProperties":3,"maxProperties":1,"required":["missing"],
				"dependentRequired":{"a":["dependency"]},
				"propertyNames":{"pattern":"^[A-Z]"}
			}`,
			instance: `{"a":1,"bad":2}`,
			want: []string{
				"", "/dependentRequired", "/maxProperties", "/minProperties",
				"/propertyNames/pattern", "/required",
			},
		},
		{
			name: "applicators",
			schema: `{
				"allOf":[{"type":"string"}],
				"anyOf":[{"type":"boolean"}],
				"oneOf":[true,true],
				"not":{}
			}`,
			instance: `1`,
			want: []string{
				"", "/allOf", "/allOf/0/type", "/anyOf", "/anyOf/0/type",
				"/not", "/oneOf",
			},
		},
		{
			name:     "draft 3 disallow",
			schema:   `{"disallow":["string",{"minimum":5}]}`,
			instance: `6`,
			options:  []jsonschema.Option{jsonschema.WithDialect(jsonschema.Draft3)},
			want:     []string{"", "/disallow"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, err := jsonschema.NewCompiler(test.options...)
			if err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile(context.Background(), []byte(test.schema))
			if err != nil {
				t.Fatal(err)
			}
			output, err := schema.ValidateOutput(
				context.Background(), []byte(test.instance), jsonschema.OutputBasic,
			)
			if err != nil {
				t.Fatal(err)
			}
			locations := make([]string, 0, len(output.Errors))
			for _, unit := range output.Errors {
				if !slices.Contains(locations, unit.KeywordLocation) {
					locations = append(locations, unit.KeywordLocation)
				}
			}
			slices.Sort(locations)
			if !slices.Equal(locations, test.want) {
				t.Fatalf("got locations %#v, want %#v", locations, test.want)
			}
		})
	}
}

func TestBasicAnnotationsPreserveReferenceEvaluationPath(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$id":"https://schemas.example.test/root",
		"$defs":{"value":{"title":"Referenced value"}},
		"properties":{"x":{"$ref":"#/$defs/value"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(
		context.Background(), []byte(`{"x":1}`), jsonschema.OutputBasic,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Annotations) != 1 {
		t.Fatalf("unexpected annotations %#v", output.Annotations)
	}
	annotation := output.Annotations[0]
	if annotation.KeywordLocation != "/properties/x/$ref/title" ||
		annotation.AbsoluteKeywordLocation !=
			"https://schemas.example.test/root#/$defs/value/title" ||
		annotation.InstanceLocation != "/x" ||
		annotation.Annotation != "Referenced value" {
		t.Fatalf("unexpected annotation %#v", annotation)
	}
}

func TestBasicOutputPreservesDynamicReferenceEvaluationPath(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), []byte(`{
		"$id":"https://schemas.example.test/root",
		"$defs":{"value":{"$dynamicAnchor":"value","type":"string"}},
		"properties":{"x":{"$dynamicRef":"#value"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	output, err := schema.ValidateOutput(
		context.Background(), []byte(`{"x":1}`), jsonschema.OutputBasic,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Errors) != 3 ||
		output.Errors[1].KeywordLocation != "/properties/x/$dynamicRef" ||
		output.Errors[2].KeywordLocation != "/properties/x/$dynamicRef/type" {
		t.Fatalf("unexpected errors %#v", output.Errors)
	}
}

func TestBasicOutputIncludesUnevaluatedKeywordFailures(t *testing.T) {
	t.Parallel()

	compiler, err := jsonschema.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		schema   string
		instance string
		want     string
	}{
		{
			name:     "properties",
			schema:   `{"properties":{"known":true},"unevaluatedProperties":false}`,
			instance: `{"known":1,"extra":2}`,
			want:     "/unevaluatedProperties",
		},
		{
			name:     "items",
			schema:   `{"prefixItems":[true],"unevaluatedItems":false}`,
			instance: `[1,2]`,
			want:     "/unevaluatedItems",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema, err := compiler.Compile(context.Background(), []byte(test.schema))
			if err != nil {
				t.Fatal(err)
			}
			output, err := schema.ValidateOutput(
				context.Background(), []byte(test.instance), jsonschema.OutputBasic,
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(output.Errors) != 2 || output.Errors[1].KeywordLocation != test.want {
				t.Fatalf("unexpected errors %#v", output.Errors)
			}
		})
	}
}

type outputFixtureGroup struct {
	Description string              `json:"description"`
	Schema      json.RawMessage     `json:"schema"`
	Tests       []outputFixtureCase `json:"tests"`
}

type outputFixtureCase struct {
	Description string                     `json:"description"`
	Data        json.RawMessage            `json:"data"`
	Output      map[string]json.RawMessage `json:"output"`
}

func TestOfficialBasicOutputFixtures(t *testing.T) {
	t.Parallel()

	for _, fixture := range []struct {
		directory string
		dialect   jsonschema.Dialect
	}{
		{directory: "draft2019-09", dialect: jsonschema.Draft201909},
		{directory: "draft2020-12", dialect: jsonschema.Draft202012},
	} {
		fixture := fixture
		for _, filename := range []string{
			"escape.json",
			"general.json",
			"readOnly.json",
			"type.json",
		} {
			filename := filename
			t.Run(fixture.directory+"/"+filename, func(t *testing.T) {
				t.Parallel()
				runOfficialOutputFixture(t, fixture.directory, filename, fixture.dialect)
			})
		}
	}
}

func runOfficialOutputFixture(
	t *testing.T,
	directory string,
	filename string,
	dialect jsonschema.Dialect,
) {
	t.Helper()
	// #nosec G304 -- arguments come from a fixed official fixture table.
	raw, err := os.ReadFile(filepath.Join(
		"testdata",
		"official",
		"JSON-Schema-Test-Suite",
		"output-tests",
		directory,
		"content",
		filename,
	))
	if err != nil {
		t.Fatal(err)
	}
	var groups []outputFixtureGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}

	compiler, err := jsonschema.NewCompiler(jsonschema.WithDialect(dialect))
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		schema, err := compiler.Compile(context.Background(), group.Schema)
		if err != nil {
			t.Fatalf("%s: compile schema: %v", group.Description, err)
		}
		for _, test := range group.Tests {
			output, err := schema.ValidateOutput(
				context.Background(),
				test.Data,
				jsonschema.OutputBasic,
			)
			if err != nil {
				t.Fatalf("%s: output: %v", test.Description, err)
			}
			encoded, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			assertOutputMatchesOfficialSchema(
				t,
				directory,
				dialect,
				test.Output["basic"],
				encoded,
			)
		}
	}
}

func assertOutputMatchesOfficialSchema(
	t *testing.T,
	directory string,
	dialect jsonschema.Dialect,
	constraint []byte,
	output []byte,
) {
	t.Helper()
	outputSchemaPath := filepath.Join(
		"testdata",
		"official",
		"JSON-Schema-Test-Suite",
		"output-tests",
		directory,
		"output-schema.json",
	)
	loader := jsonschema.ResourceLoaderFunc(func(ctx context.Context, identifier string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if identifier != "https://json-schema.org/draft/"+
			directory[len("draft"):]+"/output/schema" {
			return nil, fmt.Errorf("unexpected output schema resource %q", identifier)
		}
		// #nosec G304 -- path is fixed by the official dialect fixture table.
		return os.ReadFile(outputSchemaPath)
	})
	compiler, err := jsonschema.NewCompiler(
		jsonschema.WithDialect(dialect),
		jsonschema.WithResourceLoader(loader),
	)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(context.Background(), constraint)
	if err != nil {
		t.Fatal(err)
	}
	result, err := schema.Validate(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("output does not satisfy official constraint: %s", output)
	}
}
