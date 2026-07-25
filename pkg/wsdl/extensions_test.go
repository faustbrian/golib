package wsdl_test

import (
	"context"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestParsePreservesWSDL11ExtensionElementsAndAttributes(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:wsdl="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:ext="urn:example:extension" ext:profile="strict"` +
		` targetNamespace="urn:stock">before` +
		`<ext:policy wsdl:required="1" level="strict">alpha` +
		`<ext:rule name="one"/>omega</ext:policy>after` +
		`</definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	if len(definitions.ExtensionAttributes) != 1 ||
		definitions.ExtensionAttributes[0].Name != (wsdl.QName{
			Namespace: "urn:example:extension", Local: "profile",
		}) || definitions.ExtensionAttributes[0].Value != "strict" {
		t.Fatalf("ExtensionAttributes = %#v", definitions.ExtensionAttributes)
	}
	if len(definitions.Extensions) != 1 {
		t.Fatalf("Extensions = %#v", definitions.Extensions)
	}
	extension := definitions.Extensions[0]
	if extension.Name != (wsdl.QName{
		Namespace: "urn:example:extension", Local: "policy",
	}) || !extension.RequiredSet || !extension.Required {
		t.Fatalf("Extension = %#v", extension)
	}
	xml := string(extension.XML)
	if !strings.Contains(xml, ">alpha<ext:rule") ||
		!strings.Contains(xml, `name="one"`) ||
		!strings.Contains(xml, "></ext:rule>omega</ext:policy>") {
		t.Fatalf("Extension XML = %q", xml)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	if len(diagnostics) != 1 || diagnostics[0].Code != "WSDL_EXTENSION_REQUIRED" {
		t.Fatalf("Validate() = %#v", diagnostics)
	}
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	definitions, _ = roundTrip.Definitions11()
	if len(definitions.ExtensionAttributes) != 1 ||
		definitions.ExtensionAttributes[0].Value != "strict" ||
		len(definitions.Extensions) != 1 ||
		!definitions.Extensions[0].RequiredSet ||
		!definitions.Extensions[0].Required {
		t.Fatalf("round-trip definitions = %#v", definitions)
	}
}

func TestParseRejectsNonXMLBooleanExtensionRequired(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:wsdl="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:ext="urn:example:extension">` +
		`<ext:policy wsdl:required="TRUE"/>` +
		`</definitions>`

	if _, err := wsdl.Parse(
		context.Background(),
		[]byte(source),
		wsdl.ParseOptions{},
	); err == nil {
		t.Fatal("Parse() error = nil, want invalid XML boolean error")
	}
}

func TestParsePreservesNestedWSDL20Extensions(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:wsdl="http://www.w3.org/ns/wsdl"` +
		` xmlns:ext="urn:example:extension" targetNamespace="urn:stock">` +
		`<interface name="StockQuote" ext:profile="strict">` +
		`<ext:feature wsdl:required="false"/>` +
		`</interface></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	description, _ := document.Description20()
	interfaceValue := description.Interfaces[0]
	if len(interfaceValue.ExtensionAttributes) != 1 ||
		interfaceValue.ExtensionAttributes[0].Value != "strict" {
		t.Fatalf("ExtensionAttributes = %#v", interfaceValue.ExtensionAttributes)
	}
	if len(interfaceValue.Extensions) != 1 ||
		interfaceValue.Extensions[0].RequiredSet != true ||
		interfaceValue.Extensions[0].Required != false {
		t.Fatalf("Extensions = %#v", interfaceValue.Extensions)
	}
}

func TestWSDL20ExtensionsRoundTripAcrossComponents(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:wsdl="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:test" xmlns:ext="urn:extension"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:test" ext:root="yes">` +
		`<import namespace="urn:other" ext:marker="import"><ext:import/></import>` +
		`<include location="other.wsdl" ext:marker="include"><ext:include/></include>` +
		`<types ext:marker="types"><xs:schema/><ext:types/></types>` +
		`<interface name="API"><fault name="Failure" element="tns:Failure"` +
		` ext:marker="fault"><ext:fault/></fault>` +
		`<operation name="Call" pattern="urn:in-out" ext:marker="operation">` +
		`<input element="tns:Request"><ext:input/></input>` +
		`<outfault ref="tns:Failure"><ext:outfault/></outfault>` +
		`<ext:operation wsdl:required="true"/></operation></interface>` +
		`<binding name="Binding" interface="tns:API" type="urn:binding"` +
		` ext:marker="binding"><operation ref="tns:Call">` +
		`<ext:bound-operation/></operation><ext:binding/></binding>` +
		`<service name="Service" interface="tns:API" ext:marker="service">` +
		`<endpoint name="Endpoint" binding="tns:Binding" ext:marker="endpoint">` +
		`<ext:endpoint/></endpoint><ext:service/></service>` +
		`<ext:description/></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertWSDL20Extensions(t, document)
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertWSDL20Extensions(t, roundTrip)
}

func assertWSDL20Extensions(t *testing.T, document *wsdl.Document) {
	t.Helper()
	description, _ := document.Description20()
	interfaceValue := description.Interfaces[0]
	operation := interfaceValue.Operations[0]
	binding := description.Bindings[0]
	service := description.Services[0]
	if len(description.ExtensionAttributes) != 1 ||
		len(description.Extensions) != 1 ||
		len(description.Imports[0].ExtensionAttributes) != 1 ||
		len(description.Imports[0].Extensions) != 1 ||
		len(description.Includes[0].ExtensionAttributes) != 1 ||
		len(description.Includes[0].Extensions) != 1 ||
		len(description.Types.ExtensionAttributes) != 1 ||
		len(description.Types.Extensions) != 1 ||
		len(interfaceValue.Faults[0].ExtensionAttributes) != 1 ||
		len(interfaceValue.Faults[0].Extensions) != 1 ||
		len(operation.ExtensionAttributes) != 1 ||
		len(operation.Extensions) != 1 ||
		len(operation.Input.Extensions) != 1 ||
		len(operation.OutFaults[0].Extensions) != 1 ||
		len(binding.ExtensionAttributes) != 1 ||
		len(binding.Extensions) != 1 ||
		len(binding.Operations[0].Extensions) != 1 ||
		len(service.ExtensionAttributes) != 1 ||
		len(service.Extensions) != 1 ||
		len(service.Endpoints[0].ExtensionAttributes) != 1 ||
		len(service.Endpoints[0].Extensions) != 1 {
		t.Fatalf("WSDL 2.0 extensions were not preserved: %#v", description)
	}
}

func TestWSDL11ExtensionsRoundTripAcrossCoreComponents(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:wsdl="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:ext="urn:extension"` +
		` xmlns:http="http://schemas.xmlsoap.org/wsdl/http/"` +
		` targetNamespace="urn:test"><types ext:marker="types"><ext:types/></types>` +
		`<message name="Message" ext:marker="message"><part name="value"` +
		` type="tns:Value" ext:marker="part"><ext:part/></part><ext:message/>` +
		`</message><portType name="API" ext:marker="portType">` +
		`<operation name="Call" ext:marker="operation"><input` +
		` message="tns:Message" ext:marker="input"><ext:input/></input>` +
		`<ext:operation wsdl:required="true"/></operation><ext:portType/>` +
		`</portType><binding name="Binding" type="tns:API" ext:marker="binding">` +
		`<operation name="Call" ext:marker="boundOperation"><input` +
		` ext:marker="boundInput"><ext:boundInput/><http:custom/></input>` +
		`<ext:boundOperation/></operation><ext:binding/></binding>` +
		`<service name="Service" ext:marker="service"><port name="Endpoint"` +
		` binding="tns:Binding" ext:marker="port"><ext:port/></port>` +
		`<ext:service/></service></definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertWSDL11CoreExtensions(t, document)
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	if len(diagnostics) != 1 || diagnostics[0].Code != "WSDL_EXTENSION_REQUIRED" {
		t.Fatalf("Validate() = %#v", diagnostics)
	}
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertWSDL11CoreExtensions(t, roundTrip)
}

func assertWSDL11CoreExtensions(t *testing.T, document *wsdl.Document) {
	t.Helper()
	definitions, _ := document.Definitions11()
	message := definitions.Messages[0]
	portType := definitions.PortTypes[0]
	operation := portType.Operations[0]
	binding := definitions.Bindings[0]
	boundOperation := binding.Operations[0]
	service := definitions.Services[0]
	if len(definitions.Types.ExtensionAttributes) != 1 ||
		len(definitions.Types.Extensions) != 1 ||
		len(message.ExtensionAttributes) != 1 || len(message.Extensions) != 1 ||
		len(message.Parts[0].ExtensionAttributes) != 1 ||
		len(message.Parts[0].Extensions) != 1 ||
		len(portType.ExtensionAttributes) != 1 || len(portType.Extensions) != 1 ||
		len(operation.ExtensionAttributes) != 1 || len(operation.Extensions) != 1 ||
		len(operation.Input.ExtensionAttributes) != 1 ||
		len(operation.Input.Extensions) != 1 ||
		len(binding.ExtensionAttributes) != 1 || len(binding.Extensions) != 1 ||
		len(boundOperation.ExtensionAttributes) != 1 ||
		len(boundOperation.Extensions) != 1 ||
		len(boundOperation.Input.ExtensionAttributes) != 1 ||
		len(boundOperation.Input.Extensions) != 2 ||
		len(service.ExtensionAttributes) != 1 || len(service.Extensions) != 1 ||
		len(service.Ports[0].ExtensionAttributes) != 1 ||
		len(service.Ports[0].Extensions) != 1 {
		t.Fatalf("WSDL 1.1 extensions were not preserved: %#v", definitions)
	}
}
