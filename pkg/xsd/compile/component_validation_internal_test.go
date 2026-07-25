package compile

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestValidateSimpleTypeDefinitionRejectsEveryInvalidShape(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	baseName := xsd.QName{Namespace: "urn:test", Local: "Base"}
	listName := xsd.QName{Namespace: "urn:test", Local: "List"}
	unionName := xsd.QName{Namespace: "urn:test", Local: "Union"}
	restrictedListName := xsd.QName{Namespace: "urn:test", Local: "RestrictedList"}
	fixedWhitespaceName := xsd.QName{Namespace: "urn:test", Local: "FixedWhitespace"}
	minLengthName := xsd.QName{Namespace: "urn:test", Local: "MinLength"}
	maxLengthName := xsd.QName{Namespace: "urn:test", Local: "MaxLength"}
	fixedLengthName := xsd.QName{Namespace: "urn:test", Local: "FixedLength"}
	digitsName := xsd.QName{Namespace: "urn:test", Local: "Digits"}
	minimumName := xsd.QName{Namespace: "urn:test", Local: "Minimum"}
	maximumName := xsd.QName{Namespace: "urn:test", Local: "Maximum"}
	exclusiveMinimumName := xsd.QName{Namespace: "urn:test", Local: "ExclusiveMinimum"}
	fixedMinimumName := xsd.QName{Namespace: "urn:test", Local: "FixedMinimum"}
	finalRestriction, finalList, finalUnion := simpleTypeFinalSets(t)
	state := compileState{
		simpleTypes: map[xsd.QName]xsd.SimpleType{
			baseName:             {Name: "Base", Variety: xsd.SimpleRestriction, Base: stringType, Final: finalRestriction},
			listName:             {Name: "List", Variety: xsd.SimpleList, ItemType: stringType},
			unionName:            {Name: "Union", Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{stringType}},
			restrictedListName:   {Name: "RestrictedList", Variety: xsd.SimpleRestriction, Base: listName},
			fixedWhitespaceName:  {Name: "FixedWhitespace", Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetWhiteSpace, Value: "replace", Fixed: true}}},
			minLengthName:        {Name: "MinLength", Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "2"}}},
			maxLengthName:        {Name: "MaxLength", Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "4"}}},
			fixedLengthName:      {Name: "FixedLength", Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "3", Fixed: true}}},
			digitsName:           {Name: "Digits", Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "3"}, {Kind: xsd.FacetFractionDigits, Value: "2"}}},
			minimumName:          {Name: "Minimum", Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "1"}}},
			maximumName:          {Name: "Maximum", Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMaxExclusive, Value: "10"}}},
			exclusiveMinimumName: {Name: "ExclusiveMinimum", Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinExclusive, Value: "1"}}},
			fixedMinimumName:     {Name: "FixedMinimum", Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "1", Fixed: true}}},
			xsd.QName{Namespace: "urn:test", Local: "NoList"}: {
				Name: "NoList", Variety: xsd.SimpleRestriction, Base: stringType, Final: finalList,
			},
			xsd.QName{Namespace: "urn:test", Local: "NoUnion"}: {
				Name: "NoUnion", Variety: xsd.SimpleRestriction, Base: stringType, Final: finalUnion,
			},
		},
		typeKinds: map[xsd.QName]string{
			baseName:             "simple",
			listName:             "simple",
			unionName:            "simple",
			restrictedListName:   "simple",
			fixedWhitespaceName:  "simple",
			minLengthName:        "simple",
			maxLengthName:        "simple",
			fixedLengthName:      "simple",
			digitsName:           "simple",
			minimumName:          "simple",
			maximumName:          "simple",
			exclusiveMinimumName: "simple",
			fixedMinimumName:     "simple",
			xsd.QName{Namespace: "urn:test", Local: "NoList"}:  "simple",
			xsd.QName{Namespace: "urn:test", Local: "NoUnion"}: "simple",
		},
	}
	invalid := xsd.SimpleType{}
	for _, definition := range []xsd.SimpleType{
		{Variety: xsd.SimpleRestriction, Base: stringType, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType}},
		{Variety: xsd.SimpleRestriction, InlineBase: &invalid},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetPattern, Value: "["}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "-1"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "invalid"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetWhiteSpace, Value: "invalid"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: "unknown", Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "1"}, {Kind: xsd.FacetLength, Value: "2"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "0"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetFractionDigits, Value: "-1"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "invalid"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "1"}, {Kind: xsd.FacetMinExclusive, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMaxInclusive, Value: "1"}, {Kind: xsd.FacetMaxExclusive, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "2"}, {Kind: xsd.FacetMaxLength, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "1"}, {Kind: xsd.FacetFractionDigits, Value: "2"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "int"}, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "invalid"}}},
		{Variety: xsd.SimpleRestriction, Base: listName, Facets: []xsd.Facet{{Kind: xsd.FacetWhiteSpace, Value: "preserve"}}},
		{Variety: xsd.SimpleRestriction, Base: unionName, Facets: []xsd.Facet{{Kind: xsd.FacetWhiteSpace, Value: "collapse"}}},
		{Variety: xsd.SimpleRestriction, Base: fixedWhitespaceName, Facets: []xsd.Facet{{Kind: xsd.FacetWhiteSpace, Value: "collapse"}}},
		{Variety: xsd.SimpleRestriction, Base: minLengthName, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: maxLengthName, Facets: []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "5"}}},
		{Variety: xsd.SimpleRestriction, Base: fixedLengthName, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}}},
		{Variety: xsd.SimpleRestriction, Base: digitsName, Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "4"}}},
		{Variety: xsd.SimpleRestriction, Base: digitsName, Facets: []xsd.Facet{{Kind: xsd.FacetFractionDigits, Value: "3"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "integer"}, Facets: []xsd.Facet{{Kind: xsd.FacetFractionDigits, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}, {Kind: xsd.FacetMinLength, Value: "3"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "3"}, {Kind: xsd.FacetMaxLength, Value: "2"}}},
		{Variety: xsd.SimpleRestriction, Base: minimumName, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "0"}}},
		{Variety: xsd.SimpleRestriction, Base: maximumName, Facets: []xsd.Facet{{Kind: xsd.FacetMaxInclusive, Value: "10"}}},
		{Variety: xsd.SimpleRestriction, Base: exclusiveMinimumName, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: fixedMinimumName, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "2"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "2"}, {Kind: xsd.FacetMaxInclusive, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "date"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinExclusive, Value: "2026-01-01Z"}, {Kind: xsd.FacetMaxInclusive, Value: "2026-01-01Z"}}},
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetPattern, Value: "a", Fixed: true}}},
		{Variety: xsd.SimpleRestriction, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType}, Facets: []xsd.Facet{{Kind: xsd.FacetPattern, Value: "["}}},
		{Variety: xsd.SimpleRestriction, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleList, ItemType: stringType}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "1"}}},
		{Variety: xsd.SimpleRestriction, Base: baseName},
		{Variety: xsd.SimpleList, ItemType: stringType, InlineItem: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType}},
		{Variety: xsd.SimpleList, InlineItem: &invalid},
		{Variety: xsd.SimpleList, InlineItem: &xsd.SimpleType{Variety: xsd.SimpleList, ItemType: stringType}},
		{Variety: xsd.SimpleList, InlineItem: &xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleList, ItemType: stringType}}},
		{Variety: xsd.SimpleList, ItemType: xsd.QName{Namespace: "urn:test", Local: "NoList"}},
		{Variety: xsd.SimpleList, ItemType: listName},
		{Variety: xsd.SimpleList, ItemType: restrictedListName},
		{Variety: xsd.SimpleList, ItemType: xsd.QName{Namespace: xsd.Namespace, Local: "NOTATION"}},
		{Variety: xsd.SimpleUnion},
		{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{{Namespace: "urn:test", Local: "Missing"}}},
		{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{{Namespace: "urn:test", Local: "NoUnion"}}},
		{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{{Namespace: xsd.Namespace, Local: "NOTATION"}}},
		{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{invalid}},
		{},
	} {
		if err := state.validateSimpleTypeDefinition(definition); err == nil {
			t.Fatalf("validateSimpleTypeDefinition(%#v) succeeded", definition)
		}
	}
	for _, definition := range []xsd.SimpleType{
		{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{
			{Kind: xsd.FacetPattern, Value: "a"}, {Kind: xsd.FacetPattern, Value: "b"},
			{Kind: xsd.FacetEnumeration, Value: "a"}, {Kind: xsd.FacetEnumeration, Value: "b"},
		}},
		{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{
			{Kind: xsd.FacetTotalDigits, Value: "2"}, {Kind: xsd.FacetFractionDigits, Value: "1"},
			{Kind: xsd.FacetMinInclusive, Value: "1.0"}, {Kind: xsd.FacetMaxInclusive, Value: "9.9"},
		}},
		{Variety: xsd.SimpleRestriction, Base: listName, Facets: []xsd.Facet{{Kind: xsd.FacetWhiteSpace, Value: "collapse"}}},
		{Variety: xsd.SimpleRestriction, Base: unionName, Facets: []xsd.Facet{{Kind: xsd.FacetPattern, Value: "a"}}},
		{Variety: xsd.SimpleRestriction, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType}, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "a"}}},
	} {
		if err := state.validateSimpleTypeDefinition(definition); err != nil {
			t.Fatalf("validateSimpleTypeDefinition(%#v) error = %v", definition, err)
		}
	}
}

