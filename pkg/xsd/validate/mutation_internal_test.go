package validate

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
)

func TestOrderedLimitComparisonsRespectExactBoundary(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		value    int
		limit    int
		exceeds  bool
		negative bool
	}{
		{name: "below", value: 2, limit: 3},
		{name: "equal", value: 3, limit: 3},
		{name: "above", value: 4, limit: 3, exceeds: true},
		{name: "negative", value: -1, limit: 3, negative: true},
		{name: "zero", value: 0, limit: 3},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := exceedsLimit(test.value, test.limit); got != test.exceeds {
				t.Fatalf("exceedsLimit(%d, %d) = %t, want %t", test.value, test.limit, got, test.exceeds)
			}
			if got := isNegative(test.value); got != test.negative {
				t.Fatalf("isNegative(%d) = %t, want %t", test.value, got, test.negative)
			}
		})
	}

	if !exceedsLimit(int64(4), int64(3)) || exceedsLimit(int64(3), int64(3)) {
		t.Fatal("int64 limit comparison does not preserve the exact boundary")
	}
}

func TestBoundedIdentityArithmeticRejectsOverflowAndPreservesBoundary(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		left  int
		right int
		limit int
		want  int
		ok    bool
	}{
		{name: "add below", left: 2, right: 3, limit: 6, want: 5, ok: true},
		{name: "add equal", left: 2, right: 3, limit: 5, want: 5, ok: true},
		{name: "add above", left: 2, right: 3, limit: 4},
		{name: "add existing above", left: 5, right: 0, limit: 4},
		{name: "add negative left", left: -1, right: 1, limit: 4},
		{name: "add negative right", left: 1, right: -1, limit: 4},
		{name: "add negative limit", left: 0, right: 0, limit: -1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := addWithinLimit(test.left, test.right, test.limit)
			if got != test.want || ok != test.ok {
				t.Fatalf("addWithinLimit(%d, %d, %d) = %d, %t, want %d, %t", test.left, test.right, test.limit, got, ok, test.want, test.ok)
			}
		})
	}

	for _, test := range []struct {
		name  string
		left  int
		right int
		limit int
		want  int
		ok    bool
	}{
		{name: "multiply below", left: 2, right: 3, limit: 7, want: 6, ok: true},
		{name: "multiply equal", left: 2, right: 3, limit: 6, want: 6, ok: true},
		{name: "multiply above", left: 2, right: 3, limit: 5},
		{name: "multiply zero left", left: 0, right: 3, limit: 0, want: 0, ok: true},
		{name: "multiply zero right", left: 3, right: 0, limit: 0, want: 0, ok: true},
		{name: "multiply negative", left: -1, right: 3, limit: 6},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := multiplyWithinLimit(test.left, test.right, test.limit)
			if got != test.want || ok != test.ok {
				t.Fatalf("multiplyWithinLimit(%d, %d, %d) = %d, %t, want %d, %t", test.left, test.right, test.limit, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestConsumeIdentityWorkUsesBoundedArithmetic(t *testing.T) {
	t.Parallel()

	node := &instanceNode{Children: []*instanceNode{{}, {}}}
	constraint := xsd.IdentityConstraint{Selector: "a/b", Fields: []string{"c"}}
	state := validationState{validator: &Validator{limits: Limits{MaxXPathSteps: 9}}}
	if err := state.consumeIdentityWork(node, constraint); err != nil || state.xpathSteps != 9 {
		t.Fatalf("consumeIdentityWork(exact) = steps %d, error %v", state.xpathSteps, err)
	}
	if err := state.consumeIdentityWork(node, constraint); err == nil || state.xpathSteps != 9 {
		t.Fatalf("consumeIdentityWork(exceeded) = steps %d, error %v", state.xpathSteps, err)
	}
	fieldOverflow := validationState{validator: &Validator{limits: Limits{MaxXPathSteps: 2}}}
	if err := fieldOverflow.consumeIdentityWork(
		node,
		xsd.IdentityConstraint{Selector: ".", Fields: []string{"a/b"}},
	); err == nil {
		t.Fatal("consumeIdentityWork(field overflow) succeeded")
	}
	preexistingOverflow := validationState{
		validator:  &Validator{limits: Limits{MaxXPathSteps: 2}},
		xpathSteps: 3,
	}
	if err := preexistingOverflow.consumeIdentityWork(
		node,
		xsd.IdentityConstraint{Selector: "."},
	); err == nil {
		t.Fatal("consumeIdentityWork(preexisting overflow) succeeded")
	}

	valueState := validationState{validator: &Validator{limits: Limits{MaxIdentityValues: 3}}}
	if err := valueState.consumeIdentityValues(3); err != nil || valueState.identityValues != 3 {
		t.Fatalf("consumeIdentityValues(exact) = values %d, error %v", valueState.identityValues, err)
	}
	if err := valueState.consumeIdentityValues(1); err == nil || valueState.identityValues != 3 {
		t.Fatalf("consumeIdentityValues(exceeded) = values %d, error %v", valueState.identityValues, err)
	}
}

func TestValidityClassificationAndConjunctionTruthTables(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		left     bool
		right    bool
		relation operandValidity
	}{
		{relation: neitherOperandValid},
		{right: true, relation: oneOperandValid},
		{left: true, relation: oneOperandValid},
		{left: true, right: true, relation: bothOperandsValid},
	} {
		if got := classifyOperandValidity(test.left, test.right); got != test.relation {
			t.Fatalf("classifyOperandValidity(%t, %t) = %d, want %d", test.left, test.right, got, test.relation)
		}
	}

	for _, test := range []struct {
		left  bool
		right bool
		equal bool
		want  bool
	}{
		{},
		{equal: true},
		{right: true, equal: true},
		{left: true, equal: true},
		{left: true, right: true},
		{left: true, right: true, equal: true, want: true},
	} {
		if got := bothValidAnd(test.left, test.right, test.equal); got != test.want {
			t.Fatalf("bothValidAnd(%t, %t, %t) = %t, want %t", test.left, test.right, test.equal, got, test.want)
		}
	}
}

func TestComparisonFacetBoundaries(t *testing.T) {
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
		{kind: "unknown", comparison: 0},
	} {
		if got := comparisonSatisfiesFacet(test.comparison, test.kind); got != test.want {
			t.Fatalf("comparisonSatisfiesFacet(%d, %q) = %t, want %t", test.comparison, test.kind, got, test.want)
		}
	}
}

func TestOccurrenceAndIndexBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		unbounded bool
		count     uint64
		maximum   uint64
		want      bool
	}{
		{count: 0, maximum: 1, want: true},
		{count: 1, maximum: 1},
		{count: 2, maximum: 1},
		{unbounded: true, count: 2, maximum: 1, want: true},
	} {
		if got := occurrenceAllowed(test.unbounded, test.count, test.maximum); got != test.want {
			t.Fatalf("occurrenceAllowed(%t, %d, %d) = %t, want %t", test.unbounded, test.count, test.maximum, got, test.want)
		}
	}
	for _, test := range []struct {
		count   uint64
		minimum uint64
		want    bool
	}{
		{count: 0, minimum: 1},
		{count: 1, minimum: 1, want: true},
		{count: 2, minimum: 1, want: true},
	} {
		if got := occurrenceMinimumMet(test.count, test.minimum); got != test.want {
			t.Fatalf("occurrenceMinimumMet(%d, %d) = %t, want %t", test.count, test.minimum, got, test.want)
		}
	}
	for _, test := range []struct {
		index  int
		length int
		want   bool
	}{
		{index: -1, length: 1},
		{index: 0, length: 1, want: true},
		{index: 1, length: 1},
		{index: 2, length: 1},
	} {
		if got := indexInRange(test.index, test.length); got != test.want {
			t.Fatalf("indexInRange(%d, %d) = %t, want %t", test.index, test.length, got, test.want)
		}
	}
}

