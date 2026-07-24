package modelgen

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/internal/specification"
)

func TestGenerateCreatesVersionedImmutableTypedAccessors(t *testing.T) {
	t.Parallel()

	fields := []specification.ObjectField{
		{Object: "OpenAPI Object", Name: "openapi", Type: "string"},
		{Object: "OpenAPI Object", Name: "info", Type: "Info Object"},
		{Object: "Info Object", Name: "title", Type: "string"},
		{Object: "Info Object", Name: "deprecated", Type: "boolean"},
		{Object: "Operation Object", Name: "parameters", Type: "[Parameter Object | Reference Object]"},
		{Object: "Responses Object", Name: "default", Type: "Response Object | Reference Object"},
		{Object: "Responses Object", Name: "HTTP Status Code", Type: "Response Object | Reference Object", Pattern: true},
		{Object: "Components Object", Name: "schemas", Type: "Map[string, Schema Object]"},
	}

	raw, err := Generate(Config{
		Package:      "oas32",
		Version:      "3.2.0",
		RootObject:   "OpenAPI Object",
		VersionField: "openapi",
		Dialect:      "DialectOAS32",
	}, fields)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	generated := string(raw)
	for _, fragment := range []string{
		"type Document struct",
		"func Decode(raw jsonvalue.Value) (Document, error)",
		"func (value Info) Title() model.Field[string]",
		"func (value Info) Deprecated() model.Field[bool]",
		"func (value Operation) Parameters() model.Field[model.List[ParameterOrReference]]",
		"func (value Responses) Default() model.Field[ResponseOrReference]",
		"func (value Responses) Entries() model.Map[model.Field[ResponseOrReference]]",
		"func (value Components) Schemas() model.Field[model.Map[Schema]]",
		"type ParameterOrReference struct",
		"type ResponseOrReference struct",
	} {
		if !strings.Contains(generated, fragment) {
			t.Errorf("generated output does not contain %q", fragment)
		}
	}
}

func TestGenerateOrdersDocumentObjectsAndUnionsDeterministically(t *testing.T) {
	t.Parallel()
	if objectSortKey(generatedObject{name: "Document"}) != "" ||
		objectSortKey(generatedObject{name: "Alpha"}) != "Alpha" {
		t.Fatal("object sort keys do not prioritize Document")
	}

	fields := []specification.ObjectField{
		{Object: "OpenAPI Object", Name: "zebra", Type: "Zebra Object"},
		{Object: "OpenAPI Object", Name: "alpha", Type: "Alpha Object"},
		{Object: "OpenAPI Object", Name: "zref", Type: "Zebra Object | Reference Object"},
		{Object: "OpenAPI Object", Name: "aref", Type: "Alpha Object | Reference Object"},
	}
	generated, err := Generate(Config{
		Package: "oas32", Version: "3.2.0", RootObject: "OpenAPI Object",
	}, fields)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(generated)
	positions := []int{
		strings.Index(raw, "type Document struct"),
		strings.Index(raw, "type Alpha struct"),
		strings.Index(raw, "type Zebra struct"),
		strings.Index(raw, "type AlphaOrReference struct"),
		strings.Index(raw, "type ZebraOrReference struct"),
	}
	for index, position := range positions {
		if position < 0 || index > 0 && position <= positions[index-1] {
			t.Fatalf("generated declaration positions = %v", positions)
		}
	}
}

func TestGenerateRejectsUnmappedFieldTypes(t *testing.T) {
	t.Parallel()

	_, err := Generate(Config{Package: "oas32", Version: "3.2.0", RootObject: "OpenAPI Object"}, []specification.ObjectField{{
		Object: "Info Object",
		Name:   "mystery",
		Type:   "Unmapped Primitive",
	}})
	if err == nil {
		t.Fatal("Generate() error = nil")
	}
}

func TestGenerateTestsExercisesEveryGeneratedWrapper(t *testing.T) {
	t.Parallel()

	fields := []specification.ObjectField{
		{Object: "OpenAPI Object", Name: "openapi", Type: "string"},
		{Object: "OpenAPI Object", Name: "info", Type: "Info Object"},
		{Object: "Responses Object", Name: "default", Type: "Response Object | Reference Object"},
		{Object: "Responses Object", Name: "HTTP Status Code", Type: "Response Object | Reference Object", Pattern: true},
	}
	raw, err := GenerateTests(Config{
		Package:       "oas32",
		Version:       "3.2.0",
		RootObject:    "OpenAPI Object",
		VersionField:  "openapi",
		Dialect:       "DialectOAS32",
		BooleanSchema: true,
	}, fields)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(raw)
	for _, fragment := range []string{
		"func TestGeneratedModelSurface",
		"wrapDocument(object)",
		"wrapInfo(object)",
		"wrapResponseOrReference(object)",
		"reflect.ValueOf(value)",
		"Decode(versionObject(t, Version))",
	} {
		if !strings.Contains(generated, fragment) {
			t.Errorf("generated tests do not contain %q", fragment)
		}
	}
}

