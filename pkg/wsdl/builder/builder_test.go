package builder_test

import (
	"errors"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	"github.com/faustbrian/golib/pkg/wsdl/builder"
)

func TestDescription20BuildsValidatedDocument(t *testing.T) {
	t.Parallel()

	value := builder.New20("urn:test")
	if err := value.AddInterface(wsdl.Interface20{
		Name: "API",
		Operations: []wsdl.InterfaceOperation20{{
			Name: "Call", Pattern: wsdl.MEPInOnly,
			Input: &wsdl.InterfaceMessageReference20{},
		}},
	}); err != nil {
		t.Fatalf("AddInterface() error = %v", err)
	}
	document, err := value.Build(wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	description, _ := document.Description20()
	if description.Interfaces[0].Name != "API" {
		t.Fatalf("Description20() = %#v", description)
	}
}

func TestDescription20RejectsDuplicateAndInvalidComponents(t *testing.T) {
	t.Parallel()

	value := builder.New20("urn:test")
	if err := value.AddInterface(wsdl.Interface20{Name: "API"}); err != nil {
		t.Fatalf("AddInterface() error = %v", err)
	}
	if err := value.AddInterface(wsdl.Interface20{Name: "API"}); !errors.Is(err, builder.ErrDuplicateComponent) {
		t.Fatalf("AddInterface() error = %v, want ErrDuplicateComponent", err)
	}
	invalid := builder.New20("urn:test")
	if err := invalid.AddInterface(wsdl.Interface20{Name: "bad name"}); err != nil {
		t.Fatalf("AddInterface() error = %v", err)
	}
	if _, err := invalid.Build(wsdl.ValidationOptions{}); err == nil {
		t.Fatal("Build() error = nil")
	}
}

func TestDefinitions11BuildsValidatedDocument(t *testing.T) {
	t.Parallel()

	value := builder.New11("API", "urn:test")
	if err := value.AddMessage(wsdl.Message11{Name: "Request"}); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}
	document, err := value.Build(wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	definitions, _ := document.Definitions11()
	if definitions.Messages[0].Name != "Request" {
		t.Fatalf("Definitions11() = %#v", definitions)
	}
}

func TestBuildersExerciseCompleteComponentSurface(t *testing.T) {
	t.Parallel()

	description := builder.New20("urn:test")
	if err := description.SetDocumentation(wsdl.Documentation{Content: "API"}); err != nil {
		t.Fatal(err)
	}
	if err := description.SetTypes(wsdl.Types20{}); err != nil {
		t.Fatal(err)
	}
	if err := description.AddImport(wsdl.Import20{Namespace: "urn:other"}); err != nil {
		t.Fatal(err)
	}
	if err := description.AddInclude(wsdl.Include20{}); err != nil {
		t.Fatal(err)
	}
	if err := description.AddInterface(wsdl.Interface20{Name: "API"}); err != nil {
		t.Fatal(err)
	}
	if err := description.AddBinding(wsdl.Binding20{
		Name: "Binding", Interface: wsdl.QName{Namespace: "urn:test", Local: "API"},
		Type: "urn:binding",
	}); err != nil {
		t.Fatal(err)
	}
	if err := description.AddService(wsdl.Service20{
		Name: "Service", Interface: wsdl.QName{Namespace: "urn:test", Local: "API"},
		Endpoints: []wsdl.Endpoint20{{
			Name: "Endpoint", Binding: wsdl.QName{Namespace: "urn:test", Local: "Binding"},
			Address: "https://example.test",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := description.Build(wsdl.ValidationOptions{}); err != nil {
		t.Fatalf("Description20.Build() error = %v", err)
	}

	definitions := builder.New11("API", "urn:test")
	if err := definitions.SetTypes(wsdl.Types11{}); err != nil {
		t.Fatal(err)
	}
	if err := definitions.AddImport(wsdl.Import11{Namespace: "urn:other"}); err != nil {
		t.Fatal(err)
	}
	if err := definitions.AddMessage(wsdl.Message11{Name: "Request"}); err != nil {
		t.Fatal(err)
	}
	if err := definitions.AddPortType(wsdl.PortType11{Name: "API"}); err != nil {
		t.Fatal(err)
	}
	if err := definitions.AddBinding(wsdl.Binding11{
		Name: "Binding", Type: wsdl.QName{Namespace: "urn:test", Local: "API"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := definitions.AddService(wsdl.Service11{
		Name: "Service", Ports: []wsdl.Port11{{
			Name: "Endpoint", Binding: wsdl.QName{Namespace: "urn:test", Local: "Binding"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.Build(wsdl.ValidationOptions{}); err != nil {
		t.Fatalf("Definitions11.Build() error = %v", err)
	}
}

func TestBuildersRejectNilAndUninitializedReceivers(t *testing.T) {
	t.Parallel()

	var description *builder.Description20
	for name, call := range map[string]func() error{
		"documentation": func() error { return description.SetDocumentation(wsdl.Documentation{}) },
		"types":         func() error { return description.SetTypes(wsdl.Types20{}) },
		"import":        func() error { return description.AddImport(wsdl.Import20{}) },
		"include":       func() error { return description.AddInclude(wsdl.Include20{}) },
		"interface":     func() error { return description.AddInterface(wsdl.Interface20{}) },
		"binding":       func() error { return description.AddBinding(wsdl.Binding20{}) },
		"service":       func() error { return description.AddService(wsdl.Service20{}) },
		"build": func() error {
			_, err := description.Build(wsdl.ValidationOptions{})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("Description20.%s error = nil", name)
		}
	}

	var definitions *builder.Definitions11
	for name, call := range map[string]func() error{
		"types":     func() error { return definitions.SetTypes(wsdl.Types11{}) },
		"import":    func() error { return definitions.AddImport(wsdl.Import11{}) },
		"message":   func() error { return definitions.AddMessage(wsdl.Message11{}) },
		"port type": func() error { return definitions.AddPortType(wsdl.PortType11{}) },
		"binding":   func() error { return definitions.AddBinding(wsdl.Binding11{}) },
		"service":   func() error { return definitions.AddService(wsdl.Service11{}) },
		"build": func() error {
			_, err := definitions.Build(wsdl.ValidationOptions{})
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("Definitions11.%s error = nil", name)
		}
	}

	zero20 := &builder.Description20{}
	if err := zero20.AddInterface(wsdl.Interface20{Name: "API"}); err == nil {
		t.Fatal("zero Description20.AddInterface() error = nil")
	}
	zero11 := &builder.Definitions11{}
	if err := zero11.AddMessage(wsdl.Message11{Name: "Request"}); err == nil {
		t.Fatal("zero Definitions11.AddMessage() error = nil")
	}
}

func TestZeroValueBuilderRejectsMutation(t *testing.T) {
	t.Parallel()

	value := &builder.Description20{}
	if err := value.AddInterface(wsdl.Interface20{Name: "API"}); err == nil {
		t.Fatal("AddInterface() error = nil")
	}
}

func TestBuildersRejectEveryDuplicateNamedComponent(t *testing.T) {
	t.Parallel()

	description := builder.New20("urn:test")
	for name, addTwice := range map[string]func() error{
		"binding": func() error {
			value := wsdl.Binding20{Name: "Binding"}
			_ = description.AddBinding(value)
			return description.AddBinding(value)
		},
		"service": func() error {
			value := wsdl.Service20{Name: "Service"}
			_ = description.AddService(value)
			return description.AddService(value)
		},
	} {
		if err := addTwice(); !errors.Is(err, builder.ErrDuplicateComponent) {
			t.Errorf("duplicate %s error = %v", name, err)
		}
	}

	definitions := builder.New11("API", "urn:test")
	for name, addTwice := range map[string]func() error{
		"port type": func() error {
			value := wsdl.PortType11{Name: "Port"}
			_ = definitions.AddPortType(value)
			return definitions.AddPortType(value)
		},
		"binding": func() error {
			value := wsdl.Binding11{Name: "Binding"}
			_ = definitions.AddBinding(value)
			return definitions.AddBinding(value)
		},
		"service": func() error {
			value := wsdl.Service11{Name: "Service"}
			_ = definitions.AddService(value)
			return definitions.AddService(value)
		},
	} {
		if err := addTwice(); !errors.Is(err, builder.ErrDuplicateComponent) {
			t.Errorf("duplicate %s error = %v", name, err)
		}
	}
}
