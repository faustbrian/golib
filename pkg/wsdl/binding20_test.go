package wsdl_test

import (
	"bytes"
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestWSDL20BindingComponentsRoundTrip(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:test" xmlns:ext="urn:extension"` +
		` targetNamespace="urn:test">` +
		`<interface name="API"><fault name="Failure" element="tns:Failure"/>` +
		`<operation name="Echo" pattern="http://www.w3.org/ns/wsdl/in-out">` +
		`<input messageLabel="In" element="tns:Request"/>` +
		`<output messageLabel="Out" element="tns:Response"/>` +
		`<outfault ref="tns:Failure" messageLabel="Out"/>` +
		`</operation></interface>` +
		`<binding name="APIHTTP" interface="tns:API" type="urn:http">` +
		`<fault ref="tns:Failure" ext:format="problem"><ext:fault/></fault>` +
		`<operation ref="tns:Echo">` +
		`<input messageLabel="In" ext:format="json"><ext:input/></input>` +
		`<output messageLabel="Out"><ext:output/></output>` +
		`<outfault ref="tns:Failure" messageLabel="Out"><ext:outfault/></outfault>` +
		`</operation></binding></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertWSDL20BindingComponents(t, document)

	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertWSDL20BindingComponents(t, roundTrip)
}

func TestWSDL20BindingMayOmitInterface(t *testing.T) {
	t.Parallel()

	document, err := wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` targetNamespace="urn:test">`+
			`<binding name="Generic" type="urn:http"/>`+
			`</description>`,
	), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if bytes.Contains(payload, []byte(`interface=""`)) {
		t.Fatalf("Marshal() = %s, unexpectedly emitted empty interface", payload)
	}
}

func assertWSDL20BindingComponents(t *testing.T, document *wsdl.Document) {
	t.Helper()

	description, ok := document.Description20()
	if !ok || len(description.Bindings) != 1 {
		t.Fatalf("Bindings = %#v", description.Bindings)
	}
	binding := description.Bindings[0]
	if len(binding.Faults) != 1 || binding.Faults[0].Ref.Local != "Failure" ||
		len(binding.Faults[0].ExtensionAttributes) != 1 ||
		len(binding.Faults[0].Extensions) != 1 {
		t.Fatalf("Binding faults = %#v", binding.Faults)
	}
	if len(binding.Operations) != 1 {
		t.Fatalf("Binding operations = %#v", binding.Operations)
	}
	operation := binding.Operations[0]
	if len(operation.Inputs) != 1 || operation.Inputs[0].MessageLabel != "In" ||
		len(operation.Inputs[0].ExtensionAttributes) != 1 ||
		len(operation.Inputs[0].Extensions) != 1 ||
		len(operation.Outputs) != 1 || operation.Outputs[0].MessageLabel != "Out" ||
		len(operation.OutFaults) != 1 || operation.OutFaults[0].Ref.Local != "Failure" ||
		operation.OutFaults[0].MessageLabel != "Out" {
		t.Fatalf("Binding operation = %#v", operation)
	}
}

func TestValidateWSDL20BindingComponentReferences(t *testing.T) {
	t.Parallel()

	document, err := wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` xmlns:tns="urn:test" targetNamespace="urn:test">`+
			`<interface name="API"><fault name="Failure" element="tns:Failure"/>`+
			`<operation name="Echo" pattern="http://www.w3.org/ns/wsdl/in-out">`+
			`<input messageLabel="In" element="tns:Request"/>`+
			`<output messageLabel="Out" element="tns:Response"/>`+
			`<outfault ref="tns:Failure" messageLabel="Out"/>`+
			`</operation></interface>`+
			`<binding name="APIHTTP" interface="tns:API" type="urn:http">`+
			`<fault ref="tns:MissingFault"/>`+
			`<operation ref="tns:Echo">`+
			`<input messageLabel="Missing"/>`+
			`<outfault ref="tns:MissingFault" messageLabel="Out"/>`+
			`</operation></binding></description>`,
	), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	want := map[string]bool{
		"WSDL20_BINDING_FAULT":             false,
		"WSDL20_BINDING_MESSAGE_REFERENCE": false,
		"WSDL20_BINDING_FAULT_REFERENCE":   false,
	}
	for _, diagnostic := range diagnostics {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("Validate() diagnostics = %#v, missing %s", diagnostics, code)
		}
	}
}
