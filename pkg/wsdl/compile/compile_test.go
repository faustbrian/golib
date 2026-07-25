package compile_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
	"github.com/faustbrian/golib/pkg/wsdl/resolve"
	xsdresolve "github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestCompilerResolvesBoundedWSDL20Graph(t *testing.T) {
	t.Parallel()

	const root = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:api" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:api">` +
		`<include location="extra.wsdl"/>` +
		`<import namespace="urn:common" location="common.wsdl"/>` +
		`<types><xs:schema targetNamespace="urn:api">` +
		`<xs:element name="Request" type="xs:string"/>` +
		`</xs:schema></types>` +
		`<interface name="API"><operation name="Call" pattern="urn:in-only"` +
		`><input element="tns:Request"/></operation></interface>` +
		`<binding name="Binding" interface="tns:API" type="urn:binding">` +
		`<operation ref="tns:Call"/></binding>` +
		`<service name="Service" interface="tns:API"><endpoint name="Endpoint"` +
		` binding="tns:Binding" address="https://example.test/api"/>` +
		`</service></description>`
	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/extra.wsdl": []byte(
			`<description xmlns="http://www.w3.org/ns/wsdl"` +
				` targetNamespace="urn:api"><include location="root.wsdl"/>` +
				`<interface name="Extra"/></description>`,
		),
		"https://example.test/common.wsdl": []byte(
			`<description xmlns="http://www.w3.org/ns/wsdl"` +
				` targetNamespace="urn:common"><interface name="Common"/>` +
				`</description>`,
		),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{Resolver: resolver})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl", Content: []byte(root),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if got := set.Documents(); len(got) != 3 ||
		got[0].URI != "https://example.test/common.wsdl" ||
		got[1].URI != "https://example.test/extra.wsdl" ||
		got[2].URI != "https://example.test/root.wsdl" {
		t.Fatalf("Documents() = %#v", got)
	}
	interfaces := set.Interfaces()
	if len(interfaces) != 3 || interfaces[0].Name.Namespace != "urn:api" ||
		interfaces[0].Name.Local != "API" ||
		len(interfaces[0].Operations) != 1 ||
		interfaces[0].Operations[0].Name != "Call" {
		t.Fatalf("Interfaces() = %#v", interfaces)
	}
	bindings := set.Bindings()
	services := set.Services()
	if len(bindings) != 1 || bindings[0].Interface.Local != "API" ||
		len(services) != 1 || len(services[0].Endpoints) != 1 ||
		services[0].Endpoints[0].Address != "https://example.test/api" {
		t.Fatalf("Bindings() = %#v, Services() = %#v", bindings, services)
	}
	interfaces[0].Operations[0].Name = "mutated"
	if set.Interfaces()[0].Operations[0].Name != "Call" {
		t.Fatal("Interfaces() exposed mutable set storage")
	}

	const readers = 32
	var wait sync.WaitGroup
	wait.Add(readers)
	for range readers {
		go func() {
			defer wait.Done()
			for range 100 {
				documents := set.Documents()
				interfaces := set.Interfaces()
				bindings := set.Bindings()
				services := set.Services()
				if len(documents) != 3 || len(interfaces) != 3 ||
					len(bindings) != 1 || len(services) != 1 {
					t.Errorf("concurrent set lookup returned incomplete graph")
					return
				}
				interfaces[0].Operations[0].Name = "caller-owned"
				bindings[0].Operations = nil
				services[0].Endpoints = nil
			}
		}()
	}
	wait.Wait()
	if set.Interfaces()[0].Operations[0].Name != "Call" ||
		len(set.Bindings()[0].Operations) != 1 ||
		len(set.Services()[0].Endpoints) != 1 {
		t.Fatal("concurrent callers mutated compiled set storage")
	}
}

func TestCompilerDefaultsToDeniedResolution(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` targetNamespace="urn:root"><import namespace="urn:child"` +
			` location="child.wsdl"/></definitions>`),
	})
	if !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Compile() error = %v, want ErrAccessDenied", err)
	}
}

func TestCompilerRejectsNamespaceMismatch(t *testing.T) {
	t.Parallel()

	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/child.wsdl": []byte(
			`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
				` targetNamespace="urn:wrong"/>`,
		),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{Resolver: resolver})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` targetNamespace="urn:root"><import namespace="urn:child"` +
			` location="child.wsdl"/></definitions>`),
	})
	if !errors.Is(err, wsdlcompile.ErrNamespace) {
		t.Fatalf("Compile() error = %v, want ErrNamespace", err)
	}
}

