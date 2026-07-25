package validate

import (
	"context"
	"encoding/xml"
	"errors"
	"math"
	"strings"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
)

func TestInlineSimpleValueEqualitySelection(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:test", Content: []byte(`<schema xmlns="http://www.w3.org/2001/XMLSchema"/>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := New(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	state := validationState{validator: validator}
	decimal := xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: "decimal"},
	}
	stringType := xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		Base:    xsd.QName{Namespace: xsd.Namespace, Local: "string"},
	}
	for _, test := range []struct {
		name       string
		definition xsd.SimpleType
		left       string
		right      string
		want       bool
	}{
		{name: "inline restriction base", definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &decimal}, left: "1.0", right: "1", want: true},
		{name: "named list", definition: xsd.SimpleType{Variety: xsd.SimpleList, ItemType: builtIn("decimal")}, left: "1 2", right: "1.0 2.00", want: true},
		{name: "named list length mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleList, ItemType: builtIn("decimal")}, left: "1", right: "1 2"},
		{name: "named list item mismatch", definition: xsd.SimpleType{Variety: xsd.SimpleList, ItemType: builtIn("decimal")}, left: "1 2", right: "1 3"},
		{name: "inline list", definition: xsd.SimpleType{Variety: xsd.SimpleList, InlineItem: &decimal}, left: "1", right: "1.0", want: true},
		{name: "named union skips invalid member", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{{Namespace: xsd.Namespace, Local: "boolean"}, {Namespace: xsd.Namespace, Local: "decimal"}}}, left: "1.0", right: "1.00", want: true},
		{name: "named union rejects different selections", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{{Namespace: xsd.Namespace, Local: "boolean"}, {Namespace: xsd.Namespace, Local: "string"}}}, left: "text", right: "true"},
		{name: "inline union skips invalid member", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{decimal, stringType}}, left: "left", right: "left", want: true},
		{name: "inline union rejects different selections", definition: xsd.SimpleType{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{decimal, stringType}}, left: "text", right: "1"},
		{name: "empty union", definition: xsd.SimpleType{Variety: xsd.SimpleUnion}, left: "x", right: "x"},
		{name: "defensive invalid variety", definition: xsd.SimpleType{Variety: "invalid"}, left: "same", right: "same", want: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, equalErr := state.inlineSimpleValuesEqual(test.definition, test.left, test.right)
			if equalErr != nil || got != test.want {
				t.Fatalf("inlineSimpleValuesEqual() = %t, %v; want %t", got, equalErr, test.want)
			}
		})
	}
}

