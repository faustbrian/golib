package compile

import (
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestExpandGroupsPropagatesReferenceFailures(t *testing.T) {
	t.Parallel()

	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	model := xsd.QName{Namespace: "urn:test", Local: "Model"}
	attributes := xsd.QName{Namespace: "urn:test", Local: "Attributes"}
	complexType := xsd.QName{Namespace: "urn:test", Local: "Complex"}

	for _, test := range []struct {
		name  string
		state compileState
	}{
		{
			name: "recursive model group",
			state: compileState{modelGroups: map[xsd.QName]xsd.ModelGroupDefinition{
				model: {Content: &xsd.ModelGroup{Particles: []xsd.Particle{{GroupRef: model}}}},
			}},
		},
		{
			name: "recursive attribute group",
			state: compileState{
				modelGroups: map[xsd.QName]xsd.ModelGroupDefinition{},
				attributeGroups: map[xsd.QName]xsd.AttributeGroup{
					attributes: {References: []xsd.QName{attributes}},
				},
			},
		},
		{
			name: "complex type model reference",
			state: compileState{
				modelGroups:     map[xsd.QName]xsd.ModelGroupDefinition{},
				attributeGroups: map[xsd.QName]xsd.AttributeGroup{},
				complexTypes: map[xsd.QName]xsd.ComplexType{
					complexType: {Content: &xsd.ModelGroup{
						Particles: []xsd.Particle{{GroupRef: missing}},
					}},
				},
			},
		},
		{
			name: "complex type attribute reference",
			state: compileState{
				modelGroups:     map[xsd.QName]xsd.ModelGroupDefinition{},
				attributeGroups: map[xsd.QName]xsd.AttributeGroup{},
				complexTypes: map[xsd.QName]xsd.ComplexType{
					complexType: {AttributeGroupRefs: []xsd.QName{missing}},
				},
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.state.expandGroups(); err == nil {
				t.Fatal("expandGroups() succeeded")
			}
		})
	}
}

func TestExpandGroupsCombinesAttributeWildcards(t *testing.T) {
	t.Parallel()

	first := xsd.QName{Namespace: "urn:test", Local: "First"}
	second := xsd.QName{Namespace: "urn:test", Local: "Second"}
	typeName := xsd.QName{Namespace: "urn:test", Local: "Complex"}
	state := compileState{
		modelGroups: map[xsd.QName]xsd.ModelGroupDefinition{},
		attributeGroups: map[xsd.QName]xsd.AttributeGroup{
			first: {
				Wildcard: &xsd.Wildcard{
					Namespaces:      []string{"urn:first", "urn:shared"},
					ProcessContents: xsd.ProcessLax,
				},
			},
			second: {
				Wildcard: &xsd.Wildcard{
					Namespaces:      []string{"urn:second", "urn:shared"},
					ProcessContents: xsd.ProcessStrict,
				},
			},
		},
		complexTypes: map[xsd.QName]xsd.ComplexType{
			typeName: {AttributeGroupRefs: []xsd.QName{first, second}},
		},
	}
	if err := state.expandGroups(); err != nil {
		t.Fatalf("expandGroups() error = %v", err)
	}
	got := state.complexTypes[typeName]
	if len(got.AttributeGroupRefs) != 0 || got.AttributeWildcard == nil ||
		len(got.AttributeWildcard.Namespaces) != 1 ||
		got.AttributeWildcard.Namespaces[0] != "urn:shared" ||
		got.AttributeWildcard.ProcessContents != xsd.ProcessStrict {
		t.Fatalf("expanded complex type = %#v", got)
	}
}
