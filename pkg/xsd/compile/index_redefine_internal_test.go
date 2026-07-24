package compile

import (
	"context"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestApplyRedefinitionRejectsEveryInvalidTarget(t *testing.T) {
	t.Parallel()

	name := xsd.QName{Namespace: "urn:test", Local: "Component"}
	document := &xsd.Document{}
	for _, test := range []struct {
		name         string
		state        compileState
		redefinition xsd.Redefinition
	}{
		{name: "missing simple type", state: emptyValidationState(), redefinition: xsd.Redefinition{SimpleTypes: []xsd.SimpleType{{Name: "Component"}}}},
		{name: "invalid simple type", state: stateWithSimple(name), redefinition: xsd.Redefinition{SimpleTypes: []xsd.SimpleType{{Name: "Component", Variety: xsd.SimpleList}}}},
		{name: "missing model group", state: emptyValidationState(), redefinition: xsd.Redefinition{ModelGroups: []xsd.ModelGroupDefinition{{Name: "Component"}}}},
		{name: "missing attribute group", state: emptyValidationState(), redefinition: xsd.Redefinition{AttributeGroups: []xsd.AttributeGroup{{Name: "Component"}}}},
		{name: "attribute group without self", state: stateWithAttributeGroup(name), redefinition: xsd.Redefinition{AttributeGroups: []xsd.AttributeGroup{{Name: "Component"}}}},
		{name: "missing complex type", state: emptyValidationState(), redefinition: xsd.Redefinition{ComplexTypes: []xsd.ComplexType{{Name: "Component"}}}},
		{name: "invalid complex type", state: stateWithComplex(name), redefinition: xsd.Redefinition{ComplexTypes: []xsd.ComplexType{{Name: "Component", Base: name, Derivation: "invalid"}}}},
		{name: "invalid complex restriction", state: stateWithComplexContent(name), redefinition: xsd.Redefinition{ComplexTypes: []xsd.ComplexType{{
			Name: "Component", Base: name, Derivation: xsd.DerivationRestriction,
			Content: &xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{{Element: &xsd.Element{Name: "different"}}}},
		}}}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.state.applyRedefinition(test.redefinition, document, "urn:test", false); err == nil {
				t.Fatal("applyRedefinition() succeeded")
			}
		})
	}
}

func TestApplyRedefinitionPreservesNonSelfAttributeReferences(t *testing.T) {
	t.Parallel()

	name := xsd.QName{Namespace: "urn:test", Local: "Component"}
	other := xsd.QName{Namespace: "urn:test", Local: "Other"}
	state := stateWithAttributeGroup(name)
	err := state.applyRedefinition(xsd.Redefinition{AttributeGroups: []xsd.AttributeGroup{{
		Name: "Component", References: []xsd.QName{other, name},
	}}}, &xsd.Document{}, "urn:test", false)
	if err != nil {
		t.Fatalf("applyRedefinition() error = %v", err)
	}
	if got := state.attributeGroups[name].References; len(got) != 1 || got[0] != other {
		t.Fatalf("redefined references = %#v", got)
	}
}

func stateWithSimple(name xsd.QName) compileState {
	state := emptyValidationState()
	state.simpleTypes[name] = xsd.SimpleType{Name: name.Local, Variety: xsd.SimpleRestriction, Base: xsd.QName{Namespace: xsd.Namespace, Local: "string"}}
	return state
}

func stateWithAttributeGroup(name xsd.QName) compileState {
	state := emptyValidationState()
	state.attributeGroups[name] = xsd.AttributeGroup{Name: name.Local, Attributes: []xsd.AttributeUse{{Name: "base"}}}
	return state
}

func stateWithComplex(name xsd.QName) compileState {
	state := emptyValidationState()
	state.complexTypes[name] = xsd.ComplexType{Name: name.Local}
	return state
}

