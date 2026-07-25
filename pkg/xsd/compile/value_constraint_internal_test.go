package compile

import (
	"math"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestConstraintFacetValueDecisionTables(t *testing.T) {
	t.Parallel()

	listName := xsd.QName{Namespace: "urn:test", Local: "List"}
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		listName: {Variety: xsd.SimpleList, ItemType: xsd.QName{Namespace: xsd.Namespace, Local: "string"}},
	}}
	for _, test := range []struct {
		name       string
		definition xsd.SimpleType
		lexical    string
		want       bool
	}{
		{name: "list length", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: listName, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}}}, lexical: "a b", want: true},
		{name: "hex length", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "hexBinary"}, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "1"}}}, lexical: "0A", want: true},
		{name: "base64 length", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "base64Binary"}, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "1"}}}, lexical: "YQ==", want: true},
		{name: "maximum length mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, Facets: []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "1"}}}, lexical: "ab"},
		{name: "QName enumeration requires context", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "QName"}, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "p:item"}}}, lexical: "q:item"},
		{name: "calendar boundary deferred", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "date"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "2026-01-01"}}}, lexical: "2026-01-02", want: true},
	} {
		if got := state.restrictionConstraintFacetsValid(test.definition, test.lexical); got != test.want {
			t.Fatalf("restrictionConstraintFacetsValid(%s) = %t, want %t", test.name, got, test.want)
		}
	}

	for _, test := range []struct {
		primitive string
		lexical   string
		facet     xsd.Facet
		want      bool
	}{
		{primitive: "decimal", lexical: "invalid", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "0"}},
		{primitive: "decimal", lexical: "1.2", facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "1"}, want: true},
		{primitive: "float", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMinExclusive, Value: "0"}, want: true},
		{primitive: "double", lexical: "0", facet: xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "1"}, want: true},
		{primitive: "double", lexical: "2", facet: xsd.Facet{Kind: xsd.FacetMaxExclusive, Value: "1"}},
		{primitive: "double", lexical: "NaN", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "0"}},
		{primitive: "duration", lexical: "P2D", facet: xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "P1D"}},
		{primitive: "duration", lexical: "PT24H", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "P1D"}, want: true},
		{primitive: "date", lexical: "2025-12-31Z", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "2026-01-01Z"}},
		{primitive: "dateTime", lexical: "2026-01-01T00:00:00Z", facet: xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "2025-12-31T19:00:00-05:00"}, want: true},
	} {
		if got := constraintNumericFacetValid(test.primitive, test.lexical, test.facet); got != test.want {
			t.Fatalf("constraintNumericFacetValid(%s, %q, %#v) = %t, want %t", test.primitive, test.lexical, test.facet, got, test.want)
		}
	}
	if constraintComparisonValid(0, xsd.FacetPattern) {
		t.Fatal("constraintComparisonValid(pattern) = true")
	}

	for _, test := range []struct {
		primitive   string
		left, right string
		want        bool
	}{
		{primitive: "decimal", left: "1.0", right: "1", want: true},
		{primitive: "float", left: "NaN", right: "NaN", want: true},
		{primitive: "double", left: "INF", right: "INF", want: true},
		{primitive: "hexBinary", left: "0A", right: "0a", want: true},
		{primitive: "base64Binary", left: "YQ==", right: "YQ==", want: true},
		{primitive: "string", left: "same", right: "same", want: true},
	} {
		if got := constraintAtomicValuesEqual(test.primitive, test.left, test.right); got != test.want {
			t.Fatalf("constraintAtomicValuesEqual(%s, %q, %q) = %t", test.primitive, test.left, test.right, got)
		}
	}
	if value, ok := constraintFloat("-INF", 64); !ok || !math.IsInf(value, -1) {
		t.Fatalf("constraintFloat(-INF) = %v, %t", value, ok)
	}
}