func TestWildcardNamespaceConstraints(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		wildcard  *xsd.Wildcard
		namespace string
		target    string
		want      bool
	}{
		{name: "nil"},
		{name: "any", wildcard: &xsd.Wildcard{Namespaces: []string{"##any"}}, want: true},
		{name: "other foreign", wildcard: &xsd.Wildcard{Namespaces: []string{"##other"}}, namespace: "urn:other", target: "urn:target", want: true},
		{name: "other local", wildcard: &xsd.Wildcard{Namespaces: []string{"##other"}}, target: "urn:target"},
		{name: "other target", wildcard: &xsd.Wildcard{Namespaces: []string{"##other"}}, namespace: "urn:target", target: "urn:target"},
		{name: "local", wildcard: &xsd.Wildcard{Namespaces: []string{"##local"}}, want: true},
		{name: "local rejects named", wildcard: &xsd.Wildcard{Namespaces: []string{"##local"}}, namespace: "urn:other"},
		{name: "target", wildcard: &xsd.Wildcard{Namespaces: []string{"##targetNamespace"}}, namespace: "urn:target", target: "urn:target", want: true},
		{name: "target rejects foreign", wildcard: &xsd.Wildcard{Namespaces: []string{"##targetNamespace"}}, namespace: "urn:other", target: "urn:target"},
		{name: "literal", wildcard: &xsd.Wildcard{Namespaces: []string{"urn:other"}}, namespace: "urn:other", want: true},
		{name: "literal rejects mismatch", wildcard: &xsd.Wildcard{Namespaces: []string{"urn:other"}}, namespace: "urn:different"},
		{name: "later constraint", wildcard: &xsd.Wildcard{Namespaces: []string{"urn:first", "urn:second"}}, namespace: "urn:second", want: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := wildcardMatches(test.wildcard, test.namespace, test.target); got != test.want {
				t.Fatalf("wildcardMatches() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSortedAttributeNamesOrdersNamespaceThenLocalName(t *testing.T) {
	t.Parallel()

	names := sortedAttributeNames(map[xsd.QName]string{
		{Namespace: "urn:b", Local: "a"}: "",
		{Namespace: "urn:a", Local: "b"}: "",
		{Namespace: "urn:a", Local: "a"}: "",
	})
	want := []xsd.QName{
		{Namespace: "urn:a", Local: "a"},
		{Namespace: "urn:a", Local: "b"},
		{Namespace: "urn:b", Local: "a"},
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("sortedAttributeNames()[%d] = %#v, want %#v", index, names[index], want[index])
		}
	}
}

func TestMutationBoundaryHelpers(t *testing.T) {
	t.Parallel()

	if nextOccurrence(0) != 1 || nextOccurrence(1) != 2 {
		t.Fatal("nextOccurrence did not advance by exactly one")
	}
	if nextIndex(0) != 1 || nextIndex(7) != 8 {
		t.Fatal("nextIndex did not advance by exactly one")
	}
	if xmlFloatBitSize("float") != 32 || xmlFloatBitSize("double") != 64 || xmlFloatBitSize("unknown") != 64 {
		t.Fatal("xmlFloatBitSize selected the wrong IEEE width")
	}
	for _, test := range []struct {
		steps []string
		want  bool
	}{
		{},
		{steps: []string{"element"}},
		{steps: []string{"@id"}, want: true},
		{steps: []string{"attribute::id"}, want: true},
		{steps: []string{"parent", "@id"}, want: true},
	} {
		if got := identityAttributeStep(test.steps); got != test.want {
			t.Fatalf("identityAttributeStep(%v) = %t, want %t", test.steps, got, test.want)
		}
	}
	if !elementContentAbsent(false, &instanceNode{}) ||
		elementContentAbsent(true, &instanceNode{}) ||
		elementContentAbsent(false, &instanceNode{Text: "value"}) ||
		elementContentAbsent(false, &instanceNode{Children: []*instanceNode{{}}}) {
		t.Fatal("elementContentAbsent truth table failed")
	}
}

func TestCanonicalBinaryAndIdentityNamespaceBoundaries(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	for _, test := range []struct {
		lexical string
		want    string
	}{
		{lexical: "YQ==", want: "base64Binary:61"},
		{lexical: "not-base64", want: "lexical:not-base64"},
	} {
		if got := state.canonicalIdentityValue(builtIn("base64Binary"), test.lexical, nil); got != test.want {
			t.Fatalf("canonicalIdentityValue(base64Binary, %q) = %q, want %q", test.lexical, got, test.want)
		}
	}

	namespaces := map[string]string{"t": "urn:target"}
	if !identityNameMatches(xsd.QName{Namespace: "urn:target", Local: "value"}, "t:*", namespaces) {
		t.Fatal("identityNameMatches() rejected a matching namespace wildcard")
	}
	if identityNameMatches(xsd.QName{Namespace: "urn:other", Local: "value"}, "t:*", namespaces) {
		t.Fatal("identityNameMatches() accepted a different namespace")
	}
	if identityNameMatches(xsd.QName{Namespace: "urn:target", Local: "value"}, "missing:*", namespaces) {
		t.Fatal("identityNameMatches() accepted an unresolved namespace wildcard")
	}
}

func TestValidationCoversRemainingSchemaBoundaries(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	state := validationState{validator: &Validator{set: set, limits: Limits{MaxDiagnostics: 10}}}
	xsiType := xsd.QName{Namespace: schemaInstanceNamespace, Local: "type"}
	node := contentNode("value", map[xsd.QName]string{xsiType: "missing:Type"})
	if err := state.validateElementContent(node, xsd.Element{Type: builtIn("string")}, "/root"); err != nil ||
		len(state.diagnostics) != 1 || state.diagnostics[0].Code != "cvc-elt.4.2" {
		t.Fatalf("validateElementContent(unresolved xsi:type) = %#v, %v", state.diagnostics, err)
	}
	if methods, ok := state.typeDerivationMethods(builtIn("string"), builtIn("anyType")); !ok || len(methods) != 0 {
		t.Fatalf("typeDerivationMethods(anyType) = %#v, %t", methods, ok)
	}
	if _, _, ok := state.typeDerivationStep(xsd.QName{Namespace: "urn:test", Local: "Named"}); ok {
		t.Fatal("typeDerivationStep(complex type without derivation) succeeded")
	}
	if index, matched, err := state.matchParticle(
		xsd.Particle{MinOccurs: 1, MaxOccurs: 0}, nil, 0, "urn:test", "/root",
	); err != nil || matched || index != 0 {
		t.Fatalf("matchParticle(impossible occurrence) = %d, %t, %v", index, matched, err)
	}
}

func TestNumericFacetValidationRejectsMalformedBounds(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	for _, test := range []struct {
		name  string
		base  xsd.QName
		facet xsd.Facet
	}{
		{name: "invalid total digits", base: builtIn("decimal"), facet: xsd.Facet{Kind: xsd.FacetTotalDigits, Value: "x"}},
		{name: "invalid fraction digits", base: builtIn("decimal"), facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "x"}},
		{name: "negative fraction digits", base: builtIn("decimal"), facet: xsd.Facet{Kind: xsd.FacetFractionDigits, Value: "-1"}},
		{name: "NaN float boundary", base: builtIn("float"), facet: xsd.Facet{Kind: xsd.FacetMinInclusive, Value: "NaN"}},
	} {
		if state.numericFacetValid(test.base, "1", test.facet) {
			t.Fatalf("%s: numericFacetValid() succeeded", test.name)
		}
	}
}

func TestStrictWildcardAcceptsValidAnonymousAttribute(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{set: attributeValidationSet(t)}}
	name := xsd.QName{Namespace: "urn:wildcard", Local: "inline"}
	node := contentNode("", map[xsd.QName]string{name: "true"})
	err := state.validateAdditionalAttribute(node, name, "urn:other", &xsd.Wildcard{
		Namespaces: []string{"##other"}, ProcessContents: xsd.ProcessStrict,
	}, "/root")
	if err != nil || len(state.diagnostics) != 0 {
		t.Fatalf("validateAdditionalAttribute() = %#v, %v", state.diagnostics, err)
	}
}

