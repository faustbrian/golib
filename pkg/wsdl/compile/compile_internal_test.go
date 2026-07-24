package compile

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	"github.com/faustbrian/golib/pkg/wsdl/resolve"
	xsd "github.com/faustbrian/golib/pkg/xsd"
	xsdcompile "github.com/faustbrian/golib/pkg/xsd/compile"
	xsdresolve "github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestCompilerRejectsEveryNegativeSchemaLimit(t *testing.T) {
	t.Parallel()

	tests := map[string]xsdcompile.Limits{
		"schemas":    {MaxSchemas: -1},
		"depth":      {MaxDepth: -1},
		"references": {MaxReferences: -1},
		"bytes":      {MaxBytes: -1},
		"components": {MaxComponents: -1},
		"particles":  {MaxParticles: -1},
	}
	for name, limits := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(Options{SchemaLimits: limits}); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}

func TestResourceIdentityRequiresAbsoluteFragmentlessURI(t *testing.T) {
	t.Parallel()

	for _, identity := range []string{"relative.wsdl", "https://example.test/a.wsdl#part", "://"} {
		if err := validateIdentity(identity); !errors.Is(err, ErrResourceIdentity) {
			t.Errorf("validateIdentity(%q) error = %v", identity, err)
		}
	}
	if err := validateIdentity("https://example.test/a.wsdl"); err != nil {
		t.Fatalf("validateIdentity(valid) error = %v", err)
	}
}

func TestInterfaceInheritanceRejectsBrokenGraphs(t *testing.T) {
	t.Parallel()

	ns := "urn:test"
	parent := wsdl.QName{Namespace: ns, Local: "Parent"}
	tests := map[string][]Interface{
		"missing parent": {{Name: wsdl.QName{Namespace: ns, Local: "Child"}, Extends: []wsdl.QName{parent}}},
		"cycle": {
			{Name: parent, Extends: []wsdl.QName{{Namespace: ns, Local: "Child"}}},
			{Name: wsdl.QName{Namespace: ns, Local: "Child"}, Extends: []wsdl.QName{parent}},
		},
		"conflicting operation": {
			{Name: parent, Operations: []Operation{{Name: "Call", Pattern: "urn:one"}}},
			{Name: wsdl.QName{Namespace: ns, Local: "Child"}, Extends: []wsdl.QName{parent},
				Operations: []Operation{{Name: "Call", Pattern: "urn:two"}}},
		},
	}
	for name, interfaces := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := expandInterfaceInheritance(interfaces); err == nil {
				t.Fatal("expandInterfaceInheritance() error = nil")
			}
		})
	}
}

func TestInterfaceInheritanceDeduplicatesIdenticalMembers(t *testing.T) {
	t.Parallel()

	ns := "urn:test"
	fault := wsdl.QName{Namespace: ns, Local: "Failure"}
	operation := Operation{Name: "Call", Pattern: "urn:pattern"}
	interfaces := []Interface{
		{Name: wsdl.QName{Namespace: ns, Local: "Parent"}, Operations: []Operation{operation}, Faults: []wsdl.QName{fault}},
		{Name: wsdl.QName{Namespace: ns, Local: "Child"},
			Extends:    []wsdl.QName{{Namespace: ns, Local: "Parent"}},
			Operations: []Operation{operation}, Faults: []wsdl.QName{fault}},
	}
	added, err := expandInterfaceInheritance(interfaces)
	if err != nil {
		t.Fatalf("expandInterfaceInheritance() error = %v", err)
	}
	if added != 0 || len(interfaces[1].Operations) != 1 || len(interfaces[1].Faults) != 1 {
		t.Fatalf("expanded child = %#v, added = %d", interfaces[1], added)
	}

	inherited := []Interface{
		{Name: wsdl.QName{Namespace: ns, Local: "Parent"}, Operations: []Operation{operation}, Faults: []wsdl.QName{fault}},
		{Name: wsdl.QName{Namespace: ns, Local: "Child"}, Extends: []wsdl.QName{{Namespace: ns, Local: "Parent"}}},
	}
	added, err = expandInterfaceInheritance(inherited)
	if err != nil {
		t.Fatalf("expandInterfaceInheritance(unique) error = %v", err)
	}
	if added != 2 || len(inherited[1].Operations) != 1 || len(inherited[1].Faults) != 1 {
		t.Fatalf("unique inherited child = %#v, added = %d", inherited[1], added)
	}
}

func TestGraphValidationRejectsEveryDanglingReference(t *testing.T) {
	t.Parallel()

	ns := "urn:test"
	interfaceName := wsdl.QName{Namespace: ns, Local: "API"}
	bindingName := wsdl.QName{Namespace: ns, Local: "Binding"}
	interfaces := map[wsdl.QName]struct{}{interfaceName: {}}
	bindings := map[wsdl.QName]struct{}{bindingName: {}}
	tests := map[string]*Set{
		"binding interface": {bindings: []Binding{{Interface: wsdl.QName{Namespace: ns, Local: "Missing"}}}},
		"binding operation": {
			interfaces: []Interface{{Name: interfaceName, Operations: []Operation{{Name: "Known"}}}},
			bindings:   []Binding{{Interface: interfaceName, OperationReferences: []OperationReference{{Name: "Missing"}}}},
		},
		"service interface": {services: []Service{{Interface: wsdl.QName{Namespace: ns, Local: "Missing"}}}},
		"endpoint binding":  {services: []Service{{Endpoints: []Endpoint{{Binding: wsdl.QName{Namespace: ns, Local: "Missing"}}}}}},
	}
	for name, set := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validateGraph(set, interfaces, bindings); !errors.Is(err, ErrUnresolvedComponent) {
				t.Fatalf("validateGraph() error = %v", err)
			}
		})
	}
}