func TestConstraintValueEqualityDecisionTables(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	integerType := xsd.QName{Namespace: xsd.Namespace, Local: "integer"}
	booleanType := xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}
	namedRestriction := xsd.QName{Namespace: "urn:test", Local: "Restricted"}
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		namedRestriction: {Variety: xsd.SimpleRestriction, Base: integerType},
	}}

	if state.restrictionConstraintFacetsValidContext(
		xsd.SimpleType{},
		"value",
		nil,
		defaultMaxDepth+1,
	) {
		t.Fatal("restrictionConstraintFacetsValidContext(over depth) succeeded")
	}
	inlineBase := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: booleanType}
	if !state.restrictionConstraintFacetsValidContext(xsd.SimpleType{
		Variety:    xsd.SimpleRestriction,
		InlineBase: &inlineBase,
		Facets:     []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "true"}},
	}, "1", nil, 0) {
		t.Fatal("inline-base enumeration did not use value equality")
	}
	if !constraintAtomicValuesEqual("duration", "P1D", "PT24H") {
		t.Fatal("duration values were not equal")
	}

	for _, lexical := range []string{"", "two words", "p:", "a:b:c"} {
		if _, ok := resolveConstraintQName(lexical, map[string]string{"p": "urn:test"}); ok {
			t.Fatalf("resolveConstraintQName(%q) succeeded", lexical)
		}
	}
	if name, ok := resolveConstraintQName("local", map[string]string{"": "urn:test"}); !ok || name != (xsd.QName{Namespace: "urn:test", Local: "local"}) {
		t.Fatalf("resolveConstraintQName(local) = %#v, %t", name, ok)
	}

	if state.simpleConstraintValuesEqualContext(
		stringType,
		"a",
		"a",
		nil,
		nil,
		defaultMaxDepth+1,
	) {
		t.Fatal("simpleConstraintValuesEqualContext(over depth) succeeded")
	}
	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	if state.simpleConstraintValuesEqualContext(missing, "a", "a", nil, nil, 0) {
		t.Fatal("missing named type values were equal")
	}
	for _, test := range []struct {
		left, right string
		want        bool
	}{
		{left: "a b", right: "a b", want: true},
		{left: "a", right: "a b"},
		{left: "a b", right: "a c"},
	} {
		if got := state.simpleConstraintValuesEqualContext(
			xsd.QName{Namespace: xsd.Namespace, Local: "NMTOKENS"},
			test.left,
			test.right,
			nil,
			nil,
			0,
		); got != test.want {
			t.Fatalf("NMTOKENS equality for %q and %q = %t", test.left, test.right, got)
		}
	}

	if state.inlineConstraintValuesEqualContext(
		xsd.SimpleType{},
		"a",
		"a",
		nil,
		nil,
		defaultMaxDepth+1,
	) {
		t.Fatal("inlineConstraintValuesEqualContext(over depth) succeeded")
	}
	restriction := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: integerType}
	if !state.inlineConstraintValuesEqualContext(restriction, "01", "1", nil, nil, 0) {
		t.Fatal("restriction values were not equal")
	}
	inlineRestriction := xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &restriction}
	if !state.inlineConstraintValuesEqualContext(inlineRestriction, "01", "1", nil, nil, 0) {
		t.Fatal("inline restriction values were not equal")
	}
	list := xsd.SimpleType{Variety: xsd.SimpleList, ItemType: integerType}
	if state.inlineConstraintValuesEqualContext(list, "1", "1 2", nil, nil, 0) ||
		state.inlineConstraintValuesEqualContext(list, "1", "2", nil, nil, 0) {
		t.Fatal("unequal list values were equal")
	}
	inlineList := xsd.SimpleType{Variety: xsd.SimpleList, InlineItem: &restriction}
	if !state.inlineConstraintValuesEqualContext(inlineList, "01", "1", nil, nil, 0) {
		t.Fatal("inline list item values were not equal")
	}

	namedUnion := xsd.SimpleType{
		Variety:     xsd.SimpleUnion,
		MemberTypes: []xsd.QName{{Namespace: xsd.Namespace, Local: "date"}, booleanType},
	}
	if !state.inlineConstraintValuesEqualContext(namedUnion, "true", "1", nil, nil, 0) {
		t.Fatal("named union values were not equal")
	}
	if state.inlineConstraintValuesEqualContext(namedUnion, "true", "2", nil, nil, 0) {
		t.Fatal("different named union member values were equal")
	}
	inlineUnion := xsd.SimpleType{
		Variety: xsd.SimpleUnion,
		InlineMembers: []xsd.SimpleType{
			{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "date"}},
			{Variety: xsd.SimpleRestriction, Base: booleanType},
		},
	}
	if !state.inlineConstraintValuesEqualContext(inlineUnion, "true", "1", nil, nil, 0) {
		t.Fatal("inline union values were not equal")
	}
	if state.inlineConstraintValuesEqualContext(inlineUnion, "true", "2", nil, nil, 0) {
		t.Fatal("different inline union member values were equal")
	}
	if state.inlineConstraintValuesEqualContext(xsd.SimpleType{}, "a", "a", nil, nil, 0) {
		t.Fatal("invalid simple variety values were equal")
	}

	if !state.restrictionBaseValueValid(
		xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: namedRestriction},
		"1",
	) {
		t.Fatal("restrictionBaseValueValid() rejected a valid value")
	}
	if err := (&compileState{}).validateNotationRestriction(xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: "NOTATION"},
		Facets:  []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "missing:item"}},
	}); err == nil {
		t.Fatal("unbound NOTATION enumeration succeeded")
	}
}

