package openrpc_test

import (
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestDocumentRequiresVersionInfoAndMethods(t *testing.T) {
	t.Parallel()

	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "API", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range []openrpc.DocumentInput{
		{Info: &info, Methods: []openrpc.MethodOrReference{}},
		{Version: version, Methods: []openrpc.MethodOrReference{}},
		{Version: version, Info: &info},
	} {
		if _, err := openrpc.NewDocument(input); !errors.Is(err, openrpc.ErrMissingRequiredField) {
			t.Fatalf("NewDocument error = %v", err)
		}
	}
	_, err = openrpc.NewDocument(openrpc.DocumentInput{
		Version: version, Info: &info,
	})
	var missing *openrpc.MissingRequiredFieldError
	if !errors.As(err, &missing) || missing.Error() == "" || missing.Field != "methods" {
		t.Fatalf("missing field error = %#v", err)
	}
}

func TestDocumentPreservesEmptyMethodsAndServerDefault(t *testing.T) {
	t.Parallel()

	version, err := openrpc.ParseVersion("1.4.99")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Filtered API", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	methods := []openrpc.MethodOrReference{}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version,
		Info:    &info,
		Methods: methods,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Methods()) != 0 || document.MethodCount() != 0 {
		t.Fatalf("Methods() = %#v", document.Methods())
	}
	if _, present := document.Servers(); present {
		t.Fatal("Servers reported present")
	}
	effective := document.EffectiveServers()
	if len(effective) != 1 || effective[0].URL() != "localhost" {
		t.Fatalf("EffectiveServers() = %#v", effective)
	}
	if uri, present := document.SchemaURI(); present || uri != "https://meta.open-rpc.org/" {
		t.Fatalf("SchemaURI() = (%q, %t)", uri, present)
	}
}

func TestComponentsOwnReusableMaps(t *testing.T) {
	t.Parallel()

	schema, err := jsonschema.Parse([]byte(`true`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	schemas := map[string]jsonschema.Schema{"Anything": schema}
	components, err := openrpc.NewComponents(openrpc.ComponentsInput{Schemas: schemas})
	if err != nil {
		t.Fatal(err)
	}
	delete(schemas, "Anything")

	got, present := components.Schemas()
	if !present || len(got) != 1 {
		t.Fatalf("Schemas() = (%#v, %t)", got, present)
	}
	delete(got, "Anything")
	gotAgain, _ := components.Schemas()
	if len(gotAgain) != 1 {
		t.Fatal("Schemas exposed mutable internal storage")
	}
}
