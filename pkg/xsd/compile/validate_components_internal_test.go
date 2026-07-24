package compile

import (
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestValidateComponentsPropagatesEveryComponentFailure(t *testing.T) {
	t.Parallel()

	name := xsd.QName{Namespace: "urn:test", Local: "Component"}
	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	booleanType := xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}
	invalidWildcard := &xsd.Wildcard{Namespaces: []string{"##invalid"}, ProcessContents: xsd.ProcessStrict}
	invalidGroup := &xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{{}}}
	invalidSimple := &xsd.SimpleType{}

	for _, test := range []struct {
		name   string
		mutate func(*compileState)
	}{
		{name: "global element reference", mutate: func(s *compileState) {
			s.elements[name] = xsd.Element{Ref: missing}
		}},
		{name: "global element type", mutate: func(s *compileState) {
			s.elements[name] = xsd.Element{Type: missing}
		}},
		{name: "global anonymous type", mutate: func(s *compileState) {
			s.elements[name] = xsd.Element{InlineSimpleType: invalidSimple}
		}},
		{name: "global value constraint", mutate: func(s *compileState) {
			s.elements[name] = xsd.Element{Type: booleanType, Default: "invalid"}
		}},
		{name: "global attribute type", mutate: func(s *compileState) {
			s.attributes[name] = xsd.Attribute{Type: missing}
		}},
		{name: "global attribute duplicate type", mutate: func(s *compileState) {
			s.attributes[name] = xsd.Attribute{Type: booleanType, InlineSimpleType: &xsd.SimpleType{
				Variety: xsd.SimpleRestriction, Base: booleanType,
			}}
		}},
		{name: "global attribute anonymous type", mutate: func(s *compileState) {
			s.attributes[name] = xsd.Attribute{InlineSimpleType: invalidSimple}
		}},
		{name: "cyclic simple type", mutate: func(s *compileState) {
			other := xsd.QName{Namespace: "urn:test", Local: "Other"}
			s.simpleTypes[name] = xsd.SimpleType{Name: "Component", Variety: xsd.SimpleRestriction, Base: other}
			s.simpleTypes[other] = xsd.SimpleType{Name: "Other", Variety: xsd.SimpleRestriction, Base: name}
			s.typeKinds[name] = "simple"
			s.typeKinds[other] = "simple"
		}},
		{name: "anonymous complex model", mutate: func(s *compileState) {
			s.elements[name] = xsd.Element{InlineComplexType: &xsd.ComplexType{Content: invalidGroup}}
		}},
		{name: "model group", mutate: func(s *compileState) {
			s.modelGroups[name] = xsd.ModelGroupDefinition{Content: invalidGroup}
		}},
		{name: "attribute group wildcard", mutate: func(s *compileState) {
			s.attributeGroups[name] = xsd.AttributeGroup{Wildcard: invalidWildcard}
		}},
		{name: "complex type model", mutate: func(s *compileState) {
			s.complexTypes[name] = xsd.ComplexType{Content: invalidGroup}
		}},
		{name: "complex type wildcard", mutate: func(s *compileState) {
			s.complexTypes[name] = xsd.ComplexType{AttributeWildcard: invalidWildcard}
		}},
		{name: "complex type attribute", mutate: func(s *compileState) {
			s.complexTypes[name] = xsd.ComplexType{Attributes: []xsd.AttributeUse{{}}}
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := emptyValidationState()
			test.mutate(&state)
			if err := state.validateComponents(); err == nil {
				t.Fatal("validateComponents() succeeded")
			}
		})
	}
}

func emptyValidationState() compileState {
	return compileState{
		compiler:        &Compiler{limits: Limits{MaxParticles: 100, MaxComponents: 100}},
		elements:        map[xsd.QName]xsd.Element{},
		attributes:      map[xsd.QName]xsd.Attribute{},
		simpleTypes:     map[xsd.QName]xsd.SimpleType{},
		complexTypes:    map[xsd.QName]xsd.ComplexType{},
		modelGroups:     map[xsd.QName]xsd.ModelGroupDefinition{},
		attributeGroups: map[xsd.QName]xsd.AttributeGroup{},
		notations:       map[xsd.QName]xsd.Notation{},
		typeKinds:       map[xsd.QName]string{},
	}
}
