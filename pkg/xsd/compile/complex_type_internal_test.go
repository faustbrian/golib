package compile

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestCompileComplexTypeRejectsInvalidDerivations(t *testing.T) {
	t.Parallel()

	name := xsd.QName{Namespace: "urn:test", Local: "Derived"}
	baseName := xsd.QName{Namespace: "urn:test", Local: "Base"}
	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`+
			`<xs:complexType name="Base" final="extension"/>`+
			`</xs:schema>`,
	), xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	finalExtension := document.ComplexTypes[0].Final

	for _, test := range []struct {
		name  string
		types map[xsd.QName]xsd.ComplexType
	}{
		{
			name:  "missing base name",
			types: map[xsd.QName]xsd.ComplexType{name: {Derivation: xsd.DerivationExtension}},
		},
		{
			name: "invalid method",
			types: map[xsd.QName]xsd.ComplexType{name: {
				Base: baseName, Derivation: "invalid",
			}},
		},
		{
			name: "missing simple content base",
			types: map[xsd.QName]xsd.ComplexType{name: {
				Base: missing, Derivation: xsd.DerivationExtension, SimpleContent: true,
			}},
		},
		{
			name: "recursive simple content",
			types: map[xsd.QName]xsd.ComplexType{
				name: {
					Base: baseName, Derivation: xsd.DerivationExtension, SimpleContent: true,
				},
				baseName: {
					Base: name, Derivation: xsd.DerivationExtension, SimpleContent: true,
				},
			},
		},
		{
			name: "complex base for simple content",
			types: map[xsd.QName]xsd.ComplexType{
				name: {
					Base: baseName, Derivation: xsd.DerivationExtension, SimpleContent: true,
				},
				baseName: {},
			},
		},
		{
			name: "prohibited simple content derivation",
			types: map[xsd.QName]xsd.ComplexType{
				name: {
					Base: baseName, Derivation: xsd.DerivationExtension, SimpleContent: true,
				},
				baseName: {
					SimpleContent: true,
					SimpleBase:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
					Final:         finalExtension,
				},
			},
		},
		{
			name: "missing complex base",
			types: map[xsd.QName]xsd.ComplexType{name: {
				Base: missing, Derivation: xsd.DerivationExtension,
			}},
		},
		{
			name: "recursive complex content",
			types: map[xsd.QName]xsd.ComplexType{
				name:     {Base: baseName, Derivation: xsd.DerivationExtension},
				baseName: {Base: name, Derivation: xsd.DerivationExtension},
			},
		},
		{
			name: "prohibited complex derivation",
			types: map[xsd.QName]xsd.ComplexType{
				name:     {Base: baseName, Derivation: xsd.DerivationExtension},
				baseName: {Final: finalExtension},
			},
		},
		{
			name: "mixed content mismatch",
			types: map[xsd.QName]xsd.ComplexType{
				name: {
					Base: baseName, Derivation: xsd.DerivationExtension,
					Mixed: true, MixedSet: true,
				},
				baseName: {},
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := compileState{
				complexTypes: test.types,
				simpleTypes:  map[xsd.QName]xsd.SimpleType{},
				typeKinds:    map[xsd.QName]string{},
			}
			if err := state.compileComplexType(name, map[xsd.QName]uint8{}); err == nil {
				t.Fatal("compileComplexType() succeeded")
			}
		})
	}
}

func TestCompileComplexTypeCombinesExtensionWildcards(t *testing.T) {
	t.Parallel()

	name := xsd.QName{Namespace: "urn:test", Local: "Derived"}
	baseName := xsd.QName{Namespace: "urn:test", Local: "Base"}
	state := compileState{complexTypes: map[xsd.QName]xsd.ComplexType{
		name: {
			Base:       baseName,
			Derivation: xsd.DerivationExtension,
			AttributeWildcard: &xsd.Wildcard{
				Namespaces: []string{"urn:derived"}, ProcessContents: xsd.ProcessLax,
			},
		},
		baseName: {
			AttributeWildcard: &xsd.Wildcard{
				Namespaces: []string{"urn:base"}, ProcessContents: xsd.ProcessStrict,
			},
		},
	}}
	if err := state.compileComplexType(name, map[xsd.QName]uint8{}); err != nil {
		t.Fatalf("compileComplexType() error = %v", err)
	}
	wildcard := state.complexTypes[name].AttributeWildcard
	if wildcard == nil || len(wildcard.Namespaces) != 2 ||
		wildcard.ProcessContents != xsd.ProcessLax {
		t.Fatalf("derived wildcard = %#v", wildcard)
	}
}