func TestSimpleValueEqualityDistinguishesEveryPrimitiveBoundary(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:mutation-equality",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:mutation-equality">
<xs:simpleType name="Numbers"><xs:list itemType="xs:integer"/></xs:simpleType>
<xs:simpleType name="Choice"><xs:union memberTypes="xs:boolean xs:decimal"/></xs:simpleType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	state := validationState{validator: &Validator{set: set}}
	for _, test := range []struct {
		name     string
		typeName xsd.QName
		left     string
		right    string
		want     bool
	}{
		{name: "named list equal", typeName: xsd.QName{Namespace: "urn:mutation-equality", Local: "Numbers"}, left: "1 2", right: "01 2", want: true},
		{name: "named list length", typeName: xsd.QName{Namespace: "urn:mutation-equality", Local: "Numbers"}, left: "1", right: "1 2"},
		{name: "named list item", typeName: xsd.QName{Namespace: "urn:mutation-equality", Local: "Numbers"}, left: "1 2", right: "1 3"},
		{name: "named union equal", typeName: xsd.QName{Namespace: "urn:mutation-equality", Local: "Choice"}, left: "1.0", right: "1.00", want: true},
		{name: "float equal", typeName: builtIn("float"), left: "1.5", right: "1.5", want: true},
		{name: "float unequal", typeName: builtIn("float"), left: "1.5", right: "2.5"},
		{name: "float nan left", typeName: builtIn("float"), left: "NaN", right: "1"},
		{name: "float nan right", typeName: builtIn("float"), left: "1", right: "NaN"},
		{name: "integer equal", typeName: builtIn("integer"), left: "01", right: "1", want: true},
		{name: "integer unequal", typeName: builtIn("integer"), left: "1", right: "2"},
		{name: "unknown equal", typeName: builtIn("unknown"), left: "same", right: "same", want: true},
		{name: "unknown unequal", typeName: builtIn("unknown"), left: "left", right: "right"},
	} {
		got, gotErr := state.simpleValuesEqual(test.typeName, test.left, test.right)
		if gotErr != nil || got != test.want {
			t.Fatalf("%s: simpleValuesEqual() = %t, %v, want %t", test.name, got, gotErr, test.want)
		}
	}
}

