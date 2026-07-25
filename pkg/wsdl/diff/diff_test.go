package diff_test

import (
	"context"
	"testing"

	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
	wsdldiff "github.com/faustbrian/golib/pkg/wsdl/diff"
)

func TestCompareClassifiesInterfaceOperationChanges(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<interface name="API"><operation name="Existing" pattern="urn:in-only"/>`+
			`<operation name="Removed" pattern="urn:in-only"/></interface>`,
	)
	after := compileDescription(t,
		`<interface name="API"><operation name="Added" pattern="urn:in-only"/>`+
			`<operation name="Existing" pattern="urn:in-only"/></interface>`,
	)
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 2 {
		t.Fatalf("Changes = %#v", report.Changes)
	}
	if report.Changes[0].Kind != wsdldiff.ChangeAdded ||
		report.Changes[0].Compatibility != wsdldiff.CompatibilityNonBreaking ||
		report.Changes[1].Kind != wsdldiff.ChangeRemoved ||
		report.Changes[1].Compatibility != wsdldiff.CompatibilityBreaking {
		t.Fatalf("Changes = %#v", report.Changes)
	}
	if len(report.Caveats) == 0 {
		t.Fatal("Caveats = nil")
	}
}

func TestCompareClassifiesBindingContractChanges(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<interface name="API"/><binding name="Binding" interface="tns:API"`+
			` type="urn:before"/>`,
	)
	after := compileDescription(t,
		`<interface name="API"/><binding name="Binding" interface="tns:API"`+
			` type="urn:after"/>`,
	)
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 1 || report.Changes[0].Kind != wsdldiff.ChangeModified ||
		report.Changes[0].Compatibility != wsdldiff.CompatibilityBreaking {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func TestCompareHandlesNilSets(t *testing.T) {
	t.Parallel()

	report := wsdldiff.Compare(nil, nil)
	if len(report.Changes) != 0 || len(report.Caveats) == 0 {
		t.Fatalf("Compare(nil, nil) = %#v", report)
	}
}

func TestCompareClassifiesWholeSetAdditionAndRemoval(t *testing.T) {
	t.Parallel()

	set := compileDescription(t,
		`<types><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"`+
			` targetNamespace="urn:test"><xs:element name="Value" type="xs:string"/>`+
			`<xs:simpleType name="Code"><xs:restriction base="xs:string"/>`+
			`</xs:simpleType><xs:complexType name="Record"><xs:sequence/>`+
			`</xs:complexType></xs:schema></types><interface name="API">`+
			`<operation name="Call" pattern="urn:in-only"><input element="tns:Value"/>`+
			`</operation></interface><binding name="Binding" interface="tns:API"`+
			` type="urn:binding"><operation ref="tns:Call"/></binding>`+
			`<service name="Service" interface="tns:API"><endpoint name="Endpoint"`+
			` binding="tns:Binding" address="https://example.test"/></service>`,
	)
	added := wsdldiff.Compare(nil, set)
	removed := wsdldiff.Compare(set, nil)
	if len(added.Changes) == 0 || len(removed.Changes) == 0 {
		t.Fatalf("whole-set changes = added %#v, removed %#v", added, removed)
	}
	for _, change := range added.Changes {
		if change.Kind != wsdldiff.ChangeAdded {
			t.Fatalf("added change = %#v", change)
		}
	}
	for _, change := range removed.Changes {
		if change.Kind != wsdldiff.ChangeRemoved {
			t.Fatalf("removed change = %#v", change)
		}
	}
}

func TestCompareDetectsWSDL20MessageAndFaultChanges(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<interface name="API"><fault name="Changed" element="#none"/>`+
			`<fault name="Removed" element="#none"/><operation name="Exchange"`+
			` pattern="urn:multi"><input messageLabel="First" element="#none"/>`+
			`<input messageLabel="Second" element="#none"/>`+
			`<outfault ref="tns:Changed" messageLabel="Changed"/>`+
			`<outfault ref="tns:Removed" messageLabel="Removed"/>`+
			`</operation></interface>`,
	)
	after := compileDescription(t,
		`<interface name="API"><fault name="Changed" element="#any"/>`+
			`<fault name="Added" element="#none"/><operation name="Exchange"`+
			` pattern="urn:multi"><input messageLabel="First" element="#none"/>`+
			`<input messageLabel="Second" element="#any"/>`+
			`<outfault ref="tns:Changed" messageLabel="Changed"/>`+
			`<outfault ref="tns:Added" messageLabel="Added"/>`+
			`</operation></interface>`,
	)
	report := wsdldiff.Compare(before, after)
	modified, added, removed := false, false, false
	for _, change := range report.Changes {
		switch change.Kind {
		case wsdldiff.ChangeModified:
			modified = true
		case wsdldiff.ChangeAdded:
			added = true
		case wsdldiff.ChangeRemoved:
			removed = true
		}
	}
	if !modified || !added || !removed {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func TestCompareDetectsWSDL20OperationSafetyChange(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<interface name="API"><operation xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"`+
			` name="Call" pattern="urn:none" wsdlx:safe="false"/></interface>`,
	)
	after := compileDescription(t,
		`<interface name="API"><operation xmlns:wsdlx="http://www.w3.org/ns/wsdl-extensions"`+
			` name="Call" pattern="urn:none" wsdlx:safe="true"/></interface>`,
	)
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 1 ||
		report.Changes[0].Path != "/interfaces/{urn:test}API/operations/Call/safe" ||
		report.Changes[0].Compatibility != wsdldiff.CompatibilityBreaking {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func TestCompareDetectsWSDL20RPCSignatureChange(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<types><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"`+
			` targetNamespace="urn:test" elementFormDefault="qualified">`+
			`<xs:element name="Call"><xs:complexType><xs:sequence><xs:element`+
			` name="value" type="xs:string"/></xs:sequence></xs:complexType></xs:element>`+
			`</xs:schema></types><interface name="API"><operation`+
			` xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc"`+
			` name="Call" pattern="http://www.w3.org/ns/wsdl/in-only"`+
			` style="http://www.w3.org/ns/wsdl/style/rpc"`+
			` wrpc:signature="tns:value #in"><input element="tns:Call"/>`+
			`</operation></interface>`,
	)
	after := compileDescription(t,
		`<types><xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"`+
			` targetNamespace="urn:test" elementFormDefault="qualified">`+
			`<xs:element name="Call"><xs:complexType><xs:sequence><xs:element`+
			` name="other" type="xs:string"/></xs:sequence></xs:complexType></xs:element>`+
			`</xs:schema></types><interface name="API"><operation`+
			` xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc"`+
			` name="Call" pattern="http://www.w3.org/ns/wsdl/in-only"`+
			` style="http://www.w3.org/ns/wsdl/style/rpc"`+
			` wrpc:signature="tns:other #in"><input element="tns:Call"/>`+
			`</operation></interface>`,
	)
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 1 ||
		report.Changes[0].Path != "/interfaces/{urn:test}API/operations/Call/rpc-signature" {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func TestCompareDetectsOperationPayloadChanges(t *testing.T) {
	t.Parallel()

	before := compileDefinitions(t, "string")
	after := compileDefinitions(t, "int")
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 1 ||
		report.Changes[0].Path != "/interfaces/{urn:test}API/operations/Call/input/parts/value/type" ||
		report.Changes[0].Compatibility != wsdldiff.CompatibilityBreaking {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func TestCompareKeepsOverloadedWSDL11OperationsDistinct(t *testing.T) {
	t.Parallel()

	before := compileOverloadedDefinitions(t, "string")
	after := compileOverloadedDefinitions(t, "int")
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 1 ||
		report.Changes[0].Path !=
			"/interfaces/{urn:test}API/operations/Call[Second|]/input/parts/value/type" {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func TestCompareDetectsRemovedWSDL20MessageReference(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<interface name="API"><operation name="Exchange" pattern="urn:multi">`+
			`<input messageLabel="First" element="#none"/>`+
			`<input messageLabel="Second" element="#any"/>`+
			`</operation></interface>`,
	)
	after := compileDescription(t,
		`<interface name="API"><operation name="Exchange" pattern="urn:multi">`+
			`<input messageLabel="First" element="#none"/>`+
			`</operation></interface>`,
	)
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 1 ||
		report.Changes[0].Path != "/interfaces/{urn:test}API/operations/Exchange/inputs/Second" ||
		report.Changes[0].Kind != wsdldiff.ChangeRemoved ||
		report.Changes[0].Compatibility != wsdldiff.CompatibilityBreaking {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func TestCompareDetectsServiceEndpointChanges(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<interface name="API"/><binding name="Binding" interface="tns:API" type="urn:binding"/>`+
			`<service name="Service" interface="tns:API">`+
			`<endpoint name="Changed" binding="tns:Binding" address="https://old.test"/>`+
			`<endpoint name="Removed" binding="tns:Binding"/></service>`,
	)
	after := compileDescription(t,
		`<interface name="API"/><binding name="Binding" interface="tns:API" type="urn:binding"/>`+
			`<service name="Service" interface="tns:API">`+
			`<endpoint name="Added" binding="tns:Binding"/>`+
			`<endpoint name="Changed" binding="tns:Binding" address="https://new.test"/>`+
			`</service>`,
	)
	report := wsdldiff.Compare(before, after)
	if len(report.Changes) != 3 {
		t.Fatalf("Changes = %#v", report.Changes)
	}
	for _, change := range report.Changes {
		if change.Compatibility == "" {
			t.Fatalf("unclassified change = %#v", change)
		}
	}
}

func TestCompareDetectsServiceInterfaceChange(t *testing.T) {
	t.Parallel()

	before := compileDescription(t,
		`<interface name="Before"/><service name="Service" interface="tns:Before"/>`,
	)
	after := compileDescription(t,
		`<interface name="After"/><service name="Service" interface="tns:After"/>`,
	)
	report := wsdldiff.Compare(before, after)
	found := false
	for _, change := range report.Changes {
		if change.Path == "/services/{urn:test}Service/interface" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Changes = %#v", report.Changes)
	}
}

func compileDescription(t *testing.T, body string) *wsdlcompile.Set {
	t.Helper()
	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" targetNamespace="urn:test">` + body +
			`</description>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return set
}

func compileDefinitions(t *testing.T, partType string) *wsdlcompile.Set {
	t.Helper()
	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test"><message name="Request"><part name="value"` +
			` type="xs:` + partType + `"/></message><portType name="API">` +
			`<operation name="Call"><input message="tns:Request"/>` +
			`</operation></portType></definitions>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return set
}

func compileOverloadedDefinitions(t *testing.T, secondType string) *wsdlcompile.Set {
	t.Helper()
	compiler, err := wsdlcompile.New(wsdlcompile.Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	set, err := compiler.Compile(context.Background(), wsdlcompile.Source{
		URI: "https://example.test/root.wsdl",
		Content: []byte(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
			` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
			` targetNamespace="urn:test"><message name="First"><part name="value"` +
			` type="xs:string"/></message><message name="Second"><part name="value"` +
			` type="xs:` + secondType + `"/></message><portType name="API">` +
			`<operation name="Call"><input name="First" message="tns:First"/></operation>` +
			`<operation name="Call"><input name="Second" message="tns:Second"/></operation>` +
			`</portType></definitions>`),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return set
}
