package wsdl_test

import (
	"context"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestValidateWSDL20ReportsBrokenComponentReferences(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:broken" targetNamespace="urn:broken">` +
		`<interface name="Broken"><operation name="Call"` +
		` pattern="relative-pattern"><input element="tns:Request"/>` +
		`<outfault ref="tns:MissingFault"/></operation></interface>` +
		`<binding name="BrokenBinding" interface="tns:MissingInterface"` +
		` type="http://www.w3.org/ns/wsdl/soap">` +
		`<operation ref="tns:MissingOperation"/></binding>` +
		`<service name="BrokenService" interface="tns:MissingInterface">` +
		`<endpoint name="BrokenEndpoint" binding="tns:MissingBinding"/>` +
		`</service></description>`

	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	want := []string{
		"WSDL20_PATTERN_IRI",
		"WSDL20_FAULT_REFERENCE",
		"WSDL20_INTERFACE_REFERENCE",
		"WSDL20_SOAP_PROTOCOL_REQUIRED",
		"WSDL20_OPERATION_REFERENCE",
		"WSDL20_INTERFACE_REFERENCE",
		"WSDL20_BINDING_REFERENCE",
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("Validate() = %#v, want codes %v", diagnostics, want)
	}
	for index, code := range want {
		if diagnostics[index].Code != code {
			t.Fatalf("diagnostic[%d] = %#v, want code %s", index, diagnostics[index], code)
		}
	}
}

func TestValidateRequiresExplicitExtensionUnderstanding(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:wsdl="http://www.w3.org/ns/wsdl"` +
		` xmlns:ext="urn:extension" targetNamespace="urn:test">` +
		`<interface name="API"><ext:policy wsdl:required="true"/>` +
		`</interface><ext:optional wsdl:required="false"/></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	if len(diagnostics) != 1 || diagnostics[0].Code != "WSDL_EXTENSION_REQUIRED" {
		t.Fatalf("Validate() = %#v", diagnostics)
	}
	diagnostics = wsdl.Validate(document, wsdl.ValidationOptions{
		UnderstoodExtensions: []wsdl.QName{{
			Namespace: "urn:extension", Local: "policy",
		}},
	})
	if diagnostics.HasErrors() {
		t.Fatalf("Validate(understood) = %#v", diagnostics)
	}
}

func TestValidateTraversesRequiredExtensionsAcrossWSDL20Adjuncts(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:wsdl="http://www.w3.org/ns/wsdl" xmlns:tns="urn:test"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
		` xmlns:whttp="http://www.w3.org/ns/wsdl/http" xmlns:ext="urn:ext"` +
		` targetNamespace="urn:test"><interface name="API"><fault name="Failure"` +
		` element="#none"><ext:p wsdl:required="true"/></fault><operation name="Call"` +
		` pattern="urn:custom"><ext:p wsdl:required="true"/>` +
		`<input messageLabel="In" element="#none"><ext:p wsdl:required="true"/></input>` +
		`<output messageLabel="Out" element="#none"><ext:p wsdl:required="true"/></output>` +
		`<infault ref="tns:Failure" messageLabel="InFault"><ext:p wsdl:required="true"/></infault>` +
		`<outfault ref="tns:Failure" messageLabel="OutFault"><ext:p wsdl:required="true"/></outfault>` +
		`</operation></interface><binding name="SOAP" interface="tns:API"` +
		` type="http://www.w3.org/ns/wsdl/soap" wsoap:protocol="urn:protocol">` +
		`<wsoap:module ref="urn:binding"><ext:p wsdl:required="true"/></wsoap:module>` +
		`<fault ref="tns:Failure"><wsoap:module ref="urn:fault"><ext:p wsdl:required="true"/>` +
		`</wsoap:module><wsoap:header element="tns:Header"><ext:p wsdl:required="true"/>` +
		`</wsoap:header></fault><operation ref="tns:Call">` +
		`<wsoap:module ref="urn:operation"><ext:p wsdl:required="true"/></wsoap:module>` +
		`<input messageLabel="In"><wsoap:module ref="urn:input"><ext:p wsdl:required="true"/>` +
		`</wsoap:module><wsoap:header element="tns:Header"><ext:p wsdl:required="true"/>` +
		`</wsoap:header></input><output messageLabel="Out"><wsoap:module ref="urn:output">` +
		`<ext:p wsdl:required="true"/></wsoap:module><wsoap:header element="tns:Header">` +
		`<ext:p wsdl:required="true"/></wsoap:header></output>` +
		`<infault ref="tns:Failure" messageLabel="InFault"><wsoap:module ref="urn:in-fault">` +
		`<ext:p wsdl:required="true"/></wsoap:module></infault>` +
		`<outfault ref="tns:Failure" messageLabel="OutFault"><wsoap:module ref="urn:out-fault">` +
		`<ext:p wsdl:required="true"/></wsoap:module></outfault></operation></binding>` +
		`<binding name="HTTP" interface="tns:API" type="http://www.w3.org/ns/wsdl/http">` +
		`<fault ref="tns:Failure"><whttp:header name="X-Fault" type="xs:string">` +
		`<ext:p wsdl:required="true"/></whttp:header></fault><operation ref="tns:Call">` +
		`<input messageLabel="In"><whttp:header name="X-In" type="xs:string">` +
		`<ext:p wsdl:required="true"/></whttp:header></input><output messageLabel="Out">` +
		`<whttp:header name="X-Out" type="xs:string"><ext:p wsdl:required="true"/>` +
		`</whttp:header></output></operation></binding><service name="Service" interface="tns:API">` +
		`<endpoint name="Endpoint" binding="tns:SOAP" address="https://example.test">` +
		`<ext:p wsdl:required="true"/></endpoint></service></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	if len(diagnostics) < 15 {
		t.Fatalf("Validate() = %#v", diagnostics)
	}
	diagnostics = wsdl.Validate(document, wsdl.ValidationOptions{
		UnderstoodExtensions: []wsdl.QName{{Namespace: "urn:ext", Local: "p"}},
	})
	if diagnostics.HasErrors() {
		t.Fatalf("Validate(understood) = %#v", diagnostics)
	}
}

func TestValidateWSDL20PredefinedMessageExchangePatterns(t *testing.T) {
	t.Parallel()

	description := wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces: []wsdl.Interface20{{
			Name: "Port",
			Faults: []wsdl.InterfaceFault20{{
				Name: "Failure", MessageContentModel: wsdl.MessageContentNone,
				MessageContentModelSet: true,
			}},
			Operations: []wsdl.InterfaceOperation20{
				{
					Name: "MissingOutput", Pattern: wsdl.MEPInOut,
					Input: &wsdl.InterfaceMessageReference20{MessageLabel: "In"},
				},
				{
					Name: "WrongLabel", Pattern: wsdl.MEPInOnly,
					Input: &wsdl.InterfaceMessageReference20{MessageLabel: "Out"},
				},
				{
					Name: "ForbiddenFault", Pattern: wsdl.MEPInOnly,
					Input: &wsdl.InterfaceMessageReference20{},
					OutFaults: []wsdl.InterfaceFaultReference20{{
						Ref: wsdl.QName{Namespace: "urn:test", Local: "Failure"},
					}},
				},
			},
		}},
	}
	document, err := wsdl.NewDocument20(description, wsdl.ValidationOptions{})
	if err == nil || document != nil {
		t.Fatalf("NewDocument20() = (%v, %v), want validation error", document, err)
	}
	for _, code := range []string{
		"WSDL20_MEP_OUTPUT", "WSDL20_MESSAGE_LABEL", "WSDL20_MEP_OUTFAULT",
	} {
		if !strings.Contains(err.Error(), code) {
			t.Errorf("validation error %q does not contain %s", err, code)
		}
	}
}

func TestValidateWSDL20CustomPatternMessageLabels(t *testing.T) {
	t.Parallel()

	description := wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces: []wsdl.Interface20{{
			Name: "Port",
			Operations: []wsdl.InterfaceOperation20{{
				Name: "Exchange", Pattern: "urn:multi",
				Inputs: []wsdl.InterfaceMessageReference20{
					{MessageLabel: "Repeated"},
					{MessageLabel: "Repeated"},
				},
				Outputs: []wsdl.InterfaceMessageReference20{{}, {}},
			}},
		}},
	}
	document, err := wsdl.NewDocument20(description, wsdl.ValidationOptions{})
	if err == nil || document != nil {
		t.Fatalf("NewDocument20() = (%v, %v), want validation error", document, err)
	}
	if strings.Count(err.Error(), "WSDL20_MESSAGE_LABEL") != 3 {
		t.Fatalf("validation error = %q", err)
	}
}

func TestValidateWSDL20InterfaceGraphStylesAndEndpointAddress(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:test" targetNamespace="urn:test">` +
		`<interface name="A" extends="tns:Missing" styleDefault="relative"/>` +
		`<interface name="B" extends="tns:C"/><interface name="C" extends="tns:B"/>` +
		`<interface name="API"><operation name="Call" pattern="http://www.w3.org/ns/wsdl/out-only"` +
		` style="relative http://www.w3.org/ns/wsdl/style/rpc"><input element="#none"/>` +
		`</operation></interface><binding name="Binding" interface="tns:API" type="urn:test"/>` +
		`<service name="Service" interface="tns:API"><endpoint name="Endpoint"` +
		` binding="tns:Binding" address="relative"/></service></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	for _, code := range []string{
		"WSDL20_INTERFACE_EXTENDS", "WSDL20_INTERFACE_CYCLE", "WSDL20_STYLE_IRI",
		"WSDL20_RPC_STYLE_PATTERN", "WSDL20_ENDPOINT_ADDRESS",
	} {
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

func TestValidateWSDL20IRIAndMultipartStylesRequireElementInitialMessage(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl" xmlns:tns="urn:test"` +
		` targetNamespace="urn:test">` +
		`<interface name="API"><operation name="IRI"` +
		` pattern="http://www.w3.org/ns/wsdl/in-only"` +
		` style="http://www.w3.org/ns/wsdl/style/iri"><input element="#none"/>` +
		`</operation><operation name="Multipart"` +
		` pattern="http://www.w3.org/ns/wsdl/out-only"` +
		` style="http://www.w3.org/ns/wsdl/style/multipart"><output element="#any"/>` +
		`</operation><operation name="Wrong" pattern="http://www.w3.org/ns/wsdl/in-only"` +
		` style="http://www.w3.org/ns/wsdl/style/iri"><input element="tns:Other"/>` +
		`</operation></interface></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	for _, code := range []string{
		"WSDL20_IRI_MESSAGE_CONTENT", "WSDL20_MULTIPART_MESSAGE_CONTENT",
		"WSDL20_IRI_INITIAL_NAME",
	} {
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

func TestValidateWSDL20AdjunctEdgeCases(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl" xmlns:tns="urn:test"` +
		` xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
		` xmlns:whttp="http://www.w3.org/ns/wsdl/http"` +
		` xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc" targetNamespace="urn:test">` +
		`<interface name="API"><fault name="Failure" element="#none"/>` +
		`<operation name="Only" pattern="http://www.w3.org/ns/wsdl/in-only">` +
		`<input element="#none"/><input element="#none"/><output element="#none"/>` +
		`<infault ref="tns:Failure"/></operation><operation name="Custom" pattern="urn:custom"` +
		` wrpc:signature="tns:value #in"><input messageLabel="In" element="#none"/>` +
		`<output messageLabel="Out" element="#none"/><infault ref="tns:Failure"` +
		` messageLabel="InFault"/><outfault ref="tns:Failure" messageLabel="OutFault"/>` +
		`</operation></interface><binding name="HTTP" interface="tns:API"` +
		` type="http://www.w3.org/ns/wsdl/http" whttp:version="1"` +
		` whttp:queryParameterSeparatorDefault="ab"><fault ref="tns:Failure"` +
		` whttp:code="99"><whttp:header name="Bad Name"/></fault>` +
		`<operation ref="tns:Custom" whttp:method="BAD METHOD"` +
		` whttp:queryParameterSeparator=""><input messageLabel="In">` +
		`<whttp:header name=""/></input><output messageLabel="Out">` +
		`<whttp:header name="Bad Name"/></output></operation></binding>` +
		`<binding name="SOAP" interface="tns:API" type="http://www.w3.org/ns/wsdl/soap"` +
		` wsoap:protocol="urn:protocol" wsoap:mepDefault="relative">` +
		`<wsoap:module ref="relative"/><fault ref="tns:Failure">` +
		`<wsoap:module ref="relative"/><wsoap:header/></fault>` +
		`<operation ref="tns:Custom" wsoap:mep="relative" wsoap:action="relative">` +
		`<wsoap:module ref="relative"/><input messageLabel="In">` +
		`<wsoap:module ref="relative"/><wsoap:header/></input>` +
		`<output messageLabel="Out"><wsoap:module ref="relative"/><wsoap:header/>` +
		`</output><infault ref="tns:Failure" messageLabel="InFault">` +
		`<wsoap:module ref="relative"/></infault><outfault ref="tns:Failure"` +
		` messageLabel="OutFault"><wsoap:module ref="relative"/></outfault>` +
		`</operation></binding><service name="Service" interface="tns:API">` +
		`<endpoint name="BadScheme" binding="tns:HTTP" address="https://example.test"` +
		` whttp:authenticationScheme="bearer"/><endpoint name="RealmOnly"` +
		` binding="tns:HTTP" address="https://example.test"` +
		` whttp:authenticationRealm="api"/></service></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	diagnostics := wsdl.Validate(document, wsdl.ValidationOptions{})
	want := []string{
		"WSDL20_MEP_INPUT", "WSDL20_MEP_OUTPUT", "WSDL20_MEP_INFAULT",
		"WSDL20_RPC_SIGNATURE_STYLE", "WSDL20_HTTP_VERSION",
		"WSDL20_HTTP_QUERY_SEPARATOR", "WSDL20_HTTP_STATUS_CODE",
		"WSDL20_HTTP_METHOD", "WSDL20_HTTP_HEADER_NAME", "WSDL20_HTTP_HEADER_TYPE",
		"WSDL20_HTTP_AUTHENTICATION_SCHEME", "WSDL20_HTTP_AUTHENTICATION_REALM",
		"WSDL20_SOAP_MEP_IRI", "WSDL20_SOAP_MODULE_IRI",
		"WSDL20_SOAP_ACTION_IRI", "WSDL20_SOAP_HEADER_ELEMENT",
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
