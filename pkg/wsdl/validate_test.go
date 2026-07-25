package wsdl_test

import (
	"context"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestValidateWSDL11ReportsBrokenComponentReferences(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:broken" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:broken">` +
		`<message name="Request"><part name="value" element="tns:Value"` +
		` type="xs:string"/></message>` +
		`<portType name="BrokenPort"><operation name="Call">` +
		`<input message="tns:Missing"/></operation></portType>` +
		`<binding name="BrokenBinding" type="tns:MissingPort"/>` +
		`<service name="BrokenService"><port name="BrokenEndpoint"` +
		` binding="tns:MissingBinding"/></service>` +
		`</definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	want := []string{
		"WSDL11_PART_CONTENT",
		"WSDL11_MESSAGE_REFERENCE",
		"WSDL11_PORT_TYPE_REFERENCE",
		"WSDL11_BINDING_REFERENCE",
	}
	if !diagnostics.HasErrors() || len(diagnostics) != len(want) {
		t.Fatalf("Validate() = %#v, want codes %v", diagnostics, want)
	}
	for index, code := range want {
		if diagnostics[index].Code != code ||
			diagnostics[index].Severity != wsdl.SeverityError {
			t.Fatalf("diagnostic[%d] = %#v, want code %s", index, diagnostics[index], code)
		}
	}
}

func TestNewDocument11RejectsInconsistentOperationStyle(t *testing.T) {
	t.Parallel()

	document, err := wsdl.NewDocument11(wsdl.Definitions11{
		TargetNamespace: "urn:test",
		Messages:        []wsdl.Message11{{Name: "Input"}, {Name: "Output"}},
		PortTypes: []wsdl.PortType11{{Name: "Port", Operations: []wsdl.Operation11{{
			Name: "Wrong", Style: wsdl.OperationStyleOneWay,
			Input: &wsdl.OperationMessage11{
				Message: wsdl.QName{Namespace: "urn:test", Local: "Input"},
			},
			Output: &wsdl.OperationMessage11{
				Message: wsdl.QName{Namespace: "urn:test", Local: "Output"},
			},
		}}}},
	}, wsdl.ValidationOptions{})
	if err == nil || document != nil {
		t.Fatalf("NewDocument11() = (%v, %v), want validation error", document, err)
	}
}

func TestValidateWSDL11BindingProtocolProperties(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
		` xmlns:http="http://schemas.xmlsoap.org/wsdl/http/"` +
		` xmlns:mime="http://schemas.xmlsoap.org/wsdl/mime/" targetNamespace="urn:test">` +
		`<message name="Request"><part name="id" type="tns:ID"/></message>` +
		`<portType name="Port"><operation name="Call"><input message="tns:Request"/>` +
		`</operation></portType><binding name="Bad" type="tns:Port">` +
		`<soap:binding style="bad" transport=""/><http:binding verb="BAD VERB"/>` +
		`<operation name="Call"><input><soap:body use="bad"/>` +
		`<mime:content part="missing" type="not a media type"/></input></operation>` +
		`</binding><service name="Service"><port name="Endpoint" binding="tns:Bad">` +
		`<soap:address location="relative"/></port></service></definitions>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	want := []string{
		"WSDL11_BINDING_PROTOCOL", "WSDL11_SOAP_STYLE", "WSDL11_SOAP_TRANSPORT",
		"WSDL11_HTTP_VERB", "WSDL11_SOAP_USE", "WSDL11_MIME_PART",
		"WSDL11_MIME_TYPE", "WSDL11_ENDPOINT_ADDRESS",
	}
	for _, code := range want {
		found := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == code {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Validate() = %#v, missing %s", diagnostics, code)
		}
	}
}

func TestValidateWSDL11ResolvesOverloadedBindingOperation(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
		`<message name="First"><part name="first" type="xs:string"/></message>` +
		`<message name="Second"><part name="second" type="xs:string"/></message>` +
		`<portType name="Port"><operation name="Call"><input name="First"` +
		` message="tns:First"/></operation><operation name="Call"><input name="Second"` +
		` message="tns:Second"/></operation></portType>` +
		`<binding name="Binding" type="tns:Port"><soap:binding transport="urn:test"/>` +
		`<operation name="Call"><input name="Second"><soap:body use="literal"` +
		` parts="second"/></input></operation></binding></definitions>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{}); diagnostics.HasErrors() {
		t.Fatalf("Validate() = %#v", diagnostics)
	}
}

func TestValidateWSDL11RejectsAmbiguousBindingOperation(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" targetNamespace="urn:test">` +
		`<message name="Message"/><portType name="Port">` +
		`<operation name="Call"><input name="First" message="tns:Message"/></operation>` +
		`<operation name="Call"><input name="Second" message="tns:Message"/></operation>` +
		`</portType><binding name="Binding" type="tns:Port">` +
		`<operation name="Call"><input/></operation></binding></definitions>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	if len(diagnostics) != 1 || diagnostics[0].Code != "WSDL11_BINDING_OPERATION_REFERENCE" {
		t.Fatalf("Validate() = %#v", diagnostics)
	}
}

func TestValidateWSDL11RejectsEquivalentDefaultedOperationSignatures(t *testing.T) {
	t.Parallel()

	definitions := wsdl.Definitions11{
		TargetNamespace: "urn:test",
		Messages:        []wsdl.Message11{{Name: "Message"}},
		PortTypes: []wsdl.PortType11{{Name: "Port", Operations: []wsdl.Operation11{
			{
				Name: "Call", Style: wsdl.OperationStyleOneWay,
				Input: &wsdl.OperationMessage11{
					Message: wsdl.QName{Namespace: "urn:test", Local: "Message"},
				},
			},
			{
				Name: "Call", Style: wsdl.OperationStyleOneWay,
				Input: &wsdl.OperationMessage11{
					Name: "Call", Message: wsdl.QName{Namespace: "urn:test", Local: "Message"},
				},
			},
		}}},
	}
	document, err := wsdl.NewDocument11(definitions, wsdl.ValidationOptions{})
	if err == nil || document != nil || !strings.Contains(err.Error(), "WSDL11_OPERATION_DUPLICATE") {
		t.Fatalf("NewDocument11() = (%v, %v)", document, err)
	}
}

func TestValidateOptionsAndDiagnosticBounds(t *testing.T) {
	t.Parallel()

	diagnostics := wsdl.Validate(nil, wsdl.ValidationOptions{})
	if len(diagnostics) != 1 || diagnostics[0].Code != "WSDL_DOCUMENT" {
		t.Fatalf("Validate(nil) = %#v", diagnostics)
	}
	diagnostics = wsdl.Validate(nil, wsdl.ValidationOptions{MaxDiagnostics: -1})
	if len(diagnostics) != 1 || diagnostics[0].Code != "WSDL_OPTIONS" {
		t.Fatalf("Validate(negative) = %#v", diagnostics)
	}
	document, err := wsdl.Parse(context.Background(), []byte(
		`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
			` targetNamespace="urn:test"><message name="A"><part name="a"/></message>`+
			`<message name="B"><part name="b"/></message></definitions>`,
	), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics = wsdl.Validate(document, wsdl.ValidationOptions{MaxDiagnostics: 1})
	if len(diagnostics) != 1 {
		t.Fatalf("Validate(MaxDiagnostics: 1) = %#v", diagnostics)
	}
}
