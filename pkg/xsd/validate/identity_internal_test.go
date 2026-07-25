package validate

import (
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestValidateIdentityConstraintsStopsAtEveryLimitBoundary(t *testing.T) {
	t.Parallel()

	set := attributeValidationSet(t)
	root := identityTestNode("root")
	root.Children = []*instanceNode{identityTestNode("same"), identityTestNode("same")}
	key := xsd.IdentityConstraint{
		Kind: xsd.IdentityKey, Name: "key", Selector: ".", Fields: []string{"@id"},
	}
	keyref := xsd.IdentityConstraint{
		Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"."},
		Refer: xsd.QName{Local: "key"},
	}
	for _, test := range []struct {
		name        string
		constraints []xsd.IdentityConstraint
		limits      Limits
	}{
		{name: "key XPath work", constraints: []xsd.IdentityConstraint{key}, limits: Limits{MaxXPathSteps: 0, MaxIdentityValues: 10}},
		{name: "key values", constraints: []xsd.IdentityConstraint{key}, limits: Limits{MaxXPathSteps: 10, MaxIdentityValues: 0}},
		{name: "keyref XPath work", constraints: []xsd.IdentityConstraint{keyref}, limits: Limits{MaxXPathSteps: 0, MaxIdentityValues: 10}},
		{name: "keyref values", constraints: []xsd.IdentityConstraint{keyref}, limits: Limits{MaxXPathSteps: 10, MaxIdentityValues: 0}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := validationState{validator: &Validator{set: set, limits: test.limits}}
			if err := state.validateIdentityConstraints(root, test.constraints, "/root"); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("validateIdentityConstraints() error = %v", err)
			}
		})
	}
}

func TestValidateIdentityConstraintsStopsAtEveryDiagnosticBoundary(t *testing.T) {
	t.Parallel()

	set := attributeValidationSet(t)
	children := identityTestNode("root")
	children.Children = []*instanceNode{identityTestNode("same"), identityTestNode("same")}
	nillable := identityTestNode("value")
	nillable.Nillable = true
	duplicate := xsd.IdentityConstraint{
		Kind: xsd.IdentityUnique, Name: "unique", Selector: "child", Fields: []string{"."},
	}
	for _, test := range []struct {
		name        string
		node        *instanceNode
		constraints []xsd.IdentityConstraint
	}{
		{name: "multiple key field values", node: children, constraints: []xsd.IdentityConstraint{{
			Kind: xsd.IdentityUnique, Name: "unique", Selector: ".", Fields: []string{"child"},
		}}},
		{name: "missing key field", node: identityTestNode("root"), constraints: []xsd.IdentityConstraint{{
			Kind: xsd.IdentityKey, Name: "key", Selector: ".", Fields: []string{"@missing"},
		}}},
		{name: "duplicate identity", node: children, constraints: []xsd.IdentityConstraint{duplicate}},
		{name: "nillable key field", node: nillable, constraints: []xsd.IdentityConstraint{{
			Kind: xsd.IdentityKey, Name: "key", Selector: ".", Fields: []string{"."},
		}}},
		{name: "multiple keyref field values", node: children, constraints: []xsd.IdentityConstraint{{
			Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"child"}, Refer: xsd.QName{Local: "key"},
		}}},
		{name: "unmatched keyref", node: identityTestNode("missing"), constraints: []xsd.IdentityConstraint{{
			Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"."}, Refer: xsd.QName{Local: "key"},
		}}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := validationState{validator: &Validator{set: set, limits: Limits{
				MaxDiagnostics:    0,
				MaxXPathSteps:     100,
				MaxIdentityValues: 100,
			}}}
			if err := state.validateIdentityConstraints(test.node, test.constraints, "/root"); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("validateIdentityConstraints() error = %v", err)
			}
		})
	}
}

func TestValidateIdentityConstraintsContinuesAfterMultipleKeyrefField(t *testing.T) {
	t.Parallel()

	root := identityTestNode("root")
	root.Children = []*instanceNode{identityTestNode("first"), identityTestNode("second")}
	state := validationState{validator: &Validator{
		set:    attributeValidationSet(t),
		limits: Limits{MaxDiagnostics: 10, MaxXPathSteps: 100, MaxIdentityValues: 100},
	}}
	err := state.validateIdentityConstraints(root, []xsd.IdentityConstraint{{
		Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"child"},
		Refer: xsd.QName{Local: "key"},
	}}, "/root")
	if err != nil || len(state.diagnostics) != 1 ||
		state.diagnostics[0].Code != "cvc-identity-constraint" {
		t.Fatalf("validateIdentityConstraints() = %#v, %v", state.diagnostics, err)
	}
}

func identityTestNode(text string) *instanceNode {
	return &instanceNode{
		Name:           xsd.QName{Local: "child"},
		Attributes:     map[xsd.QName]string{},
		AttributeTypes: map[xsd.QName]xsd.QName{},
		Namespaces:     map[string]string{},
		Text:           text,
	}
}
