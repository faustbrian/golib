package wsdl_test

import (
	"context"
	"strings"
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestWSDL20RPCSignatureRoundTrips(t *testing.T) {
	t.Parallel()

	const source = `<description xmlns="http://www.w3.org/ns/wsdl"` +
		` xmlns:tns="urn:test" xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc"` +
		` targetNamespace="urn:test"><interface name="API"><operation name="Call"` +
		` pattern="http://www.w3.org/ns/wsdl/in-out"` +
		` style="http://www.w3.org/ns/wsdl/style/rpc"` +
		` wrpc:signature="tns:value #in tns:result #return">` +
		`<input element="tns:Call"/><output element="tns:CallResponse"/>` +
		`</operation></interface></description>`
	document, err := wsdl.Parse(context.Background(), []byte(source), wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	assertSignature := func(document *wsdl.Document) {
		description, _ := document.Description20()
		operation := description.Interfaces[0].Operations[0]
		if !operation.RPCSignatureSet || len(operation.RPCSignature) != 2 ||
			operation.RPCSignature[0].Name.Local != "value" ||
			operation.RPCSignature[0].Direction != wsdl.RPCDirectionIn ||
			operation.RPCSignature[1].Direction != wsdl.RPCDirectionReturn ||
			len(operation.ExtensionAttributes) != 0 {
			t.Fatalf("operation = %#v", operation)
		}
	}
	assertSignature(document)
	payload, err := wsdl.Marshal(document, wsdl.MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := wsdl.Parse(context.Background(), payload, wsdl.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	assertSignature(roundTrip)
}

func TestWSDL20RPCSignatureRejectsInvalidLexicalForms(t *testing.T) {
	t.Parallel()

	for _, signature := range []string{
		"tns:value",
		"tns:value #sideways",
		"missing:value #in",
	} {
		source := `<description xmlns="http://www.w3.org/ns/wsdl"` +
			` xmlns:tns="urn:test" xmlns:wrpc="http://www.w3.org/ns/wsdl/rpc"` +
			` targetNamespace="urn:test"><interface name="API"><operation name="Call"` +
			` pattern="urn:none" wrpc:signature="` + signature + `"/>` +
			`</interface></description>`
		if _, err := wsdl.Parse(
			context.Background(), []byte(source), wsdl.ParseOptions{},
		); err == nil {
			t.Errorf("Parse(signature %q) error = nil", signature)
		}
	}
}

func TestValidateWSDL20RPCStyleRules(t *testing.T) {
	t.Parallel()

	description := wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces: []wsdl.Interface20{{
			Name: "API",
			Operations: []wsdl.InterfaceOperation20{{
				Name: "Call", Pattern: wsdl.MEPOutOnly, Style: []string{wsdl.StyleRPC},
				Inputs: []wsdl.InterfaceMessageReference20{{
					Element:             wsdl.QName{Namespace: "urn:test", Local: "Wrong"},
					MessageContentModel: wsdl.MessageContentElement, MessageContentModelSet: true,
				}},
				Outputs: []wsdl.InterfaceMessageReference20{{
					Element:             wsdl.QName{Namespace: "urn:other", Local: "Response"},
					MessageContentModel: wsdl.MessageContentElement, MessageContentModelSet: true,
				}},
				RPCSignatureSet: true,
				RPCSignature: []wsdl.RPCSignatureParameter20{
					{Name: wsdl.QName{Namespace: "urn:test", Local: "value"}, Direction: wsdl.RPCDirectionIn},
					{Name: wsdl.QName{Namespace: "urn:test", Local: "value"}, Direction: "#bad"},
				},
			}},
		}},
	}
	_, err := wsdl.NewDocument20(description, wsdl.ValidationOptions{})
	if err == nil {
		t.Fatal("NewDocument20() error = nil")
	}
	for _, code := range []string{
		"WSDL20_RPC_STYLE_PATTERN", "WSDL20_RPC_INPUT_NAME",
		"WSDL20_RPC_MESSAGE_NAMESPACE", "WSDL20_RPC_SIGNATURE_DIRECTION",
		"WSDL20_RPC_SIGNATURE_DUPLICATE",
	} {
		if !strings.Contains(err.Error(), code) {
			t.Errorf("validation error %q missing %s", err, code)
		}
	}
}

func TestValidateWSDL20RPCRequiresSignatureAndElementContent(t *testing.T) {
	t.Parallel()

	description := wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces: []wsdl.Interface20{{
			Name: "API",
			Operations: []wsdl.InterfaceOperation20{{
				Name: "Call", Pattern: wsdl.MEPInOnly, Style: []string{wsdl.StyleRPC},
				Inputs: []wsdl.InterfaceMessageReference20{{
					MessageContentModel: wsdl.MessageContentAny, MessageContentModelSet: true,
				}},
			}},
		}},
	}
	_, err := wsdl.NewDocument20(description, wsdl.ValidationOptions{})
	if err == nil || !strings.Contains(err.Error(), "WSDL20_RPC_SIGNATURE_REQUIRED") ||
		!strings.Contains(err.Error(), "WSDL20_RPC_MESSAGE_CONTENT") {
		t.Fatalf("NewDocument20() error = %v", err)
	}
}

func TestValidateWSDL20RPCRejectsEmptySignatureName(t *testing.T) {
	t.Parallel()

	_, err := wsdl.NewDocument20(wsdl.Description20{
		TargetNamespace: "urn:test",
		Interfaces: []wsdl.Interface20{{
			Name: "API",
			Operations: []wsdl.InterfaceOperation20{{
				Name: "Call", Pattern: wsdl.MEPInOnly, Style: []string{wsdl.StyleRPC},
				Inputs: []wsdl.InterfaceMessageReference20{{
					Element:             wsdl.QName{Namespace: "urn:test", Local: "Call"},
					MessageContentModel: wsdl.MessageContentElement, MessageContentModelSet: true,
				}},
				RPCSignatureSet: true,
				RPCSignature: []wsdl.RPCSignatureParameter20{{
					Direction: wsdl.RPCDirectionIn,
				}},
			}},
		}},
	}, wsdl.ValidationOptions{})
	if err == nil || !strings.Contains(err.Error(), "WSDL20_RPC_SIGNATURE_NAME") {
		t.Fatalf("NewDocument20() error = %v", err)
	}
}
