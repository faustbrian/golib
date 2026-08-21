package compile

import (
	"math"
	"reflect"
	"strings"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestConstraintComparisonExhaustiveBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		kind       xsd.FacetKind
		comparison int
		want       bool
	}{
		{kind: xsd.FacetMinInclusive, comparison: -1},
		{kind: xsd.FacetMinInclusive, comparison: 0, want: true},
		{kind: xsd.FacetMinInclusive, comparison: 1, want: true},
		{kind: xsd.FacetMinExclusive, comparison: -1},
		{kind: xsd.FacetMinExclusive, comparison: 0},
		{kind: xsd.FacetMinExclusive, comparison: 1, want: true},
		{kind: xsd.FacetMaxInclusive, comparison: -1, want: true},
		{kind: xsd.FacetMaxInclusive, comparison: 0, want: true},
		{kind: xsd.FacetMaxInclusive, comparison: 1},
		{kind: xsd.FacetMaxExclusive, comparison: -1, want: true},
		{kind: xsd.FacetMaxExclusive, comparison: 0},
		{kind: xsd.FacetMaxExclusive, comparison: 1},
		{kind: xsd.FacetPattern, comparison: 0},
	} {
		if got := constraintComparisonValid(test.comparison, test.kind); got != test.want {
			t.Fatalf("constraintComparisonValid(%d, %s) = %t, want %t", test.comparison, test.kind, got, test.want)
		}
	}
}

func TestCompileDepthBoundary(t *testing.T) {
	t.Parallel()

	if compileDepthExceeded(defaultMaxDepth) {
		t.Fatal("the maximum compile depth was rejected")
	}
	if !compileDepthExceeded(defaultMaxDepth + 1) {
		t.Fatal("a compile depth beyond the maximum was accepted")
	}
	if got := compileChildDepth(defaultMaxDepth); got != defaultMaxDepth+1 {
		t.Fatalf("compileChildDepth(%d) = %d, want %d", defaultMaxDepth, got, defaultMaxDepth+1)
	}
}

func TestCompilerLimitDefaultsAndNegativeValues(t *testing.T) {
	t.Parallel()

	compiler, err := New(Options{})
	if err != nil {
		t.Fatalf("New(defaults) error = %v", err)
	}
	wantDefaults := Limits{
		MaxSchemas:    defaultMaxSchemas,
		MaxDepth:      defaultMaxDepth,
		MaxReferences: defaultMaxReferences,
		MaxBytes:      defaultMaxBytes,
		MaxComponents: defaultMaxComponents,
		MaxParticles:  defaultMaxParticles,
	}
	if !reflect.DeepEqual(compiler.limits, wantDefaults) {
		t.Fatalf("New(defaults) limits = %#v, want %#v", compiler.limits, wantDefaults)
	}

	for name, limits := range map[string]Limits{
		"schemas":    {MaxSchemas: -1},
		"depth":      {MaxDepth: -1},
		"references": {MaxReferences: -1},
		"bytes":      {MaxBytes: -1},
		"components": {MaxComponents: -1},
		"particles":  {MaxParticles: -1},
	} {
		if _, err := New(Options{Limits: limits}); err == nil || err.Error() != "xsd compile: limits must not be negative" {
			t.Fatalf("New(negative %s) error = %v", name, err)
		}
	}
}

func TestDeterministicOrderingComparators(t *testing.T) {
	t.Parallel()

	nameA := xsd.QName{Namespace: "urn:a", Local: "a"}
	nameB := xsd.QName{Namespace: "urn:a", Local: "b"}
	nameC := xsd.QName{Namespace: "urn:b", Local: "a"}
	for _, test := range []struct {
		left, right xsd.QName
		want        bool
	}{
		{left: nameA, right: nameB, want: true},
		{left: nameB, right: nameA},
		{left: nameB, right: nameC, want: true},
		{left: nameC, right: nameB},
		{left: nameA, right: nameA},
	} {
		if got := expandedNameLess(test.left, test.right); got != test.want {
			t.Fatalf("expandedNameLess(%v, %v) = %t, want %t", test.left, test.right, got, test.want)
		}
	}

	documentA := Document{URI: "a", Namespace: "a"}
	documentB := Document{URI: "a", Namespace: "b"}
	documentC := Document{URI: "b", Namespace: "a"}
	for _, test := range []struct {
		left, right Document
		want        bool
	}{
		{left: documentA, right: documentB, want: true},
		{left: documentB, right: documentA},
		{left: documentB, right: documentC, want: true},
		{left: documentC, right: documentB},
		{left: documentA, right: documentA},
	} {
		if got := documentLess(test.left, test.right); got != test.want {
			t.Fatalf("documentLess(%v, %v) = %t, want %t", test.left, test.right, got, test.want)
		}
	}
}

func TestConstraintNumericFacetsExhaustiveBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		primitive string
		lexical   string
		facet     xsd.Facet
		want      bool
	}{
		{name: "total digits exact", primitive: "decimal", lexical: "12", facet: xsd.Facet{Kind: xsd.FacetTotalDigits, Value: "2"}, want: true},
		{name: "total digits exceeded", primitive: "decimal", lexical: "123", facet: xsd.Facet{Kind: xsd.FacetTotalDigits, Value: "2"}},
		{name: "total digits zero", primitive: "decimal", lexical: "0", facet: xsd.Facet{Kind: xsd.FacetTotalDigits, Value: "0"}},
		{name: "total digits invalid", primitive: "decimal", lexical: "0", facet: xsd.Facet{Kind: xsd.FacetTotalDigits, Value: "x"}},
		{name: "fraction digits exact", primitive: "decimal", lexical: "1.23", facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "2"}, want: true},
		{name: "fraction digits exceeded", primitive: "decimal", lexical: "1.23", facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "1"}},
		{name: "fraction digits zero", primitive: "decimal", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "0"}, want: true},
		{name: "fraction digits invalid", primitive: "decimal", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "x"}},
		{name: "fraction digits negative", primitive: "decimal", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "-1"}},
		{name: "decimal equal inclusive", primitive: "decimal", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "1"}, want: true},
		{name: "decimal equal exclusive", primitive: "decimal", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMinExclusive, Value: "1"}},
		{name: "decimal invalid boundary", primitive: "decimal", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "x"}},
		{name: "float less", primitive: "float", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMaxExclusive, Value: "2"}, want: true},
		{name: "float equal", primitive: "float", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "1"}, want: true},
		{name: "float equal minimum", primitive: "float", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "1"}, want: true},
		{name: "float greater", primitive: "float", lexical: "2", facet: xsd.Facet{Kind: xsd.FacetMinExclusive, Value: "1"}, want: true},
		{name: "float invalid value", primitive: "float", lexical: "bad", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "1"}},
		{name: "float invalid boundary", primitive: "float", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "bad"}},
		{name: "float NaN boundary", primitive: "float", lexical: "1", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "NaN"}},
		{name: "ordered incomparable", primitive: "string", lexical: "a", facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "b"}},
		{name: "float32 precision collapse", primitive: "float", lexical: "16777217", facet: xsd.Facet{Kind: xsd.FacetMinExclusive, Value: "16777216"}},
		{name: "float64 precision preserved", primitive: "double", lexical: "16777217", facet: xsd.Facet{Kind: xsd.FacetMinExclusive, Value: "16777216"}, want: true},
	} {
		if got := constraintNumericFacetValid(test.primitive, test.lexical, test.facet); got != test.want {
			t.Fatalf("%s: constraintNumericFacetValid() = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestConstraintAtomicEqualityExhaustiveBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		primitive   string
		left, right string
		want        bool
	}{
		{primitive: "boolean", left: "true", right: "1", want: true},
		{primitive: "boolean", left: "false", right: "0", want: true},
		{primitive: "boolean", left: "true", right: "false"},
		{primitive: "decimal", left: "1.0", right: "1", want: true},
		{primitive: "decimal", left: "1", right: "2"},
		{primitive: "decimal", left: "bad", right: "1"},
		{primitive: "decimal", left: "1", right: "bad"},
		{primitive: "float", left: "NaN", right: "NaN", want: true},
		{primitive: "float", left: "NaN", right: "1"},
		{primitive: "float", left: "bad", right: "1"},
		{primitive: "float", left: "1", right: "bad"},
		{primitive: "float", left: "1", right: "2"},
		{primitive: "float", left: "16777217", right: "16777216", want: true},
		{primitive: "double", left: "16777217", right: "16777216"},
		{primitive: "hexBinary", left: "0A", right: "0a", want: true},
		{primitive: "hexBinary", left: "0A", right: "0B"},
		{primitive: "hexBinary", left: "x", right: "0A"},
		{primitive: "hexBinary", left: "0A", right: "x"},
		{primitive: "base64Binary", left: "YQ==", right: "YQ==", want: true},
		{primitive: "base64Binary", left: "YQ==", right: "Yg=="},
		{primitive: "base64Binary", left: "x", right: "YQ=="},
		{primitive: "base64Binary", left: "YQ==", right: "x"},
		{primitive: "date", left: "2026-01-01Z", right: "2026-01-01Z", want: true},
		{primitive: "date", left: "2026-01-01Z", right: "2026-01-02Z"},
		{primitive: "string", left: "same", right: "same", want: true},
		{primitive: "string", left: "left", right: "right"},
	} {
		if got := constraintAtomicValuesEqual(test.primitive, test.left, test.right); got != test.want {
			t.Fatalf("constraintAtomicValuesEqual(%s, %q, %q) = %t, want %t", test.primitive, test.left, test.right, got, test.want)
		}
	}
	if value, ok := constraintFloat("INF", 64); !ok || !math.IsInf(value, 1) {
		t.Fatalf("constraintFloat(INF) = %v, %t", value, ok)
	}
	if value, ok := constraintFloat("1.5", 32); !ok || value != 1.5 {
		t.Fatalf("constraintFloat(1.5) = %v, %t", value, ok)
	}
	if _, ok := constraintFloat("invalid", 64); ok {
		t.Fatal("constraintFloat(invalid) succeeded")
	}
}

func TestRestrictionFacetRelationsAtEveryBoundary(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	decimalType := xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}
	integerType := xsd.QName{Namespace: xsd.Namespace, Local: "integer"}
	state := &compileState{}
	for _, test := range []struct {
		name    string
		base    xsd.QName
		facets  []xsd.Facet
		wantErr string
	}{
		{name: "zero length", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "0"}}},
		{name: "negative length", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "-1"}}, wantErr: "invalid value"},
		{name: "one total digit", base: decimalType, facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "1"}}},
		{name: "zero total digits", base: decimalType, facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "0"}}, wantErr: "invalid value"},
		{name: "equal min and max length", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "2"}, {Kind: xsd.FacetMaxLength, Value: "2"}}},
		{name: "min exceeds max length", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "3"}, {Kind: xsd.FacetMaxLength, Value: "2"}}, wantErr: "minLength exceeds maxLength"},
		{name: "min equals length", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}, {Kind: xsd.FacetMinLength, Value: "2"}}},
		{name: "min exceeds length", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}, {Kind: xsd.FacetMinLength, Value: "3"}}, wantErr: "minLength exceeds length"},
		{name: "length equals max", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}, {Kind: xsd.FacetMaxLength, Value: "2"}}},
		{name: "length exceeds max", base: stringType, facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "3"}, {Kind: xsd.FacetMaxLength, Value: "2"}}, wantErr: "length exceeds maxLength"},
		{name: "fraction equals total", base: decimalType, facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "2"}, {Kind: xsd.FacetFractionDigits, Value: "2"}}},
		{name: "fraction exceeds total", base: decimalType, facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "2"}, {Kind: xsd.FacetFractionDigits, Value: "3"}}, wantErr: "fractionDigits exceeds totalDigits"},
		{name: "integer fraction zero", base: integerType, facets: []xsd.Facet{{Kind: xsd.FacetFractionDigits, Value: "0"}}},
		{name: "integer fraction nonzero", base: integerType, facets: []xsd.Facet{{Kind: xsd.FacetFractionDigits, Value: "1"}}, wantErr: "must be zero"},
	} {
		err := state.validateRestrictionFacets(xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: test.base, Facets: test.facets})
		if test.wantErr == "" && err != nil {
			t.Fatalf("%s: validateRestrictionFacets() error = %v", test.name, err)
		}
		if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
			t.Fatalf("%s: validateRestrictionFacets() error = %v, want %q", test.name, err, test.wantErr)
		}
	}
}

