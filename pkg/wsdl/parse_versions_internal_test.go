package wsdl

import (
	"encoding/xml"
	"testing"
)

func TestOperationStyle11RecognizesEveryMessageOrder(t *testing.T) {
	t.Parallel()

	input := &OperationMessage11{}
	output := &OperationMessage11{}
	for name, test := range map[string]struct {
		input  *OperationMessage11
		output *OperationMessage11
		order  []string
		want   OperationStyle11
	}{
		"one way":          {input: input, want: OperationStyleOneWay},
		"notification":     {output: output, want: OperationStyleNotification},
		"request response": {input: input, output: output, order: []string{"input"}, want: OperationStyleRequestResponse},
		"solicit response": {input: input, output: output, order: []string{"output"}, want: OperationStyleSolicitResponse},
		"empty":            {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := operationStyle11(test.input, test.output, test.order); got != test.want {
				t.Fatalf("operationStyle11() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWSDL11BindingDecodersRecognizeEveryTypedChild(t *testing.T) {
	t.Parallel()

	attribute := func(local, value string) xml.Attr {
		return xml.Attr{Name: xml.Name{Local: local}, Value: value}
	}
	binding, err := decodeBinding11(&xmlNode{
		name:       xml.Name{Space: NamespaceWSDL11, Local: "binding"},
		attributes: []xml.Attr{attribute("name", "Binding")},
		children: []*xmlNode{
			{name: xml.Name{Space: NamespaceSOAP11Binding, Local: "binding"}},
			{name: xml.Name{Space: NamespaceHTTPBinding, Local: "binding"}},
			{name: xml.Name{Space: NamespaceWSDL11, Local: "operation"}, attributes: []xml.Attr{attribute("name", "Call")}},
		},
	})
	if err != nil {
		t.Fatalf("decodeBinding11() error = %v", err)
	}
	if binding.SOAP == nil || binding.HTTP == nil || len(binding.Operations) != 1 {
		t.Fatalf("decodeBinding11() = %#v", binding)
	}

	operation, err := decodeBindingOperation11(&xmlNode{
		name: xml.Name{Space: NamespaceWSDL11, Local: "operation"},
		children: []*xmlNode{
			{name: xml.Name{Space: NamespaceHTTPBinding, Local: "operation"}},
			{name: xml.Name{Space: NamespaceWSDL11, Local: "input"}},
			{name: xml.Name{Space: NamespaceWSDL11, Local: "output"}},
			{name: xml.Name{Space: NamespaceWSDL11, Local: "fault"}, attributes: []xml.Attr{attribute("name", "Failure")}},
		},
	})
	if err != nil {
		t.Fatalf("decodeBindingOperation11() error = %v", err)
	}
	if operation.HTTP == nil || operation.Input == nil || operation.Output == nil ||
		len(operation.Faults) != 1 {
		t.Fatalf("decodeBindingOperation11() = %#v", operation)
	}
}

func TestNamespaceDeclarationAndDocumentationHelpers(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		attribute xml.Attr
		want      bool
	}{
		"default":  {attribute: xml.Attr{Name: xml.Name{Local: "xmlns"}}, want: true},
		"prefixed": {attribute: xml.Attr{Name: xml.Name{Space: "xmlns", Local: "ext"}}, want: true},
		"ordinary": {attribute: xml.Attr{Name: xml.Name{Local: "name"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := isNamespaceDeclarationAttribute(test.attribute); got != test.want {
				t.Fatalf("isNamespaceDeclarationAttribute() = %t, want %t", got, test.want)
			}
		})
	}

	documentation := &xmlNode{name: xml.Name{Space: NamespaceWSDL20, Local: "documentation"}}
	documentation.text.WriteString("first")
	node := &xmlNode{children: []*xmlNode{{name: xml.Name{Local: "other"}}, documentation}}
	if got := firstDocumentation(node); got == nil || got.Content != "first" {
		t.Fatalf("firstDocumentation() = %#v", got)
	}
	if got := firstDocumentation(&xmlNode{}); got != nil {
		t.Fatalf("firstDocumentation(empty) = %#v", got)
	}
}

func TestWSDL20SOAPDecodersDistinguishEmptyModulesAndHeaders(t *testing.T) {
	t.Parallel()

	empty := &xmlNode{}
	module := &xmlNode{
		name:       xml.Name{Space: NamespaceWSDL20SOAP, Local: "module"},
		attributes: []xml.Attr{{Name: xml.Name{Local: "ref"}, Value: "urn:module"}},
	}
	header := &xmlNode{
		name:       xml.Name{Space: NamespaceWSDL20SOAP, Local: "header"},
		attributes: []xml.Attr{{Name: xml.Name{Local: "element"}, Value: "Value"}},
	}
	moduleNode := &xmlNode{children: []*xmlNode{module}}
	headerNode := &xmlNode{children: []*xmlNode{header}}
	bothNode := &xmlNode{children: []*xmlNode{module, header}}

	if value, err := decodeSOAPBinding20(empty); err != nil || value != nil {
		t.Fatalf("decodeSOAPBinding20(empty) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPBinding20(moduleNode); err != nil || value == nil || len(value.Modules) != 1 {
		t.Fatalf("decodeSOAPBinding20(module) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPFaultBinding20(empty); err != nil || value != nil {
		t.Fatalf("decodeSOAPFaultBinding20(empty) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPFaultBinding20(bothNode); err != nil || value == nil ||
		len(value.Modules) != 1 || len(value.Headers) != 1 {
		t.Fatalf("decodeSOAPFaultBinding20(both) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPOperationBinding20(empty); err != nil || value != nil {
		t.Fatalf("decodeSOAPOperationBinding20(empty) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPOperationBinding20(moduleNode); err != nil || value == nil || len(value.Modules) != 1 {
		t.Fatalf("decodeSOAPOperationBinding20(module) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPMessageBinding20(empty); err != nil || value != nil {
		t.Fatalf("decodeSOAPMessageBinding20(empty) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPMessageBinding20(moduleNode); err != nil || value == nil || len(value.Modules) != 1 {
		t.Fatalf("decodeSOAPMessageBinding20(module) = (%#v, %v)", value, err)
	}
	if value, err := decodeSOAPMessageBinding20(headerNode); err != nil || value == nil || len(value.Headers) != 1 {
		t.Fatalf("decodeSOAPMessageBinding20(header) = (%#v, %v)", value, err)
	}
}
