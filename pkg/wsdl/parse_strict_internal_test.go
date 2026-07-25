package wsdl

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

const strictDescription20Prefix = `<description xmlns="http://www.w3.org/ns/wsdl"` +
	` xmlns:wsdl="http://www.w3.org/ns/wsdl" xmlns:tns="urn:test"` +
	` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
	` xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
	` xmlns:whttp="http://www.w3.org/ns/wsdl/http" xmlns:ext="urn:extension"` +
	` targetNamespace="urn:test">`

const invalidRequiredExtension20 = `<ext:invalid wsdl:required="invalid"/>`

const strictDefinitions11Prefix = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
	` xmlns:wsdl="http://schemas.xmlsoap.org/wsdl/" xmlns:tns="urn:test"` +
	` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
	` xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
	` xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"` +
	` xmlns:http="http://schemas.xmlsoap.org/wsdl/http/"` +
	` xmlns:mime="http://schemas.xmlsoap.org/wsdl/mime/" xmlns:ext="urn:extension"` +
	` targetNamespace="urn:test">`

const invalidRequiredExtension11 = `<ext:invalid wsdl:required="invalid"/>`

func TestParseRejectsMalformedWSDL20AtEveryNestedDecoderBoundary(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"description extension": invalidRequiredExtension20,
		"import extension": `<import namespace="urn:other" location="other.wsdl">` +
			invalidRequiredExtension20 + `</import>`,
		"include extension": `<include location="other.wsdl">` +
			invalidRequiredExtension20 + `</include>`,
		"types extension":     `<types>` + invalidRequiredExtension20 + `</types>`,
		"interface extension": `<interface name="API">` + invalidRequiredExtension20 + `</interface>`,
		"interface fault extension": `<interface name="API"><fault name="Failure" element="#none">` +
			invalidRequiredExtension20 + `</fault></interface>`,
		"interface operation extension": `<interface name="API"><operation name="Call" pattern="urn:mep">` +
			invalidRequiredExtension20 + `</operation></interface>`,
		"interface input extension": `<interface name="API"><operation name="Call" pattern="urn:mep">` +
			`<input element="#none">` + invalidRequiredExtension20 + `</input></operation></interface>`,
		"interface fault reference QName": `<interface name="API"><operation name="Call" pattern="urn:mep">` +
			`<infault ref="missing:Failure"/></operation></interface>`,
		"interface fault reference extension": `<interface name="API"><operation name="Call" pattern="urn:mep">` +
			`<infault ref="tns:Failure">` + invalidRequiredExtension20 + `</infault></operation></interface>`,
		"binding interface QName": `<binding name="Binding" interface="missing:API" type="urn:binding"/>`,
		"binding extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			invalidRequiredExtension20 + `</binding>`,
		"binding fault QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<fault ref="missing:Failure"/></binding>`,
		"binding fault extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<fault ref="tns:Failure">` + invalidRequiredExtension20 + `</fault></binding>`,
		"binding operation QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="missing:Call"/></binding>`,
		"binding operation extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call">` + invalidRequiredExtension20 + `</operation></binding>`,
		"binding input extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><input>` + invalidRequiredExtension20 +
			`</input></operation></binding>`,
		"binding output extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><output>` + invalidRequiredExtension20 +
			`</output></operation></binding>`,
		"binding fault reference QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><infault ref="missing:Failure"/></operation></binding>`,
		"binding outfault reference QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><outfault ref="missing:Failure"/></operation></binding>`,
		"binding fault reference extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><infault ref="tns:Failure">` + invalidRequiredExtension20 +
			`</infault></operation></binding>`,
		"SOAP module required": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><input><wsoap:module ref="urn:module" required="invalid"/>` +
			`</input></operation></binding>`,
		"SOAP header element QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><input><wsoap:header element="missing:Header"/>` +
			`</input></operation></binding>`,
		"HTTP header type QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><input><whttp:header name="X-Test" type="missing:Type"/>` +
			`</input></operation></binding>`,
		"HTTP header extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><input><whttp:header name="X-Test" type="tns:Type">` +
			invalidRequiredExtension20 + `</whttp:header></input></operation></binding>`,
		"SOAP fault code QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<fault ref="tns:Failure" wsoap:code="missing:Code"/></binding>`,
		"SOAP fault subcode QName": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<fault ref="tns:Failure" wsoap:subcodes="missing:Code"/></binding>`,
		"SOAP fault module": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<fault ref="tns:Failure"><wsoap:module ref="urn:module" required="invalid"/></fault></binding>`,
		"SOAP operation module": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><wsoap:module ref="urn:module" required="invalid"/></operation></binding>`,
		"SOAP fault reference module": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><infault ref="tns:Failure">` +
			`<wsoap:module ref="urn:module" required="invalid"/></infault></operation></binding>`,
		"SOAP module extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<wsoap:module ref="urn:module">` + invalidRequiredExtension20 + `</wsoap:module></binding>`,
		"SOAP header extension": `<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<operation ref="tns:Call"><input><wsoap:header element="tns:Header">` +
			invalidRequiredExtension20 + `</wsoap:header></input></operation></binding>`,
		"service interface QName": `<service name="Service" interface="missing:API"/>`,
		"service extension": `<service name="Service" interface="tns:API">` +
			invalidRequiredExtension20 + `</service>`,
		"endpoint binding QName": `<service name="Service" interface="tns:API">` +
			`<endpoint name="Endpoint" binding="missing:Binding"/></service>`,
		"endpoint extension": `<service name="Service" interface="tns:API">` +
			`<endpoint name="Endpoint" binding="tns:Binding">` + invalidRequiredExtension20 +
			`</endpoint></service>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(context.Background(), []byte(strictDescription20Prefix+body+`</description>`), ParseOptions{}); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
}

func TestParseRejectsWSDL20DuplicateCollectionsAndSchemaBounds(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"bindings": `<binding name="Duplicate" type="urn:binding"/><binding name="Duplicate" type="urn:binding"/>`,
		"services": `<service name="Duplicate" interface="tns:API"/><service name="Duplicate" interface="tns:API"/>`,
		"binding faults": `<binding name="Binding" type="urn:binding"><fault ref="tns:Failure"/>` +
			`<fault ref="tns:Failure"/></binding>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(context.Background(), []byte(strictDescription20Prefix+body+`</description>`), ParseOptions{}); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
	imports := `<types><xs:import/><xs:import/></types>`
	if _, err := Parse(context.Background(), []byte(strictDescription20Prefix+imports+`</description>`), ParseOptions{MaxSchemas: 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Parse(schema limit) error = %v", err)
	}
	invalidSchema := `<types><xs:schema><xs:element name="Value" type="missing:Type"/></xs:schema></types>`
	if _, err := Parse(context.Background(), []byte(strictDescription20Prefix+invalidSchema+`</description>`), ParseOptions{}); err == nil {
		t.Fatal("Parse(invalid schema) error = nil")
	}
	invalidImport := `<types><xs:import schemaLocation="relative.xsd"/></types>`
	if _, err := Parse(context.Background(), []byte(strictDescription20Prefix+invalidImport+`</description>`), ParseOptions{SystemID: "%"}); err == nil {
		t.Fatal("Parse(invalid schema import base) error = nil")
	}
}

func TestParseAcceptsWSDL20SOAPAnyFaultCodes(t *testing.T) {
	t.Parallel()

	source := strictDescription20Prefix + `<binding name="Binding" type="urn:binding">` +
		`<fault ref="tns:Failure" wsoap:code="#any" wsoap:subcodes="#any"/></binding></description>`
	document, err := Parse(context.Background(), []byte(source), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	description, _ := document.Description20()
	soap := description.Bindings[0].Faults[0].SOAP
	if soap == nil || !soap.CodeAny || !soap.SubcodesAny {
		t.Fatalf("SOAP fault = %#v", soap)
	}
}

func TestParseRejectsInvalidImportAndIncludeBases(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"WSDL 1.1 import": strictDefinitions11Prefix +
			`<import namespace="urn:other" location="relative.wsdl"/></definitions>`,
		"WSDL 2.0 import": strictDescription20Prefix +
			`<import namespace="urn:other" location="relative.wsdl"/></description>`,
		"WSDL 2.0 include": strictDescription20Prefix +
			`<include location="relative.wsdl"/></description>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(context.Background(), []byte(source), ParseOptions{SystemID: "%"}); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
}

func TestNestedDecodersPropagateXMLNodeSerializationFailure(t *testing.T) {
	injected := errors.New("injected node serialization failure")
	originalMarshalNode := marshalNode
	marshalNode = func(*xmlNode) ([]byte, error) { return nil, injected }
	t.Cleanup(func() { marshalNode = originalMarshalNode })

	if _, err := decodeExtension(&xmlNode{}, NamespaceWSDL11); !errors.Is(err, injected) {
		t.Fatalf("decodeExtension() error = %v", err)
	}
	schema := &xmlNode{name: xml.Name{Space: NamespaceXMLSchema, Local: "schema"}}
	types := &xmlNode{children: []*xmlNode{schema}}
	if _, err := decodeTypes11(context.Background(), types, ParseOptions{MaxSchemas: 1}); !errors.Is(err, injected) {
		t.Fatalf("decodeTypes11() error = %v", err)
	}
	if _, err := decodeTypes20(context.Background(), types, ParseOptions{MaxSchemas: 1}); !errors.Is(err, injected) {
		t.Fatalf("decodeTypes20() error = %v", err)
	}
}

func TestParsePropagatesTopLevelDecoderFailures(t *testing.T) {
	originalDecoderToken := decoderToken
	t.Cleanup(func() { decoderToken = originalDecoderToken })

	for name, injected := range map[string]error{
		"empty":     io.EOF,
		"decoder":   errors.New("injected decoder failure"),
		"nil token": nil,
	} {
		t.Run(name, func(t *testing.T) {
			decoderToken = func(*xml.Decoder) (xml.Token, error) { return nil, injected }
			_, err := Parse(context.Background(), []byte(`<description xmlns="http://www.w3.org/ns/wsdl"/>`), ParseOptions{})
			if err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
}

func TestVersionDecodersPropagateTreeAndComponentLimitFailures(t *testing.T) {
	t.Parallel()

	for name, parse := range map[string]func() error{
		"WSDL 1.1 tree": func() error {
			_, err := parseDefinitions11(xml.NewDecoder(strings.NewReader("")), xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL11, Local: "definitions"},
			}, &parseState{options: ParseOptions{MaxDepth: 1, MaxElements: 1, MaxAttributes: 1, MaxTextBytes: 1}})
			return err
		},
		"WSDL 2.0 tree": func() error {
			_, err := parseDescription20(xml.NewDecoder(strings.NewReader("")), xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL20, Local: "description"},
			}, &parseState{options: ParseOptions{MaxDepth: 1, MaxElements: 1, MaxAttributes: 1, MaxTextBytes: 1}})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := parse(); err == nil {
				t.Fatal("parse version root error = nil")
			}
		})
	}
	source := strictDefinitions11Prefix + `<service name="Service">` +
		`<port name="One" binding="tns:Binding"/><port name="Two" binding="tns:Binding"/>` +
		`</service></definitions>`
	if _, err := Parse(context.Background(), []byte(source), ParseOptions{MaxEndpoints: 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Parse(endpoint limit) error = %v", err)
	}
}

func TestParseRejectsGeneralXMLAndResourceBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := Parse(context.Background(), []byte(`<description xmlns="http://www.w3.org/ns/wsdl"/>`), ParseOptions{
		MaxExtensions: -1,
	}); err == nil {
		t.Fatal("Parse(negative limit) error = nil")
	}
	if _, err := Parse(context.Background(), []byte(`<description/>`), ParseOptions{MaxDocumentBytes: 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Parse(byte limit) error = %v", err)
	}
	for name, source := range map[string]string{
		"malformed XML":    `<description`,
		"unsupported root": `<schema/>`,
		"DTD":              `<!DOCTYPE description><description xmlns="http://www.w3.org/ns/wsdl"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(context.Background(), []byte(source), ParseOptions{}); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
	for name, source := range map[string]string{
		"WSDL 1.1": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/" xml:base="relative"/>`,
		"WSDL 2.0": `<description xmlns="http://www.w3.org/ns/wsdl" xml:base="relative"/>`,
	} {
		t.Run(name+" invalid base", func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(context.Background(), []byte(source), ParseOptions{SystemID: "%"}); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
}

func TestParseRejectsMalformedWSDL11AtEveryNestedDecoderBoundary(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"import extension": `<import namespace="urn:other" location="other.wsdl">` +
			invalidRequiredExtension11 + `</import>`,
		"message extension":  `<message name="Message">` + invalidRequiredExtension11 + `</message>`,
		"types extension":    `<types>` + invalidRequiredExtension11 + `</types>`,
		"part element QName": `<message name="Message"><part name="value" element="missing:Value"/></message>`,
		"part type QName":    `<message name="Message"><part name="value" type="missing:Value"/></message>`,
		"part extension": `<message name="Message"><part name="value" type="xs:string">` +
			invalidRequiredExtension11 + `</part></message>`,
		"port type extension": `<portType name="Port">` + invalidRequiredExtension11 + `</portType>`,
		"operation extension": `<portType name="Port"><operation name="Call">` +
			invalidRequiredExtension11 + `</operation></portType>`,
		"operation message QName": `<portType name="Port"><operation name="Call">` +
			`<input message="missing:Message"/></operation></portType>`,
		"operation message extension": `<portType name="Port"><operation name="Call">` +
			`<input message="tns:Message">` + invalidRequiredExtension11 + `</input></operation></portType>`,
		"binding type QName": `<binding name="Binding" type="missing:Port"/>`,
		"binding extension": `<binding name="Binding" type="tns:Port">` +
			invalidRequiredExtension11 + `</binding>`,
		"binding operation extension": `<binding name="Binding" type="tns:Port">` +
			`<operation name="Call">` + invalidRequiredExtension11 + `</operation></binding>`,
		"binding message extension": `<binding name="Binding" type="tns:Port">` +
			`<operation name="Call"><input>` + invalidRequiredExtension11 + `</input></operation></binding>`,
		"binding output extension": `<binding name="Binding" type="tns:Port">` +
			`<operation name="Call"><output>` + invalidRequiredExtension11 + `</output></operation></binding>`,
		"binding fault extension": `<binding name="Binding" type="tns:Port">` +
			`<operation name="Call"><fault name="Failure">` + invalidRequiredExtension11 + `</fault></operation></binding>`,
		"SOAP header message QName": `<binding name="Binding" type="tns:Port"><operation name="Call">` +
			`<input><soap:header message="missing:Message" part="value" use="literal"/>` +
			`</input></operation></binding>`,
		"SOAP header fault QName": `<binding name="Binding" type="tns:Port"><operation name="Call">` +
			`<input><soap:header message="tns:Message" part="value" use="literal">` +
			`<soap:headerfault message="missing:Message" part="value" use="literal"/>` +
			`</soap:header></input></operation></binding>`,
		"service extension":  `<service name="Service">` + invalidRequiredExtension11 + `</service>`,
		"port binding QName": `<service name="Service"><port name="Port" binding="missing:Binding"/></service>`,
		"port extension": `<service name="Service"><port name="Port" binding="tns:Binding">` +
			invalidRequiredExtension11 + `</port></service>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(context.Background(), []byte(strictDefinitions11Prefix+body+`</definitions>`), ParseOptions{}); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
}

func TestParseRejectsWSDL11DuplicateCollectionsAndSchemaBounds(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"port types": `<portType name="Duplicate"/><portType name="Duplicate"/>`,
		"bindings":   `<binding name="Duplicate" type="tns:Port"/><binding name="Duplicate" type="tns:Port"/>`,
		"services":   `<service name="Duplicate"/><service name="Duplicate"/>`,
		"inputs": `<portType name="Port"><operation name="Call"><input message="tns:Message"/>` +
			`<input message="tns:Message"/></operation></portType>`,
		"outputs": `<portType name="Port"><operation name="Call"><output message="tns:Message"/>` +
			`<output message="tns:Message"/></operation></portType>`,
		"binding faults": `<binding name="Binding" type="tns:Port"><operation name="Call">` +
			`<fault name="Duplicate"/><fault name="Duplicate"/></operation></binding>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(context.Background(), []byte(strictDefinitions11Prefix+body+`</definitions>`), ParseOptions{}); err == nil {
				t.Fatal("Parse() error = nil")
			}
		})
	}
	types := `<types><xs:schema/><xs:schema/></types>`
	if _, err := Parse(context.Background(), []byte(strictDefinitions11Prefix+types+`</definitions>`), ParseOptions{MaxSchemas: 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Parse(schema limit) error = %v", err)
	}
}

func TestParseClassifiesWSDL11NotificationOperation(t *testing.T) {
	t.Parallel()

	source := strictDefinitions11Prefix + `<portType name="Port"><operation name="Notify">` +
		`<output message="tns:Message"/></operation></portType></definitions>`
	document, err := Parse(context.Background(), []byte(source), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	if definitions.PortTypes[0].Operations[0].Style != OperationStyleNotification {
		t.Fatalf("operation = %#v", definitions.PortTypes[0].Operations[0])
	}
}

func TestParseIgnoresNonProtocolChildrenInWSDL11AdjunctContainers(t *testing.T) {
	t.Parallel()

	source := strictDefinitions11Prefix + `<binding name="Binding" type="tns:Port">` +
		`<operation name="Call"><input><soap:header message="tns:Message" part="value"` +
		` use="literal"><ext:ignored/></soap:header><mime:multipartRelated>` +
		`<ext:ignored/><mime:part/></mime:multipartRelated></input></operation></binding></definitions>`
	if _, err := Parse(context.Background(), []byte(source), ParseOptions{}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
}