func TestCompilerRejectsUnresolvedBindingOperation(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test">` +
			`<interface name="API"><operation name="Known" pattern="urn:in-only"/>` +
			`</interface><binding name="Binding" interface="tns:API"` +
			` type="urn:binding"><operation ref="tns:Missing"/>` +
			`</binding></description>`),
	})
	if !errors.Is(err, wsdlcompile.ErrUnresolvedComponent) {
		t.Fatalf("Compile() error = %v, want ErrUnresolvedComponent", err)
	}
}

func TestCompilerBoundsNestedComponents(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{Limits: wsdlcompile.Limits{
		MaxComponents: 3,
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test">` +
			`<interface name="API">` +
			`<operation name="One" pattern="urn:in-only"/>` +
			`<operation name="Two" pattern="urn:in-only"/>` +
			`</interface><binding name="Binding" interface="tns:API"` +
			` type="urn:binding"/></description>`),
	})
	if !errors.Is(err, wsdlcompile.ErrLimitExceeded) {
		t.Fatalf("Compile() error = %v, want ErrLimitExceeded", err)
	}
}

func TestCompilerBoundsWSDL20BindingReferences(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{Limits: wsdlcompile.Limits{
		MaxComponents: 9,
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test">` +
			`<interface name="API"><fault name="Failure" element="tns:Failure"/>` +
			`<operation name="Call" pattern="urn:in-only">` +
			`<input messageLabel="In" element="tns:Request"/>` +
			`<infault ref="tns:Failure" messageLabel="In"/>` +
			`</operation></interface>` +
			`<binding name="Binding" interface="tns:API" type="urn:binding">` +
			`<fault ref="tns:Failure"/><operation ref="tns:Call">` +
			`<input messageLabel="In"/>` +
			`<infault ref="tns:Failure" messageLabel="In"/>` +
			`</operation></binding></description>`),
	})
	if !errors.Is(err, wsdlcompile.ErrLimitExceeded) {
		t.Fatalf("Compile() error = %v, want ErrLimitExceeded", err)
	}
}

func TestCompilerCompilesInlineSchemas(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test"><types><xs:schema targetNamespace="urn:test"` +
			` elementFormDefault="qualified"><xs:element name="Request"` +
			` type="xs:string"/></xs:schema></types><interface name="API">` +
			`<operation name="Call" pattern="urn:in-only">` +
			`<input element="tns:Request"/></operation>` +
			`</interface></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if set.Schemas() == nil {
		t.Fatal("Schemas() = nil")
	}
	names := set.Schemas().ElementNames()
	if len(names) != 1 || names[0].Namespace != "urn:test" ||
		names[0].Local != "Request" {
		t.Fatalf("Schemas().ElementNames() = %#v", names)
	}
}

func TestCompiledSetSupportsOwnedComponentLookup(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test">` +
			`<interface name="API"><operation name="Call" pattern="urn:in-only"/>` +
			`</interface><binding name="Binding" interface="tns:API" type="urn:test"` +
			`><operation ref="tns:Call"/></binding><service name="Service"` +
			` interface="tns:API"><endpoint name="Endpoint" binding="tns:Binding"/>` +
			`</service></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	interfaceName := wsdl.QName{Namespace: "urn:test", Local: "API"}
	interfaceValue, ok := set.Interface(interfaceName)
	if !ok || len(interfaceValue.Operations) != 1 {
		t.Fatalf("Interface() = %#v, %t", interfaceValue, ok)
	}
	interfaceValue.Operations[0].Name = "Changed"
	owned, _ := set.Interface(interfaceName)
	if owned.Operations[0].Name != "Call" {
		t.Fatalf("Interface() leaked mutable storage: %#v", owned)
	}
	if operation, ok := set.Operation(interfaceName, "Call"); !ok ||
		operation.Pattern != "urn:in-only" {
		t.Fatalf("Operation() = %#v, %t", operation, ok)
	}
	if _, ok := set.Binding(wsdl.QName{Namespace: "urn:test", Local: "Binding"}); !ok {
		t.Fatal("Binding() did not find Binding")
	}
	if _, ok := set.Service(wsdl.QName{Namespace: "urn:test", Local: "Service"}); !ok {
		t.Fatal("Service() did not find Service")
	}
	if _, ok := set.Interface(wsdl.QName{Namespace: "urn:test", Local: "Missing"}); ok {
		t.Fatal("Interface() found Missing")
	}
}

func TestCompilerRejectsUnknownWSDL20SchemaElement(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test"><types><xs:schema targetNamespace="urn:test"/>` +
			`</types><interface name="API"><operation name="Call" pattern="urn:in-only">` +
			`<input element="tns:Missing"/></operation>` +
			`</interface></description>`),
	})
	if !errors.Is(err, wsdlcompile.ErrUnresolvedComponent) {
		t.Fatalf("Compile() error = %v, want ErrUnresolvedComponent", err)
	}
}