func TestOperationMessageDefaultsCoverEveryWSDL11Style(t *testing.T) {
	t.Parallel()

	tests := map[wsdl.OperationStyle11][2]string{
		wsdl.OperationStyleOneWay:          {"Call", ""},
		wsdl.OperationStyleRequestResponse: {"CallRequest", "CallResponse"},
		wsdl.OperationStyleSolicitResponse: {"CallResponse", "CallSolicit"},
		wsdl.OperationStyleNotification:    {"", "Call"},
	}
	for style, expected := range tests {
		operation := wsdl.Operation11{
			Name: "Call", Style: style,
			Input: &wsdl.OperationMessage11{}, Output: &wsdl.OperationMessage11{},
		}
		input, output := operationMessageNames11(operation)
		if input != expected[0] || output != expected[1] {
			t.Errorf("operationMessageNames11(%s) = (%q, %q)", style, input, output)
		}
	}
}

func TestRPCMessageShapeRequiresOneCompiledWrapper(t *testing.T) {
	t.Parallel()

	empty, err := rpcMessageShape("Call", "input", nil, nil)
	if err != nil || len(empty.elements) != 0 {
		t.Fatalf("rpcMessageShape(empty) = (%#v, %v)", empty, err)
	}
	messages := []wsdl.InterfaceMessageReference20{{Element: wsdl.QName{Namespace: "urn:test", Local: "Call"}}}
	if _, err := rpcMessageShape("Call", "input", messages, nil); !errors.Is(err, ErrInvalidRPCStyle) {
		t.Fatalf("rpcMessageShape(uncompiled) error = %v", err)
	}
	if _, err := rpcMessageShape("Call", "input", append(messages, messages[0]), nil); !errors.Is(err, ErrInvalidRPCStyle) {
		t.Fatalf("rpcMessageShape(multiple) error = %v", err)
	}
}

func TestComponentNamesAreUniquePerKind(t *testing.T) {
	t.Parallel()

	name := wsdl.QName{Namespace: "urn:test", Local: "API"}
	names := make(map[wsdl.QName]struct{})
	if err := addName(names, "interface", name); err != nil {
		t.Fatalf("addName(first) error = %v", err)
	}
	if err := addName(names, "interface", name); !errors.Is(err, ErrDuplicateComponent) {
		t.Fatalf("addName(duplicate) error = %v", err)
	}
}

func TestWSDL11CompilationPreservesSolicitResponseFaultDirection(t *testing.T) {
	t.Parallel()

	operation := wsdl.Operation11{
		Name: "Notify", Style: wsdl.OperationStyleSolicitResponse,
		Input:  &wsdl.OperationMessage11{Message: wsdl.QName{Namespace: "urn:test", Local: "Response"}},
		Output: &wsdl.OperationMessage11{Message: wsdl.QName{Namespace: "urn:test", Local: "Request"}},
		Faults: []wsdl.OperationMessage11{{Name: "Failure", Message: wsdl.QName{Namespace: "urn:test", Local: "Fault"}}},
	}
	compiled := compileOperation11(operation, map[wsdl.QName]Message{
		operation.Input.Message:  {Name: operation.Input.Message},
		operation.Output.Message: {Name: operation.Output.Message},
	})
	if len(compiled.Inputs) != 1 || len(compiled.Outputs) != 1 ||
		len(compiled.Faults) != 1 || compiled.Faults[0].Direction != "in" {
		t.Fatalf("compileOperation11() = %#v", compiled)
	}
}