func TestFacetRecursionStopsAtTheExactDepthBoundary(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	integerType := xsd.QName{Namespace: xsd.Namespace, Local: "integer"}
	decimalType := xsd.QName{Namespace: xsd.Namespace, Local: "decimal"}
	namedString := xsd.QName{Namespace: "urn:test", Local: "String"}
	namedBound := xsd.QName{Namespace: "urn:test", Local: "Bound"}
	namedInteger := xsd.QName{Namespace: "urn:test", Local: "Integer"}
	namedNotation := xsd.QName{Namespace: "urn:test", Local: "Notation"}
	boundFacet := xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "1"}
	lengthFacet := xsd.Facet{Kind: xsd.FacetLength, Value: "1"}
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		namedString:   {Variety: xsd.SimpleRestriction, Base: stringType},
		namedBound:    {Variety: xsd.SimpleRestriction, Base: decimalType, Facets: []xsd.Facet{boundFacet, lengthFacet}},
		namedInteger:  {Variety: xsd.SimpleRestriction, Base: integerType},
		namedNotation: {Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "NOTATION"}, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "n"}}},
	}}
	inlineString := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType}
	inlineBound := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: namedBound}

	if state.restrictionConstraintFacetsValidContext(xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "a"}}}, "a", nil, defaultMaxDepth) {
		t.Fatal("enumeration comparison crossed the maximum depth")
	}
	if state.restrictionConstraintFacetsValidContext(xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &inlineString, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "a"}}}, "a", nil, defaultMaxDepth) {
		t.Fatal("inline enumeration comparison crossed the maximum depth")
	}
	if state.simpleConstraintValuesEqualContext(namedString, "a", "a", nil, nil, defaultMaxDepth) {
		t.Fatal("named equality crossed the maximum depth")
	}
	if state.inlineConstraintValuesEqualContext(inlineString, "a", "a", nil, nil, defaultMaxDepth) {
		t.Fatal("inline equality crossed the maximum depth")
	}
	if state.inlineConstraintValuesEqualContext(xsd.SimpleType{Variety: xsd.SimpleList, ItemType: stringType}, "a", "a", nil, nil, defaultMaxDepth) {
		t.Fatal("list equality crossed the maximum depth")
	}
	if state.inlineConstraintValuesEqualContext(xsd.SimpleType{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{stringType}}, "a", "a", nil, nil, defaultMaxDepth) {
		t.Fatal("named union equality crossed the maximum depth")
	}
	if state.inlineConstraintValuesEqualContext(xsd.SimpleType{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{inlineString}}, "a", "a", nil, nil, defaultMaxDepth) {
		t.Fatal("inline union equality crossed the maximum depth")
	}
	if _, ok := state.restrictionAncestorBound(xsd.SimpleType{Base: namedBound}, true, defaultMaxDepth); ok {
		t.Fatal("named bound lookup crossed the maximum depth")
	}
	if _, ok := state.restrictionAncestorBound(xsd.SimpleType{InlineBase: &inlineBound}, true, defaultMaxDepth); ok {
		t.Fatal("inline bound lookup crossed the maximum depth")
	}
	if _, ok := state.definitionBound(xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: namedBound}, true, defaultMaxDepth); ok {
		t.Fatal("inherited bound lookup crossed the maximum depth")
	}
	if _, ok := state.restrictionAncestorFacet(xsd.SimpleType{Base: namedBound}, xsd.FacetLength, defaultMaxDepth); ok {
		t.Fatal("named facet lookup crossed the maximum depth")
	}
	if _, ok := state.definitionFacet(xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: namedBound}, xsd.FacetLength, defaultMaxDepth); ok {
		t.Fatal("inherited facet lookup crossed the maximum depth")
	}
	if state.restrictionBaseDerivesFromInteger(xsd.SimpleType{Base: namedInteger}, defaultMaxDepth) {
		t.Fatal("integer derivation crossed the maximum depth")
	}
	if state.definitionDerivesFromInteger(xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: namedInteger}, defaultMaxDepth) {
		t.Fatal("definition derivation crossed the maximum depth")
	}
	if state.namedDerivesFromInteger(namedInteger, defaultMaxDepth) {
		t.Fatal("named integer derivation crossed the maximum depth")
	}
	if state.hasNotationEnumeration(xsd.SimpleType{Base: namedNotation}, defaultMaxDepth) {
		t.Fatal("notation lookup crossed the maximum depth")
	}
	if got := state.definitionShape(xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: namedString}, defaultMaxDepth); got != (simpleShape{}) {
		t.Fatalf("definitionShape() crossed the maximum depth: %#v", got)
	}
	if value, fixed := state.definitionWhitespace(xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: namedString}, defaultMaxDepth); value != "" || fixed {
		t.Fatalf("definitionWhitespace() crossed the maximum depth: %q, %t", value, fixed)
	}
}

