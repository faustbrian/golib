package compile

import (
	"context"
	"slices"
	"strings"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestApplySimpleContentDerivation(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	baseAttribute := xsd.AttributeUse{
		Name: "base",
		Type: stringType,
		Use:  xsd.AttributeOptional,
	}
	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`+
			`<xs:complexType name="Base" final="extension"/>`+
			`</xs:schema>`,
	), xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	finalExtension := document.ComplexTypes[0].Final
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{}}
	for _, test := range []struct {
		name       string
		derived    xsd.ComplexType
		base       xsd.ComplexType
		wantAttrs  int
		wantSimple xsd.QName
		wantWild   []string
		wantError  string
	}{
		{
			name:      "final prohibits extension",
			derived:   xsd.ComplexType{Derivation: xsd.DerivationExtension},
			base:      xsd.ComplexType{Final: finalExtension},
			wantError: "prohibits",
		},
		{
			name:       "restriction inherits simple base",
			derived:    xsd.ComplexType{Derivation: xsd.DerivationRestriction},
			base:       xsd.ComplexType{SimpleBase: stringType, Attributes: []xsd.AttributeUse{baseAttribute}},
			wantAttrs:  1,
			wantSimple: stringType,
		},
		{
			name: "invalid attribute restriction",
			derived: xsd.ComplexType{
				Derivation: xsd.DerivationRestriction,
				Attributes: []xsd.AttributeUse{{Name: "extra", Type: stringType}},
			},
			base: xsd.ComplexType{
				Base:       stringType,
				Attributes: []xsd.AttributeUse{{Name: "required", Type: stringType, Use: xsd.AttributeRequired}},
			},
			wantError: "attribute uses",
		},
		{
			name: "extension appends attributes and clones wildcard",
			derived: xsd.ComplexType{
				Derivation: xsd.DerivationExtension,
				Attributes: []xsd.AttributeUse{{Name: "derived", Type: stringType}},
			},
			base: xsd.ComplexType{
				Base:              stringType,
				Attributes:        []xsd.AttributeUse{baseAttribute},
				AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:base"}},
			},
			wantAttrs:  2,
			wantSimple: stringType,
			wantWild:   []string{"urn:base"},
		},
		{
			name: "extension unions wildcards",
			derived: xsd.ComplexType{
				Derivation:        xsd.DerivationExtension,
				AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:derived"}},
			},
			base: xsd.ComplexType{
				SimpleBase:        stringType,
				AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:base"}},
			},
			wantSimple: stringType,
			wantWild:   []string{"urn:base", "urn:derived"},
		},
		{
			name:    "extension inherits anonymous simple content type",
			derived: xsd.ComplexType{Derivation: xsd.DerivationExtension},
			base: xsd.ComplexType{
				SimpleContent:    true,
				SimpleBase:       stringType,
				InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType},
			},
			wantSimple: stringType,
		},
		{
			name: "restriction inherits anonymous simple content type",
			derived: xsd.ComplexType{
				Derivation:   xsd.DerivationRestriction,
				SimpleFacets: []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "3"}},
			},
			base: xsd.ComplexType{
				SimpleContent:    true,
				SimpleBase:       stringType,
				InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType},
			},
			wantSimple: stringType,
		},
		{
			name: "extension rejects facets",
			derived: xsd.ComplexType{
				Derivation:   xsd.DerivationExtension,
				SimpleFacets: []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "3"}},
			},
			base:      xsd.ComplexType{SimpleBase: stringType},
			wantError: "cannot declare facets",
		},
		{
			name: "restriction rejects invalid facet",
			derived: xsd.ComplexType{
				Derivation:   xsd.DerivationRestriction,
				SimpleFacets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "3"}},
			},
			base:      xsd.ComplexType{SimpleBase: stringType},
			wantError: "not applicable",
		},
		{
			name: "restriction rejects wider wildcard",
			derived: xsd.ComplexType{
				Derivation: xsd.DerivationRestriction,
				AttributeWildcard: &xsd.Wildcard{
					Namespaces: []string{"##any"},
				},
			},
			base: xsd.ComplexType{
				SimpleBase: stringType,
				AttributeWildcard: &xsd.Wildcard{
					Namespaces: []string{"urn:base"},
				},
			},
			wantError: "wildcard",
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			derived := test.derived
			err := state.applySimpleContentDerivation(&derived, test.base)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("applySimpleContentDerivation() error = %v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("applySimpleContentDerivation() error = %v", err)
			}
			if len(derived.Attributes) != test.wantAttrs {
				t.Fatalf("attributes = %d, want %d", len(derived.Attributes), test.wantAttrs)
			}
			if derived.SimpleBase != test.wantSimple {
				t.Fatalf("simple base = %#v, want %#v", derived.SimpleBase, test.wantSimple)
			}
			if test.wantWild == nil {
				if derived.AttributeWildcard != nil {
					t.Fatalf("wildcard = %#v, want nil", derived.AttributeWildcard)
				}
				return
			}
			if derived.AttributeWildcard == nil ||
				!slices.Equal(derived.AttributeWildcard.Namespaces, test.wantWild) {
				t.Fatalf("wildcard = %#v, want %v", derived.AttributeWildcard, test.wantWild)
			}
		})
	}
}

func TestSimpleContentExtensionRejectsProgrammaticFacets(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	invalid := xsd.ComplexType{
		SimpleContent: true,
		Derivation:    xsd.DerivationExtension,
		Base:          stringType,
		SimpleFacets:  []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "1"}},
	}
	state := emptyValidationState()
	anonymous := invalid
	if err := state.expandAnonymousComplexType(&anonymous); err == nil {
		t.Fatal("expandAnonymousComplexType(extension facets) succeeded")
	}
	name := xsd.QName{Namespace: "urn:test", Local: "Invalid"}
	invalid.Name = name.Local
	state.complexTypes[name] = invalid
	if err := state.compileComplexType(name, map[xsd.QName]uint8{}); err == nil {
		t.Fatal("compileComplexType(extension facets) succeeded")
	}
}

func TestTypeDerivationMethodChains(t *testing.T) {
	t.Parallel()

	base := xsd.QName{Namespace: "urn:test", Local: "Base"}
	extended := xsd.QName{Namespace: "urn:test", Local: "Extended"}
	restricted := xsd.QName{Namespace: "urn:test", Local: "Restricted"}
	cycleA := xsd.QName{Namespace: "urn:test", Local: "CycleA"}
	cycleB := xsd.QName{Namespace: "urn:test", Local: "CycleB"}
	state := &compileState{
		complexTypes: map[xsd.QName]xsd.ComplexType{
			base:     {},
			extended: {Base: base, Derivation: xsd.DerivationExtension},
			cycleA:   {Base: cycleB, Derivation: xsd.DerivationExtension},
			cycleB:   {Base: cycleA, Derivation: xsd.DerivationRestriction},
		},
		simpleTypes: map[xsd.QName]xsd.SimpleType{
			restricted: {
				Variety: xsd.SimpleRestriction,
				Base:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
			},
		},
	}
	for _, test := range []struct {
		name    string
		derived xsd.QName
		base    xsd.QName
		want    []xsd.Derivation
		ok      bool
	}{
		{name: "empty base accepts any type", derived: extended, ok: true},
		{name: "any type accepts any type", derived: extended, base: xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}, ok: true},
		{name: "same type", derived: base, base: base, ok: true},
		{name: "complex extension", derived: extended, base: base, want: []xsd.Derivation{xsd.DerivationExtension}, ok: true},
		{name: "named simple restriction", derived: restricted, base: xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}, want: []xsd.Derivation{xsd.DerivationRestriction, xsd.DerivationRestriction}, ok: true},
		{name: "built in restriction chain", derived: xsd.QName{Namespace: xsd.Namespace, Local: "token"}, base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, want: []xsd.Derivation{xsd.DerivationRestriction, xsd.DerivationRestriction}, ok: true},
		{name: "built in list", derived: xsd.QName{Namespace: xsd.Namespace, Local: "IDREFS"}, base: xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}, want: []xsd.Derivation{xsd.DerivationList}, ok: true},
		{name: "unknown built in", derived: xsd.QName{Namespace: xsd.Namespace, Local: "unknown"}, base: base},
		{name: "unknown external", derived: xsd.QName{Namespace: "urn:test", Local: "Missing"}, base: base},
		{name: "cycle", derived: cycleA, base: base},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := state.typeDerivationMethods(test.derived, test.base)
			if ok != test.ok || !slices.Equal(got, test.want) {
				t.Fatalf("typeDerivationMethods() = %v, %t; want %v, %t", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestElementTypeDerivationMethodSources(t *testing.T) {
	t.Parallel()

	base := xsd.QName{Namespace: "urn:test", Local: "Base"}
	extended := xsd.QName{Namespace: "urn:test", Local: "Extended"}
	state := &compileState{complexTypes: map[xsd.QName]xsd.ComplexType{
		extended: {Base: base, Derivation: xsd.DerivationExtension},
	}}
	for _, test := range []struct {
		name    string
		element xsd.Element
		base    xsd.QName
		want    []xsd.Derivation
		ok      bool
	}{
		{name: "named type", element: xsd.Element{Type: extended}, base: base, want: []xsd.Derivation{xsd.DerivationExtension}, ok: true},
		{name: "inline complex type", element: xsd.Element{InlineComplexType: &xsd.ComplexType{Base: base, Derivation: xsd.DerivationRestriction}}, base: base, want: []xsd.Derivation{xsd.DerivationRestriction}, ok: true},
		{name: "inline simple type", element: xsd.Element{InlineSimpleType: &xsd.SimpleType{Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}}}, base: xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}, want: []xsd.Derivation{xsd.DerivationRestriction, xsd.DerivationRestriction}, ok: true},
		{name: "nested inline simple base", element: xsd.Element{InlineSimpleType: &xsd.SimpleType{InlineBase: &xsd.SimpleType{Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}}}}, base: xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}, want: []xsd.Derivation{xsd.DerivationRestriction, xsd.DerivationRestriction}, ok: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := state.elementTypeDerivationMethods(test.element, test.base)
			if ok != test.ok || !slices.Equal(got, test.want) {
				t.Fatalf("elementTypeDerivationMethods() = %v, %t; want %v, %t", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestSetElementTypeDerivationMethodSources(t *testing.T) {
	t.Parallel()

	base := xsd.QName{Namespace: "urn:test", Local: "Base"}
	set := &Set{complexTypes: map[xsd.QName]xsd.ComplexType{}, simpleTypes: map[xsd.QName]xsd.SimpleType{}}
	for _, element := range []xsd.Element{
		{Type: base},
		{InlineComplexType: &xsd.ComplexType{Base: base, Derivation: xsd.DerivationExtension}},
		{InlineSimpleType: &xsd.SimpleType{Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}}},
	} {
		if _, ok := setElementTypeDerivationMethods(set, element, xsd.QName{}); !ok {
			t.Fatalf("setElementTypeDerivationMethods(%#v) failed", element)
		}
	}
}