func TestBindingOperationResolutionUsesPortTypeAndMessageNames(t *testing.T) {
	t.Parallel()

	document := mustParseDocument(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` xmlns:tns="urn:test" targetNamespace="urn:test"><message name="Message"/>`+
		`<portType name="Port"><operation name="Call"><input name="First"`+
		` message="tns:Message"/><output message="tns:Message"/></operation>`+
		`<operation name="Call"><input name="Second" message="tns:Message"/>`+
		`<output message="tns:Message"/></operation></portType></definitions>`)
	resources := map[string]*resourceDocument{
		"urn:test": {document: document},
		"urn:other": {document: mustParseDocument(t,
			`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
				` targetNamespace="urn:other"><portType name="Port">`+
				`<operation name="Call"/></portType></definitions>`)},
		"urn:wrong-port": {document: mustParseDocument(t,
			`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
				` targetNamespace="urn:test"><portType name="Other">`+
				`<operation name="Call"/></portType></definitions>`)},
		"urn:wrong-operation": {document: mustParseDocument(t,
			`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
				` targetNamespace="urn:test"><portType name="Port">`+
				`<operation name="Other"/></portType></definitions>`)},
		"urn:wsdl20": {document: mustParseDocument(t,
			`<description xmlns="http://www.w3.org/ns/wsdl" targetNamespace="urn:test"/>`)},
	}
	port := wsdl.QName{Namespace: "urn:test", Local: "Port"}
	resolved := compileBindingOperationReference11(wsdl.BindingOperation11{
		Name: "Call", Input: &wsdl.BindingMessage11{Name: "Second"},
	}, port, resources)
	if resolved.Input != "Second" || resolved.Output != "CallResponse" {
		t.Fatalf("compileBindingOperationReference11() = %#v", resolved)
	}
	fallback := compileBindingOperationReference11(wsdl.BindingOperation11{
		Name: "Missing", Output: &wsdl.BindingMessage11{Name: "Result"},
	}, port, resources)
	if fallback.Name != "Missing" || fallback.Output != "Result" {
		t.Fatalf("compileBindingOperationReference11(fallback) = %#v", fallback)
	}
}

func TestLegacyWSDL20MessageReferenceRemainsCompilable(t *testing.T) {
	t.Parallel()

	legacy := &wsdl.InterfaceMessageReference20{
		Element: wsdl.QName{Namespace: "urn:test", Local: "Request"},
	}
	messages := interfaceMessages20(nil, legacy)
	if len(messages) != 1 || messages[0].Element != legacy.Element {
		t.Fatalf("interfaceMessages20() = %#v", messages)
	}
	if compileMessage20(nil, "In") != nil {
		t.Fatal("compileMessage20(nil) != nil")
	}
	if label := defaultMessageLabel20("In", 0); label != "In" {
		t.Fatalf("defaultMessageLabel20(first) = %q", label)
	}
	if label := defaultMessageLabel20("In", 1); label != "" {
		t.Fatalf("defaultMessageLabel20(later) = %q", label)
	}
}

func TestSchemaWrapperPreservesIncludesAndImports(t *testing.T) {
	t.Parallel()

	content, err := schemaWrapper(
		[]inlineSchemaSource{
			{uri: "urn:no-namespace"},
			{uri: "urn:typed", namespace: "urn:types&more"},
		},
		[]xsd.SchemaReference{
			{Namespace: "urn:external&more", URI: "https://example.test/a&b.xsd"},
			{},
		},
	)
	if err != nil {
		t.Fatalf("schemaWrapper() error = %v", err)
	}
	for _, expected := range []string{
		`<xs:include schemaLocation="urn:no-namespace"/>`,
		`namespace="urn:types&amp;more" schemaLocation="urn:typed"`,
		`namespace="urn:external&amp;more"`,
		`schemaLocation="https://example.test/a&amp;b.xsd"`,
		`<xs:import/>`,
	} {
		if !strings.Contains(string(content), expected) {
			t.Errorf("schemaWrapper() = %s, missing %s", content, expected)
		}
	}
	if _, err := inlineSchemaURI("%", 0); !errors.Is(err, ErrResourceIdentity) {
		t.Fatalf("inlineSchemaURI(invalid) error = %v", err)
	}
	uri, err := inlineSchemaURI("https://example.test/service.wsdl?existing=yes", 0)
	if err != nil {
		t.Fatalf("inlineSchemaURI(valid) error = %v", err)
	}
	if uri != "https://example.test/service.wsdl?existing=yes&wsdl-inline-schema=1" {
		t.Fatalf("inlineSchemaURI(valid) = %q", uri)
	}
}

func TestSchemaReferenceHelpersRejectUnknownComponents(t *testing.T) {
	t.Parallel()

	missing := wsdl.QName{Namespace: "urn:test", Local: "Missing"}
	if err := validateBindingMessageSchema20(
		wsdl.BindingMessageReference20{SOAP: &wsdl.SOAPMessageBinding20{
			Headers: []wsdl.SOAPHeader20{{Element: missing}},
		}}, nil, nil,
	); !errors.Is(err, ErrUnresolvedComponent) {
		t.Fatalf("validateBindingMessageSchema20(SOAP) error = %v", err)
	}
	if err := validateBindingMessageSchema20(
		wsdl.BindingMessageReference20{HTTP: &wsdl.HTTPMessageBinding20{
			Headers: []wsdl.HTTPHeader20{{Type: missing}},
		}}, nil, nil,
	); !errors.Is(err, ErrUnresolvedComponent) {
		t.Fatalf("validateBindingMessageSchema20(HTTP) error = %v", err)
	}
	if err := validateHTTPHeaderTypes20(
		[]wsdl.HTTPHeader20{{Type: wsdl.QName{Namespace: wsdl.NamespaceXMLSchema, Local: "string"}}}, nil,
	); err != nil {
		t.Fatalf("validateHTTPHeaderTypes20(built-in) error = %v", err)
	}
	if err := validateBindingMessageSchema20(wsdl.BindingMessageReference20{}, nil, nil); err != nil {
		t.Fatalf("validateBindingMessageSchema20(empty) error = %v", err)
	}
}

func TestRPCComplexTypeRequiresNamedOrInlineType(t *testing.T) {
	t.Parallel()

	inline := xsd.ComplexType{}
	if got, ok := rpcComplexType(xsd.Element{InlineComplexType: &inline}, nil); !ok || got.Name != inline.Name {
		t.Fatalf("rpcComplexType(inline) = (%#v, %t)", got, ok)
	}
	if _, ok := rpcComplexType(xsd.Element{}, nil); ok {
		t.Fatal("rpcComplexType(unnamed) succeeded")
	}
}

func TestOperationStylesSelectTheMEPInitialMessage(t *testing.T) {
	t.Parallel()

	input := wsdl.InterfaceMessageReference20{MessageLabel: "In"}
	output := wsdl.InterfaceMessageReference20{MessageLabel: "Out"}
	tests := []struct {
		operation wsdl.InterfaceOperation20
		want      string
	}{
		{operation: wsdl.InterfaceOperation20{Pattern: wsdl.MEPInOnly, Inputs: []wsdl.InterfaceMessageReference20{input}}, want: "In"},
		{operation: wsdl.InterfaceOperation20{Pattern: wsdl.MEPOutOnly, Outputs: []wsdl.InterfaceMessageReference20{output}}, want: "Out"},
		{operation: wsdl.InterfaceOperation20{Pattern: "urn:custom", Outputs: []wsdl.InterfaceMessageReference20{output}}, want: "Out"},
	}
	for _, test := range tests {
		message := initialOperationMessage20(test.operation)
		if message == nil || message.MessageLabel != test.want {
			t.Errorf("initialOperationMessage20() = %#v, want %q", message, test.want)
		}
	}
	if message := initialOperationMessage20(wsdl.InterfaceOperation20{}); message != nil {
		t.Fatalf("initialOperationMessage20(empty) = %#v", message)
	}
	if message := initialOperationMessage20(wsdl.InterfaceOperation20{
		Pattern: wsdl.MEPOutOnly,
	}); message != nil {
		t.Fatalf("initialOperationMessage20(empty out-only) = %#v", message)
	}
}

func TestOperationStyleSchemaRequiresCompiledWrapper(t *testing.T) {
	t.Parallel()

	message := wsdl.InterfaceMessageReference20{
		Element: wsdl.QName{Namespace: "urn:test", Local: "Call"},
	}
	if err := validateOperationStyleSchema20(wsdl.StyleIRI, "Call", message, nil); !errors.Is(err, ErrInvalidIRIStyle) {
		t.Fatalf("validateOperationStyleSchema20(IRI) error = %v", err)
	}
	if err := validateOperationStyleSchema20(wsdl.StyleMultipart, "Call", message, nil); !errors.Is(err, ErrInvalidMultipartStyle) {
		t.Fatalf("validateOperationStyleSchema20(multipart) error = %v", err)
	}
}

func TestIRISimpleTypeRulesCoverInlineAndBuiltInTypes(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	qNameType := xsd.QName{Namespace: xsd.Namespace, Local: "QName"}
	if !iriSimpleElementAllowed(xsd.Element{Type: stringType}, nil) {
		t.Fatal("xs:string was rejected")
	}
	if iriSimpleElementAllowed(xsd.Element{Type: qNameType}, nil) {
		t.Fatal("xs:QName was accepted")
	}
	if iriSimpleElementAllowed(xsd.Element{Type: xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}}, nil) {
		t.Fatal("xs:anyType was accepted")
	}
	if iriSimpleElementAllowed(xsd.Element{}, nil) {
		t.Fatal("untyped element was accepted")
	}
	if !iriSimpleElementAllowed(xsd.Element{InlineSimpleType: &xsd.SimpleType{}}, nil) {
		t.Fatal("inline simple type was rejected")
	}
	if iriSimpleElementAllowed(xsd.Element{
		InlineSimpleType: &xsd.SimpleType{InlineBase: &xsd.SimpleType{Base: qNameType}},
	}, nil) {
		t.Fatal("inline QName-derived type was accepted")
	}
	if forbiddenIRIPrimitive(xsd.QName{Namespace: "urn:other", Local: "QName"}) {
		t.Fatal("foreign QName type was treated as XML Schema QName")
	}
	if iriSimpleTypeForbidden(xsd.SimpleType{Base: stringType}, &xsdcompile.Set{}, nil) {
		t.Fatal("xs:string-derived type was rejected")
	}
}

func TestComponentCountersIncludeEveryNestedWSDL20Component(t *testing.T) {
	t.Parallel()

	interfaceValue := wsdl.Interface20{
		Faults: []wsdl.InterfaceFault20{{Name: "Failure"}},
		Operations: []wsdl.InterfaceOperation20{{
			Name:      "Call",
			Inputs:    []wsdl.InterfaceMessageReference20{{MessageLabel: "In"}},
			Outputs:   []wsdl.InterfaceMessageReference20{{MessageLabel: "Out"}},
			InFaults:  []wsdl.InterfaceFaultReference20{{Ref: wsdl.QName{Local: "Failure"}}},
			OutFaults: []wsdl.InterfaceFaultReference20{{Ref: wsdl.QName{Local: "Failure"}}},
		}},
	}
	if count := countInterface20Components(interfaceValue); count != 7 {
		t.Fatalf("countInterface20Components() = %d, want 7", count)
	}
	bindingValue := wsdl.Binding20{
		Faults: []wsdl.BindingFault20{{Ref: wsdl.QName{Local: "Failure"}}},
		Operations: []wsdl.BindingOperation20{{
			Ref:       wsdl.QName{Local: "Call"},
			Inputs:    []wsdl.BindingMessageReference20{{MessageLabel: "In"}},
			Outputs:   []wsdl.BindingMessageReference20{{MessageLabel: "Out"}},
			InFaults:  []wsdl.BindingFaultReference20{{Ref: wsdl.QName{Local: "Failure"}}},
			OutFaults: []wsdl.BindingFaultReference20{{Ref: wsdl.QName{Local: "Failure"}}},
		}},
	}
	if count := countBinding20Components(bindingValue); count != 7 {
		t.Fatalf("countBinding20Components() = %d, want 7", count)
	}
}

func TestBuildSetCountsWSDL20ServiceAndEndpointComponents(t *testing.T) {
	t.Parallel()

	document := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` xmlns:tns="urn:test" targetNamespace="urn:test">`+
		`<interface name="API"/><binding name="Binding" interface="tns:API"`+
		` type="urn:binding"/><service name="Service" interface="tns:API">`+
		`<endpoint name="Endpoint" binding="tns:Binding"/></service></description>`)
	state := compileState{
		compiler: &Compiler{limits: Limits{MaxComponents: 3}},
		resources: map[string]*resourceDocument{
			"urn:test": {document: document},
		},
	}
	if _, err := state.buildSet(context.Background()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("buildSet() error = %v, want ErrLimitExceeded", err)
	}
}

func TestCompiledOrderingUsesFullStableIdentity(t *testing.T) {
	t.Parallel()

	operations := []Operation{
		{Name: "Z"},
		{Name: "Call", Input: &Message{Label: "B"}, Output: &Message{Label: "A"}},
		{Name: "Call", Input: &Message{Label: "A"}, Output: &Message{Label: "B"}},
		{Name: "Call", Input: &Message{Label: "A"}, Output: &Message{Label: "A"}},
	}
	sortOperations(operations)
	if operations[0].Output.Label != "A" || operations[1].Output.Label != "B" ||
		operations[2].Input.Label != "B" || operations[3].Name != "Z" {
		t.Fatalf("sortOperations() = %#v", operations)
	}
	references := []OperationReference{
		{Name: "Z"}, {Name: "A", Input: "B"},
		{Name: "A", Input: "A", Output: "B"}, {Name: "A", Input: "A", Output: "A"},
	}
	sortOperationReferences(references)
	if references[0].Output != "A" || references[1].Output != "B" ||
		references[2].Input != "B" || references[3].Name != "Z" {
		t.Fatalf("sortOperationReferences() = %#v", references)
	}
	endpoints := []Endpoint{{Name: "Z"}, {Name: "A"}}
	sortEndpoints(endpoints)
	bindings := []Binding{{Name: wsdl.QName{Namespace: "z"}}, {Name: wsdl.QName{Namespace: "a"}}}
	sortBindings(bindings)
	services := []Service{{Name: wsdl.QName{Namespace: "z"}}, {Name: wsdl.QName{Namespace: "a"}}}
	sortServices(services)
	if endpoints[0].Name != "A" || bindings[0].Name.Namespace != "a" || services[0].Name.Namespace != "a" {
		t.Fatalf("sorted values = %#v, %#v, %#v", endpoints, bindings, services)
	}
}

func TestBuildSetAppliesComponentLimitAtEveryCollectionBoundary(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"WSDL 1.1 message": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
			`<message name="Message"><part name="value" type="xs:string"/></message></definitions>`,
		"WSDL 1.1 port type": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><portType name="Port">` +
			`<operation name="Call"><input message="tns:Message"/></operation></portType></definitions>`,
		"WSDL 1.1 binding": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><binding name="Binding"` +
			` type="tns:Port"><operation name="Call"/></binding></definitions>`,
		"WSDL 1.1 service": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><service name="Service">` +
			`<port name="Port" binding="tns:Binding"/></service></definitions>`,
		"WSDL 2.0 interface": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><interface name="API"/></description>`,
		"WSDL 2.0 binding": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><binding name="Binding"` +
			` interface="tns:API" type="urn:binding"/></description>`,
		"WSDL 2.0 service": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><service name="Service"` +
			` interface="tns:API"/></description>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			state := compileState{
				compiler: &Compiler{limits: Limits{MaxComponents: 0}},
				resources: map[string]*resourceDocument{
					"urn:test": {document: mustParseDocument(t, source)},
				},
			}
			if _, err := state.buildSet(context.Background()); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("buildSet() error = %v", err)
			}
		})
	}
}

