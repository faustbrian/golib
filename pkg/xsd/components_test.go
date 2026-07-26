package xsd_test

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestParseCapturesGlobalDeclarationsAndTypeDefinitions(t *testing.T) {
	t.Parallel()

	const schema = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:orders="urn:orders" targetNamespace="urn:orders">
 <xs:element name="order" type="orders:OrderType" abstract="true"
  nillable="1" default="new"/>
 <xs:attribute name="currency" type="xs:string" default="EUR"/>
 <xs:simpleType name="OrderCode">
  <xs:restriction base="xs:string"/>
 </xs:simpleType>
 <xs:complexType name="OrderType" abstract="false" mixed="true"
  block="restriction" final="#all"/>
</xs:schema>`

	document, err := xsd.Parse(
		context.Background(),
		[]byte(schema),
		xsd.ParseOptions{SystemID: "https://example.test/orders.xsd"},
	)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(document.Elements) != 1 {
		t.Fatalf("Elements = %#v", document.Elements)
	}
	element := document.Elements[0]
	if element.Name != "order" ||
		element.Type != (xsd.QName{Namespace: "urn:orders", Local: "OrderType"}) ||
		!element.Abstract || !element.Nillable || element.Default != "new" {
		t.Fatalf("Element = %#v", element)
	}

	if len(document.Attributes) != 1 {
		t.Fatalf("Attributes = %#v", document.Attributes)
	}
	attribute := document.Attributes[0]
	if attribute.Name != "currency" || attribute.Type.Namespace != xsd.Namespace ||
		attribute.Type.Local != "string" || attribute.Default != "EUR" {
		t.Fatalf("Attribute = %#v", attribute)
	}

	if len(document.SimpleTypes) != 1 {
		t.Fatalf("SimpleTypes = %#v", document.SimpleTypes)
	}
	simple := document.SimpleTypes[0]
	if simple.Name != "OrderCode" || simple.Variety != xsd.SimpleRestriction ||
		simple.Base != (xsd.QName{Namespace: xsd.Namespace, Local: "string"}) {
		t.Fatalf("SimpleType = %#v", simple)
	}

	if len(document.ComplexTypes) != 1 {
		t.Fatalf("ComplexTypes = %#v", document.ComplexTypes)
	}
	complex := document.ComplexTypes[0]
	if complex.Name != "OrderType" || complex.Abstract || !complex.Mixed ||
		!complex.Block.Contains(xsd.DerivationRestriction) || !complex.Final.All() {
		t.Fatalf("ComplexType = %#v", complex)
	}
}
