package validate

import (
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestSimpleValueEqualityRejectsInvalidOperands(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	for _, test := range []struct {
		typeName xsd.QName
		left     string
		right    string
		want     bool
	}{
		{typeName: builtIn("decimal"), left: "invalid", right: "1"},
		{typeName: builtIn("decimal"), left: "1", right: "invalid"},
		{typeName: builtIn("double"), left: "invalid", right: "1"},
		{typeName: builtIn("double"), left: "NaN", right: "NaN", want: true},
		{typeName: builtIn("integer"), left: "invalid", right: "1"},
		{typeName: builtIn("integer"), left: "1", right: "invalid"},
	} {
		got, err := state.simpleValuesEqual(test.typeName, test.left, test.right)
		if err != nil || got != test.want {
			t.Fatalf("simpleValuesEqual(%s, %q, %q) = %t, %v", test.typeName.Local, test.left, test.right, got, err)
		}
	}
	complexName := xsd.QName{Namespace: "urn:wildcard", Local: "Restricted"}
	if got := state.canonicalIdentityValue(complexName, "01", nil); got != "decimal:1.0" {
		t.Fatalf("canonicalIdentityValue(Restricted, 01) = %q", got)
	}
}

func TestIdentityFieldValueBoundaryBranches(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	first := xsd.QName{Local: "first"}
	second := xsd.QName{Local: "second"}
	node := identityTestNode("root")
	node.Attributes[first] = "a"
	node.Attributes[second] = "b"
	node.AttributeTypes[first] = builtIn("string")
	node.AttributeTypes[second] = builtIn("string")
	if got := state.identityFieldValues(node, "@*", nil); len(got) != 2 || got[0] != "lexical:a" || got[1] != "lexical:b" {
		t.Fatalf("identityFieldValues(@*) = %#v", got)
	}
	if got := state.identityFieldValues(node, "@missing:value", nil); got != nil {
		t.Fatalf("identityFieldValues(invalid QName) = %#v", got)
	}
	if got := followIdentityElementPath([]*instanceNode{node}, []string{""}, nil); got != nil {
		t.Fatalf("followIdentityElementPath(empty) = %#v", got)
	}
	if got := followIdentityElementPath([]*instanceNode{node}, []string{"."}, nil); len(got) != 1 || got[0] != node {
		t.Fatalf("followIdentityElementPath(current) = %#v", got)
	}
	child := identityTestNode("child")
	child.Attributes[first] = "a"
	child.AttributeTypes[first] = builtIn("string")
	node.Children = []*instanceNode{child}
	if got := state.identityFieldValues(node, ".//@first", nil); len(got) != 1 || got[0] != "lexical:a" {
		t.Fatalf("identityFieldValues(.//@first) = %#v", got)
	}
	if got := state.identityFieldValues(node, ".//./@first", nil); len(got) != 1 || got[0] != "lexical:a" {
		t.Fatalf("identityFieldValues(.//./@first) = %#v", got)
	}
	selected := selectIdentityNodes(node, xsd.IdentityConstraint{Selector: ".//."})
	if len(selected) != 1 || selected[0] != child {
		t.Fatalf("selectIdentityNodes(.//.) = %#v", selected)
	}
	child.Nillable = true
	for _, field := range []string{"child", ".//.", ".//child", ".//missing | .//child"} {
		if !identityFieldSelectsNillable(node, field, nil) {
			t.Fatalf("identityFieldSelectsNillable(%q) = false", field)
		}
	}
	for _, field := range []string{".", "@first", ".//missing | .//other"} {
		if identityFieldSelectsNillable(node, field, nil) {
			t.Fatalf("identityFieldSelectsNillable(%q) = true", field)
		}
	}
	node.Nillable = true
	if !identityFieldSelectsNillable(node, ".", nil) {
		t.Fatal("identityFieldSelectsNillable(.) = false")
	}
}

func TestCanonicalIdentityValueBoundaryBranches(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	for _, test := range []struct {
		name       string
		definition xsd.SimpleType
		lexical    string
		want       string
	}{
		{name: "inline restriction", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: builtIn("integer")}}, lexical: "01", want: "decimal:1.0"},
		{name: "named restriction", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: builtIn("integer")}, lexical: "01", want: "decimal:1.0"},
		{name: "inline list item", definition: xsd.SimpleType{Variety: xsd.SimpleList, InlineItem: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: builtIn("integer")}}, lexical: "01 2", want: "list:11:decimal:1.0;11:decimal:2.0;"},
		{name: "inline union second member", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{{Variety: xsd.SimpleRestriction, Base: builtIn("boolean")}, {Variety: xsd.SimpleRestriction, Base: builtIn("decimal")}}}, lexical: "01", want: "decimal:1.0"},
		{name: "unknown", definition: xsd.SimpleType{Variety: "unknown"}, lexical: "value", want: ":lexical:value"},
	} {
		if got := state.canonicalIdentityDefinition(test.definition, test.lexical, nil); got != test.want {
			t.Fatalf("canonicalIdentityDefinition(%s) = %q, want %q", test.name, got, test.want)
		}
	}
	for _, test := range []struct {
		typeName xsd.QName
		lexical  string
		want     string
	}{
		{typeName: builtIn("float"), lexical: "1.5", want: "float:1.5"},
		{typeName: builtIn("float"), lexical: "NaN", want: "float:NaN"},
		{typeName: builtIn("double"), lexical: "-0", want: "double:0"},
	} {
		if got := state.canonicalIdentityValue(test.typeName, test.lexical, nil); got != test.want {
			t.Fatalf("canonicalIdentityValue(%s, %q) = %q, want %q", test.typeName.Local, test.lexical, got, test.want)
		}
	}
}