func TestCompilerUsesOnlyInjectedSchemaResolver(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:types="urn:types" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
		` targetNamespace="urn:test"><types>` +
		`<xs:import namespace="urn:types" schemaLocation="types.xsd"/>` +
		`</types><interface name="API"><operation name="Call"` +
		` pattern="urn:in-only"><input element="types:Request"/>` +
		`</operation></interface></description>`

	denied, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := denied.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl", Content: []byte(source),
	}); !errors.Is(err, xsdresolve.ErrAccessDenied) {
		t.Fatalf("Compile() error = %v, want schema access denial", err)
	}

	memory, err := xsdresolve.NewMemory(map[string][]byte{
		"https://example.test/types.xsd": []byte(
			`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
				` targetNamespace="urn:types"><xs:element name="Request"` +
				` type="xs:string"/></xs:schema>`,
		),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{SchemaResolver: memory})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl", Content: []byte(source),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(set.Schemas().ElementNames()) != 1 {
		t.Fatalf("Schemas().ElementNames() = %#v", set.Schemas().ElementNames())
	}
}

func TestCompilerRejectsUnknownWSDL11SchemaType(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test"><types><xs:schema targetNamespace="urn:test"/>` +
			`</types><message name="Request"><part name="value" type="tns:Missing"/>` +
			`</message></definitions>`),
	})
	if !errors.Is(err, wsdlcompile.ErrUnresolvedComponent) {
		t.Fatalf("Compile() error = %v, want ErrUnresolvedComponent", err)
	}
}

func TestCompilerEnforcesResourceBoundsAndIdentity(t *testing.T) {
	t.Parallel()

	const root = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"><include location="child.wsdl"/>` +
		`</description>`
	memory, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/child.wsdl": []byte(
			`<description xmlns="http://www.w3.org/ns/wsdl"` +
				` targetNamespace="urn:test"/>`,
		),
		"https://example.test/other.wsdl": []byte(
			`<description xmlns="http://www.w3.org/ns/wsdl"` +
				` targetNamespace="urn:test"/>`,
		),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	tests := map[string]struct {
		options wsdlcompile.Options
		source  string
	}{
		"documents": {
			options: wsdlcompile.Options{Resolver: memory, Limits: wsdlcompile.Limits{MaxDocuments: 1}},
			source:  root,
		},
		"depth": {
			options: wsdlcompile.Options{Resolver: memory, Limits: wsdlcompile.Limits{MaxDepth: 1}},
			source:  root,
		},
		"references": {
			options: wsdlcompile.Options{Resolver: memory, Limits: wsdlcompile.Limits{MaxReferences: 1}},
			source: `<description xmlns="http://www.w3.org/ns/wsdl"` +
				` targetNamespace="urn:test"><include location="child.wsdl"/>` +
				`<include location="other.wsdl"/></description>`,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler, newErr := wsdlcompile.New(test.options)
			if newErr != nil {
				t.Fatalf("New() error = %v", newErr)
			}
			_, compileErr := compiler.Compile(context.Background(), wsdlcompile.Source{
				URI: "https://example.test/root.wsdl", Content: []byte(test.source),
			})
			if !errors.Is(compileErr, wsdlcompile.ErrLimitExceeded) {
				t.Fatalf("Compile() error = %v, want ErrLimitExceeded", compileErr)
			}
		})
	}

	compiler, err := wsdlcompile.New(wsdlcompile.Options{
		Resolver: resolverFunc(func(context.Context, resolve.Request) (resolve.Resource, error) {
			return resolve.Resource{URI: "https://example.test/other.wsdl"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl", Content: []byte(root),
	})
	if !errors.Is(err, wsdlcompile.ErrResourceIdentity) {
		t.Fatalf("Compile(identity mismatch) error = %v", err)
	}
}

func TestCompilerAcceptsGraphsAtEveryExactResourceLimit(t *testing.T) {
	t.Parallel()

	root := []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"><include location="child.wsdl"/>` +
		`<interface name="API"/></description>`)
	child := []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"/>`)
	memory, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/child.wsdl": child,
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{
		Resolver: memory,
		Limits: wsdlcompile.Limits{
			MaxDocuments:  2,
			MaxDepth:      2,
			MaxReferences: 1,
			MaxBytes:      int64(len(root) + len(child)),
			MaxComponents: 1,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl", Content: root,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(set.Documents()) != 2 {
		t.Fatalf("documents = %d, want 2", len(set.Documents()))
	}

	rootOnly := []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
		` targetNamespace="urn:test"/>`)
	rootCompiler, err := wsdlcompile.New(wsdlcompile.Options{
		Limits: wsdlcompile.Limits{MaxBytes: int64(len(rootOnly))},
	})
	if err != nil {
		t.Fatalf("New(root-only) error = %v", err)
	}
	if _, err := rootCompiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root-only.wsdl", Content: rootOnly,
	}); err != nil {
		t.Fatalf("Compile(root-only exact bytes) error = %v", err)
	}
}

func TestCompilerRejectsInvalidOptionsAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	if _, err := wsdlcompile.New(wsdlcompile.Options{Limits: wsdlcompile.Limits{
		MaxDepth: -1,
	}}); err == nil {
		t.Fatal("New(negative limit) error = nil")
	}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = compiler.Compile(ctx, wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"/>`),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile(canceled) error = %v", err)
	}
}

func TestCompiledSetNilAndMissingLookups(t *testing.T) {
	t.Parallel()

	var set *wsdlcompile.Set
	if set.Schemas() != nil || set.Documents() != nil || set.Interfaces() != nil ||
		set.Bindings() != nil || set.Services() != nil {
		t.Fatal("nil Set collection accessor returned data")
	}
	name := wsdl.QName{Namespace: "urn:test", Local: "Missing"}
	if _, ok := set.Interface(name); ok {
		t.Fatal("nil Set.Interface() succeeded")
	}
	if _, ok := set.Operation(name, "Missing"); ok {
		t.Fatal("nil Set.Operation() succeeded")
	}
	if _, ok := set.OperationBySignature(name, "Missing", "", ""); ok {
		t.Fatal("nil Set.OperationBySignature() succeeded")
	}
	if _, ok := set.Binding(name); ok {
		t.Fatal("nil Set.Binding() succeeded")
	}
	if _, ok := set.Service(name); ok {
		t.Fatal("nil Set.Service() succeeded")
	}

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><interface name="Present"/></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if _, ok := set.Interface(name); ok {
		t.Fatal("missing Interface() succeeded")
	}
	if _, ok := set.Operation(name, "Missing"); ok {
		t.Fatal("missing Operation() succeeded")
	}
	if _, ok := set.OperationBySignature(
		wsdl.QName{Namespace: "urn:test", Local: "Present"}, "Missing", "", "",
	); ok {
		t.Fatal("missing OperationBySignature() succeeded")
	}
	if _, ok := set.Binding(name); ok {
		t.Fatal("missing Binding() succeeded")
	}
	if _, ok := set.Service(name); ok {
		t.Fatal("missing Service() succeeded")
	}
}

func TestCompilerRejectsNilReceiverAndOversizedRoot(t *testing.T) {
	t.Parallel()

	var compiler *wsdlcompile.Compiler
	if _, err := compiler.Compile(context.Background(), wsdlcompile.Source{}); err == nil {
		t.Fatal("nil Compiler.Compile() error = nil")
	}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{
		Limits: wsdlcompile.Limits{MaxBytes: 1},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl", Content: []byte("too large"),
	})
	if !errors.Is(err, wsdlcompile.ErrLimitExceeded) {
		t.Fatalf("Compile() error = %v", err)
	}
}

