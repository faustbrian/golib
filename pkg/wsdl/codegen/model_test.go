package codegen_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wsdl/codegen"
	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
)

func TestBuildCreatesOwnedDeterministicGenerationModel(t *testing.T) {
	t.Parallel()

	set := compileSet(t)
	model, err := codegen.Build(set, codegen.Options{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(model.Interfaces) != 2 || model.Interfaces[0].Name.Local != "API" ||
		len(model.Interfaces[0].Operations) != 2 {
		t.Fatalf("model = %#v", model)
	}
	if len(model.Bindings[0].OperationReferences) != 2 {
		t.Fatalf("binding operation references = %#v", model.Bindings[0])
	}
	operation := model.Interfaces[0].Operations[0]
	if operation.Input == nil || operation.Input.Parts[0].Name != "value" {
		t.Fatalf("operation input = %#v", operation.Input)
	}
	model.Interfaces[0].Operations[0].Input.Parts[0].Name = "changed"
	again, err := codegen.Build(set, codegen.Options{})
	if err != nil || again.Interfaces[0].Operations[0].Input.Parts[0].Name != "value" {
		t.Fatalf("second Build() = (%#v, %v)", again, err)
	}
}

func TestBuildEnforcesGenerationModelLimits(t *testing.T) {
	t.Parallel()

	set := compileSet(t)
	limits := []codegen.Limits{
		{MaxInterfaces: 1}, {MaxOperations: 1}, {MaxParts: 1},
		{MaxFaults: 1}, {MaxBindings: 1}, {MaxServices: 1},
		{MaxEndpoints: 1}, {MaxTypes: 1}, {MaxElements: 1},
	}
	for _, limit := range limits {
		_, err := codegen.Build(set, codegen.Options{Limits: limit})
		if !errors.Is(err, codegen.ErrLimitExceeded) {
			t.Errorf("Build(%#v) error = %v, want ErrLimitExceeded", limit, err)
		}
	}
	if _, err := codegen.Build(nil, codegen.Options{}); err == nil {
		t.Fatal("Build(nil) error = nil")
	}
	negativeLimits := []codegen.Limits{
		{MaxInterfaces: -1}, {MaxOperations: -1}, {MaxParts: -1},
		{MaxFaults: -1}, {MaxBindings: -1}, {MaxServices: -1},
		{MaxEndpoints: -1}, {MaxTypes: -1}, {MaxElements: -1},
	}
	for _, limit := range negativeLimits {
		if _, err := codegen.Build(set, codegen.Options{Limits: limit}); err == nil {
			t.Errorf("Build(%#v) negative limit error = nil", limit)
		}
	}
}

func TestBuildPreservesMultipleWSDL20Messages(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"` +
			` targetNamespace="urn:test"><interface name="API">` +
			`<operation name="Exchange" pattern="urn:multi" wsdlx:safe="true">` +
			`<input messageLabel="First" element="#none"/>` +
			`<input messageLabel="Second" element="#any"/>` +
			`<output messageLabel="Third" element="#other"/>` +
			`</operation></interface></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	model, err := codegen.Build(set, codegen.Options{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	operation := model.Interfaces[0].Operations[0]
	if !operation.Safe || len(operation.Inputs) != 2 || len(operation.Outputs) != 1 ||
		operation.Inputs[1].Label != "Second" || operation.Input == nil ||
		operation.Input.Label != "First" {
		t.Fatalf("operation = %#v", operation)
	}
}

func TestBuildPreservesWSDL20RPCSignature(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc"` +
			` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
			`<types><xs:schema targetNamespace="urn:test" elementFormDefault="qualified">` +
			`<xs:element name="Call"><xs:complexType><xs:sequence><xs:element` +
			` name="value" type="xs:string"/></xs:sequence></xs:complexType></xs:element>` +
			`</xs:schema></types><interface name="API"><operation name="Call"` +
			` pattern="http://www.w3.org/ns/wsdl/in-only"` +
			` style="http://www.w3.org/ns/wsdl/style/rpc"` +
			` wrpc:signature="tns:value #in"><input element="tns:Call"/>` +
			`</operation></interface></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	model, err := codegen.Build(set, codegen.Options{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	operation := model.Interfaces[0].Operations[0]
	if !operation.RPCSignatureSet || len(operation.RPCSignature) != 1 ||
		operation.RPCSignature[0].Direction != "#in" {
		t.Fatalf("operation = %#v", operation)
	}
}

func compileSet(t *testing.T) *wsdlcompile.Set {
	t.Helper()
	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test"><types><xs:schema targetNamespace="urn:test">` +
			`<xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType>` +
			`<xs:complexType name="Record"><xs:sequence/></xs:complexType>` +
			`<xs:element name="One" type="xs:string"/><xs:element name="Two" type="xs:string"/>` +
			`</xs:schema></types><message name="Request">` +
			`<part name="value" type="xs:string"/><part name="count" type="xs:int"/>` +
			`</message><message name="Response"><part name="result" type="xs:string"/>` +
			`</message><message name="Failure"><part name="error" type="xs:string"/>` +
			`</message><portType name="API"><operation name="Call">` +
			`<input message="tns:Request"/><output message="tns:Response"/>` +
			`<fault name="First" message="tns:Failure"/><fault name="Second" message="tns:Failure"/>` +
			`</operation><operation name="Ping"><input message="tns:Request"/></operation>` +
			`</portType><portType name="Zed"/>` +
			`<binding name="A" type="tns:API"><operation name="Call"/>` +
			`<operation name="Ping"/></binding><binding name="B" type="tns:API">` +
			`<operation name="Call"/><operation name="Ping"/></binding>` +
			`<service name="A"><port name="One" binding="tns:A"/></service>` +
			`<service name="B"><port name="Two" binding="tns:B"/></service>` +
			`</definitions>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return set
}
