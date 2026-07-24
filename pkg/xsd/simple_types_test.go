package xsd_test

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestParseCapturesRestrictionFacetsListsAndUnions(t *testing.T) {
	t.Parallel()

	const schema = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:types" targetNamespace="urn:types">
 <xs:simpleType name="Code"><xs:restriction base="xs:token">
  <xs:minLength value="2"/><xs:maxLength value="5"/>
  <xs:pattern value="[A-Z]+"/><xs:enumeration value="AB"/>
 </xs:restriction></xs:simpleType>
 <xs:simpleType name="Codes"><xs:list itemType="tns:Code"/></xs:simpleType>
 <xs:simpleType name="Choice"><xs:union memberTypes="xs:decimal tns:Code"/></xs:simpleType>
</xs:schema>`

	document, err := xsd.Parse(context.Background(), []byte(schema), xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	code := document.SimpleTypes[0]
	if code.Variety != xsd.SimpleRestriction ||
		code.Base != (xsd.QName{Namespace: xsd.Namespace, Local: "token"}) ||
		len(code.Facets) != 4 || code.Facets[0].Kind != xsd.FacetMinLength ||
		code.Facets[3].Value != "AB" {
		t.Fatalf("Code = %#v", code)
	}
	codes := document.SimpleTypes[1]
	if codes.Variety != xsd.SimpleList ||
		codes.ItemType != (xsd.QName{Namespace: "urn:types", Local: "Code"}) {
		t.Fatalf("Codes = %#v", codes)
	}
	choice := document.SimpleTypes[2]
	if choice.Variety != xsd.SimpleUnion || len(choice.MemberTypes) != 2 ||
		choice.MemberTypes[0] != (xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}) ||
		choice.MemberTypes[1] != (xsd.QName{Namespace: "urn:types", Local: "Code"}) {
		t.Fatalf("Choice = %#v", choice)
	}
}
