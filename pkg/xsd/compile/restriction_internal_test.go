package compile

import (
	"strings"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestValidateComplexRestrictionBranches(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	baseAttribute := xsd.AttributeUse{
		Name: "required",
		Type: stringType,
		Use:  xsd.AttributeRequired,
	}
	base := xsd.ComplexType{
		Content:           group(xsd.Sequence, elementParticle("value", 0, 1)),
		Attributes:        []xsd.AttributeUse{baseAttribute},
		AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:a", "urn:b"}, ProcessContents: xsd.ProcessLax},
	}
	valid := xsd.ComplexType{
		Content:           group(xsd.Sequence, elementParticle("value", 1, 1)),
		Attributes:        []xsd.AttributeUse{baseAttribute},
		AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:a"}, ProcessContents: xsd.ProcessStrict},
	}
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{}}
	for _, test := range []struct {
		name      string
		derived   xsd.ComplexType
		base      xsd.ComplexType
		wantError string
	}{
		{name: "valid", derived: valid, base: base},
		{name: "mixed content", derived: xsd.ComplexType{Mixed: true}, wantError: "mixed"},
		{name: "content model", derived: xsd.ComplexType{Content: group(xsd.Choice)}, base: base, wantError: "content model"},
		{name: "attribute uses", derived: xsd.ComplexType{Content: base.Content}, base: base, wantError: "attribute uses"},
		{name: "wildcard without base", derived: xsd.ComplexType{AttributeWildcard: valid.AttributeWildcard}, wantError: "wildcard"},
		{name: "wildcard expansion", derived: xsd.ComplexType{AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:c"}}}, base: xsd.ComplexType{AttributeWildcard: base.AttributeWildcard}, wantError: "wildcard"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := state.validateComplexRestriction(test.derived, test.base)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validateComplexRestriction() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateComplexRestriction() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestExpandAttributeGroupBranches(t *testing.T) {
	t.Parallel()

	baseName := xsd.QName{Namespace: "urn:test", Local: "Base"}
	cloneName := xsd.QName{Namespace: "urn:test", Local: "Clone"}
	intersectName := xsd.QName{Namespace: "urn:test", Local: "Intersect"}
	missingName := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	state := &compileState{attributeGroups: map[xsd.QName]xsd.AttributeGroup{
		baseName: {
			Attributes: []xsd.AttributeUse{{Name: "base"}},
			Wildcard:   &xsd.Wildcard{Namespaces: []string{"urn:a", "urn:b"}},
		},
		cloneName: {
			Attributes: []xsd.AttributeUse{{Name: "clone"}},
			References: []xsd.QName{baseName},
		},
		intersectName: {
			References: []xsd.QName{baseName},
			Wildcard:   &xsd.Wildcard{Namespaces: []string{"urn:b", "urn:c"}},
		},
	}}
	if _, err := state.expandAttributeGroup(baseName, map[xsd.QName]uint8{baseName: 1}); err == nil {
		t.Fatal("expandAttributeGroup() accepted a recursive reference")
	}
	if group, err := state.expandAttributeGroup(baseName, map[xsd.QName]uint8{baseName: 2}); err != nil || len(group.Attributes) != 1 {
		t.Fatalf("cached expandAttributeGroup() = %#v, %v", group, err)
	}
	if _, err := state.expandAttributeGroup(missingName, map[xsd.QName]uint8{}); err == nil {
		t.Fatal("expandAttributeGroup() found a missing group")
	}
	brokenName := xsd.QName{Namespace: "urn:test", Local: "Broken"}
	state.attributeGroups[brokenName] = xsd.AttributeGroup{References: []xsd.QName{missingName}}
	if _, err := state.expandAttributeGroup(brokenName, map[xsd.QName]uint8{}); err == nil {
		t.Fatal("expandAttributeGroup() accepted a missing reference")
	}
	cloned, err := state.expandAttributeGroup(cloneName, map[xsd.QName]uint8{})
	if err != nil || len(cloned.Attributes) != 2 || cloned.Wildcard == nil ||
		len(cloned.References) != 0 {
		t.Fatalf("cloned group = %#v, %v", cloned, err)
	}
	intersected, err := state.expandAttributeGroup(intersectName, map[xsd.QName]uint8{})
	if err != nil || intersected.Wildcard == nil ||
		len(intersected.Wildcard.Namespaces) != 1 ||
		intersected.Wildcard.Namespaces[0] != "urn:b" {
		t.Fatalf("intersected group = %#v, %v", intersected, err)
	}
}

func TestAttributeRestrictionRules(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	tokenType := xsd.QName{Namespace: xsd.Namespace, Local: "token"}
	base := xsd.AttributeUse{Name: "value", Type: stringType}
	required := base
	required.Use = xsd.AttributeRequired
	fixed := base
	fixed.Fixed = "locked"
	fixed.FixedSet = true
	global := xsd.QName{Namespace: "urn:test", Local: "global"}
	globalInline := xsd.QName{Namespace: "urn:test", Local: "inline"}
	globalUntyped := xsd.QName{Namespace: "urn:test", Local: "untyped"}
	state := &compileState{
		simpleTypes: map[xsd.QName]xsd.SimpleType{},
		attributes: map[xsd.QName]xsd.Attribute{global: {
			Name: "global", Type: stringType, Fixed: "locked", FixedSet: true,
		}, globalInline: {
			Name: "inline",
			InlineSimpleType: &xsd.SimpleType{
				Variety: xsd.SimpleRestriction,
				Base:    stringType,
			},
		}, globalUntyped: {Name: "untyped"}},
	}
	for _, test := range []struct {
		name     string
		derived  []xsd.AttributeUse
		base     []xsd.AttributeUse
		wildcard *xsd.Wildcard
		want     bool
	}{
		{name: "optional omitted", base: []xsd.AttributeUse{base}, want: true},
		{name: "required omitted", base: []xsd.AttributeUse{required}},
		{name: "matching named type", derived: []xsd.AttributeUse{base}, base: []xsd.AttributeUse{base}, want: true},
		{name: "reference mismatch", derived: []xsd.AttributeUse{{Ref: xsd.QName{Local: "value"}}}, base: []xsd.AttributeUse{base}},
		{name: "fixed removed", derived: []xsd.AttributeUse{base}, base: []xsd.AttributeUse{fixed}},
		{name: "fixed changed", derived: []xsd.AttributeUse{{Name: "value", Type: stringType, Fixed: "changed"}}, base: []xsd.AttributeUse{fixed}},
		{name: "required weakened", derived: []xsd.AttributeUse{base}, base: []xsd.AttributeUse{required}},
		{name: "optional prohibited", derived: []xsd.AttributeUse{{Name: "value", Use: xsd.AttributeProhibited}}, base: []xsd.AttributeUse{base}, want: true},
		{name: "inline restriction", derived: []xsd.AttributeUse{{Name: "value", InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: tokenType}}}, base: []xsd.AttributeUse{base}, want: true},
		{name: "nested inline restriction", derived: []xsd.AttributeUse{{Name: "value", InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleRestriction, InlineBase: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: tokenType}}}}, base: []xsd.AttributeUse{base}, want: true},
		{name: "inline invalid variety", derived: []xsd.AttributeUse{{Name: "value", InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleList}}}, base: []xsd.AttributeUse{base}},
		{name: "inline unrelated type", derived: []xsd.AttributeUse{{Name: "value", InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}}}}, base: []xsd.AttributeUse{base}},
		{name: "named unrelated type", derived: []xsd.AttributeUse{{Name: "value", Type: xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}}}, base: []xsd.AttributeUse{base}},
		{name: "extra without wildcard", derived: []xsd.AttributeUse{{Name: "extra", Type: stringType}}},
		{name: "extra with wildcard", derived: []xsd.AttributeUse{{Name: "extra", Type: stringType}}, wildcard: &xsd.Wildcard{Namespaces: []string{"##any"}}, want: true},
		{name: "extra outside wildcard", derived: []xsd.AttributeUse{{Name: "extra", Type: stringType}}, wildcard: &xsd.Wildcard{Namespaces: []string{"urn:allowed"}}},
		{name: "matching global reference", derived: []xsd.AttributeUse{{Ref: global}}, base: []xsd.AttributeUse{{Ref: global}}, want: true},
		{name: "global fixed changed", derived: []xsd.AttributeUse{{Ref: global, Fixed: "changed", FixedSet: true}}, base: []xsd.AttributeUse{{Ref: global}}},
		{name: "global redeclared with restriction", derived: []xsd.AttributeUse{{Name: "global", Namespace: "urn:test", Type: tokenType, Fixed: "locked", FixedSet: true}}, base: []xsd.AttributeUse{{Ref: global}}, want: true},
		{name: "anonymous base declaration cannot be redeclared", derived: []xsd.AttributeUse{{Name: "inline", Namespace: "urn:test", Type: tokenType}}, base: []xsd.AttributeUse{{Ref: globalInline}}},
		{name: "untyped base declaration is any simple type", derived: []xsd.AttributeUse{{Name: "untyped", Namespace: "urn:test", Type: tokenType}}, base: []xsd.AttributeUse{{Ref: globalUntyped}}, want: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := state.attributesRestrict(test.derived, test.base, test.wildcard); got != test.want {
				t.Fatalf("attributesRestrict() = %t, want %t", got, test.want)
			}
		})
	}
	qualified := xsd.AttributeUse{Name: "extra", Namespace: "urn:test", Type: stringType}
	targetWildcard := &xsd.Wildcard{Namespaces: []string{"##targetNamespace"}}
	if !state.attributesRestrictContext(
		[]xsd.AttributeUse{qualified},
		nil,
		targetWildcard,
		"urn:test",
	) {
		t.Fatal("attributesRestrictContext(target namespace) = false")
	}
}

