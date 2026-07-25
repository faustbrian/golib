package compose

import (
	"errors"
	"reflect"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestMergeRejectsUnsupportedInternalVersion(t *testing.T) {
	t.Parallel()

	document := &wsdl.Document{}
	if _, err := Merge(document); !errors.Is(err, ErrVersion) {
		t.Fatalf("Merge(unsupported) error = %v", err)
	}
}

func TestDeterministicSortHelpersUseEveryKey(t *testing.T) {
	t.Parallel()

	includes := uniqueIncludes20([]wsdl.Include20{
		{Location: "z.wsdl"}, {Location: "a.wsdl"}, {Location: "m.wsdl"},
	})
	if got := []string{includes[0].Location, includes[1].Location, includes[2].Location}; !reflect.DeepEqual(got, []string{"a.wsdl", "m.wsdl", "z.wsdl"}) {
		t.Fatalf("uniqueIncludes20() locations = %v", got)
	}

	extensibility := wsdl.Extensibility{
		ExtensionAttributes: []wsdl.ExtensionAttribute{
			{Name: wsdl.QName{Namespace: "urn:z", Local: "a"}},
			{Name: wsdl.QName{Namespace: "urn:a", Local: "z"}},
			{Name: wsdl.QName{Namespace: "urn:a", Local: "a"}},
		},
		Extensions: []wsdl.Extension{
			{Name: wsdl.QName{Namespace: "urn:z", Local: "a"}, XML: []byte("a")},
			{Name: wsdl.QName{Namespace: "urn:a", Local: "z"}, XML: []byte("a")},
			{Name: wsdl.QName{Namespace: "urn:a", Local: "a"}, XML: []byte("z")},
			{Name: wsdl.QName{Namespace: "urn:a", Local: "a"}, XML: []byte("a")},
		},
	}
	sortRootExtensibility(&extensibility)
	attributeKeys := make([]string, len(extensibility.ExtensionAttributes))
	for i, value := range extensibility.ExtensionAttributes {
		attributeKeys[i] = value.Name.Namespace + ":" + value.Name.Local
	}
	if !reflect.DeepEqual(attributeKeys, []string{"urn:a:a", "urn:a:z", "urn:z:a"}) {
		t.Fatalf("extension attribute keys = %v", attributeKeys)
	}
	extensionKeys := make([]string, len(extensibility.Extensions))
	for i, value := range extensibility.Extensions {
		extensionKeys[i] = value.Name.Namespace + ":" + value.Name.Local + ":" + string(value.XML)
	}
	if !reflect.DeepEqual(extensionKeys, []string{
		"urn:a:a:a", "urn:a:a:z", "urn:a:z:a", "urn:z:a:a",
	}) {
		t.Fatalf("extension keys = %v", extensionKeys)
	}

	err := reportConflicts([]Conflict{
		{Kind: "service", Name: "Alpha"},
		{Kind: "binding", Name: "Zulu"},
		{Kind: "binding", Name: "Alpha"},
	})
	var conflicts *ConflictError
	if !errors.As(err, &conflicts) || !reflect.DeepEqual(conflicts.Conflicts, []Conflict{
		{Kind: "binding", Name: "Alpha"},
		{Kind: "binding", Name: "Zulu"},
		{Kind: "service", Name: "Alpha"},
	}) {
		t.Fatalf("reportConflicts() = %#v", err)
	}
}

func TestCompositionHelpersUseCompleteDeterministicKeys(t *testing.T) {
	if got := (&ConflictError{Conflicts: []Conflict{{Kind: "service", Name: "API"}}}).Error(); got != `wsdl compose: component conflict: service "API"` {
		t.Fatalf("ConflictError.Error() = %q", got)
	}
	if got := componentName11(struct{}{}); got != "" {
		t.Fatalf("componentName11(unknown) = %q", got)
	}
	if got := schemaKey(nil); got != "" {
		t.Fatalf("schemaKey(nil) = %q", got)
	}
	originalMarshalSchema := marshalSchema
	marshalSchema = func(*xsd.Document) ([]byte, error) { return nil, errors.New("marshal") }
	t.Cleanup(func() { marshalSchema = originalMarshalSchema })
	invalidSchema := &xsd.Document{TargetNamespace: "urn:invalid", SystemID: "schema.xsd"}
	if got := schemaKey(invalidSchema); got != "urn:invalid\x00schema.xsd" {
		t.Fatalf("schemaKey(invalid) = %q", got)
	}
	marshalSchema = originalMarshalSchema
	schemas := []*xsd.Document{{TargetNamespace: "urn:z"}, nil, {TargetNamespace: "urn:a"}}
	sortSchemas(schemas)
	if schemas[0] != nil || schemas[1].TargetNamespace != "urn:a" {
		t.Fatalf("sortSchemas() = %#v", schemas)
	}

	imports20 := uniqueImports20([]wsdl.Import20{
		{Namespace: "urn:z", Location: "a.wsdl"},
		{Namespace: "urn:a", Location: "z.wsdl"},
		{Namespace: "urn:a", Location: "a.wsdl"},
	})
	if imports20[0].Namespace != "urn:a" || imports20[0].Location != "a.wsdl" ||
		imports20[2].Namespace != "urn:z" {
		t.Fatalf("uniqueImports20() = %#v", imports20)
	}
	imports11 := uniqueImports11([]wsdl.Import11{
		{Namespace: "urn:z", Location: "a.wsdl"},
		{Namespace: "urn:a", Location: "z.wsdl"},
		{Namespace: "urn:a", Location: "a.wsdl"},
	})
	if imports11[0].Namespace != "urn:a" || imports11[0].Location != "a.wsdl" ||
		imports11[2].Namespace != "urn:z" {
		t.Fatalf("uniqueImports11() = %#v", imports11)
	}

	left := &wsdl.Documentation{Language: "a", Content: "Alpha"}
	right := &wsdl.Documentation{Language: "z", Content: "Zulu"}
	if got := selectDocumentation(left, right); got != left {
		t.Fatalf("selectDocumentation() = %#v", got)
	}
	conflictErr := reportConflicts([]Conflict{
		{Kind: "service", Name: "Zulu"}, {Kind: "service", Name: "Alpha"},
	})
	var conflicts *ConflictError
	if !errors.As(conflictErr, &conflicts) || conflicts.Conflicts[0].Name != "Alpha" {
		t.Fatalf("reportConflicts() = %#v", conflictErr)
	}
	names := make(map[string]struct{})
	if duplicate(names, "name") || !duplicate(names, "name") {
		t.Fatal("duplicate() did not distinguish first and repeated names")
	}
}

func TestMergeSortsEveryWSDLComponentCollection(t *testing.T) {
	t.Parallel()

	document20, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces:      []wsdl.Interface20{{Name: "Zulu"}, {Name: "Alpha"}},
		Bindings: []wsdl.Binding20{
			{Name: "Zulu", Interface: wsdl.QName{Namespace: "urn:test", Local: "Zulu"}, Type: "urn:binding"},
			{Name: "Alpha", Interface: wsdl.QName{Namespace: "urn:test", Local: "Alpha"}, Type: "urn:binding"},
		},
		Services: []wsdl.Service20{
			{Name: "Zulu", Interface: wsdl.QName{Namespace: "urn:test", Local: "Zulu"}},
			{Name: "Alpha", Interface: wsdl.QName{Namespace: "urn:test", Local: "Alpha"}},
		},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument20() error = %v", err)
	}
	merged20, err := Merge(document20)
	if err != nil {
		t.Fatalf("Merge(WSDL 2.0) error = %v", err)
	}
	description, _ := merged20.Description20()
	if description.Interfaces[0].Name != "Alpha" || description.Bindings[0].Name != "Alpha" ||
		description.Services[0].Name != "Alpha" {
		t.Fatalf("Description20() = %#v", description)
	}

	document11, err := wsdl.NewDocument11(wsdl.Definitions11{
		TargetNamespace: "urn:test",
		Messages:        []wsdl.Message11{{Name: "Zulu"}, {Name: "Alpha"}},
		PortTypes:       []wsdl.PortType11{{Name: "Zulu"}, {Name: "Alpha"}},
		Bindings: []wsdl.Binding11{
			{Name: "Zulu", Type: wsdl.QName{Namespace: "urn:other", Local: "Port"}},
			{Name: "Alpha", Type: wsdl.QName{Namespace: "urn:other", Local: "Port"}},
		},
		Services: []wsdl.Service11{{Name: "Zulu"}, {Name: "Alpha"}},
		Types: &wsdl.Types11{Schemas: []*xsd.Document{
			{TargetNamespace: "urn:z"}, {TargetNamespace: "urn:a"},
		}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument11() error = %v", err)
	}
	merged11, err := Merge(document11)
	if err != nil {
		t.Fatalf("Merge(WSDL 1.1) error = %v", err)
	}
	definitions, _ := merged11.Definitions11()
	if definitions.Messages[0].Name != "Alpha" || definitions.PortTypes[0].Name != "Alpha" ||
		definitions.Bindings[0].Name != "Alpha" || definitions.Services[0].Name != "Alpha" {
		t.Fatalf("Definitions11() = %#v", definitions)
	}
}
