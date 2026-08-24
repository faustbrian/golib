package convert

import (
	"context"
	"net/url"
	"strings"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

func TestOAS31PureHelpersCoverCompleteValueAlgebra(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		left  string
		right string
		want  bool
	}{
		{`null`, `null`, true},
		{`null`, `false`, false},
		{`true`, `true`, true},
		{`true`, `false`, false},
		{`1e-2`, `0.01`, true},
		{`1`, `2`, false},
		{`1e999999999999999999999999`, `1`, false},
		{`1`, `1e999999999999999999999999`, false},
		{`"one"`, `"one"`, true},
		{`"one"`, `"two"`, false},
		{`[1]`, `[1,2]`, false},
		{`[1]`, `[2]`, false},
		{`[1]`, `[1.0]`, true},
		{`{"a":1}`, `{}`, false},
		{`{"a":1}`, `{"b":1}`, false},
		{`{"a":1}`, `{"a":2}`, false},
		{`{"a":1}`, `{"a":1.0}`, true},
	} {
		if got := conversionValueEqual(
			conversionValue(t, test.left), conversionValue(t, test.right),
		); got != test.want {
			t.Errorf("conversionValueEqual(%s, %s) = %t", test.left, test.right, got)
		}
	}
	if conversionValueEqual(jsonvalue.Value{}, jsonvalue.Value{}) {
		t.Fatal("invalid values compared equal")
	}
	if _, valid := exactNumber(jsonvalue.Boolean(true)); valid {
		t.Fatal("boolean parsed as an exact number")
	}
	tooLargeExponent, err := jsonvalue.Number("1e999999999999999999999999")
	if err != nil {
		t.Fatal(err)
	}
	if _, valid := exactNumber(tooLargeExponent); valid {
		t.Fatal("unrepresentable exponent parsed as an exact number")
	}

	if constEnumConflict(conversionValue(t, `{"const":1,"enum":true}`)) {
		t.Fatal("scalar enum conflicted")
	}
	if !constEnumConflict(conversionValue(t, `{"const":1,"enum":[2]}`)) {
		t.Fatal("disjoint const and enum did not conflict")
	}

	for raw, want := range map[string]bool{
		`{"type":[1,"string"]}`:                    false,
		`{"type":[1,"null"]}`:                      true,
		`{"type":["string"]}`:                      false,
		`{"type":["string","null"],"anyOf":[]}`:    false,
		`{"type":["string","integer"]}`:            false,
		`{"type":["string","integer"],"anyOf":[]}`: true,
		`{"type":["null"]}`:                        true,
	} {
		value := conversionValue(t, raw)
		if got := schemaTypeContainsNull(value); got != strings.Contains(raw, `"null"`) {
			t.Errorf("schemaTypeContainsNull(%s) = %t", raw, got)
		}
		if got := schemaTypeNeedsAllOf(value); got != want {
			t.Errorf("schemaTypeNeedsAllOf(%s) = %t, want %t", raw, got, want)
		}
	}
}

func TestOAS31DowngradeHelpersCoverMalformedAndLegacyInputs(t *testing.T) {
	t.Parallel()

	converter := &oas31SchemaConverter{ctx: context.Background(), maxNodes: 100}
	if got := converter.downgradeXML(jsonvalue.Boolean(true), "/xml"); got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar XML = %#v", got)
	}
	for _, test := range []struct {
		raw           string
		hasAttribute  bool
		wantAttribute bool
		wantLosses    int
	}{
		{raw: `{"nodeType":"text","name":"value"}`, wantLosses: 1},
		{raw: `{"nodeType":1,"name":"value"}`, wantLosses: 1},
		{raw: `{"nodeType":"attribute","attribute":false,"name":"value"}`, hasAttribute: true, wantAttribute: true},
		{raw: `{"nodeType":"element","name":"value"}`, hasAttribute: true},
		{raw: `{"attribute":true,"name":"value"}`, hasAttribute: true, wantAttribute: true},
	} {
		converter.diagnostics = nil
		converted := converter.downgradeXML(conversionValue(t, test.raw), "/xml")
		if len(converter.diagnostics) != test.wantLosses {
			t.Errorf("XML %s diagnostics = %#v", test.raw, converter.diagnostics)
		}
		attribute, exists := converted.Lookup("attribute")
		if !test.hasAttribute {
			if exists {
				t.Errorf("XML %s retained attribute", test.raw)
			}
			continue
		}
		got, valid := attribute.Bool()
		if !exists || !valid || got != test.wantAttribute {
			t.Errorf("XML %s attribute = %#v", test.raw, attribute)
		}
	}
	if got := converter.withoutObjectField(
		jsonvalue.Boolean(true), "/value", "field", "message",
	); got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar field removal = %#v", got)
	}
	if got := converter.withoutObjectField(
		conversionValue(t, `{"keep":true}`), "/value", "field", "message",
	); got.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("absent field removal = %#v", got)
	}

	for _, test := range []struct {
		raw       string
		bound     string
		exclusive string
		lower     bool
		wantValue string
		wantFlag  bool
		present   bool
	}{
		{raw: `{}`, bound: "minimum", exclusive: "exclusiveMinimum"},
		{raw: `{"exclusiveMinimum":true}`, bound: "minimum", exclusive: "exclusiveMinimum"},
		{raw: `{"exclusiveMinimum":2}`, bound: "minimum", exclusive: "exclusiveMinimum", wantValue: "2", wantFlag: true, present: true},
		{raw: `{"minimum":1,"exclusiveMinimum":2}`, bound: "minimum", exclusive: "exclusiveMinimum", lower: true, wantValue: "2", wantFlag: true, present: true},
		{raw: `{"minimum":2,"exclusiveMinimum":1}`, bound: "minimum", exclusive: "exclusiveMinimum", lower: true, wantValue: "2", present: true},
		{raw: `{"minimum":1,"exclusiveMinimum":1}`, bound: "minimum", exclusive: "exclusiveMinimum", lower: true, wantValue: "1", wantFlag: true, present: true},
		{raw: `{"maximum":2,"exclusiveMaximum":1}`, bound: "maximum", exclusive: "exclusiveMaximum", wantValue: "1", wantFlag: true, present: true},
		{raw: `{"maximum":1,"exclusiveMaximum":2}`, bound: "maximum", exclusive: "exclusiveMaximum", wantValue: "1", present: true},
		{raw: `{"maximum":1,"exclusiveMaximum":1}`, bound: "maximum", exclusive: "exclusiveMaximum", wantValue: "1", wantFlag: true, present: true},
		{raw: `{"minimum":1,"exclusiveMinimum":1e999999999999999999999999}`, bound: "minimum", exclusive: "exclusiveMinimum", lower: true, wantValue: "1e999999999999999999999999", wantFlag: true, present: true},
	} {
		value, flag, present := downgradeExclusiveBound(
			conversionValue(t, test.raw), test.bound, test.exclusive, test.lower,
		)
		if flag != test.wantFlag || present != test.present {
			t.Errorf("exclusive bound %s = %t, %t", test.raw, flag, present)
		}
		if test.wantValue != "" {
			text, _ := value.NumberText()
			if text != test.wantValue {
				t.Errorf("exclusive bound %s value = %q", test.raw, text)
			}
		}
	}
}

func TestOAS31ConverterPropagatesRecursiveFailures(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	converter := &oas31SchemaConverter{ctx: canceled, maxNodes: 10}
	if _, err := converter.document(
		conversionValue(t, `{}`), "", map[string]struct{}{},
		map[string]struct{}{}, map[string]struct{}{},
	); err != context.Canceled {
		t.Fatalf("document cancellation = %v", err)
	}

	converter = &oas31SchemaConverter{ctx: context.Background(), maxNodes: 0}
	locations := map[string]struct{}{`/0`: {}}
	if _, err := converter.document(
		conversionValue(t, `[{}]`), "", locations,
		map[string]struct{}{}, map[string]struct{}{},
	); err != ErrLimitExceeded {
		t.Fatalf("array document error = %v", err)
	}
	converter = &oas31SchemaConverter{ctx: context.Background(), maxNodes: 10}
	if got, err := converter.document(
		conversionValue(t, `[true]`), "", map[string]struct{}{},
		map[string]struct{}{}, map[string]struct{}{},
	); err != nil || got.Kind() != jsonvalue.ArrayKind {
		t.Fatalf("array document = %#v, %v", got, err)
	}
	canceled, cancel = context.WithCancel(context.Background())
	cancel()
	converter = &oas31SchemaConverter{ctx: canceled, maxNodes: 10}
	if _, err := converter.schema(conversionValue(t, `{}`), "/schema"); err != context.Canceled {
		t.Fatalf("schema cancellation = %v", err)
	}
	converter = &oas31SchemaConverter{ctx: context.Background(), maxNodes: 10}
	if got, err := converter.schema(jsonvalue.Null(), "/schema"); err != nil || got.Kind() != jsonvalue.NullKind {
		t.Fatalf("scalar schema = %#v, %v", got, err)
	}
	converter.maxNodes = 1
	converter.nodes = 0
	if _, err := converter.schema(conversionValue(t, `{}`), "/schema"); err != nil {
		t.Fatalf("schema at exact node limit = %v", err)
	}
	if _, err := converter.schema(conversionValue(t, `{}`), "/schema"); err != ErrLimitExceeded {
		t.Fatalf("schema beyond exact node limit = %v", err)
	}
	converter.maxNodes = 10
	converter.nodes = 0
	converter.diagnostics = nil
	converted, err := converter.schema(conversionValue(t, `{"examples":[1]}`), "/schema")
	if err != nil || len(converter.diagnostics) != 0 {
		t.Fatalf("single schema example = %#v, %v", converter.diagnostics, err)
	}
	if _, exists := converted.Lookup("example"); !exists {
		t.Fatal("single schema example was not retained")
	}
	converter.nodes = 0
	converter.diagnostics = nil
	if _, err := converter.schema(conversionValue(t, `{"examples":[1,2]}`), "/schema"); err != nil || len(converter.diagnostics) != 1 {
		t.Fatalf("multiple schema examples = %#v, %v", converter.diagnostics, err)
	}
	converter.maxNodes = 0
	converter.nodes = 0
	if _, err := converter.schemaMap(
		conversionValue(t, `{"value":{}}`), "/properties",
	); err != ErrLimitExceeded {
		t.Fatalf("schema map error = %v", err)
	}
	converter.nodes = 0
	if _, err := converter.schemaArray(
		conversionValue(t, `[{}]`), "/allOf",
	); err != ErrLimitExceeded {
		t.Fatalf("schema array error = %v", err)
	}
	if got, err := converter.schemaMap(jsonvalue.Boolean(true), "/properties"); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar schema map = %#v, %v", got, err)
	}
	if got, err := converter.schemaArray(jsonvalue.Boolean(true), "/allOf"); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar schema array = %#v, %v", got, err)
	}

	for _, raw := range []string{
		`{"type":["null"]}`,
		`{"type":["string","integer"],"anyOf":true}`,
	} {
		converter.nodes = 0
		schema := conversionValue(t, raw)
		typeValue, _ := schema.Lookup("type")
		if _, _, err := converter.schemaType(schema, typeValue, "/type"); err != ErrLimitExceeded {
			t.Errorf("schema type %s error = %v", raw, err)
		}
	}
	converter = &oas31SchemaConverter{ctx: context.Background(), maxNodes: 1}
	if _, err := converter.schema(
		conversionValue(t, `{"type":["null"]}`), "/schema",
	); err != ErrLimitExceeded {
		t.Fatalf("schema type propagation error = %v", err)
	}
	converter = &oas31SchemaConverter{ctx: context.Background(), maxNodes: 10}
	if _, err := converter.schema(
		conversionValue(t, `{"const":1,"enum":[2],"not":{"type":"string"}}`),
		"/schema",
	); err != nil {
		t.Fatal(err)
	}

	converter = &oas31SchemaConverter{ctx: context.Background(), maxNodes: 10}
	if _, err := converter.typeConstraintAllOf(
		conversionValue(t, `{"allOf":true}`), conversionValue(t, `{}`), "/type",
	); err != nil {
		t.Fatal(err)
	}
}

func TestOAS30ConverterCoversDefensiveTraversal(t *testing.T) {
	t.Parallel()

	converter := &oas30SchemaConverter{ctx: context.Background(), maxNodes: 0}
	if _, err := converter.document(
		conversionValue(t, `[{}]`), "", map[string]struct{}{`/0`: {}},
		map[string]struct{}{},
	); err != ErrLimitExceeded {
		t.Fatalf("array document propagation error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	converter = &oas30SchemaConverter{ctx: canceled, maxNodes: 10}
	if _, err := converter.schema(conversionValue(t, `{}`), "/schema"); err != context.Canceled {
		t.Fatalf("schema cancellation = %v", err)
	}
	converter = &oas30SchemaConverter{ctx: context.Background(), maxNodes: 100}
	if got, err := converter.schema(jsonvalue.Null(), "/schema"); err != nil || got.Kind() != jsonvalue.NullKind {
		t.Fatalf("scalar schema = %#v, %v", got, err)
	}
	exact := &oas30SchemaConverter{ctx: context.Background(), maxNodes: 1}
	if _, err := exact.schema(conversionValue(t, `{}`), "/schema"); err != nil {
		t.Fatalf("exact schema-node limit error = %v", err)
	}
	for _, raw := range []string{
		`{"minimum":1,"exclusiveMinimum":true}`,
		`{"minimum":1,"exclusiveMinimum":false}`,
		`{"maximum":2,"exclusiveMaximum":true}`,
	} {
		if _, err := converter.schema(conversionValue(t, raw), "/schema"); err != nil {
			t.Fatal(err)
		}
	}
	converted, err := converter.schema(
		conversionValue(t, `{"maximum":2,"exclusiveMaximum":true}`),
		"/schema",
	)
	if err != nil {
		t.Fatal(err)
	}
	exclusive, _ := converted.Lookup("exclusiveMaximum")
	if number, _ := exclusive.NumberText(); number != "2" {
		t.Fatalf("exclusive maximum = %q", number)
	}

	converter = &oas30SchemaConverter{ctx: context.Background(), maxNodes: 0}
	if _, err := converter.schemaMap(
		conversionValue(t, `{"value":{}}`), "/properties",
	); err != ErrLimitExceeded {
		t.Fatalf("schema map error = %v", err)
	}
	converter.nodes = 0
	if _, err := converter.schemaArray(
		conversionValue(t, `[{}]`), "/allOf",
	); err != ErrLimitExceeded {
		t.Fatalf("schema array error = %v", err)
	}
	if got, err := converter.schemaMap(jsonvalue.Boolean(true), "/properties"); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar schema map = %#v, %v", got, err)
	}
	if got, err := converter.schemaArray(jsonvalue.Boolean(true), "/allOf"); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar schema array = %#v, %v", got, err)
	}
}