func TestBuildSetCompilesEveryWSDL11EndpointAddress(t *testing.T) {
	t.Parallel()

	document := mustParseDocument(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` xmlns:tns="urn:test" xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"`+
		` xmlns:http="http://schemas.xmlsoap.org/wsdl/http/" targetNamespace="urn:test">`+
		`<portType name="Port"/><binding name="Binding" type="tns:Port"/>`+
		`<service name="Service"><port name="SOAP" binding="tns:Binding">`+
		`<soap:address location="https://soap.test"/></port><port name="HTTP" binding="tns:Binding">`+
		`<http:address location="https://http.test"/></port></service></definitions>`)
	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	state := compileState{
		compiler:  compiler,
		resources: map[string]*resourceDocument{"urn:test": {document: document}},
	}
	set, err := state.buildSet(context.Background())
	if err != nil {
		t.Fatalf("buildSet() error = %v", err)
	}
	if len(set.services) != 1 || len(set.services[0].Endpoints) != 2 ||
		set.services[0].Endpoints[0].Address != "https://http.test" ||
		set.services[0].Endpoints[1].Address != "https://soap.test" {
		t.Fatalf("services = %#v", set.services)
	}
}

func TestCompileRejectsInvalidRootIdentity(t *testing.T) {
	t.Parallel()

	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := compiler.Compile(context.Background(), Source{URI: "relative.wsdl"}); !errors.Is(err, ErrResourceIdentity) {
		t.Fatalf("Compile() error = %v", err)
	}
}

