package soap_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/soap"
	"github.com/faustbrian/golib/pkg/wire/xmlwire"
)

func TestParseSOAP11EnvelopePreservesRawSections(t *testing.T) {
	t.Parallel()

	payload := readFixture(t, "soap11-response.xml")
	envelope, err := soap.Parse(payload, soap.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if envelope.Version != soap.Version11 {
		t.Fatalf("Parse() version = %q", envelope.Version)
	}
	if !strings.Contains(string(envelope.HeaderXML()), "request-id") {
		t.Fatalf("HeaderXML() = %q", envelope.HeaderXML())
	}
	if !strings.Contains(string(envelope.BodyXML()), "GetRateResponse") {
		t.Fatalf("BodyXML() = %q", envelope.BodyXML())
	}
	if string(envelope.RawXML()) != string(payload) {
		t.Fatal("RawXML() did not preserve the envelope")
	}
}

func TestParseReaderSupportsBoundedStreams(t *testing.T) {
	t.Parallel()

	payload := readFixture(t, "soap11-response.xml")
	exact, err := soap.ParseReader(bytes.NewReader(payload), soap.ParseOptions{MaxBytes: int64(len(payload))})
	if err != nil || exact.Version != soap.Version11 {
		t.Fatalf("exact ParseReader() = %#v, %v", exact, err)
	}
	envelope, err := soap.ParseReader(strings.NewReader(string(payload)), soap.ParseOptions{MaxBytes: math.MaxInt64})
	if err != nil || envelope.Version != soap.Version11 {
		t.Fatalf("ParseReader() = %#v, %v", envelope, err)
	}

	for _, tc := range []struct {
		reader  io.Reader
		options soap.ParseOptions
		kind    error
	}{
		{reader: nil, kind: wire.ErrValidation},
		{reader: failingReader{}, kind: wire.ErrParse},
		{reader: strings.NewReader(string(payload)), options: soap.ParseOptions{MaxBytes: -1}, kind: wire.ErrValidation},
		{reader: strings.NewReader(string(payload)), options: soap.ParseOptions{MaxBytes: 3}, kind: wire.ErrSizeLimit},
	} {
		_, err := soap.ParseReader(tc.reader, tc.options)
		assertKind(t, err, tc.kind)
	}
}

func TestParseEnforcesXMLTokenDepthLimit(t *testing.T) {
	t.Parallel()

	payload := []byte(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><a><b/></a></soap:Body></soap:Envelope>`)
	if _, err := soap.Parse(payload, soap.ParseOptions{MaxDepth: 4}); err != nil {
		t.Fatalf("exact depth error = %v", err)
	}
	if _, err := soap.Parse(payload, soap.ParseOptions{MaxDepth: 3}); !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("depth error = %v", err)
	}
	if _, err := soap.Parse(payload, soap.ParseOptions{MaxDepth: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative depth error = %v", err)
	}
	if _, err := soap.ParseReader(bytes.NewReader(payload), soap.ParseOptions{MaxDepth: -1}); !errors.Is(err, wire.ErrValidation) {
		t.Fatalf("negative reader depth error = %v", err)
	}
}

func TestParseTracksDepthAcrossSiblingElements(t *testing.T) {
	t.Parallel()

	payload := []byte(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><a/><b/><c/></soap:Body></soap:Envelope>`)
	if _, err := soap.Parse(payload, soap.ParseOptions{MaxDepth: 3, MaxBytes: int64(len(payload))}); err != nil {
		t.Fatalf("Parse() exact limits error = %v", err)
	}
}

func TestEnvelopeRawAccessReturnsCopies(t *testing.T) {
	t.Parallel()

	envelope, err := soap.Parse(readFixture(t, "soap11-response.xml"), soap.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, getter := range []func() []byte{envelope.RawXML, envelope.HeaderXML, envelope.BodyXML} {
		first := getter()
		original := first[0]
		first[0] = 'x'
		if getter()[0] != original {
			t.Fatal("raw getter exposed mutable envelope storage")
		}
	}
}

func TestDecodeBodyRetainsInheritedNamespaces(t *testing.T) {
	t.Parallel()

	envelope, err := soap.Parse(readFixture(t, "soap11-response.xml"), soap.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		XMLName xml.Name `xml:"urn:rates GetRateResponse"`
		Amount  string   `xml:"urn:rates Amount"`
	}
	if err := envelope.DecodeBody(&response); err != nil {
		t.Fatalf("DecodeBody() error = %v", err)
	}
	if response.Amount != "12.50" || response.XMLName.Space != "urn:rates" {
		t.Fatalf("DecodeBody() = %#v", response)
	}
}

func TestDecodeBodySkipsHeaderAndWhitespace(t *testing.T) {
	t.Parallel()

	payload := []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Header><trace>abc</trace></env:Header><env:Body>
 <result>ok</result>
 </env:Body></env:Envelope>`)
	envelope, err := soap.Parse(payload, soap.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Value string `xml:",chardata"`
	}
	if err := envelope.DecodeBody(&result); err != nil {
		t.Fatalf("DecodeBody() error = %v", err)
	}
	if result.Value != "ok" {
		t.Fatalf("DecodeBody() value = %q", result.Value)
	}
}

func TestParseUsesDefaultCharsetReader(t *testing.T) {
	t.Parallel()

	payload := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><env:Envelope xmlns:env=\"http://schemas.xmlsoap.org/soap/envelope/\"><env:Body><result>\xe4</result></env:Body></env:Envelope>")
	envelope, err := soap.Parse(payload, soap.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var result struct {
		Value string `xml:",chardata"`
	}
	if err := envelope.DecodeBody(&result); err != nil {
		t.Fatalf("DecodeBody() error = %v", err)
	}
	if result.Value != "ä" {
		t.Fatalf("DecodeBody() value = %q", result.Value)
	}
}

func TestParseSOAP12FaultReturnsTypedErrorAndEnvelope(t *testing.T) {
	t.Parallel()

	envelope, err := soap.Parse(readFixture(t, "soap12-fault.xml"), soap.ParseOptions{})
	if envelope == nil || envelope.Fault == nil {
		t.Fatal("Parse() did not return the fault envelope")
	}
	if !errors.Is(err, wire.ErrSOAPFault) {
		t.Fatalf("Parse() error = %v, want SOAP fault", err)
	}
	var faultError *soap.FaultError
	if !errors.As(err, &faultError) {
		t.Fatalf("Parse() error type = %T", err)
	}
	fault := faultError.Fault
	if fault.Version != soap.Version12 || fault.Code != "env:Sender" || fault.Reason != "Invalid shipment" {
		t.Fatalf("fault = %#v", fault)
	}
	if len(fault.Subcodes) != 1 || fault.Subcodes[0] != "rates:InvalidPostalCode" {
		t.Fatalf("fault subcodes = %#v", fault.Subcodes)
	}
	if len(fault.Reasons) != 2 || fault.Reasons[1].Language != "fi" {
		t.Fatalf("fault reasons = %#v", fault.Reasons)
	}
	if !strings.Contains(string(fault.Detail), "PostalCode") || len(fault.Raw) == 0 {
		t.Fatalf("fault raw fields = %#v", fault)
	}
}

func TestParseSOAP11FaultShape(t *testing.T) {
	t.Parallel()

	envelope, err := soap.Parse(readFixture(t, "soap11-fault.xml"), soap.ParseOptions{})
	if !errors.Is(err, wire.ErrSOAPFault) {
		t.Fatalf("Parse() error = %v", err)
	}
	fault := envelope.Fault
	if fault.Code != "soap:Server" || fault.Reason != "Carrier unavailable" || fault.Actor != "rates" {
		t.Fatalf("fault = %#v", fault)
	}
}

func TestParseRejectsInvalidEnvelopeStructure(t *testing.T) {
	t.Parallel()

	tests := []string{
		`<Envelope xmlns="urn:not-soap"><Body/></Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/><env:Body/></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Header/><env:Header/><env:Body/></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/><env:Header/></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><other/><env:Body/></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Other/><env:Body/></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/">text<env:Body/></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body><env:Fault/><other/></env:Body></env:Envelope>`,
	}

	for _, payload := range tests {
		_, err := soap.Parse([]byte(payload), soap.ParseOptions{})
		assertKind(t, err, wire.ErrEnvelope)
	}
}

func TestParseRejectsMalformedSizeAndOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		payload []byte
		options soap.ParseOptions
		kind    error
	}{
		{payload: readFixture(t, "malformed.xml"), kind: wire.ErrParse},
		{payload: nil, kind: wire.ErrParse},
		{payload: []byte(`text<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/></env:Envelope>`), kind: wire.ErrParse},
		{payload: []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Header><x></env:Header><env:Body/></env:Envelope>`), kind: wire.ErrParse},
		{payload: []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Header/></broken>`), kind: wire.ErrParse},
		{payload: []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body><x></env:Body></env:Envelope>`), kind: wire.ErrParse},
		{payload: []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body><env:Fault><faultcode>x</env:Fault></env:Body></env:Envelope>`), kind: wire.ErrParse},
		{payload: []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/></env:Envelope><extra/>`), kind: wire.ErrParse},
		{payload: []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/></env:Envelope>text`), kind: wire.ErrParse},
		{payload: []byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/></env:Envelope><`), kind: wire.ErrParse},
		{payload: []byte(`<x/>`), options: soap.ParseOptions{MaxBytes: 2}, kind: wire.ErrSizeLimit},
		{payload: []byte(`<x/>`), options: soap.ParseOptions{MaxBytes: -1}, kind: wire.ErrValidation},
	}
	for _, tt := range tests {
		_, err := soap.Parse(tt.payload, tt.options)
		assertKind(t, err, tt.kind)
	}
}

func TestParseAllowsCommentsAroundEnvelopeSections(t *testing.T) {
	t.Parallel()

	payload := []byte(`<!--before--><env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><!--envelope--><env:Body><!--body--><result/></env:Body></env:Envelope><!--after-->`)
	if _, err := soap.Parse(payload, soap.ParseOptions{}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestDecodeBodyRejectsInvalidShapes(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/></env:Envelope>`,
		`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body><one/><two/></env:Body></env:Envelope>`,
	} {
		envelope, err := soap.Parse([]byte(payload), soap.ParseOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var target any
		assertKind(t, envelope.DecodeBody(&target), wire.ErrEnvelope)
	}

	envelope, err := soap.Parse(readFixture(t, "soap11-response.xml"), soap.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertKind(t, envelope.DecodeBody(nil), wire.ErrTarget)
	assertKind(t, envelope.DecodeBody(struct{}{}), wire.ErrTarget)
	var nilTarget *struct{}
	assertKind(t, envelope.DecodeBody(nilTarget), wire.ErrTarget)
	var wrong struct {
		Amount int `xml:"Amount"`
	}
	assertKind(t, envelope.DecodeBody(&wrong), wire.ErrValidation)

	faultEnvelope, faultErr := soap.Parse(readFixture(t, "soap11-fault.xml"), soap.ParseOptions{})
	if !errors.Is(faultErr, wire.ErrSOAPFault) || !errors.Is(faultEnvelope.DecodeBody(&wrong), wire.ErrSOAPFault) {
		t.Fatal("DecodeBody() did not preserve SOAP fault classification")
	}

	var target any
	assertKind(t, (&soap.Envelope{}).DecodeBody(&target), wire.ErrParse)
}

func TestMarshalEnvelopeRoundTripsRawFragments(t *testing.T) {
	t.Parallel()

	header := []byte(`<trace xmlns="urn:trace">abc</trace>`)
	body := []byte(`<GetRate xmlns="urn:rates"><PostalCode>00100</PostalCode></GetRate>`)
	payload, err := soap.Marshal(soap.Version12, header, body)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := soap.Parse(payload, soap.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Version != soap.Version12 || string(envelope.HeaderXML()) != string(header) || string(envelope.BodyXML()) != string(body) {
		t.Fatalf("round trip = %#v", envelope)
	}

	payload, err = soap.Marshal(soap.Version11, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `<soap:Body></soap:Body>`) || strings.Contains(string(payload), `<soap:Header>`) {
		t.Fatalf("Marshal() = %q", payload)
	}
}

func TestEncodeTurnsTypedValuesIntoSOAP(t *testing.T) {
	t.Parallel()

	header := struct {
		XMLName xml.Name `xml:"urn:trace request-id"`
		Value   string   `xml:",chardata"`
	}{Value: "abc"}
	body := struct {
		XMLName xml.Name `xml:"urn:rates GetRateResponse"`
		Amount  string   `xml:"urn:rates Amount"`
	}{Amount: "12.50"}

	payload, err := soap.Encode(soap.Version12, header, body, soap.EncodeOptions{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	envelope, err := soap.Parse(payload, soap.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !strings.Contains(string(envelope.HeaderXML()), "request-id") {
		t.Fatalf("HeaderXML() = %q", envelope.HeaderXML())
	}
	var decoded struct {
		XMLName xml.Name `xml:"urn:rates GetRateResponse"`
		Amount  string   `xml:"urn:rates Amount"`
	}
	if err := envelope.DecodeBody(&decoded); err != nil || decoded.Amount != "12.50" {
		t.Fatalf("DecodeBody() = %#v, %v", decoded, err)
	}
}

func TestSOAPWriterAPIsWriteCompleteEnvelopes(t *testing.T) {
	t.Parallel()

	body := struct {
		XMLName xml.Name `xml:"Ping"`
		Value   string   `xml:",chardata"`
	}{Value: "ok"}

	var typed bytes.Buffer
	if err := soap.EncodeWriter(&typed, soap.Version11, nil, body, soap.EncodeOptions{}); err != nil {
		t.Fatalf("EncodeWriter() error = %v", err)
	}
	if _, err := soap.Parse(typed.Bytes(), soap.ParseOptions{}); err != nil {
		t.Fatalf("Parse(EncodeWriter()) error = %v", err)
	}

	var raw bytes.Buffer
	if err := soap.MarshalWriter(&raw, soap.Version11, nil, []byte(`<Ping>ok</Ping>`)); err != nil {
		t.Fatalf("MarshalWriter() error = %v", err)
	}

	var fault bytes.Buffer
	if err := soap.MarshalFaultWriter(&fault, soap.Fault{Version: soap.Version11, Code: "soap:Server", Reason: "failed"}); err != nil {
		t.Fatalf("MarshalFaultWriter() error = %v", err)
	}
	if _, err := soap.Parse(fault.Bytes(), soap.ParseOptions{}); !errors.Is(err, wire.ErrSOAPFault) {
		t.Fatalf("Parse(MarshalFaultWriter()) error = %v", err)
	}
}

func TestSOAPEncodingClassifiesValueAndWriterFailures(t *testing.T) {
	t.Parallel()
	body := struct {
		XMLName xml.Name `xml:"Ping"`
	}{}

	for _, tc := range []struct {
		header any
		body   any
	}{
		{header: make(chan int), body: struct{}{}},
		{body: make(chan int)},
	} {
		_, err := soap.Encode(soap.Version11, tc.header, tc.body, soap.EncodeOptions{})
		assertKind(t, err, wire.ErrValidation)
	}

	assertKind(t, soap.EncodeWriter(errorWriter{}, soap.Version11, nil, body, soap.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, soap.EncodeWriter(shortWriter{}, soap.Version11, nil, body, soap.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, soap.MarshalWriter(errorWriter{}, soap.Version11, nil, nil), wire.ErrWrite)
	assertKind(t, soap.MarshalWriter(shortWriter{}, soap.Version11, nil, nil), wire.ErrWrite)
	assertKind(t, soap.MarshalFaultWriter(errorWriter{}, soap.Fault{Version: soap.Version11, Code: "x", Reason: "y"}), wire.ErrWrite)
	assertKind(t, soap.MarshalFaultWriter(shortWriter{}, soap.Fault{Version: soap.Version11, Code: "x", Reason: "y"}), wire.ErrWrite)
	assertKind(t, soap.EncodeWriter(nil, soap.Version11, nil, body, soap.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, soap.EncodeWriter(&bytes.Buffer{}, soap.Version11, nil, make(chan int), soap.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, soap.MarshalWriter(&bytes.Buffer{}, "unknown", nil, nil), wire.ErrValidation)
	assertKind(t, soap.MarshalFaultWriter(&bytes.Buffer{}, soap.Fault{}), wire.ErrValidation)
}

func TestMarshalRejectsVersionAndMalformedFragments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		version soap.Version
		header  []byte
		body    []byte
	}{
		{version: "unknown"},
		{version: soap.Version11, header: []byte(`<broken>`)},
		{version: soap.Version11, body: []byte(`<broken>`)},
		{version: soap.Version11, body: []byte(`text`)},
		{version: soap.Version11, body: []byte(`<result/>text`)},
	} {
		_, err := soap.Marshal(tc.version, tc.header, tc.body)
		if err == nil {
			t.Fatal("Marshal() error = nil")
		}
	}
}

func TestMarshalFaultRoundTripsBothVersions(t *testing.T) {
	t.Parallel()

	tests := []soap.Fault{
		{Version: soap.Version11, Code: "soap:Server", Reason: "Unavailable", Actor: "rates", Detail: []byte(`<retry>later</retry>`)},
		{Version: soap.Version11, Code: "soap:Client", Reason: "Invalid"},
		{Version: soap.Version12, Code: "env:Sender", Subcodes: []string{"rates:Invalid"}, Reasons: []soap.FaultReason{{Language: "en", Text: "Invalid"}, {Language: "fi", Text: "Virhe"}}, Node: "node", Role: "role", Detail: []byte(`<field>postal_code</field>`)},
		{Version: soap.Version12, Code: "env:Receiver", Reason: "Unavailable"},
	}
	for _, fault := range tests {
		payload, err := soap.MarshalFault(fault)
		if err != nil {
			t.Fatalf("MarshalFault() error = %v", err)
		}
		envelope, err := soap.Parse(payload, soap.ParseOptions{})
		if !errors.Is(err, wire.ErrSOAPFault) {
			t.Fatalf("Parse() error = %v", err)
		}
		if envelope.Fault.Code != fault.Code || envelope.Fault.Version != fault.Version ||
			envelope.Fault.Actor != fault.Actor || envelope.Fault.Node != fault.Node || envelope.Fault.Role != fault.Role ||
			string(envelope.Fault.Detail) != string(fault.Detail) {
			t.Fatalf("round-trip fault = %#v", envelope.Fault)
		}
	}
}

func TestMarshalFaultOmitsEmptyOptionalElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fault     soap.Fault
		forbidden []string
	}{
		{
			fault:     soap.Fault{Version: soap.Version11, Code: "soap:Server", Reason: "failed"},
			forbidden: []string{"<faultactor>", "<detail>"},
		},
		{
			fault:     soap.Fault{Version: soap.Version12, Code: "env:Receiver", Reason: "failed"},
			forbidden: []string{"<soap:Node>", "<soap:Role>", "<soap:Detail>"},
		},
	}
	for _, test := range tests {
		payload, err := soap.MarshalFault(test.fault)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range test.forbidden {
			if strings.Contains(string(payload), forbidden) {
				t.Fatalf("MarshalFault() = %q, contains %q", payload, forbidden)
			}
		}
	}
}

func TestMarshalFaultValidatesRequiredFieldsAndDetail(t *testing.T) {
	t.Parallel()

	for _, fault := range []soap.Fault{
		{},
		{Version: soap.Version11},
		{Version: soap.Version11, Code: "code"},
		{Version: soap.Version12, Code: "code"},
		{Version: soap.Version11, Code: "code", Reason: "reason", Detail: []byte(`<broken>`)},
	} {
		if _, err := soap.MarshalFault(fault); err == nil {
			t.Fatal("MarshalFault() error = nil")
		}
	}
}

func TestMarshalFaultEscapesTextAndAttributeValues(t *testing.T) {
	t.Parallel()

	fault := soap.Fault{
		Version: soap.Version12,
		Code:    `env:Sender<&`,
		Reasons: []soap.FaultReason{{Language: `en" data-x="bad`, Text: `invalid <postal> & value`}},
	}
	payload, err := soap.MarshalFault(fault)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := soap.Parse(payload, soap.ParseOptions{})
	if !errors.Is(err, wire.ErrSOAPFault) {
		t.Fatalf("Parse() error = %v", err)
	}
	if envelope.Fault.Code != fault.Code || len(envelope.Fault.Reasons) != 1 || envelope.Fault.Reasons[0] != fault.Reasons[0] {
		t.Fatalf("round-trip escaped fault = %#v", envelope.Fault)
	}
}

func TestFaultErrorWithoutReason(t *testing.T) {
	t.Parallel()

	err := &soap.FaultError{Fault: soap.Fault{Code: "code"}}
	if got := err.Error(); got != "soap fault: code" {
		t.Fatalf("Error() = %q", got)
	}
	if !errors.Is(err, wire.ErrSOAPFault) {
		t.Fatal("errors.Is() = false")
	}

	err = &soap.FaultError{Fault: soap.Fault{Code: "code", Reason: "reason"}}
	if got := err.Error(); got != "soap fault: code: reason" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestParseRejectsInvalidFaultShapes(t *testing.T) {
	t.Parallel()

	validFault := `<env:Fault><faultcode>env:Server</faultcode><faultstring>failure</faultstring></env:Fault>`
	for _, body := range []string{
		`<env:Fault/>`,
		validFault + `<other/>`,
		`text`,
	} {
		payload := `<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body>` + body + `</env:Body></env:Envelope>`
		_, err := soap.Parse([]byte(payload), soap.ParseOptions{})
		assertKind(t, err, wire.ErrEnvelope)
	}
}

func TestParseDoesNotClassifyFaultLookalikes(t *testing.T) {
	t.Parallel()

	for _, child := range []string{
		`<Fault xmlns="urn:not-soap"><faultcode>x</faultcode><faultstring>y</faultstring></Fault>`,
		`<env:Result>Fault</env:Result>`,
	} {
		payload := `<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body>` + child + `</env:Body></env:Envelope>`
		envelope, err := soap.Parse([]byte(payload), soap.ParseOptions{})
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if envelope.Fault != nil {
			t.Fatalf("Parse() fault = %#v", envelope.Fault)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(readFixture(f, "soap11-response.xml"))
	f.Add(readFixture(f, "soap11-fault.xml"))
	f.Add(readFixture(f, "soap12-fault.xml"))
	f.Add(readFixture(f, "malformed.xml"))
	f.Add([]byte(`<Envelope xmlns="urn:not-soap"><Body/></Envelope>`))
	f.Add([]byte(`<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body/><env:Body/></env:Envelope>`))
	f.Add([]byte("<root>\x00</root>"))
	f.Add([]byte(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body>` + strings.Repeat("<a>", xmlwire.DefaultMaxDepth) + strings.Repeat("</a>", xmlwire.DefaultMaxDepth) + `</soap:Body></soap:Envelope>`))
	f.Fuzz(func(t *testing.T, payload []byte) {
		_, _ = soap.Parse(payload, soap.ParseOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkParse(b *testing.B) {
	payload := readFixture(b, "soap11-response.xml")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := soap.Parse(payload, soap.ParseOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal(b *testing.B) {
	body := []byte(`<GetRate xmlns="urn:rates"><PostalCode>00100</PostalCode></GetRate>`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := soap.Marshal(soap.Version11, nil, body); err != nil {
			b.Fatal(err)
		}
	}
}

func readFixture(tb testing.TB, name string) []byte {
	tb.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		tb.Fatal(err)
	}
	return payload
}

func assertKind(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want errors.Is(_, %v)", err, target)
	}
	var wireErr *wire.Error
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatSOAP {
		t.Fatalf("error = %#v, want SOAP *wire.Error", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("vendor stream failed")
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("vendor stream failed")
}

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) {
	return len(payload) - 1, nil
}
