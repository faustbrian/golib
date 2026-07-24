package xmlvalue_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/xmlvalue"
)

func TestPlanNullUsesXSIForElementsAndOmitsAttributes(t *testing.T) {
	t.Parallel()

	element, err := xmlvalue.PlanNull(xmlvalue.Element)
	if err != nil {
		t.Fatal(err)
	}
	if element.Omit || !element.EmptyElement || len(element.Attributes) != 1 {
		t.Fatalf("element plan = %#v", element)
	}
	attribute := element.Attributes[0]
	if attribute.Name.Space != xmlvalue.XMLSchemaInstanceNamespace ||
		attribute.Name.Local != "nil" || attribute.Value != "true" {
		t.Fatalf("nil attribute = %#v", attribute)
	}

	attributePlan, err := xmlvalue.PlanNull(xmlvalue.Attribute)
	if err != nil {
		t.Fatal(err)
	}
	if !attributePlan.Omit || attributePlan.EmptyElement ||
		len(attributePlan.Attributes) != 0 {
		t.Fatalf("attribute plan = %#v", attributePlan)
	}
}

func TestRestoreOmittedAttributeAddsNullWhenSchemaAllowsIt(t *testing.T) {
	t.Parallel()

	schema := compileSchema(t, mustValue(t, `{"type":["number","null"]}`))
	instance := mustValue(t, `{"existing":1}`)
	restored, changed, err := xmlvalue.RestoreOmittedAttribute(
		context.Background(), instance, "optional", schema,
	)
	if err != nil || !changed {
		t.Fatalf("restored = %#v, %t, %v", restored, changed, err)
	}
	value, present := restored.Lookup("optional")
	if !present || value.Kind() != jsonvalue.NullKind {
		t.Fatalf("optional = %#v, %t", value, present)
	}
	members, _ := restored.Members()
	if len(members) != 2 || members[0].Name != "existing" ||
		members[1].Name != "optional" {
		t.Fatalf("member order = %#v", members)
	}
}

func TestRestoreOmittedAttributeLeavesNonNullableAndPresentProperties(t *testing.T) {
	t.Parallel()

	numberSchema := compileSchema(t, mustValue(t, `{"type":"number"}`))
	missing := mustValue(t, `{}`)
	actual, changed, err := xmlvalue.RestoreOmittedAttribute(
		context.Background(), missing, "value", numberSchema,
	)
	if err != nil || changed || actual.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("non-nullable = %#v, %t, %v", actual, changed, err)
	}

	nullableSchema := compileSchema(t, mustValue(t, `{"type":["string","null"]}`))
	present := mustValue(t, `{"value":"kept"}`)
	actual, changed, err = xmlvalue.RestoreOmittedAttribute(
		context.Background(), present, "value", nullableSchema,
	)
	if err != nil || changed {
		t.Fatalf("present = %#v, %t, %v", actual, changed, err)
	}
	value, _ := actual.Lookup("value")
	text, _ := value.Text()
	if text != "kept" {
		t.Fatalf("present value = %q", text)
	}
}

func TestXMLNullHandlingRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := xmlvalue.PlanNull(99); !errors.Is(err, xmlvalue.ErrInvalidInput) {
		t.Fatalf("invalid kind error = %v", err)
	}
	schema := compileSchema(t, mustValue(t, `{}`))
	for _, test := range []struct {
		name     string
		ctx      context.Context
		instance jsonvalue.Value
		property string
		schema   *jsonschema.Schema
	}{
		{name: "nil context", instance: mustValue(t, `{}`), property: "value", schema: schema},
		{name: "non-object", ctx: context.Background(), instance: mustValue(t, `[]`), property: "value", schema: schema},
		{name: "empty property", ctx: context.Background(), instance: mustValue(t, `{}`), schema: schema},
		{name: "nil schema", ctx: context.Background(), instance: mustValue(t, `{}`), property: "value"},
		{name: "invalid property", ctx: context.Background(), instance: mustValue(t, `{}`), property: string([]byte{0xff}), schema: schema},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := xmlvalue.RestoreOmittedAttribute(
				test.ctx, test.instance, test.property, test.schema,
			)
			if !errors.Is(err, xmlvalue.ErrInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRestoreOmittedAttributeHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := xmlvalue.RestoreOmittedAttribute(
		ctx,
		mustValue(t, `{}`),
		"value",
		compileSchema(t, mustValue(t, `{}`)),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func compileSchema(t *testing.T, schema jsonvalue.Value) *jsonschema.Schema {
	t.Helper()
	compiler, err := jsonschema.NewCompiler(jsonschema.DialectOAS31)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(context.Background(), schema)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func mustValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := parse.JSON(
		context.Background(), strings.NewReader(raw), parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
