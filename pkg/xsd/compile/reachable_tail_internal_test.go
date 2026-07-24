package compile

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestCompilerAndSetReachableBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{Limits: Limits{MaxParticles: -1}}); err == nil {
		t.Fatal("New(negative MaxParticles) succeeded")
	}
	set := &Set{modelGroups: map[xsd.QName]xsd.ModelGroupDefinition{}}
	if _, ok := set.ModelGroup(xsd.QName{Local: "missing"}); ok {
		t.Fatal("ModelGroup(missing) succeeded")
	}
	compiler, err := New(Options{Limits: Limits{MaxBytes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []Source{
		{URI: "%", Content: []byte(`<schema/>`)},
		{URI: "https://example.test/schema.xsd", Content: []byte(`<schema/>`)},
	} {
		if _, err := compiler.Compile(context.Background(), source); err == nil {
			t.Fatalf("Compile(%#v) succeeded", source)
		}
	}
}

func TestSubstitutionMemberRejectsInvalidAffiliationChains(t *testing.T) {
	t.Parallel()

	head := xsd.QName{Namespace: "urn:test", Local: "Head"}
	member := xsd.QName{Namespace: "urn:test", Local: "Member"}
	intermediate := xsd.QName{Namespace: "urn:test", Local: "Intermediate"}
	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	for _, set := range []*Set{
		{
			elements:          map[xsd.QName]xsd.Element{head: {Type: stringType}},
			substitutionHeads: map[xsd.QName]xsd.QName{member: head},
			complexTypes:      map[xsd.QName]xsd.ComplexType{}, simpleTypes: map[xsd.QName]xsd.SimpleType{},
		},
		{
			elements: map[xsd.QName]xsd.Element{
				head: {Type: stringType}, member: {Type: xsd.QName{Namespace: "urn:test", Local: "Missing"}},
			},
			substitutionHeads: map[xsd.QName]xsd.QName{member: head},
			complexTypes:      map[xsd.QName]xsd.ComplexType{}, simpleTypes: map[xsd.QName]xsd.SimpleType{},
		},
		{
			elements:          map[xsd.QName]xsd.Element{head: {Type: stringType}, member: {Type: stringType}},
			substitutionHeads: map[xsd.QName]xsd.QName{member: intermediate, intermediate: head},
			complexTypes:      map[xsd.QName]xsd.ComplexType{}, simpleTypes: map[xsd.QName]xsd.SimpleType{},
		},
		{
			elements:          map[xsd.QName]xsd.Element{head: {Type: stringType}, member: {Type: stringType}, intermediate: {Type: stringType}},
			substitutionHeads: map[xsd.QName]xsd.QName{member: intermediate},
			complexTypes:      map[xsd.QName]xsd.ComplexType{}, simpleTypes: map[xsd.QName]xsd.SimpleType{},
		},
	} {
		if _, ok := set.SubstitutionMember(head, member); ok {
			t.Fatalf("SubstitutionMember() accepted %#v", set)
		}
	}
}

func TestCompileSubstitutionsRejectsMissingAndUnrelatedTypes(t *testing.T) {
	t.Parallel()

	head := xsd.QName{Namespace: "urn:test", Local: "Head"}
	member := xsd.QName{Namespace: "urn:test", Local: "Member"}
	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	for _, state := range []compileState{
		{
			elements:          map[xsd.QName]xsd.Element{member: {SubstitutionGroup: missing}},
			substitutionHeads: map[xsd.QName]xsd.QName{},
		},
		{
			elements: map[xsd.QName]xsd.Element{
				head:   {Type: xsd.QName{Namespace: xsd.Namespace, Local: "string"}},
				member: {Type: missing, SubstitutionGroup: head},
			},
			substitutionHeads: map[xsd.QName]xsd.QName{},
			simpleTypes:       map[xsd.QName]xsd.SimpleType{}, complexTypes: map[xsd.QName]xsd.ComplexType{},
		},
	} {
		state := state
		if err := state.compileSubstitutions(); err == nil {
			t.Fatal("compileSubstitutions() succeeded")
		}
	}
}

func TestAnonymousComplexTypeReachableBranches(t *testing.T) {
	t.Parallel()

	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	anyType := xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}
	state := compileState{
		modelGroups:     map[xsd.QName]xsd.ModelGroupDefinition{},
		attributeGroups: map[xsd.QName]xsd.AttributeGroup{},
		complexTypes:    map[xsd.QName]xsd.ComplexType{},
		simpleTypes:     map[xsd.QName]xsd.SimpleType{},
		typeKinds:       map[xsd.QName]string{},
	}
	for _, definition := range []xsd.ComplexType{
		{Derivation: xsd.DerivationExtension},
		{Derivation: "invalid", Base: anyType},
		{Derivation: xsd.DerivationExtension, Base: anyType},
		{Derivation: xsd.DerivationExtension, Base: missing},
	} {
		definition := definition
		err := state.expandAnonymousComplexType(&definition)
		if definition.Base == anyType && definition.Derivation == xsd.DerivationExtension {
			if err != nil {
				t.Fatalf("expandAnonymousComplexType(anyType) error = %v", err)
			}
		} else if err == nil {
			t.Fatalf("expandAnonymousComplexType(%#v) succeeded", definition)
		}
	}

	element := xsd.Element{InlineComplexType: &xsd.ComplexType{Content: &xsd.ModelGroup{
		Particles: []xsd.Particle{{Element: &xsd.Element{InlineComplexType: &xsd.ComplexType{Base: missing, Derivation: xsd.DerivationExtension}}}},
	}}}
	if err := state.compileElementAnonymousType(&element, "urn:test"); err == nil {
		t.Fatal("compileElementAnonymousType(nested error) succeeded")
	}
}

func TestParticleAndAttributeHelpersCoverRemainingBranches(t *testing.T) {
	t.Parallel()

	base := []xsd.AttributeUse{{Name: "base"}}
	got := restrictedAttributes(base, []xsd.AttributeUse{{Name: "derived"}})
	if len(got) != 2 || got[1].Name != "derived" {
		t.Fatalf("restrictedAttributes() = %#v", got)
	}
	if !particleNullable(xsd.Particle{MinOccurs: 1, Group: &xsd.ModelGroup{
		Compositor: xsd.Choice, Particles: []xsd.Particle{{MinOccurs: 0}},
	}}) {
		t.Fatal("particleNullable(nullable choice) = false")
	}
	if particleNullable(xsd.Particle{MinOccurs: 1, Group: &xsd.ModelGroup{
		Compositor: xsd.Choice, Particles: []xsd.Particle{{MinOccurs: 1}},
	}}) {
		t.Fatal("particleNullable(required choice) = true")
	}
	if !particleNullable(xsd.Particle{MinOccurs: 1, Group: &xsd.ModelGroup{
		Compositor: xsd.Sequence, Particles: []xsd.Particle{{MinOccurs: 0}},
	}}) {
		t.Fatal("particleNullable(nullable sequence) = false")
	}
	if particleNullable(xsd.Particle{MinOccurs: 1, Group: &xsd.ModelGroup{
		Compositor: xsd.All, Particles: []xsd.Particle{{MinOccurs: 1}},
	}}) {
		t.Fatal("particleNullable(required all) = true")
	}
	if particleNullable(xsd.Particle{MinOccurs: 1, Group: &xsd.ModelGroup{Compositor: "invalid"}}) {
		t.Fatal("particleNullable(invalid compositor) = true")
	}
}

func TestCompilerCoversRemainingAnonymousAndRecursiveBranches(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	listName := xsd.QName{Namespace: "urn:test", Local: "List"}
	state := emptyValidationState()
	state.simpleTypes[listName] = xsd.SimpleType{Variety: xsd.SimpleList, ItemType: stringType}
	if methods, ok := state.typeDerivationMethods(listName, xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}); !ok || len(methods) != 1 || methods[0] != xsd.DerivationList {
		t.Fatalf("typeDerivationMethods(List) = %#v, %t", methods, ok)
	}

	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	nested := &xsd.ModelGroup{Particles: []xsd.Particle{{Group: &xsd.ModelGroup{
		Particles: []xsd.Particle{{Element: &xsd.Element{InlineComplexType: &xsd.ComplexType{
			Base: missing, Derivation: xsd.DerivationExtension,
		}}}},
	}}}}
	if err := state.compileAnonymousTypesInGroup(nested, "urn:test"); err == nil {
		t.Fatal("compileAnonymousTypesInGroup(nested error) succeeded")
	}
	state.complexTypes[xsd.QName{Namespace: "urn:test", Local: "Root"}] = xsd.ComplexType{Content: nested}
	if err := state.compileAnonymousComplexTypes(); err == nil {
		t.Fatal("compileAnonymousComplexTypes() succeeded")
	}
	if _, err := state.expandModelGroupContent(&xsd.ModelGroup{Particles: []xsd.Particle{{
		Group: &xsd.ModelGroup{Particles: []xsd.Particle{{GroupRef: missing}}},
	}}}, map[xsd.QName]uint8{}); err == nil {
		t.Fatal("expandModelGroupContent(nested missing reference) succeeded")
	}

	baseName := xsd.QName{Namespace: "urn:test", Local: "Base"}
	state.complexTypes[baseName] = xsd.ComplexType{SimpleContent: true, SimpleBase: stringType}
	definition := xsd.ComplexType{SimpleContent: true, Base: baseName, Derivation: xsd.DerivationExtension}
	if err := state.expandAnonymousComplexType(&definition); err != nil {
		t.Fatalf("expandAnonymousComplexType(simple content) error = %v", err)
	}

	state.complexTypes[baseName] = xsd.ComplexType{}
	definition = xsd.ComplexType{Base: baseName, Derivation: xsd.DerivationExtension, Mixed: true, MixedSet: true}
	if err := state.expandAnonymousComplexType(&definition); err == nil {
		t.Fatal("expandAnonymousComplexType(mixed mismatch) succeeded")
	}
	definition = xsd.ComplexType{Base: baseName, Derivation: xsd.DerivationRestriction, Content: &xsd.ModelGroup{
		Compositor: xsd.Sequence, Particles: []xsd.Particle{{Element: &xsd.Element{Name: "extra"}}},
	}}
	if err := state.expandAnonymousComplexType(&definition); err == nil {
		t.Fatal("expandAnonymousComplexType(invalid restriction) succeeded")
	}

	name := xsd.QName{Namespace: "urn:test", Local: "AnyDerived"}
	state.complexTypes[name] = xsd.ComplexType{Base: xsd.QName{Namespace: xsd.Namespace, Local: "anyType"}, Derivation: xsd.DerivationExtension}
	if err := state.compileComplexType(name, map[xsd.QName]uint8{}); err != nil {
		t.Fatalf("compileComplexType(anyType) error = %v", err)
	}

	document, err := xsd.Parse(context.Background(), []byte(
		`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`+
			`<xs:complexType name="Base" final="extension"/>`+
			`</xs:schema>`,
	), xsd.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	state.complexTypes[baseName] = xsd.ComplexType{Final: document.ComplexTypes[0].Final}
	definition = xsd.ComplexType{Base: baseName, Derivation: xsd.DerivationExtension}
	if err := state.expandAnonymousComplexType(&definition); err == nil {
		t.Fatal("expandAnonymousComplexType(prohibited derivation) succeeded")
	}
}

func TestSubstitutionMemberStopsWhenAffiliationEnds(t *testing.T) {
	t.Parallel()

	head := xsd.QName{Namespace: "urn:test", Local: "Head"}
	member := xsd.QName{Namespace: "urn:test", Local: "Member"}
	set := &Set{
		elements: map[xsd.QName]xsd.Element{
			head: {}, member: {}, {}: {},
		},
		substitutionHeads: map[xsd.QName]xsd.QName{member: {}},
		complexTypes:      map[xsd.QName]xsd.ComplexType{},
		simpleTypes:       map[xsd.QName]xsd.SimpleType{},
	}
	if _, ok := set.SubstitutionMember(head, member); ok {
		t.Fatal("SubstitutionMember() succeeded")
	}
}
