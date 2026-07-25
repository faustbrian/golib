package xsd_test

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestParsePreservesCompositionDirectivesAndResolvedBaseURIs(t *testing.T) {
	t.Parallel()

	const schema = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:orders="urn:example:orders" targetNamespace="urn:example:orders"
 xml:base="schemas/">
 <xs:include schemaLocation="common.xsd"/>
 <xs:import namespace="urn:example:customers"
  schemaLocation="../customers/customer.xsd"/>
 <xs:import namespace="urn:example:external"/>
 <xs:redefine schemaLocation="legacy.xsd"/>
</xs:schema>`

	document, err := xsd.Parse(
		context.Background(),
		[]byte(schema),
		xsd.ParseOptions{SystemID: "https://example.test/wsdl/root.xsd"},
	)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if document.BaseURI != "https://example.test/wsdl/schemas/" {
		t.Fatalf("BaseURI = %q", document.BaseURI)
	}
	if document.Namespaces["xs"] != xsd.Namespace ||
		document.Namespaces["orders"] != "urn:example:orders" {
		t.Fatalf("Namespaces = %#v", document.Namespaces)
	}

	want := []xsd.SchemaReference{
		{
			Kind:     xsd.ReferenceInclude,
			Location: "common.xsd",
			URI:      "https://example.test/wsdl/schemas/common.xsd",
		},
		{
			Kind:      xsd.ReferenceImport,
			Namespace: "urn:example:customers",
			Location:  "../customers/customer.xsd",
			URI:       "https://example.test/wsdl/customers/customer.xsd",
		},
		{
			Kind:      xsd.ReferenceImport,
			Namespace: "urn:example:external",
		},
		{
			Kind:     xsd.ReferenceRedefine,
			Location: "legacy.xsd",
			URI:      "https://example.test/wsdl/schemas/legacy.xsd",
		},
	}
	if len(document.References) != len(want) {
		t.Fatalf("len(References) = %d, want %d", len(document.References), len(want))
	}
	for index := range want {
		if document.References[index] != want[index] {
			t.Fatalf("References[%d] = %#v, want %#v", index, document.References[index], want[index])
		}
	}
}
