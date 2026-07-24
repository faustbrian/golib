package validate

import (
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestModelMatchingPropagatesNestedValidationFailures(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	child := &instanceNode{
		Name:           xsd.QName{Namespace: "urn:test", Local: "child"},
		Attributes:     map[xsd.QName]string{},
		AttributeTypes: map[xsd.QName]xsd.QName{},
		Namespaces:     map[string]string{"": "urn:test"},
		Text:           "invalid",
	}
	declaration := xsd.Element{Ref: child.Name}
	particle := xsd.Particle{MinOccurs: 1, MaxOccurs: 1, Element: &declaration}

	for _, test := range []struct {
		name string
		run  func(*validationState) error
	}{
		{name: "repeating group", run: func(s *validationState) error {
			_, _, err := s.matchGroup(&xsd.ModelGroup{
				Compositor: xsd.Sequence, OccursSet: true, MinOccurs: 1, MaxOccurs: 1,
				Particles: []xsd.Particle{particle},
			}, []*instanceNode{child}, 0, "urn:test", "/root")
			return err
		}},
		{name: "choice", run: func(s *validationState) error {
			_, _, err := s.matchGroupOnce(&xsd.ModelGroup{
				Compositor: xsd.Choice, Particles: []xsd.Particle{particle},
			}, []*instanceNode{child}, 0, "urn:test", "/root")
			return err
		}},
		{name: "all", run: func(s *validationState) error {
			_, _, err := s.matchGroupOnce(&xsd.ModelGroup{
				Compositor: xsd.All, Particles: []xsd.Particle{particle},
			}, []*instanceNode{child}, 0, "urn:test", "/root")
			return err
		}},
		{name: "repeating particle", run: func(s *validationState) error {
			_, _, err := s.matchParticle(particle, []*instanceNode{child}, 0, "urn:test", "/root")
			return err
		}},
		{name: "declared element", run: func(s *validationState) error {
			_, _, err := s.matchParticleOnce(particle, []*instanceNode{child}, 0, "urn:test", "/root")
			return err
		}},
		{name: "declared wildcard", run: func(s *validationState) error {
			_, _, err := s.matchParticleOnce(xsd.Particle{Wildcard: &xsd.Wildcard{
				Namespaces: []string{"##any"}, ProcessContents: xsd.ProcessStrict,
			}}, []*instanceNode{child}, 0, "urn:test", "/root")
			return err
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			state := diagnosticLimitState(set)
			if err := test.run(&state); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("model matcher error = %v", err)
			}
		})
	}
}

func TestModelMatchingCoversUnmatchedTerms(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	state := validationState{validator: &Validator{set: set, limits: Limits{MaxDiagnostics: 10}}}
	missing := &instanceNode{Name: xsd.QName{Namespace: "urn:other", Local: "missing"}}
	if _, _, err := state.matchParticleOnce(xsd.Particle{
		Element: &xsd.Element{Ref: missing.Name},
	}, []*instanceNode{missing}, 0, "urn:test", "/root"); err != nil {
		t.Fatalf("missing reference diagnostic error = %v", err)
	}
	if _, matched, err := state.matchParticleOnce(xsd.Particle{}, nil, 0, "urn:test", "/root"); err != nil || matched {
		t.Fatalf("empty particle = matched %t, error %v", matched, err)
	}
	if _, matched, err := state.matchGroupOnce(&xsd.ModelGroup{Compositor: "invalid"}, nil, 0, "urn:test", "/root"); err != nil || matched {
		t.Fatalf("invalid compositor = matched %t, error %v", matched, err)
	}
	all := &xsd.ModelGroup{
		Compositor: xsd.All,
		Particles:  []xsd.Particle{{MinOccurs: 0, MaxOccurs: 1, Element: &xsd.Element{Name: "other"}}},
	}
	if _, matched, err := state.matchGroupOnce(all, nil, 0, "urn:test", "/root"); err != nil || !matched {
		t.Fatalf("nullable all group = matched %t, error %v", matched, err)
	}
}