func TestQNameAndUnionEqualityRejectOneSidedValidity(t *testing.T) {
	t.Parallel()

	qnameType := xsd.QName{Namespace: xsd.Namespace, Local: "QName"}
	integerType := xsd.QName{Namespace: xsd.Namespace, Local: "integer"}
	state := &compileState{}
	namespaces := map[string]string{"p": "urn:test"}
	for _, test := range []struct {
		left, right string
	}{
		{left: "missing:value", right: "p:value"},
		{left: "p:value", right: "missing:value"},
	} {
		if state.simpleConstraintValuesEqualContext(qnameType, test.left, test.right, namespaces, namespaces, 0) {
			t.Fatalf("QName equality accepted %q and %q", test.left, test.right)
		}
	}

	for _, definition := range []xsd.SimpleType{
		{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{integerType}},
		{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{{Variety: xsd.SimpleRestriction, Base: integerType}}},
	} {
		if state.inlineConstraintValuesEqualContext(definition, "1", "invalid", nil, nil, 0) {
			t.Fatal("union equality accepted an invalid right value")
		}
		if state.inlineConstraintValuesEqualContext(definition, "invalid", "1", nil, nil, 0) {
			t.Fatal("union equality accepted an invalid left value")
		}
		if state.inlineConstraintValuesEqualContext(definition, "invalid", "also-invalid", nil, nil, 0) {
			t.Fatal("union equality accepted two invalid values")
		}
	}

	for _, definition := range []xsd.SimpleType{
		{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{integerType, qnameType}},
		{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{
			{Variety: xsd.SimpleRestriction, Base: integerType},
			{Variety: xsd.SimpleRestriction, Base: qnameType},
		}},
	} {
		if !state.inlineConstraintValuesEqualContext(definition, "p:value", "p:value", namespaces, namespaces, 0) {
			t.Fatal("union equality stopped before a later matching member")
		}
	}
}

