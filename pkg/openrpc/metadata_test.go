package openrpc_test

import (
	"errors"
	"reflect"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
)

func TestMetadataObjectsPreserveOptionalPresence(t *testing.T) {
	t.Parallel()

	name := "API contact"
	contact, err := openrpc.NewContact(openrpc.ContactInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := contact.Name(); !ok || got != name {
		t.Fatalf("Name() = (%q, %t)", got, ok)
	}
	if _, ok := contact.Email(); ok {
		t.Fatal("Email reported present")
	}

	license, err := openrpc.NewLicense(openrpc.LicenseInput{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{
		Title:   "Example API",
		Version: "2026.1",
		Contact: &contact,
		License: &license,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.Title() != "Example API" || info.Version() != "2026.1" {
		t.Fatalf("unexpected info: %q %q", info.Title(), info.Version())
	}
	if _, ok := info.Contact(); !ok {
		t.Fatal("Contact reported absent")
	}
	if _, ok := info.Description(); ok {
		t.Fatal("Description reported present")
	}
}

func TestRequiredMetadataFieldsAreExplicit(t *testing.T) {
	t.Parallel()

	_, err := openrpc.NewInfo(openrpc.InfoInput{Version: "1"})
	if !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("NewInfo error = %v", err)
	}
	_, err = openrpc.NewExternalDocumentation(openrpc.ExternalDocumentationInput{})
	if !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("NewExternalDocumentation error = %v", err)
	}
	_, err = openrpc.NewServer(openrpc.ServerInput{})
	if !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("NewServer error = %v", err)
	}
	_, err = openrpc.NewServerVariable(openrpc.ServerVariableInput{})
	if !errors.Is(err, openrpc.ErrMissingRequiredField) {
		t.Fatalf("NewServerVariable error = %v", err)
	}
}

func TestServerOwnsVariablesAndEnumValues(t *testing.T) {
	t.Parallel()

	defaultValue := "production"
	variable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{
		Default: &defaultValue,
		Enum:    []string{"production", "staging"},
		HasEnum: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	variables := map[string]openrpc.ServerVariable{"environment": variable}
	server, err := openrpc.NewServer(openrpc.ServerInput{
		URL:          "https://{environment}.example.com",
		Variables:    variables,
		HasVariables: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	delete(variables, "environment")

	gotVariables, ok := server.Variables()
	if !ok || len(gotVariables) != 1 {
		t.Fatalf("Variables() = (%#v, %t)", gotVariables, ok)
	}
	delete(gotVariables, "environment")
	if lenMust(server.Variables()) != 1 {
		t.Fatal("Variables exposed mutable internal storage")
	}

	values, ok := variable.Enum()
	if !ok || !reflect.DeepEqual(values, []string{"production", "staging"}) {
		t.Fatalf("Enum() = (%#v, %t)", values, ok)
	}
	values[0] = "changed"
	valuesAgain, _ := variable.Enum()
	if valuesAgain[0] != "production" {
		t.Fatal("Enum exposed mutable internal storage")
	}
}

func TestServerPreservesRelativeURLsAndRichText(t *testing.T) {
	t.Parallel()

	description := "**Internal** deployment endpoint"
	server, err := openrpc.NewServer(openrpc.ServerInput{
		URL: "../rpc", Description: &description,
	})
	if err != nil {
		t.Fatal(err)
	}
	if server.URL() != "../rpc" {
		t.Fatalf("URL() = %q", server.URL())
	}
	if actual, present := server.Description(); !present || actual != description {
		t.Fatalf("Description() = (%q, %t)", actual, present)
	}
}

func lenMust(values map[string]openrpc.ServerVariable, ok bool) int {
	if !ok {
		return -1
	}
	return len(values)
}
