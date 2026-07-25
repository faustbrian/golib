package compile

import (
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestValidateModelGroupRejectsInvalidParticles(t *testing.T) {
	t.Parallel()

	booleanType := xsd.QName{Namespace: xsd.Namespace, Local: "boolean"}
	missing := xsd.QName{Namespace: "urn:test", Local: "Missing"}
	inlineBoolean := &xsd.SimpleType{Variety: xsd.SimpleRestriction, Base: booleanType}
	state := &compileState{
		compiler:     &Compiler{limits: Limits{MaxParticles: 100}},
		elements:     map[xsd.QName]xsd.Element{},
		simpleTypes:  map[xsd.QName]xsd.SimpleType{},
		complexTypes: map[xsd.QName]xsd.ComplexType{},
		typeKinds:    map[xsd.QName]string{},
	}

	for _, test := range []struct {
		name     string
		particle xsd.Particle
	}{
		{
			name: "local substitution group",
			particle: xsd.Particle{Element: &xsd.Element{
				Name: "local", SubstitutionGroup: missing,
			}},
		},
		{
			name: "default and fixed",
			particle: xsd.Particle{Element: &xsd.Element{
				Name: "local", Default: "a", Fixed: "b",
			}},
		},
		{
			name: "missing type",
			particle: xsd.Particle{Element: &xsd.Element{
				Name: "local", Type: missing,
			}},
		},
		{
			name: "multiple type definitions",
			particle: xsd.Particle{Element: &xsd.Element{
				Name: "local", Type: booleanType, InlineSimpleType: inlineBoolean,
			}},
		},
		{
			name: "invalid value constraint",
			particle: xsd.Particle{Element: &xsd.Element{
				Name: "local", Type: booleanType, Default: "invalid",
			}},
		},
		{
			name: "invalid wildcard",
			particle: xsd.Particle{Wildcard: &xsd.Wildcard{
				Namespaces: []string{"##invalid"}, ProcessContents: xsd.ProcessStrict,
			}},
		},
		{name: "missing term", particle: xsd.Particle{}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			particles := 0
			err := state.validateModelGroup(&xsd.ModelGroup{
				Compositor: xsd.Sequence,
				Particles:  []xsd.Particle{test.particle},
			}, "urn:test", &particles)
			if !errors.Is(err, ErrInvalidComponent) &&
				!errors.Is(err, ErrUnresolvedComponent) {
				t.Fatalf("validateModelGroup() error = %v", err)
			}
		})
	}
}

func TestValidateModelGroupEnforcesParticleLimitRecursively(t *testing.T) {
	t.Parallel()

	state := &compileState{compiler: &Compiler{limits: Limits{MaxParticles: 1}}}
	group := &xsd.ModelGroup{
		Compositor: xsd.Sequence,
		Particles: []xsd.Particle{{Group: &xsd.ModelGroup{
			Compositor: xsd.Sequence,
			Particles: []xsd.Particle{{Element: &xsd.Element{
				Name: "nested",
			}}},
		}}},
	}
	particles := 0
	if err := state.validateModelGroup(group, "urn:test", &particles); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateModelGroup() error = %v", err)
	}
}