func TestNamedAndContextualRestrictionValueEquality(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:equality",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
 xmlns:t="urn:equality" targetNamespace="urn:equality">
 <xs:simpleType name="Named"><xs:restriction base="xs:decimal"/></xs:simpleType>
 <xs:simpleType name="Inline"><xs:restriction>
  <xs:simpleType><xs:restriction base="xs:decimal"/></xs:simpleType>
 </xs:restriction></xs:simpleType>
 <xs:simpleType name="QNameList"><xs:list itemType="xs:QName"/></xs:simpleType>
 <xs:simpleType name="InlineQNameList"><xs:list>
  <xs:simpleType><xs:restriction base="xs:QName"/></xs:simpleType>
 </xs:list></xs:simpleType>
 <xs:simpleType name="QNameUnion"><xs:union memberTypes="xs:boolean xs:QName"/></xs:simpleType>
 <xs:simpleType name="InlineQNameUnion"><xs:union>
  <xs:simpleType><xs:restriction base="xs:boolean"/></xs:simpleType>
  <xs:simpleType><xs:restriction base="xs:QName"/></xs:simpleType>
 </xs:union></xs:simpleType>
 <xs:simpleType name="Plain"><xs:restriction base="xs:string"/></xs:simpleType>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	state := validationState{validator: &Validator{set: set}}
	for _, name := range []string{"Named", "Inline"} {
		equal, equalErr := state.simpleValuesEqual(
			xsd.QName{Namespace: "urn:equality", Local: name},
			"1.0",
			"1",
		)
		if equalErr != nil || !equal {
			t.Fatalf("simpleValuesEqual(%s) = %t, %v", name, equal, equalErr)
		}
	}
	definition := xsd.SimpleType{
		Variety: xsd.SimpleRestriction,
		InlineBase: &xsd.SimpleType{
			Variety: xsd.SimpleRestriction,
			Base:    builtIn("QName"),
		},
	}
	equal, err := state.inlineSimpleValuesEqualContext(
		definition,
		"left:item",
		"right:item",
		map[string]string{"left": "urn:value"},
		map[string]string{"right": "urn:value"},
	)
	if err != nil || !equal {
		t.Fatalf("inlineSimpleValuesEqualContext() = %t, %v", equal, err)
	}
	leftNamespaces := map[string]string{"left": "urn:value"}
	rightNamespaces := map[string]string{"right": "urn:value"}
	for _, test := range []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "QNameList", left: "left:item", right: "right:item", want: true},
		{name: "QNameList", left: "left:item", right: "right:item right:other"},
		{name: "QNameList", left: "left:item", right: "right:other"},
		{name: "InlineQNameList", left: "left:item", right: "right:item", want: true},
		{name: "QNameUnion", left: "left:item", right: "right:item", want: true},
		{name: "QNameUnion", left: "true", right: "right:item"},
		{name: "QNameUnion", left: "invalid:name:again", right: "also:invalid:again"},
		{name: "InlineQNameUnion", left: "left:item", right: "right:item", want: true},
		{name: "InlineQNameUnion", left: "true", right: "right:item"},
	} {
		got, gotErr := state.simpleValuesEqualContext(
			xsd.QName{Namespace: "urn:equality", Local: test.name},
			test.left,
			test.right,
			leftNamespaces,
			rightNamespaces,
		)
		if gotErr != nil || got != test.want {
			t.Fatalf("simpleValuesEqualContext(%s, %q, %q) = %t, %v; want %t", test.name, test.left, test.right, got, gotErr, test.want)
		}
	}
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "Named"},
		{name: "Inline"},
		{name: "QNameList", want: true},
		{name: "InlineQNameList", want: true},
		{name: "QNameUnion", want: true},
		{name: "InlineQNameUnion", want: true},
		{name: "Plain"},
		{name: "Missing"},
	} {
		got := state.simpleTypeUsesNamespaceContext(
			xsd.QName{Namespace: "urn:equality", Local: test.name},
		)
		if got != test.want {
			t.Fatalf("simpleTypeUsesNamespaceContext(%s) = %t; want %t", test.name, got, test.want)
		}
	}
	for _, test := range []struct {
		definition xsd.SimpleType
		want       bool
	}{
		{definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: builtIn("QName")}, want: true},
		{definition: xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: builtIn("QName")}}, want: true},
		{definition: xsd.SimpleType{Variety: xsd.SimpleList, ItemType: builtIn("QName")}, want: true},
		{definition: xsd.SimpleType{Variety: xsd.SimpleList, InlineItem: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: builtIn("QName")}}, want: true},
		{definition: xsd.SimpleType{Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{builtIn("string"), builtIn("QName")}}, want: true},
		{definition: xsd.SimpleType{Variety: xsd.SimpleUnion, InlineMembers: []xsd.SimpleType{{Variety: xsd.SimpleRestriction, Base: builtIn("string")}, {Variety: xsd.SimpleRestriction, Base: builtIn("QName")}}}, want: true},
		{definition: xsd.SimpleType{Variety: xsd.SimpleUnion}},
	} {
		if got := state.inlineTypeUsesNamespaceContext(test.definition); got != test.want {
			t.Fatalf("inlineTypeUsesNamespaceContext(%#v) = %t; want %t", test.definition, got, test.want)
		}
	}
	contextualUnion := xsd.SimpleType{
		Variety:     xsd.SimpleUnion,
		MemberTypes: []xsd.QName{builtIn("boolean"), builtIn("QName")},
	}
	inlineContextualUnion := xsd.SimpleType{
		Variety: xsd.SimpleUnion,
		InlineMembers: []xsd.SimpleType{
			{Variety: xsd.SimpleRestriction, Base: builtIn("boolean")},
			{Variety: xsd.SimpleRestriction, Base: builtIn("QName")},
		},
	}
	for _, test := range []struct {
		definition xsd.SimpleType
		left       string
		right      string
		want       bool
	}{
		{definition: contextualUnion, left: "left:item", right: "right:item", want: true},
		{definition: contextualUnion, left: "true", right: "right:item"},
		{definition: contextualUnion, left: "invalid:name:again", right: "also:invalid:again"},
		{definition: inlineContextualUnion, left: "left:item", right: "right:item", want: true},
		{definition: inlineContextualUnion, left: "true", right: "right:item"},
		{definition: inlineContextualUnion, left: "invalid:name:again", right: "also:invalid:again"},
		{definition: xsd.SimpleType{Variety: xsd.SimpleUnion}, left: "left", right: "right"},
		{definition: xsd.SimpleType{Variety: "unknown"}, left: "same", right: "same", want: true},
	} {
		got, gotErr := state.inlineSimpleValuesEqualContext(
			test.definition,
			test.left,
			test.right,
			leftNamespaces,
			rightNamespaces,
		)
		if gotErr != nil || got != test.want {
			t.Fatalf("inlineSimpleValuesEqualContext(%#v, %q, %q) = %t, %v; want %t", test.definition, test.left, test.right, got, gotErr, test.want)
		}
	}
}

