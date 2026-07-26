package validate

import (
	"context"
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
)

func TestValidateAttributesStopsWhenDiagnosticsLimitIsReached(t *testing.T) {
	t.Parallel()

	set := attributeValidationSet(t)
	booleanType := xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}
	missing := xsd.QName{Namespace: "urn:attributes", Local: "missing"}
	inline := xsd.QName{Namespace: "urn:wildcard", Local: "inline"}
	typed := xsd.QName{Namespace: "urn:wildcard", Local: "typed"}
	local := xsd.QName{Local: "local"}
	inlineBoolean := &xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    booleanType,
	}
	inlineQName := &xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: "QName"},
	}
	strictWildcard := &xsd.Wildcard{
		Namespaces:      []string{"##other"},
		ProcessContents: xsd.ProcessStrict,
	}

	for _, test := range []struct {
		name          string
		attributes    map[xsd.QName]string
		uses          []xsd.AttributeUse
		wildcard      *xsd.Wildcard
		typeNamespace string
	}{
		{
			name: "unresolved reference",
			uses: []xsd.AttributeUse{{Ref: missing}},
		},
		{
			name: "required attribute",
			uses: []xsd.AttributeUse{{Name: "local", Use: xsd.AttributeRequired}},
		},
		{
			name:       "prohibited attribute",
			attributes: map[xsd.QName]string{local: "value"},
			uses:       []xsd.AttributeUse{{Name: "local", Use: xsd.AttributeProhibited}},
		},
		{
			name:       "anonymous lexical value",
			attributes: map[xsd.QName]string{local: "invalid"},
			uses:       []xsd.AttributeUse{{Name: "local", InlineSimpleType: inlineBoolean}},
		},
		{
			name:       "anonymous namespace context",
			attributes: map[xsd.QName]string{local: "missing:value"},
			uses:       []xsd.AttributeUse{{Name: "local", InlineSimpleType: inlineQName}},
		},
		{
			name:       "named lexical value",
			attributes: map[xsd.QName]string{local: "invalid"},
			uses:       []xsd.AttributeUse{{Name: "local", Type: booleanType}},
		},
		{
			name:       "fixed value",
			attributes: map[xsd.QName]string{local: "false"},
			uses:       []xsd.AttributeUse{{Name: "local", Type: booleanType, Fixed: "true"}},
		},
		{
			name:          "strict undeclared wildcard",
			attributes:    map[xsd.QName]string{missing: "value"},
			wildcard:      strictWildcard,
			typeNamespace: "urn:attributes",
		},
		{
			name:          "wildcard anonymous lexical value",
			attributes:    map[xsd.QName]string{inline: "invalid"},
			wildcard:      strictWildcard,
			typeNamespace: "urn:attributes",
		},
		{
			name:          "wildcard named lexical value",
			attributes:    map[xsd.QName]string{typed: "invalid"},
			wildcard:      strictWildcard,
			typeNamespace: "urn:attributes",
		},
		{
			name:       "attribute without wildcard",
			attributes: map[xsd.QName]string{missing: "value"},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			state := validationState{validator: &Validator{
				set:    set,
				limits: Limits{MaxDiagnostics: 0},
			}}
			node := &instanceNode{
				Attributes:     test.attributes,
				AttributeTypes: map[xsd.QName]xsd.QName{},
				Namespaces:     map[string]string{},
			}
			err := state.validateAttributes(
				node,
				test.typeNamespace,
				test.uses,
				test.wildcard,
				"/root",
			)
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("validateAttributes() error = %v", err)
			}
		})
	}
}

func TestUntypedAttributeFixedValueEquality(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	for _, test := range []struct {
		left, right string
		want        bool
	}{
		{left: "same", right: "same", want: true},
		{left: "left", right: "right"},
	} {
		equal, err := state.attributeValuesEqual(
			xsd.AttributeUse{},
			test.left,
			test.right,
			nil,
		)
		if err != nil || equal != test.want {
			t.Fatalf("attributeValuesEqual(%q, %q) = %t, %v", test.left, test.right, equal, err)
		}
	}
}

func attributeValidationSet(t *testing.T) *compile.Set {
	t.Helper()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/attributes.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:wildcard"
 targetNamespace="urn:wildcard">
 <xs:attribute name="inline"><xs:simpleType><xs:restriction base="xs:boolean"/></xs:simpleType></xs:attribute>
 <xs:attribute name="typed" type="xs:boolean"/>
 <xs:complexType name="IntegerContent"><xs:simpleContent>
  <xs:extension base="xs:integer"/>
 </xs:simpleContent></xs:complexType>
 <xs:complexType name="Restricted"><xs:simpleContent>
  <xs:restriction base="t:IntegerContent"><xs:minInclusive value="0"/></xs:restriction>
 </xs:simpleContent></xs:complexType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}