func TestCompilerBuildsTypedOperationMessages(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test"><types><xs:schema targetNamespace="urn:test">` +
			`<xs:element name="Result" type="xs:string"/></xs:schema></types>` +
			`<message name="Request">` +
			`<part name="id" type="xs:string"/></message>` +
			`<message name="Response"><part name="result" element="tns:Result"/>` +
			`</message><portType name="API"><operation name="Call">` +
			`<input name="request" message="tns:Request"/>` +
			`<output name="response" message="tns:Response"/>` +
			`</operation></portType></definitions>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	operation, ok := set.Operation(wsdl.QName{Namespace: "urn:test", Local: "API"}, "Call")
	if !ok || operation.Style != string(wsdl.OperationStyleRequestResponse) {
		t.Fatalf("Operation() = (%#v, %v)", operation, ok)
	}
	if operation.Input == nil || operation.Input.Label != "request" ||
		operation.Input.Name.Local != "Request" || len(operation.Input.Parts) != 1 ||
		operation.Input.Parts[0].Type.Local != "string" {
		t.Fatalf("compiled input = %#v", operation.Input)
	}
	if operation.Output == nil || operation.Output.Label != "response" ||
		operation.Output.Parts[0].Element.Local != "Result" {
		t.Fatalf("compiled output = %#v", operation.Output)
	}

	operation.Input.Parts[0].Name = "changed"
	again, _ := set.Operation(wsdl.QName{Namespace: "urn:test", Local: "API"}, "Call")
	if again.Input.Parts[0].Name != "id" {
		t.Fatalf("compiled graph was mutated: %#v", again.Input)
	}
}

func TestCompilerPreservesWSDL11OverloadedOperationIdentity(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><message name="Message"/>` +
			`<portType name="Port"><operation name="Call"><input name="First"` +
			` message="tns:Message"/></operation><operation name="Call"><input name="Second"` +
			` message="tns:Message"/></operation></portType><binding name="Binding"` +
			` type="tns:Port"><operation name="Call"><input name="Second"/>` +
			`</operation></binding></definitions>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	port := wsdl.QName{Namespace: "urn:test", Local: "Port"}
	if _, ok := set.Operation(port, "Call"); ok {
		t.Fatal("Operation() resolved an overloaded name")
	}
	operation, ok := set.OperationBySignature(port, "Call", "Second", "")
	if !ok || operation.Input == nil || operation.Input.Label != "Second" {
		t.Fatalf("OperationBySignature() = (%#v, %v)", operation, ok)
	}
	bindings := set.Bindings()
	if len(bindings) != 1 || len(bindings[0].OperationReferences) != 1 ||
		bindings[0].OperationReferences[0].Input != "Second" {
		t.Fatalf("Bindings() = %#v", bindings)
	}
}

func TestCompilerExpandsWSDL20InterfaceInheritance(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test"><interface name="Parent">` +
			`<fault name="Failure" element="#none"/>` +
			`<operation name="Inherited" pattern="http://www.w3.org/ns/wsdl/in-only">` +
			`<input element="#none"/></operation></interface>` +
			`<interface name="Child" extends="tns:Parent"/>` +
			`<binding name="Binding" interface="tns:Child" type="urn:binding">` +
			`<operation ref="tns:Inherited"/></binding></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	child, ok := set.Interface(wsdl.QName{Namespace: "urn:test", Local: "Child"})
	if !ok || len(child.Extends) != 1 || len(child.Operations) != 1 || len(child.Faults) != 1 ||
		child.Operations[0].Name != "Inherited" {
		t.Fatalf("Child interface = %#v", child)
	}
}

func TestCompilerPreservesMultipleWSDL20Messages(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"` +
			` targetNamespace="urn:test"><interface name="API">` +
			`<operation name="Exchange" pattern="urn:multi" wsdlx:safe="true">` +
			`<input messageLabel="First" element="#none"/>` +
			`<input messageLabel="Second" element="#any"/>` +
			`<output messageLabel="Third" element="#other"/>` +
			`<output messageLabel="Fourth" element="#none"/>` +
			`</operation></interface></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	operation, ok := set.Operation(wsdl.QName{Namespace: "urn:test", Local: "API"}, "Exchange")
	if !ok || !operation.Safe || len(operation.Inputs) != 2 || len(operation.Outputs) != 2 ||
		operation.Inputs[1].Label != "Second" || operation.Outputs[1].Label != "Fourth" ||
		operation.Input == nil || operation.Input.Label != "First" {
		t.Fatalf("Operation() = (%#v, %v)", operation, ok)
	}
	operation.Inputs[1].Label = "changed"
	again, _ := set.Operation(wsdl.QName{Namespace: "urn:test", Local: "API"}, "Exchange")
	if again.Inputs[1].Label != "Second" {
		t.Fatalf("compiled graph was mutated: %#v", again.Inputs)
	}
}

func TestCompilerPreservesWSDL20RPCSignature(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc"` +
			` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
			`<types><xs:schema targetNamespace="urn:test" elementFormDefault="qualified">` +
			`<xs:complexType name="CallType"><xs:sequence><xs:element` +
			` name="value" type="xs:string"/><xs:any/></xs:sequence></xs:complexType>` +
			`<xs:element name="Call" type="tns:CallType"/>` +
			`</xs:schema></types><interface name="API"><operation name="Call"` +
			` pattern="http://www.w3.org/ns/wsdl/in-only"` +
			` style="http://www.w3.org/ns/wsdl/style/rpc"` +
			` wrpc:signature="tns:value #in"><input element="tns:Call"/>` +
			`</operation></interface></description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	operation, ok := set.Operation(wsdl.QName{Namespace: "urn:test", Local: "API"}, "Call")
	if !ok || !operation.RPCSignatureSet || len(operation.RPCSignature) != 1 ||
		operation.RPCSignature[0].Direction != wsdl.RPCDirectionIn {
		t.Fatalf("Operation() = (%#v, %v)", operation, ok)
	}
	operation.RPCSignature[0].Name.Local = "changed"
	again, _ := set.Operation(wsdl.QName{Namespace: "urn:test", Local: "API"}, "Call")
	if again.RPCSignature[0].Name.Local != "value" {
		t.Fatalf("compiled graph was mutated: %#v", again.RPCSignature)
	}
}