func TestFacetLengthsUseValueSpaceAndExactBounds(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:mutation-facets",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:mutation-facets">
<xs:simpleType name="Numbers"><xs:list itemType="xs:integer"/></xs:simpleType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	state := validationState{validator: &Validator{set: set}}
	for _, test := range []struct {
		name       string
		definition xsd.SimpleType
		lexical    string
		want       bool
	}{
		{name: "string exact", definition: xsd.SimpleType{Base: builtIn("string"), Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}}}, lexical: "ab", want: true},
		{name: "string mismatch", definition: xsd.SimpleType{Base: builtIn("string"), Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "1"}}}, lexical: "ab"},
		{name: "hex decoded", definition: xsd.SimpleType{Base: builtIn("hexBinary"), Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "1"}}}, lexical: "0A", want: true},
		{name: "base64 decoded", definition: xsd.SimpleType{Base: builtIn("base64Binary"), Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "1"}}}, lexical: "YQ==", want: true},
		{name: "named list", definition: xsd.SimpleType{Base: xsd.QName{Namespace: "urn:mutation-facets", Local: "Numbers"}, Facets: []xsd.Facet{{Kind: xsd.FacetLength, Value: "2"}}}, lexical: "1 2", want: true},
		{name: "pattern second match", definition: xsd.SimpleType{Base: builtIn("string"), Facets: []xsd.Facet{{Kind: xsd.FacetPattern, Value: "z"}, {Kind: xsd.FacetPattern, Value: "a"}}}, lexical: "a", want: true},
		{name: "enumeration match", definition: xsd.SimpleType{Base: builtIn("integer"), Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "01"}}}, lexical: "1", want: true},
		{name: "enumeration mismatch", definition: xsd.SimpleType{Base: builtIn("integer"), Facets: []xsd.Facet{{Kind: xsd.FacetEnumeration, Value: "2"}}}, lexical: "1"},
		{name: "total digits zero", definition: xsd.SimpleType{Base: builtIn("decimal"), Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "0"}}}, lexical: "1"},
		{name: "total digits exact", definition: xsd.SimpleType{Base: builtIn("decimal"), Facets: []xsd.Facet{{Kind: xsd.FacetTotalDigits, Value: "2"}}}, lexical: "12", want: true},
	} {
		if got := state.facetsValid(test.definition, test.lexical); got != test.want {
			t.Fatalf("%s: facetsValid() = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestDurationComponentsApplySignToMonthsAndSeconds(t *testing.T) {
	t.Parallel()

	positive, ok := parseDurationValue("P1Y2M3DT4H5M6S")
	if !ok {
		t.Fatal("positive duration did not parse")
	}
	months, seconds := durationComponents(positive)
	if months.String() != "14" || seconds.RatString() != "273906" {
		t.Fatalf("positive components = %s months, %s seconds", months, seconds)
	}
	negative, ok := parseDurationValue("-P1Y2M3DT4H5M6S")
	if !ok {
		t.Fatal("negative duration did not parse")
	}
	months, seconds = durationComponents(negative)
	if months.String() != "-14" || seconds.RatString() != "-273906" {
		t.Fatalf("negative components = %s months, %s seconds", months, seconds)
	}
}

func TestMissingAttributeReferenceDoesNotStopLaterUseValidation(t *testing.T) {
	t.Parallel()

	set := attributeValidationSet(t)
	state := validationState{validator: &Validator{set: set, limits: Limits{MaxDiagnostics: 10}}}
	node := contentNode("", nil)
	err := state.validateAttributes(node, "urn:test", []xsd.AttributeUse{
		{Ref: xsd.QName{Namespace: "urn:missing", Local: "missing"}},
		{Name: "required", Use: xsd.AttributeRequired, Type: builtIn("string")},
	}, nil, "/root")
	if err != nil || len(state.diagnostics) != 2 {
		t.Fatalf("validateAttributes() = %d diagnostics, %v; want 2", len(state.diagnostics), err)
	}
}
