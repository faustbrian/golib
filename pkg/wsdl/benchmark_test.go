package wsdl_test

import (
	"context"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
)

var benchmarkWSDL = []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
	` xmlns:tns="urn:benchmark" targetNamespace="urn:benchmark">` +
	`<interface name="API"><operation name="Call"` +
	` pattern="http://www.w3.org/ns/wsdl/in-out"><input element="#none"/>` +
	`<output element="#none"/></operation></interface>` +
	`<binding name="Binding" interface="tns:API" type="urn:binding">` +
	`<operation ref="tns:Call"/></binding><service name="Service" interface="tns:API">` +
	`<endpoint name="Endpoint" binding="tns:Binding" address="https://example.test"/>` +
	`</service></description>`)

func BenchmarkParseWSDL(b *testing.B) {
	for b.Loop() {
		if _, err := wsdl.Parse(context.Background(), benchmarkWSDL, wsdl.ParseOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalWSDL(b *testing.B) {
	document, err := wsdl.Parse(context.Background(), benchmarkWSDL, wsdl.ParseOptions{})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := wsdl.Marshal(document, wsdl.MarshalOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileWSDL(b *testing.B) {
	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := compiler.Compile(context.Background(), wsdlcompile.Source{
			URI: "https://example.test/service.wsdl", Content: benchmarkWSDL,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
