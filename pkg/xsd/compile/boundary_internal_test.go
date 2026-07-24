package compile

import (
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestRequiredCompositionNamespaces(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		reference xsd.SchemaReference
		parent    string
		actual    string
		want      string
		wantErr   bool
	}{
		{name: "include same namespace", reference: xsd.SchemaReference{Kind: xsd.ReferenceInclude}, parent: "urn:test", actual: "urn:test", want: "urn:test"},
		{name: "chameleon include", reference: xsd.SchemaReference{Kind: xsd.ReferenceInclude}, parent: "urn:test", want: "urn:test"},
		{name: "include mismatch", reference: xsd.SchemaReference{Kind: xsd.ReferenceInclude}, parent: "urn:test", actual: "urn:other", wantErr: true},
		{name: "redefine same namespace", reference: xsd.SchemaReference{Kind: xsd.ReferenceRedefine}, parent: "urn:test", actual: "urn:test", want: "urn:test"},
		{name: "import requested namespace", reference: xsd.SchemaReference{Kind: xsd.ReferenceImport, Namespace: "urn:other"}, parent: "urn:test", actual: "urn:other", want: "urn:other"},
		{name: "import mismatch", reference: xsd.SchemaReference{Kind: xsd.ReferenceImport, Namespace: "urn:other"}, parent: "urn:test", actual: "urn:wrong", wantErr: true},
		{name: "unknown reference", reference: xsd.SchemaReference{Kind: "unknown"}, wantErr: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := requiredNamespace(test.reference, test.parent, test.actual)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("requiredNamespace() = %q, %v; want %q, error=%t", got, err, test.want, test.wantErr)
			}
			if test.wantErr && test.reference.Kind != "unknown" && !errors.Is(err, ErrNamespace) {
				t.Fatalf("requiredNamespace() error = %v, want ErrNamespace", err)
			}
		})
	}
}

func TestWildcardDefinitionValidation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		wildcard *xsd.Wildcard
		wantErr  bool
	}{
		{name: "nil"},
		{name: "explicit namespace", wildcard: &xsd.Wildcard{Namespaces: []string{"urn:test"}, ProcessContents: xsd.ProcessStrict}},
		{name: "standard namespace tokens", wildcard: &xsd.Wildcard{Namespaces: []string{"##local", "##targetNamespace"}, ProcessContents: xsd.ProcessLax}},
		{name: "skip", wildcard: &xsd.Wildcard{Namespaces: []string{"##any"}, ProcessContents: xsd.ProcessSkip}},
		{name: "invalid process contents", wildcard: &xsd.Wildcard{Namespaces: []string{"##any"}, ProcessContents: "invalid"}, wantErr: true},
		{name: "empty namespace set", wildcard: &xsd.Wildcard{ProcessContents: xsd.ProcessStrict}, wantErr: true},
		{name: "invalid namespace token", wildcard: &xsd.Wildcard{Namespaces: []string{"##invalid"}, ProcessContents: xsd.ProcessStrict}, wantErr: true},
		{name: "duplicate namespace", wildcard: &xsd.Wildcard{Namespaces: []string{"urn:test", "urn:test"}, ProcessContents: xsd.ProcessStrict}, wantErr: true},
		{name: "any must stand alone", wildcard: &xsd.Wildcard{Namespaces: []string{"##any", "urn:test"}, ProcessContents: xsd.ProcessStrict}, wantErr: true},
		{name: "other must stand alone", wildcard: &xsd.Wildcard{Namespaces: []string{"##other", "urn:test"}, ProcessContents: xsd.ProcessStrict}, wantErr: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateWildcard(test.wildcard)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateWildcard() error = %v, want error=%t", err, test.wantErr)
			}
			if test.wantErr && !errors.Is(err, ErrInvalidComponent) {
				t.Fatalf("validateWildcard() error = %v, want ErrInvalidComponent", err)
			}
		})
	}
}

func TestAttributeUseExpandedName(t *testing.T) {
	t.Parallel()

	reference := xsd.QName{Namespace: "urn:test", Local: "global"}
	if got := attributeUseName(xsd.AttributeUse{Ref: reference}); got != reference {
		t.Fatalf("attributeUseName(ref) = %#v", got)
	}
	want := xsd.QName{Namespace: "urn:test", Local: "local"}
	if got := attributeUseName(xsd.AttributeUse{Name: "local", Namespace: "urn:test"}); got != want {
		t.Fatalf("attributeUseName(local) = %#v", got)
	}
}

func TestAttributeUseRejectsDirectNotation(t *testing.T) {
	t.Parallel()

	state := emptyValidationState()
	if err := state.validateAttributeUse(xsd.AttributeUse{
		Name: "notation", Type: xsd.QName{Namespace: xsd.Namespace, Local: "NOTATION"},
	}, ""); !errors.Is(err, ErrInvalidComponent) {
		t.Fatalf("validateAttributeUse() error = %v", err)
	}
}
