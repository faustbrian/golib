package wsdl_test

import (
	"context"
	"errors"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestParseRecognizesWSDL11Definitions(t *testing.T) {
	t.Parallel()

	source := []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
		` name="StockQuote" targetNamespace="urn:stock"/>`)

	document, err := wsdl.Parse(context.Background(), source, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if document.Version() != wsdl.Version11 {
		t.Fatalf("Version() = %q, want %q", document.Version(), wsdl.Version11)
	}
	definitions, ok := document.Definitions11()
	if !ok {
		t.Fatal("Definitions11() did not return the WSDL 1.1 model")
	}
	if definitions.Name != "StockQuote" || definitions.TargetNamespace != "urn:stock" {
		t.Fatalf("Definitions11() = %#v", definitions)
	}
}

func TestParseRejectsDocumentsBeyondDepthLimit(t *testing.T) {
	t.Parallel()

	source := []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/">` +
		`<message name="input"><part name="value" type="string"/></message>` +
		`</definitions>`)

	_, err := wsdl.Parse(context.Background(), source, wsdl.ParseOptions{MaxDepth: 2})
	if !errors.Is(err, wsdl.ErrLimitExceeded) {
		t.Fatalf("Parse() error = %v, want ErrLimitExceeded", err)
	}
}

func TestParseRecognizesWSDL20Description(t *testing.T) {
	t.Parallel()

	source := []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:stock"/>`)

	document, err := wsdl.Parse(context.Background(), source, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if document.Version() != wsdl.Version20 {
		t.Fatalf("Version() = %q, want %q", document.Version(), wsdl.Version20)
	}
	description, ok := document.Description20()
	if !ok {
		t.Fatal("Description20() did not return the WSDL 2.0 model")
	}
	if description.TargetNamespace != "urn:stock" {
		t.Fatalf("Description20() = %#v", description)
	}
}

func TestParseAcceptsExactDocumentAndComponentLimits(t *testing.T) {
	t.Parallel()

	source := []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"><include location="child.wsdl"/>` +
		`</description>`)
	document, err := wsdl.Parse(context.Background(), source, wsdl.ParseOptions{
		MaxDocumentBytes: int64(len(source)),
		MaxImports:       1,
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if document.Version() != wsdl.Version20 {
		t.Fatalf("Version() = %q, want %q", document.Version(), wsdl.Version20)
	}
}

func TestParseEnforcesComponentLimitsBeforeModelConstruction(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body    string
		options wsdl.ParseOptions
	}{
		"imports": {
			body:    `<include location="one.wsdl"/><include location="two.wsdl"/>`,
			options: wsdl.ParseOptions{MaxImports: 1},
		},
		"operations": {
			body: `<interface name="API"><operation name="One" pattern="urn:one"/>` +
				`<operation name="Two" pattern="urn:two"/></interface>`,
			options: wsdl.ParseOptions{MaxOperations: 1},
		},
		"bindings": {
			body:    `<binding name="One"/><binding name="Two"/>`,
			options: wsdl.ParseOptions{MaxBindings: 1},
		},
		"endpoints": {
			body: `<service name="API"><endpoint name="One"/>` +
				`<endpoint name="Two"/></service>`,
			options: wsdl.ParseOptions{MaxEndpoints: 1},
		},
		"extensions": {
			body:    `<ext:one/><ext:two/>`,
			options: wsdl.ParseOptions{MaxExtensions: 1},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := `<description xmlns="http://www.w3.org/ns/wsdl"` +
				` xmlns:ext="urn:extension" targetNamespace="urn:test">` +
				test.body + `</description>`
			_, err := wsdl.Parse(
				context.Background(),
				[]byte(source),
				test.options,
			)
			if !errors.Is(err, wsdl.ErrLimitExceeded) {
				t.Fatalf("Parse() error = %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestParseRejectsDuplicateComponents(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"WSDL 1.1 messages": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` targetNamespace="urn:test"><message name="Same"/><message name="Same"/>` +
			`</definitions>`,
		"WSDL 1.1 parts": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:x="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
			`<message name="M"><part name="same" type="x:string"/>` +
			`<part name="same" type="x:string"/>` +
			`</message></definitions>`,
		"WSDL 2.0 operations": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><interface name="I"><operation name="same"` +
			` pattern="urn:custom"/><operation name="same" pattern="urn:custom"/>` +
			`</interface></description>`,
		"WSDL 2.0 endpoints": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><service name="S" interface="x:I" xmlns:x="urn:test">` +
			`<endpoint name="same" binding="x:B"/><endpoint name="same" binding="x:B"/>` +
			`</service></description>`,
	}
	for name, source := range tests {
		name, source := name, source
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
			if !errors.Is(err, wsdl.ErrDuplicateSymbol) {
				t.Fatalf("Parse() error = %v, want ErrDuplicateSymbol", err)
			}
		})
	}
}

func TestParseRejectsInvalidQNameLexicalForms(t *testing.T) {
	t.Parallel()

	for _, lexical := range []string{
		" value", "value ", ":value", "value:", "one:two:three",
		"bad name", "1invalid", "prefix:1invalid",
	} {
		lexical := lexical
		t.Run(lexical, func(t *testing.T) {
			t.Parallel()
			source := `<description xmlns="http://www.w3.org/ns/wsdl"` +
				` xmlns:prefix="urn:test" targetNamespace="urn:test">` +
				`<interface name="API"><fault name="Failure" element="` +
				lexical + `"/></interface></description>`
			if _, err := wsdl.Parse(
				context.Background(), []byte(source), wsdl.ParseOptions{},
			); err == nil {
				t.Fatalf("Parse() accepted QName %q", lexical)
			}
		})
	}
}

func TestParseRejectsInvalidCoreNCNames(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"WSDL 1.1": `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/">` +
			`<message name="bad name"/></definitions>`,
		"WSDL 2.0": `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><interface name="1invalid"/>` +
			`</description>`,
	} {
		source := source
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := wsdl.Parse(
				context.Background(), []byte(source), wsdl.ParseOptions{},
			); err == nil {
				t.Fatal("Parse() accepted an invalid core NCName")
			}
		})
	}
}