func TestConstraintValidityPairDistinguishesEveryState(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		left, right bool
		want        constraintValidity
	}{
		{want: noConstraintsValid},
		{right: true, want: oneConstraintValid},
		{left: true, want: oneConstraintValid},
		{left: true, right: true, want: bothConstraintsValid},
	} {
		if got := constraintValidityPair(test.left, test.right); got != test.want {
			t.Fatalf("constraintValidityPair(%t, %t) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestIntegerFacetRestrictionDirections(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		kind       xsd.FacetKind
		derived    string
		fixed      bool
		wantReject bool
	}{
		{name: "length equal", kind: xsd.FacetLength, derived: "2"},
		{name: "length lower", kind: xsd.FacetLength, derived: "1", wantReject: true},
		{name: "length higher", kind: xsd.FacetLength, derived: "3", wantReject: true},
		{name: "minimum equal", kind: xsd.FacetMinLength, derived: "2"},
		{name: "minimum lower", kind: xsd.FacetMinLength, derived: "1", wantReject: true},
		{name: "minimum higher", kind: xsd.FacetMinLength, derived: "3"},
		{name: "maximum equal", kind: xsd.FacetMaxLength, derived: "2"},
		{name: "maximum lower", kind: xsd.FacetMaxLength, derived: "1"},
		{name: "maximum higher", kind: xsd.FacetMaxLength, derived: "3", wantReject: true},
		{name: "total equal", kind: xsd.FacetTotalDigits, derived: "2"},
		{name: "total lower", kind: xsd.FacetTotalDigits, derived: "1"},
		{name: "total higher", kind: xsd.FacetTotalDigits, derived: "3", wantReject: true},
		{name: "fraction equal", kind: xsd.FacetFractionDigits, derived: "2"},
		{name: "fraction lower", kind: xsd.FacetFractionDigits, derived: "1"},
		{name: "fraction higher", kind: xsd.FacetFractionDigits, derived: "3", wantReject: true},
		{name: "fixed lower", kind: xsd.FacetMinLength, derived: "1", fixed: true, wantReject: true},
		{name: "fixed higher", kind: xsd.FacetMinLength, derived: "3", fixed: true, wantReject: true},
	} {
		baseName := xsd.QName{Namespace: "urn:test", Local: test.name}
		state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
			baseName: {
				Variety: xsd.SimpleRestriction,
				Base:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
				Facets:  []xsd.Facet{{Kind: test.kind, Value: "2", Fixed: test.fixed}},
			},
		}}
		err := state.validateFacetRestriction(
			xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: baseName},
			map[xsd.FacetKind]datatype.Integer{test.kind: mustFacetInteger(t, test.derived)},
		)
		if test.wantReject && (err == nil || !strings.Contains(err.Error(), "does not restrict")) {
			t.Fatalf("%s: validateFacetRestriction() error = %v", test.name, err)
		}
		if !test.wantReject && err != nil {
			t.Fatalf("%s: validateFacetRestriction() error = %v", test.name, err)
		}
	}
}