func TestResolveDocumentPropagatesContextAndExistingReferenceFailure(t *testing.T) {
	t.Parallel()

	root := mustParseDocumentWithSystemID(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` targetNamespace="urn:root"><import namespace="urn:child" location="child.wsdl"/>`+
		`</definitions>`, "https://example.test/root.wsdl")
	child20 := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` targetNamespace="urn:child"/>`)
	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	state := compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"https://example.test/root.wsdl":  {document: root},
		"https://example.test/child.wsdl": {document: child20},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := state.resolveDocument(ctx, "https://example.test/root.wsdl", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("resolveDocument(canceled) error = %v", err)
	}
	if err := state.resolveDocument(context.Background(), "https://example.test/root.wsdl", 1); !errors.Is(err, ErrVersion) {
		t.Fatalf("resolveDocument(existing) error = %v", err)
	}
}

func TestCompileRejectsInvalidResolvedIdentityBytesAndContent(t *testing.T) {
	t.Parallel()

	root := func(location string) []byte {
		return []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` targetNamespace="urn:root"><import namespace="urn:child" location="` +
			location + `"/></definitions>`)
	}
	tests := map[string]struct {
		location string
		content  []byte
		limit    int64
		want     error
	}{
		"identity": {
			location: "child.wsdl#fragment",
			content:  []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/" targetNamespace="urn:child"/>`),
			want:     ErrResourceIdentity,
		},
		"bytes": {
			location: "child.wsdl",
			content:  []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/" targetNamespace="urn:child"/>`),
			limit:    int64(len(root("child.wsdl")) + 1),
			want:     ErrLimitExceeded,
		},
		"content": {
			location: "child.wsdl",
			content:  []byte(`<not-wsdl/>`),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler, err := New(Options{
				Resolver: internalResolverFunc(func(_ context.Context, request resolve.Request) (resolve.Resource, error) {
					return resolve.Resource{URI: request.URI, Content: test.content}, nil
				}),
				Limits: Limits{MaxBytes: test.limit},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = compiler.Compile(context.Background(), Source{
				URI: "https://example.test/root.wsdl", Content: root(test.location),
			})
			if test.want == nil && err == nil {
				t.Fatal("Compile() error = nil")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Compile() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateReferenceRejectsIncludeParentVersionMismatch(t *testing.T) {
	t.Parallel()

	parent := mustParseDocument(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` targetNamespace="urn:test"/>`)
	child := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` targetNamespace="urn:test"/>`)
	err := validateReference(reference{
		version: wsdl.Version20, namespace: "urn:test", include: true,
	}, child, parent)
	if !errors.Is(err, ErrVersion) {
		t.Fatalf("validateReference() error = %v", err)
	}
}

func TestResolveDocumentRejectsReferenceWithoutAbsoluteURI(t *testing.T) {
	t.Parallel()

	document := mustParseDocument(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` targetNamespace="urn:test"><import namespace="urn:other"/></definitions>`)
	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	state := compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"urn:root": {document: document},
	}}
	if err := state.resolveDocument(context.Background(), "urn:root", 1); !errors.Is(err, ErrResourceIdentity) {
		t.Fatalf("resolveDocument() error = %v", err)
	}
}

func TestBuildSetRejectsDuplicateNamesForEveryComponentKind(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"WSDL 1.1 interface": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` targetNamespace="urn:test"><portType name="Duplicate"/></definitions>`,
		"WSDL 1.1 binding": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` targetNamespace="urn:test"><binding name="Duplicate" type="foreign:Port"` +
			` xmlns:foreign="urn:foreign"/></definitions>`,
		"WSDL 1.1 service": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` targetNamespace="urn:test"><service name="Duplicate"/></definitions>`,
		"WSDL 2.0 interface": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><interface name="Duplicate"/></description>`,
		"WSDL 2.0 binding": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><binding name="Duplicate" type="urn:binding"/></description>`,
		"WSDL 2.0 service": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:foreign="urn:foreign" targetNamespace="urn:test"><service name="Duplicate"` +
			` interface="foreign:API"/></description>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler, err := New(Options{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			state := compileState{compiler: compiler, resources: map[string]*resourceDocument{
				"urn:first":  {document: mustParseDocument(t, source)},
				"urn:second": {document: mustParseDocument(t, source)},
			}}
			if _, err := state.buildSet(context.Background()); !errors.Is(err, ErrDuplicateComponent) {
				t.Fatalf("buildSet() error = %v", err)
			}
		})
	}
}

func TestBuildSetPropagatesInheritanceAndInheritedComponentLimitFailures(t *testing.T) {
	t.Parallel()

	missingParent := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` xmlns:tns="urn:test" targetNamespace="urn:test"><interface name="Child"`+
		` extends="tns:Missing"/></description>`)
	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	state := compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"urn:missing": {document: missingParent},
	}}
	if _, err := state.buildSet(context.Background()); !errors.Is(err, ErrUnresolvedComponent) {
		t.Fatalf("buildSet(inheritance) error = %v", err)
	}

	inherited := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` xmlns:tns="urn:test" targetNamespace="urn:test"><interface name="Parent">`+
		`<operation name="Call" pattern="urn:none"/></interface><interface name="Child"`+
		` extends="tns:Parent"/></description>`)
	limited, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	limited.limits.MaxComponents = 3
	state = compileState{compiler: limited, resources: map[string]*resourceDocument{
		"urn:limited": {document: inherited},
	}}
	if _, err := state.buildSet(context.Background()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("buildSet(inherited limit) error = %v", err)
	}
}

