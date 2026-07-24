package wsdl_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestParseWSDL20CoreDescription(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:stock" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:stock">` +
		`<documentation xml:lang="en">Stock prices</documentation>` +
		`<import namespace="urn:common" location="common.wsdl"/>` +
		`<interface name="StockQuote">` +
		`<fault name="InvalidSymbol" element="tns:InvalidSymbol"/>` +
		`<operation name="GetPrice" pattern="http://www.w3.org/ns/wsdl/in-out">` +
		`<input messageLabel="In" element="tns:GetPriceRequest"/>` +
		`<output messageLabel="Out" element="tns:GetPriceResponse"/>` +
		`<outfault ref="tns:InvalidSymbol" messageLabel="Out"/>` +
		`</operation></interface>` +
		`<binding name="StockQuoteSOAP" interface="tns:StockQuote"` +
		` type="http://www.w3.org/ns/wsdl/soap">` +
		`<operation ref="tns:GetPrice"/>` +
		`</binding>` +
		`<service name="StockQuoteService" interface="tns:StockQuote">` +
		`<endpoint name="StockQuoteEndpoint" binding="tns:StockQuoteSOAP"` +
		` address="https://example.test/stock"/>` +
		`</service></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	description, ok := document.Description20()
	if !ok {
		t.Fatal("Description20() did not return the WSDL 2.0 model")
	}
	if description.Documentation == nil ||
		description.Documentation.Content != "Stock prices" {
		t.Fatalf("Documentation = %#v", description.Documentation)
	}
	if len(description.Imports) != 1 ||
		description.Imports[0].Location != "common.wsdl" {
		t.Fatalf("Imports = %#v", description.Imports)
	}
	if len(description.Interfaces) != 1 ||
		description.Interfaces[0].Operations[0].Pattern != wsdl.MEPInOut ||
		description.Interfaces[0].Operations[0].Input.Element.Local !=
			"GetPriceRequest" ||
		description.Interfaces[0].Operations[0].OutFaults[0].Ref.Local !=
			"InvalidSymbol" {
		t.Fatalf("Interfaces = %#v", description.Interfaces)
	}
	if len(description.Bindings) != 1 ||
		description.Bindings[0].Interface.Local != "StockQuote" ||
		description.Bindings[0].Operations[0].Ref.Local != "GetPrice" {
		t.Fatalf("Bindings = %#v", description.Bindings)
	}
	if len(description.Services) != 1 ||
		description.Services[0].Endpoints[0].Address !=
			"https://example.test/stock" {
		t.Fatalf("Services = %#v", description.Services)
	}
}

func TestParseWSDL20EmbeddedSchemas(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:stock"><types>` +
		`<xs:schema targetNamespace="urn:stock">` +
		`<xs:element name="Stock" type="xs:string"/>` +
		`</xs:schema></types></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	description, _ := document.Description20()
	if description.Types == nil || len(description.Types.Schemas) != 1 ||
		description.Types.Schemas[0].TargetNamespace != "urn:stock" {
		t.Fatalf("Types = %#v", description.Types)
	}
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	description, _ = roundTrip.Description20()
	if description.Types == nil || len(description.Types.Schemas) != 1 ||
		description.Types.Schemas[0].TargetNamespace != "urn:stock" {
		t.Fatalf("round-trip Types = %#v", description.Types)
	}
}

func TestParseWSDL20EmbeddedSchemasRespectLimit(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:stock"><types>` +
		`<xs:schema/><xs:schema/></types></description>`

	_, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{
		MaxSchemas: 1,
	})
	if !errors.Is(err, wsdl.ErrLimitExceeded) {
		t.Fatalf("Parse() error = %v, want ErrLimitExceeded", err)
	}
}