func TestFacetRestrictionHelperBoundaries(t *testing.T) {
	t.Parallel()

	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	broken := xsd.QName{Namespace: "urn:test", Local: "Broken"}
	state := compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		broken: {Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "invalid"}}},
	}}
	definition := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: broken}
	one, _ := datatype.ParseInteger("1")
	if err := state.validateFacetRestriction(definition, map[xsd.FacetKind]datatype.Integer{xsd.FacetLength: one}); err != nil {
		t.Fatalf("validateFacetRestriction(invalid ancestor) error = %v", err)
	}
	if _, ok := state.restrictionAncestorFacet(definition, xsd.FacetLength, defaultMaxDepth+1); ok {
		t.Fatal("restrictionAncestorFacet(over depth) succeeded")
	}
	if _, ok := state.restrictionAncestorFacet(xsd.SimpleType{Base: missing}, xsd.FacetLength, 0); ok {
		t.Fatal("restrictionAncestorFacet(missing) succeeded")
	}
	if state.restrictionBaseDerivesFromInteger(definition, defaultMaxDepth+1) {
		t.Fatal("restrictionBaseDerivesFromInteger(over depth) succeeded")
	}
	if !state.restrictionBaseDerivesFromInteger(xsd.SimpleType{InlineBase: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "integer"}}}, 0) {
		t.Fatal("inline integer base was not recognized")
	}
	if state.definitionDerivesFromInteger(xsd.SimpleType{Variety: xsd.SimpleList}, 0) {
		t.Fatal("list definition derived from integer")
	}
	if state.namedDerivesFromInteger(xsd.QName{Namespace: xsd.Namespace, Local: "unknown"}, 0) ||
		state.namedDerivesFromInteger(missing, 0) ||
		state.namedDerivesFromInteger(xsd.QName{Namespace: xsd.Namespace, Local: "integer"}, defaultMaxDepth+1) {
		t.Fatal("invalid integer derivation was accepted")
	}
}