func TestCompilerRejectsInvalidWSDL20RPCSchemas(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		schema    string
		signature string
	}{
		"simple wrapper": {
			schema:    `<xs:element name="Call" type="xs:string"/>` + rpcOutputSchema("result", "xs:string"),
			signature: `tns:value #in tns:result #return`,
		},
		"choice": {
			schema: rpcInputWith(`<xs:choice><xs:element name="value" type="xs:string"/></xs:choice>`) +
				rpcOutputSchema("result", "xs:string"),
			signature: `tns:value #in tns:result #return`,
		},
		"local attribute": {
			schema: rpcInputWith(`<xs:sequence><xs:element name="value" type="xs:string"/>`+
				`</xs:sequence><xs:attribute name="local" type="xs:string"/>`) +
				rpcOutputSchema("result", "xs:string"),
			signature: `tns:value #in tns:result #return`,
		},
		"output wildcard": {
			schema:    rpcInputSchema("value", "xs:string") + rpcOutputWith(`<xs:sequence><xs:any/></xs:sequence>`),
			signature: `tns:value #in`,
		},
		"input wildcard not last": {
			schema: rpcInputWith(`<xs:sequence><xs:any/><xs:element name="value"`+
				` type="xs:string"/></xs:sequence>`) + rpcOutputSchema("result", "xs:string"),
			signature: `tns:value #in tns:result #return`,
		},
		"referenced child": {
			schema: `<xs:element name="Global" type="xs:string"/>` +
				rpcInputWith(`<xs:sequence><xs:element ref="tns:Global"/></xs:sequence>`) +
				rpcOutputSchema("result", "xs:string"),
			signature: `tns:Global #in tns:result #return`,
		},
		"duplicate child": {
			schema: rpcInputWith(`<xs:sequence><xs:element name="value" type="xs:string"/>`+
				`<xs:element name="value" type="xs:string"/></xs:sequence>`) +
				rpcOutputSchema("result", "xs:string"),
			signature: `tns:value #in tns:result #return`,
		},
		"wrong direction": {
			schema:    rpcInputSchema("value", "xs:string") + rpcOutputSchema("result", "xs:string"),
			signature: `tns:value #out tns:result #return`,
		},
		"omitted output": {
			schema:    rpcInputSchema("value", "xs:string") + rpcOutputSchema("result", "xs:string"),
			signature: `tns:value #in`,
		},
		"shared unnamed or different types": {
			schema:    rpcInputSchema("value", "xs:string") + rpcOutputSchema("value", "xs:int"),
			signature: `tns:value #inout`,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler, err := wsdlcompile.New(wsdlcompile.Options{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
				URI:     "https://example.test/service.wsdl",
				Content: []byte(rpcDescription20(testCase.schema, testCase.signature)),
			})
			if !errors.Is(err, wsdlcompile.ErrInvalidRPCStyle) {
				t.Fatalf("Compile() error = %v, want ErrInvalidRPCStyle", err)
			}
		})
	}
}

