package wsdl_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestParseWSDL11CoreAndSOAPDescription(t *testing.T) {
	t.Parallel()

	const source = `<wsdl:definitions xmlns:wsdl="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:stock" xmlns:xsd="http://www.w3.org/2001/XMLSchema"` +
		` xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
		` name="StockQuote" targetNamespace="urn:stock">` +
		`<wsdl:documentation xml:lang="en">Stock prices</wsdl:documentation>` +
		`<wsdl:message name="GetPriceInput">` +
		`<wsdl:part name="symbol" type="xsd:string"/>` +
		`</wsdl:message>` +
		`<wsdl:message name="GetPriceOutput">` +
		`<wsdl:part name="price" type="xsd:decimal"/>` +
		`</wsdl:message>` +
		`<wsdl:portType name="StockQuotePortType">` +
		`<wsdl:operation name="GetPrice">` +
		`<wsdl:input message="tns:GetPriceInput"/>` +
		`<wsdl:output message="tns:GetPriceOutput"/>` +
		`</wsdl:operation></wsdl:portType>` +
		`<wsdl:binding name="StockQuoteSOAP" type="tns:StockQuotePortType">` +
		`<soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>` +
		`<wsdl:operation name="GetPrice">` +
		`<soap:operation soapAction="urn:get-price"/>` +
		`<wsdl:input><soap:body use="literal"/></wsdl:input>` +
		`<wsdl:output><soap:body use="literal"/></wsdl:output>` +
		`</wsdl:operation></wsdl:binding>` +
		`<wsdl:service name="StockQuoteService">` +
		`<wsdl:port name="StockQuotePort" binding="tns:StockQuoteSOAP">` +
		`<soap:address location="https://example.test/stock"/>` +
		`</wsdl:port></wsdl:service>` +
		`</wsdl:definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, ok := document.Definitions11()
	if !ok {
		t.Fatal("Definitions11() did not return the WSDL 1.1 model")
	}
	if definitions.Documentation == nil ||
		definitions.Documentation.Language != "en" ||
		definitions.Documentation.Content != "Stock prices" {
		t.Fatalf("Documentation = %#v", definitions.Documentation)
	}
	if len(definitions.Messages) != 2 ||
		definitions.Messages[0].Parts[0].Type != (wsdl.QName{
			Namespace: wsdl.NamespaceXMLSchema, Local: "string",
		}) {
		t.Fatalf("Messages = %#v", definitions.Messages)
	}
	if len(definitions.PortTypes) != 1 ||
		definitions.PortTypes[0].Operations[0].Input.Message.Local != "GetPriceInput" {
		t.Fatalf("PortTypes = %#v", definitions.PortTypes)
	}
	if len(definitions.Bindings) != 1 ||
		definitions.Bindings[0].SOAP == nil ||
		definitions.Bindings[0].SOAP.Style != wsdl.SOAPStyleDocument ||
		definitions.Bindings[0].Operations[0].SOAP.Action != "urn:get-price" ||
		definitions.Bindings[0].Operations[0].Input.SOAPBody.Use != wsdl.SOAPUseLiteral {
		t.Fatalf("Bindings = %#v", definitions.Bindings)
	}
	if len(definitions.Services) != 1 ||
		definitions.Services[0].Ports[0].SOAPAddress.Location !=
			"https://example.test/stock" {
		t.Fatalf("Services = %#v", definitions.Services)
	}
}

func TestParseWSDL11RejectsDuplicateTopLevelSymbols(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` targetNamespace="urn:stock">` +
		`<message name="Request"/><message name="Request"/>` +
		`</definitions>`

	_, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if !errors.Is(err, wsdl.ErrDuplicateSymbol) {
		t.Fatalf("Parse() error = %v, want ErrDuplicateSymbol", err)
	}
}

func TestWSDL11RejectsInvalidQNameAtEveryTypedBoundary(t *testing.T) {
	t.Parallel()

	replacements := map[string]string{
		`element="tns:Value"`:                  `element="missing:Value"`,
		`type="xs:string"`:                     `type="missing:Type"`,
		`message="tns:Request"`:                `message="missing:Request"`,
		`type="tns:API"`:                       `type="missing:API"`,
		`<soap12:header message="tns:Request"`: `<soap12:header message="missing:Request"`,
		`binding="tns:SOAP"`:                   `binding="missing:SOAP"`,
	}
	for needle, replacement := range replacements {
		if !strings.Contains(serializationWSDL11, needle) {
			t.Fatalf("fixture does not contain %q", needle)
		}
		source := strings.Replace(serializationWSDL11, needle, replacement, 1)
		if _, err := wsdl.Parse(
			context.Background(), []byte(source), wsdl.ParseOptions{},
		); err == nil {
			t.Errorf("Parse(replace %q) error = nil", needle)
		}
	}
}

func TestWSDL11RejectsDuplicateNestedSymbols(t *testing.T) {
	t.Parallel()

	bodies := []string{
		`<message name="Message"><part name="value" type="xs:string"/>` +
			`<part name="value" type="xs:string"/></message>`,
		`<message name="Message"/><portType name="Port"><operation name="Call">` +
			`<fault name="Failure" message="tns:Message"/><fault name="Failure"` +
			` message="tns:Message"/></operation></portType>`,
		`<message name="Message"/><portType name="Port"><operation name="Call">` +
			`<input name="Input" message="tns:Message"/></operation><operation name="Call">` +
			`<input name="Input" message="tns:Message"/></operation></portType>`,
		`<portType name="Port"/><binding name="Binding" type="tns:Port">` +
			`<operation name="Call"><input name="Input"/></operation>` +
			`<operation name="Call"><input name="Input"/></operation></binding>`,
		`<portType name="Port"/><binding name="Binding" type="tns:Port"/>` +
			`<service name="Service"><port name="Endpoint" binding="tns:Binding"/>` +
			`<port name="Endpoint" binding="tns:Binding"/></service>`,
	}
	for _, body := range bodies {
		source := `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test">` + body + `</definitions>`
		if _, err := wsdl.Parse(
			context.Background(), []byte(source), wsdl.ParseOptions{},
		); !errors.Is(err, wsdl.ErrDuplicateSymbol) {
			t.Errorf("Parse() error = %v, want ErrDuplicateSymbol", err)
		}
	}
}

