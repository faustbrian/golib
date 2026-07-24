package wsdl_test

import (
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestSOAP12HeadersFaultsAndActionPresenceRoundTrip(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"` +
		` targetNamespace="urn:test"><message name="Request">` +
		`<part name="body" type="tns:Body"/></message><message name="Header">` +
		`<part name="token" type="tns:Token"/></message>` +
		`<portType name="API"><operation name="Call"><input message="tns:Request"/>` +
		`<output message="tns:Request"/>` +
		`<fault name="Failure" message="tns:Request"/></operation></portType>` +
		`<binding name="Binding" type="tns:API"><soap12:binding` +
		` transport="http://schemas.xmlsoap.org/soap/http"/>` +
		`<operation name="Call"><soap12:operation soapAction=""` +
		` soapActionRequired="false"/><input><soap12:body use="literal" parts=""/>` +
		`<soap12:header message="tns:Header" part="token" use="literal"` +
		` namespace="urn:headers" encodingStyle="urn:encoding">` +
		`<soap12:headerfault message="tns:Header" part="token" use="encoded"` +
		` namespace="urn:fault-headers" encodingStyle="urn:fault-encoding"/>` +
		`</soap12:header></input><fault name="Failure"><soap12:fault` +
		` name="Failure" use="literal" namespace="urn:fault"` +
		` encodingStyle="urn:fault-encoding"/></fault></operation>` +
		`</binding></definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertSOAP12BindingDetails(t, document)
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertSOAP12BindingDetails(t, roundTrip)
}

func assertSOAP12BindingDetails(t *testing.T, document *wsdl.Document) {
	t.Helper()
	definitions, _ := document.Definitions11()
	operation := definitions.Bindings[0].Operations[0]
	if operation.SOAP == nil || operation.SOAP.Version != wsdl.Version12 ||
		!operation.SOAP.ActionSet || operation.SOAP.Action != "" ||
		!operation.SOAP.ActionRequiredSet || operation.SOAP.ActionRequired {
		t.Fatalf("SOAP operation = %#v", operation.SOAP)
	}
	input := operation.Input
	if input.SOAPBody == nil || input.SOAPBody.Version != wsdl.Version12 ||
		!input.SOAPBody.UseSet || !input.SOAPBody.PartsSet ||
		len(input.SOAPBody.Parts) != 0 || len(input.SOAPHeaders) != 1 ||
		len(input.Extensions) != 0 {
		t.Fatalf("binding input = %#v", input)
	}
	header := input.SOAPHeaders[0]
	if header.Message.Local != "Header" || header.Part != "token" ||
		header.Use != wsdl.SOAPUseLiteral || header.Namespace != "urn:headers" ||
		len(header.EncodingStyle) != 1 || len(header.HeaderFaults) != 1 ||
		header.HeaderFaults[0].Use != wsdl.SOAPUseEncoded {
		t.Fatalf("SOAP header = %#v", header)
	}
	fault := operation.Faults[0].SOAPFault
	if fault == nil || fault.Version != wsdl.Version12 || fault.Name != "Failure" ||
		fault.Use != wsdl.SOAPUseLiteral || fault.Namespace != "urn:fault" ||
		len(fault.EncodingStyle) != 1 {
		t.Fatalf("SOAP fault = %#v", fault)
	}
}

func TestSOAP11EncodingStyleRemainsAURIList(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
		` targetNamespace="urn:test"><portType name="API"><operation name="Call"/>` +
		`</portType><binding name="Binding" type="tns:API"><operation name="Call">` +
		`<input><soap:body use="encoded" encodingStyle="urn:one urn:two"/>` +
		`</input></operation></binding></definitions>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	body := definitions.Bindings[0].Operations[0].Input.SOAPBody
	if body.Version != wsdl.Version11 || len(body.EncodingStyle) != 2 ||
		body.EncodingStyle[0] != "urn:one" || body.EncodingStyle[1] != "urn:two" {
		t.Fatalf("SOAP body = %#v", body)
	}
}

func TestSOAP12RejectsNonXMLActionRequiredBoolean(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"` +
		` targetNamespace="urn:test"><portType name="API"><operation name="Call"/>` +
		`</portType><binding name="Binding" type="tns:API"><operation name="Call">` +
		`<soap12:operation soapActionRequired="TRUE"/></operation>` +
		`</binding></definitions>`
	if _, err := wsdl.Parse(
		context.Background(),
		[]byte(source),
		wsdl.ParseOptions{},
	); err == nil {
		t.Fatal("Parse() error = nil")
	}
}

func TestValidateSOAPPartsHeadersAndFaultNames(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
		` targetNamespace="urn:test"><message name="Request">` +
		`<part name="body" type="tns:Body"/></message><message name="Header">` +
		`<part name="token" type="tns:Token"/></message>` +
		`<portType name="API"><operation name="Call"><input message="tns:Request"/>` +
		`<output message="tns:Request"/>` +
		`<fault name="Failure" message="tns:Request"/></operation></portType>` +
		`<binding name="Binding" type="tns:API"><operation name="Call"><input>` +
		`<soap:body use="literal" parts="missing"/>` +
		`<soap:header message="tns:Missing" part="value" use="literal"/>` +
		`<soap:header message="tns:Header" part="missing" use="literal"/>` +
		`</input><fault name="Failure"><soap:fault name="Other" use="literal"/>` +
		`</fault></operation></binding></definitions>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	want := []string{
		"WSDL11_SOAP_BODY_PART",
		"WSDL11_SOAP_HEADER_MESSAGE",
		"WSDL11_SOAP_HEADER_PART",
		"WSDL11_SOAP_FAULT_NAME",
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("Validate() = %#v, want %v", diagnostics, want)
	}
	for index, code := range want {
		if diagnostics[index].Code != code {
			t.Fatalf("diagnostic[%d] = %#v, want %s", index, diagnostics[index], code)
		}
	}
}
