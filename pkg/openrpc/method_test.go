package openrpc_test

import (
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestReferenceAndContentDescriptorRequiredFields(t *testing.T) {
	t.Parallel()

	if _, err := openrpc.NewReference(""); !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("NewReference error = %v", err)
	}
	if _, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{Name: "id"}); !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("NewContentDescriptor error = %v", err)
	}

	schema, err := jsonschema.Parse([]byte(`false`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	required := false
	descriptor, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{
		Name:     "id",
		Schema:   &schema,
		Required: &required,
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, present := descriptor.Required(); !present || value {
		t.Fatalf("Required() = (%t, %t)", value, present)
	}
	if descriptor.RequiredOrDefault() {
		t.Fatal("RequiredOrDefault() = true")
	}
}

func TestExamplesPreserveNullAndNotificationResultAbsence(t *testing.T) {
	t.Parallel()

	nullValue, err := jsonvalue.Parse([]byte(`null`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	example, err := openrpc.NewExample(openrpc.ExampleInput{
		Name:  "null identifier",
		Value: nullValue,
	})
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := openrpc.NewExamplePairing(openrpc.ExamplePairingInput{
		Name:   "notification",
		Params: []openrpc.ExampleOrReference{openrpc.ExampleValue(example)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pairing.Result(); ok {
		t.Fatal("notification pairing reported a result")
	}
	params := pairing.Params()
	params[0] = openrpc.ExampleOrReference{}
	if len(pairing.Params()) != 1 {
		t.Fatal("Params exposed mutable storage")
	}
}

func TestMethodOwnsCollectionsAndPreservesDefaults(t *testing.T) {
	t.Parallel()

	schema, err := jsonschema.Parse([]byte(`{"type":"integer"}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{
		Name:   "value",
		Schema: &schema,
	})
	if err != nil {
		t.Fatal(err)
	}
	params := []openrpc.ContentDescriptorOrReference{openrpc.ContentDescriptorValue(descriptor)}
	method, err := openrpc.NewMethod(openrpc.MethodInput{
		Name:   "math.double",
		Params: params,
	})
	if err != nil {
		t.Fatal(err)
	}
	params[0] = openrpc.ContentDescriptorOrReference{}
	if len(method.Params()) != 1 {
		t.Fatal("Params exposed caller storage")
	}
	if structure, present := method.ParamStructure(); present || structure != openrpc.ParamStructureEither {
		t.Fatalf("ParamStructure() = (%q, %t)", structure, present)
	}
	if method.DeprecatedOrDefault() {
		t.Fatal("DeprecatedOrDefault() = true")
	}
	if _, ok := method.Result(); ok {
		t.Fatal("notification-only method reported a result")
	}
}

func TestMethodPreservesNormativeOptionalSemantics(t *testing.T) {
	t.Parallel()

	schema, err := jsonschema.Parse([]byte(`true`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	deprecated := true
	descriptor, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{
		Name: "value", Schema: &schema, Deprecated: &deprecated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if value, present := descriptor.Deprecated(); !present || !value {
		t.Fatalf("descriptor.Deprecated() = (%t, %t)", value, present)
	}
	code, err := openrpc.ParseInteger("-32000")
	if err != nil {
		t.Fatal(err)
	}
	methodError, err := openrpc.NewError(openrpc.ErrorInput{
		Code: code, Message: "Application failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	structure := openrpc.ParamStructureByName
	result := openrpc.ContentDescriptorValue(descriptor)
	method, err := openrpc.NewMethod(openrpc.MethodInput{
		Name:           "read",
		Params:         []openrpc.ContentDescriptorOrReference{result},
		ParamStructure: &structure,
		Result:         &result,
		Errors:         []openrpc.ErrorOrReference{openrpc.ErrorValue(methodError)},
		Deprecated:     &deprecated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if actual, present := method.ParamStructure(); !present || actual != structure {
		t.Fatalf("ParamStructure() = (%q, %t)", actual, present)
	}
	parameter, inline := method.Params()[0].Descriptor()
	if !inline || parameter.Name() != "value" {
		t.Fatal("by-name parameter key was not preserved")
	}
	if _, present := method.Result(); !present {
		t.Fatal("explicit result reported absent")
	}
	if errors, present := method.Errors(); !present || len(errors) != 1 {
		t.Fatalf("Errors() = (%#v, %t)", errors, present)
	}
	if value, present := method.Deprecated(); !present || !value {
		t.Fatalf("Deprecated() = (%t, %t)", value, present)
	}
}

func TestMethodRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	_, err := openrpc.NewMethod(openrpc.MethodInput{Name: "missing.params"})
	if !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("NewMethod error = %v", err)
	}
}

func TestErrorCodePreservesArbitraryPrecision(t *testing.T) {
	t.Parallel()

	code, err := openrpc.ParseInteger("123456789012345678901234567890")
	if err != nil {
		t.Fatal(err)
	}
	object, err := openrpc.NewError(openrpc.ErrorInput{Code: code, Message: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	if object.Code().String() != "123456789012345678901234567890" {
		t.Fatalf("Code() = %q", object.Code().String())
	}
	if _, err := openrpc.ParseInteger("1.0"); !errors.Is(err, openrpc.ErrInvalidInteger) {
		t.Fatalf("ParseInteger error = %v", err)
	}
}