func TestOrderedFacetRestrictionDecisionTables(t *testing.T) {
	t.Parallel()

	decimal := "decimal"
	minimum := xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "1"}
	exclusiveMinimum := xsd.Facet{Kind: xsd.FacetMinExclusive, Value: "1"}
	maximum := xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "10"}
	exclusiveMaximum := xsd.Facet{Kind: xsd.FacetMaxExclusive, Value: "10"}
	if !orderedLowerRestricts(decimal, xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "2"}, minimum) ||
		orderedLowerRestricts(decimal, minimum, exclusiveMinimum) ||
		orderedLowerRestricts("string", minimum, minimum) {
		t.Fatal("orderedLowerRestricts() decision table failed")
	}
	if !orderedUpperRestricts(decimal, xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "9"}, maximum) ||
		orderedUpperRestricts(decimal, maximum, exclusiveMaximum) ||
		orderedUpperRestricts("string", maximum, maximum) {
		t.Fatal("orderedUpperRestricts() decision table failed")
	}
	for _, test := range []struct {
		primitive   string
		left, right string
		want        int
		comparable  bool
	}{
		{primitive: "decimal", left: "invalid", right: "1"},
		{primitive: "float", left: "1", right: "2", want: -1, comparable: true},
		{primitive: "double", left: "2", right: "1", want: 1, comparable: true},
		{primitive: "double", left: "1", right: "1", comparable: true},
		{primitive: "double", left: "NaN", right: "1"},
	} {
		comparison, comparable := constraintOrderedCompare(test.primitive, test.left, test.right)
		if comparison != test.want || comparable != test.comparable {
			t.Fatalf("constraintOrderedCompare(%s, %q, %q) = %d, %t", test.primitive, test.left, test.right, comparison, comparable)
		}
	}

	rangeName := xsd.QName{Namespace: "urn:test", Local: "Range"}
	state := compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{rangeName: {
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: decimal},
		Facets:  []xsd.Facet{minimum, maximum},
	}}}
	for _, definition := range []xsd.SimpleType{
		{Variety: xsd.SimpleRestriction, Base: rangeName, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "0"}}},
		{Variety: xsd.SimpleRestriction, Base: rangeName, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "11"}}},
		{Variety: xsd.SimpleRestriction, Base: rangeName, Facets: []xsd.Facet{{Kind: xsd.FacetMaxInclusive, Value: "11"}}},
		{Variety: xsd.SimpleRestriction, Base: rangeName, Facets: []xsd.Facet{{Kind: xsd.FacetMaxInclusive, Value: "0"}}},
	} {
		if err := state.validateOrderedFacetRestriction(definition, decimal); err == nil {
			t.Fatalf("validateOrderedFacetRestriction(%#v) succeeded", definition)
		}
	}
	if _, ok := state.restrictionAncestorBound(xsd.SimpleType{}, true, defaultMaxDepth+1); ok {
		t.Fatal("restrictionAncestorBound(over depth) succeeded")
	}
	if _, ok := state.restrictionAncestorBound(xsd.SimpleType{Base: xsd.QName{Namespace: "urn:test", Local: "missing"}}, true, 0); ok {
		t.Fatal("restrictionAncestorBound(missing) succeeded")
	}
	if _, ok := state.restrictionAncestorBound(xsd.SimpleType{InlineBase: &xsd.SimpleType{Variety: xsd.SimpleList}}, true, 0); ok {
		t.Fatal("restrictionAncestorBound(inline list) succeeded")
	}
}

