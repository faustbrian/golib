package xsd_test

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestParseCapturesNestedParticlesAndAttributeUses(t *testing.T) {
	t.Parallel()

	const schema = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:order" targetNamespace="urn:order"
 elementFormDefault="qualified">
 <xs:element name="note" type="xs:string"/>
 <xs:complexType name="OrderType">
  <xs:sequence>
   <xs:element name="id" type="xs:string"/>
   <xs:choice minOccurs="0" maxOccurs="unbounded">
    <xs:element name="price" type="xs:decimal"/>
    <xs:element ref="tns:note"/>
   </xs:choice>
  </xs:sequence>
  <xs:attribute name="status" type="xs:string" use="required"/>
 </xs:complexType>
</xs:schema>`

	document, err := xsd.Parse(context.Background(), []byte(schema), xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	complex := document.ComplexTypes[0]
	if complex.Content == nil || complex.Content.Compositor != xsd.Sequence ||
		len(complex.Content.Particles) != 2 {
		t.Fatalf("Content = %#v", complex.Content)
	}
	first := complex.Content.Particles[0]
	if first.Element == nil || first.Element.Name != "id" ||
		first.MinOccurs != 1 || first.MaxOccurs != 1 {
		t.Fatalf("first particle = %#v", first)
	}
	choice := complex.Content.Particles[1]
	if choice.Group == nil || choice.Group.Compositor != xsd.Choice ||
		choice.MinOccurs != 0 || !choice.Unbounded ||
		len(choice.Group.Particles) != 2 {
		t.Fatalf("choice particle = %#v", choice)
	}
	if choice.Group.Particles[1].Element == nil ||
		choice.Group.Particles[1].Element.Ref != (xsd.QName{Namespace: "urn:order", Local: "note"}) {
		t.Fatalf("reference particle = %#v", choice.Group.Particles[1])
	}
	if len(complex.Attributes) != 1 || complex.Attributes[0].Name != "status" ||
		complex.Attributes[0].Use != xsd.AttributeRequired {
		t.Fatalf("Attributes = %#v", complex.Attributes)
	}
}

func TestParseCapturesTopLevelModelGroupOccurrences(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:complexType name="Items"><xs:sequence minOccurs="0" maxOccurs="unbounded">
  <xs:element name="item" type="xs:string"/>
 </xs:sequence></xs:complexType>
</xs:schema>`), xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	group := document.ComplexTypes[0].Content
	if group == nil || !group.OccursSet || group.MinOccurs != 0 || !group.Unbounded {
		t.Fatalf("Content = %#v", group)
	}
}
