package compile

import (
	"context"
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestCompilerResourceLimitsAcceptExactBoundaryAndRejectOneBeyond(t *testing.T) {
	t.Parallel()

	root := Source{
		URI:     "https://example.test/root.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`),
	}
	compiler, err := New(Options{Limits: Limits{MaxBytes: int64(len(root.Content))}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(context.Background(), root); err != nil {
		t.Fatalf("Compile(exact byte limit) error = %v", err)
	}
	compiler, err = New(Options{Limits: Limits{MaxBytes: int64(len(root.Content) - 1)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(context.Background(), root); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Compile(over byte limit) error = %v", err)
	}
}

func TestLoadAccumulatesBytesAtTheExactLimit(t *testing.T) {
	t.Parallel()

	const uri = "https://example.test/dependency.xsd"
	content := []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	state := loadState(t, staticResolver(resolve.Resource{URI: uri, Content: content}), int64(len(content)+1))
	state.bytes = 1
	if _, _, err := state.load(context.Background(), xsd.SchemaReference{URI: uri}); err != nil {
		t.Fatalf("load(exact cumulative limit) error = %v", err)
	}
	if want := int64(len(content) + 1); state.bytes != want {
		t.Fatalf("load() bytes = %d, want %d", state.bytes, want)
	}

	state = loadState(t, staticResolver(resolve.Resource{URI: uri, Content: content}), int64(len(content)))
	state.bytes = 1
	if _, _, err := state.load(context.Background(), xsd.SchemaReference{URI: uri}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("load(over cumulative limit) error = %v", err)
	}
}

func TestCompileDocumentCountersUseExactLimits(t *testing.T) {
	t.Parallel()

	const rootURI = "https://example.test/root.xsd"
	const childURI = "https://example.test/child.xsd"
	newState := func() compileState {
		state := emptyValidationState()
		state.compiler.limits.MaxDepth = 1
		state.compiler.limits.MaxSchemas = 2
		state.compiler.limits.MaxReferences = 1
		state.resources = map[string]resourceDocument{
			rootURI:  {document: &xsd.Document{References: []xsd.SchemaReference{{Kind: xsd.ReferenceInclude, URI: childURI}}}},
			childURI: {document: &xsd.Document{}},
		}
		state.instances = map[instanceKey]*Document{}
		return state
	}

	state := newState()
	if err := state.compileDocument(context.Background(), rootURI, "", 0); err != nil {
		t.Fatalf("compileDocument(exact limits) error = %v", err)
	}
	if state.references != 1 || len(state.instances) != 2 {
		t.Fatalf("compileDocument counters = references %d, schemas %d", state.references, len(state.instances))
	}

	state = newState()
	if err := state.compileDocument(context.Background(), rootURI, "", 1); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("compileDocument(child beyond depth) error = %v", err)
	}
}

func TestComponentCounterUsesExactLimit(t *testing.T) {
	t.Parallel()

	state := emptyValidationState()
	state.compiler.limits.MaxComponents = 1
	if _, err := state.componentName("urn:test", "first", "element"); err != nil {
		t.Fatalf("componentName(exact limit) error = %v", err)
	}
	if state.components != 1 {
		t.Fatalf("component count = %d, want 1", state.components)
	}
	if _, err := state.componentName("urn:test", "second", "element"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("componentName(over limit) error = %v", err)
	}
}

func TestAnonymousElementRejectsNamedAndInlineComplexTypesTogether(t *testing.T) {
	t.Parallel()

	state := emptyValidationState()
	element := xsd.Element{
		Type:              xsd.QName{Namespace: xsd.Namespace, Local: "string"},
		InlineComplexType: &xsd.ComplexType{},
	}
	if err := state.validateAnonymousElementType(element, "", nil); !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("validateAnonymousElementType(multiple types) error = %v", err)
	}
}

func TestUPAStateTracksPositionsAndOccurrenceBoundaries(t *testing.T) {
	t.Parallel()

	state := upaState{follow: map[int]upaPositions{}}
	first := state.leaf(nameClass{})
	second := state.leaf(nameClass{})
	if _, ok := first.first[0]; !ok {
		t.Fatalf("first leaf positions = %#v", first.first)
	}
	if _, ok := second.first[1]; !ok || state.next != 2 {
		t.Fatalf("second leaf positions = %#v, next = %d", second.first, state.next)
	}

	state.follow = map[int]upaPositions{}
	state.occurs(first, 1, 1, false)
	if len(state.follow) != 0 {
		t.Fatalf("single occurrence follow = %#v", state.follow)
	}
	repeated := state.occurs(first, 1, 2, false)
	if len(state.follow[0]) != 1 || repeated.nullable {
		t.Fatalf("repeated occurrence = %#v, follow = %#v", repeated, state.follow)
	}
	optional := state.occurs(first, 0, 1, false)
	if !optional.nullable {
		t.Fatal("optional occurrence is not nullable")
	}
}

func TestUPAGroupComputesNullableStatesAndAllPairs(t *testing.T) {
	t.Parallel()

	state := upaState{compile: emptyValidationStatePointer(), follow: map[int]upaPositions{}}
	optional := xsd.Particle{MinOccurs: 0, MaxOccurs: 1, Element: &xsd.Element{Name: "optional"}}
	required := xsd.Particle{MinOccurs: 1, MaxOccurs: 1, Element: &xsd.Element{Name: "required"}}
	choice := state.group(&xsd.ModelGroup{Compositor: xsd.Choice, Particles: []xsd.Particle{required, optional}}, "")
	if !choice.nullable {
		t.Fatal("choice with an optional child is not nullable")
	}
	all := state.group(&xsd.ModelGroup{Compositor: xsd.All, Particles: []xsd.Particle{optional, required}}, "")
	if all.nullable {
		t.Fatal("all group with a required child is nullable")
	}
	sequence := state.group(&xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{optional, required}}, "")
	if sequence.nullable {
		t.Fatal("sequence with a required child is nullable")
	}
}

func emptyValidationStatePointer() *compileState {
	state := emptyValidationState()
	return &state
}

func TestExplicitAndImplicitValueConstraintFlags(t *testing.T) {
	t.Parallel()

	if !elementDefaultSet(xsd.Element{DefaultSet: true}) || !elementDefaultSet(xsd.Element{Default: "value"}) {
		t.Fatal("element default presence was not detected")
	}
	if !elementFixedSet(xsd.Element{FixedSet: true}) || !elementFixedSet(xsd.Element{Fixed: "value"}) {
		t.Fatal("element fixed presence was not detected")
	}
	if !attributeDefaultSet(xsd.AttributeUse{DefaultSet: true}) || !attributeDefaultSet(xsd.AttributeUse{Default: "value"}) {
		t.Fatal("attribute-use default presence was not detected")
	}
	if !attributeFixedSet(xsd.AttributeUse{FixedSet: true}) || !attributeFixedSet(xsd.AttributeUse{Fixed: "value"}) {
		t.Fatal("attribute-use fixed presence was not detected")
	}
	if !attributeDeclarationDefaultSet(xsd.Attribute{DefaultSet: true}) || !attributeDeclarationDefaultSet(xsd.Attribute{Default: "value"}) {
		t.Fatal("attribute declaration default presence was not detected")
	}
	if !attributeDeclarationFixedSet(xsd.Attribute{FixedSet: true}) || !attributeDeclarationFixedSet(xsd.Attribute{Fixed: "value"}) {
		t.Fatal("attribute declaration fixed presence was not detected")
	}
}

func TestAnonymousComplexTypeComposesReferencedWildcards(t *testing.T) {
	t.Parallel()

	first := xsd.QName{Namespace: "urn:test", Local: "First"}
	second := xsd.QName{Namespace: "urn:test", Local: "Second"}
	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	state := emptyValidationState()
	state.attributeGroups[first] = xsd.AttributeGroup{Wildcard: &xsd.Wildcard{Namespaces: []string{"urn:a", "urn:b"}}}
	state.attributeGroups[second] = xsd.AttributeGroup{Wildcard: &xsd.Wildcard{Namespaces: []string{"urn:b", "urn:c"}}}

	definition := xsd.ComplexType{AttributeGroupRefs: []xsd.QName{first, second}}
	if err := state.expandAnonymousComplexType(&definition); err != nil {
		t.Fatalf("expandAnonymousComplexType(wildcards) error = %v", err)
	}
	if definition.AttributeWildcard == nil || len(definition.AttributeWildcard.Namespaces) != 1 || definition.AttributeWildcard.Namespaces[0] != "urn:b" {
		t.Fatalf("composed wildcard = %#v", definition.AttributeWildcard)
	}

	definition = xsd.ComplexType{AttributeGroupRefs: []xsd.QName{missing}}
	if err := state.expandAnonymousComplexType(&definition); err == nil {
		t.Fatal("expandAnonymousComplexType(missing attribute group) succeeded")
	}
}

func TestAnonymousComplexExtensionComposesBaseWildcards(t *testing.T) {
	t.Parallel()

	baseName := xsd.QName{Namespace: "urn:test", Local: "Base"}
	state := emptyValidationState()
	state.complexTypes[baseName] = xsd.ComplexType{AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:a"}}}

	definition := xsd.ComplexType{Base: baseName, Derivation: xsd.DerivationExtension}
	if err := state.expandAnonymousComplexType(&definition); err != nil {
		t.Fatalf("expandAnonymousComplexType(inherited wildcard) error = %v", err)
	}
	if definition.AttributeWildcard == nil || !wildcardHas(definition.AttributeWildcard, "urn:a") {
		t.Fatalf("inherited wildcard = %#v", definition.AttributeWildcard)
	}

	definition = xsd.ComplexType{
		Base:              baseName,
		Derivation:        xsd.DerivationExtension,
		AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"urn:b"}},
	}
	if err := state.expandAnonymousComplexType(&definition); err != nil {
		t.Fatalf("expandAnonymousComplexType(union wildcard) error = %v", err)
	}
	if definition.AttributeWildcard == nil || len(definition.AttributeWildcard.Namespaces) != 2 {
		t.Fatalf("union wildcard = %#v", definition.AttributeWildcard)
	}
}

func TestSimpleContentRestrictionRejectsWildcardWithoutBaseWildcard(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	state := emptyValidationState()
	derived := xsd.ComplexType{
		Derivation:        xsd.DerivationRestriction,
		SimpleContent:     true,
		SimpleBase:        stringType,
		AttributeWildcard: &xsd.Wildcard{Namespaces: []string{"##any"}},
	}
	base := xsd.ComplexType{SimpleContent: true, SimpleBase: stringType}
	if err := state.applySimpleContentDerivation(&derived, base); err == nil {
		t.Fatal("applySimpleContentDerivation(wildcard without base) succeeded")
	}
}

func TestElementAndAttributeTypeDefaults(t *testing.T) {
	t.Parallel()

	state := emptyValidationState()
	baseRef := xsd.QName{Namespace: "urn:test", Local: "Base"}
	if state.elementTermRestricts(xsd.Element{Name: "value"}, xsd.Element{Ref: baseRef}) {
		t.Fatal("local element restricted a referenced base element")
	}
	if !state.elementTermRestricts(xsd.Element{Name: "value"}, xsd.Element{Name: "value"}) {
		t.Fatal("untyped local element did not restrict the default anyType base")
	}

	attributeRef := xsd.QName{Namespace: "urn:test", Local: "attribute"}
	state.attributes[attributeRef] = xsd.Attribute{Name: "attribute"}
	typeName, inline, ok := state.attributeUseType(xsd.AttributeUse{Ref: attributeRef})
	want := xsd.QName{Namespace: xsd.Namespace, Local: "anySimpleType"}
	if !ok || inline != nil || typeName != want {
		t.Fatalf("attributeUseType(untyped reference) = %#v, %#v, %t", typeName, inline, ok)
	}
}

func TestAnonymousComplexElementValidatesNestedAttributeUses(t *testing.T) {
	t.Parallel()

	state := emptyValidationState()
	particles := 0
	element := xsd.Element{InlineComplexType: &xsd.ComplexType{Attributes: []xsd.AttributeUse{{
		Ref: xsd.QName{Namespace: "urn:test", Local: "missing"},
	}}}}
	if err := state.validateAnonymousElementType(element, "urn:test", &particles); err == nil {
		t.Fatal("validateAnonymousElementType(missing nested attribute) succeeded")
	}
}

func TestParticleLimitAcceptsExactCount(t *testing.T) {
	t.Parallel()

	state := emptyValidationState()
	state.compiler.limits.MaxParticles = 1
	group := &xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{{
		Element: &xsd.Element{Name: "value"}, MinOccurs: 1, MaxOccurs: 1,
	}}}
	particles := 0
	if err := state.validateModelGroup(group, "", &particles); err != nil {
		t.Fatalf("validateModelGroup(exact limit) error = %v", err)
	}
	if particles != 1 {
		t.Fatalf("particle count = %d, want 1", particles)
	}
	if err := state.validateModelGroup(group, "", &particles); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateModelGroup(over limit) error = %v", err)
	}
}