func TestParseWSDL11TypesWithGoXSD(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:stock"><types>` +
		`<xs:schema targetNamespace="urn:stock">` +
		`<xs:element name="GetPrice" type="xs:string"/>` +
		`</xs:schema></types></definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, ok := document.Definitions11()
	if !ok {
		t.Fatal("Definitions11() did not return the WSDL 1.1 model")
	}
	if definitions.Types == nil || len(definitions.Types.Schemas) != 1 {
		t.Fatalf("Types = %#v", definitions.Types)
	}
	schema := definitions.Types.Schemas[0]
	if schema.TargetNamespace != "urn:stock" || len(schema.Elements) != 1 ||
		schema.Elements[0].Name != "GetPrice" {
		t.Fatalf("Schema = %#v", schema)
	}
}

func TestParseWSDL11HTTPAndMIMEBindings(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:asset"` +
		` xmlns:http="http://schemas.xmlsoap.org/wsdl/http/"` +
		` xmlns:mime="http://schemas.xmlsoap.org/wsdl/mime/"` +
		` targetNamespace="urn:asset">` +
		`<binding name="AssetHTTP" type="tns:AssetPortType">` +
		`<http:binding verb="GET"/>` +
		`<operation name="GetAsset"><http:operation location="/assets/(id)"/>` +
		`<input><http:urlReplacement/></input>` +
		`<output><mime:content part="asset" type="image/png"/></output>` +
		`</operation></binding>` +
		`<service name="AssetService"><port name="AssetPort" binding="tns:AssetHTTP">` +
		`<http:address location="https://example.test/api"/>` +
		`</port></service></definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	binding := definitions.Bindings[0]
	if binding.HTTP == nil || binding.HTTP.Verb != "GET" ||
		binding.Operations[0].HTTP.Location != "/assets/(id)" ||
		binding.Operations[0].Input.HTTP.Mode != wsdl.HTTPURLReplacement {
		t.Fatalf("HTTP binding = %#v", binding)
	}
	content := binding.Operations[0].Output.MIME.Contents[0]
	if content.Part != "asset" || content.Type != "image/png" {
		t.Fatalf("MIME content = %#v", content)
	}
	address := definitions.Services[0].Ports[0].HTTPAddress
	if address == nil || address.Location != "https://example.test/api" {
		t.Fatalf("HTTP address = %#v", address)
	}
}

func TestParseWSDL11ImportResolvesURIWithoutLoading(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xml:base="../" targetNamespace="urn:stock">` +
		`<import namespace="urn:common" location="common.wsdl"/>` +
		`</definitions>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{
		SystemID: "https://example.test/wsdl/service.wsdl",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	if len(definitions.Imports) != 1 {
		t.Fatalf("Imports = %#v", definitions.Imports)
	}
	importValue := definitions.Imports[0]
	if importValue.Namespace != "urn:common" ||
		importValue.Location != "common.wsdl" ||
		importValue.URI != "https://example.test/common.wsdl" {
		t.Fatalf("Import = %#v", importValue)
	}
}

func TestParseWSDL11PreservesSolicitResponseOperationOrder(t *testing.T) {
	t.Parallel()

	document, err := wsdl.Parse(context.Background(), []byte(
		`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/" `+
			`xmlns:tns="urn:test" targetNamespace="urn:test">`+
			`<message name="Request"/><message name="Response"/>`+
			`<portType name="Events"><operation name="Notify">`+
			`<output message="tns:Response"/><input message="tns:Request"/>`+
			`</operation></portType></definitions>`,
	), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	operation := definitions.PortTypes[0].Operations[0]
	if operation.Style != wsdl.OperationStyleSolicitResponse {
		t.Fatalf("operation style = %q", operation.Style)
	}

	encoded, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	outputIndex := strings.Index(string(encoded), "<output")
	inputIndex := strings.Index(string(encoded), "<input")
	if outputIndex < 0 || inputIndex < 0 || outputIndex > inputIndex {
		t.Fatalf("serialized operation order = %s", encoded)
	}
}

func TestWSDL11MIMEMultipartPreservesSOAPBody(t *testing.T) {
	t.Parallel()

	const source = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:tns="urn:test" xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"` +
		` xmlns:mime="http://schemas.xmlsoap.org/wsdl/mime/" targetNamespace="urn:test">` +
		`<binding name="Binding" type="tns:Port"><operation name="Call"><input>` +
		`<mime:multipartRelated><mime:part><soap:body use="literal" parts="body"/>` +
		`</mime:part><mime:part><mime:content part="image" type="image/png"/>` +
		`</mime:part></mime:multipartRelated></input></operation></binding></definitions>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	parts := definitions.Bindings[0].Operations[0].Input.MIME.Multipart[0].Parts
	if parts[0].SOAPBody == nil || parts[0].SOAPBody.Use != wsdl.SOAPUseLiteral ||
		parts[0].SOAPBody.Parts[0] != "body" {
		t.Fatalf("MIME parts = %#v", parts)
	}
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(payload), "<soap:body") {
		t.Fatalf("Marshal() = %s", payload)
	}
}
