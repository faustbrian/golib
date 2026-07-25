package wsdl_test

import (
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestWSDL20SOAPBindingRoundTrip(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:test" xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
		` xmlns:env="urn:soap-envelope" targetNamespace="urn:test">` +
		`<interface name="API"><fault name="Failure" element="tns:Failure"/>` +
		`<operation name="Call" pattern="http://www.w3.org/ns/wsdl/in-out">` +
		`<input messageLabel="In" element="tns:Request"/>` +
		`<output messageLabel="Out" element="tns:Response"/>` +
		`<outfault ref="tns:Failure" messageLabel="Out"/>` +
		`</operation></interface>` +
		`<binding name="SOAP" interface="tns:API"` +
		` type="http://www.w3.org/ns/wsdl/soap" wsoap:version="1.1"` +
		` wsoap:protocol="urn:protocol" wsoap:mepDefault="urn:mep">` +
		`<wsoap:module ref="urn:binding-module" required="false"/>` +
		`<fault ref="tns:Failure" wsoap:code="env:Sender"` +
		` wsoap:subcodes="#any">` +
		`<wsoap:module ref="urn:fault-module"/>` +
		`<wsoap:header element="tns:Header" mustUnderstand="true" required="false"/>` +
		`</fault>` +
		`<operation ref="tns:Call" wsoap:mep="urn:operation-mep" wsoap:action="">` +
		`<wsoap:module ref="urn:operation-module" required="true"/>` +
		`<input messageLabel="In">` +
		`<wsoap:module ref="urn:input-module"/>` +
		`<wsoap:header element="tns:Header" mustUnderstand="false"/>` +
		`</input><output messageLabel="Out"/>` +
		`<outfault ref="tns:Failure" messageLabel="Out">` +
		`<wsoap:module ref="urn:reference-module"/>` +
		`</outfault></operation></binding></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertWSDL20SOAPBinding(t, document)

	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertWSDL20SOAPBinding(t, roundTrip)
}

func assertWSDL20SOAPBinding(t *testing.T, document *wsdl.Document) {
	t.Helper()

	description, _ := document.Description20()
	binding := description.Bindings[0]
	if binding.SOAP == nil || binding.SOAP.Version != "1.1" ||
		!binding.SOAP.VersionSet || binding.SOAP.Protocol != "urn:protocol" ||
		!binding.SOAP.ProtocolSet || binding.SOAP.MEPDefault != "urn:mep" ||
		!binding.SOAP.MEPDefaultSet || len(binding.SOAP.Modules) != 1 ||
		!binding.SOAP.Modules[0].RequiredSet || binding.SOAP.Modules[0].Required {
		t.Fatalf("Binding SOAP = %#v", binding.SOAP)
	}
	fault := binding.Faults[0].SOAP
	if fault == nil || !fault.CodeSet || fault.CodeAny ||
		fault.Code.Local != "Sender" || fault.Code.Namespace != "urn:soap-envelope" ||
		!fault.SubcodesSet || !fault.SubcodesAny || len(fault.Modules) != 1 ||
		len(fault.Headers) != 1 || !fault.Headers[0].MustUnderstand ||
		!fault.Headers[0].MustUnderstandSet || fault.Headers[0].Required ||
		!fault.Headers[0].RequiredSet {
		t.Fatalf("Fault SOAP = %#v", fault)
	}
	operation := binding.Operations[0]
	if operation.SOAP == nil || operation.SOAP.MEP != "urn:operation-mep" ||
		!operation.SOAP.MEPSet || operation.SOAP.Action != "" ||
		!operation.SOAP.ActionSet || len(operation.SOAP.Modules) != 1 ||
		!operation.SOAP.Modules[0].Required {
		t.Fatalf("Operation SOAP = %#v", operation.SOAP)
	}
	if len(operation.Inputs) != 1 || operation.Inputs[0].SOAP == nil ||
		len(operation.Inputs[0].SOAP.Modules) != 1 ||
		len(operation.Inputs[0].SOAP.Headers) != 1 ||
		operation.Inputs[0].SOAP.Headers[0].MustUnderstand ||
		!operation.Inputs[0].SOAP.Headers[0].MustUnderstandSet {
		t.Fatalf("Input SOAP = %#v", operation.Inputs)
	}
	if len(operation.OutFaults) != 1 || operation.OutFaults[0].SOAP == nil ||
		len(operation.OutFaults[0].SOAP.Modules) != 1 {
		t.Fatalf("OutFault SOAP = %#v", operation.OutFaults)
	}
}

func TestParseWSDL20SOAPRejectsInvalidBoolean(t *testing.T) {
	t.Parallel()

	_, err := wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"`+
			` targetNamespace="urn:test"><binding name="SOAP"`+
			` type="http://www.w3.org/ns/wsdl/soap">`+
			`<wsoap:module ref="urn:module" required="maybe"/>`+
			`</binding></description>`,
	), wsdl.ParseOptions{})
	if err == nil {
		t.Fatal("Parse() error = nil, want invalid boolean error")
	}
}

func TestValidateWSDL20SOAPBindingProperties(t *testing.T) {
	t.Parallel()

	document, err := wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` xmlns:tns="urn:test" xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"`+
			` targetNamespace="urn:test"><interface name="API">`+
			`<operation name="Call" pattern="urn:in-only"/>`+
			`</interface><binding name="SOAP" interface="tns:API"`+
			` type="http://www.w3.org/ns/wsdl/soap" wsoap:version="3.0">`+
			`<operation ref="tns:Call" wsoap:mep="relative-mep"><input>`+
			`<wsoap:header/></input></operation>`+
			`</binding></description>`,
	), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := map[string]bool{
		"WSDL20_SOAP_PROTOCOL_REQUIRED": false,
		"WSDL20_SOAP_VERSION":           false,
		"WSDL20_SOAP_MEP_IRI":           false,
		"WSDL20_SOAP_HEADER_ELEMENT":    false,
	}
	for _, diagnostic := range wsdl.Validate(document, wsdl.ValidationOptions{}) {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, found := range want {
		if !found {
			t.Errorf("Validate() missing %s", code)
		}
	}
}