func mustFacetInteger(t *testing.T, lexical string) datatype.Integer {
	t.Helper()

	value, err := datatype.ParseInteger(lexical)
	if err != nil {
		t.Fatalf("ParseInteger(%q) error = %v", lexical, err)
	}
	return value
}

func TestConstraintFacetLengthsAndEnumerationBoundaries(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	booleanType := xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}
	inlineBoolean := xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: booleanType}
	state := &compileState{}
	for _, test := range []struct {
		name       string
		definition xsd.SimpleType
		lexical    string
		want       bool
	}{
		{name: "length exact", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}}}, lexical: "ab", want: true},
		{name: "length low", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}}}, lexical: "a"},
		{name: "minimum exact", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "2"}}}, lexical: "ab", want: true},
		{name: "minimum low", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMinLength, Value: "2"}}}, lexical: "a"},
		{name: "maximum exact", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "2"}}}, lexical: "ab", want: true},
		{name: "maximum high", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetMaxLength, Value: "2"}}}, lexical: "abc"},
		{name: "invalid length", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "x"}}}, lexical: "a"},
		{name: "named enumeration match", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: booleanType, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "true"}}}, lexical: "1", want: true},
		{name: "named enumeration mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: booleanType, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "true"}}}, lexical: "false"},
		{name: "inline enumeration match", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &inlineBoolean, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "true"}}}, lexical: "1", want: true},
		{name: "multiple enumeration later match", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: stringType, Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "a"}, {Kind: xsd.FacetEnumeration, Value: "b"}}}, lexical: "b", want: true},
	} {
		if got := state.restrictionConstraintFacetsValid(test.definition, test.lexical); got != test.want {
			t.Fatalf("%s: restrictionConstraintFacetsValid() = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestOrderedFacetHelpersExhaustiveBoundaries(t *testing.T) {
	t.Parallel()

	inclusiveMin := xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "1"}
	exclusiveMin := xsd.Facet{Kind: xsd.FacetMinExclusive, Value: "1"}
	inclusiveMax := xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "1"}
	exclusiveMax := xsd.Facet{Kind: xsd.FacetMaxExclusive, Value: "1"}
	for _, test := range []struct {
		minimum, maximum xsd.Facet
		want             bool
	}{
		{minimum: inclusiveMin, maximum: inclusiveMax, want: true},
		{minimum: exclusiveMin, maximum: exclusiveMax, want: true},
		{minimum: inclusiveMin, maximum: exclusiveMax},
		{minimum: exclusiveMin, maximum: inclusiveMax},
		{minimum: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "0"}, maximum: inclusiveMax, want: true},
		{minimum: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "2"}, maximum: inclusiveMax},
	} {
		if got := orderedIntervalValid("decimal", test.minimum, test.maximum); got != test.want {
			t.Fatalf("orderedIntervalValid(%#v, %#v) = %t, want %t", test.minimum, test.maximum, got, test.want)
		}
	}
	for _, test := range []struct {
		derived, base xsd.Facet
		lower         bool
		want          bool
	}{
		{derived: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "2"}, base: inclusiveMin, lower: true, want: true},
		{derived: inclusiveMin, base: inclusiveMin, lower: true, want: true},
		{derived: inclusiveMin, base: exclusiveMin, lower: true},
		{derived: exclusiveMin, base: exclusiveMin, lower: true, want: true},
		{derived: xsd.Facet{Kind: xsd.FacetMaxInclusive, Value: "0"}, base: inclusiveMax, want: true},
		{derived: inclusiveMax, base: inclusiveMax, want: true},
		{derived: inclusiveMax, base: exclusiveMax},
		{derived: exclusiveMax, base: exclusiveMax, want: true},
	} {
		var got bool
		if test.lower {
			got = orderedLowerRestricts("decimal", test.derived, test.base)
		} else {
			got = orderedUpperRestricts("decimal", test.derived, test.base)
		}
		if got != test.want {
			t.Fatalf("ordered restriction (%#v, %#v) = %t, want %t", test.derived, test.base, got, test.want)
		}
	}

	if comparison, ok := constraintOrderedCompare("float", "16777217", "16777216"); !ok || comparison != 0 {
		t.Fatalf("float comparison = %d, %t, want 0, true", comparison, ok)
	}
	if comparison, ok := constraintOrderedCompare("double", "16777217", "16777216"); !ok || comparison != 1 {
		t.Fatalf("double comparison = %d, %t, want 1, true", comparison, ok)
	}
	for _, test := range []struct {
		primitive string
		left      string
		right     string
	}{
		{primitive: "decimal", left: "1", right: "invalid"},
		{primitive: "float", left: "invalid", right: "1"},
		{primitive: "float", left: "1", right: "invalid"},
		{primitive: "float", left: "NaN", right: "1"},
		{primitive: "float", left: "1", right: "NaN"},
	} {
		if _, ok := constraintOrderedCompare(test.primitive, test.left, test.right); ok {
			t.Fatalf("constraintOrderedCompare(%q, %q, %q) succeeded", test.primitive, test.left, test.right)
		}
	}
}