func TestCompilerValidatesWSDL20IRIAndMultipartStyleSchemas(t *testing.T) {
	t.Parallel()

	valid := map[string]struct {
		style  string
		schema string
	}{
		"IRI": {
			style: wsdl.StyleIRI,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="xs:string"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
		},
		"multipart": {
			style: wsdl.StyleMultipart,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="metadata" type="xs:string"/>` +
				`<xs:element name="content" type="xs:base64Binary"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
		},
		"multipart complex child": {
			style: wsdl.StyleMultipart,
			schema: `<xs:complexType name="Metadata"><xs:sequence>` +
				`<xs:element name="name" type="xs:string"/>` +
				`</xs:sequence></xs:complexType><xs:element name="Call">` +
				`<xs:complexType><xs:sequence><xs:element name="metadata"` +
				` type="tns:Metadata"/></xs:sequence></xs:complexType></xs:element>`,
		},
	}
	for name, testCase := range valid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler, err := wsdlcompile.New(wsdlcompile.Options{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if _, err := compiler.Compile(context.Background(), wsdlcompile.Source{
				URI:     "https://example.test/" + name + ".wsdl",
				Content: []byte(operationStyleDescription20(testCase.style, testCase.schema)),
			}); err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
		})
	}

	invalid := map[string]struct {
		style  string
		schema string
		want   error
	}{
		"IRI choice": {
			style: wsdl.StyleIRI,
			schema: `<xs:element name="Call"><xs:complexType><xs:choice>` +
				`<xs:element name="value" type="xs:string"/>` +
				`</xs:choice></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidIRIStyle,
		},
		"IRI wrapper attribute": {
			style: wsdl.StyleIRI,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="xs:string"/></xs:sequence>` +
				`<xs:attribute name="id" type="xs:string"/>` +
				`</xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidIRIStyle,
		},
		"IRI referenced child": {
			style: wsdl.StyleIRI,
			schema: `<xs:element name="Value" type="xs:string"/>` +
				`<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element ref="tns:Value"/></xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidIRIStyle,
		},
		"IRI forbidden QName": {
			style: wsdl.StyleIRI,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="xs:QName"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidIRIStyle,
		},
		"IRI complex child": {
			style: wsdl.StyleIRI,
			schema: `<xs:complexType name="Value"><xs:sequence/></xs:complexType>` +
				`<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="tns:Value"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidIRIStyle,
		},
		"IRI derived forbidden type": {
			style: wsdl.StyleIRI,
			schema: `<xs:simpleType name="Binary"><xs:restriction base="xs:base64Binary"/>` +
				`</xs:simpleType><xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="tns:Binary"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidIRIStyle,
		},
		"multipart repeated child": {
			style: wsdl.StyleMultipart,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="xs:string"/>` +
				`<xs:element name="value" type="xs:string"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidMultipartStyle,
		},
		"multipart optional child": {
			style: wsdl.StyleMultipart,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="xs:string" minOccurs="0"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidMultipartStyle,
		},
		"multipart repeated occurrence": {
			style: wsdl.StyleMultipart,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="xs:string" maxOccurs="2"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidMultipartStyle,
		},
		"multipart wildcard": {
			style: wsdl.StyleMultipart,
			schema: `<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:any/></xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidMultipartStyle,
		},
		"multipart child attribute": {
			style: wsdl.StyleMultipart,
			schema: `<xs:complexType name="Value"><xs:sequence/>` +
				`<xs:attribute name="id" type="xs:string"/></xs:complexType>` +
				`<xs:element name="Call"><xs:complexType><xs:sequence>` +
				`<xs:element name="value" type="tns:Value"/>` +
				`</xs:sequence></xs:complexType></xs:element>`,
			want: wsdlcompile.ErrInvalidMultipartStyle,
		},
	}
	for name, testCase := range invalid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiler, err := wsdlcompile.New(wsdlcompile.Options{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
				URI:     "https://example.test/invalid.wsdl",
				Content: []byte(operationStyleDescription20(testCase.style, testCase.schema)),
			})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Compile() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func operationStyleDescription20(style string, schema string) string {
	return `<description xmlns="http://www.w3.org/ns/wsdl" xmlns:tns="urn:test"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
		`<types><xs:schema targetNamespace="urn:test" elementFormDefault="qualified">` +
		schema + `</xs:schema></types><interface name="API"><operation name="Call"` +
		` pattern="http://www.w3.org/ns/wsdl/in-only" style="` + style + `">` +
		`<input element="tns:Call"/></operation></interface></description>`
}

func rpcDescription20(schema string, signature string) string {
	return `<description xmlns="http://www.w3.org/ns/wsdl" xmlns:tns="urn:test"` +
		` xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc"` +
		` xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:test">` +
		`<types><xs:schema targetNamespace="urn:test" elementFormDefault="qualified">` +
		schema + `</xs:schema></types><interface name="API"><operation name="Call"` +
		` pattern="http://www.w3.org/ns/wsdl/in-out"` +
		` style="http://www.w3.org/ns/wsdl/style/rpc" wrpc:signature="` + signature + `">` +
		`<input element="tns:Call"/><output element="tns:CallResponse"/>` +
		`</operation></interface></description>`
}

func rpcInputSchema(name string, typeName string) string {
	return rpcInputWith(`<xs:sequence><xs:element name="` + name + `" type="` + typeName +
		`"/></xs:sequence>`)
}

func rpcOutputSchema(name string, typeName string) string {
	return rpcOutputWith(`<xs:sequence><xs:element name="` + name + `" type="` + typeName +
		`"/></xs:sequence>`)
}

func rpcInputWith(content string) string {
	return `<xs:element name="Call"><xs:complexType>` + content + `</xs:complexType></xs:element>`
}

func rpcOutputWith(content string) string {
	return `<xs:element name="CallResponse"><xs:complexType>` + content + `</xs:complexType></xs:element>`
}

func TestCompilerRunsSemanticValidationWithOwnedOptions(t *testing.T) {
	t.Parallel()

	understood := []wsdl.QName{{Namespace: "urn:extension", Local: "policy"}}
	compiler, err := wsdlcompile.New(wsdlcompile.Options{
		Validation: wsdl.ValidationOptions{UnderstoodExtensions: understood},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	understood[0].Local = "changed"
	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/service.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:wsdl="http://www.w3.org/ns/wsdl" xmlns:ext="urn:extension"` +
			` targetNamespace="urn:test"><ext:policy wsdl:required="true"/>` +
			`</description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/invalid.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` targetNamespace="urn:test"><interface name="API"><operation name="Call"` +
			` pattern="http://www.w3.org/ns/wsdl/in-out"><input element="#none"/>` +
			`</operation></interface></description>`),
	})
	if err == nil || !strings.Contains(err.Error(), "WSDL20_MEP_OUTPUT") {
		t.Fatalf("Compile(invalid) error = %v", err)
	}
}