func TestBindingOperationResolutionSkipsForeignDocumentsAndOutputMismatches(t *testing.T) {
	t.Parallel()

	matching := mustParseDocument(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` targetNamespace="urn:test"><portType name="Port"><operation name="Call">`+
		`<input name="Input"/><output name="Output"/></operation></portType></definitions>`)
	foreign := mustParseDocument(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` targetNamespace="urn:foreign"><portType name="Port"/></definitions>`)
	wrongPort := mustParseDocument(t, `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"`+
		` targetNamespace="urn:test"><portType name="Other"/></definitions>`)
	version20 := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` targetNamespace="urn:test"/>`)
	resolved := compileBindingOperationReference11(wsdl.BindingOperation11{
		Name: "Call", Input: &wsdl.BindingMessage11{Name: "Input"},
		Output: &wsdl.BindingMessage11{Name: "Mismatch"},
	}, wsdl.QName{Namespace: "urn:test", Local: "Port"}, map[string]*resourceDocument{
		"matching": {document: matching}, "foreign": {document: foreign},
		"wrongPort": {document: wrongPort}, "version20": {document: version20},
	})
	if resolved.Input != "Input" || resolved.Output != "Mismatch" {
		t.Fatalf("compileBindingOperationReference11() = %#v", resolved)
	}
}