func TestOrderedFacetValidationContinuesToLaterFixedFacets(t *testing.T) {
	t.Parallel()

	baseName := xsd.QName{Namespace: "urn:test", Local: "Base"}
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		baseName: {
			Variety: xsd.SimpleRestriction,
			Base:    xsd.QName{Namespace: xsd.Namespace, Local: "decimal"},
			Facets: []xsd.Facet{
				{Kind: xsd.FacetMinInclusive, Value: "0"},
				{Kind: xsd.FacetMaxInclusive, Value: "10", Fixed: true},
			},
		},
	}}

	for _, facets := range [][]xsd.Facet{
		{
			{Kind: xsd.FacetPattern, Value: ".*"},
			{Kind: xsd.FacetMaxInclusive, Value: "9"},
		},
		{
			{Kind: xsd.FacetMinInclusive, Value: "1"},
			{Kind: xsd.FacetMaxInclusive, Value: "9"},
		},
	} {
		err := state.validateOrderedFacetRestriction(xsd.SimpleType{
			Variety: xsd.SimpleRestriction,
			Base:    baseName,
			Facets:  facets,
		}, "decimal")
		if err == nil || !strings.Contains(err.Error(), "fixed facet maxInclusive was changed") {
			t.Fatalf("validateOrderedFacetRestriction() error = %v", err)
		}
	}
}

func TestProcessContentsStrengthOrdering(t *testing.T) {
	t.Parallel()

	values := []xsd.ProcessContents{xsd.ProcessSkip, xsd.ProcessLax, xsd.ProcessStrict}
	for leftIndex, left := range values {
		for rightIndex, right := range values {
			strongIndex := leftIndex
			if rightIndex > strongIndex {
				strongIndex = rightIndex
			}
			weakIndex := leftIndex
			if rightIndex < weakIndex {
				weakIndex = rightIndex
			}
			if got := strongerProcessContents(left, right); got != values[strongIndex] {
				t.Fatalf("strongerProcessContents(%q, %q) = %q, want %q", left, right, got, values[strongIndex])
			}
			if got := weakerProcessContents(left, right); got != values[weakIndex] {
				t.Fatalf("weakerProcessContents(%q, %q) = %q, want %q", left, right, got, values[weakIndex])
			}
		}
	}
}