func TestInlineValueConstraintValidation(t *testing.T) {
	t.Parallel()

	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{}}
	decimalRestriction := xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: "decimal"},
	}
	patternRestriction := xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
		Facets:  []xsd.Facet{{Kind: xsd.FacetPattern, Value: `[A-Z]+`}},
	}
	for _, test := range []struct {
		name       string
		definition xsd.SimpleType
		lexical    string
		want       bool
	}{
		{name: "restriction", definition: decimalRestriction, lexical: "1.0", want: true},
		{name: "restriction rejects base lexical", definition: decimalRestriction, lexical: "one"},
		{name: "inline base", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &decimalRestriction}, lexical: "1.0", want: true},
		{name: "pattern", definition: patternRestriction, lexical: "ABC", want: true},
		{name: "minimum length", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "2"}}}, lexical: "AB", want: true},
		{name: "minimum length mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "2"}}}, lexical: "A"},
		{name: "enumeration", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "true"}}}, lexical: "1", want: true},
		{name: "enumeration mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "true"}}}, lexical: "false"},
		{name: "minimum inclusive", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "1"}}}, lexical: "1.0", want: true},
		{name: "minimum inclusive mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetMinInclusive, Value: "1"}}}, lexical: "0.9"},
		{name: "total digits mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "2"}}}, lexical: "123"},
		{name: "pattern mismatch", definition: patternRestriction, lexical: "abc"},
		{name: "invalid pattern", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, Facets: []xsd.Facet{{Kind: xsd.FacetPattern, Value: "["}}}, lexical: "x"},
		{name: "named list", definition: xsd.SimpleType{Variety: xsd.SimpleList, ItemType: xsd.QName{Namespace: xsd.Namespace, Local: "integer"}}, lexical: "1 2", want: true},
		{name: "empty list", definition: xsd.SimpleType{Variety: xsd.SimpleList, ItemType: xsd.QName{Namespace: xsd.Namespace, Local: "integer"}}},
		{name: "invalid named list item", definition: xsd.SimpleType{Variety: xsd.SimpleList, ItemType: xsd.QName{Namespace: xsd.Namespace, Local: "integer"}}, lexical: "1 two"},
		{name: "inline list", definition: xsd.SimpleType{Variety: xsd.SimpleList, InlineItem: &decimalRestriction}, lexical: "1.0 2.0", want: true},
		{name: "invalid inline list item", definition: xsd.SimpleType{Variety: xsd.SimpleList, InlineItem: &decimalRestriction}, lexical: "1.0 two"},
		{name: "named union", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{{Namespace: xsd.Namespace, Local: "boolean"}}}, lexical: "true", want: true},
		{name: "inline union", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{decimalRestriction}}, lexical: "1.0", want: true},
		{name: "union mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{{Namespace: xsd.Namespace, Local: "boolean"}}, InlineMembers: []xsd.SimpleType{decimalRestriction}}, lexical: "neither"},
		{name: "empty union", definition: xsd.SimpleType{Variety: xsd.SimpleUnion}, lexical: "value"},
		{name: "invalid variety", definition: xsd.SimpleType{Variety: "invalid"}, lexical: "value"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := state.inlineConstraintValid(test.definition, test.lexical); got != test.want {
				t.Fatalf("inlineConstraintValid() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestNamedValueConstraintValidation(t *testing.T) {
	t.Parallel()

	code := xsd.QName{Namespace: "urn:test", Local: "Code"}
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		code: {
			Variety: xsd.SimpleRestriction,
			Base:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
			Facets:  []xsd.Facet{{Kind: xsd.FacetPattern, Value: `[A-Z]+`}},
		},
	}}
	for _, test := range []struct {
		name     string
		typeName xsd.QName
		lexical  string
		want     bool
	}{
		{name: "any simple", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}, want: true},
		{name: "boolean", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}, lexical: "1", want: true},
		{name: "invalid boolean", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}, lexical: "yes"},
		{name: "decimal", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, lexical: "1.0", want: true},
		{name: "invalid decimal", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}, lexical: "one"},
		{name: "bounded integer", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "byte"}, lexical: "127", want: true},
		{name: "collapsed integer", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "integer"}, lexical: "  1\t", want: true},
		{name: "out of range integer", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "byte"}, lexical: "128"},
		{name: "built in lexical", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "date"}, lexical: "2026-07-19", want: true},
		{name: "invalid built in lexical", typeName: xsd.QName{Namespace: xsd.Namespace, Local: "date"}, lexical: "today"},
		{name: "named restriction", typeName: code, lexical: "ABC", want: true},
		{name: "named restriction mismatch", typeName: code, lexical: "abc"},
		{name: "missing named type", typeName: xsd.QName{Namespace: "urn:test", Local: "Missing"}, lexical: "value"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := state.simpleConstraintValid(test.typeName, test.lexical); got != test.want {
				t.Fatalf("simpleConstraintValid() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestValidateAttributeUseDecisionTable(t *testing.T) {
	t.Parallel()

	global := xsd.QName{Namespace: "urn:test", Local: "Global"}
	namedType := xsd.QName{Namespace: "urn:test", Local: "Code"}
	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	state := &compileState{
		attributes: map[xsd.QName]xsd.Attribute{global: {Name: "Global"}},
		simpleTypes: map[xsd.QName]xsd.SimpleType{
			namedType: {Variety: xsd.SimpleRestriction, Base: stringType},
		},
		typeKinds: map[xsd.QName]string{namedType: "simple"},
	}
	validInline := &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType}
	invalidInline := &xsd.SimpleType{Variety: xsd.SimpleList}
	for _, test := range []struct {
		name      string
		attribute xsd.AttributeUse
		wantError bool
	}{
		{name: "local untyped", attribute: xsd.AttributeUse{Name: "local"}},
		{name: "local named type", attribute: xsd.AttributeUse{Name: "local", Type: namedType}},
		{name: "local inline type", attribute: xsd.AttributeUse{Name: "local", InlineSimpleType: validInline}},
		{name: "global reference", attribute: xsd.AttributeUse{Ref: global}},
		{name: "neither name nor reference", wantError: true},
		{name: "both name and reference", attribute: xsd.AttributeUse{Name: "local", Ref: global}, wantError: true},
		{name: "reference with named type", attribute: xsd.AttributeUse{Ref: global, Type: stringType}, wantError: true},
		{name: "reference with inline type", attribute: xsd.AttributeUse{Ref: global, InlineSimpleType: validInline}, wantError: true},
		{name: "default and fixed", attribute: xsd.AttributeUse{Name: "local", Default: "a", Fixed: "b"}, wantError: true},
		{name: "default required", attribute: xsd.AttributeUse{Name: "local", Default: "a", Use: xsd.AttributeRequired}, wantError: true},
		{name: "missing reference", attribute: xsd.AttributeUse{Ref: xsd.QName{Namespace: "urn:test", Local: "Missing"}}, wantError: true},
		{name: "multiple type definitions", attribute: xsd.AttributeUse{Name: "local", Type: stringType, InlineSimpleType: validInline}, wantError: true},
		{name: "invalid inline type", attribute: xsd.AttributeUse{Name: "local", InlineSimpleType: invalidInline}, wantError: true},
		{name: "missing named type", attribute: xsd.AttributeUse{Name: "local", Type: xsd.QName{Namespace: "urn:test", Local: "Missing"}}, wantError: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := state.validateAttributeUse(test.attribute, "urn:test")
			if test.wantError != (err != nil) {
				t.Fatalf("validateAttributeUse() error = %v", err)
			}
		})
	}
}