func stateWithComplexContent(name xsd.QName) compileState {
	state := stateWithComplex(name)
	state.complexTypes[name] = xsd.ComplexType{Name: name.Local, Content: &xsd.ModelGroup{
		Compositor: xsd.Sequence, Particles: []xsd.Particle{{Element: &xsd.Element{Name: "required"}}},
	}}
	return state
}

func TestCompileDocumentBoundaries(t *testing.T) {
	t.Parallel()

	identity := "https://example.test/schema.xsd"
	baseDocument := func(references ...xsd.SchemaReference) compileState {
		state := emptyValidationState()
		state.compiler.limits.MaxDepth = 1
		state.compiler.limits.MaxSchemas = 10
		state.compiler.limits.MaxReferences = 10
		state.compiler.resolver = resolve.Deny()
		state.resources = map[string]resourceDocument{identity: {
			document: &xsd.Document{References: references},
		}}
		state.instances = map[instanceKey]*Document{}
		return state
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	state := baseDocument()
	if err := state.compileDocument(canceled, identity, "", 0); err == nil {
		t.Fatal("compileDocument(canceled) succeeded")
	}
	state = baseDocument()
	if err := state.compileDocument(context.Background(), identity, "", 2); err == nil {
		t.Fatal("compileDocument(depth) succeeded")
	}
	state = baseDocument(xsd.SchemaReference{Kind: xsd.ReferenceInclude})
	if err := state.compileDocument(context.Background(), identity, "", 0); err != nil {
		t.Fatalf("compileDocument(empty include) error = %v", err)
	}
	state = baseDocument(xsd.SchemaReference{Kind: xsd.ReferenceImport})
	if err := state.compileDocument(context.Background(), identity, "", 0); err != nil {
		t.Fatalf("compileDocument(locationless import) error = %v", err)
	}
	state = baseDocument(xsd.SchemaReference{Kind: xsd.ReferenceInclude, URI: "child.xsd"})
	state.compiler.limits.MaxReferences = 0
	if err := state.compileDocument(context.Background(), identity, "", 0); err == nil {
		t.Fatal("compileDocument(reference limit) succeeded")
	}
	state = baseDocument(xsd.SchemaReference{Kind: xsd.ReferenceInclude, URI: "child.xsd"})
	state.resources["child.xsd"] = resourceDocument{document: &xsd.Document{TargetNamespace: "urn:other"}}
	if err := state.compileDocument(context.Background(), identity, "urn:test", 0); err == nil {
		t.Fatal("compileDocument(namespace mismatch) succeeded")
	}
	state = baseDocument(xsd.SchemaReference{Kind: xsd.ReferenceRedefine, URI: "child.xsd"})
	state.resources[identity].document.Redefinitions = []xsd.Redefinition{{
		Reference:   xsd.SchemaReference{Kind: xsd.ReferenceRedefine, URI: "child.xsd"},
		SimpleTypes: []xsd.SimpleType{{Name: "Component", Variety: xsd.SimpleList}},
	}}
	state.resources["child.xsd"] = resourceDocument{document: &xsd.Document{
		SimpleTypes: []xsd.SimpleType{{Name: "Component"}},
	}}
	if err := state.compileDocument(context.Background(), identity, "", 0); err == nil {
		t.Fatal("compileDocument(invalid redefine) succeeded")
	}
}

func TestIndexComponentsRejectsMissingNamesAndDuplicates(t *testing.T) {
	t.Parallel()

	for _, document := range []*xsd.Document{
		{Notations: []xsd.Notation{{}}},
		{Attributes: []xsd.Attribute{{}}},
		{SimpleTypes: []xsd.SimpleType{{}}},
		{ComplexTypes: []xsd.ComplexType{{}}},
		{ModelGroups: []xsd.ModelGroupDefinition{{}}},
		{AttributeGroups: []xsd.AttributeGroup{{}}},
	} {
		state := emptyValidationState()
		if err := state.indexComponents(document, "urn:test", false); err == nil {
			t.Fatalf("indexComponents(%#v) succeeded", document)
		}
	}
	state := emptyValidationState()
	name := xsd.QName{Namespace: "urn:test", Local: "duplicate"}
	state.notations[name] = xsd.Notation{Name: "duplicate"}
	if err := state.indexComponents(&xsd.Document{Notations: []xsd.Notation{{Name: "duplicate", Public: "id"}}}, "urn:test", false); err == nil {
		t.Fatal("indexComponents(duplicate notation) succeeded")
	}
	state = emptyValidationState()
	state.attributes[name] = xsd.Attribute{Name: "duplicate"}
	if err := state.indexComponents(&xsd.Document{Attributes: []xsd.Attribute{{Name: "duplicate"}}}, "urn:test", false); err == nil {
		t.Fatal("indexComponents(duplicate attribute) succeeded")
	}
	state = emptyValidationState()
	state.compiler.limits.MaxComponents = 0
	if err := state.indexComponents(&xsd.Document{Elements: []xsd.Element{{Name: "element"}}}, "urn:test", false); err == nil {
		t.Fatal("indexComponents(component limit) succeeded")
	}
	state = emptyValidationState()
	state.attributeGroups[name] = xsd.AttributeGroup{Name: "duplicate"}
	if err := state.indexComponents(&xsd.Document{AttributeGroups: []xsd.AttributeGroup{{Name: "duplicate"}}}, "urn:test", false); err == nil {
		t.Fatal("indexComponents(duplicate attribute group) succeeded")
	}
	state = emptyValidationState()
	if err := state.indexComponents(&xsd.Document{Notations: []xsd.Notation{{Name: "image", Public: "id"}}}, "urn:chameleon", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := state.notations[xsd.QName{Namespace: "urn:chameleon", Local: "image"}]; !ok {
		t.Fatal("chameleon notation was not indexed in the effective namespace")
	}
}

func TestChameleonNormalizationAdoptsNestedReferences(t *testing.T) {
	t.Parallel()

	namespace := "urn:test"
	local := xsd.QName{Local: "Local"}
	typeDefinition := normalizeInlineSimpleType(xsd.SimpleType{
		Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{local},
	}, namespace, true)
	if typeDefinition.MemberTypes[0].Namespace != namespace {
		t.Fatalf("normalized member = %#v", typeDefinition.MemberTypes[0])
	}
	group := normalizeAttributeGroup(xsd.AttributeGroup{References: []xsd.QName{local}}, "", namespace, true)
	if group.References[0].Namespace != namespace {
		t.Fatalf("normalized attribute group = %#v", group)
	}
	model := &xsd.ModelGroup{Particles: []xsd.Particle{{Element: &xsd.Element{
		Name: "local", IdentityConstraints: []xsd.IdentityConstraint{{Refer: local}},
	}}}}
	normalizeModelGroup(model, xsd.FormQualified, "", namespace, true)
	if model.Particles[0].Element.IdentityConstraints[0].Refer.Namespace != namespace {
		t.Fatalf("normalized model = %#v", model)
	}
}

func TestChameleonIndexingAdoptsGlobalNestedReferences(t *testing.T) {
	t.Parallel()

	namespace := "urn:test"
	local := xsd.QName{Local: "Local"}
	state := emptyValidationState()
	document := &xsd.Document{
		Elements:    []xsd.Element{{Name: "element", IdentityConstraints: []xsd.IdentityConstraint{{Refer: local}}}},
		SimpleTypes: []xsd.SimpleType{{Name: "Type", Variety: xsd.SimpleUnion, MemberTypes: []xsd.QName{local}}},
	}
	if err := state.indexComponents(document, namespace, true); err != nil {
		t.Fatalf("indexComponents() error = %v", err)
	}
	if got := state.elements[xsd.QName{Namespace: namespace, Local: "element"}].IdentityConstraints[0].Refer.Namespace; got != namespace {
		t.Fatalf("identity namespace = %q", got)
	}
	if got := state.simpleTypes[xsd.QName{Namespace: namespace, Local: "Type"}].MemberTypes[0].Namespace; got != namespace {
		t.Fatalf("member namespace = %q", got)
	}
}