func TestFieldTypeHelpersCoverSupportedModelSurface(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"Any", "*", "Any | {expression}", "string", "boolean", "integer",
		"number", "[*]", "Map[ string, string]", "Map[string, [Info Object]]",
		"[Map[string, boolean]]", "Response Object | Reference Object",
		"OpenAPI Object", "Info Object",
	} {
		node, err := parseFieldType(raw, "OpenAPI Object")
		if err != nil {
			t.Fatalf("parseFieldType(%q) error = %v", raw, err)
		}
		if got := goType(node); got == "" {
			t.Fatalf("goType(parseFieldType(%q)) is empty", raw)
		}
		_ = decoderExpression(node)
	}
	for _, raw := range []string{
		"Map[string]", "Map[, string]", "Map[number, string]", "Map[string, Mystery]",
		"[Mystery]", "Info Object | string", "Mystery",
	} {
		if _, err := parseFieldType(raw, "OpenAPI Object"); err == nil {
			t.Fatalf("parseFieldType(%q) error = nil", raw)
		}
	}
}

func TestGeneratedAccessorsAndDecodersCoverEveryNodeKind(t *testing.T) {
	t.Parallel()

	stringNode := fieldType{kind: kindString}
	boolNode := fieldType{kind: kindBoolean}
	integerNode := fieldType{kind: kindInteger}
	numberNode := fieldType{kind: kindNumber}
	rawNode := fieldType{kind: kindRaw}
	objectNode := fieldType{kind: kindObject, name: "Info"}
	unionNode := fieldType{kind: kindReferenceUnion, name: "InfoOrReference"}
	listNode := fieldType{kind: kindList, elem: &stringNode}
	mapNode := fieldType{kind: kindMap, elem: &objectNode}
	for _, node := range []fieldType{
		stringNode, boolNode, integerNode, numberNode, rawNode, objectNode,
		unionNode, listNode, mapNode,
	} {
		if got := fieldAccessor(node, "field"); !strings.HasPrefix(got, "return ") {
			t.Fatalf("fieldAccessor(%v) = %q", node.kind, got)
		}
		if got := goType(node); got == "" {
			t.Fatalf("goType(%v) is empty", node.kind)
		}
	}
	for _, node := range []fieldType{
		stringNode, boolNode, integerNode, numberNode, rawNode, objectNode,
		unionNode,
	} {
		if got := decoderName(node); got == "" {
			t.Fatalf("decoderName(%v) is empty", node.kind)
		}
	}
	nested := fieldType{kind: kindList, elem: &mapNode}
	if got := decoderExpression(nested); !strings.Contains(got, "ListValue") ||
		!strings.Contains(got, "MapValue") {
		t.Fatalf("nested decoder expression = %q", got)
	}
	invalidNode := fieldType{kind: typeKind(255)}
	assertPanics(t, func() { fieldAccessor(invalidNode, "field") })
	assertPanics(t, func() { decoderName(listNode) })
	assertPanics(t, func() { goType(invalidNode) })
}

func TestModelNamingAndPatternPolicies(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"OpenAPI Object":                "Document",
		"OAuth Flows Object":            "OAuthFlows",
		"External Documentation Object": "ExternalDocumentation",
	} {
		if got := goObjectName(raw, "OpenAPI Object"); got != want {
			t.Fatalf("goObjectName(%q) = %q, want %q", raw, got, want)
		}
	}
	for raw, want := range map[string]string{
		"Document": "OpenAPI Object", "MediaType": "Media Type Object",
		"OAuthFlow": "OAuth Flow Object", "PathItem": "Path Item Object",
		"Unknown": "Unknown Object",
	} {
		if got := objectRawName(raw); got != want {
			t.Fatalf("objectRawName(%q) = %q, want %q", raw, got, want)
		}
	}
	for raw, want := range map[string]string{
		"": "Field", "$ref": "Ref", "operationId": "OperationID",
		"clientIdUrl": "ClientIDURL", "plain": "Plain",
	} {
		if got := goFieldName(raw); got != want {
			t.Fatalf("goFieldName(%q) = %q, want %q", raw, got, want)
		}
	}
	for raw, want := range map[string]string{
		"": "", "oauth": "OAuth", "openapi": "OpenAPI", "xml": "XML",
		"schema": "Schema",
	} {
		if got := exportedWord(raw); got != want {
			t.Fatalf("exportedWord(%q) = %q, want %q", raw, got, want)
		}
	}
	pathPattern := generatedField{rawName: "/{path}"}
	for _, test := range []struct {
		object generatedObject
		want   string
	}{
		{object: generatedObject{pattern: &pathPattern}, want: "pathPatternName"},
		{object: generatedObject{rawName: "Paths Object"}, want: "extensionAwarePatternName"},
		{object: generatedObject{rawName: "Responses Object"}, want: "extensionAwarePatternName"},
		{object: generatedObject{rawName: "Callback Object"}, want: "extensionAwarePatternName"},
		{object: generatedObject{rawName: "Info Object"}, want: "anyPatternName"},
	} {
		if got := patternPredicate(test.object); got != test.want {
			t.Fatalf("patternPredicate(%q) = %q, want %q",
				test.object.rawName, got, test.want)
		}
	}
}

