package wsdl_test

import (
	"bytes"
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestMarshalWSDL11IsDeterministicAndRoundTrips(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:echo" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
		` name="Echo" targetNamespace="urn:echo">` +
		`<message name="EchoInput"><part name="value" type="xs:string"/></message>` +
		`<portType name="EchoPortType"><operation name="Echo">` +
		`<input message="tns:EchoInput"/></operation></portType>` +
		`<binding name="EchoSOAP" type="tns:EchoPortType">` +
		`<soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>` +
		`<operation name="Echo"><soap:operation soapAction="urn:echo"/>` +
		`<input><soap:body use="literal"/></input></operation></binding>` +
		`<service name="EchoService"><port name="EchoPort" binding="tns:EchoSOAP">` +
		`<soap:address location="https://example.test/echo"/>` +
		`</port></service></definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	first, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	second, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Marshal() second error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Marshal() is not deterministic:\n%s\n%s", first, second)
	}
	for _, element := range []string{
		"<soap:binding", "<soap:operation", "<soap:body", "<soap:address",
	} {
		if bytes.Count(first, []byte(element)) != 1 {
			t.Fatalf("Marshal() duplicated typed extension %q:\n%s", element, first)
		}
	}
	roundTrip, err := wsdl.Parse(context.Background(), first, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v; output:\n%s", err, first)
	}
	definitions, _ := roundTrip.Definitions11()
	if definitions.Name != "Echo" || len(definitions.Messages) != 1 ||
		len(definitions.Bindings) != 1 || len(definitions.Services) != 1 ||
		definitions.Services[0].Ports[0].SOAPAddress.Location !=
			"https://example.test/echo" {
		t.Fatalf("round-trip definitions = %#v", definitions)
	}
}

func TestMarshalWSDL20IsDeterministicAndRoundTrips(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:echo" targetNamespace="urn:echo">` +
		`<interface name="EchoInterface"><fault name="EchoFault"` +
		` element="tns:EchoFault"/><operation name="Echo"` +
		` pattern="http://www.w3.org/ns/wsdl/in-out">` +
		`<input messageLabel="In" element="tns:EchoRequest"/>` +
		`<output messageLabel="Out" element="tns:EchoResponse"/>` +
		`<outfault messageLabel="Out" ref="tns:EchoFault"/>` +
		`</operation></interface>` +
		`<binding name="EchoBinding" interface="tns:EchoInterface"` +
		` type="http://www.w3.org/ns/wsdl/soap">` +
		`<operation ref="tns:Echo"/></binding>` +
		`<service name="EchoService" interface="tns:EchoInterface">` +
		`<endpoint name="EchoEndpoint" binding="tns:EchoBinding"` +
		` address="https://example.test/echo"/>` +
		`</service></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	first, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	second, err := wsdl.Marshal(document, wsdl.MarshalOptions{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Marshal() second error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Marshal() is not deterministic:\n%s\n%s", first, second)
	}
	roundTrip, err := wsdl.Parse(context.Background(), first, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v; output:\n%s", err, first)
	}
	description, _ := roundTrip.Description20()
	if len(description.Interfaces) != 1 || len(description.Bindings) != 1 ||
		len(description.Services) != 1 ||
		description.Services[0].Endpoints[0].Address !=
			"https://example.test/echo" {
		t.Fatalf("round-trip description = %#v", description)
	}
}