func TestAttributeValueConstraintBoundaryBranches(t *testing.T) {
	t.Parallel()

	state := &compileState{
		attributes:  map[xsd.QName]xsd.Attribute{},
		simpleTypes: map[xsd.QName]xsd.SimpleType{},
	}
	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	if err := state.validateAttributeUseValueConstraint(xsd.AttributeUse{
		Ref: missing, Default: "value",
	}); err == nil {
		t.Fatal("missing referenced attribute value constraint succeeded")
	}
	if err := state.validateAttributeConstraint(xsd.QName{}, nil, ""); err != nil {
		t.Fatalf("untyped empty value constraint error = %v", err)
	}
	id := xsd.QName{Namespace: xsd.Namespace, Local: "ID"}
	if err := state.validateAttributeConstraint(id, nil, "identifier"); err == nil {
		t.Fatal("ID value constraint succeeded")
	}
	inlineID := &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: id}
	if err := state.validateAttributeConstraint(xsd.QName{}, inlineID, "identifier"); err == nil {
		t.Fatal("inline ID value constraint succeeded")
	}
	inlineBoolean := &xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: "boolean"},
	}
	if err := state.validateAttributeConstraint(xsd.QName{}, inlineBoolean, "true"); err != nil {
		t.Fatalf("inline boolean value constraint error = %v", err)
	}
	cycle := &xsd.SimpleType{Variety: xsd.SimpleRestriction}
	cycle.InlineBase = cycle
	if state.inlineSimpleTypeDerivesFrom(*cycle, id) {
		t.Fatal("cyclic inline type derived from ID")
	}
}