func TestOAS30CollectorCoversMalformedAndReferencedObjects(t *testing.T) {
	t.Parallel()

	collector := oas30SchemaCollector{
		locations:  map[string]struct{}{},
		references: map[string]struct{}{},
		pathItems:  map[string]struct{}{},
	}
	referenceValue := conversionValue(t, `{"$ref":"#/components/schemas/Value"}`)
	collector.pathItem(referenceValue, "/path")
	for _, value := range []jsonvalue.Value{jsonvalue.Boolean(true), referenceValue} {
		collector.parameter(value, "/parameter")
		collector.response(value, "/response")
		collector.callback(value, "/callback")
	}
	collector.content(
		conversionValue(t, `{"content":{"application/json":true}}`), "/content",
	)
	collector.referenceOrVisit(jsonvalue.Boolean(true), "/value", nil)
	if len(collector.locations) != 0 {
		t.Fatalf("malformed collector locations = %#v", collector.locations)
	}
}

func TestOAS30ConversionContinuesAfterSkippedSchemaAndMediaMembers(t *testing.T) {
	t.Parallel()

	converter := &oas30SchemaConverter{ctx: context.Background(), maxNodes: 100}
	nullableNull, err := converter.schema(
		conversionValue(t, `{"type":"null","nullable":true,"x-after":true}`),
		"/schema",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := textValue(t, memberAt(t, nullableNull, "type")); got != "null" {
		t.Fatalf("nullable null type = %q", got)
	}
	for _, test := range []struct {
		raw    string
		absent []string
	}{
		{
			raw:    `{"maximum":2,"exclusiveMaximum":true,"x-after":true}`,
			absent: []string{"maximum"},
		},
		{
			raw:    `{"exclusiveMinimum":false,"x-after":true}`,
			absent: []string{"exclusiveMinimum"},
		},
		{
			raw:    `{"exclusiveMinimum":true,"x-after":true}`,
			absent: []string{"exclusiveMinimum"},
		},
		{
			raw: `{"minimum":"invalid","exclusiveMinimum":true,"x-after":true}`,
			absent: []string{
				"minimum", "exclusiveMinimum",
			},
		},
		{
			raw: `{"maximum":"invalid","exclusiveMaximum":true,"x-after":true}`,
			absent: []string{
				"maximum", "exclusiveMaximum",
			},
		},
	} {
		converted, err := converter.schema(conversionValue(t, test.raw), "/schema")
		if err != nil {
			t.Fatal(err)
		}
		if after, exists := converted.Lookup("x-after"); !exists || after.Kind() != jsonvalue.BooleanKind {
			t.Fatalf("schema member after skipped keyword = %#v", converted)
		}
		for _, name := range test.absent {
			if _, exists := converted.Lookup(name); exists {
				t.Fatalf("skipped schema keyword %q was retained in %#v", name, converted)
			}
		}
	}

	collector := oas30SchemaCollector{
		locations:  map[string]struct{}{},
		references: map[string]struct{}{},
	}
	collector.content(conversionValue(t, `{
		"content":{
			"application/broken":true,
			"application/json":{"schema":{"type":"string"}}
		}
	}`), "/request")
	if _, exists := collector.locations["/request/application~1json/schema"]; !exists {
		t.Fatalf("schema locations = %#v", collector.locations)
	}
	collector.pathItem(conversionValue(t, `{
		"get":true,
		"put":{"requestBody":{"content":{"application/json":{
			"schema":{"type":"string"}
		}}}}
	}`), "/path")
	if _, exists := collector.locations["/path/put/requestBody/content/application~1json/schema"]; !exists {
		t.Fatalf("operation after malformed method = %#v", collector.locations)
	}
}

func TestOAS32ConverterCoversScalarAndVisitFailures(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas32DocumentConverter {
		return &oas32DocumentConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			mediaTypes: map[string]jsonvalue.Value{},
			mediaCache: map[string]jsonvalue.Value{},
			resolving:  map[string]struct{}{},
		}
	}
	scalar := jsonvalue.Boolean(true)
	exact := newConverter(1)
	if err := exact.visit(); err != nil {
		t.Fatalf("exact document-node limit error = %v", err)
	}
	converter := newConverter(100)
	visitors := []func(jsonvalue.Value, string) (jsonvalue.Value, error){
		converter.pathItem,
		converter.operation,
		converter.response,
		converter.components,
		converter.parameter,
		converter.requestBody,
		converter.mediaType,
		converter.example,
		converter.encoding,
		converter.securityScheme,
		converter.oauthFlows,
		func(value jsonvalue.Value, pointer string) (jsonvalue.Value, error) {
			return converter.withoutFields(value, pointer, map[string]string{})
		},
	}
	for index, visit := range visitors {
		got, err := visit(scalar, "/value")
		if err != nil || got.Kind() != jsonvalue.BooleanKind {
			t.Errorf("scalar visitor %d = %#v, %v", index, got, err)
		}
	}
	for index, visit := range []func(*oas32DocumentConverter) error{
		func(c *oas32DocumentConverter) error {
			_, err := c.pathItem(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.operation(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.response(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.components(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.parameter(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.requestBody(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.mediaType(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.example(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.encoding(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.securityScheme(conversionValue(t, `{}`), "/value")
			return err
		},
		func(c *oas32DocumentConverter) error {
			_, err := c.withoutFields(conversionValue(t, `{}`), "/value", map[string]string{})
			return err
		},
	} {
		if err := visit(newConverter(0)); err != ErrLimitExceeded {
			t.Errorf("visit failure %d = %v", index, err)
		}
	}
	if got, err := converter.objectMap(scalar, "/map", converter.example); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar object map = %#v, %v", got, err)
	}
	if got, err := converter.objectArray(scalar, "/array", converter.example); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar object array = %#v, %v", got, err)
	}
	want := ErrLimitExceeded
	fail := func(jsonvalue.Value, string) (jsonvalue.Value, error) {
		return jsonvalue.Value{}, want
	}
	if _, err := converter.objectMap(
		conversionValue(t, `{"value":{}}`), "/map", fail,
	); err != want {
		t.Fatalf("object map error = %v", err)
	}
	if _, err := converter.objectArray(
		conversionValue(t, `[{}]`), "/array", fail,
	); err != want {
		t.Fatalf("object array error = %v", err)
	}
}

func TestOAS32ConverterPropagatesNestedFailures(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas32DocumentConverter {
		return &oas32DocumentConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			mediaTypes: map[string]jsonvalue.Value{},
			mediaCache: map[string]jsonvalue.Value{},
			resolving:  map[string]struct{}{},
		}
	}
	for _, raw := range []string{
		`{"servers":[{}]}`,
		`{"paths":{"/value":{}}}`,
		`{"components":{}}`,
		`{"tags":[{}]}`,
	} {
		if _, err := newConverter(1).document(conversionValue(t, raw)); err != ErrLimitExceeded {
			t.Errorf("document %s error = %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"get":{}}`, `{"servers":[{}]}`, `{"parameters":[{}]}`,
	} {
		if _, err := newConverter(1).pathItem(conversionValue(t, raw), "/path"); err != ErrLimitExceeded {
			t.Errorf("path item %s error = %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"servers":[{}]}`,
		`{"responses":{"200":{}}}`,
		`{"parameters":[{}]}`,
		`{"requestBody":{}}`,
		`{"callbacks":{"event":{"expression":{}}}}`,
	} {
		if _, err := newConverter(1).operation(conversionValue(t, raw), "/operation"); err != ErrLimitExceeded {
			t.Errorf("operation %s error = %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"content":{"application/json":{}}}`,
		`{"headers":{"X-Value":{}}}`,
	} {
		if _, err := newConverter(1).response(conversionValue(t, raw), "/response"); err != ErrLimitExceeded {
			t.Errorf("response %s error = %v", raw, err)
		}
	}
	if _, err := newConverter(10).response(
		conversionValue(t, `{"description":"ok","headers":{"X-Value":{}}}`),
		"/response",
	); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"securitySchemes":{"value":{}}}`,
		`{"responses":{"value":{}}}`,
		`{"parameters":{"value":{}}}`,
		`{"requestBodies":{"value":{}}}`,
		`{"examples":{"value":{}}}`,
		`{"callbacks":{"value":{"expression":{}}}}`,
		`{"pathItems":{"value":{}}}`,
	} {
		if _, err := newConverter(1).components(conversionValue(t, raw), "/components"); err != ErrLimitExceeded {
			t.Errorf("components %s error = %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"content":{"application/json":{}}}`,
		`{"examples":{"value":{}}}`,
	} {
		if _, err := newConverter(1).parameter(conversionValue(t, raw), "/parameter"); err != ErrLimitExceeded {
			t.Errorf("parameter %s error = %v", raw, err)
		}
	}
	if _, err := newConverter(1).requestBody(
		conversionValue(t, `{"description":"value","content":{"application/json":{}}}`),
		"/requestBody",
	); err != ErrLimitExceeded {
		t.Fatalf("request body error = %v", err)
	}
	for _, raw := range []string{
		`{"examples":{"value":{}}}`,
		`{"encoding":{"value":{}}}`,
	} {
		if _, err := newConverter(1).mediaType(conversionValue(t, raw), "/media"); err != ErrLimitExceeded {
			t.Errorf("media type %s error = %v", raw, err)
		}
	}
	if _, err := newConverter(1).encoding(
		conversionValue(t, `{"headers":{"X-Value":{}}}`), "/encoding",
	); err != ErrLimitExceeded {
		t.Fatalf("encoding error = %v", err)
	}
	if _, err := newConverter(1).securityScheme(
		conversionValue(t, `{"flows":{"password":{}}}`), "/security",
	); err != ErrLimitExceeded {
		t.Fatalf("security scheme error = %v", err)
	}
	if _, err := newConverter(0).oauthFlows(
		conversionValue(t, `{"password":{}}`), "/flows",
	); err != ErrLimitExceeded {
		t.Fatalf("OAuth flow error = %v", err)
	}
}

func TestOAS32MediaReferenceCacheAndFailure(t *testing.T) {
	t.Parallel()

	referenceName := "#/components/mediaTypes/Shared"
	cached := conversionValue(t, `{"schema":{"type":"string"}}`)
	converter := &oas32DocumentConverter{
		ctx: context.Background(), maxNodes: 10,
		mediaTypes: map[string]jsonvalue.Value{referenceName: cached},
		mediaCache: map[string]jsonvalue.Value{referenceName: cached},
		resolving:  map[string]struct{}{},
	}
	got, err := converter.mediaType(
		conversionValue(t, `{"$ref":"`+referenceName+`"}`), "/media",
	)
	if err != nil || got.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("cached media type = %#v, %v", got, err)
	}
	converter.mediaCache = map[string]jsonvalue.Value{}
	converter.maxNodes = 0
	if _, err := converter.mediaType(
		conversionValue(t, `{"$ref":"`+referenceName+`"}`), "/media",
	); err != ErrLimitExceeded {
		t.Fatalf("referenced media conversion error = %v", err)
	}
}

func TestSwaggerUpgradeCoversDefensiveInputsAndBounds(t *testing.T) {
	t.Parallel()

	newConverter := func(documentNodes int, schemaNodes int) *swagger20Converter {
		return &swagger20Converter{
			ctx: context.Background(), maxDocumentNodes: documentNodes,
			maxSchemaNodes: schemaNodes,
			bodyRefs:       map[string]struct{}{},
			formRefs:       map[string]jsonvalue.Value{},
			parameterRefs:  map[string]jsonvalue.Value{},
		}
	}
	converter := newConverter(100, 100)
	for _, raw := range []string{
		`{"schemes":["https"]}`,
		`{"basePath":"/api","schemes":["https"]}`,
		`{"host":"example.test","schemes":[true]}`,
	} {
		converter.servers(conversionValue(t, raw))
	}
	for _, test := range []struct {
		root    string
		schemes string
	}{
		{root: `{}`, schemes: `["https"]`},
		{root: `{"host":"example.test"}`, schemes: `true`},
		{root: `{"host":"example.test"}`, schemes: `[true]`},
	} {
		converter.serversForSchemes(
			conversionValue(t, test.root), conversionValue(t, test.schemes), "/schemes",
		)
	}

	scalar := jsonvalue.Boolean(true)
	if got, err := converter.paths(scalar, conversionValue(t, `{}`), "/paths"); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar paths = %#v, %v", got, err)
	}
	for _, call := range []func(*swagger20Converter) error{
		func(c *swagger20Converter) error {
			_, err := c.pathItem(conversionValue(t, `{}`), conversionValue(t, `{}`), "/path")
			return err
		},
		func(c *swagger20Converter) error {
			_, err := c.operation(conversionValue(t, `{}`), conversionValue(t, `{}`), "/operation", nil)
			return err
		},
		func(c *swagger20Converter) error {
			_, err := c.parameter(conversionValue(t, `{}`), "/parameter")
			return err
		},
		func(c *swagger20Converter) error {
			_, err := c.requestBody(conversionValue(t, `{}`), nil, "/body")
			return err
		},
		func(c *swagger20Converter) error { _, err := c.formRequestBody(nil, nil, "/form"); return err },
		func(c *swagger20Converter) error {
			_, err := c.response(conversionValue(t, `{}`), nil, "/response")
			return err
		},
		func(c *swagger20Converter) error {
			_, err := c.securityScheme(conversionValue(t, `{}`), "/security")
			return err
		},
	} {
		if err := call(newConverter(0, 100)); err != ErrLimitExceeded {
			t.Errorf("document visit error = %v", err)
		}
	}
	for _, visit := range []func(*swagger20Converter) (jsonvalue.Value, error){
		func(c *swagger20Converter) (jsonvalue.Value, error) {
			return c.pathItem(scalar, conversionValue(t, `{}`), "/path")
		},
		func(c *swagger20Converter) (jsonvalue.Value, error) {
			return c.operation(scalar, conversionValue(t, `{}`), "/operation", nil)
		},
		func(c *swagger20Converter) (jsonvalue.Value, error) { return c.parameter(scalar, "/parameter") },
		func(c *swagger20Converter) (jsonvalue.Value, error) { return c.response(scalar, nil, "/response") },
	} {
		got, err := visit(newConverter(100, 100))
		if err != nil || got.Kind() != jsonvalue.BooleanKind {
			t.Errorf("scalar visitor = %#v, %v", got, err)
		}
	}

	path := conversionValue(t, `{
		"$ref":"external.json#/path","summary":"value","parameters":[]
	}`)
	if _, err := converter.pathItem(path, conversionValue(t, `{}`), "/path"); err != nil {
		t.Fatal(err)
	}
	if value, inherited, err := converter.splitPathParameters(
		scalar, "/parameters",
	); err != nil || len(inherited) != 0 || value.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar split parameters = %#v, %#v, %v", value, inherited, err)
	}
	converter.bodyRefs["#/parameters/Body"] = struct{}{}
	converter.formRefs["#/parameters/Form"] = conversionValue(t, `{"in":"formData"}`)
	if !converter.isRequestParameter(referenceValue("#/parameters/Body")) ||
		!converter.isRequestParameter(referenceValue("#/parameters/Form")) {
		t.Fatal("reusable request parameters were not classified")
	}
	if got := converter.parameterIdentity(referenceValue("external.json#/P")); got != "$ref\x00external.json#/P" {
		t.Fatalf("external parameter identity = %q", got)
	}
	if value, body, err := converter.operationParameters(
		scalar, nil, "/parameters", nil,
	); err != nil || value.Kind() != jsonvalue.BooleanKind ||
		body.Kind() != jsonvalue.InvalidKind {
		t.Fatalf("scalar operation parameters = %#v, %#v, %v", value, body, err)
	}
	if regular, bodies, err := converter.reusableParameters(
		scalar, nil, "/parameters",
	); err != nil || regular.Kind() != jsonvalue.BooleanKind ||
		bodies.Kind() != jsonvalue.InvalidKind {
		t.Fatalf("scalar reusable parameters = %#v, %#v, %v", regular, bodies, err)
	}
	converter.indexBodyParameters(conversionValue(t, `{
		"parameters":{"Form":{"in":"formData"}}
	}`))

	for _, helper := range []func(jsonvalue.Value) (jsonvalue.Value, error){
		func(value jsonvalue.Value) (jsonvalue.Value, error) {
			return converter.responseMap(value, nil, "/responses")
		},
		func(value jsonvalue.Value) (jsonvalue.Value, error) { return converter.headers(value, "/headers") },
		func(value jsonvalue.Value) (jsonvalue.Value, error) { return converter.schemaMap(value, "/schemas") },
		func(value jsonvalue.Value) (jsonvalue.Value, error) { return converter.schemaArray(value, "/allOf") },
		func(value jsonvalue.Value) (jsonvalue.Value, error) {
			return converter.securitySchemes(value, "/security")
		},
	} {
		got, err := helper(scalar)
		if err != nil || got.Kind() != jsonvalue.BooleanKind {
			t.Errorf("scalar collection helper = %#v, %v", got, err)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	converter = newConverter(100, 100)
	converter.ctx = canceled
	if _, err := converter.schema(conversionValue(t, `{}`), "/schema"); err != context.Canceled {
		t.Fatalf("schema cancellation = %v", err)
	}
	converter = newConverter(100, 100)
	if got, err := converter.schema(scalar, "/schema"); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar schema = %#v, %v", got, err)
	}
	if _, err := newConverter(100, 1).parameter(
		conversionValue(t, `{"name":"value","in":"query","type":"array","items":{"type":"string"}}`),
		"/parameter",
	); err != ErrLimitExceeded {
		t.Fatalf("parameter schema error = %v", err)
	}
	if _, err := newConverter(100, 0).requestBody(
		conversionValue(t, `{"schema":{}}`), nil, "/body",
	); err != ErrLimitExceeded {
		t.Fatalf("request body schema error = %v", err)
	}
	if _, err := newConverter(100, 0).response(
		conversionValue(t, `{"schema":{}}`), nil, "/response",
	); err != ErrLimitExceeded {
		t.Fatalf("response schema error = %v", err)
	}
	if _, err := newConverter(0, 100).response(
		conversionValue(t, `{"headers":{"X-Value":{}}}`), nil, "/response",
	); err != ErrLimitExceeded {
		t.Fatalf("response header error = %v", err)
	}
	if _, err := newConverter(0, 100).headers(
		conversionValue(t, `{"X-Value":{}}`), "/headers",
	); err != ErrLimitExceeded {
		t.Fatalf("headers error = %v", err)
	}
}

func TestSwaggerUpgradePureHelpersCoverMissingAndDuplicateValues(t *testing.T) {
	t.Parallel()

	if got := stringArrayMember(conversionValue(t, `{"values":true}`), "values"); got != nil {
		t.Fatalf("scalar string array = %#v", got)
	}
	if got := appendUnique([]string{"one"}, "two"); len(got) != 2 {
		t.Fatalf("unique append = %#v", got)
	}
	if got, err := appendObjectMember(
		jsonvalue.Boolean(true), "value", jsonvalue.Boolean(true),
	); err != nil || got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("scalar object append = %#v, %v", got, err)
	}
	object := conversionValue(t, `{"value":true}`)
	if got, err := appendObjectMember(
		object, "value", jsonvalue.Boolean(false),
	); err != nil || !conversionValueEqual(got, object) {
		t.Fatalf("duplicate object append = %#v, %v", got, err)
	}
}

func TestSwaggerUpgradePropagatesNestedFailures(t *testing.T) {
	t.Parallel()

	newConverter := func(documentNodes int, schemaNodes int) *swagger20Converter {
		return &swagger20Converter{
			ctx: context.Background(), maxDocumentNodes: documentNodes,
			maxSchemaNodes: schemaNodes,
			bodyRefs:       map[string]struct{}{},
			formRefs:       map[string]jsonvalue.Value{},
			parameterRefs:  map[string]jsonvalue.Value{},
		}
	}
	for _, raw := range []string{
		`{"definitions":{"Value":{}}}`,
		`{"parameters":{"Value":{"in":"query","type":"string"}}}`,
		`{"responses":{"Value":{"description":"ok"}}}`,
		`{"securityDefinitions":{"Value":{"type":"basic"}}}`,
	} {
		converter := newConverter(0, 0)
		if _, _, err := converter.components(conversionValue(t, raw)); err != ErrLimitExceeded {
			t.Errorf("components %s error = %v", raw, err)
		}
	}
	if _, err := newConverter(1, 100).pathItem(
		conversionValue(t, `{"parameters":[{"in":"query","type":"string"}]}`),
		conversionValue(t, `{}`), "/path",
	); err != ErrLimitExceeded {
		t.Fatalf("path parameter error = %v", err)
	}
	for _, raw := range []string{
		`{"parameters":[{"in":"query","type":"string"}]}`,
		`{"responses":{"200":{"description":"ok"}}}`,
	} {
		if _, err := newConverter(1, 100).operation(
			conversionValue(t, raw), conversionValue(t, `{}`), "/operation", nil,
		); err != ErrLimitExceeded {
			t.Errorf("operation %s error = %v", raw, err)
		}
	}
	inherited := []swaggerParameterInput{{
		value:   conversionValue(t, `{"name":"old","in":"query","type":"string"}`),
		pointer: "/inherited",
	}}
	if _, err := newConverter(1, 100).operation(
		conversionValue(t, `{}`), conversionValue(t, `{}`), "/operation", inherited,
	); err != ErrLimitExceeded {
		t.Fatalf("inherited operation parameter error = %v", err)
	}
	if converted, err := newConverter(100, 100).operation(
		conversionValue(t, `{}`), conversionValue(t, `{}`), "/operation", inherited,
	); err != nil {
		t.Fatal(err)
	} else if parameters, exists := converted.Lookup("parameters"); !exists || parameters.Kind() != jsonvalue.ArrayKind {
		t.Fatalf("inherited operation parameters = %#v", converted)
	}
	if _, _, err := newConverter(0, 100).splitPathParameters(
		conversionValue(t, `[{"in":"query","type":"string"}]`), "/parameters",
	); err != ErrLimitExceeded {
		t.Fatalf("split parameter error = %v", err)
	}
}

func TestSwaggerUpgradeExactDecisionBoundaries(t *testing.T) {
	t.Parallel()

	newConverter := func(documentNodes int, schemaNodes int) *swagger20Converter {
		return &swagger20Converter{
			ctx: context.Background(), maxDocumentNodes: documentNodes,
			maxSchemaNodes: schemaNodes,
			bodyRefs:       map[string]struct{}{},
			formRefs:       map[string]jsonvalue.Value{},
			parameterRefs:  map[string]jsonvalue.Value{},
		}
	}

	exactDocument := newConverter(1, 1)
	if err := exactDocument.visit(); err != nil {
		t.Fatalf("exact document-node limit = %v", err)
	}
	if err := exactDocument.visit(); err != ErrLimitExceeded {
		t.Fatalf("document node beyond exact limit = %v", err)
	}
	exactSchema := newConverter(10, 1)
	if _, err := exactSchema.schema(jsonvalue.Boolean(true), "/schema"); err != nil {
		t.Fatalf("exact schema-node limit = %v", err)
	}
	if _, err := exactSchema.schema(jsonvalue.Boolean(true), "/schema"); err != ErrLimitExceeded {
		t.Fatalf("schema node beyond exact limit = %v", err)
	}

	converter := newConverter(100, 100)
	regular, bodies, err := converter.reusableParameters(
		conversionValue(t, `{}`), nil, "/parameters",
	)
	if err != nil || regular.Kind() != jsonvalue.InvalidKind ||
		bodies.Kind() != jsonvalue.InvalidKind {
		t.Fatalf("empty reusable parameters = %#v, %#v, %v", regular, bodies, err)
	}
	regular, bodies, err = converter.reusableParameters(conversionValue(t, `{
		"Value":{"name":"value","in":"query","type":"string"}
	}`), nil, "/parameters")
	if err != nil || regular.Kind() != jsonvalue.ObjectKind ||
		bodies.Kind() != jsonvalue.InvalidKind {
		t.Fatalf("regular reusable parameter = %#v, %#v, %v", regular, bodies, err)
	}
	regular, bodies, err = converter.reusableParameters(conversionValue(t, `{
		"Body":{"name":"body","in":"body","schema":{}}
	}`), []string{"application/json"}, "/parameters")
	if err != nil || regular.Kind() != jsonvalue.InvalidKind ||
		bodies.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("reusable body parameter = %#v, %#v, %v", regular, bodies, err)
	}

	parameter, err := newConverter(100, 100).parameter(
		conversionValue(t, `{"name":"id","in":"query"}`), "/parameter",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := parameter.Lookup("schema"); exists {
		t.Fatalf("schema-less parameter = %#v", parameter)
	}

	pointerConverter := newConverter(100, 100)
	if _, err := pointerConverter.formRequestBody([]swaggerParameterInput{{
		value: conversionValue(t, `{
			"name":"values","in":"formData","type":"array",
			"items":{"type":"string"},"collectionFormat":"unknown"
		}`),
		pointer: "/provided",
	}}, nil, "/generated"); err != nil {
		t.Fatal(err)
	}
	if len(pointerConverter.diagnostics) != 1 ||
		pointerConverter.diagnostics[0].Pointer != "/provided/collectionFormat" {
		t.Fatalf("form parameter pointer = %#v", pointerConverter.diagnostics)
	}

	for _, test := range []struct {
		name        string
		parameter   string
		mediaType   string
		required    bool
		hasEncoding bool
		hasRequired bool
	}{
		{
			name: "file", parameter: `{"name":"file","in":"formData","type":"file"}`,
			mediaType: "multipart/form-data",
		},
		{
			name: "plain", parameter: `{"name":"value","in":"formData","type":"string"}`,
			mediaType: "application/x-www-form-urlencoded",
		},
		{
			name: "required array", parameter: `{"name":"values","in":"formData","type":"array","items":{"type":"string"},"required":true}`,
			mediaType: "application/x-www-form-urlencoded", required: true,
			hasEncoding: true, hasRequired: true,
		},
	} {
		body, err := newConverter(100, 100).formRequestBody(
			[]swaggerParameterInput{{value: conversionValue(t, test.parameter)}},
			nil, "/form",
		)
		if err != nil {
			t.Fatalf("%s form body: %v", test.name, err)
		}
		media := memberAt(t, body, "content", test.mediaType)
		schema := memberAt(t, media, "schema")
		_, hasRequired := schema.Lookup("required")
		_, hasEncoding := media.Lookup("encoding")
		requestRequired, _ := memberAt(t, body, "required").Bool()
		if requestRequired != test.required || hasRequired != test.hasRequired ||
			hasEncoding != test.hasEncoding {
			t.Errorf("%s form body = %#v", test.name, body)
		}
	}

	responseConverter := newConverter(100, 100)
	empty, err := responseConverter.response(conversionValue(t, `{}`), nil, "/response")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := empty.Lookup("content"); exists {
		t.Fatalf("empty response content = %#v", empty)
	}
	exampleOnly, err := responseConverter.response(conversionValue(t, `{
		"examples":{"text/plain":"value"}
	}`), nil, "/response")
	if err != nil {
		t.Fatal(err)
	}
	memberAt(t, exampleOnly, "content", "text/plain", "example")
	schemaOnly, err := responseConverter.response(
		conversionValue(t, `{"schema":{"type":"string"}}`), nil, "/response",
	)
	if err != nil {
		t.Fatal(err)
	}
	memberAt(t, schemaOnly, "content", "application/json", "schema")

	customContent := contentValue(
		conversionValue(t, `{}`), []string{"text/plain"}, jsonvalue.Value{},
	)
	if _, exists := customContent.Lookup("application/json"); exists {
		t.Fatalf("custom content gained default media type = %#v", customContent)
	}
	memberAt(t, customContent, "text/plain")
}

func TestSwaggerOperationParameterCombinationEdges(t *testing.T) {
	t.Parallel()

	newConverter := func(documentNodes int) *swagger20Converter {
		return &swagger20Converter{
			ctx: context.Background(), maxDocumentNodes: documentNodes,
			maxSchemaNodes: 100,
			bodyRefs: map[string]struct{}{
				"#/parameters/Body": {},
			},
			formRefs: map[string]jsonvalue.Value{
				"#/parameters/Form": conversionValue(t, `{"name":"form","in":"formData","type":"string"}`),
			},
			parameterRefs: map[string]jsonvalue.Value{},
		}
	}
	inherited := []swaggerParameterInput{
		{value: conversionValue(t, `{"name":"kept","in":"query","type":"string"}`), pointer: "/kept"},
		{value: conversionValue(t, `{"name":"replaced","in":"query","type":"string"}`), pointer: "/old"},
	}
	if _, _, err := newConverter(100).operationParameters(
		conversionValue(t, `[{"name":"replaced","in":"query","type":"string"}]`),
		nil, "/parameters", inherited,
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newConverter(100).operationParameters(
		conversionValue(t, `[
			{"$ref":"#/parameters/Body"},
			{"$ref":"#/parameters/Body"}
		]`), nil, "/parameters", nil,
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newConverter(100).operationParameters(
		conversionValue(t, `[{"$ref":"#/parameters/Form"}]`),
		nil, "/parameters", nil,
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newConverter(0).operationParameters(
		conversionValue(t, `[{"name":"body","in":"body","schema":{}}]`),
		nil, "/parameters", nil,
	); err != ErrLimitExceeded {
		t.Fatalf("body parameter error = %v", err)
	}
	if _, _, err := newConverter(0).operationParameters(
		conversionValue(t, `[{"name":"value","in":"query","type":"string"}]`),
		nil, "/parameters", nil,
	); err != ErrLimitExceeded {
		t.Fatalf("regular parameter error = %v", err)
	}
	if parameters, body, err := newConverter(100).operationParameters(
		conversionValue(t, `[
			{"name":"body","in":"body","schema":{}},
			{"name":"form","in":"formData","type":"string"}
		]`), nil, "/parameters", nil,
	); err != nil || parameters.Kind() != jsonvalue.InvalidKind ||
		body.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("body and form conflict = %#v, %#v, %v", parameters, body, err)
	}
	if _, _, err := newConverter(0).operationParameters(
		conversionValue(t, `[{"name":"form","in":"formData","type":"string"}]`),
		nil, "/parameters", nil,
	); err != ErrLimitExceeded {
		t.Fatalf("form body error = %v", err)
	}
}

func TestSwaggerReusableAndFormParameterEdges(t *testing.T) {
	t.Parallel()

	newConverter := func(documentNodes int, schemaNodes int) *swagger20Converter {
		return &swagger20Converter{
			ctx: context.Background(), maxDocumentNodes: documentNodes,
			maxSchemaNodes: schemaNodes,
		}
	}
	for _, raw := range []string{
		`{"Body":{"name":"body","in":"body","schema":{}}}`,
		`{"Value":{"name":"value","in":"query","type":"string"}}`,
	} {
		if _, _, err := newConverter(0, 100).reusableParameters(
			conversionValue(t, raw), nil, "/parameters",
		); err != ErrLimitExceeded {
			t.Errorf("reusable parameters %s error = %v", raw, err)
		}
	}
	if _, _, err := newConverter(100, 100).reusableParameters(
		conversionValue(t, `{"Form":{"name":"form","in":"formData","type":"string"}}`),
		nil, "/parameters",
	); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"name":"file","in":"formData","type":"file"}`,
		`{"name":"value","in":"formData","type":"string"}`,
	} {
		if _, err := newConverter(100, 100).formRequestBody(
			[]swaggerParameterInput{{value: conversionValue(t, raw)}}, nil, "/form",
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := newConverter(1, 100).formRequestBody(
		[]swaggerParameterInput{{
			value: conversionValue(t, `{"name":"value","in":"formData","type":"string"}`),
		}}, nil, "/form",
	); err != ErrLimitExceeded {
		t.Fatalf("form parameter propagation error = %v", err)
	}
	if _, err := newConverter(1, 100).response(
		conversionValue(t, `{"headers":{"X-Value":{"type":"string"}}}`),
		nil, "/response",
	); err != ErrLimitExceeded {
		t.Fatalf("response header propagation error = %v", err)
	}
	if _, err := newConverter(0, 100).responseMap(
		conversionValue(t, `{"200":{"description":"ok"}}`), nil, "/responses",
	); err != ErrLimitExceeded {
		t.Fatalf("response map error = %v", err)
	}
	if _, err := newConverter(100, 1).schema(
		conversionValue(t, `{"properties":{"value":{}}}`), "/schema",
	); err != ErrLimitExceeded {
		t.Fatalf("schema map propagation error = %v", err)
	}
	if _, err := newConverter(100, 1).schema(
		conversionValue(t, `{"items":{}}`), "/schema",
	); err != ErrLimitExceeded {
		t.Fatalf("schema child propagation error = %v", err)
	}
	if _, err := newConverter(0, 100).securitySchemes(
		conversionValue(t, `{"Basic":{"type":"basic"}}`), "/security",
	); err != ErrLimitExceeded {
		t.Fatalf("security scheme map error = %v", err)
	}
}

func TestSwaggerUpgradePreservesMembersAfterSkippedInputs(t *testing.T) {
	t.Parallel()

	target, _ := openapi.ParseVersion("3.0.4")
	converted, _, err := convertSwagger20Root(
		context.Background(),
		conversionValue(t, `{
			"swagger":"2.0",
			"host":"api.example.test",
			"components":{},
			"paths":{"/pets":{"x-before":true,"get":{"responses":{}}}}
		}`),
		target,
		1_000,
		1_000,
	)
	if err != nil {
		t.Fatal(err)
	}
	memberAt(t, converted, "paths", "/pets", "get")
	for _, name := range []string{"host", "components"} {
		if _, exists := converted.Lookup(name); exists {
			t.Fatalf("discarded Swagger root member %q was retained in %#v", name, converted)
		}
	}
	if _, exists := memberAt(t, converted, "paths", "/pets").Lookup("x-before"); !exists {
		t.Fatalf("custom path member was discarded in %#v", converted)
	}

	converter := &swagger20Converter{
		ctx: context.Background(), maxDocumentNodes: 1_000,
		maxSchemaNodes: 1_000,
		bodyRefs:       map[string]struct{}{"#/parameters/Body": {}},
		formRefs: map[string]jsonvalue.Value{
			"#/parameters/Form": conversionValue(t, `{"name":"form","in":"formData","type":"string"}`),
		},
		parameterRefs: map[string]jsonvalue.Value{},
	}

	servers, exists := converter.servers(conversionValue(t, `{
		"host":"api.example.test","schemes":[true,"https"]
	}`))
	if !exists {
		t.Fatal("valid scheme after malformed scheme produced no server")
	}
	serverValues, _ := servers.Elements()
	if len(serverValues) != 1 || textValue(t, memberAt(t, serverValues[0], "url")) != "https://api.example.test" {
		t.Fatalf("servers = %#v", servers)
	}

	regular, special, err := converter.splitPathParameters(
		conversionValue(t, `[
			{"$ref":"#/parameters/Body"},
			{"name":"trace","in":"query","type":"string"}
		]`),
		"/parameters",
	)
	if err != nil {
		t.Fatal(err)
	}
	regularValues, _ := regular.Elements()
	if len(special) != 1 || len(regularValues) != 1 {
		t.Fatalf("split parameters = %#v, %#v", regular, special)
	}

	for _, raw := range []string{
		`[
			{"$ref":"#/parameters/Body"},
			{"$ref":"#/parameters/Body"},
			{"name":"trace","in":"query","type":"string"}
		]`,
		`[
			{"$ref":"#/parameters/Form"},
			{"name":"trace","in":"query","type":"string"}
		]`,
	} {
		parameters, _, operationErr := converter.operationParameters(
			conversionValue(t, raw), nil, "/parameters", nil,
		)
		if operationErr != nil {
			t.Fatal(operationErr)
		}
		values, _ := parameters.Elements()
		if len(values) != 1 || textValue(t, memberAt(t, values[0], "name")) != "trace" {
			t.Fatalf("operation parameters = %#v", parameters)
		}
	}
}

func TestSwaggerUpgradePreservesReusableAndParameterMembersAfterSkippedInputs(t *testing.T) {
	t.Parallel()

	converter := &swagger20Converter{
		ctx: context.Background(), maxDocumentNodes: 1_000, maxSchemaNodes: 1_000,
	}
	parameters, bodies, err := converter.reusableParameters(
		conversionValue(t, `{
			"Body":{"name":"body","in":"body","schema":{"type":"string"}},
			"Form":{"name":"form","in":"formData","type":"string"},
			"Trace":{"name":"trace","in":"query","type":"string"}
		}`),
		nil,
		"/parameters",
	)
	if err != nil {
		t.Fatal(err)
	}
	memberAt(t, parameters, "Trace")
	memberAt(t, bodies, "Body")

	parameter, err := converter.parameter(
		conversionValue(t, `{
			"name":"tags","in":"query","collectionFormat":"csv",
			"type":"array","items":{"type":"string"}
		}`),
		"/parameter",
	)
	if err != nil {
		t.Fatal(err)
	}
	if textValue(t, memberAt(t, parameter, "schema", "type")) != "array" {
		t.Fatalf("parameter schema = %#v", parameter)
	}
	if _, exists := parameter.Lookup("collectionFormat"); exists {
		t.Fatalf("Swagger collection format was retained in %#v", parameter)
	}

	oauth, err := converter.oauth2SecurityScheme(
		conversionValue(t, `{
			"type":"oauth2","flow":"password",
			"authUrl":"https://auth.example.test/authorize",
			"tokenUrl":"https://auth.example.test/token","scopes":{},
			"x-after":true
		}`),
		"/security",
	)
	if err != nil {
		t.Fatal(err)
	}
	if after, exists := oauth.Lookup("x-after"); !exists || after.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("OAuth member after Swagger fields = %#v", oauth)
	}
	for _, name := range []string{"flow", "authUrl", "tokenUrl", "scopes"} {
		if _, exists := oauth.Lookup(name); exists {
			t.Fatalf("Swagger OAuth field %q was retained in %#v", name, oauth)
		}
	}
}

func TestSwaggerDowngradePureClassificationEdges(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`true`,
		`{"$ref":"#/components/parameters/Value"}`,
		`{"in":"query","schema":{"$ref":"#/components/schemas/Value"}}`,
	} {
		if swaggerParameterSupported(conversionValue(t, raw)) {
			t.Errorf("unsupported parameter %s was accepted", raw)
		}
	}
	if !swaggerParameterSupported(conversionValue(
		t, `{"in":"query","content":{"application/json":{"schema":{"type":"string"}}}}`,
	)) {
		t.Fatal("content-backed primitive parameter was rejected")
	}
	if !swaggerParameterSchemaField("x-value") ||
		swaggerParameterSchemaField("notSupported") {
		t.Fatal("Swagger parameter schema field classification failed")
	}
	for _, test := range []struct {
		location   string
		style      string
		explode    bool
		hasExplode bool
		array      bool
		want       string
		valid      bool
	}{
		{location: "header", style: "form", array: true},
		{location: "query", style: "spaceDelimited", array: true, want: "ssv", valid: true},
		{location: "query", style: "pipeDelimited", array: true, want: "pipes", valid: true},
		{location: "query", style: "spaceDelimited", explode: true, hasExplode: true, array: true},
		{location: "query", style: "unknown", array: true},
	} {
		got, valid := swaggerCollectionFormat(
			test.location, test.style, test.explode, test.hasExplode, test.array,
		)
		if got != test.want || valid != test.valid {
			t.Errorf("collection format %#v = %q, %t", test, got, valid)
		}
	}
	if got := (&oas30SwaggerConverter{}).reference(
		jsonvalue.Boolean(true), "/reference",
	); got.Kind() != jsonvalue.BooleanKind {
		t.Fatalf("non-string reference = %#v", got)
	}
	values := uniqueStrings([]jsonvalue.Value{
		jsonvalue.Boolean(true),
		conversionValue(t, `"one"`),
		conversionValue(t, `"one"`),
	})
	if len(values) != 1 {
		t.Fatalf("unique strings = %#v", values)
	}
}

func TestSwaggerDowngradeCoversScalarAndVisitFailures(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{},
			requestBodies:    map[string]jsonvalue.Value{},
			securitySchemes:  map[string]bool{},
			parameters:       map[string]bool{},
		}
	}
	scalar := jsonvalue.Boolean(true)
	converter := newConverter(100)
	for _, call := range []func() error{
		func() error { _, err := converter.parameterMap(scalar, "/parameters"); return err },
		func() error { _, _, err := converter.requestBodyMap(scalar, "/bodies"); return err },
		func() error { _, err := converter.pathMap(scalar, "/paths"); return err },
		func() error { _, err := converter.pathItem(scalar, "/path"); return err },
		func() error { _, err := converter.operation(scalar, "/operation"); return err },
		func() error { _, err := converter.parameterArray(scalar, "/parameters"); return err },
		func() error { _, err := converter.parameter(scalar, "/parameter"); return err },
		func() error { _, _, err := converter.requestBody(scalar, "/body"); return err },
		func() error { _, _, err := converter.responseMap(scalar, "/responses"); return err },
		func() error { _, _, err := converter.response(scalar, "/response"); return err },
		func() error { _, err := converter.headerMap(scalar, "/headers"); return err },
		func() error { _, err := converter.schemaMap(scalar, "/schemas"); return err },
		func() error { _, err := converter.securitySchemeMap(scalar, "/security"); return err },
		func() error { converter.securityRequirements(scalar, "/security"); return nil },
		func() error { _, err := converter.schema(scalar, "/schema"); return err },
		func() error { _, err := converter.schemaArray(scalar, "/allOf"); return err },
	} {
		if err := call(); err != nil {
			t.Errorf("scalar visitor error = %v", err)
		}
	}
	for index, call := range []func(*oas30SwaggerConverter) error{
		func(c *oas30SwaggerConverter) error {
			_, err := c.server(conversionValue(t, `[]`), "/servers")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, err := c.components(conversionValue(t, `{}`), "/components")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, err := c.pathItem(conversionValue(t, `{}`), "/path")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, err := c.operation(conversionValue(t, `{}`), "/operation")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, err := c.parameter(conversionValue(t, `{}`), "/parameter")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, _, err := c.requestBody(conversionValue(t, `{}`), "/body")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, _, err := c.response(conversionValue(t, `{}`), "/response")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, _, err := c.securityScheme(conversionValue(t, `{}`), "/security")
			return err
		},
		func(c *oas30SwaggerConverter) error {
			_, err := c.schema(conversionValue(t, `{}`), "/schema")
			return err
		},
	} {
		if err := call(newConverter(0)); err != ErrLimitExceeded {
			t.Errorf("visit failure %d = %v", index, err)
		}
	}
}

func TestSwaggerDowngradeExactDecisionBoundaries(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{},
			requestBodies:    map[string]jsonvalue.Value{},
			securitySchemes:  map[string]bool{},
			parameters:       map[string]bool{},
		}
	}

	exact := newConverter(1)
	if err := exact.visit(); err != nil {
		t.Fatalf("exact downgrade node limit = %v", err)
	}
	if err := exact.visit(); err != ErrLimitExceeded {
		t.Fatalf("downgrade node beyond exact limit = %v", err)
	}

	if !swaggerParameterSupported(conversionValue(t, `{
		"in":"query","content":{}
	}`)) {
		t.Fatal("empty parameter content was rejected as an object schema")
	}
	if swaggerParameterSupported(conversionValue(t, `{
		"in":"query","content":{"application/json":{"schema":{"type":"object"}}}
	}`)) {
		t.Fatal("content-backed object parameter was accepted")
	}

	converter := newConverter(100)
	converter.indexRequestBodyNames(conversionValue(t, `{
		"components":{
			"parameters":{"Pet":{},"PetRequestBody":{},"PetRequestBody2":{}},
			"requestBodies":{"Pet":{"content":{"application/json":{"schema":{}}}}}
		}
	}`))
	if got := converter.requestBodyNames["#/components/requestBodies/Pet"]; got != "PetRequestBody3" {
		t.Fatalf("third colliding request body name = %q", got)
	}

	serverConverter := newConverter(100)
	serverMembers, err := serverConverter.server(conversionValue(t, `[
		{"url":"https://example.test/base"},{"url":"https://ignored.test"}
	]`), "/servers")
	if err != nil || len(serverMembers) == 0 || len(serverConverter.diagnostics) != 1 ||
		serverConverter.diagnostics[0].Pointer != "/servers/1" {
		t.Fatalf("multiple servers = %#v, %#v, %v", serverMembers, serverConverter.diagnostics, err)
	}
	invalidServer := newConverter(100)
	serverMembers, err = invalidServer.server(
		conversionValue(t, `[{"url":"relative"}]`), "/servers",
	)
	if err != nil || len(serverMembers) != 0 || len(invalidServer.diagnostics) != 1 {
		t.Fatalf("relative server = %#v, %#v, %v", serverMembers, invalidServer.diagnostics, err)
	}

	operationConverter := newConverter(100)
	emptyOperation, err := operationConverter.operation(conversionValue(t, `{}`), "/operation")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := emptyOperation.Lookup("parameters"); exists {
		t.Fatalf("empty operation parameters = %#v", emptyOperation)
	}
	explicitEmpty, err := operationConverter.operation(
		conversionValue(t, `{"parameters":[]}`), "/operation",
	)
	if err != nil {
		t.Fatal(err)
	}
	if parameters, exists := explicitEmpty.Lookup("parameters"); !exists ||
		parameters.Kind() != jsonvalue.ArrayKind {
		t.Fatalf("explicit empty parameters = %#v", explicitEmpty)
	}
	withMedia, err := operationConverter.operation(conversionValue(t, `{
		"requestBody":{"content":{"application/json":{"schema":{"type":"string"}}}},
		"responses":{"200":{"content":{"text/plain":{"schema":{"type":"string"}}}}}
	}`), "/operation")
	if err != nil {
		t.Fatal(err)
	}
	consumes, _ := memberAt(t, withMedia, "consumes").Elements()
	produces, _ := memberAt(t, withMedia, "produces").Elements()
	if len(consumes) != 1 || textValue(t, consumes[0]) != "application/json" ||
		len(produces) != 1 || textValue(t, produces[0]) != "text/plain" {
		t.Fatalf("operation media types = %#v", withMedia)
	}

	emptyParameters, mediaTypes, err := operationConverter.operationRequestBody(
		conversionValue(t, `{"content":{}}`), "/body",
	)
	if err != nil || len(mediaTypes) != 0 || len(emptyParameters) != 0 {
		t.Fatalf("empty request body content = %#v, %#v, %v", emptyParameters, mediaTypes, err)
	}
	formBody := conversionValue(t, `{
		"content":{"multipart/form-data":{"schema":{"type":"object","properties":{}}}}
	}`)
	parameters, mediaTypes, err := operationConverter.operationRequestBody(formBody, "/body")
	if err != nil || len(parameters) != 0 || len(mediaTypes) != 1 {
		t.Fatalf("form request body = %#v, %#v, %v", parameters, mediaTypes, err)
	}

	for _, test := range []struct {
		style string
		valid bool
	}{
		{style: "", valid: true},
		{style: "form", valid: true},
		{style: "simple", valid: true},
		{style: "deepObject"},
	} {
		format, valid := swaggerCollectionFormat(
			"query", test.style, false, false, false,
		)
		if format != "" || valid != test.valid {
			t.Errorf("scalar collection style %q = %q, %t", test.style, format, valid)
		}
	}

	responseConverter := newConverter(100)
	withoutExample, _, err := responseConverter.response(conversionValue(t, `{
		"content":{"application/json":{"schema":{"type":"string"}}}
	}`), "/response")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := withoutExample.Lookup("examples"); exists {
		t.Fatalf("response without example = %#v", withoutExample)
	}
	withExample, _, err := responseConverter.response(conversionValue(t, `{
		"content":{"application/json":{"example":"value"}}
	}`), "/response")
	if err != nil {
		t.Fatal(err)
	}
	if textValue(t, memberAt(t, withExample, "examples", "application/json")) != "value" {
		t.Fatalf("response example = %#v", withExample)
	}

	for _, location := range []string{"header", "query"} {
		value := conversionValue(t, `{"type":"apiKey","in":"`+location+`"}`)
		if !swaggerSecuritySchemeSupported(value) {
			t.Errorf("API key location %q was not supported", location)
		}
		if _, supported, err := newConverter(100).securityScheme(
			value, "/security",
		); err != nil || !supported {
			t.Errorf("API key conversion %q = %t, %v", location, supported, err)
		}
	}
	for _, flow := range []string{
		"implicit", "password", "clientCredentials", "authorizationCode",
	} {
		value := conversionValue(t, `{"type":"oauth2","flows":{"`+flow+`":{}}}`)
		if !swaggerSecuritySchemeSupported(value) {
			t.Errorf("OAuth flow %q was not supported", flow)
		}
	}
	for _, raw := range []string{
		`{"type":"oauth2"}`,
		`{"type":"oauth2","flows":{}}`,
		`{"type":"oauth2","flows":{"deviceAuthorization":{}}}`,
	} {
		if swaggerSecuritySchemeSupported(conversionValue(t, raw)) {
			t.Errorf("unsupported OAuth scheme %s was accepted", raw)
		}
	}
}

func TestSwaggerDowngradeServersComponentsAndPaths(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}
	for _, raw := range []string{
		`[]`,
		`[{"url":"https://example.test/{missing}"}]`,
		`[{"url":"relative"}]`,
		`[{"url":"https://user@example.test/path"}]`,
	} {
		if _, err := newConverter(100).server(conversionValue(t, raw), "/servers"); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := newConverter(100).components(
		conversionValue(t, `{"unknown":{}}`), "/components",
	); err != nil || len(result) != 0 {
		t.Fatalf("unknown components = %#v, %v", result, err)
	}
	for _, raw := range []string{
		`{"servers":[],"parameters":[]}`,
		`{"get":{}}`,
	} {
		if _, err := newConverter(100).pathItem(conversionValue(t, raw), "/path"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := newConverter(1).pathItem(
		conversionValue(t, `{"get":{}}`), "/path",
	); err != ErrLimitExceeded {
		t.Fatalf("path operation error = %v", err)
	}
	if _, err := newConverter(1).pathItem(
		conversionValue(t, `{"parameters":[{}]}`), "/path",
	); err != ErrLimitExceeded {
		t.Fatalf("path parameter error = %v", err)
	}
	for _, raw := range []string{
		`{"servers":[],"callbacks":{}}`,
		`{"responses":{}}`,
		`{"parameters":[]}`,
		`{"requestBody":{}}`,
		`{"security":[]}`,
	} {
		if _, err := newConverter(100).operation(conversionValue(t, raw), "/operation"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSwaggerDowngradeFormAndParameterEdges(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			parameters: map[string]bool{}, requestBodies: map[string]jsonvalue.Value{},
			requestBodyNames: map[string]string{}, securitySchemes: map[string]bool{},
		}
	}
	media := jsonvalue.Member{
		Name:  "multipart/form-data",
		Value: conversionValue(t, `{"schema":{}}`),
	}
	if _, _, err := newConverter(0).formRequestBody(
		conversionValue(t, `{}`), media, "/body",
	); err != ErrLimitExceeded {
		t.Fatalf("form visit error = %v", err)
	}
	if _, _, err := newConverter(100).formRequestBody(
		conversionValue(t, `{"description":"value","required":true,"x-value":true}`),
		media, "/body",
	); err != nil {
		t.Fatal(err)
	}
	media.Value = conversionValue(t, `{
		"schema":{"type":"object","properties":{"value":{"type":"string"}},
			"additionalProperties":false},
		"example":{}
	}`)
	if _, _, err := newConverter(1).formRequestBody(
		conversionValue(t, `{}`), media, "/body",
	); err != ErrLimitExceeded {
		t.Fatalf("form property error = %v", err)
	}
	converter := newConverter(100)
	if got, err := converter.formParameter(
		"value", conversionValue(t, `{"type":"object"}`),
		conversionValue(t, `{}`), "/property", false,
	); err != nil || got.Kind() != jsonvalue.InvalidKind {
		t.Fatalf("object form property = %#v, %v", got, err)
	}
	if _, err := converter.formParameter(
		"value",
		conversionValue(t, `{"type":"array","items":{"type":"string"},"unsupported":true}`),
		conversionValue(t, `{"value":{"style":"unknown","headers":{}}}`),
		"/property", true,
	); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"schema":{"$ref":"#/components/schemas/Value"}}`,
		`{"content":{}}`,
		`{"deprecated":false,"allowReserved":false,"example":1,"examples":{}}`,
	} {
		if _, err := converter.parameter(conversionValue(t, raw), "/parameter"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSwaggerDowngradeDocumentAndComponentFailures(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}
	for _, raw := range []string{
		`{"openapi":"3.0.4","servers":[]}`,
		`{"openapi":"3.0.4","components":{}}`,
	} {
		if _, err := newConverter(1).document(conversionValue(t, raw)); err != ErrLimitExceeded {
			t.Errorf("document %s error = %v", raw, err)
		}
	}
	if _, err := newConverter(100).document(
		conversionValue(t, `{"openapi":"3.0.4","security":[]}`),
	); err != nil {
		t.Fatal(err)
	}
	converter := newConverter(100)
	converter.indexRequestBodyNames(conversionValue(t, `{
		"components":{
			"parameters":{"Pet":{},"PetRequestBody":{}},
			"requestBodies":{"Pet":{"content":{"application/json":{"schema":{}}}}}
		}
	}`))
	if converter.requestBodyNames["#/components/requestBodies/Pet"] !=
		"PetRequestBody2" {
		t.Fatalf("colliding request body names = %#v", converter.requestBodyNames)
	}
	if members, err := converter.components(jsonvalue.Boolean(true), "/components"); err != nil || len(members) != 0 {
		t.Fatalf("scalar components = %#v, %v", members, err)
	}
	for _, raw := range []string{
		`{"schemas":{"Value":{}}}`,
		`{"parameters":{"Value":{}}}`,
		`{"requestBodies":{"Value":{"content":{"application/json":{"schema":{}}}}}}`,
		`{"responses":{"Value":{}}}`,
		`{"securitySchemes":{"Value":{"type":"http","scheme":"basic"}}}`,
	} {
		if _, err := newConverter(1).components(conversionValue(t, raw), "/components"); err != ErrLimitExceeded {
			t.Errorf("components %s error = %v", raw, err)
		}
	}
	if _, err := newConverter(0).parameterMap(
		conversionValue(t, `{"Value":{}}`), "/parameters",
	); err != ErrLimitExceeded {
		t.Fatalf("parameter map error = %v", err)
	}
	converter = newConverter(0)
	if _, _, err := converter.requestBodyMap(
		conversionValue(t, `{"Value":{"content":{"application/json":{"schema":{}}}}}`),
		"/bodies",
	); err != ErrLimitExceeded {
		t.Fatalf("request body map error = %v", err)
	}
	if got := replaceObjectString(
		conversionValue(t, `{}`), "name", "value",
	); textValue(t, memberAt(t, got, "name")) != "value" {
		t.Fatalf("appended object string = %#v", got)
	}
}

func TestSwaggerDowngradeOperationAndBodyFailures(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}
	for _, raw := range []string{
		`{"responses":{"200":{}}}`,
		`{"parameters":[{}]}`,
		`{"requestBody":{}}`,
	} {
		if _, err := newConverter(1).operation(conversionValue(t, raw), "/operation"); err != ErrLimitExceeded {
			t.Errorf("operation %s error = %v", raw, err)
		}
	}
	if _, err := newConverter(100).operation(
		conversionValue(t, `{"security":[]}`), "/operation",
	); err != nil {
		t.Fatal(err)
	}
	converter := newConverter(100)
	if parameters, media, err := converter.operationRequestBody(
		jsonvalue.Boolean(true), "/body",
	); err != nil || len(parameters) != 0 || len(media) != 0 {
		t.Fatalf("scalar operation body = %#v, %#v, %v", parameters, media, err)
	}
	if _, _, err := newConverter(0).operationRequestBody(
		conversionValue(t, `{}`), "/body",
	); err != ErrLimitExceeded {
		t.Fatalf("operation body error = %v", err)
	}
	formValue := conversionValue(t, `{
		"content":{
			"multipart/form-data":{"schema":{"type":"object","properties":{}}},
			"application/x-www-form-urlencoded":{"schema":{"type":"object","properties":{}}}
		}
	}`)
	selected, _ := swaggerFormMediaType(formValue)
	if _, media, err := converter.operationFormRequestBody(
		formValue, selected, "/body",
	); err != nil || len(media) != 2 {
		t.Fatalf("matching form media = %#v, %v", media, err)
	}
	if _, _, err := newConverter(0).operationFormRequestBody(
		formValue, selected, "/body",
	); err != ErrLimitExceeded {
		t.Fatalf("operation form error = %v", err)
	}
	media := jsonvalue.Member{
		Name: "multipart/form-data",
		Value: conversionValue(t, `{
			"schema":{"type":"object","properties":{},"required":[]},
			"encoding":{},"example":{}
		}`),
	}
	if _, _, err := converter.formRequestBody(
		conversionValue(t, `{"required":true}`), media, "/body",
	); err != nil {
		t.Fatal(err)
	}
}

func TestSwaggerDowngradeRequestAndResponseContentEdges(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}
	converter := newConverter(100)
	for _, raw := range []string{
		`{"description":"value"}`,
		`{"content":{"multipart/form-data":{},"application/json":{}}}`,
		`{"content":{"application/json":{"schema":{"type":"string"}},"text/plain":{"schema":{"type":"integer"}}}}`,
	} {
		if _, _, err := converter.requestBody(conversionValue(t, raw), "/body"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := newConverter(1).parameter(
		conversionValue(t, `{"in":"query","schema":{"type":"string"}}`),
		"/parameter",
	); err != ErrLimitExceeded {
		t.Fatalf("parameter schema propagation error = %v", err)
	}
	if _, err := newConverter(1).parameter(
		conversionValue(t, `{"in":"query","content":{"application/json":{"schema":{"type":"string"}}}}`),
		"/parameter",
	); err != ErrLimitExceeded {
		t.Fatalf("parameter content propagation error = %v", err)
	}
	if _, err := converter.parameter(
		conversionValue(t, `{
			"in":"query","style":"unknown",
			"schema":{"type":"array","items":{"type":"string"}}
		}`), "/parameter",
	); err != nil {
		t.Fatal(err)
	}
	if _, _, err := newConverter(1).requestBody(
		conversionValue(t, `{"content":{"application/json":{"schema":{}}}}`),
		"/body",
	); err != ErrLimitExceeded {
		t.Fatalf("request body schema error = %v", err)
	}
	if _, _, err := newConverter(0).responseMap(
		conversionValue(t, `{"200":{}}`), "/responses",
	); err != ErrLimitExceeded {
		t.Fatalf("response map error = %v", err)
	}
	if _, _, err := newConverter(1).response(
		conversionValue(t, `{"headers":{"X-Value":{}}}`), "/response",
	); err != ErrLimitExceeded {
		t.Fatalf("response header error = %v", err)
	}
	if _, _, err := newConverter(1).response(
		conversionValue(t, `{"content":{"application/json":{"schema":{}}}}`),
		"/response",
	); err != ErrLimitExceeded {
		t.Fatalf("response schema error = %v", err)
	}
	for _, raw := range []string{
		`{"content":true}`,
		`{"content":{"application/json":{}}}`,
		`{"content":{"application/json":{"schema":{"type":"string"}},"text/plain":{"schema":{"type":"integer"}}}}`,
	} {
		if _, _, err := converter.response(conversionValue(t, raw), "/response"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := newConverter(0).headerMap(
		conversionValue(t, `{"X-Value":{}}`), "/headers",
	); err != ErrLimitExceeded {
		t.Fatalf("header map error = %v", err)
	}
}

func TestSwaggerDowngradeSecurityAndSchemaEdges(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}
	for _, raw := range []string{
		`true`, `{"$ref":"#/components/securitySchemes/Value"}`,
		`{"type":"apiKey","in":"cookie"}`,
		`{"type":"unknown"}`,
	} {
		if swaggerSecuritySchemeSupported(conversionValue(t, raw)) {
			t.Errorf("unsupported security scheme %s was accepted", raw)
		}
	}
	converter := newConverter(100)
	for _, raw := range []string{
		`true`,
		`{"$ref":"#/components/securitySchemes/Value"}`,
		`{"type":"apiKey","in":"cookie"}`,
		`{"type":"unknown"}`,
		`{"type":"http","scheme":"digest"}`,
		`{"type":"http","scheme":"basic","description":"value"}`,
	} {
		if _, _, err := converter.securityScheme(conversionValue(t, raw), "/security"); err != nil {
			t.Fatal(err)
		}
	}
	if got := converter.securityRequirements(
		conversionValue(t, `[true]`), "/security",
	); got.Kind() != jsonvalue.ArrayKind {
		t.Fatalf("scalar security requirement = %#v", got)
	}
	for _, raw := range []string{
		`{"type":"oauth2"}`,
		`{"type":"oauth2","flows":{"deviceAuthorization":{}}}`,
		`{"type":"oauth2","description":"value","x-value":true,"flows":{
			"implicit":{"authorizationUrl":"https://example.test","scopes":{},"refreshUrl":"https://example.test/refresh","x-flow":true},
			"password":{"tokenUrl":"https://example.test/token","scopes":{}}
		}}`,
	} {
		if _, _, err := converter.oauth2SecurityScheme(
			conversionValue(t, raw), "/security",
		); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		`{"properties":{"value":{}}}`,
		`{"items":{}}`,
		`{"allOf":[{}]}`,
	} {
		if _, err := newConverter(1).schema(conversionValue(t, raw), "/schema"); err != ErrLimitExceeded {
			t.Errorf("schema %s error = %v", raw, err)
		}
	}
	if _, err := converter.schema(
		conversionValue(t, `{"discriminator":{"mapping":{}}}`), "/schema",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := newConverter(0).schemaArray(
		conversionValue(t, `[{}]`), "/allOf",
	); err != ErrLimitExceeded {
		t.Fatalf("schema array error = %v", err)
	}
}

func TestSwaggerDowngradePreservesMembersAfterDiscardedInputs(t *testing.T) {
	t.Parallel()

	newConverter := func(maxNodes int) *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: maxNodes,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}

	if got := availableSwaggerParameterName("Body", map[string]struct{}{
		"Body2": {}, "Body3": {},
	}); got != "Body4" {
		t.Fatalf("available request body name = %q", got)
	}

	parameterConverter := newConverter(100)
	parameters, err := parameterConverter.parameterMap(conversionValue(t, `{
		"discarded":{"in":"query","schema":{"type":"object"}},
		"retained":{"name":"value","in":"query","schema":{"type":"string"}}
	}`), "/parameters")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := parameters.Lookup("discarded"); exists {
		t.Fatal("unsupported parameter was retained")
	}
	if _, exists := parameters.Lookup("retained"); !exists {
		t.Fatalf("parameter after discarded input = %#v", parameters)
	}

	bodyConverter := newConverter(100)
	bodies, mediaTypes, err := bodyConverter.requestBodyMap(conversionValue(t, `{
		"Form":{"content":{"multipart/form-data":{"schema":{"type":"object","properties":{}}}}},
		"Empty":{"content":{}},
		"EmptyReference":{"$ref":""},
		"MalformedReference":{"$ref":true},
		"Payload":{"content":{"application/json":{"schema":{"type":"string"}}}}
	}`), "/requestBodies")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := bodies.Lookup("Form"); exists {
		t.Fatal("reusable form body was retained")
	}
	if _, exists := bodies.Lookup("Empty"); exists {
		t.Fatal("empty reusable body was retained")
	}
	if _, exists := bodies.Lookup("EmptyReference"); exists {
		t.Fatal("empty reusable body reference was retained")
	}
	if _, exists := bodies.Lookup("MalformedReference"); exists {
		t.Fatal("malformed reusable body reference was retained")
	}
	if _, exists := bodies.Lookup("Payload"); !exists || len(mediaTypes) != 1 ||
		textValue(t, mediaTypes[0]) != "application/json" {
		t.Fatalf("body after inlined form body = %#v, %#v", bodies, mediaTypes)
	}

	pathConverter := newConverter(100)
	path, err := pathConverter.pathItem(conversionValue(t, `{
		"servers":[],"summary":"discarded",
		"get":{"responses":{"204":{"description":"retained"}}}
	}`), "/path")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := path.Lookup("servers"); exists {
		t.Fatal("path servers were retained")
	}
	if textValue(t, memberAt(t, path, "get", "responses", "204", "description")) != "retained" {
		t.Fatalf("operation after discarded path members = %#v", path)
	}

	operationConverter := newConverter(100)
	operation, err := operationConverter.operation(conversionValue(t, `{
		"servers":[],"callbacks":{},
		"responses":{"200":{"description":"retained"}}
	}`), "/operation")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := operation.Lookup("servers"); exists {
		t.Fatal("operation servers were retained")
	}
	if textValue(t, memberAt(t, operation, "responses", "200", "description")) != "retained" {
		t.Fatalf("response after discarded operation members = %#v", operation)
	}

	formConverter := newConverter(100)
	formValue := conversionValue(t, `{"content":{
		"multipart/form-data":{"schema":{"type":"object","properties":{}}},
		"application/x-www-form-urlencoded":{"schema":{"type":"object","properties":{"different":{"type":"string"}}}},
		"application/json":{"schema":{"type":"object"}}
	}}`)
	selected, _ := swaggerFormMediaType(formValue)
	_, formMediaTypes, err := formConverter.operationFormRequestBody(
		formValue, selected, "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(formMediaTypes) != 1 || textValue(t, formMediaTypes[0]) != "multipart/form-data" ||
		len(formConverter.diagnostics) != 2 {
		t.Fatalf("form media after incompatible entries = %#v, %#v", formMediaTypes, formConverter.diagnostics)
	}

	requestConverter := newConverter(100)
	request, requestMediaTypes, err := requestConverter.requestBody(conversionValue(t, `{
		"content":{"text/plain":{},"application/json":{"schema":{"type":"string"}}}
	}`), "/body")
	if err != nil {
		t.Fatal(err)
	}
	if textValue(t, memberAt(t, request, "schema", "type")) != "string" ||
		len(requestMediaTypes) != 2 {
		t.Fatalf("request schema after schemaless media = %#v, %#v", request, requestMediaTypes)
	}

	for name, raw := range map[string]string{
		"missing content":   `{}`,
		"malformed content": `{"content":true}`,
		"empty content":     `{"content":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			emptyRequestConverter := newConverter(100)
			emptyRequest, emptyMediaTypes, requestErr := emptyRequestConverter.requestBody(
				conversionValue(t, raw), "/body",
			)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			if emptyRequest.Kind() != jsonvalue.InvalidKind || len(emptyMediaTypes) != 0 ||
				len(emptyRequestConverter.diagnostics) != 1 ||
				emptyRequestConverter.diagnostics[0].Code != "openapi.convert.request-body-removed" ||
				emptyRequestConverter.diagnostics[0].Pointer != "/body/content" {
				t.Fatalf(
					"empty request conversion = %#v, %#v, %#v",
					emptyRequest,
					emptyMediaTypes,
					emptyRequestConverter.diagnostics,
				)
			}
		})
	}

	invalidMediaRequestConverter := newConverter(100)
	invalidMediaRequest, invalidRequestMediaTypes, err := invalidMediaRequestConverter.requestBody(
		conversionValue(t, `{"content":{"0":{}}}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if memberAt(t, invalidMediaRequest, "schema").Kind() != jsonvalue.ObjectKind ||
		len(invalidRequestMediaTypes) != 0 ||
		len(invalidMediaRequestConverter.diagnostics) != 1 ||
		invalidMediaRequestConverter.diagnostics[0].Code != "openapi.convert.media-type-removed" ||
		invalidMediaRequestConverter.diagnostics[0].Pointer != "/body/content/0" {
		t.Fatalf(
			"invalid request media conversion = %#v, %#v, %#v",
			invalidMediaRequest,
			invalidRequestMediaTypes,
			invalidMediaRequestConverter.diagnostics,
		)
	}

	malformedMediaRequestConverter := newConverter(100)
	malformedMediaRequest, malformedRequestMediaTypes, err := malformedMediaRequestConverter.requestBody(
		conversionValue(t, `{"content":{"bad media":{}}}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if memberAt(t, malformedMediaRequest, "schema").Kind() != jsonvalue.ObjectKind ||
		len(malformedRequestMediaTypes) != 0 ||
		len(malformedMediaRequestConverter.diagnostics) != 1 ||
		malformedMediaRequestConverter.diagnostics[0].Code != "openapi.convert.media-type-removed" ||
		malformedMediaRequestConverter.diagnostics[0].Pointer != "/body/content/bad media" {
		t.Fatalf(
			"malformed request media conversion = %#v, %#v, %#v",
			malformedMediaRequest,
			malformedRequestMediaTypes,
			malformedMediaRequestConverter.diagnostics,
		)
	}

	emptyReference := "#/components/requestBodies/Empty"
	emptyReferenceConverter := newConverter(100)
	emptyReferenceConverter.requestBodies[emptyReference] = conversionValue(t, `{"content":{}}`)
	emptyReferenceBody, emptyReferenceMediaTypes, err := emptyReferenceConverter.requestBody(
		conversionValue(t, `{"$ref":"`+emptyReference+`"}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if emptyReferenceBody.Kind() != jsonvalue.InvalidKind || len(emptyReferenceMediaTypes) != 0 ||
		len(emptyReferenceConverter.diagnostics) != 1 ||
		emptyReferenceConverter.diagnostics[0].Code != "openapi.convert.request-body-removed" ||
		emptyReferenceConverter.diagnostics[0].Pointer != "/body/$ref" {
		t.Fatalf(
			"empty request reference conversion = %#v, %#v, %#v",
			emptyReferenceBody,
			emptyReferenceMediaTypes,
			emptyReferenceConverter.diagnostics,
		)
	}

	emptyRootReferenceConverter := newConverter(100)
	emptyRootReferenceBody, emptyRootReferenceMediaTypes, err := emptyRootReferenceConverter.requestBody(
		conversionValue(t, `{"$ref":""}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if emptyRootReferenceBody.Kind() != jsonvalue.InvalidKind || len(emptyRootReferenceMediaTypes) != 0 ||
		len(emptyRootReferenceConverter.diagnostics) != 1 ||
		emptyRootReferenceConverter.diagnostics[0].Code !=
			"openapi.convert.request-body-reference-removed" ||
		emptyRootReferenceConverter.diagnostics[0].Pointer != "/body/$ref" {
		t.Fatalf(
			"empty root request reference conversion = %#v, %#v, %#v",
			emptyRootReferenceBody,
			emptyRootReferenceMediaTypes,
			emptyRootReferenceConverter.diagnostics,
		)
	}

	unknownLocalReferenceConverter := newConverter(100)
	unknownLocalReferenceBody, unknownLocalReferenceMediaTypes, err :=
		unknownLocalReferenceConverter.requestBody(
			conversionValue(t, `{"$ref":"#/components"}`), "/body",
		)
	if err != nil {
		t.Fatal(err)
	}
	if unknownLocalReferenceBody.Kind() != jsonvalue.InvalidKind ||
		len(unknownLocalReferenceMediaTypes) != 0 ||
		len(unknownLocalReferenceConverter.diagnostics) != 1 ||
		unknownLocalReferenceConverter.diagnostics[0].Code !=
			"openapi.convert.request-body-reference-removed" ||
		unknownLocalReferenceConverter.diagnostics[0].Pointer != "/body/$ref" {
		t.Fatalf(
			"unknown local request reference conversion = %#v, %#v, %#v",
			unknownLocalReferenceBody,
			unknownLocalReferenceMediaTypes,
			unknownLocalReferenceConverter.diagnostics,
		)
	}

	aliasReference := "#/components/requestBodies/Alias"
	aliasReferenceConverter := newConverter(100)
	aliasReferenceConverter.requestBodies[emptyReference] = conversionValue(t, `{"content":{}}`)
	aliasReferenceConverter.requestBodies[aliasReference] = conversionValue(t, `{"$ref":"`+emptyReference+`"}`)
	aliasReferenceBody, aliasReferenceMediaTypes, err := aliasReferenceConverter.requestBody(
		conversionValue(t, `{"$ref":"`+aliasReference+`"}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if aliasReferenceBody.Kind() != jsonvalue.InvalidKind || len(aliasReferenceMediaTypes) != 0 ||
		len(aliasReferenceConverter.diagnostics) != 1 ||
		aliasReferenceConverter.diagnostics[0].Code != "openapi.convert.request-body-removed" {
		t.Fatalf(
			"aliased request reference conversion = %#v, %#v, %#v",
			aliasReferenceBody,
			aliasReferenceMediaTypes,
			aliasReferenceConverter.diagnostics,
		)
	}

	emptyAliasReference := "#/components/requestBodies/EmptyAlias"
	emptyAliasConverter := newConverter(100)
	emptyAliasConverter.requestBodies[emptyAliasReference] = conversionValue(t, `{"$ref":""}`)
	emptyAliasBody, emptyAliasMediaTypes, err := emptyAliasConverter.requestBody(
		conversionValue(t, `{"$ref":"`+emptyAliasReference+`"}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if emptyAliasBody.Kind() != jsonvalue.InvalidKind || len(emptyAliasMediaTypes) != 0 ||
		len(emptyAliasConverter.diagnostics) != 1 ||
		emptyAliasConverter.diagnostics[0].Code != "openapi.convert.request-body-removed" {
		t.Fatalf(
			"empty alias request reference conversion = %#v, %#v, %#v",
			emptyAliasBody,
			emptyAliasMediaTypes,
			emptyAliasConverter.diagnostics,
		)
	}

	contentReference := "#/components/requestBodies/Content"
	contentReferenceConverter := newConverter(100)
	contentReferenceConverter.requestBodies[contentReference] = conversionValue(t, `{
		"content":{"application/json":{"schema":{"type":"string"}}}
	}`)
	contentReferenceBody, contentReferenceMediaTypes, err := contentReferenceConverter.requestBody(
		conversionValue(t, `{"$ref":"`+contentReference+`"}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if contentReferenceBody.Kind() != jsonvalue.ObjectKind || len(contentReferenceMediaTypes) != 0 ||
		len(contentReferenceConverter.diagnostics) != 0 {
		t.Fatalf(
			"content request reference conversion = %#v, %#v, %#v",
			contentReferenceBody,
			contentReferenceMediaTypes,
			contentReferenceConverter.diagnostics,
		)
	}

	externalAliasReference := "#/components/requestBodies/ExternalAlias"
	externalAliasConverter := newConverter(100)
	externalAliasConverter.requestBodies[externalAliasReference] = conversionValue(t, `{
		"$ref":"https://example.test/request-bodies.json#/Payload"
	}`)
	externalAliasBody, externalAliasMediaTypes, err := externalAliasConverter.requestBody(
		conversionValue(t, `{"$ref":"`+externalAliasReference+`"}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if externalAliasBody.Kind() != jsonvalue.ObjectKind || len(externalAliasMediaTypes) != 0 ||
		len(externalAliasConverter.diagnostics) != 1 ||
		externalAliasConverter.diagnostics[0].Code != "openapi.convert.external-reference" ||
		textValue(t, memberAt(t, externalAliasBody, "$ref")) !=
			"https://example.test/request-bodies.json#/Payload" {
		t.Fatalf(
			"external request reference conversion = %#v, %#v, %#v",
			externalAliasBody,
			externalAliasMediaTypes,
			externalAliasConverter.diagnostics,
		)
	}

	aliasMapConverter := newConverter(100)
	aliasMapConverter.requestBodies[contentReference] = contentReferenceConverter.requestBodies[contentReference]
	aliasMapConverter.requestBodies[aliasReference] = conversionValue(t, `{"$ref":"`+contentReference+`"}`)
	aliasMapConverter.requestBodies[externalAliasReference] = externalAliasConverter.requestBodies[externalAliasReference]
	aliasMap, _, err := aliasMapConverter.requestBodyMap(conversionValue(t, `{
		"Alias":{"$ref":"`+contentReference+`"},
		"ExternalAlias":{"$ref":"https://example.test/request-bodies.json#/Payload"}
	}`), "/requestBodies")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := aliasMap.Lookup("Alias"); !exists {
		t.Fatal("resolvable local request body alias was not flattened")
	}
	if _, exists := aliasMap.Lookup("ExternalAlias"); exists {
		t.Fatal("external request body alias was retained as an invalid reusable parameter")
	}

	malformedAliasReference := "#/components/requestBodies/MalformedAlias"
	malformedAliasConverter := newConverter(100)
	malformedAliasConverter.requestBodies[malformedAliasReference] = conversionValue(t, `{"$ref":true}`)
	malformedAliasBody, malformedAliasMediaTypes, err := malformedAliasConverter.requestBody(
		conversionValue(t, `{"$ref":"`+malformedAliasReference+`"}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if malformedAliasBody.Kind() != jsonvalue.InvalidKind || len(malformedAliasMediaTypes) != 0 ||
		len(malformedAliasConverter.diagnostics) != 1 ||
		malformedAliasConverter.diagnostics[0].Code != "openapi.convert.request-body-removed" {
		t.Fatalf(
			"malformed request reference conversion = %#v, %#v, %#v",
			malformedAliasBody,
			malformedAliasMediaTypes,
			malformedAliasConverter.diagnostics,
		)
	}

	cycleReference := "#/components/requestBodies/Cycle"
	cycleReferenceConverter := newConverter(100)
	cycleReferenceConverter.requestBodies[cycleReference] = conversionValue(t, `{"$ref":"`+cycleReference+`"}`)
	cycleBody, cycleMediaTypes, err := cycleReferenceConverter.requestBody(
		conversionValue(t, `{"$ref":"`+cycleReference+`"}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}

	missingDescriptionConverter := newConverter(100)
	missingDescriptionResponse, _, err := missingDescriptionConverter.response(
		conversionValue(t, `{"x-source":"retained"}`), "/response",
	)
	if err != nil {
		t.Fatal(err)
	}
	if textValue(t, memberAt(t, missingDescriptionResponse, "description")) != "Response" ||
		textValue(t, memberAt(t, missingDescriptionResponse, "x-source")) != "retained" ||
		len(missingDescriptionConverter.diagnostics) != 1 ||
		missingDescriptionConverter.diagnostics[0].Code != "openapi.convert.response-description-added" {
		t.Fatalf("response without description conversion = %#v", missingDescriptionResponse)
	}

	unknownResponseFieldConverter := newConverter(100)
	unknownResponseField, unknownResponseMediaTypes, err := unknownResponseFieldConverter.response(
		conversionValue(t, `{
			"description":"response",
			"unknown":{"application/json":{"schema":{"type":"string"}}}
		}`),
		"/response",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(unknownResponseMediaTypes) != 0 ||
		len(unknownResponseFieldConverter.diagnostics) != 1 ||
		unknownResponseFieldConverter.diagnostics[0].Code != "openapi.convert.response-field-removed" {
		t.Fatalf(
			"unknown response field conversion = %#v, %#v, %#v",
			unknownResponseField,
			unknownResponseMediaTypes,
			unknownResponseFieldConverter.diagnostics,
		)
	}
	if cycleBody.Kind() != jsonvalue.InvalidKind || len(cycleMediaTypes) != 0 ||
		len(cycleReferenceConverter.diagnostics) != 1 ||
		cycleReferenceConverter.diagnostics[0].Code != "openapi.convert.request-body-removed" {
		t.Fatalf(
			"cyclic request reference conversion = %#v, %#v, %#v",
			cycleBody,
			cycleMediaTypes,
			cycleReferenceConverter.diagnostics,
		)
	}

	responseConverter := newConverter(100)
	response, responseMediaTypes, err := responseConverter.response(conversionValue(t, `{
		"content":{"text/plain":{},"application/json":{"schema":{"type":"string"}}}
	}`), "/response")
	if err != nil {
		t.Fatal(err)
	}
	if textValue(t, memberAt(t, response, "schema", "type")) != "string" ||
		len(responseMediaTypes) != 2 {
		t.Fatalf("response schema after schemaless media = %#v, %#v", response, responseMediaTypes)
	}

	invalidMediaResponseConverter := newConverter(100)
	invalidMediaResponse, invalidResponseMediaTypes, err := invalidMediaResponseConverter.response(
		conversionValue(t, `{"description":"response","content":{"0":{},"application/json":{}}}`), "/response",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(invalidResponseMediaTypes) != 1 ||
		textValue(t, invalidResponseMediaTypes[0]) != "application/json" ||
		len(invalidMediaResponseConverter.diagnostics) != 1 ||
		invalidMediaResponseConverter.diagnostics[0].Code != "openapi.convert.media-type-removed" ||
		invalidMediaResponseConverter.diagnostics[0].Pointer != "/response/content/0" {
		t.Fatalf(
			"invalid response media conversion = %#v, %#v, %#v",
			invalidMediaResponse,
			invalidResponseMediaTypes,
			invalidMediaResponseConverter.diagnostics,
		)
	}

	securityConverter := newConverter(100)
	security, err := securityConverter.securitySchemeMap(conversionValue(t, `{
		"Bearer":{"type":"http","scheme":"bearer"},
		"Basic":{"type":"http","scheme":"basic","bearerFormat":"discarded","x-retained":true}
	}`), "/securitySchemes")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := security.Lookup("Bearer"); exists {
		t.Fatal("unsupported security scheme was retained")
	}
	if _, exists := memberAt(t, security, "Basic").Lookup("x-retained"); !exists {
		t.Fatalf("security scheme after discarded fields = %#v", security)
	}

	securityConverter.securitySchemes["Bearer"] = false
	securityConverter.securitySchemes["Basic"] = true
	requirements := securityConverter.securityRequirements(conversionValue(t, `[
		true,{"Bearer":[]},{"Basic":[]}
	]`), "/security")
	elements, _ := requirements.Elements()
	if len(elements) != 2 || elements[0].Kind() != jsonvalue.BooleanKind {
		t.Fatalf("security alternatives after discarded requirement = %#v", requirements)
	}
	if _, exists := elements[1].Lookup("Basic"); !exists {
		t.Fatalf("supported security requirement = %#v", requirements)
	}

	schemaConverter := newConverter(100)
	schema, err := schemaConverter.schema(conversionValue(t, `{
		"discriminator":{"mapping":{}},"nullable":true,"x-retained":true
	}`), "/schema")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := schema.Lookup("discriminator"); exists {
		t.Fatal("invalid discriminator was retained")
	}
	if _, exists := schema.Lookup("nullable"); exists {
		t.Fatal("unsupported schema keyword was retained")
	}
	if _, exists := schema.Lookup("x-retained"); !exists {
		t.Fatalf("schema member after discarded keywords = %#v", schema)
	}

	strings := uniqueStrings([]jsonvalue.Value{
		jsonvalue.Boolean(true), conversionValue(t, `"one"`),
		conversionValue(t, `"one"`), conversionValue(t, `"two"`),
	})
	if len(strings) != 2 || textValue(t, strings[0]) != "one" ||
		textValue(t, strings[1]) != "two" {
		t.Fatalf("unique strings after discarded values = %#v", strings)
	}
}

func TestSwaggerDowngradeMutationBoundaries(t *testing.T) {
	t.Parallel()

	newConverter := func() *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: 10_000,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}

	for _, test := range []struct {
		raw  string
		want bool
	}{
		{"https://user@example.test/path", true},
		{"https://example.test/path?query=1", true},
		{"https://example.test/path#fragment", true},
		{"https:/missing-host", true},
		{"relative", true},
		{"/relative", false},
		{"https://example.test/path", false},
	} {
		parsed, err := url.Parse(test.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := unsupportedSwaggerServerURL(parsed); got != test.want {
			t.Errorf("unsupported server URL %q = %t", test.raw, got)
		}
	}
	if got := expandServerURL(
		"/{invalid}/{valid}",
		conversionValue(t, `{
			"invalid":{"default":true},
			"valid":{"default":"expanded"}
		}`),
	); got != "/{invalid}/expanded" {
		t.Fatalf("expanded server URL = %q", got)
	}

	nameConverter := newConverter()
	nameConverter.indexRequestBodyNames(conversionValue(t, `{
		"components":{"requestBodies":{
			"Form":{"content":{"multipart/form-data":{
				"schema":{"type":"object","properties":{}}
			}}},
			"Payload":{"content":{"application/json":{
				"schema":{"type":"string"}
			}}}
		}}
	}`))
	if _, exists := nameConverter.requestBodyNames["#/components/requestBodies/Form"]; exists {
		t.Fatal("form request body received a reusable parameter name")
	}
	if got := nameConverter.requestBodyNames["#/components/requestBodies/Payload"]; got != "Payload" {
		t.Fatalf("request body after form body = %q", got)
	}

	formMediaConverter := newConverter()
	formBody := conversionValue(t, `{"content":{
		"multipart/form-data":{"schema":{"type":"object","properties":{}}},
		"application/x-www-form-urlencoded":{"schema":{"type":"object","properties":{}}},
		"application/json":{"schema":{"type":"string"}}
	}}`)
	selected, _ := swaggerFormMediaType(formBody)
	_, mediaTypes, err := formMediaConverter.operationFormRequestBody(
		formBody, selected, "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(mediaTypes) != 2 ||
		textValue(t, mediaTypes[0]) != "multipart/form-data" ||
		textValue(t, mediaTypes[1]) != "application/x-www-form-urlencoded" ||
		len(formMediaConverter.diagnostics) != 1 {
		t.Fatalf("compatible form media types = %#v, %#v", mediaTypes, formMediaConverter.diagnostics)
	}

	for _, test := range []struct {
		name       string
		schema     string
		wantType   string
		wantFormat string
	}{
		{
			name:     "binary file",
			schema:   `{"type":"string","format":"binary","description":"file"}`,
			wantType: "file",
		},
		{
			name:     "formatted string",
			schema:   `{"type":"string","format":"uuid"}`,
			wantType: "string", wantFormat: "uuid",
		},
		{
			name:     "formatted integer",
			schema:   `{"type":"integer","format":"binary"}`,
			wantType: "integer", wantFormat: "binary",
		},
	} {
		converted, conversionErr := newConverter().formParameter(
			test.name, conversionValue(t, test.schema), conversionValue(t, `{}`),
			"/property", false,
		)
		if conversionErr != nil {
			t.Fatal(conversionErr)
		}
		if got := textValue(t, memberAt(t, converted, "type")); got != test.wantType {
			t.Errorf("%s type = %q", test.name, got)
		}
		format, exists := converted.Lookup("format")
		if test.wantFormat == "" {
			if exists {
				t.Errorf("%s retained format %#v", test.name, format)
			}
		} else if got := textValue(t, format); !exists || got != test.wantFormat {
			t.Errorf("%s format = %q, %t", test.name, got, exists)
		}
		if test.name == "binary file" &&
			textValue(t, memberAt(t, converted, "description")) != "file" {
			t.Errorf("binary file discarded member after format: %#v", converted)
		}
	}
	filtered, err := newConverter().formParameter(
		"value",
		conversionValue(t, `{"type":"string","title":"discarded","x-after":true}`),
		conversionValue(t, `{}`), "/property", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := filtered.Lookup("title"); exists {
		t.Fatalf("unsupported form property field was retained in %#v", filtered)
	}
	if _, exists := filtered.Lookup("x-after"); !exists {
		t.Fatalf("form property field after discarded field = %#v", filtered)
	}

	requestConverter := newConverter()
	request, requestMediaTypes, err := requestConverter.requestBody(
		conversionValue(t, `{"content":{
			"application/json":{"schema":{"type":"string"}},
			"text/plain":{"schema":{"type":"string"}},
			"application/xml":{"schema":{"type":"integer"}}
		}}`), "/body",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(requestMediaTypes) != 3 ||
		textValue(t, memberAt(t, request, "schema", "type")) != "string" ||
		len(requestConverter.diagnostics) != 1 {
		t.Fatalf("request media traversal = %#v, %#v, %#v", request, requestMediaTypes, requestConverter.diagnostics)
	}

	responseConverter := newConverter()
	response, responseMediaTypes, err := responseConverter.response(
		conversionValue(t, `{
			"x-meta":{"fake":{"schema":{"type":"integer"}}},
			"content":{"application/json":{"schema":{"type":"string"}}}
		}`), "/response",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(responseMediaTypes) != 1 ||
		textValue(t, responseMediaTypes[0]) != "application/json" ||
		textValue(t, memberAt(t, response, "schema", "type")) != "string" {
		t.Fatalf("response extension traversal = %#v, %#v", response, responseMediaTypes)
	}
	memberAt(t, response, "x-meta")
	response, _, err = newConverter().response(
		conversionValue(t, `{"content":true,"x-after":true}`), "/response",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := response.Lookup("x-after"); !exists {
		t.Fatalf("response member after malformed content = %#v", response)
	}

	if sameJSONValue(jsonvalue.Value{}, conversionValue(t, `null`)) ||
		sameJSONValue(conversionValue(t, `null`), jsonvalue.Value{}) {
		t.Fatal("invalid JSON values compared equal to valid values")
	}
}

func TestSwaggerDowngradePipelinePropagatesIntermediateFailures(t *testing.T) {
	t.Parallel()

	options := DefaultOptions()
	options.MaxDocumentNodes = 1
	if _, _, err := convertOpenAPIToSwagger20(
		context.Background(),
		conversionValue(t, `{
			"openapi":"3.2.0","info":{"title":"API","version":"1"},
			"paths":{"/value":{}}
		}`),
		openapi.DialectOAS32, options,
	); err != ErrLimitExceeded {
		t.Fatalf("OpenAPI 3.2 stage error = %v", err)
	}
	options = DefaultOptions()
	options.MaxSchemaNodes = 1
	if _, _, err := convertOpenAPIToSwagger20(
		context.Background(),
		conversionValue(t, `{
			"openapi":"3.1.2","info":{"title":"API","version":"1"},
			"paths":{},"components":{"schemas":{"Value":{"items":{}}}}
		}`),
		openapi.DialectOAS31, options,
	); err != ErrLimitExceeded {
		t.Fatalf("OpenAPI 3.1 stage error = %v", err)
	}
}

func TestTopLevelConversionPropagatesEveryStageFailure(t *testing.T) {
	t.Parallel()

	version30, err := openapi.ParseVersion("3.0.4")
	if err != nil {
		t.Fatal(err)
	}
	version31, err := openapi.ParseVersion("3.1.2")
	if err != nil {
		t.Fatal(err)
	}
	version20, err := openapi.ParseVersion("2.0")
	if err != nil {
		t.Fatal(err)
	}
	base := `"info":{"title":"API","version":"1"},"paths":{}`
	tests := []struct {
		name    string
		source  string
		target  openapi.Version
		options Options
	}{
		{
			name:    "OpenAPI 3.0 schema upgrade",
			source:  `{"openapi":"3.0.4",` + base + `,"components":{"schemas":{"Value":{"items":{}}}}}`,
			target:  version31,
			options: Options{MaxRootMembers: 100, MaxDocumentNodes: 100, MaxSchemaNodes: 1},
		},
		{
			name:    "OpenAPI 3.1 schema downgrade",
			source:  `{"openapi":"3.1.2",` + base + `,"components":{"schemas":{"Value":{"items":{}}}}}`,
			target:  version30,
			options: Options{MaxRootMembers: 100, MaxDocumentNodes: 100, MaxSchemaNodes: 1},
		},
		{
			name:    "OpenAPI 3.2 document downgrade",
			source:  `{"openapi":"3.2.0","info":{"title":"API","version":"1"},"paths":{"/value":{}}}`,
			target:  version31,
			options: Options{MaxRootMembers: 100, MaxDocumentNodes: 1, MaxSchemaNodes: 100},
		},
		{
			name: "Swagger staged schema upgrade",
			source: `{
				"swagger":"2.0","info":{"title":"API","version":"1"},
				"paths":{"/value":{"post":{
					"parameters":[
						{"name":"one","in":"formData","type":"string"},
						{"name":"two","in":"formData","type":"string"}
					],"responses":{"200":{"description":"ok"}}
				}}}
			}`,
			target:  version31,
			options: Options{MaxRootMembers: 100, MaxDocumentNodes: 100, MaxSchemaNodes: 2},
		},
		{
			name:    "OpenAPI 3.2 staged schema downgrade",
			source:  `{"openapi":"3.2.0",` + base + `,"components":{"schemas":{"Value":{"items":{}}}}}`,
			target:  version30,
			options: Options{MaxRootMembers: 100, MaxDocumentNodes: 100, MaxSchemaNodes: 1},
		},
		{
			name:    "OpenAPI to Swagger downgrade",
			source:  `{"openapi":"3.0.4","info":{"title":"API","version":"1"},"paths":{"/value":{}}}`,
			target:  version20,
			options: Options{MaxRootMembers: 100, MaxDocumentNodes: 1, MaxSchemaNodes: 100},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := To(
				context.Background(), swaggerDocument(t, test.source),
				test.target, test.options,
			); err != ErrLimitExceeded {
				t.Fatalf("stage error = %v", err)
			}
		})
	}
	if _, err := To(
		context.Background(),
		conversionDocument{raw: conversionValue(t, `{}`)},
		version30, DefaultOptions(),
	); err == nil || !strings.Contains(err.Error(), ErrUnsupportedConversion.Error()) {
		t.Fatalf("unsupported conversion error = %v", err)
	}
}

func TestConversionDefensiveGuardsRejectMalformedShapes(t *testing.T) {
	t.Parallel()

	if enabledBoolean(conversionValue(t, `{"enabled":1}`), "enabled") {
		t.Fatal("numeric boolean member was enabled")
	}
	if _, flag, present := downgradeExclusiveBound(
		conversionValue(t, `{"minimum":true,"exclusiveMinimum":1}`),
		"minimum", "exclusiveMinimum", true,
	); !flag || !present {
		t.Fatalf("malformed bound result = %t, %t", flag, present)
	}
	if isSecurityType(conversionValue(t, `{"type":true}`), "oauth2") {
		t.Fatal("non-string security type matched")
	}
	if isSecurityType(conversionValue(t, `{}`), "oauth2") {
		t.Fatal("missing security type matched")
	}

	collector := oas30SchemaCollector{}
	collector.parameters(conversionValue(t, `{"parameters":true}`), "/parameters")
	collector.requestBody(jsonvalue.Boolean(true), "/requestBody")
	collector.requestBody(
		conversionValue(t, `{"$ref":"#/components/requestBodies/Value"}`),
		"/requestBody",
	)

	mediaConverter := &oas32DocumentConverter{mediaTypes: map[string]jsonvalue.Value{}}
	mediaConverter.indexMediaTypes(conversionValue(t, `{"components":{"mediaTypes":true}}`))
	if len(mediaConverter.mediaTypes) != 0 {
		t.Fatalf("malformed media types were indexed: %#v", mediaConverter.mediaTypes)
	}

	upgrade := &swagger20Converter{}
	upgrade.indexBodyParameters(conversionValue(t, `{"parameters":true}`))
	for _, raw := range []string{
		`{"host":"example.test","schemes":true}`,
		`{"host":"example.test","schemes":[]}`,
	} {
		servers, exists := upgrade.servers(conversionValue(t, raw))
		if !exists || servers.Kind() != jsonvalue.ArrayKind {
			t.Fatalf("fallback servers for %s = %#v, %t", raw, servers, exists)
		}
	}
}

func TestSwaggerDowngradeDefensiveGuardsPreserveLosses(t *testing.T) {
	t.Parallel()

	newConverter := func() *oas30SwaggerConverter {
		return &oas30SwaggerConverter{
			ctx: context.Background(), maxNodes: 100,
			requestBodyNames: map[string]string{}, requestBodies: map[string]jsonvalue.Value{},
			securitySchemes: map[string]bool{}, parameters: map[string]bool{},
		}
	}
	converter := newConverter()
	if members, err := converter.server(
		conversionValue(t, `[{"url":true}]`), "/servers",
	); err != nil || members != nil {
		t.Fatalf("malformed server = %#v, %v", members, err)
	}
	for _, raw := range []string{`{}`, `{"content":{}}`} {
		if _, ok := swaggerFormMediaType(conversionValue(t, raw)); ok {
			t.Fatalf("malformed form content %s selected a media type", raw)
		}
	}
	media := jsonvalue.Member{
		Name: "multipart/form-data", Value: conversionValue(t, `{}`),
	}
	if parameters, mediaTypes, err := converter.formRequestBody(
		conversionValue(t, `{}`), media, "/body",
	); err != nil || len(parameters) != 0 || len(mediaTypes) != 1 {
		t.Fatalf("missing form schema = %#v, %#v, %v", parameters, mediaTypes, err)
	}
	malformedRequestConverter := newConverter()
	if body, mediaTypes, err := malformedRequestConverter.requestBody(
		conversionValue(t, `{"content":true}`), "/body",
	); err != nil || body.Kind() != jsonvalue.InvalidKind || len(mediaTypes) != 0 ||
		len(malformedRequestConverter.diagnostics) != 1 ||
		malformedRequestConverter.diagnostics[0].Code != "openapi.convert.request-body-removed" {
		t.Fatalf(
			"malformed request content = %#v, %#v, %#v, %v",
			body,
			mediaTypes,
			malformedRequestConverter.diagnostics,
			err,
		)
	}
	if _, ok, err := converter.oauth2SecurityScheme(
		conversionValue(t, `{"flows":{}}`), "/security",
	); err != nil || ok {
		t.Fatalf("empty OAuth flows = %t, %v", ok, err)
	}
	for _, test := range []struct {
		style    string
		location string
	}{
		{style: "spaceDelimited", location: "query"},
		{style: "pipeDelimited", location: "query"},
		{style: "simple", location: "path"},
	} {
		if format, ok := swaggerCollectionFormat(
			test.location, test.style, true, true, true,
		); ok || format != "" {
			t.Errorf("exploded %s format = %q, %t", test.style, format, ok)
		}
	}
}

type conversionDocument struct {
	raw     jsonvalue.Value
	version openapi.Version
}

func (document conversionDocument) Raw() jsonvalue.Value {
	return document.raw
}

func (document conversionDocument) SpecificationVersion() openapi.Version {
	return document.version
}

func conversionValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
