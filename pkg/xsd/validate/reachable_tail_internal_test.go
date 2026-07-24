package validate

import (
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestValidatorCoversRemainingReachableBranches(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	state := validationState{validator: &Validator{set: set, limits: Limits{
		MaxDiagnostics: 10, MaxXPathSteps: 100, MaxIdentityValues: 100,
	}}}
	listType := xsd.QName{Namespace: "urn:test", Local: "List"}
	if methods, ok := state.typeDerivationMethods(listType, builtIn("anySimpleType")); !ok || len(methods) != 1 || methods[0] != xsd.DerivationList {
		t.Fatalf("typeDerivationMethods(List) = %#v, %t", methods, ok)
	}
	for _, name := range []xsd.QName{
		{Namespace: xsd.Namespace, Local: "unknown"},
		{Namespace: "urn:test", Local: "Missing"},
	} {
		if _, ok := state.typeDerivationMethods(name, builtIn("string")); ok {
			t.Fatalf("typeDerivationMethods(%#v) succeeded", name)
		}
	}
	if state.simpleContextValid(xsd.QName{Namespace: "urn:test", Local: "Missing"}, "value", nil) {
		t.Fatal("simpleContextValid(missing) succeeded")
	}
	if state.simpleLexicalValid(listType, "") {
		t.Fatal("simpleLexicalValid(empty list) succeeded")
	}

	incompleteKeyref := xsd.IdentityConstraint{
		Kind: xsd.IdentityKeyRef, Name: "ref", Selector: ".", Fields: []string{"@missing"},
		Refer: xsd.QName{Local: "key"},
	}
	if err := state.validateIdentityConstraints(identityTestNode("root"), []xsd.IdentityConstraint{incompleteKeyref}, "/root"); err != nil {
		t.Fatalf("validateIdentityConstraints(incomplete keyref) error = %v", err)
	}

	limited := diagnosticLimitState(set)
	node := contentNode("value", map[xsd.QName]string{{Local: "extra"}: "value"})
	if err := limited.validateSimple(node, builtIn("string"), "/root"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateSimple() error = %v", err)
	}

	if err := state.validateComplex(contentNode("value", nil), "urn:test", xsd.ComplexType{
		SimpleContent: true, Base: builtIn("string"),
	}, "/root"); err != nil {
		t.Fatalf("validateComplex(simple base fallback) error = %v", err)
	}

	invalidChild := &instanceNode{
		Name: xsd.QName{Namespace: "urn:test", Local: "child"}, Text: "invalid",
		Attributes: map[xsd.QName]string{}, AttributeTypes: map[xsd.QName]xsd.QName{},
		Namespaces: map[string]string{"": "urn:test"},
	}
	content := &xsd.ModelGroup{Compositor: xsd.Sequence, Particles: []xsd.Particle{{
		MinOccurs: 1, MaxOccurs: 1,
		Element: &xsd.Element{Ref: xsd.QName{Namespace: "urn:test", Local: "child"}},
	}}}
	limited = diagnosticLimitState(set)
	if err := limited.validateComplex(nodeWithChild(invalidChild), "urn:test", xsd.ComplexType{Content: content}, "/root"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateComplex(model error) = %v", err)
	}

	missing := xsd.QName{Namespace: "urn:other", Local: "missing"}
	missingNode := contentNode("", nil)
	if err := state.validateAttributes(missingNode, "urn:test", []xsd.AttributeUse{{Ref: missing}}, nil, "/root"); err != nil || len(state.diagnostics) == 0 {
		t.Fatalf("validateAttributes(missing ref) = %#v, %v", state.diagnostics, err)
	}
	limited = diagnosticLimitState(set)
	missingNode.Attributes[missing] = "value"
	if err := limited.validateAttributes(missingNode, "urn:test", nil, &xsd.Wildcard{
		Namespaces: []string{"##other"}, ProcessContents: xsd.ProcessStrict,
	}, "/root"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateAttributes(strict wildcard) error = %v", err)
	}

	all := &xsd.ModelGroup{Compositor: xsd.All, Particles: []xsd.Particle{{
		MinOccurs: 0, MaxOccurs: 1, Element: &xsd.Element{Name: "other"},
	}}}
	if _, matched, err := state.matchGroupOnce(all, []*instanceNode{invalidChild}, 0, "urn:test", "/root"); err != nil || !matched {
		t.Fatalf("matchGroupOnce(unconsumed all) = %t, %v", matched, err)
	}
}

func TestValidateRootPropagatesIDReferenceLimit(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	validator := &Validator{set: set, limits: Limits{MaxDiagnostics: 0}}
	root := contentNode("", map[xsd.QName]string{{Local: "ref"}: "missing"})
	root.Name = xsd.QName{Namespace: "urn:test", Local: "root"}
	if _, err := validator.validateRoot(root); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateRoot() error = %v", err)
	}
}

func TestNilledNamedComplexTypeValidatesAttributes(t *testing.T) {
	t.Parallel()

	set := contentValidationSet(t)
	state := diagnosticLimitState(set)
	node := contentNode("", map[xsd.QName]string{{Namespace: schemaInstanceNamespace, Local: "nil"}: "true"})
	err := state.validateElementContent(node, xsd.Element{
		Nillable: true,
		Type:     xsd.QName{Namespace: "urn:test", Local: "Named"},
	}, "/root")
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateElementContent() error = %v", err)
	}
}