func TestTypeDerivationMethodsFollowBuiltInSimpleTypeChains(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI:     "urn:types",
		Content: []byte(`<schema xmlns="http://www.w3.org/2001/XMLSchema"/>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := New(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	state := validationState{validator: validator}
	methods, ok := state.typeDerivationMethods(
		xsd.QName{Namespace: xsd.Namespace, Local: "token"},
		xsd.QName{Namespace: xsd.Namespace, Local: "string"},
	)
	if !ok || len(methods) != 2 ||
		methods[0] != xsd.DerivationRestriction ||
		methods[1] != xsd.DerivationRestriction {
		t.Fatalf("typeDerivationMethods() = %v, %t", methods, ok)
	}
	methods, ok = state.typeDerivationMethods(
		xsd.QName{Namespace: xsd.Namespace, Local: "IDREFS"},
		xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"},
	)
	if !ok || len(methods) != 1 || methods[0] != xsd.DerivationList {
		t.Fatalf("IDREFS typeDerivationMethods() = %v, %t", methods, ok)
	}
}

func TestDurationValueParsingBoundaries(t *testing.T) {
	t.Parallel()

	value, ok := parseDurationValue("-P1Y2M3DT4H5M6.5S")
	if !ok || value.sign != -1 || value.years.String() != "1" ||
		value.months.String() != "2" || value.days.String() != "3" ||
		value.hours.String() != "4" || value.minutes.String() != "5" ||
		value.seconds.RatString() != "13/2" {
		t.Fatalf("parseDurationValue() = %#v, %t", value, ok)
	}
	large, ok := parseDurationValue("P" + strings.Repeat("9", 100) + "Y")
	if !ok || large.years.String() != strings.Repeat("9", 100) {
		t.Fatalf("parseDurationValue(large) = %#v, %t", large, ok)
	}
	for _, lexical := range []string{"invalid", "PT1e3S"} {
		if _, ok := parseDurationValue(lexical); ok {
			t.Fatalf("parseDurationValue(%q) succeeded", lexical)
		}
	}
}

func TestDurationComparisonPreservesArbitraryPrecision(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		left  string
		right string
		want  int
	}{
		{left: "PT0.0000000001S", right: "PT0.0000000002S", want: -1},
		{left: "P100000000000000000000Y", right: "P99999999999999999999Y", want: 1},
		{left: "P1Y", right: "P12M", want: 0},
		{left: "-P1D", right: "PT0S", want: -1},
		{left: "-P2000Y1M", right: "-P2000Y", want: -1},
	} {
		comparison, comparable := compareDurations(test.left, test.right)
		if !comparable || comparison != test.want {
			t.Fatalf("compareDurations(%q, %q) = %d, %t; want %d, true", test.left, test.right, comparison, comparable, test.want)
		}
	}
	if !durationValuesEqual("-P1D", "-PT24H") {
		t.Fatal("durationValuesEqual() rejected equal negative durations")
	}
	if durationValuesEqual("invalid", "P1D") {
		t.Fatal("durationValuesEqual() accepted an invalid duration")
	}
}

func TestCalendarComparisonBoundaries(t *testing.T) {
	t.Parallel()

	if comparison, ok := compareCalendarValues(
		"date",
		"2026-07-19Z",
		"2026-07-20Z",
	); !ok || comparison >= 0 {
		t.Fatalf("compareCalendarValues() = %d, %t", comparison, ok)
	}
	for _, values := range [][2]string{
		{"", "2026-07-20Z"},
		{"2026-07-19Z", ""},
	} {
		if _, ok := compareCalendarValues("date", values[0], values[1]); ok {
			t.Fatalf("compareCalendarValues(%q, %q) succeeded", values[0], values[1])
		}
	}
}

func TestCalendarComparisonUsesXMLSchemaValueSpace(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		kind       string
		left       string
		right      string
		want       int
		comparable bool
	}{
		{kind: "dateTime", left: "2000-01-01T12:00:00+02:00", right: "2000-01-01T10:00:00Z", want: 0, comparable: true},
		{kind: "dateTime", left: "2000-01-01T24:00:00Z", right: "2000-01-02T00:00:00Z", want: 0, comparable: true},
		{kind: "date", left: "-0002-01-01Z", right: "-0001-01-01Z", want: -1, comparable: true},
		{kind: "gYear", left: "100000000000000000000Z", right: "99999999999999999999Z", want: 1, comparable: true},
		{kind: "time", left: "12:00:00+02:00", right: "10:00:00Z", want: 0, comparable: true},
		{kind: "gYearMonth", left: "2000-02Z", right: "2000-01Z", want: 1, comparable: true},
		{kind: "gMonthDay", left: "--02-29Z", right: "--02-28Z", want: 1, comparable: true},
		{kind: "gDay", left: "---02Z", right: "---01Z", want: 1, comparable: true},
		{kind: "gMonth", left: "--02--Z", right: "--01--Z", want: 1, comparable: true},
		{kind: "dateTime", left: "2000-01-01T00:00:00", right: "2000-01-01T00:00:00Z", comparable: false},
		{kind: "dateTime", left: "2000-01-01T00:00:00Z", right: "2000-01-01T00:00:00", comparable: false},
		{kind: "dateTime", left: "2000-01-03T00:00:00", right: "2000-01-01T00:00:00Z", want: 1, comparable: true},
		{kind: "dateTime", left: "2000-01-01T00:00:00", right: "2000-01-03T00:00:00Z", want: -1, comparable: true},
		{kind: "dateTime", left: "2000-01-01T00:00:00Z", right: "2000-01-03T00:00:00", want: -1, comparable: true},
		{kind: "dateTime", left: "2000-01-03T00:00:00Z", right: "2000-01-01T00:00:00", want: 1, comparable: true},
		{kind: "dateTime", left: "2000-01-01T10:00:00-02:00", right: "2000-01-01T12:00:00Z", want: 0, comparable: true},
		{kind: "gYear", left: "2000", right: "2001", want: -1, comparable: true},
	} {
		comparison, comparable := compareCalendarValues(test.kind, test.left, test.right)
		if comparable != test.comparable || comparable && comparison != test.want {
			t.Fatalf("compareCalendarValues(%q, %q, %q) = %d, %t; want %d, %t", test.kind, test.left, test.right, comparison, comparable, test.want, test.comparable)
		}
	}
}

func TestContextReaderChecksCancellation(t *testing.T) {
	t.Parallel()

	reader := &contextReader{ctx: context.Background(), reader: strings.NewReader("ok")}
	buffer := make([]byte, 2)
	if count, err := reader.Read(buffer); err != nil || count != 2 || string(buffer) != "ok" {
		t.Fatalf("Read() = %d, %v, %q", count, err, buffer)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	reader = &contextReader{ctx: canceled, reader: strings.NewReader("ignored")}
	if count, err := reader.Read(buffer); !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatalf("Read() = %d, %v", count, err)
	}
}

func TestValidationPrimitiveDecisionTables(t *testing.T) {
	t.Parallel()

	state := validationState{validator: &Validator{limits: Limits{MaxDiagnostics: 1}}}
	location := xsd.Location{Line: 2, Column: 3}
	if err := state.add(location, "/root", "code", "message"); err != nil {
		t.Fatalf("add() error = %v", err)
	}
	if len(state.diagnostics) != 1 || state.diagnostics[0].Location != location ||
		state.diagnostics[0].Path != "/root" || state.diagnostics[0].Code != "code" {
		t.Fatalf("diagnostic = %#v", state.diagnostics)
	}
	if err := state.add(location, "/root", "extra", "extra"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("second add() error = %v", err)
	}

	namespaces := map[string]string{"": "urn:default", "t": "urn:test"}
	for lexical, want := range map[string]xsd.QName{
		"local":   {Namespace: "urn:default", Local: "local"},
		"t:local": {Namespace: "urn:test", Local: "local"},
	} {
		got, ok := resolveInstanceQName(lexical, namespaces)
		if !ok || got != want {
			t.Fatalf("resolveInstanceQName(%q) = %#v, %t", lexical, got, ok)
		}
	}
	for _, lexical := range []string{"", " two words ", "prefix:", "a:b:c", "missing:name"} {
		if got, ok := resolveInstanceQName(lexical, namespaces); ok {
			t.Fatalf("resolveInstanceQName(%q) = %#v, true", lexical, got)
		}
	}

	for _, local := range []string{"nil", "type", "schemaLocation", "noNamespaceSchemaLocation"} {
		if !permittedSchemaInstanceAttribute(xsd.QName{
			Namespace: schemaInstanceNamespace,
			Local:     local,
		}) {
			t.Fatalf("permittedSchemaInstanceAttribute(%q) = false", local)
		}
	}
	for _, name := range []xsd.QName{
		{Namespace: "urn:other", Local: "nil"},
		{Namespace: schemaInstanceNamespace, Local: "other"},
	} {
		if permittedSchemaInstanceAttribute(name) {
			t.Fatalf("permittedSchemaInstanceAttribute(%#v) = true", name)
		}
	}

	for _, name := range []xml.Name{
		{Local: "xmlns"},
		{Space: "xmlns", Local: "prefix"},
	} {
		if !isNamespaceDeclaration(name) {
			t.Fatalf("isNamespaceDeclaration(%#v) = false", name)
		}
	}
	if isNamespaceDeclaration(xml.Name{Local: "attribute"}) {
		t.Fatal("isNamespaceDeclaration(attribute) = true")
	}

	cloned := cloneNamespaces(namespaces)
	cloned["t"] = "mutated"
	if namespaces["t"] != "urn:test" {
		t.Fatal("cloneNamespaces() retained a map alias")
	}

	for _, test := range []struct {
		name string
		got  bool
		want bool
	}{
		{name: "element default lexical", got: validatorElementDefaultSet(xsd.Element{Default: "value"}), want: true},
		{name: "element default explicit empty", got: validatorElementDefaultSet(xsd.Element{DefaultSet: true}), want: true},
		{name: "element default absent", got: validatorElementDefaultSet(xsd.Element{})},
		{name: "element fixed lexical", got: validatorElementFixedSet(xsd.Element{Fixed: "value"}), want: true},
		{name: "element fixed explicit empty", got: validatorElementFixedSet(xsd.Element{FixedSet: true}), want: true},
		{name: "element fixed absent", got: validatorElementFixedSet(xsd.Element{})},
		{name: "attribute default lexical", got: validatorAttributeDefaultSet(xsd.AttributeUse{Default: "value"}), want: true},
		{name: "attribute default explicit empty", got: validatorAttributeDefaultSet(xsd.AttributeUse{DefaultSet: true}), want: true},
		{name: "attribute default absent", got: validatorAttributeDefaultSet(xsd.AttributeUse{})},
		{name: "attribute fixed lexical", got: validatorAttributeFixedSet(xsd.AttributeUse{Fixed: "value"}), want: true},
		{name: "attribute fixed explicit empty", got: validatorAttributeFixedSet(xsd.AttributeUse{FixedSet: true}), want: true},
		{name: "attribute fixed absent", got: validatorAttributeFixedSet(xsd.AttributeUse{})},
	} {
		if test.got != test.want {
			t.Fatalf("%s = %t, want %t", test.name, test.got, test.want)
		}
	}
}

func TestValidateElementFixedConstraintBranches(t *testing.T) {
	t.Parallel()

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:fixed", Content: []byte(
			`<schema xmlns="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
				`<complexType name="Mixed" mixed="true"/>` +
				`</schema>`,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := New(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	mixedType := xsd.QName{Namespace: "urn:test", Local: "Mixed"}
	missingType := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	child := &instanceNode{Name: xsd.QName{Local: "child"}}
	for _, test := range []struct {
		name        string
		node        *instanceNode
		effective   *instanceNode
		element     xsd.Element
		typeName    xsd.QName
		nilled      bool
		wantError   bool
		wantMessage bool
	}{
		{name: "no fixed value", node: &instanceNode{}, effective: &instanceNode{}},
		{name: "nilled", node: &instanceNode{}, effective: &instanceNode{}, element: xsd.Element{Fixed: "x"}, nilled: true},
		{name: "child element", node: &instanceNode{Children: []*instanceNode{child}}, effective: &instanceNode{}, element: xsd.Element{Fixed: "x"}, wantMessage: true},
		{name: "inline mixed equal", node: &instanceNode{}, effective: &instanceNode{Text: "x"}, element: xsd.Element{Fixed: "x", InlineComplexType: &xsd.ComplexType{Mixed: true}}},
		{name: "inline mixed mismatch", node: &instanceNode{}, effective: &instanceNode{Text: "y"}, element: xsd.Element{Fixed: "x", InlineComplexType: &xsd.ComplexType{Mixed: true}}, wantMessage: true},
		{name: "named mixed equal", node: &instanceNode{}, effective: &instanceNode{Text: "x"}, element: xsd.Element{Fixed: "x"}, typeName: mixedType},
		{name: "named mixed mismatch", node: &instanceNode{}, effective: &instanceNode{Text: "y"}, element: xsd.Element{Fixed: "x"}, typeName: mixedType, wantMessage: true},
		{name: "untyped equal", node: &instanceNode{}, effective: &instanceNode{Text: "x"}, element: xsd.Element{Fixed: "x"}},
		{name: "untyped mismatch", node: &instanceNode{}, effective: &instanceNode{Text: "y"}, element: xsd.Element{Fixed: "x"}, wantMessage: true},
		{name: "simple equal", node: &instanceNode{}, effective: &instanceNode{Text: "x"}, element: xsd.Element{Fixed: "x"}, typeName: stringType},
		{name: "simple mismatch", node: &instanceNode{}, effective: &instanceNode{Text: "y"}, element: xsd.Element{Fixed: "x"}, typeName: stringType, wantMessage: true},
		{name: "missing type", node: &instanceNode{}, effective: &instanceNode{Text: "x"}, element: xsd.Element{Fixed: "x"}, typeName: missingType, wantMessage: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := validationState{validator: validator}
			err := state.validateElementFixedConstraint(
				test.node,
				test.effective,
				test.element,
				test.typeName,
				test.nilled,
				"/root",
			)
			if test.wantError != (err != nil) {
				t.Fatalf("validateElementFixedConstraint() error = %v", err)
			}
			if test.wantMessage != (len(state.diagnostics) == 1) {
				t.Fatalf("diagnostics = %#v", state.diagnostics)
			}
		})
	}
}

func TestIdentityAndFacetPrimitiveDecisionTables(t *testing.T) {
	t.Parallel()

	namespaces := map[string]string{"t": "urn:test"}
	for _, test := range []struct {
		expression string
		want       xsd.QName
		ok         bool
	}{
		{expression: "local", want: xsd.QName{Local: "local"}, ok: true},
		{expression: "t:local", want: xsd.QName{Namespace: "urn:test", Local: "local"}, ok: true},
		{expression: ""},
		{expression: "t:", want: xsd.QName{Namespace: "urn:test"}},
		{expression: "missing:local", want: xsd.QName{Local: "local"}},
		{expression: "a:b:c"},
	} {
		got, ok := identityQName(test.expression, namespaces)
		if ok != test.ok || got != test.want {
			t.Fatalf("identityQName(%q) = %#v, %t", test.expression, got, ok)
		}
	}
	if !identityNameMatches(xsd.QName{Local: "anything"}, "*", namespaces) ||
		!identityNameMatches(
			xsd.QName{Namespace: "urn:test", Local: "local"},
			"t:local",
			namespaces,
		) || identityNameMatches(xsd.QName{Local: "other"}, "local", namespaces) ||
		identityNameMatches(xsd.QName{Local: "local"}, "missing:local", namespaces) {
		t.Fatal("identityNameMatches() decision table failed")
	}

	for _, test := range []struct {
		comparison int
		kind       xsd.FacetKind
		want       bool
	}{
		{comparison: 0, kind: xsd.FacetMinInclusive, want: true},
		{comparison: -1, kind: xsd.FacetMinInclusive},
		{comparison: 1, kind: xsd.FacetMinExclusive, want: true},
		{comparison: 0, kind: xsd.FacetMinExclusive},
		{comparison: 0, kind: xsd.FacetMaxInclusive, want: true},
		{comparison: 1, kind: xsd.FacetMaxInclusive},
		{comparison: -1, kind: xsd.FacetMaxExclusive, want: true},
		{comparison: 0, kind: xsd.FacetMaxExclusive},
		{comparison: 0, kind: xsd.FacetPattern},
	} {
		if got := comparisonSatisfiesFacet(test.comparison, test.kind); got != test.want {
			t.Fatalf("comparisonSatisfiesFacet(%d, %q) = %t, want %t", test.comparison, test.kind, got, test.want)
		}
	}
}

func TestValidateIDReferencesBranches(t *testing.T) {
	t.Parallel()

	validator := &Validator{limits: Limits{MaxDiagnostics: 2}}
	state := validationState{
		validator: validator,
		ids:       map[string]struct{}{"known": {}},
		idReferences: []idReference{
			{value: "known", path: "/root/@known"},
			{value: "missing", path: "/root/@missing"},
		},
	}
	if err := state.validateIDReferences(); err != nil ||
		len(state.diagnostics) != 1 ||
		state.diagnostics[0].Path != "/root/@missing" {
		t.Fatalf("validateIDReferences() = %#v, %v", state.diagnostics, err)
	}
	state = validationState{
		validator:    &Validator{limits: Limits{MaxDiagnostics: 0}},
		ids:          map[string]struct{}{},
		idReferences: []idReference{{value: "missing"}},
	}
	if err := state.validateIDReferences(); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateIDReferences() error = %v", err)
	}
}

func TestParseXMLFloatSpecialValues(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		lexical string
		want    float64
		valid   bool
		nan     bool
	}{
		{lexical: "INF", want: math.Inf(1), valid: true},
		{lexical: "-INF", want: math.Inf(-1), valid: true},
		{lexical: "NaN", valid: true, nan: true},
		{lexical: "1.5", want: 1.5, valid: true},
		{lexical: "invalid"},
	} {
		got, valid := parseXMLFloat(test.lexical, 64)
		if valid != test.valid || test.nan != math.IsNaN(got) ||
			!test.nan && valid && got != test.want {
			t.Fatalf("parseXMLFloat(%q) = %v, %t", test.lexical, got, valid)
		}
	}
}