func TestSimpleContextAndFacetBoundaryBranches(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	qName := builtIn("QName")
	for _, definition := range []xsd.SimpleType{
		{Variety: xsd.SimpleList, ItemType: qName},
		{Variety: xsd.SimpleList, InlineItem: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: qName}},
		{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{qName}},
		{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{{Variety: xsd.SimpleRestriction, Base: qName}}},
	} {
		if state.inlineSimpleContextValid(definition, "missing:value", nil) {
			t.Fatalf("inlineSimpleContextValid(%#v) succeeded", definition)
		}
	}
	if !state.inlineSimpleContextValid(xsd.SimpleType{Variety: "unknown"}, "value", nil) {
		t.Fatal("unknown variety context was rejected")
	}
	if state.inlineSimpleLexicalValid(xsd.SimpleType{Variety: "unknown"}, "value") {
		t.Fatal("unknown variety lexical value was accepted")
	}

	stringType := builtIn("string")
	for _, facet := range []xsd.Facet{
		{Kind: xsd.FacetLength, Value: "invalid"},
		{Kind: xsd.FacetWhiteSpace, Value: "invalid"},
		{Kind: xsd.FacetPattern, Value: "["},
	} {
		if state.facetsValid(xsd.SimpleType{Base: stringType, Facets: []xsd.Facet{facet}}, "value") {
			t.Fatalf("facetsValid(%#v) succeeded", facet)
		}
	}
	for _, pattern := range []string{`\I`, `\C`} {
		if _, err := datatype.CompilePattern(pattern); err != nil {
			t.Fatalf("CompilePattern(%q) error = %v", pattern, err)
		}
	}
	unionBase := xsd.SimpleType{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{stringType}}
	if got := state.normalizeRestrictionLexical(xsd.SimpleType{
		Variety: xsd.SimpleRestriction, InlineBase: &unionBase,
	}, " value "); got != " value " {
		t.Fatalf("normalizeRestrictionLexical(inline union) = %q", got)
	}
}

func TestNumericFacetBoundaryBranches(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	for _, test := range []struct {
		base    xsd.QName
		lexical string
		facet   xsd.Facet
	}{
		{base: builtIn("decimal"), lexical: "invalid", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "0"}},
		{base: builtIn("decimal"), lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "invalid"}},
		{base: builtIn("double"), lexical: "invalid", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "0"}},
		{base: builtIn("double"), lexical: "NaN", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "0"}},
		{base: builtIn("duration"), lexical: "invalid", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "P1D"}},
		{base: builtIn("date"), lexical: "invalid", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "2026-01-01"}},
		{base: builtIn("string"), lexical: "value", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "a"}},
	} {
		if state.numericFacetValid(test.base, test.lexical, test.facet) {
			t.Fatalf("numericFacetValid(%s, %q, %#v) succeeded", test.base.Local, test.lexical, test.facet)
		}
	}
	if _, comparable := compareDurations("P1M", "P30D"); comparable {
		t.Fatal("compareDurations(P1M, P30D) was comparable")
	}
}

func builtIn(local string) xsd.QName {
	return xsd.QName{Namespace: xsd.Namespace, Local: local}
}