func TestGenerateCoversInventoryFilteringAndConfigurationFailures(t *testing.T) {
	t.Parallel()

	for _, config := range []Config{
		{}, {Package: "oas32"},
		{Package: "oas32", Version: "3.2.0"},
	} {
		if _, err := Generate(config, nil); err == nil {
			t.Fatalf("Generate(%#v) error = nil", config)
		}
	}
	if _, err := Generate(Config{
		Package: "bad-name", Version: "3.2.0", RootObject: "OpenAPI Object",
	}, nil); err == nil {
		t.Fatal("Generate accepted an invalid Go package name")
	}
	if _, err := GenerateTests(Config{}, nil); err == nil {
		t.Fatal("GenerateTests accepted an invalid configuration")
	}
	config := Config{
		Package: "oas32", Version: "3.2.0", RootObject: "OpenAPI Object",
	}
	fields := []specification.ObjectField{
		{Object: "OpenAPI Object", Name: "openapi", Type: "string"},
		{Object: "OpenAPI Object", Name: "openapi", Type: "string"},
		{Object: "Paths Object", Name: "^x-", Type: "Mystery", Pattern: true},
		{Object: "Paths Object", Name: "^ignored", Type: "Mystery"},
		{Object: "Paths Object", Name: "{ignored}", Type: "Mystery"},
		{Object: "Paths Object", Name: "/{path}", Type: "Path Item Object", Pattern: true},
	}
	generated, err := Generate(config, fields)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(generated), "func (value Document) OpenAPI()") != 1 ||
		!strings.Contains(string(generated), "pathPatternName") {
		t.Fatalf("filtered generated output is incorrect:\n%s", generated)
	}
	fields[len(fields)-1].Type = "Mystery"
	if _, err := Generate(config, fields); err == nil {
		t.Fatal("Generate accepted an unmapped patterned field")
	}
}

func TestGenerateCoversBooleanSchemasAndVersionlessModels(t *testing.T) {
	t.Parallel()

	fields := []specification.ObjectField{
		{Object: "OpenAPI Object", Name: "schema", Type: "Schema Object"},
		{Object: "OpenAPI Object", Name: "info", Type: "Info Object"},
		{Object: "Schema Object", Name: "type", Type: "string"},
		{Object: "Info Object", Name: "title", Type: "string"},
	}
	config := Config{
		Package: "oas31", Version: "3.1.2", RootObject: "OpenAPI Object",
		VersionField: "openapi", Dialect: "DialectOAS31", BooleanSchema: true,
	}
	generated, err := Generate(config, fields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated),
		"raw.Kind() != jsonvalue.ObjectKind && raw.Kind() != jsonvalue.BooleanKind") {
		t.Fatalf("boolean Schema wrapper is absent:\n%s", generated)
	}
	tests, err := GenerateTests(config, fields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tests), "wrapSchema(jsonvalue.Boolean(true))") {
		t.Fatalf("boolean Schema test is absent:\n%s", tests)
	}
	swaggerTests, err := GenerateTests(Config{
		Package: "swagger20", Version: "2.0", RootObject: "Swagger Object",
		VersionField: "swagger", Dialect: "DialectSwagger20",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(swaggerTests), `versionObject(t, "3.2.0")`) {
		t.Fatalf("Swagger wrong-version case is absent:\n%s", swaggerTests)
	}
	versionless, err := Generate(Config{
		Package: "model", Version: "1", RootObject: "Root Object",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(versionless), "return document, nil") {
		t.Fatalf("versionless Decode branch is absent:\n%s", versionless)
	}
	for _, config := range []Config{
		{Package: "model", Version: "1", RootObject: "Root Object", VersionField: "version"},
		{Package: "model", Version: "1", RootObject: "Root Object", Dialect: "DialectOAS32"},
	} {
		if _, err := Generate(config, nil); err == nil {
			t.Fatalf("partial version configuration was accepted: %#v", config)
		}
	}
	if strings.Contains(string(versionless), "SpecificationVersion()") {
		t.Fatalf("versionless Document has a version accessor:\n%s", versionless)
	}
	booleanGenerated := string(generated)
	if strings.Count(booleanGenerated, "SpecificationVersion()") != 1 {
		t.Fatalf("version accessor leaked to a non-Document object:\n%s", booleanGenerated)
	}
	infoStart := strings.Index(booleanGenerated, "func wrapInfo")
	if infoStart < 0 {
		t.Fatal("Info wrapper is absent")
	}
	infoEnd := strings.Index(booleanGenerated[infoStart:], "func (value Info) Raw")
	if infoEnd < 0 || strings.Contains(
		booleanGenerated[infoStart:infoStart+infoEnd], "BooleanKind",
	) {
		t.Fatalf("boolean schema policy leaked into Info wrapper:\n%s", booleanGenerated)
	}
}

func assertPanics(t *testing.T, function func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("function did not panic")
		}
	}()
	function()
}
