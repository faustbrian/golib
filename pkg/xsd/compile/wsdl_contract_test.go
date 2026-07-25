package compile_test

import (
	"context"
	"fmt"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
)

type wsdlSchemaSet interface {
	Documents() []compile.Document
	Document(string) (compile.Document, bool)
	ElementNames() []xsd.QName
	Element(xsd.QName) (xsd.Element, bool)
	AttributeNames() []xsd.QName
	Attribute(xsd.QName) (xsd.Attribute, bool)
	SimpleTypeNames() []xsd.QName
	SimpleType(xsd.QName) (xsd.SimpleType, bool)
	ComplexTypeNames() []xsd.QName
	ComplexType(xsd.QName) (xsd.ComplexType, bool)
	ModelGroupNames() []xsd.QName
	ModelGroup(xsd.QName) (xsd.ModelGroupDefinition, bool)
	AttributeGroupNames() []xsd.QName
	AttributeGroup(xsd.QName) (xsd.AttributeGroup, bool)
	NotationNames() []xsd.QName
	Notation(xsd.QName) (xsd.Notation, bool)
	SubstitutionMember(xsd.QName, xsd.QName) (xsd.Element, bool)
}

var _ wsdlSchemaSet = (*compile.Set)(nil)

func TestCompiledSetSatisfiesWSDLConsumerContract(t *testing.T) {
	t.Parallel()

	const source = `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` xmlns:t="urn:wsdl" targetNamespace="urn:wsdl" elementFormDefault="qualified">` +
		`<xs:simpleType name="Code"><xs:restriction base="xs:string"/></xs:simpleType>` +
		`<xs:group name="Payload"><xs:sequence><xs:element name="code" type="t:Code"/></xs:sequence></xs:group>` +
		`<xs:attributeGroup name="Metadata"><xs:attribute name="version" type="xs:string"/></xs:attributeGroup>` +
		`<xs:complexType name="Request"><xs:group ref="t:Payload"/><xs:attributeGroup ref="t:Metadata"/></xs:complexType>` +
		`<xs:element name="request" type="t:Request"/>` +
		`<xs:element name="alternate" type="t:Request" substitutionGroup="t:request"/>` +
		`<xs:attribute name="language" type="xs:language"/>` +
		`<xs:notation name="wire" public="urn:wsdl:wire"/>` +
		`</xs:schema>`

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/service.xsd", Content: []byte(source),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if err := consumeForWSDL(set); err != nil {
		t.Fatal(err)
	}
}

func consumeForWSDL(set wsdlSchemaSet) error {
	if len(set.Documents()) != 1 {
		return fmt.Errorf("WSDL consumer: compiled document graph is unavailable")
	}
	if _, ok := set.Document("https://example.test/service.xsd"); !ok {
		return fmt.Errorf("WSDL consumer: root document is unavailable")
	}

	namespace := "urn:wsdl"
	requestName := xsd.QName{Namespace: namespace, Local: "request"}
	alternateName := xsd.QName{Namespace: namespace, Local: "alternate"}
	typeName := xsd.QName{Namespace: namespace, Local: "Request"}
	if len(set.ElementNames()) != 2 || len(set.AttributeNames()) != 1 ||
		len(set.SimpleTypeNames()) != 1 || len(set.ComplexTypeNames()) != 1 ||
		len(set.ModelGroupNames()) != 1 || len(set.AttributeGroupNames()) != 1 ||
		len(set.NotationNames()) != 1 {
		return fmt.Errorf("WSDL consumer: component inventories are incomplete")
	}
	if _, ok := set.Element(requestName); !ok {
		return fmt.Errorf("WSDL consumer: root element is unavailable")
	}
	if _, ok := set.Attribute(xsd.QName{Namespace: namespace, Local: "language"}); !ok {
		return fmt.Errorf("WSDL consumer: global attribute is unavailable")
	}
	if _, ok := set.SimpleType(xsd.QName{Namespace: namespace, Local: "Code"}); !ok {
		return fmt.Errorf("WSDL consumer: simple type is unavailable")
	}
	typeDefinition, ok := set.ComplexType(typeName)
	if !ok || typeDefinition.Content == nil ||
		len(typeDefinition.Content.Particles) != 1 ||
		typeDefinition.Content.Particles[0].Group == nil ||
		len(typeDefinition.Content.Particles[0].Group.Particles) != 1 ||
		typeDefinition.Content.Particles[0].Group.Particles[0].Element == nil ||
		typeDefinition.Content.Particles[0].GroupRef.Local != "" ||
		len(typeDefinition.Attributes) != 1 ||
		len(typeDefinition.AttributeGroupRefs) != 0 {
		return fmt.Errorf(
			"WSDL consumer: groups were not expanded in the type plan: %#v",
			typeDefinition,
		)
	}
	if _, ok := set.ModelGroup(xsd.QName{Namespace: namespace, Local: "Payload"}); !ok {
		return fmt.Errorf("WSDL consumer: model group is unavailable")
	}
	if _, ok := set.AttributeGroup(xsd.QName{Namespace: namespace, Local: "Metadata"}); !ok {
		return fmt.Errorf("WSDL consumer: attribute group is unavailable")
	}
	if _, ok := set.Notation(xsd.QName{Namespace: namespace, Local: "wire"}); !ok {
		return fmt.Errorf("WSDL consumer: notation is unavailable")
	}
	if _, ok := set.SubstitutionMember(requestName, alternateName); !ok {
		return fmt.Errorf("WSDL consumer: substitution membership is unavailable")
	}
	return nil
}
