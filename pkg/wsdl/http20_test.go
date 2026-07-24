package wsdl_test

import (
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestWSDL20HTTPBindingRoundTrip(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:test" xmlns:whttp="http://www.w3.org/ns/wsdl/http"` +
		` targetNamespace="urn:test"><interface name="API">` +
		`<fault name="Failure" element="tns:Failure"/>` +
		`<operation name="Call" pattern="http://www.w3.org/ns/wsdl/in-out">` +
		`<input messageLabel="In" element="tns:Request"/>` +
		`<output messageLabel="Out" element="tns:Response"/>` +
		`<outfault ref="tns:Failure" messageLabel="Out"/>` +
		`</operation></interface>` +
		`<binding name="HTTP" interface="tns:API"` +
		` type="http://www.w3.org/ns/wsdl/http" whttp:methodDefault="POST"` +
		` whttp:queryParameterSeparatorDefault=";" whttp:cookies="false"` +
		` whttp:contentEncodingDefault="gzip">` +
		`<fault ref="tns:Failure" whttp:code="500" whttp:contentEncoding="br">` +
		`<whttp:header name="Retry-After" type="tns:RetryAfter" required="false"/>` +
		`</fault><operation ref="tns:Call" whttp:location="items/{id}"` +
		` whttp:method="PUT" whttp:inputSerialization="application/json"` +
		` whttp:outputSerialization="application/json"` +
		` whttp:faultSerialization="application/problem+json"` +
		` whttp:queryParameterSeparator="&amp;"` +
		` whttp:contentEncodingDefault="identity" whttp:ignoreUncited="true">` +
		`<input messageLabel="In" whttp:contentEncoding="gzip">` +
		`<whttp:header name="X-Request-ID" type="tns:RequestID" required="true"/>` +
		`</input><output messageLabel="Out" whttp:contentEncoding="br"/>` +
		`<outfault ref="tns:Failure" messageLabel="Out"/>` +
		`</operation></binding><service name="Service" interface="tns:API">` +
		`<endpoint name="Endpoint" binding="tns:HTTP" address="https://example.test"` +
		` whttp:authenticationScheme="basic" whttp:authenticationRealm="api"/>` +
		`</service></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertWSDL20HTTPBinding(t, document)
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertWSDL20HTTPBinding(t, roundTrip)
}

func assertWSDL20HTTPBinding(t *testing.T, document *wsdl.Document) {
	t.Helper()

	description, _ := document.Description20()
	binding := description.Bindings[0]
	if binding.HTTP == nil || binding.HTTP.MethodDefault != "POST" ||
		!binding.HTTP.MethodDefaultSet ||
		binding.HTTP.QueryParameterSeparatorDefault != ";" ||
		!binding.HTTP.QueryParameterSeparatorDefaultSet || binding.HTTP.Cookies ||
		!binding.HTTP.CookiesSet || binding.HTTP.ContentEncodingDefault != "gzip" ||
		!binding.HTTP.ContentEncodingDefaultSet {
		t.Fatalf("Binding HTTP = %#v", binding.HTTP)
	}
	fault := binding.Faults[0].HTTP
	if fault == nil || fault.Code != "500" || !fault.CodeSet ||
		fault.ContentEncoding != "br" || !fault.ContentEncodingSet ||
		len(fault.Headers) != 1 || fault.Headers[0].Name != "Retry-After" ||
		fault.Headers[0].Type.Local != "RetryAfter" || fault.Headers[0].Required ||
		!fault.Headers[0].RequiredSet {
		t.Fatalf("Fault HTTP = %#v", fault)
	}
	operation := binding.Operations[0]
	if operation.HTTP == nil || operation.HTTP.Location != "items/{id}" ||
		!operation.HTTP.LocationSet || operation.HTTP.Method != "PUT" ||
		!operation.HTTP.MethodSet || operation.HTTP.InputSerialization != "application/json" ||
		operation.HTTP.OutputSerialization != "application/json" ||
		operation.HTTP.FaultSerialization != "application/problem+json" ||
		operation.HTTP.QueryParameterSeparator != "&" ||
		operation.HTTP.ContentEncodingDefault != "identity" ||
		!operation.HTTP.IgnoreUncited || !operation.HTTP.IgnoreUncitedSet {
		t.Fatalf("Operation HTTP = %#v", operation.HTTP)
	}
	input := operation.Inputs[0].HTTP
	if input == nil || input.ContentEncoding != "gzip" ||
		!input.ContentEncodingSet || len(input.Headers) != 1 ||
		!input.Headers[0].Required {
		t.Fatalf("Input HTTP = %#v", input)
	}
	endpoint := description.Services[0].Endpoints[0].HTTP
	if endpoint == nil || endpoint.AuthenticationScheme != "basic" ||
		!endpoint.AuthenticationSchemeSet || endpoint.AuthenticationRealm != "api" ||
		!endpoint.AuthenticationRealmSet {
		t.Fatalf("Endpoint HTTP = %#v", endpoint)
	}
}

func TestParseWSDL20HTTPRejectsInvalidBoolean(t *testing.T) {
	t.Parallel()

	_, err := wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` xmlns:whttp="http://www.w3.org/ns/wsdl/http"`+
			` targetNamespace="urn:test"><binding name="HTTP"`+
			` type="http://www.w3.org/ns/wsdl/http" whttp:cookies="sometimes"/>`+
			`</description>`,
	), wsdl.ParseOptions{})
	if err == nil {
		t.Fatal("Parse() error = nil, want invalid boolean error")
	}
}

func TestWSDL20OperationSafetyRoundTripsWithPresence(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"` +
		` targetNamespace="urn:test"><interface name="API">` +
		`<operation name="Safe" pattern="urn:none" wsdlx:safe="true"/>` +
		`<operation name="Unsafe" pattern="urn:none" wsdlx:safe="false"/>` +
		`<operation name="Unspecified" pattern="urn:none"/>` +
		`<operation name="SchemaForm" pattern="urn:none" safe="true"/>` +
		`</interface></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertSafety := func(document *wsdl.Document) {
		description, _ := document.Description20()
		operations := description.Interfaces[0].Operations
		if !operations[0].Safe || !operations[0].SafeSet ||
			operations[1].Safe || !operations[1].SafeSet || operations[2].SafeSet ||
			!operations[3].Safe || !operations[3].SafeSet ||
			len(operations[0].ExtensionAttributes) != 0 {
			t.Fatalf("operations = %#v", operations)
		}
	}
	assertSafety(document)
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertSafety(roundTrip)

	_, err = wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"`+
			` targetNamespace="urn:test"><interface name="API"><operation`+
			` name="Call" pattern="urn:none" wsdlx:safe="maybe"/>`+
			`</interface></description>`,
	), wsdl.ParseOptions{})
	if err == nil {
		t.Fatal("Parse(invalid safety) error = nil")
	}
	_, err = wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"`+
			` targetNamespace="urn:test"><interface name="API"><operation`+
			` name="Call" pattern="urn:none" safe="true" wsdlx:safe="true"/>`+
			`</interface></description>`,
	), wsdl.ParseOptions{})
	if err == nil {
		t.Fatal("Parse(duplicate safety forms) error = nil")
	}
}

