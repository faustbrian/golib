package xsd_test

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestParseCapturesIdentityConstraints(t *testing.T) {
	t.Parallel()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:tns="urn:identity" targetNamespace="urn:identity">
 <xs:element name="root" type="xs:string">
  <xs:key name="itemKey"><xs:selector xpath="tns:item"/><xs:field xpath="@id"/></xs:key>
  <xs:keyref name="itemRef" refer="tns:itemKey">
   <xs:selector xpath="tns:reference"/><xs:field xpath="@target"/>
  </xs:keyref>
 </xs:element>
</xs:schema>`), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	constraints := document.Elements[0].IdentityConstraints
	if len(constraints) != 2 || constraints[0].Kind != xsd.IdentityKey ||
		constraints[0].Selector != "tns:item" ||
		constraints[0].Fields[0] != "@id" ||
		constraints[1].Refer != (xsd.QName{Namespace: "urn:identity", Local: "itemKey"}) {
		t.Fatalf("IdentityConstraints = %#v", constraints)
	}
}