func TestCompileSchemasPropagatesEveryConstructionFailure(t *testing.T) {
	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	schemaSource := `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
		`<types><xs:schema targetNamespace="urn:test"/></types></definitions>`

	nilSchema := mustParseDocument(t, schemaSource)
	definitions, _ := nilSchema.Definitions11()
	definitions.Types.Schemas[0] = nil
	state := compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"urn:nil-schema": {document: nilSchema},
	}}
	if _, err := state.compileSchemas(context.Background(), []string{"urn:nil-schema"}); err == nil {
		t.Fatal("compileSchemas(nil schema) error = nil")
	}

	invalidOwner := mustParseDocument(t, schemaSource)
	state = compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"%": {document: invalidOwner},
	}}
	if _, err := state.compileSchemas(context.Background(), []string{"%"}); !errors.Is(err, ErrResourceIdentity) {
		t.Fatalf("compileSchemas(invalid owner) error = %v", err)
	}

	relativeOwner := mustParseDocument(t, schemaSource)
	state = compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"relative.wsdl": {document: relativeOwner},
	}}
	if _, err := state.compileSchemas(context.Background(), []string{"relative.wsdl"}); err == nil {
		t.Fatal("compileSchemas(relative resolver key) error = nil")
	}

	importOnly := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">`+
		`<types><xs:import namespace="urn:external" schemaLocation="urn:external"/>`+
		`</types></description>`)
	invalidCompiler := &Compiler{
		schemaResolver: xsdresolve.Deny(),
		schemaLimits:   xsdcompile.Limits{MaxSchemas: -1},
	}
	state = compileState{compiler: invalidCompiler, resources: map[string]*resourceDocument{
		"urn:import": {document: importOnly},
	}}
	if _, err := state.compileSchemas(context.Background(), []string{"urn:import"}); err == nil {
		t.Fatal("compileSchemas(invalid compiler) error = nil")
	}

	injected := errors.New("injected schema wrapper failure")
	originalEscapeSchemaText := escapeSchemaText
	escapeSchemaText = func(io.Writer, []byte) error { return injected }
	t.Cleanup(func() { escapeSchemaText = originalEscapeSchemaText })
	state = compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"urn:import": {document: importOnly},
	}}
	if _, err := state.compileSchemas(context.Background(), []string{"urn:import"}); !errors.Is(err, injected) {
		t.Fatalf("compileSchemas(wrapper) error = %v", err)
	}
}

func TestSchemaWrapperPropagatesEveryEscapeFailure(t *testing.T) {
	injected := errors.New("injected schema escape failure")
	originalEscapeSchemaText := escapeSchemaText
	escapeSchemaText = func(io.Writer, []byte) error { return injected }
	t.Cleanup(func() { escapeSchemaText = originalEscapeSchemaText })

	tests := map[string]struct {
		sources []inlineSchemaSource
		imports []xsd.SchemaReference
	}{
		"source namespace": {sources: []inlineSchemaSource{{namespace: "urn:test", uri: "urn:schema"}}},
		"source URI":       {sources: []inlineSchemaSource{{uri: "urn:schema"}}},
		"import namespace": {imports: []xsd.SchemaReference{{Namespace: "urn:test"}}},
		"import URI":       {imports: []xsd.SchemaReference{{URI: "urn:schema"}}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := schemaWrapper(test.sources, test.imports); !errors.Is(err, injected) {
				t.Fatalf("schemaWrapper() error = %v", err)
			}
		})
	}
}