func TestSimpleTypeDerivationRules(t *testing.T) {
	t.Parallel()

	base := xsd.QName{Namespace: "urn:test", Local: "Base"}
	derived := xsd.QName{Namespace: "urn:test", Local: "Derived"}
	cycleA := xsd.QName{Namespace: "urn:test", Local: "CycleA"}
	cycleB := xsd.QName{Namespace: "urn:test", Local: "CycleB"}
	state := &compileState{simpleTypes: map[xsd.QName]xsd.SimpleType{
		base:    {Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}},
		derived: {Base: base},
		cycleA:  {Base: cycleB},
		cycleB:  {Base: cycleA},
	}}
	for _, test := range []struct {
		name    string
		derived xsd.QName
		base    xsd.QName
		want    bool
	}{
		{name: "empty base", derived: derived, want: true},
		{name: "empty derived", base: base},
		{name: "any simple base", derived: derived, base: xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}, want: true},
		{name: "named chain", derived: derived, base: base, want: true},
		{name: "built in chain", derived: xsd.QName{Namespace: xsd.Namespace, Local: "token"}, base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, want: true},
		{name: "built in list", derived: xsd.QName{Namespace: xsd.Namespace, Local: "IDREFS"}, base: xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}, want: true},
		{name: "unknown built in", derived: xsd.QName{Namespace: xsd.Namespace, Local: "unknown"}, base: base},
		{name: "unknown external", derived: xsd.QName{Namespace: "urn:test", Local: "Missing"}, base: base},
		{name: "cycle", derived: cycleA, base: base},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := state.simpleTypeDerivesFrom(test.derived, test.base); got != test.want {
				t.Fatalf("simpleTypeDerivesFrom() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestModelGroupRestrictionRules(t *testing.T) {
	t.Parallel()

	required := elementParticle("required", 1, 1)
	optional := elementParticle("optional", 0, 1)
	for _, test := range []struct {
		name    string
		derived *xsd.ModelGroup
		base    *xsd.ModelGroup
		want    bool
	}{
		{name: "both empty", want: true},
		{name: "empty restricts nullable", base: group(xsd.Sequence, optional), want: true},
		{name: "empty cannot restrict required", base: group(xsd.Sequence, required)},
		{name: "nonempty cannot restrict empty", derived: group(xsd.Sequence, required)},
		{name: "compositor must match", derived: group(xsd.Choice, required), base: group(xsd.Sequence, required)},
		{name: "sequence may omit optional prefix", derived: group(xsd.Sequence, required), base: group(xsd.Sequence, optional, required), want: true},
		{name: "sequence cannot omit required prefix", derived: group(xsd.Sequence, required), base: group(xsd.Sequence, required, required)},
		{name: "sequence cannot skip a required mismatch", derived: group(xsd.Sequence, elementParticle("other", 1, 1)), base: group(xsd.Sequence, required)},
		{name: "sequence may omit optional suffix", derived: group(xsd.Sequence, required), base: group(xsd.Sequence, required, optional), want: true},
		{name: "sequence cannot add a term", derived: group(xsd.Sequence, required, required), base: group(xsd.Sequence, required)},
		{name: "choice terms must exist in base", derived: group(xsd.Choice, required), base: group(xsd.Choice, required), want: true},
		{name: "choice rejects unknown term", derived: group(xsd.Choice, elementParticle("other", 1, 1)), base: group(xsd.Choice, required)},
		{name: "all may omit optional term", derived: group(xsd.All, required), base: group(xsd.All, required, optional), want: true},
		{name: "all cannot omit required term", derived: group(xsd.All, optional), base: group(xsd.All, required, optional)},
		{name: "unknown compositor is invalid", derived: group("invalid"), base: group("invalid")},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := modelGroupRestricts(test.derived, test.base); got != test.want {
				t.Fatalf("modelGroupRestricts() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestParticleRestrictionRules(t *testing.T) {
	t.Parallel()

	base := elementParticle("value", 1, 2)
	for _, test := range []struct {
		name    string
		derived xsd.Particle
		base    xsd.Particle
		want    bool
	}{
		{name: "same element", derived: base, base: base, want: true},
		{name: "minimum cannot widen", derived: elementParticle("value", 0, 2), base: base},
		{name: "maximum cannot widen", derived: elementParticle("value", 1, 3), base: base},
		{name: "finite cannot become unbounded", derived: unboundedElement("value", 1), base: base},
		{name: "unbounded base permits finite", derived: base, base: unboundedElement("value", 1), want: true},
		{name: "element term must match", derived: elementParticle("other", 1, 2), base: base},
		{name: "nested groups restrict", derived: groupParticle(group(xsd.Sequence, elementParticle("value", 1, 1))), base: groupParticle(group(xsd.Sequence, elementParticle("value", 0, 2))), want: true},
		{name: "wildcards restrict", derived: wildcardParticle("urn:a"), base: wildcardParticle("urn:a", "urn:b"), want: true},
		{name: "term kinds must match", derived: elementParticle("value", 1, 1), base: wildcardParticle("##any")},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := particleRestricts(test.derived, test.base); got != test.want {
				t.Fatalf("particleRestricts() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestModelGroupNullability(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		group *xsd.ModelGroup
		want  bool
	}{
		{name: "nil", want: true},
		{name: "empty", group: group(xsd.Sequence), want: true},
		{name: "choice with optional term", group: group(xsd.Choice, elementParticle("required", 1, 1), elementParticle("optional", 0, 1)), want: true},
		{name: "required choice", group: group(xsd.Choice, elementParticle("required", 1, 1))},
		{name: "nullable sequence", group: group(xsd.Sequence, elementParticle("optional", 0, 1)), want: true},
		{name: "required sequence", group: group(xsd.Sequence, elementParticle("required", 1, 1))},
		{name: "nullable all", group: group(xsd.All, elementParticle("optional", 0, 1)), want: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := modelGroupNullable(test.group); got != test.want {
				t.Fatalf("modelGroupNullable() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestElementTermRestrictionRules(t *testing.T) {
	t.Parallel()

	name := xsd.QName{Namespace: "urn:test", Local: "value"}
	if !elementTermEqual(xsd.Element{Ref: name}, xsd.Element{Ref: name}) {
		t.Fatal("equal references did not match")
	}
	if elementTermEqual(xsd.Element{Ref: name}, xsd.Element{Name: "value"}) {
		t.Fatal("reference matched a local declaration")
	}
	base := xsd.Element{Name: "value", Type: xsd.QName{Namespace: xsd.Namespace, Local: "string"}, Fixed: "x", FixedSet: true}
	if elementTermEqual(xsd.Element{Name: "value", Type: base.Type}, base) {
		t.Fatal("restriction removed a fixed value")
	}
	derived := base
	if !elementTermEqual(derived, base) {
		t.Fatal("equal fixed declarations did not match")
	}
	derived.Nillable = true
	if elementTermEqual(derived, base) {
		t.Fatal("restriction changed nillability")
	}
	inlineBase := xsd.Element{
		Name:             "value",
		InlineSimpleType: &xsd.SimpleType{Variety: xsd.SimpleRestriction},
	}
	if elementTermEqual(xsd.Element{Name: "value"}, inlineBase) {
		t.Fatal("restriction discarded an anonymous base type")
	}
}

func TestWildcardSetOperationsAndOverlap(t *testing.T) {
	t.Parallel()

	strictAny := &xsd.Wildcard{Namespaces: []string{"##any"}, ProcessContents: xsd.ProcessStrict}
	laxA := &xsd.Wildcard{Namespaces: []string{"urn:a"}, ProcessContents: xsd.ProcessLax}
	skipAB := &xsd.Wildcard{Namespaces: []string{"urn:a", "urn:b"}, ProcessContents: xsd.ProcessSkip}
	if got := intersectWildcards(strictAny, laxA); got.ProcessContents != xsd.ProcessLax || len(got.Namespaces) != 1 {
		t.Fatalf("intersectWildcards(any, a) = %#v", got)
	}
	if got := intersectWildcards(laxA, strictAny); got.ProcessContents != xsd.ProcessLax || len(got.Namespaces) != 1 {
		t.Fatalf("intersectWildcards(a, any) = %#v", got)
	}
	if got := intersectWildcards(laxA, &xsd.Wildcard{Namespaces: []string{"urn:b"}}); got != nil {
		t.Fatalf("disjoint intersection = %#v", got)
	}
	if got := intersectWildcards(skipAB, laxA); len(got.Namespaces) != 1 || got.ProcessContents != xsd.ProcessLax {
		t.Fatalf("intersection = %#v", got)
	}
	if got := unionWildcards(strictAny, laxA); !wildcardHas(got, "##any") || got.ProcessContents != xsd.ProcessLax {
		t.Fatalf("union(any, a) = %#v", got)
	}
	if got := unionWildcards(laxA, skipAB); len(got.Namespaces) != 2 || got.ProcessContents != xsd.ProcessSkip {
		t.Fatalf("union(a, ab) = %#v", got)
	}

	a := xsd.QName{Namespace: "urn:a", Local: "value"}
	b := xsd.QName{Namespace: "urn:b", Local: "value"}
	if !nameClassesOverlap(nameClass{name: &a}, nameClass{name: &a}, "urn:test") ||
		nameClassesOverlap(nameClass{name: &a}, nameClass{name: &b}, "urn:test") {
		t.Fatal("exact name overlap is incorrect")
	}
	if !nameClassesOverlap(nameClass{name: &a}, nameClass{wildcard: laxA}, "urn:test") ||
		!nameClassesOverlap(nameClass{wildcard: laxA}, nameClass{name: &a}, "urn:test") {
		t.Fatal("name and wildcard overlap is incorrect")
	}
	if nameClassesOverlap(nameClass{name: &b}, nameClass{wildcard: laxA}, "urn:test") {
		t.Fatal("disjoint name and wildcard overlap")
	}
	local := &xsd.Wildcard{Namespaces: []string{"##local"}}
	other := &xsd.Wildcard{Namespaces: []string{"##other"}}
	if nameClassesOverlap(nameClass{wildcard: local}, nameClass{wildcard: other}, "urn:test") {
		t.Fatal("disjoint wildcards overlap")
	}
	if !nameClassesOverlap(nameClass{wildcard: laxA}, nameClass{wildcard: skipAB}, "urn:test") {
		t.Fatal("explicit wildcard intersection was missed")
	}
}

func TestWildcardRestrictionAndNamespaceRules(t *testing.T) {
	t.Parallel()

	wildcard := func(process xsd.ProcessContents, namespaces ...string) *xsd.Wildcard {
		return &xsd.Wildcard{Namespaces: namespaces, ProcessContents: process}
	}
	for _, test := range []struct {
		name    string
		derived *xsd.Wildcard
		base    *xsd.Wildcard
		want    bool
	}{
		{name: "same", derived: wildcard(xsd.ProcessStrict, "urn:a"), base: wildcard(xsd.ProcessStrict, "urn:a"), want: true},
		{name: "subset", derived: wildcard(xsd.ProcessStrict, "urn:a"), base: wildcard(xsd.ProcessStrict, "urn:a", "urn:b"), want: true},
		{name: "namespace expansion", derived: wildcard(xsd.ProcessStrict, "urn:b"), base: wildcard(xsd.ProcessStrict, "urn:a")},
		{name: "strict to lax", derived: wildcard(xsd.ProcessLax, "urn:a"), base: wildcard(xsd.ProcessStrict, "urn:a")},
		{name: "strict to skip", derived: wildcard(xsd.ProcessSkip, "urn:a"), base: wildcard(xsd.ProcessStrict, "urn:a")},
		{name: "skip remains skip", derived: wildcard(xsd.ProcessSkip, "urn:a"), base: wildcard(xsd.ProcessSkip, "urn:a"), want: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := wildcardRestricts(test.derived, test.base); got != test.want {
				t.Fatalf("wildcardRestricts() = %t, want %t", got, test.want)
			}
		})
	}

	target := "urn:target"
	for _, test := range []struct {
		name      string
		wildcard  *xsd.Wildcard
		namespace string
		want      bool
	}{
		{name: "nil", namespace: "urn:a"},
		{name: "any", wildcard: wildcard(xsd.ProcessStrict, "##any"), namespace: target, want: true},
		{name: "other", wildcard: wildcard(xsd.ProcessStrict, "##other"), namespace: "urn:other", want: true},
		{name: "other excludes local", wildcard: wildcard(xsd.ProcessStrict, "##other")},
		{name: "other excludes target", wildcard: wildcard(xsd.ProcessStrict, "##other"), namespace: target},
		{name: "local", wildcard: wildcard(xsd.ProcessStrict, "##local"), want: true},
		{name: "local excludes named", wildcard: wildcard(xsd.ProcessStrict, "##local"), namespace: "urn:a"},
		{name: "target", wildcard: wildcard(xsd.ProcessStrict, "##targetNamespace"), namespace: target, want: true},
		{name: "target excludes other", wildcard: wildcard(xsd.ProcessStrict, "##targetNamespace"), namespace: "urn:a"},
		{name: "explicit", wildcard: wildcard(xsd.ProcessStrict, "urn:a"), namespace: "urn:a", want: true},
		{name: "explicit mismatch", wildcard: wildcard(xsd.ProcessStrict, "urn:a"), namespace: "urn:b"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := wildcardAllows(test.wildcard, test.namespace, target); got != test.want {
				t.Fatalf("wildcardAllows() = %t, want %t", got, test.want)
			}
		})
	}
}

func group(compositor xsd.Compositor, particles ...xsd.Particle) *xsd.ModelGroup {
	return &xsd.ModelGroup{Compositor: compositor, Particles: particles}
}

func elementParticle(name string, minimum uint64, maximum uint64) xsd.Particle {
	return xsd.Particle{
		MinOccurs: minimum,
		MaxOccurs: maximum,
		Element:   &xsd.Element{Name: name},
	}
}

func unboundedElement(name string, minimum uint64) xsd.Particle {
	particle := elementParticle(name, minimum, 0)
	particle.Unbounded = true
	return particle
}

func groupParticle(value *xsd.ModelGroup) xsd.Particle {
	return xsd.Particle{MinOccurs: 1, MaxOccurs: 1, Group: value}
}

func wildcardParticle(namespaces ...string) xsd.Particle {
	return xsd.Particle{
		MinOccurs: 1,
		MaxOccurs: 1,
		Wildcard: &xsd.Wildcard{
			Namespaces:      namespaces,
			ProcessContents: xsd.ProcessStrict,
		},
	}
}
