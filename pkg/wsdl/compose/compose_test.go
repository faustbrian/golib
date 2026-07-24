package compose_test

import (
	"bytes"
	"errors"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	"github.com/faustbrian/golib/pkg/wsdl/compose"
	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestMergeWSDL20IsDeterministic(t *testing.T) {
	t.Parallel()

	left := document20(t, "Zulu")
	right := document20(t, "Alpha")
	merged, err := compose.Merge(left, right)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	description, _ := merged.Description20()
	if len(description.Interfaces) != 2 || description.Interfaces[0].Name != "Alpha" ||
		description.Interfaces[1].Name != "Zulu" {
		t.Fatalf("Interfaces = %#v", description.Interfaces)
	}
	reversed, err := compose.Merge(right, left)
	if err != nil {
		t.Fatalf("Merge(reversed) error = %v", err)
	}
	first, err := wsdl.Marshal(merged, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	second, err := wsdl.Marshal(reversed, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal(reversed) error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("composition differs by input order:\n%s\n%s", first, second)
	}
}

func TestMergeReportsComponentConflicts(t *testing.T) {
	t.Parallel()

	_, err := compose.Merge(document20(t, "API"), document20(t, "API"))
	if !errors.Is(err, compose.ErrConflict) {
		t.Fatalf("Merge() error = %v, want ErrConflict", err)
	}
	var conflicts *compose.ConflictError
	if !errors.As(err, &conflicts) || len(conflicts.Conflicts) != 1 ||
		conflicts.Conflicts[0].Kind != "interface" ||
		conflicts.Conflicts[0].Name != "API" {
		t.Fatalf("ConflictError = %#v", conflicts)
	}
}

func TestMergeRejectsVersionAndNamespaceMismatch(t *testing.T) {
	t.Parallel()

	otherNamespace, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:other",
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument20() error = %v", err)
	}
	if _, err := compose.Merge(document20(t, "API"), otherNamespace); !errors.Is(err, compose.ErrNamespace) {
		t.Fatalf("Merge() error = %v, want ErrNamespace", err)
	}
	version11, err := wsdl.NewDocument11(wsdl.Definitions11{
		TargetNamespace: "urn:test",
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument11() error = %v", err)
	}
	if _, err := compose.Merge(document20(t, "API"), version11); !errors.Is(err, compose.ErrVersion) {
		t.Fatalf("Merge() error = %v, want ErrVersion", err)
	}
}

func TestMergeRejectsEmptyAndNilDocuments(t *testing.T) {
	t.Parallel()

	if _, err := compose.Merge(); !errors.Is(err, compose.ErrEmpty) {
		t.Fatalf("Merge() error = %v", err)
	}
	if _, err := compose.Merge(nil); err == nil {
		t.Fatal("Merge(nil) error = nil")
	}
}

func TestMergeWSDL11SortsTopLevelComponents(t *testing.T) {
	t.Parallel()

	left, err := wsdl.NewDocument11(wsdl.Definitions11{
		Name: "Zulu", TargetNamespace: "urn:test",
		Messages: []wsdl.Message11{{Name: "Zulu"}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument11() error = %v", err)
	}
	right, err := wsdl.NewDocument11(wsdl.Definitions11{
		Name: "Alpha", TargetNamespace: "urn:test",
		Messages: []wsdl.Message11{{Name: "Alpha"}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument11() error = %v", err)
	}
	merged, err := compose.Merge(left, right)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	definitions, _ := merged.Definitions11()
	if definitions.Name != "Alpha" || len(definitions.Messages) != 2 ||
		definitions.Messages[0].Name != "Alpha" || definitions.Messages[1].Name != "Zulu" {
		t.Fatalf("Definitions11() = %#v", definitions)
	}
}

func TestMergeCombinesTypesImportsIncludesAndExtensions(t *testing.T) {
	t.Parallel()

	extension := wsdl.Extension{
		Name: wsdl.QName{Namespace: "urn:extension", Local: "policy"},
		XML:  []byte(`<e:policy xmlns:e="urn:extension"/>`),
	}
	left, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:test",
		Documentation:   &wsdl.Documentation{Language: "z", Content: "Zulu"},
		Imports:         []wsdl.Import20{{Namespace: "urn:other", Location: "other.wsdl"}},
		Includes:        []wsdl.Include20{{Location: "same.wsdl"}},
		Types:           &wsdl.Types20{Schemas: []*xsd.Document{{TargetNamespace: "urn:z"}}},
		Extensibility:   wsdl.Extensibility{Extensions: []wsdl.Extension{extension}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	right, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:test",
		Documentation:   &wsdl.Documentation{Language: "a", Content: "Alpha"},
		Imports:         []wsdl.Import20{{Namespace: "urn:other", Location: "other.wsdl"}},
		Includes:        []wsdl.Include20{{Location: "same.wsdl"}},
		Types:           &wsdl.Types20{Schemas: []*xsd.Document{{TargetNamespace: "urn:a"}}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := compose.Merge(left, right)
	if err != nil {
		t.Fatalf("Merge() error = %v", err)
	}
	description, _ := merged.Description20()
	if len(description.Imports) != 1 || len(description.Includes) != 1 ||
		description.Documentation.Content != "Alpha" || len(description.Types.Schemas) != 2 ||
		description.Types.Schemas[0].TargetNamespace != "urn:a" || len(description.Extensions) != 1 {
		t.Fatalf("Description20() = %#v", description)
	}

	if (&compose.ConflictError{}).Error() != compose.ErrConflict.Error() ||
		(*compose.ConflictError)(nil).Error() != compose.ErrConflict.Error() {
		t.Fatal("empty ConflictError does not use sentinel text")
	}
}

func TestMergeWSDL11CombinesTypesAndImports(t *testing.T) {
	t.Parallel()

	left, err := wsdl.NewDocument11(wsdl.Definitions11{
		Name: "Zulu", TargetNamespace: "urn:test",
		Documentation: &wsdl.Documentation{Content: "Zulu"},
		Imports:       []wsdl.Import11{{Namespace: "urn:other", Location: "other.wsdl"}},
		Types:         &wsdl.Types11{Schemas: []*xsd.Document{{TargetNamespace: "urn:z"}}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	right, err := wsdl.NewDocument11(wsdl.Definitions11{
		Name: "Alpha", TargetNamespace: "urn:test",
		Imports: []wsdl.Import11{{Namespace: "urn:other", Location: "other.wsdl"}},
		Types:   &wsdl.Types11{Schemas: []*xsd.Document{{TargetNamespace: "urn:a"}}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := compose.Merge(left, right)
	if err != nil {
		t.Fatal(err)
	}
	definitions, _ := merged.Definitions11()
	if len(definitions.Imports) != 1 || len(definitions.Types.Schemas) != 2 ||
		definitions.Types.Schemas[0].TargetNamespace != "urn:a" ||
		definitions.Documentation.Content != "Zulu" {
		t.Fatalf("Definitions11() = %#v", definitions)
	}
}

func TestMergeReportsEveryTopLevelConflictKind(t *testing.T) {
	t.Parallel()

	want20 := wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces:      []wsdl.Interface20{{Name: "API"}},
		Bindings: []wsdl.Binding20{{
			Name: "Binding", Interface: wsdl.QName{Namespace: "urn:test", Local: "API"},
			Type: "urn:binding",
		}},
		Services: []wsdl.Service20{{
			Name: "Service", Interface: wsdl.QName{Namespace: "urn:test", Local: "API"},
			Endpoints: []wsdl.Endpoint20{{
				Name: "Endpoint", Binding: wsdl.QName{Namespace: "urn:test", Local: "Binding"},
				Address: "https://example.test",
			}},
		}},
	}
	left20, err := wsdl.NewDocument20(want20, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	right20, err := wsdl.NewDocument20(want20, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compose.Merge(left20, right20)
	var conflicts *compose.ConflictError
	if !errors.As(err, &conflicts) || len(conflicts.Conflicts) != 3 {
		t.Fatalf("WSDL 2.0 conflicts = %#v", conflicts)
	}

	want11 := wsdl.Definitions11{
		TargetNamespace: "urn:test",
		Messages:        []wsdl.Message11{{Name: "Message"}},
		PortTypes:       []wsdl.PortType11{{Name: "Port"}},
		Bindings: []wsdl.Binding11{{
			Name: "Binding", Type: wsdl.QName{Namespace: "urn:test", Local: "Port"},
		}},
		Services: []wsdl.Service11{{
			Name: "Service", Ports: []wsdl.Port11{{
				Name: "Endpoint", Binding: wsdl.QName{Namespace: "urn:test", Local: "Binding"},
			}},
		}},
	}
	left11, err := wsdl.NewDocument11(want11, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	right11, err := wsdl.NewDocument11(want11, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = compose.Merge(left11, right11)
	if !errors.As(err, &conflicts) || len(conflicts.Conflicts) != 4 {
		t.Fatalf("WSDL 1.1 conflicts = %#v", conflicts)
	}
	if conflicts.Is(errors.New("different")) {
		t.Fatal("ConflictError.Is() matched unrelated error")
	}
}

func TestMergeSortsRootAndTypeExtensibility(t *testing.T) {
	t.Parallel()

	extensions := []wsdl.Extension{
		{Name: wsdl.QName{Namespace: "urn:z", Local: "same"}, XML: []byte(`<z:same xmlns:z="urn:z">z</z:same>`)},
		{Name: wsdl.QName{Namespace: "urn:a", Local: "z"}, XML: []byte(`<a:z xmlns:a="urn:a"/>`)},
		{Name: wsdl.QName{Namespace: "urn:a", Local: "a"}, XML: []byte(`<a:a xmlns:a="urn:a">z</a:a>`)},
		{Name: wsdl.QName{Namespace: "urn:a", Local: "a"}, XML: []byte(`<a:a xmlns:a="urn:a">a</a:a>`)},
	}
	attributes := []wsdl.ExtensionAttribute{
		{Name: wsdl.QName{Namespace: "urn:z", Local: "a"}, Value: "z"},
		{Name: wsdl.QName{Namespace: "urn:a", Local: "z"}, Value: "z"},
		{Name: wsdl.QName{Namespace: "urn:a", Local: "a"}, Value: "a"},
	}
	left, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:test",
		Extensibility: wsdl.Extensibility{
			Extensions: extensions, ExtensionAttributes: attributes,
		},
		Types: &wsdl.Types20{
			Imports: []xsd.SchemaReference{
				{Namespace: "urn:z", Location: "z.xsd"},
				{Namespace: "urn:a", Location: "z.xsd"},
				{Namespace: "urn:a", Location: "a.xsd"},
			},
			Extensibility: wsdl.Extensibility{
				Extensions: extensions, ExtensionAttributes: attributes,
			},
		},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := compose.Merge(left)
	if err != nil {
		t.Fatal(err)
	}
	description, _ := merged.Description20()
	if description.Extensions[0].Name.Namespace != "urn:a" ||
		description.Extensions[0].Name.Local != "a" ||
		description.ExtensionAttributes[0].Name.Local != "a" ||
		description.Types.Imports[0].Location != "a.xsd" {
		t.Fatalf("Description20() = %#v", description)
	}
}

func document20(t *testing.T, name string) *wsdl.Document {
	t.Helper()
	document, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces:      []wsdl.Interface20{{Name: name}},
	}, wsdl.ValidationOptions{})
	if err != nil {
		t.Fatalf("NewDocument20() error = %v", err)
	}
	return document
}