func TestValidateElementValueConstraintDecisionTable(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	booleanType := xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}
	idType := xsd.QName{Namespace: xsd.Namespace, Local: "ID"}
	mixedType := xsd.QName{Namespace: "urn:test", Local: "Mixed"}
	simpleContentType := xsd.QName{Namespace: "urn:test", Local: "SimpleContent"}
	state := &compileState{
		complexTypes: map[xsd.QName]xsd.ComplexType{
			mixedType:         {Mixed: true},
			simpleContentType: {SimpleContent: true, SimpleBase: booleanType},
		},
		typeKinds: map[xsd.QName]string{
			mixedType:         "complex",
			simpleContentType: "complex",
		},
		simpleTypes: map[xsd.QName]xsd.SimpleType{},
	}
	inlineBoolean := &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: booleanType}
	for _, test := range []struct {
		name      string
		element   xsd.Element
		wantError bool
	}{
		{name: "no constraint"},
		{name: "inline simple valid", element: xsd.Element{Default: "true", InlineSimpleType: inlineBoolean}},
		{name: "inline simple invalid", element: xsd.Element{Default: "invalid", InlineSimpleType: inlineBoolean}, wantError: true},
		{name: "inline simple content valid", element: xsd.Element{Default: "true", InlineComplexType: &xsd.ComplexType{SimpleContent: true, SimpleBase: booleanType}}},
		{name: "inline simple content invalid", element: xsd.Element{Default: "invalid", InlineComplexType: &xsd.ComplexType{SimpleContent: true, SimpleBase: booleanType}}, wantError: true},
		{name: "inline mixed", element: xsd.Element{Default: "anything", InlineComplexType: &xsd.ComplexType{Mixed: true}}},
		{name: "inline element only", element: xsd.Element{Default: "anything", InlineComplexType: &xsd.ComplexType{}}, wantError: true},
		{name: "untyped", element: xsd.Element{DefaultSet: true}},
		{name: "any type", element: xsd.Element{Default: "anything", Type: xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}}},
		{name: "ID type", element: xsd.Element{Default: "id", Type: idType}, wantError: true},
		{name: "simple valid", element: xsd.Element{Default: "true", Type: booleanType}},
		{name: "simple invalid", element: xsd.Element{Default: "invalid", Type: booleanType}, wantError: true},
		{name: "fixed overrides default", element: xsd.Element{Default: "invalid", Fixed: "true", Type: booleanType}},
		{name: "named mixed", element: xsd.Element{Default: "anything", Type: mixedType}},
		{name: "named simple content valid", element: xsd.Element{Default: "true", Type: simpleContentType}},
		{name: "named simple content invalid", element: xsd.Element{Default: "invalid", Type: simpleContentType}, wantError: true},
		{name: "missing type", element: xsd.Element{Default: "anything", Type: xsd.QName{Namespace: "urn:test", Local: "Missing"}}, wantError: true},
		{name: "string", element: xsd.Element{Default: "value", Type: stringType}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := state.validateElementValueConstraint(test.element)
			if test.wantError != (err != nil) {
				t.Fatalf("validateElementValueConstraint() error = %v", err)
			}
		})
	}
}