func TestSchemaReferenceValidationRejectsEveryWSDLReferenceShape(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"WSDL 1.1 message part": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><message name="Message">` +
			`<part name="value" element="tns:Missing"/></message></definitions>`,
		"WSDL 2.0 interface fault": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><interface name="API">` +
			`<fault name="Failure" element="tns:Missing"/></interface></description>`,
		"WSDL 2.0 binding fault header": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
			` targetNamespace="urn:test"><binding name="Binding" type="urn:binding">` +
			`<fault ref="tns:Failure"><wsoap:header element="tns:Missing"/></fault></binding></description>`,
		"WSDL 2.0 binding input header": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
			` targetNamespace="urn:test"><binding name="Binding" type="urn:binding">` +
			`<operation ref="tns:Call"><input><wsoap:header element="tns:Missing"/>` +
			`</input></operation></binding></description>`,
		"WSDL 2.0 binding output header": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
			` targetNamespace="urn:test"><binding name="Binding" type="urn:binding">` +
			`<operation ref="tns:Call"><output><wsoap:header element="tns:Missing"/>` +
			`</output></operation></binding></description>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			document := mustParseDocument(t, source)
			err := validateSchemaReferences20(map[string]*resourceDocument{
				"urn:test": {document: document},
			}, nil)
			if !errors.Is(err, ErrUnresolvedComponent) {
				t.Fatalf("validateSchemaReferences20() error = %v", err)
			}
		})
	}
}

func TestOperationStyleAndRPCSchemaHelpersCoverMissingShapes(t *testing.T) {
	t.Parallel()

	document := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` targetNamespace="urn:test"><interface name="API"><operation name="Call"`+
		` pattern="urn:none" style="http://www.w3.org/ns/wsdl/style/iri"/>`+
		`</interface></description>`)
	if err := validateOperationStyleSchemas20(map[string]*resourceDocument{
		"urn:test": {document: document},
	}, nil); err != nil {
		t.Fatalf("validateOperationStyleSchemas20() error = %v", err)
	}
	emptySchemas := &xsdcompile.Set{}
	message := wsdl.InterfaceMessageReference20{
		Element: wsdl.QName{Namespace: "urn:test", Local: "Missing"},
	}
	if err := validateOperationStyleSchema20(wsdl.StyleIRI, "Call", message, emptySchemas); !errors.Is(err, ErrInvalidIRIStyle) {
		t.Fatalf("validateOperationStyleSchema20() error = %v", err)
	}
	if _, err := rpcMessageShape("Call", "input", []wsdl.InterfaceMessageReference20{message}, emptySchemas); !errors.Is(err, ErrInvalidRPCStyle) {
		t.Fatalf("rpcMessageShape() error = %v", err)
	}
	missing := wsdl.QName{Namespace: "urn:test", Local: "Missing"}
	if err := validateRPCSignature20(wsdl.InterfaceOperation20{Name: "Call"},
		rpcMessageShape20{elements: map[wsdl.QName]xsd.QName{missing: {}}},
		rpcMessageShape20{elements: map[wsdl.QName]xsd.QName{}}); !errors.Is(err, ErrInvalidRPCStyle) {
		t.Fatalf("validateRPCSignature20() error = %v", err)
	}
}

func TestRPCSharedParametersRequireNamedMatchingTypes(t *testing.T) {
	t.Parallel()

	name := wsdl.QName{Namespace: "urn:test", Local: "Value"}
	namedType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	operation := wsdl.InterfaceOperation20{
		Name: "Call",
		RPCSignature: []wsdl.RPCSignatureParameter20{{
			Name: name, Direction: wsdl.RPCDirectionInOut,
		}},
	}
	tests := map[string][2]xsd.QName{
		"both unnamed":   {{}, {}},
		"unnamed input":  {{}, namedType},
		"unnamed output": {namedType, {}},
	}
	for testName, types := range tests {
		err := validateRPCSignature20(
			operation,
			rpcMessageShape20{elements: map[wsdl.QName]xsd.QName{name: types[0]}},
			rpcMessageShape20{elements: map[wsdl.QName]xsd.QName{name: types[1]}},
		)
		if !errors.Is(err, ErrInvalidRPCStyle) {
			t.Errorf("validateRPCSignature20(%s) error = %v", testName, err)
		}
	}
}

func TestStyleTypeHelpersCoverInlineMissingAndCycleBoundaries(t *testing.T) {
	t.Parallel()

	inlineComplex := &xsd.ComplexType{}
	if got, ok := elementComplexType(xsd.Element{InlineComplexType: inlineComplex}, nil); !ok || got.Name != inlineComplex.Name {
		t.Fatalf("elementComplexType(inline) = (%#v, %t)", got, ok)
	}
	if _, ok := elementComplexType(xsd.Element{}, nil); ok {
		t.Fatal("elementComplexType(empty) succeeded")
	}
	if iriSimpleElementAllowed(xsd.Element{InlineComplexType: inlineComplex}, nil) {
		t.Fatal("iriSimpleElementAllowed(complex) succeeded")
	}
	base := xsd.QName{Namespace: "urn:test", Local: "Base"}
	definition := xsd.SimpleType{Base: base}
	if iriSimpleTypeForbidden(definition, nil, nil) {
		t.Fatal("iriSimpleTypeForbidden(nil schemas) = true")
	}
	if iriSimpleTypeForbidden(definition, &xsdcompile.Set{}, map[xsd.QName]struct{}{base: {}}) {
		t.Fatal("iriSimpleTypeForbidden(cycle) = true")
	}
	if iriSimpleTypeForbidden(definition, &xsdcompile.Set{}, make(map[xsd.QName]struct{})) {
		t.Fatal("iriSimpleTypeForbidden(missing base) = true")
	}
}

func TestFaultOrderingCoversDeclaredAndInheritedCollections(t *testing.T) {
	t.Parallel()

	namespace := "urn:test"
	faults := []wsdl.QName{
		{Namespace: namespace, Local: "Zulu"},
		{Namespace: namespace, Local: "Alpha"},
	}
	interfaces := []Interface{
		{Name: wsdl.QName{Namespace: namespace, Local: "Parent"}, Faults: faults},
		{Name: wsdl.QName{Namespace: namespace, Local: "Child"},
			Extends: []wsdl.QName{{Namespace: namespace, Local: "Parent"}}},
	}
	if _, err := expandInterfaceInheritance(interfaces); err != nil {
		t.Fatalf("expandInterfaceInheritance() error = %v", err)
	}
	if interfaces[1].Faults[0].Local != "Alpha" {
		t.Fatalf("inherited faults = %#v", interfaces[1].Faults)
	}
	document := mustParseDocument(t, `<description xmlns="http://www.w3.org/ns/wsdl"`+
		` targetNamespace="urn:test"><interface name="API"><fault name="Zulu" element="#none"/>`+
		`<fault name="Alpha" element="#none"/></interface></description>`)
	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	state := compileState{compiler: compiler, resources: map[string]*resourceDocument{
		"urn:test": {document: document},
	}}
	set, err := state.buildSet(context.Background())
	if err != nil {
		t.Fatalf("buildSet() error = %v", err)
	}
	if set.interfaces[0].Faults[0].Local != "Alpha" {
		t.Fatalf("declared faults = %#v", set.interfaces[0].Faults)
	}
}

type internalResolverFunc func(context.Context, resolve.Request) (resolve.Resource, error)

func (f internalResolverFunc) Resolve(ctx context.Context, request resolve.Request) (resolve.Resource, error) {
	return f(ctx, request)
}

func mustParseDocument(t *testing.T, source string) *wsdl.Document {
	t.Helper()
	return mustParseDocumentWithSystemID(t, source, "")
}

func mustParseDocumentWithSystemID(t *testing.T, source string, systemID string) *wsdl.Document {
	t.Helper()
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{SystemID: systemID})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return document
}
