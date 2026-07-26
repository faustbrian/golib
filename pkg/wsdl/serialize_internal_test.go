package wsdl

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

var errInjectedWrite = errors.New("injected XML write failure")

type recordingWriter struct {
	writes []int
	bytes  int
}

func (w *recordingWriter) Write(payload []byte) (int, error) {
	w.writes = append(w.writes, len(payload))
	w.bytes += len(payload)
	return len(payload), nil
}

type boundedFailureWriter struct {
	remaining int
}

func (w *boundedFailureWriter) Write(payload []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errInjectedWrite
	}
	if len(payload) > w.remaining {
		written := w.remaining
		w.remaining = 0
		return written, errInjectedWrite
	}
	w.remaining -= len(payload)
	return len(payload), nil
}

type countingTokenEncoder struct {
	tokens int
}

func (e *countingTokenEncoder) EncodeToken(xml.Token) error {
	e.tokens++
	return nil
}

type boundedTokenEncoder struct {
	remaining int
}

func (e *boundedTokenEncoder) EncodeToken(xml.Token) error {
	if e.remaining == 0 {
		return errInjectedWrite
	}
	e.remaining--
	return nil
}

type failOnceTokenEncoder struct {
	failAt int
	index  int
}

func (e *failOnceTokenEncoder) EncodeToken(xml.Token) error {
	index := e.index
	e.index++
	if index == e.failAt {
		return errInjectedWrite
	}
	return nil
}

type recordingTokenEncoder struct {
	tokens []xml.Token
}

func (e *recordingTokenEncoder) EncodeToken(token xml.Token) error {
	e.tokens = append(e.tokens, token)
	return nil
}

type failNamedEndEncoder struct {
	local  string
	failed bool
}

func (e *failNamedEndEncoder) EncodeToken(token xml.Token) error {
	end, ok := token.(xml.EndElement)
	if ok && !e.failed && end.Name.Local == e.local {
		e.failed = true
		return errInjectedWrite
	}
	return nil
}

func TestWSDL20SerializerPropagatesEveryPhysicalWriteFailure(t *testing.T) {
	t.Parallel()

	assertPhysicalWriteFailures(t, exhaustiveSerializationDocument20())
}

func TestWSDL11SerializerPropagatesEveryPhysicalWriteFailure(t *testing.T) {
	t.Parallel()

	assertPhysicalWriteFailures(t, exhaustiveSerializationDocument11(t))
}

func TestSerializersPropagateEveryTokenWriteFailure(t *testing.T) {
	t.Parallel()

	t.Run("WSDL 1.1", func(t *testing.T) {
		t.Parallel()
		assertTokenWriteFailures(t, exhaustiveSerializationDocument11(t))
	})
	t.Run("WSDL 2.0", func(t *testing.T) {
		t.Parallel()
		assertTokenWriteFailures(t, exhaustiveSerializationDocument20())
	})
}

func assertTokenWriteFailures(t *testing.T, document *Document) {
	t.Helper()

	value, err := newMarshalValue(document)
	if err != nil {
		t.Fatalf("newMarshalValue() error = %v", err)
	}
	encode := func(encoder tokenEncoder) error {
		if definitions, ok := document.Definitions11(); ok {
			return value.definitions11(encoder, definitions)
		}
		description, _ := document.Description20()
		return value.description20(encoder, description)
	}
	counter := &countingTokenEncoder{}
	if err := encode(counter); err != nil {
		t.Fatalf("encode() error = %v", err)
	}
	if counter.tokens < 50 {
		t.Fatalf("token count = %d", counter.tokens)
	}
	for index := 0; index < counter.tokens; index++ {
		if err := encode(&boundedTokenEncoder{remaining: index}); !errors.Is(err, errInjectedWrite) {
			t.Fatalf("token %d error = %v", index, err)
		}
		if err := encode(&failOnceTokenEncoder{failAt: index}); !errors.Is(err, errInjectedWrite) {
			t.Fatalf("one-time token %d error = %v", index, err)
		}
	}
}