func TestFacetApplicabilityCompleteMatrix(t *testing.T) {
	t.Parallel()

	allKinds := []xsd.FacetKind{
		xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength, xsd.FacetPattern,
		xsd.FacetEnumeration, xsd.FacetWhiteSpace, xsd.FacetTotalDigits,
		xsd.FacetFractionDigits, xsd.FacetMinInclusive, xsd.FacetMinExclusive,
		xsd.FacetMaxInclusive, xsd.FacetMaxExclusive,
	}
	allowed := func(kinds ...xsd.FacetKind) map[xsd.FacetKind]bool {
		result := make(map[xsd.FacetKind]bool, len(kinds))
		for _, kind := range kinds {
			result[kind] = true
		}
		return result
	}
	for _, test := range []struct {
		shape simpleShape
		allow map[xsd.FacetKind]bool
	}{
		{shape: simpleShape{variety: listShape}, allow: allowed(xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength, xsd.FacetPattern, xsd.FacetEnumeration, xsd.FacetWhiteSpace)},
		{shape: simpleShape{variety: unionShape}, allow: allowed(xsd.FacetPattern, xsd.FacetEnumeration)},
		{shape: simpleShape{variety: atomicShape, primitive: "string"}, allow: allowed(xsd.FacetLength, xsd.FacetMinLength, xsd.FacetMaxLength, xsd.FacetPattern, xsd.FacetEnumeration, xsd.FacetWhiteSpace)},
		{shape: simpleShape{variety: atomicShape, primitive: "decimal"}, allow: allowed(xsd.FacetPattern, xsd.FacetEnumeration, xsd.FacetWhiteSpace, xsd.FacetTotalDigits, xsd.FacetFractionDigits, xsd.FacetMinInclusive, xsd.FacetMinExclusive, xsd.FacetMaxInclusive, xsd.FacetMaxExclusive)},
		{shape: simpleShape{variety: atomicShape, primitive: "date"}, allow: allowed(xsd.FacetPattern, xsd.FacetEnumeration, xsd.FacetWhiteSpace, xsd.FacetMinInclusive, xsd.FacetMinExclusive, xsd.FacetMaxInclusive, xsd.FacetMaxExclusive)},
		{shape: simpleShape{variety: "invalid"}, allow: allowed()},
	} {
		for _, kind := range allKinds {
			if got := facetApplicable(test.shape, kind); got != test.allow[kind] {
				t.Fatalf("facetApplicable(%#v, %s) = %t, want %t", test.shape, kind, got, test.allow[kind])
			}
		}
	}
}

func TestWhitespaceFacetSemantics(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		"preserve": true, "replace": true, "collapse": true, "": false, "invalid": false,
	} {
		if got := validWhitespaceValue(value); got != want {
			t.Fatalf("validWhitespaceValue(%q) = %t, want %t", value, got, want)
		}
	}
	if whitespaceRank("preserve") != 0 || whitespaceRank("replace") != 1 ||
		whitespaceRank("collapse") != 2 || whitespaceRank("invalid") != 0 {
		t.Fatal("whitespaceRank() ordering is incorrect")
	}
	state := &compileState{}
	for _, test := range []struct {
		name  xsd.QName
		value string
		fixed bool
	}{
		{name: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, value: "preserve"},
		{name: xsd.QName{Namespace: xsd.Namespace, Local: "normalizedString"}, value: "replace"},
		{name: xsd.QName{Namespace: xsd.Namespace, Local: "token"}, value: "collapse"},
		{name: xsd.QName{Namespace: xsd.Namespace, Local: "language"}, value: "collapse", fixed: true},
		{name: xsd.QName{Namespace: xsd.Namespace, Local: "integer"}, value: "collapse", fixed: true},
	} {
		value, fixed := state.namedWhitespace(test.name, 0)
		if value != test.value || fixed != test.fixed {
			t.Fatalf("namedWhitespace(%v) = %q, %t, want %q, %t", test.name, value, fixed, test.value, test.fixed)
		}
	}
}