func TestElementValueConstraintsAcceptSimpleContentBases(t *testing.T) {
	t.Parallel()

	stringType := xsd.QName{Namespace: xsd.Namespace, Local: "string"}
	state := emptyValidationState()
	inline := xsd.Element{
		Default: "value",
		InlineComplexType: &xsd.ComplexType{
			SimpleContent: true,
			SimpleBase:    stringType,
		},
	}
	if err := state.validateElementValueConstraint(inline); err != nil {
		t.Fatalf("validateElementValueConstraint(inline simple base) error = %v", err)
	}
	namedType := xsd.QName{Namespace: "urn:test", Local: "Text"}
	state.complexTypes[namedType] = xsd.ComplexType{SimpleContent: true, SimpleBase: stringType}
	if err := state.validateElementValueConstraint(xsd.Element{Type: namedType, Default: "value"}); err != nil {
		t.Fatalf("validateElementValueConstraint(named simple base) error = %v", err)
	}
}

func TestCompilableReferenceKinds(t *testing.T) {
	t.Parallel()

	if compilableReference(xsd.SchemaReference{Kind: xsd.ReferenceInclude}) {
		t.Fatal("locationless include is compilable")
	}
	if !compilableReference(xsd.SchemaReference{Kind: xsd.ReferenceImport}) {
		t.Fatal("locationless import is not compilable")
	}
	if !compilableReference(xsd.SchemaReference{Kind: xsd.ReferenceInclude, URI: "child.xsd"}) {
		t.Fatal("located include is not compilable")
	}
}