func TestWSDL20DirectSchemaImportsRoundTrip(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:test"><types>` +
		`<xs:import namespace="urn:types" schemaLocation="schemas/types.xsd"/>` +
		`</types></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{
		SystemID: "https://example.test/root.wsdl",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	description, _ := document.Description20()
	if description.Types == nil || len(description.Types.Imports) != 1 ||
		description.Types.Imports[0].Namespace != "urn:types" ||
		description.Types.Imports[0].Location != "schemas/types.xsd" ||
		description.Types.Imports[0].URI != "https://example.test/schemas/types.xsd" {
		t.Fatalf("Types = %#v", description.Types)
	}
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{
		SystemID: "https://example.test/root.wsdl",
	})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	description, _ = roundTrip.Description20()
	if len(description.Types.Imports) != 1 ||
		description.Types.Imports[0].Namespace != "urn:types" {
		t.Fatalf("round-trip Types = %#v", description.Types)
	}
}

func TestWSDL20MessageContentModelsRoundTrip(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"><interface name="API">` +
		`<fault name="AnyFault" element="#any"/>` +
		`<fault name="DefaultFault"/>` +
		`<operation name="Call" pattern="http://www.w3.org/ns/wsdl/in-out">` +
		`<input messageLabel="In" element="#none"/>` +
		`<output messageLabel="Out" element="#other"/>` +
		`</operation></interface></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertWSDL20MessageContentModels(t, document)
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertWSDL20MessageContentModels(t, roundTrip)
}

func assertWSDL20MessageContentModels(t *testing.T, document *wsdl.Document) {
	t.Helper()
	description, _ := document.Description20()
	interfaceValue := description.Interfaces[0]
	if interfaceValue.Faults[0].MessageContentModel != wsdl.MessageContentAny ||
		!interfaceValue.Faults[0].MessageContentModelSet ||
		interfaceValue.Faults[1].MessageContentModel != wsdl.MessageContentOther ||
		interfaceValue.Faults[1].MessageContentModelSet ||
		interfaceValue.Operations[0].Input.MessageContentModel != wsdl.MessageContentNone ||
		interfaceValue.Operations[0].Output.MessageContentModel != wsdl.MessageContentOther {
		t.Fatalf("Interface = %#v", interfaceValue)
	}
}

func TestParseWSDL20RejectsDuplicateComponents(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"interfaces": `<interface name="Duplicate"/>` +
			`<interface name="Duplicate"/>`,
		"interface faults": `<interface name="API">` +
			`<fault name="Duplicate" element="tns:Fault"/>` +
			`<fault name="Duplicate" element="tns:Fault"/>` +
			`</interface>`,
		"interface operations": `<interface name="API">` +
			`<operation name="Duplicate" pattern="urn:in-only"/>` +
			`<operation name="Duplicate" pattern="urn:in-only"/>` +
			`</interface>`,
		"binding operations": `<binding name="API" interface="tns:API" type="urn:test">` +
			`<operation ref="tns:Duplicate"/>` +
			`<operation ref="tns:Duplicate"/>` +
			`</binding>`,
		"service endpoints": `<service name="API" interface="tns:API">` +
			`<endpoint name="Duplicate" binding="tns:API"/>` +
			`<endpoint name="Duplicate" binding="tns:API"/>` +
			`</service>`,
	}

	for name, body := range tests {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := `<description xmlns="http://www.w3.org/ns/wsdl"` +
				` xmlns:tns="urn:test" targetNamespace="urn:test">` +
				body + `</description>`
			_, err := wsdl.Parse(
				context.Background(),
				[]byte(source),
				wsdl.ParseOptions{},
			)
			if !errors.Is(err, wsdl.ErrDuplicateSymbol) {
				t.Fatalf("Parse() error = %v, want ErrDuplicateSymbol", err)
			}
		})
	}
}

func TestWSDL20PreservesMultipleInterfaceMessageReferences(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"><interface name="API"><operation name="Exchange"` +
		` pattern="urn:multi"><input messageLabel="First" element="#none"/>` +
		`<input messageLabel="Second" element="#any"/>` +
		`<output messageLabel="Third" element="#other"/>` +
		`<output messageLabel="Fourth" element="#none"/></operation>` +
		`</interface></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertMessages := func(document *wsdl.Document) {
		description, _ := document.Description20()
		operation := description.Interfaces[0].Operations[0]
		if len(operation.Inputs) != 2 || len(operation.Outputs) != 2 ||
			operation.Inputs[0].MessageLabel != "First" ||
			operation.Inputs[1].MessageLabel != "Second" ||
			operation.Outputs[1].MessageLabel != "Fourth" {
			t.Fatalf("operation messages = %#v / %#v", operation.Inputs, operation.Outputs)
		}
	}
	assertMessages(document)
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertMessages(roundTrip)
}

func TestParseWSDL20ImportAndIncludeResolveURIsWithoutLoading(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xml:base="descriptions/" targetNamespace="urn:stock">` +
		`<import namespace="urn:common" location="common.wsdl"/>` +
		`<include location="stock-extra.wsdl"/>` +
		`</description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{
		SystemID: "https://example.test/root.wsdl",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	description, _ := document.Description20()
	if description.Imports[0].URI !=
		"https://example.test/descriptions/common.wsdl" ||
		description.Includes[0].URI !=
			"https://example.test/descriptions/stock-extra.wsdl" {
		t.Fatalf(
			"Import URI = %q, include URI = %q",
			description.Imports[0].URI,
			description.Includes[0].URI,
		)
	}
}

func TestWSDL20RejectsInvalidQNameAtEveryTypedBoundary(t *testing.T) {
	t.Parallel()

	replacements := map[string]string{
		`element="tns:Failure"`:                             `element="missing:Failure"`,
		`element="tns:Request"`:                             `element="missing:Request"`,
		`element="tns:Response"`:                            `element="missing:Response"`,
		`ref="tns:Failure"`:                                 `ref="missing:Failure"`,
		`interface="tns:API"`:                               `interface="missing:API"`,
		`wsoap:code="env:Sender"`:                           `wsoap:code="missing:Sender"`,
		`<operation ref="tns:Call"`:                         `<operation ref="missing:Call"`,
		`<wsoap:header element="tns:Request"`:               `<wsoap:header element="missing:Request"`,
		`<whttp:header name="Retry-After" type="xs:string"`: `<whttp:header name="Retry-After" type="missing:Type"`,
		`<service name="Service" interface="tns:API"`:       `<service name="Service" interface="missing:API"`,
		`binding="tns:SOAP"`:                                `binding="missing:SOAP"`,
	}
	for needle, replacement := range replacements {
		if !strings.Contains(serializationWSDL20, needle) {
			t.Fatalf("fixture does not contain %q", needle)
		}
		source := strings.Replace(serializationWSDL20, needle, replacement, 1)
		if _, err := wsdl.Parse(
			context.Background(), []byte(source), wsdl.ParseOptions{},
		); err == nil {
			t.Errorf("Parse(replace %q) error = nil", needle)
		}
	}
	const invalidExtends = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"><interface name="API" extends="missing:Parent"/>` +
		`</description>`
	if _, err := wsdl.Parse(
		context.Background(), []byte(invalidExtends), wsdl.ParseOptions{},
	); err == nil {
		t.Fatal("Parse(invalid extends) error = nil")
	}
}

func TestWSDL20RejectsInvalidBooleanAtEveryAdjunctBoundary(t *testing.T) {
	t.Parallel()

	needles := []string{
		`wsdlx:safe="true"`,
		`<wsoap:module ref="urn:module" required="true"`,
		`mustUnderstand="true"`,
		`required="false"`,
		`whttp:cookies="true"`,
		`whttp:ignoreUncited="true"`,
		`<whttp:header name="X-Request-ID" type="xs:string" required="true"`,
	}
	for _, needle := range needles {
		if !strings.Contains(serializationWSDL20, needle) {
			t.Fatalf("fixture does not contain %q", needle)
		}
		invalid := strings.Replace(needle, `"true"`, `"maybe"`, 1)
		invalid = strings.Replace(invalid, `"false"`, `"maybe"`, 1)
		source := strings.Replace(serializationWSDL20, needle, invalid, 1)
		if _, err := wsdl.Parse(
			context.Background(), []byte(source), wsdl.ParseOptions{},
		); err == nil {
			t.Errorf("Parse(replace %q) error = nil", needle)
		}
	}
}