func simpleTypeFinalSets(t *testing.T) (xsd.DerivationSet, xsd.DerivationSet, xsd.DerivationSet) {
	t.Helper()

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`+
			`<xs:simpleType name="Restriction" final="restriction"><xs:restriction base="xs:string"/></xs:simpleType>`+
			`<xs:simpleType name="List" final="list"><xs:restriction base="xs:string"/></xs:simpleType>`+
			`<xs:simpleType name="Union" final="union"><xs:restriction base="xs:string"/></xs:simpleType>`+
			`</xs:schema>`,
	), xsd.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return document.SimpleTypes[0].Final, document.SimpleTypes[1].Final, document.SimpleTypes[2].Final
}

func TestFacetHelperBoundaryDecisions(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	name := xsd.QName{Namespace: "urn:test", Local: "Derived"}
	state := compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		name: {Variety: xsd.SimpleRestriction, Base: stringType},
	}}
	if got := state.definitionShape(xsd.SimpleType{}, 0); got != (simpleShape{}) {
		t.Fatalf("definitionShape(unknown) = %#v", got)
	}
	if got := state.definitionShape(xsd.SimpleType{}, defaultMaxDepth+1); got != (simpleShape{}) {
		t.Fatalf("definitionShape(over depth) = %#v", got)
	}
	if got := state.namedShape(xsd.QName{Namespace: "urn:test", Local: "missing"}, 0); got != (simpleShape{}) {
		t.Fatalf("namedShape(missing) = %#v", got)
	}
	if got := state.namedShape(name, defaultMaxDepth+1); got != (simpleShape{}) {
		t.Fatalf("namedShape(over depth) = %#v", got)
	}
	if got := state.namedShape(xsd.QName{Namespace: xsd.Namespace, Local: "unknown"}, 0); got != (simpleShape{variety: atomicShape, primitive: "unknown"}) {
		t.Fatalf("namedShape(unknown built-in) = %#v", got)
	}

	inlineList := xsd.SimpleType{Variety: xsd.SimpleList, ItemType: stringType}
	if value, fixed := state.restrictionBaseWhitespace(xsd.SimpleType{InlineBase: &inlineList}); value != "collapse" || !fixed {
		t.Fatalf("restrictionBaseWhitespace(inline list) = %q, %t", value, fixed)
	}
	inlineRestriction := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType}
	if value, fixed := state.definitionWhitespace(xsd.SimpleType{
		Variety: xsd.SimpleRestriction, InlineBase: &inlineRestriction,
	}, 0); value != "preserve" || fixed {
		t.Fatalf("definitionWhitespace(inline restriction) = %q, %t", value, fixed)
	}
	if value, fixed := state.definitionWhitespace(xsd.SimpleType{Variety: xsd.SimpleUnion}, 0); value != "" || fixed {
		t.Fatalf("definitionWhitespace(union) = %q, %t", value, fixed)
	}
	if value, fixed := state.definitionWhitespace(inlineRestriction, defaultMaxDepth+1); value != "" || fixed {
		t.Fatalf("definitionWhitespace(over depth) = %q, %t", value, fixed)
	}
	if value, fixed := state.namedWhitespace(xsd.QName{Namespace: "urn:test", Local: "missing"}, 0); value != "" || fixed {
		t.Fatalf("namedWhitespace(missing) = %q, %t", value, fixed)
	}
	if value, fixed := state.namedWhitespace(name, defaultMaxDepth+1); value != "" || fixed {
		t.Fatalf("namedWhitespace(over depth) = %q, %t", value, fixed)
	}
	if state.simpleConstraintValidDepth(stringType, "value", defaultMaxDepth+1) {
		t.Fatal("simpleConstraintValidDepth(over depth) succeeded")
	}
	if state.inlineConstraintValidDepth(inlineRestriction, "value", defaultMaxDepth+1) {
		t.Fatal("inlineConstraintValidDepth(over depth) succeeded")
	}
	if state.hasNotationEnumeration(xsd.SimpleType{}, defaultMaxDepth+1) {
		t.Fatal("hasNotationEnumeration(over depth) succeeded")
	}
	notationEnumeration := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "NOTATION"}, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration}}}
	if !state.hasNotationEnumeration(xsd.SimpleType{InlineBase: &notationEnumeration}, 0) {
		t.Fatal("hasNotationEnumeration(inline base) failed")
	}
	notationName := xsd.QName{Namespace: "urn:test", Local: "Notation"}
	state.simpleTypes[notationName] = notationEnumeration
	if !state.hasNotationEnumeration(xsd.SimpleType{Base: notationName}, 0) {
		t.Fatal("hasNotationEnumeration(named base) failed")
	}
	names := sortedComponentNames(map[xsd.QName]int{
		{Namespace: "urn:z", Local: "a"}: 1,
		{Namespace: "urn:a", Local: "z"}: 2,
	})
	if names[0].Namespace != "urn:a" {
		t.Fatalf("sortedComponentNames() = %#v", names)
	}
}

func TestValidateIdentityConstraintsRejectsEveryInvalidShape(t *testing.T) {
	t.Parallel()

	validKey := xsd.IdentityConstraint{
		Kind: xsd.IdentityKey, Name: "key", Selector: ".", Fields: []string{"@id"},
	}
	for _, test := range []struct {
		name  string
		state compileState
	}{
		{name: "incomplete", state: identityState(xsd.IdentityConstraint{Kind: xsd.IdentityKey})},
		{name: "duplicate", state: compileState{elements: map[xsd.QName]xsd.Element{
			{Namespace: "urn:test", Local: "first"}:  {IdentityConstraints: []xsd.IdentityConstraint{validKey}},
			{Namespace: "urn:test", Local: "second"}: {IdentityConstraints: []xsd.IdentityConstraint{validKey}},
		}}},
		{name: "invalid selector", state: identityState(xsd.IdentityConstraint{
			Kind: xsd.IdentityKey, Name: "key", Selector: "//", Fields: []string{"@id"},
		})},
		{name: "invalid field", state: identityState(xsd.IdentityConstraint{
			Kind: xsd.IdentityKey, Name: "key", Selector: ".", Fields: []string{"//"},
		})},
		{name: "keyref without refer", state: identityState(xsd.IdentityConstraint{
			Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"@id"},
		})},
		{name: "key with refer", state: identityState(xsd.IdentityConstraint{
			Kind: xsd.IdentityKey, Name: "key", Selector: ".", Fields: []string{"@id"},
			Refer: xsd.QName{Namespace: "urn:test", Local: "other"},
		})},
		{name: "nested element constraint", state: compileState{elements: map[xsd.QName]xsd.Element{
			{Namespace: "urn:test", Local: "root"}: {InlineComplexType: &xsd.ComplexType{
				Content: identityModel(xsd.IdentityConstraint{Kind: xsd.IdentityKey}),
			}},
		}}},
		{name: "complex type constraint", state: compileState{
			elements: map[xsd.QName]xsd.Element{},
			complexTypes: map[xsd.QName]xsd.ComplexType{
				{Namespace: "urn:test", Local: "Root"}: {
					Content: identityModel(xsd.IdentityConstraint{Kind: xsd.IdentityKey}),
				},
			},
		}},
		{name: "unresolved keyref", state: identityState(xsd.IdentityConstraint{
			Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"@id"},
			Refer: xsd.QName{Namespace: "urn:test", Local: "missing"},
		})},
		{name: "keyref to keyref", state: identityState(
			xsd.IdentityConstraint{Kind: xsd.IdentityKeyRef, Name: "first", Selector: ".", Fields: []string{"@id"}, Refer: xsd.QName{Namespace: "urn:test", Local: "second"}},
			xsd.IdentityConstraint{Kind: xsd.IdentityKeyRef, Name: "second", Selector: ".", Fields: []string{"@id"}, Refer: xsd.QName{Namespace: "urn:test", Local: "first"}},
		)},
		{name: "field count mismatch", state: identityState(
			validKey,
			xsd.IdentityConstraint{Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"@id", "@other"}, Refer: xsd.QName{Namespace: "urn:test", Local: "key"}},
		)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.state.validateIdentityConstraints(); err == nil {
				t.Fatal("validateIdentityConstraints() succeeded")
			}
		})
	}
}

func identityState(constraints ...xsd.IdentityConstraint) compileState {
	return compileState{elements: map[xsd.QName]xsd.Element{
		{Namespace: "urn:test", Local: "root"}: {IdentityConstraints: constraints},
	}}
}

func identityModel(constraint xsd.IdentityConstraint) *xsd.ModelGroup {
	return &xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{{
		Element: &xsd.Element{Name: "child", IdentityConstraints: []xsd.IdentityConstraint{constraint}},
	}}}
}