func TestExhaustiveWSDL20ModelRoundTripsDeterministically(t *testing.T) {
	t.Parallel()

	first, err := Marshal(exhaustiveSerializationDocument20(), MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	document, err := Parse(context.Background(), first, ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	second, err := Marshal(document, MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal(round trip) error = %v", err)
	}
	reparsed, err := Parse(context.Background(), second, ParseOptions{})
	if err != nil {
		t.Fatalf("Parse(round trip) error = %v", err)
	}
	third, err := Marshal(reparsed, MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal(second round trip) error = %v", err)
	}
	if !bytes.Equal(second, third) {
		t.Fatal("WSDL 2.0 serialization is not deterministic after round trip")
	}
	assertSerializedFragments(t, first,
		`extends="tns:Parent"`, `styleDefault="urn:style"`,
		`style="urn:style"`, `wsdlx:safe="true"`,
		`wrpc:signature="tns:value #in"`, `interfaceInFault`,
		`interfaceOutFault`, `wsoap:version="1.2"`,
		`wsoap:protocol="urn:protocol"`, `wsoap:mepDefault="urn:mep"`,
		`whttp:methodDefault="POST"`, `whttp:version="1.1"`,
		`whttp:queryParameterSeparatorDefault=";"`,
		`whttp:contentEncodingDefault="gzip"`,
		`whttp:defaultTransferCoding="chunked"`, `whttp:cookies="true"`,
		`wsoap:code="tns:Code"`, `wsoap:subcodes="tns:Subcode"`,
		`whttp:code="500"`, `whttp:location="items/{id}"`,
		`location="other.wsdl"`,
		`whttp:method="PUT"`, `whttp:ignoreUncited="true"`,
		`ref="urn:faultModule"`, `ref="urn:inputModule"`,
		`ref="urn:inFaultModule"`, `ref="urn:outFaultModule"`,
		`name="X-Fault"`, `name="X-Input"`,
		`whttp:authenticationScheme="basic"`,
		`whttp:authenticationRealm="api"`,
		`xml:lang="en"`,
	)
}

func TestWSDL11SerializationPreservesEveryProtocolBranch(t *testing.T) {
	t.Parallel()

	document, err := Parse(context.Background(), []byte(serializationSource11), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	output, err := Marshal(document, MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	assertSerializedFragments(t, output,
		`namespace="urn:other" location="other.wsdl"`,
		`parameterOrder="body value"`,
		`<soap12:binding transport="urn:soap" style="rpc"`,
		`soapAction="urn:call"`, `soapActionRequired="true"`,
		`style="document"`, `<http:binding verb="POST"`,
		`<http:operation location="/call/(body)"`,
		`<http:urlReplacement>`, `use="encoded"`, `namespace="urn:body"`,
		`encodingStyle="urn:one urn:two"`, `parts="body"`,
		`namespace="urn:header"`, `namespace="urn:header-fault"`,
		`<mime:multipartRelated>`, `<mime:content part="body" type="application/xml"`,
		`<mime:mimeXml part="body"`,
		`<soap12:address location="https://example.test/soap"`,
		`<http:address location="https://example.test/http"`,
	)
}

func TestWSDL11SerializationPreservesUnflaggedValues(t *testing.T) {
	t.Parallel()

	tns := func(local string) QName { return QName{Namespace: "urn:test", Local: local} }
	document := &Document{version: Version11, definitions11: &Definitions11{
		TargetNamespace: "urn:test",
		Bindings: []Binding11{{
			Name: "Binding", Type: tns("API"),
			SOAP: &SOAPBinding11{Version: Version12, Transport: "urn:transport", Style: SOAPStyleRPC},
			Operations: []BindingOperation11{{
				Name: "Call",
				SOAP: &SOAPOperation11{Version: Version12, Action: "urn:action", Style: SOAPStyleDocument},
				Input: &BindingMessage11{SOAPBody: &SOAPBody11{
					Version: Version12, Use: SOAPUseEncoded, Namespace: "urn:body",
					EncodingStyle: []string{"urn:encoding"}, Parts: []string{"body"},
				}},
				Faults: []BindingMessage11{{Name: "Failure", SOAPFault: &SOAPFault11{
					Version: Version12, Name: "Failure", Use: SOAPUseLiteral,
					Namespace: "urn:fault", EncodingStyle: []string{"urn:fault-encoding"},
				}}},
			}},
		}},
	}}
	output, err := Marshal(document, MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	assertSerializedFragments(t, output,
		`transport="urn:transport"`, `style="rpc"`, `soapAction="urn:action"`,
		`style="document"`, `use="encoded"`, `namespace="urn:body"`,
		`encodingStyle="urn:encoding"`, `parts="body"`,
		`use="literal"`, `namespace="urn:fault"`,
		`encodingStyle="urn:fault-encoding"`,
	)
}

func TestWSDL11SolicitResponseSerializesOutputBeforeInput(t *testing.T) {
	t.Parallel()

	document := &Document{version: Version11, definitions11: &Definitions11{
		TargetNamespace: "urn:test",
		PortTypes: []PortType11{{Name: "API", Operations: []Operation11{{
			Name: "Notify", Style: OperationStyleSolicitResponse,
			Input:  &OperationMessage11{Name: "In"},
			Output: &OperationMessage11{Name: "Out"},
			Faults: []OperationMessage11{{Name: "Failure"}},
		}}}},
	}}
	output, err := Marshal(document, MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	outputIndex := bytes.Index(output, []byte(`name="Out"`))
	inputIndex := bytes.Index(output, []byte(`name="In"`))
	if outputIndex < 0 || inputIndex < 0 || outputIndex >= inputIndex {
		t.Fatalf("solicit-response order is wrong: %s", output)
	}
	if !bytes.Contains(output, []byte(`name="Failure"`)) {
		t.Fatalf("solicit-response fault is missing: %s", output)
	}
}

func TestSOAP11AttributeHelpersHandleEmptyAndUnflaggedValues(t *testing.T) {
	t.Parallel()

	emptyBody := &recordingTokenEncoder{}
	if err := encodeSOAPBody11(emptyBody, SOAPBody11{}); err != nil {
		t.Fatalf("encodeSOAPBody11(empty) error = %v", err)
	}
	if attributes := emptyBody.tokens[0].(xml.StartElement).Attr; len(attributes) != 0 {
		t.Fatalf("empty SOAP body attributes = %#v", attributes)
	}

	body := &recordingTokenEncoder{}
	if err := encodeSOAPBody11(body, SOAPBody11{
		Use: SOAPUseEncoded, Namespace: "urn:body",
		EncodingStyle: []string{"urn:encoding"}, Parts: []string{"body"},
	}); err != nil {
		t.Fatalf("encodeSOAPBody11(unflagged) error = %v", err)
	}
	if attributes := body.tokens[0].(xml.StartElement).Attr; len(attributes) != 4 {
		t.Fatalf("unflagged SOAP body attributes = %#v", attributes)
	}

	if attributes := soapMessageAttributes("body", SOAPUseEncoded, false, "urn:body", false,
		[]string{"urn:encoding"}, false); len(attributes) != 4 {
		t.Fatalf("unflagged SOAP message attributes = %#v", attributes)
	}
	if attributes := soapMessageAttributes("body", "", false, "", false, nil, false); len(attributes) != 1 {
		t.Fatalf("empty SOAP message attributes = %#v", attributes)
	}

	fault := &recordingTokenEncoder{}
	if err := encodeSOAPFault11(fault, SOAPFault11{
		Name: "Failure", Use: SOAPUseLiteral, Namespace: "urn:fault",
		EncodingStyle: []string{"urn:fault-encoding"},
	}); err != nil {
		t.Fatalf("encodeSOAPFault11(unflagged) error = %v", err)
	}
	if attributes := fault.tokens[0].(xml.StartElement).Attr; len(attributes) != 4 {
		t.Fatalf("unflagged SOAP fault attributes = %#v", attributes)
	}
	emptyFault := &recordingTokenEncoder{}
	if err := encodeSOAPFault11(emptyFault, SOAPFault11{Name: "Failure"}); err != nil {
		t.Fatalf("encodeSOAPFault11(empty) error = %v", err)
	}
	if attributes := emptyFault.tokens[0].(xml.StartElement).Attr; len(attributes) != 1 {
		t.Fatalf("empty SOAP fault attributes = %#v", attributes)
	}
}

func TestWSDL11SerializerPropagatesNamedContainerCloseFailures(t *testing.T) {
	t.Parallel()

	value := marshalValue{prefixes: map[string]string{"urn:test": "tns"}}
	tests := map[string]struct {
		local  string
		encode func(tokenEncoder) error
	}{
		"message part": {
			local: "part",
			encode: func(encoder tokenEncoder) error {
				return value.message11(encoder, Message11{
					Name: "Request", Parts: []Part11{{Name: "body"}},
				})
			},
		},
		"MIME part": {
			local: "mime:part",
			encode: func(encoder tokenEncoder) error {
				return encodeMIMEMessage11(encoder, MIMEMessage11{Multipart: []MIMEMultipart11{{
					Parts: []MIMEPart11{{}},
				}}})
			},
		},
		"MIME multipart": {
			local: "mime:multipartRelated",
			encode: func(encoder tokenEncoder) error {
				return encodeMIMEMessage11(encoder, MIMEMessage11{Multipart: []MIMEMultipart11{{}}})
			},
		},
		"service port": {
			local: "port",
			encode: func(encoder tokenEncoder) error {
				return value.service11(encoder, Service11{
					Name: "Service", Ports: []Port11{{
						Name: "Port", Binding: QName{Namespace: "urn:test", Local: "Binding"},
					}},
				})
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			encoder := &failNamedEndEncoder{local: test.local}
			if err := test.encode(encoder); !errors.Is(err, errInjectedWrite) {
				t.Fatalf("encode error = %v", err)
			}
			if !encoder.failed {
				t.Fatalf("end element %q was not encoded", test.local)
			}
		})
	}
}

func TestWSDL20SerializerPropagatesNamedContainerCloseFailures(t *testing.T) {
	t.Parallel()

	value := marshalValue{prefixes: map[string]string{
		NamespaceWSDL20SOAP: "wsoap",
		NamespaceWSDL20HTTP: "whttp",
	}}
	tests := map[string]struct {
		local  string
		encode func(tokenEncoder) error
	}{
		"import": {
			local: "import",
			encode: func(encoder tokenEncoder) error {
				return value.description20(encoder, Description20{
					Imports: []Import20{{Namespace: "urn:other"}},
				})
			},
		},
		"include": {
			local: "include",
			encode: func(encoder tokenEncoder) error {
				return value.description20(encoder, Description20{
					Includes: []Include20{{Location: "included.wsdl"}},
				})
			},
		},
		"interface fault": {
			local: "fault",
			encode: func(encoder tokenEncoder) error {
				return value.interface20(encoder, Interface20{
					Name: "API", Faults: []InterfaceFault20{{Name: "Failure"}},
				})
			},
		},
		"binding operation": {
			local: "operation",
			encode: func(encoder tokenEncoder) error {
				return value.binding20(encoder, Binding20{
					Name: "Binding", Operations: []BindingOperation20{{}},
				})
			},
		},
		"endpoint": {
			local: "endpoint",
			encode: func(encoder tokenEncoder) error {
				return value.service20(encoder, Service20{
					Name: "Service", Endpoints: []Endpoint20{{Name: "Endpoint"}},
				})
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			encoder := &failNamedEndEncoder{local: test.local}
			if err := test.encode(encoder); !errors.Is(err, errInjectedWrite) {
				t.Fatalf("encode error = %v", err)
			}
			if !encoder.failed {
				t.Fatalf("end element %q was not encoded", test.local)
			}
		})
	}
}

func TestSerializersOmitUnsetCollectionAttributes(t *testing.T) {
	t.Parallel()

	document20 := &Document{version: Version20, description20: &Description20{
		TargetNamespace: "urn:test",
		Interfaces: []Interface20{{Name: "API", Operations: []InterfaceOperation20{{
			Name: "Call", Pattern: MEPInOnly,
		}}}},
	}}
	output20, err := Marshal(document20, MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal(WSDL 2.0) error = %v", err)
	}
	for _, attribute := range []string{"extends=", "styleDefault=", " style=", "wrpc:signature="} {
		if bytes.Contains(output20, []byte(attribute)) {
			t.Errorf("WSDL 2.0 output contains unset attribute %q", attribute)
		}
	}

	document11 := &Document{version: Version11, definitions11: &Definitions11{
		TargetNamespace: "urn:test",
		PortTypes:       []PortType11{{Name: "API", Operations: []Operation11{{Name: "Call"}}}},
	}}
	output11, err := Marshal(document11, MarshalOptions{})
	if err != nil {
		t.Fatalf("Marshal(WSDL 1.1) error = %v", err)
	}
	if bytes.Contains(output11, []byte("parameterOrder=")) {
		t.Fatal("WSDL 1.1 output contains an unset parameterOrder")
	}
}

func TestNamespaceCollectionCoversNestedMessageAndProtocolQNames(t *testing.T) {
	t.Parallel()

	namespaces11 := map[string]struct{}{}
	collectDefinition11Namespaces(Definitions11{PortTypes: []PortType11{{
		Operations: []Operation11{{Input: &OperationMessage11{
			Message: QName{Namespace: "urn:input", Local: "Request"},
		}}},
	}}}, namespaces11)
	if _, exists := namespaces11["urn:input"]; !exists {
		t.Fatalf("WSDL 1.1 namespaces = %v", namespaces11)
	}

	namespaces20 := map[string]struct{}{}
	collectDescription20Namespaces(Description20{Bindings: []Binding20{{
		Operations: []BindingOperation20{{
			InFaults: []BindingFaultReference20{{SOAP: &SOAPFaultReferenceBinding20{
				Modules: []SOAPModule20{{Extensibility: Extensibility{ExtensionAttributes: []ExtensionAttribute{{
					Name: QName{Namespace: "urn:module", Local: "flag"},
				}}}}},
			}}},
			Inputs: []BindingMessageReference20{{
				SOAP: &SOAPMessageBinding20{Headers: []SOAPHeader20{{
					Element: QName{Namespace: "urn:header", Local: "Value"},
				}}},
				HTTP: &HTTPMessageBinding20{Headers: []HTTPHeader20{{
					Type: QName{Namespace: "urn:http-type", Local: "Value"},
				}}},
			}},
		}},
	}}}, namespaces20)
	for _, namespace := range []string{"urn:module", "urn:header", "urn:http-type"} {
		if _, exists := namespaces20[namespace]; !exists {
			t.Errorf("WSDL 2.0 namespaces missing %q: %v", namespace, namespaces20)
		}
	}
}

func TestRawExtensionXMLNormalizesNamespaceDeclarations(t *testing.T) {
	t.Parallel()

	encoder := &recordingTokenEncoder{}
	if err := encodeRawXML(encoder, []byte(`<ext:item xmlns="urn:default" xmlns:ext="urn:ext"/>`)); err != nil {
		t.Fatalf("encodeRawXML() error = %v", err)
	}
	start, ok := encoder.tokens[0].(xml.StartElement)
	if !ok {
		t.Fatalf("first token = %#v", encoder.tokens[0])
	}
	if len(start.Attr) != 1 || start.Attr[0].Name.Local != "xmlns:ext" || start.Attr[0].Value != "urn:ext" {
		t.Fatalf("normalized attributes = %#v", start.Attr)
	}
	if err := encodeRawXML(&recordingTokenEncoder{}, []byte(`<extension>`)); err == nil {
		t.Fatal("encodeRawXML(malformed) error = nil")
	}
}

func assertSerializedFragments(t *testing.T, output []byte, fragments ...string) {
	t.Helper()

	for _, fragment := range fragments {
		if !bytes.Contains(output, []byte(fragment)) {
			t.Errorf("serialized WSDL is missing %q", fragment)
		}
	}
}

func TestSerializerAvoidsPreferredPrefixCollisions(t *testing.T) {
	t.Parallel()

	document := &Document{
		version: Version11,
		definitions11: &Definitions11{
			Types: &Types11{Schemas: []*xsd.Document{{Namespaces: map[string]string{
				"soap": "urn:a",
				"xml":  "urn:b",
				"ns1":  "urn:c",
			}}}},
			Messages: []Message11{{Name: "Values", Parts: []Part11{
				{Name: "a", Type: QName{Namespace: "urn:a", Local: "Value"}},
				{Name: "b", Type: QName{Namespace: "urn:b", Local: "Value"}},
				{Name: "c", Type: QName{Namespace: "urn:c", Local: "Value"}},
			}}},
			Bindings: []Binding11{{SOAP: &SOAPBinding11{Version: Version11}}},
		},
	}
	value, err := newMarshalValue(document)
	if err != nil {
		t.Fatalf("newMarshalValue() error = %v", err)
	}
	if value.prefixes["urn:a"] != "ns2" || value.prefixes["urn:b"] != "ns3" ||
		value.prefixes["urn:c"] != "ns1" {
		t.Fatalf("prefixes = %#v", value.prefixes)
	}

	prefixes := map[string]string{"urn:target": "tns", "urn:used": "blocked"}
	preferTargetPrefix(prefixes, map[string][]string{
		"urn:target": {"", "xml", "blocked", "preferred"},
	}, "urn:target")
	if prefixes["urn:target"] != "preferred" {
		t.Fatalf("target prefix = %q", prefixes["urn:target"])
	}
}

func TestSerializerAssignsTargetAndSchemaPreferredPrefixesForBothVersions(t *testing.T) {
	t.Parallel()

	value11, err := newMarshalValue(&Document{
		version:       Version11,
		definitions11: &Definitions11{TargetNamespace: "urn:target"},
	})
	if err != nil {
		t.Fatalf("newMarshalValue(WSDL 1.1) error = %v", err)
	}
	if value11.prefixes["urn:target"] != "tns" {
		t.Fatalf("WSDL 1.1 target prefix = %q", value11.prefixes["urn:target"])
	}

	value20, err := newMarshalValue(&Document{
		version: Version20,
		description20: &Description20{
			TargetNamespace: "urn:target",
			Types: &Types20{Schemas: []*xsd.Document{{Namespaces: map[string]string{
				"api": "urn:target",
			}}}},
		},
	})
	if err != nil {
		t.Fatalf("newMarshalValue(WSDL 2.0) error = %v", err)
	}
	if value20.prefixes["urn:target"] != "api" {
		t.Fatalf("WSDL 2.0 target prefix = %q", value20.prefixes["urn:target"])
	}
}

func assertPhysicalWriteFailures(t *testing.T, document *Document) {
	t.Helper()
	value, err := newMarshalValue(document)
	if err != nil {
		t.Fatalf("newMarshalValue() error = %v", err)
	}
	recorder := &recordingWriter{}
	encoder := xml.NewEncoder(recorder)
	if err := value.MarshalXML(encoder, xml.StartElement{}); err != nil {
		t.Fatalf("MarshalXML() error = %v", err)
	}
	if err := encoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(recorder.writes) < 10 || recorder.bytes < 50_000 {
		t.Fatalf("physical writes = %d, bytes = %d", len(recorder.writes), recorder.bytes)
	}

	limit := 0
	for index, size := range recorder.writes {
		writer := &boundedFailureWriter{remaining: limit}
		encoder := xml.NewEncoder(writer)
		marshalErr := value.MarshalXML(encoder, xml.StartElement{})
		if marshalErr == nil {
			marshalErr = encoder.Flush()
		}
		if !errors.Is(marshalErr, errInjectedWrite) {
			t.Fatalf("write %d at byte %d error = %v", index, limit, marshalErr)
		}
		limit += size
	}
}

func exhaustiveSerializationDocument11(t *testing.T) *Document {
	t.Helper()
	document, err := Parse(context.Background(), []byte(serializationSource11), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	definitions, ok := document.Definitions11()
	if !ok {
		t.Fatal("Definitions11() failed")
	}
	large := &Documentation{Language: "en", Content: strings.Repeat("documentation ", 400)}
	definitions.Documentation = large
	for index := range definitions.Imports {
		definitions.Imports[index].Documentation = large
	}
	if definitions.Types != nil {
		definitions.Types.Extensions = []Extension{{
			Name: QName{Namespace: "urn:extension", Local: "types"},
			XML: []byte(`<ext:types xmlns:ext="urn:extension">` +
				strings.Repeat("extension ", 400) + `</ext:types>`),
		}}
	}
	for messageIndex := range definitions.Messages {
		message := &definitions.Messages[messageIndex]
		message.Documentation = large
	}
	for portTypeIndex := range definitions.PortTypes {
		portType := &definitions.PortTypes[portTypeIndex]
		portType.Documentation = large
		for operationIndex := range portType.Operations {
			operation := &portType.Operations[operationIndex]
			operation.Documentation = large
			if operation.Input != nil {
				operation.Input.Documentation = large
			}
			if operation.Output != nil {
				operation.Output.Documentation = large
			}
			for faultIndex := range operation.Faults {
				operation.Faults[faultIndex].Documentation = large
			}
		}
	}
	for bindingIndex := range definitions.Bindings {
		binding := &definitions.Bindings[bindingIndex]
		binding.Documentation = large
		for operationIndex := range binding.Operations {
			operation := &binding.Operations[operationIndex]
			operation.Documentation = large
			if operation.Input != nil {
				operation.Input.Documentation = large
			}
			if operation.Output != nil {
				operation.Output.Documentation = large
			}
			for faultIndex := range operation.Faults {
				operation.Faults[faultIndex].Documentation = large
			}
		}
	}
	for serviceIndex := range definitions.Services {
		service := &definitions.Services[serviceIndex]
		service.Documentation = large
		for portIndex := range service.Ports {
			service.Ports[portIndex].Documentation = large
		}
	}
	return &Document{version: Version11, definitions11: &definitions}
}

const serializationSource11 = `<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"` +
	` xmlns:tns="urn:test" xmlns:xs="http://www.w3.org/2001/XMLSchema"` +
	` xmlns:soap12="http://schemas.xmlsoap.org/wsdl/soap12/"` +
	` xmlns:http="http://schemas.xmlsoap.org/wsdl/http/"` +
	` xmlns:mime="http://schemas.xmlsoap.org/wsdl/mime/"` +
	` xmlns:ext="urn:extension" name="Complete" targetNamespace="urn:test"` +
	` ext:flag="present"><documentation>Complete fixture</documentation>` +
	`<import namespace="urn:other" location="other.wsdl"><documentation>Import</documentation>` +
	`<ext:policy/></import><types><xs:schema targetNamespace="urn:test">` +
	`<xs:element name="Value" type="xs:string"/></xs:schema></types>` +
	`<message name="Request"><part name="body" element="tns:Value"/></message>` +
	`<message name="Response"><part name="value" type="xs:string"/></message>` +
	`<portType name="API"><operation name="Call" parameterOrder="body value">` +
	`<input name="Request" message="tns:Request"/><output name="Response"` +
	` message="tns:Response"/><fault name="Failure" message="tns:Response"/>` +
	`</operation></portType><binding name="SOAP" type="tns:API">` +
	`<soap12:binding style="rpc" transport="urn:soap"/><operation name="Call">` +
	`<soap12:operation soapAction="urn:call" soapActionRequired="true" style="document"/>` +
	`<input name="Request"><soap12:body use="encoded" namespace="urn:body"` +
	` encodingStyle="urn:one urn:two" parts="body"/><soap12:header` +
	` message="tns:Request" part="body" use="literal" namespace="urn:header"` +
	` encodingStyle="urn:header-encoding"><soap12:headerfault message="tns:Response"` +
	` part="value" use="encoded" namespace="urn:header-fault"` +
	` encodingStyle="urn:fault-encoding"/></soap12:header></input>` +
	`<output name="Response"><soap12:body use="literal"/></output>` +
	`<fault name="Failure"><soap12:fault name="Failure" use="literal"` +
	` namespace="urn:fault" encodingStyle="urn:fault-encoding"/></fault>` +
	`</operation></binding><binding name="HTTP" type="tns:API">` +
	`<http:binding verb="POST"/><operation name="Call"><http:operation` +
	` location="/call/(body)"/><input name="Request"><http:urlEncoded/>` +
	`<http:urlReplacement/><mime:multipartRelated><mime:part>` +
	`<soap12:body use="literal"/><mime:content part="body" type="application/xml"/>` +
	`<mime:mimeXml part="body"/></mime:part></mime:multipartRelated></input>` +
	`<output name="Response"><mime:content part="value" type="text/plain"/>` +
	`<mime:mimeXml part="value"/></output></operation></binding>` +
	`<service name="Service"><port name="SOAPPort" binding="tns:SOAP">` +
	`<soap12:address location="https://example.test/soap"/></port>` +
	`<port name="HTTPPort" binding="tns:HTTP"><http:address` +
	` location="https://example.test/http"/></port></service><ext:root/></definitions>`

func exhaustiveSerializationDocument20() *Document {
	largeDocumentation := &Documentation{Language: "en", Content: strings.Repeat("documentation ", 400)}
	tns := func(local string) QName { return QName{Namespace: "urn:test", Local: local} }
	xs := func(local string) QName { return QName{Namespace: NamespaceXMLSchema, Local: local} }
	extensibility := func(name string) Extensibility {
		return Extensibility{
			ExtensionAttributes: []ExtensionAttribute{{
				Name: QName{Namespace: "urn:extension", Local: name}, Value: "present",
			}},
			Extensions: []Extension{{
				Name: QName{Namespace: "urn:extension", Local: name},
				XML: []byte(`<ext:` + name + ` xmlns:ext="urn:extension">` +
					strings.Repeat("extension ", 400) + `</ext:` + name + `>`),
			}},
		}
	}
	module := func(name string) SOAPModule20 {
		return SOAPModule20{
			Extensibility: extensibility(name), Ref: "urn:" + name,
			Required: true, RequiredSet: true, Documentation: largeDocumentation,
		}
	}
	header := func(name string) SOAPHeader20 {
		return SOAPHeader20{
			Extensibility: extensibility(name), Element: tns("Request"),
			MustUnderstand: true, MustUnderstandSet: true,
			Required: true, RequiredSet: true, Documentation: largeDocumentation,
		}
	}
	httpHeader := func(name string) HTTPHeader20 {
		return HTTPHeader20{
			Extensibility: extensibility(name), Name: "X-" + name, Type: xs("string"),
			Required: true, RequiredSet: true, Documentation: largeDocumentation,
		}
	}
	description := Description20{
		Extensibility:   extensibility("root"),
		TargetNamespace: "urn:test",
		Documentation:   largeDocumentation,
		Imports: []Import20{{
			Extensibility: extensibility("import"), Namespace: "urn:other",
			Location: "other.wsdl", Documentation: largeDocumentation,
		}},
		Includes: []Include20{{
			Extensibility: extensibility("include"), Location: "included.wsdl",
			Documentation: largeDocumentation,
		}},
		Types: &Types20{Extensibility: extensibility("types")},
		Interfaces: []Interface20{{
			Extensibility: extensibility("interface"), Name: "API",
			Extends: []QName{tns("Parent")}, StyleDefault: []string{"urn:style"},
			Documentation: largeDocumentation,
			Faults: []InterfaceFault20{{
				Extensibility: extensibility("interfaceFault"), Name: "Failure",
				Element: tns("Failure"), MessageContentModel: MessageContentElement,
				MessageContentModelSet: true, Documentation: largeDocumentation,
			}},
			Operations: []InterfaceOperation20{{
				Extensibility: extensibility("interfaceOperation"), Name: "Call",
				Pattern: MEPInOut, Style: []string{"urn:style"}, Safe: true, SafeSet: true,
				RPCSignature:    []RPCSignatureParameter20{{Name: tns("value"), Direction: RPCDirectionIn}},
				RPCSignatureSet: true, Documentation: largeDocumentation,
				Inputs: []InterfaceMessageReference20{{
					Extensibility: extensibility("interfaceInput"), MessageLabel: "In",
					Element: tns("Request"), MessageContentModel: MessageContentElement,
					MessageContentModelSet: true, Documentation: largeDocumentation,
				}},
				Outputs: []InterfaceMessageReference20{{
					Extensibility: extensibility("interfaceOutput"), MessageLabel: "Out",
					Element: tns("Response"), MessageContentModel: MessageContentElement,
					MessageContentModelSet: true, Documentation: largeDocumentation,
				}},
				InFaults: []InterfaceFaultReference20{{
					Extensibility: extensibility("interfaceInFault"), Ref: tns("Failure"),
					MessageLabel: "In", Documentation: largeDocumentation,
				}},
				OutFaults: []InterfaceFaultReference20{{
					Extensibility: extensibility("interfaceOutFault"), Ref: tns("Failure"),
					MessageLabel: "Out", Documentation: largeDocumentation,
				}},
			}},
		}},
	}
	binding := Binding20{
		Extensibility: extensibility("binding"), Name: "Binding", Interface: tns("API"),
		Type: NamespaceWSDL20SOAP, Documentation: largeDocumentation,
		SOAP: &SOAPBinding20{
			Version: "1.2", VersionSet: true, Protocol: "urn:protocol", ProtocolSet: true,
			MEPDefault: "urn:mep", MEPDefaultSet: true, Modules: []SOAPModule20{module("bindingModule")},
		},
		HTTP: &HTTPBinding20{
			MethodDefault: "POST", MethodDefaultSet: true, Version: "1.1", VersionSet: true,
			QueryParameterSeparatorDefault: ";", QueryParameterSeparatorDefaultSet: true,
			ContentEncodingDefault: "gzip", ContentEncodingDefaultSet: true,
			DefaultTransferCoding: "chunked", DefaultTransferCodingSet: true,
			Cookies: true, CookiesSet: true,
		},
		Faults: []BindingFault20{{
			Extensibility: extensibility("bindingFault"), Ref: tns("Failure"),
			Documentation: largeDocumentation,
			SOAP: &SOAPFaultBinding20{
				Code: tns("Code"), CodeSet: true, Subcodes: []QName{tns("Subcode")},
				SubcodesSet: true, Modules: []SOAPModule20{module("faultModule")},
				Headers: []SOAPHeader20{header("faultHeader")},
			},
			HTTP: &HTTPFaultBinding20{
				Code: "500", CodeSet: true, ContentEncoding: "gzip", ContentEncodingSet: true,
				TransferCoding: "chunked", TransferCodingSet: true,
				Headers: []HTTPHeader20{httpHeader("Fault")},
			},
		}},
	}
	binding.Operations = []BindingOperation20{{
		Extensibility: extensibility("bindingOperation"), Ref: tns("Call"),
		Documentation: largeDocumentation,
		SOAP: &SOAPOperationBinding20{
			MEP: "urn:mep", MEPSet: true, Action: "urn:action", ActionSet: true,
			Modules: []SOAPModule20{module("operationModule")},
		},
		HTTP: &HTTPOperationBinding20{
			Location: "items/{id}", LocationSet: true, Method: "PUT", MethodSet: true,
			InputSerialization: "application/xml", InputSerializationSet: true,
			OutputSerialization: "application/xml", OutputSerializationSet: true,
			FaultSerialization: "application/xml", FaultSerializationSet: true,
			QueryParameterSeparator: "&", QueryParameterSeparatorSet: true,
			ContentEncodingDefault: "gzip", ContentEncodingDefaultSet: true,
			DefaultTransferCoding: "chunked", DefaultTransferCodingSet: true,
			IgnoreUncited: true, IgnoreUncitedSet: true,
		},
		Inputs: []BindingMessageReference20{{
			Extensibility: extensibility("bindingInput"), MessageLabel: "In",
			Documentation: largeDocumentation,
			SOAP: &SOAPMessageBinding20{
				Modules: []SOAPModule20{module("inputModule")}, Headers: []SOAPHeader20{header("inputHeader")},
			},
			HTTP: &HTTPMessageBinding20{
				ContentEncoding: "gzip", ContentEncodingSet: true,
				TransferCoding: "chunked", TransferCodingSet: true,
				Headers: []HTTPHeader20{httpHeader("Input")},
			},
		}},
		Outputs: []BindingMessageReference20{{
			Extensibility: extensibility("bindingOutput"), MessageLabel: "Out",
			Documentation: largeDocumentation,
			SOAP:          &SOAPMessageBinding20{Modules: []SOAPModule20{module("outputModule")}},
			HTTP:          &HTTPMessageBinding20{ContentEncoding: "br", ContentEncodingSet: true},
		}},
		InFaults: []BindingFaultReference20{{
			Extensibility: extensibility("bindingInFault"), Ref: tns("Failure"),
			MessageLabel: "In", Documentation: largeDocumentation,
			SOAP: &SOAPFaultReferenceBinding20{Modules: []SOAPModule20{module("inFaultModule")}},
			HTTP: &HTTPFaultReferenceBinding20{TransferCoding: "chunked", TransferCodingSet: true},
		}},
		OutFaults: []BindingFaultReference20{{
			Extensibility: extensibility("bindingOutFault"), Ref: tns("Failure"),
			MessageLabel: "Out", Documentation: largeDocumentation,
			SOAP: &SOAPFaultReferenceBinding20{Modules: []SOAPModule20{module("outFaultModule")}},
			HTTP: &HTTPFaultReferenceBinding20{TransferCoding: "chunked", TransferCodingSet: true},
		}},
	}}
	description.Bindings = []Binding20{binding}
	description.Services = []Service20{{
		Extensibility: extensibility("service"), Name: "Service", Interface: tns("API"),
		Documentation: largeDocumentation,
		Endpoints: []Endpoint20{{
			Extensibility: extensibility("endpoint"), Name: "Endpoint", Binding: tns("Binding"),
			Address: "https://example.test/service", Documentation: largeDocumentation,
			HTTP: &HTTPEndpoint20{
				AuthenticationScheme: "basic", AuthenticationSchemeSet: true,
				AuthenticationRealm: "api", AuthenticationRealmSet: true,
			},
		}},
	}}
	return &Document{version: Version20, description20: &description}
}

func TestMarshalValueQNameAndVersionFailures(t *testing.T) {
	t.Parallel()

	empty := &Document{}
	if _, err := newMarshalValue(empty); err == nil {
		t.Fatal("newMarshalValue(empty) error = nil")
	}
	value := marshalValue{document: empty, prefixes: map[string]string{}}
	if err := value.MarshalXML(xml.NewEncoder(&bytes.Buffer{}), xml.StartElement{}); err == nil {
		t.Fatal("MarshalXML(empty) error = nil")
	}
	if _, err := value.qname(QName{Namespace: "urn:missing", Local: "Name"}); err == nil {
		t.Fatal("qname(unbound) error = nil")
	}
	if lexical, err := value.qname(QName{}); err != nil || lexical != "" {
		t.Fatalf("qname(empty) = (%q, %v)", lexical, err)
	}
	if lexical, err := value.qname(QName{Local: "Name"}); err != nil || lexical != "Name" {
		t.Fatalf("qname(local) = (%q, %v)", lexical, err)
	}
}

func TestMarshalRejectsNilInvalidLimitsAndOutputOverflow(t *testing.T) {
	t.Parallel()

	if _, err := Marshal(nil, MarshalOptions{}); err == nil {
		t.Fatal("Marshal(nil) error = nil")
	}
	if _, err := Marshal(&Document{}, MarshalOptions{MaxBytes: -1}); err == nil {
		t.Fatal("Marshal(negative limit) error = nil")
	}
	if _, err := Marshal(exhaustiveSerializationDocument20(), MarshalOptions{MaxBytes: 0}); err != nil {
		t.Fatalf("Marshal(default limit) error = %v", err)
	}
	if _, err := Marshal(&Document{}, MarshalOptions{}); err == nil {
		t.Fatal("Marshal(empty model) error = nil")
	}
	if _, err := Marshal(exhaustiveSerializationDocument20(), MarshalOptions{MaxBytes: 1}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Marshal(output limit) error = %v", err)
	}
}

func TestSerializationHelpersRejectUnboundNamesAndUnsafeRawXML(t *testing.T) {
	t.Parallel()

	attributes := namespaceAttributes(map[string]string{
		"": "empty", "urn:empty": "", "urn:z": "z", "urn:a": "a",
	})
	if len(attributes) != 2 || attributes[0].Name.Local != "xmlns:a" {
		t.Fatalf("namespaceAttributes() = %#v", attributes)
	}
	value := marshalValue{prefixes: map[string]string{
		"urn:known": "known", "urn:other": "other",
	}}
	ordered, err := value.extensionAttributes(nil, Extensibility{ExtensionAttributes: []ExtensionAttribute{
		{Name: QName{Namespace: "urn:other", Local: "First"}},
		{Name: QName{Namespace: "urn:known", Local: "Zulu"}},
		{Name: QName{Namespace: "urn:known", Local: "Alpha"}},
	}})
	if err != nil || len(ordered) != 3 || ordered[0].Name.Local != "known:Alpha" {
		t.Fatalf("extensionAttributes() = (%#v, %v)", ordered, err)
	}
	unbound := QName{Namespace: "urn:unbound", Local: "Name"}
	if _, err := value.extensionAttributes(nil, Extensibility{ExtensionAttributes: []ExtensionAttribute{{Name: unbound}}}); err == nil {
		t.Fatal("extensionAttributes(unbound) error = nil")
	}
	if _, err := value.qualifiedAttribute(nil, unbound, "value"); err == nil {
		t.Fatal("qualifiedAttribute(unbound) error = nil")
	}
	if err := encodeRawXML(&countingTokenEncoder{}, []byte(`<broken`)); err == nil {
		t.Fatal("encodeRawXML(malformed) error = nil")
	}
	if err := encodeRawXML(&countingTokenEncoder{}, []byte(`<!DOCTYPE extension>`)); !errors.Is(err, ErrDTDForbidden) {
		t.Fatalf("encodeRawXML(DTD) error = %v", err)
	}
	if err := encodeRawXML(&boundedTokenEncoder{}, []byte(`<extension/>`)); !errors.Is(err, errInjectedWrite) {
		t.Fatalf("encodeRawXML(write) error = %v", err)
	}
	unsafeDocument := &Document{version: Version20, description20: &Description20{
		Extensibility: Extensibility{Extensions: []Extension{{XML: []byte(`<!DOCTYPE extension>`)}}},
	}}
	if _, err := Marshal(unsafeDocument, MarshalOptions{}); err == nil {
		t.Fatal("Marshal(unsafe extension) error = nil")
	}
}

func TestDocumentAndLegacyMessageAccessorsCoverEmptyForms(t *testing.T) {
	t.Parallel()

	var document *Document
	if _, ok := document.Description20(); ok {
		t.Fatal("Description20(nil) succeeded")
	}
	if _, ok := document.Definitions11(); ok {
		t.Fatal("Definitions11(nil) succeeded")
	}
	input := InterfaceMessageReference20{MessageLabel: "Input"}
	output := InterfaceMessageReference20{MessageLabel: "Output"}
	if got := interfaceInputs20(InterfaceOperation20{Input: &input}); len(got) != 1 || got[0].MessageLabel != "Input" {
		t.Fatalf("interfaceInputs20() = %#v", got)
	}
	if got := interfaceOutputs20(InterfaceOperation20{Output: &output}); len(got) != 1 || got[0].MessageLabel != "Output" {
		t.Fatalf("interfaceOutputs20() = %#v", got)
	}
}

func TestWSDL11SerializerRejectsUnboundNamesAtEveryModelBoundary(t *testing.T) {
	t.Parallel()

	badName := QName{Namespace: "urn:unbound", Local: "Name"}
	badExtensibility := Extensibility{ExtensionAttributes: []ExtensionAttribute{{Name: badName}}}
	value := marshalValue{prefixes: map[string]string{}}
	encoder := &countingTokenEncoder{}
	tests := map[string]func() error{
		"definitions extension": func() error {
			return value.definitions11(encoder, Definitions11{ExtensionAttributes: badExtensibility.ExtensionAttributes})
		},
		"import extension": func() error {
			return value.import11(encoder, Import11{Extensibility: badExtensibility})
		},
		"types extension": func() error {
			return value.types11(encoder, Types11{Extensibility: badExtensibility})
		},
		"nil schema": func() error {
			return value.types11(encoder, Types11{Schemas: []*xsd.Document{nil}})
		},
		"message extension": func() error {
			return value.message11(encoder, Message11{Extensibility: badExtensibility})
		},
		"part element": func() error {
			return value.message11(encoder, Message11{Parts: []Part11{{Element: badName}}})
		},
		"part type": func() error {
			return value.message11(encoder, Message11{Parts: []Part11{{Type: badName}}})
		},
		"part extension": func() error {
			return value.message11(encoder, Message11{Parts: []Part11{{Extensibility: badExtensibility}}})
		},
		"port type extension": func() error {
			return value.portType11(encoder, PortType11{Extensibility: badExtensibility})
		},
		"operation extension": func() error {
			return value.operation11(encoder, Operation11{Extensibility: badExtensibility})
		},
		"operation message": func() error {
			return value.operationMessage11(encoder, "input", OperationMessage11{Message: badName})
		},
		"operation message extension": func() error {
			return value.operationMessage11(encoder, "input", OperationMessage11{Extensibility: badExtensibility})
		},
		"binding type": func() error {
			return value.binding11(encoder, Binding11{Type: badName})
		},
		"binding extension": func() error {
			return value.binding11(encoder, Binding11{Extensibility: badExtensibility})
		},
		"binding operation extension": func() error {
			return value.bindingOperation11(encoder, BindingOperation11{Extensibility: badExtensibility})
		},
		"binding message extension": func() error {
			return value.bindingMessage11(encoder, "input", BindingMessage11{Extensibility: badExtensibility})
		},
		"SOAP header message": func() error {
			return value.soapHeader11(encoder, SOAPHeader11{Message: badName})
		},
		"SOAP header fault message": func() error {
			return value.soapHeader11(encoder, SOAPHeader11{HeaderFaults: []SOAPHeaderFault11{{Message: badName}}})
		},
		"service extension": func() error {
			return value.service11(encoder, Service11{Extensibility: badExtensibility})
		},
		"port binding": func() error {
			return value.service11(encoder, Service11{Ports: []Port11{{Binding: badName}}})
		},
		"port extension": func() error {
			return value.service11(encoder, Service11{Ports: []Port11{{Extensibility: badExtensibility}}})
		},
	}
	for name, encode := range tests {
		t.Run(name, func(t *testing.T) {
			if err := encode(); err == nil {
				t.Fatal("encode() error = nil")
			}
		})
	}
}

func TestWSDL11SolicitResponsePropagatesBothMessageFailures(t *testing.T) {
	t.Parallel()

	value := marshalValue{prefixes: map[string]string{}}
	operation := Operation11{
		Style:  OperationStyleSolicitResponse,
		Input:  &OperationMessage11{},
		Output: &OperationMessage11{},
	}
	assertTokenClosureFailures(t, func(encoder tokenEncoder) error {
		return value.operation11(encoder, operation)
	})
	if err := value.operation11(&countingTokenEncoder{}, Operation11{
		Style: OperationStyleSolicitResponse, Output: &OperationMessage11{},
	}); err != nil {
		t.Fatalf("operation11(notification) error = %v", err)
	}
}

func TestWSDL20SerializerRejectsUnboundNamesAtEveryModelBoundary(t *testing.T) {
	t.Parallel()

	badName := QName{Namespace: "urn:unbound", Local: "Name"}
	badExtensibility := Extensibility{ExtensionAttributes: []ExtensionAttribute{{Name: badName}}}
	value := marshalValue{prefixes: map[string]string{
		NamespaceWSDL20SOAP: "wsoap",
		NamespaceWSDL20HTTP: "whttp",
	}}
	unqualified := marshalValue{prefixes: map[string]string{}}
	encoder := &countingTokenEncoder{}
	tests := map[string]func() error{
		"description extension": func() error {
			return value.description20(encoder, Description20{Extensibility: badExtensibility})
		},
		"import extension": func() error {
			return value.description20(encoder, Description20{Imports: []Import20{{Extensibility: badExtensibility}}})
		},
		"include extension": func() error {
			return value.description20(encoder, Description20{Includes: []Include20{{Extensibility: badExtensibility}}})
		},
		"types extension": func() error {
			return value.types20(encoder, Types20{Extensibility: badExtensibility})
		},
		"nil schema": func() error {
			return value.types20(encoder, Types20{Schemas: []*xsd.Document{nil}})
		},
		"interface parent": func() error {
			return value.interface20(encoder, Interface20{Extends: []QName{badName}})
		},
		"interface extension": func() error {
			return value.interface20(encoder, Interface20{Extensibility: badExtensibility})
		},
		"interface fault element": func() error {
			return value.interface20(encoder, Interface20{Faults: []InterfaceFault20{{Element: badName}}})
		},
		"interface fault extension": func() error {
			return value.interface20(encoder, Interface20{Faults: []InterfaceFault20{{Extensibility: badExtensibility}}})
		},
		"RPC signature": func() error {
			return value.interfaceOperation20(encoder, InterfaceOperation20{
				RPCSignatureSet: true, RPCSignature: []RPCSignatureParameter20{{Name: badName}},
			})
		},
		"interface operation extension": func() error {
			return value.interfaceOperation20(encoder, InterfaceOperation20{Extensibility: badExtensibility})
		},
		"interface message element": func() error {
			return value.interfaceMessage20(encoder, "input", InterfaceMessageReference20{Element: badName})
		},
		"interface message extension": func() error {
			return value.interfaceMessage20(encoder, "input", InterfaceMessageReference20{Extensibility: badExtensibility})
		},
		"interface fault reference": func() error {
			return value.interfaceFaultReference20(encoder, "infault", InterfaceFaultReference20{Ref: badName})
		},
		"interface fault reference extension": func() error {
			return value.interfaceFaultReference20(encoder, "infault", InterfaceFaultReference20{Extensibility: badExtensibility})
		},
		"binding interface": func() error {
			return value.binding20(encoder, Binding20{Interface: badName})
		},
		"binding SOAP attribute": func() error {
			return unqualified.binding20(encoder, Binding20{SOAP: &SOAPBinding20{VersionSet: true}})
		},
		"binding SOAP protocol attribute": func() error {
			return unqualified.binding20(encoder, Binding20{SOAP: &SOAPBinding20{ProtocolSet: true}})
		},
		"binding SOAP MEP default attribute": func() error {
			return unqualified.binding20(encoder, Binding20{SOAP: &SOAPBinding20{MEPDefaultSet: true}})
		},
		"binding HTTP attribute": func() error {
			return unqualified.binding20(encoder, Binding20{HTTP: &HTTPBinding20{MethodDefaultSet: true}})
		},
		"binding extension": func() error {
			return value.binding20(encoder, Binding20{Extensibility: badExtensibility})
		},
		"binding operation reference": func() error {
			return value.binding20(encoder, Binding20{Operations: []BindingOperation20{{Ref: badName}}})
		},
		"binding operation SOAP MEP": func() error {
			return unqualified.binding20(encoder, Binding20{Operations: []BindingOperation20{{SOAP: &SOAPOperationBinding20{MEPSet: true}}}})
		},
		"binding operation SOAP action": func() error {
			return unqualified.binding20(encoder, Binding20{Operations: []BindingOperation20{{SOAP: &SOAPOperationBinding20{ActionSet: true}}}})
		},
		"binding operation HTTP attribute": func() error {
			return unqualified.binding20(encoder, Binding20{Operations: []BindingOperation20{{HTTP: &HTTPOperationBinding20{MethodSet: true}}}})
		},
		"binding operation extension": func() error {
			return value.binding20(encoder, Binding20{Operations: []BindingOperation20{{Extensibility: badExtensibility}}})
		},
		"binding fault reference": func() error {
			return value.bindingFault20(encoder, BindingFault20{Ref: badName})
		},
		"binding fault SOAP code": func() error {
			return value.bindingFault20(encoder, BindingFault20{SOAP: &SOAPFaultBinding20{CodeSet: true, Code: badName}})
		},
		"binding fault SOAP code attribute": func() error {
			return unqualified.bindingFault20(encoder, BindingFault20{SOAP: &SOAPFaultBinding20{CodeSet: true, CodeAny: true}})
		},
		"binding fault SOAP subcode": func() error {
			return value.bindingFault20(encoder, BindingFault20{SOAP: &SOAPFaultBinding20{SubcodesSet: true, Subcodes: []QName{badName}}})
		},
		"binding fault SOAP subcode attribute": func() error {
			return unqualified.bindingFault20(encoder, BindingFault20{SOAP: &SOAPFaultBinding20{SubcodesSet: true, SubcodesAny: true}})
		},
		"binding fault HTTP attribute": func() error {
			return unqualified.bindingFault20(encoder, BindingFault20{HTTP: &HTTPFaultBinding20{CodeSet: true}})
		},
		"binding fault extension": func() error {
			return value.bindingFault20(encoder, BindingFault20{Extensibility: badExtensibility})
		},
		"binding message HTTP attribute": func() error {
			return unqualified.bindingMessage20(encoder, "input", BindingMessageReference20{HTTP: &HTTPMessageBinding20{ContentEncodingSet: true}})
		},
		"binding message extension": func() error {
			return value.bindingMessage20(encoder, "input", BindingMessageReference20{Extensibility: badExtensibility})
		},
		"binding fault reference name": func() error {
			return value.bindingFaultReference20(encoder, "infault", BindingFaultReference20{Ref: badName})
		},
		"binding fault reference HTTP attribute": func() error {
			return unqualified.bindingFaultReference20(encoder, "infault", BindingFaultReference20{HTTP: &HTTPFaultReferenceBinding20{TransferCodingSet: true}})
		},
		"binding fault reference extension": func() error {
			return value.bindingFaultReference20(encoder, "infault", BindingFaultReference20{Extensibility: badExtensibility})
		},
		"SOAP module extension": func() error {
			return value.soapModule20(encoder, SOAPModule20{Extensibility: badExtensibility})
		},
		"SOAP header element": func() error {
			return value.soapHeader20(encoder, SOAPHeader20{Element: badName})
		},
		"SOAP header extension": func() error {
			return value.soapHeader20(encoder, SOAPHeader20{Extensibility: badExtensibility})
		},
		"typed attribute": func() error {
			_, err := unqualified.typedAttributes20(nil, NamespaceWSDL20HTTP, []typedAttribute20{{local: "method", set: true}})
			return err
		},
		"HTTP header type": func() error {
			return value.httpHeader20(encoder, HTTPHeader20{Type: badName})
		},
		"HTTP header extension": func() error {
			return value.httpHeader20(encoder, HTTPHeader20{Extensibility: badExtensibility})
		},
		"service interface": func() error {
			return value.service20(encoder, Service20{Interface: badName})
		},
		"service extension": func() error {
			return value.service20(encoder, Service20{Extensibility: badExtensibility})
		},
		"endpoint binding": func() error {
			return value.service20(encoder, Service20{Endpoints: []Endpoint20{{Binding: badName}}})
		},
		"endpoint HTTP attribute": func() error {
			return unqualified.service20(encoder, Service20{Endpoints: []Endpoint20{{HTTP: &HTTPEndpoint20{AuthenticationSchemeSet: true}}}})
		},
		"endpoint extension": func() error {
			return value.service20(encoder, Service20{Endpoints: []Endpoint20{{Extensibility: badExtensibility}}})
		},
	}
	for name, encode := range tests {
		t.Run(name, func(t *testing.T) {
			if err := encode(); err == nil {
				t.Fatal("encode() error = nil")
			}
		})
	}
}

func TestWSDL20TypesPropagateImportAndSchemaTokenFailures(t *testing.T) {
	t.Parallel()

	value := marshalValue{prefixes: map[string]string{}}
	assertTokenClosureFailures(t, func(encoder tokenEncoder) error {
		return value.types20(encoder, Types20{Imports: []xsd.SchemaReference{{}}})
	})
	assertTokenClosureFailures(t, func(encoder tokenEncoder) error {
		return value.types20(encoder, Types20{Schemas: []*xsd.Document{{}}})
	})
}

func TestCanonicalizationPropagatesMarshalAndPostParseValidationFailures(t *testing.T) {
	injected := errors.New("injected canonicalization failure")
	originalMarshal := canonicalMarshal
	originalParse := canonicalParse
	originalValidate := canonicalValidate
	t.Cleanup(func() {
		canonicalMarshal = originalMarshal
		canonicalParse = originalParse
		canonicalValidate = originalValidate
	})
	document := &Document{
		version:       Version20,
		description20: &Description20{TargetNamespace: "urn:test"},
	}
	canonicalMarshal = func(*Document, MarshalOptions) ([]byte, error) { return nil, injected }
	if _, err := canonicalDocument(document, ValidationOptions{}); !errors.Is(err, injected) {
		t.Fatalf("canonicalDocument(marshal) error = %v", err)
	}
	canonicalMarshal = originalMarshal
	validationCalls := 0
	canonicalValidate = func(*Document, ValidationOptions) Diagnostics {
		validationCalls++
		if validationCalls == 2 {
			return Diagnostics{{Code: "INJECTED", Severity: SeverityError, Message: "injected"}}
		}
		return nil
	}
	if _, err := canonicalDocument(document, ValidationOptions{}); err == nil {
		t.Fatal("canonicalDocument(post-parse validation) error = nil")
	}
}

func assertTokenClosureFailures(t *testing.T, encode func(tokenEncoder) error) {
	t.Helper()

	counter := &countingTokenEncoder{}
	if err := encode(counter); err != nil {
		t.Fatalf("encode() error = %v", err)
	}
	for index := 0; index < counter.tokens; index++ {
		if err := encode(&boundedTokenEncoder{remaining: index}); !errors.Is(err, errInjectedWrite) {
			t.Fatalf("token %d error = %v", index, err)
		}
	}
}
