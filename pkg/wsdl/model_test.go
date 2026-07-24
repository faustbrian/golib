package wsdl_test

import (
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestNewDocument20CanonicalizesAndValidatesModel(t *testing.T) {
	t.Parallel()

	model := wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces: []wsdl.Interface20{{
			Name: "API",
			Operations: []wsdl.InterfaceOperation20{{
				Name: "Call", Pattern: wsdl.MEPInOnly,
				Input: &wsdl.InterfaceMessageReference20{},
			}},
		}},
	}
	document, err := wsdl.NewDocument20(model, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument20() error = %v", err)
	}
	model.Interfaces[0].Name = "Changed"
	description, ok := document.Description20()
	if !ok || description.Interfaces[0].Name != "API" {
		t.Fatalf("Description20() = %#v", description)
	}
}

func TestNewDocument20RejectsInvalidModel(t *testing.T) {
	t.Parallel()

	_, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces: []wsdl.Interface20{{
			Name: "bad name",
		}},
	}, wsdl.ValidationOptions{})
	if err == nil {
		t.Fatal("NewDocument20() error = nil")
	}
}

func TestNewDocument11CanonicalizesModel(t *testing.T) {
	t.Parallel()

	document, err := wsdl.NewDocument11(wsdl.Definitions11{
		Name: "API", TargetNamespace: "urn:test",
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument11() error = %v", err)
	}
	definitions, ok := document.Definitions11()
	if !ok || definitions.Name != "API" {
		t.Fatalf("Definitions11() = %#v", definitions)
	}
}