func TestValidateWSDL20HTTPBindingProperties(t *testing.T) {
	t.Parallel()

	document, err := wsdl.Parse(context.Background(), []byte(
		`<description xmlns="http://www.w3.org/ns/wsdl"`+
			` xmlns:tns="urn:test" xmlns:whttp="http://www.w3.org/ns/wsdl/http"`+
			` targetNamespace="urn:test"><interface name="API">`+
			`<fault name="Failure" element="tns:Failure"/>`+
			`</interface><binding name="HTTP" interface="tns:API"`+
			` type="http://www.w3.org/ns/wsdl/http"`+
			` whttp:version="invalid"`+
			` whttp:queryParameterSeparatorDefault="xx">`+
			`<fault ref="tns:Failure" whttp:code="99">`+
			`<whttp:header name="" type="tns:Header"/>`+
			`</fault></binding><service name="Service" interface="tns:API">`+
			`<endpoint name="Endpoint" binding="tns:HTTP"`+
			` whttp:authenticationScheme="bearer"/>`+
			`</service></description>`,
	), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := map[string]bool{
		"WSDL20_HTTP_VERSION":               false,
		"WSDL20_HTTP_QUERY_SEPARATOR":       false,
		"WSDL20_HTTP_STATUS_CODE":           false,
		"WSDL20_HTTP_HEADER_NAME":           false,
		"WSDL20_HTTP_AUTHENTICATION_SCHEME": false,
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
