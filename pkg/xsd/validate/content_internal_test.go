package validate

import (
	"context"
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
)

func TestValidateElementPropagatesEveryContentFailure(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	boolType := builtIn("boolean")
	qNameType := builtIn("QName")
	missingType := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	xsiNil := xsd.QName{Namespace: schemaInstanceNamespace, Local: "nil"}
	for _, test := range []struct {
		name    string
		node    *instanceNode
		element xsd.Element
	}{
		{name: "invalid xsi nil", node: contentNode("", map[xsd.QName]string{xsiNil: "invalid"}), element: xsd.Element{Nillable: true}},
		{name: "anonymous QName context", node: contentNode("missing:value", nil), element: xsd.Element{InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: qNameType}}},
		{name: "anonymous complex content", node: contentNode("text", nil), element: xsd.Element{InlineComplexType: &xsd.ComplexType{}}},
		{name: "missing named type", node: contentNode("value", nil), element: xsd.Element{Type: missingType}},
		{name: "invalid named simple value", node: contentNode("invalid", nil), element: xsd.Element{Type: boolType}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := diagnosticLimitState(set)
			if err := state.validateElement(test.node, test.element, "/root"); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("validateElement() error = %v", err)
			}
		})
	}
}

func TestValidateComplexPropagatesEveryNestedFailure(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	child := &instanceNode{
		Name:           xsd.QName{Namespace: "urn:test", Local: "child"},
		Attributes:     map[xsd.QName]string{},
		AttributeTypes: map[xsd.QName]xsd.QName{},
		Namespaces:     map[string]string{"": "urn:test"},
		Text:           "invalid",
	}
	required := xsd.AttributeUse{Name: "required", Use: xsd.AttributeRequired}
	content := &xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{{
		Element: &xsd.Element{Ref: xsd.QName{Namespace: "urn:test", Local: "child"}},
	}}}
	for _, test := range []struct {
		name       string
		node       *instanceNode
		definition xsd.ComplexType
	}{
		{name: "character data", node: contentNode("text", nil), definition: xsd.ComplexType{}},
		{name: "attributes", node: contentNode("", nil), definition: xsd.ComplexType{Attributes: []xsd.AttributeUse{required}}},
		{name: "simple content child", node: nodeWithChild(child), definition: xsd.ComplexType{SimpleContent: true, SimpleBase: builtIn("string")}},
		{name: "empty content child", node: nodeWithChild(child), definition: xsd.ComplexType{}},
		{name: "matched child validation", node: nodeWithChild(child), definition: xsd.ComplexType{Content: content}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := diagnosticLimitState(set)
			if err := state.validateComplex(test.node, "urn:test", test.definition, "/root"); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("validateComplex() error = %v", err)
			}
		})
	}

	state := diagnosticLimitState(set)
	if err := state.validateAnyType(nodeWithChild(child), "/root"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateAnyType() error = %v", err)
	}
	state = diagnosticLimitState(set)
	if err := state.validateSimpleAttributeSet(
		contentNode("", map[xsd.QName]string{{Local: "extra"}: "value"}),
		"/root",
	); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateSimpleAttributeSet() error = %v", err)
	}
}

func diagnosticLimitState(set *compile.Set) validationState {
	return validationState{validator: &Validator{set: set, limits: Limits{
		MaxDiagnostics: 0,
	}}}
}

func contentNode(text string, attributes map[xsd.QName]string) *instanceNode {
	if attributes == nil {
		attributes = map[xsd.QName]string{}
	}
	return &instanceNode{
		Name:           xsd.QName{Namespace: "urn:test", Local: "root"},
		Attributes:     attributes,
		AttributeTypes: map[xsd.QName]xsd.QName{},
		Namespaces:     map[string]string{"": "urn:test"},
		Text:           text,
	}
}

func nodeWithChild(child *instanceNode) *instanceNode {
	node := contentNode("", nil)
	node.Children = []*instanceNode{child}
	return node
}

func contentValidationSet(t *testing.T) *compile.Set {
	t.Helper()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/content.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 targetNamespace="urn:test"><xs:simpleType name="List"><xs:list itemType="xs:boolean"/></xs:simpleType>
 <xs:complexType name="Named"><xs:attribute name="required" use="required"/></xs:complexType>
 <xs:element name="child" type="xs:boolean"/>
 <xs:element name="root"><xs:complexType><xs:attribute name="ref" type="xs:IDREF"/></xs:complexType></xs:element>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}