func TestCompilerValidatesWSDL20BindingHeaderSchemas(t *testing.T) {
	t.Parallel()

	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	source := func(faultHeaderType, inputHeaderType string) []byte {
		return []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` xmlns:wsoap="http://www.w3.org/ns/wsdl/soap"` +
			` xmlns:whttp="http://www.w3.org/ns/wsdl/http" targetNamespace="urn:test">` +
			`<types><xs:schema targetNamespace="urn:test"><xs:element name="Header"` +
			` type="xs:string"/><xs:simpleType name="HeaderType"><xs:restriction` +
			` base="xs:string"/></xs:simpleType></xs:schema></types>` +
			`<interface name="API"><fault name="Failure" element="#none"/>` +
			`<operation name="Call" pattern="http://www.w3.org/ns/wsdl/robust-in-only">` +
			`<input element="#none"/><outfault ref="tns:Failure"/></operation></interface>` +
			`<binding name="Binding" interface="tns:API"` +
			` type="http://www.w3.org/ns/wsdl/soap" wsoap:protocol="urn:protocol">` +
			`<fault ref="tns:Failure"><wsoap:header element="tns:Header"/>` +
			`<whttp:header name="X-Fault" type="tns:` + faultHeaderType + `"/></fault>` +
			`<operation ref="tns:Call"><input><wsoap:header element="tns:Header"/>` +
			`<whttp:header name="X-Input" type="tns:` + inputHeaderType + `"/>` +
			`</input></operation></binding></description>`)
	}
	if _, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/valid.wsdl", Content: source("HeaderType", "HeaderType"),
	}); err != nil {
		t.Fatalf("Compile(valid) error = %v", err)
	}
	invalid := map[string][]byte{
		"fault": source("Missing", "HeaderType"),
		"input": source("HeaderType", "Missing"),
	}
	for name, content := range invalid {
		_, err = compiler.Compile(context.Background(), wsdlcompile.Source{
			URI: "https://example.test/invalid-" + name + ".wsdl", Content: content,
		})
		if !errors.Is(err, wsdlcompile.ErrUnresolvedComponent) {
			t.Errorf("Compile(invalid %s) error = %v, want ErrUnresolvedComponent", name, err)
		}
	}
}

type resolverFunc func(context.Context, resolve.Request) (resolve.Resource, error)

func (f resolverFunc) Resolve(
	ctx context.Context,
	request resolve.Request,
) (resolve.Resource, error) {
	return f(ctx, request)
}
