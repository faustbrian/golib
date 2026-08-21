package wsdl

import "testing"

func TestHTTPVersionAndTokenLexicalRules(t *testing.T) {
	t.Parallel()

	for value, valid := range map[string]bool{
		"1.1": true, "20.0": true, "9.9": true,
		"": false, "1": false, ".1": false, "1.": false,
		"1.2.3": false, "/.1": false, "1.:": false,
	} {
		if got := isHTTPVersion20(value); got != valid {
			t.Errorf("isHTTPVersion20(%q) = %t, want %t", value, got, valid)
		}
	}
	for value, valid := range map[string]bool{
		"GET": true, "M-SEARCH": true, "": false, "BAD METHOD": false,
		"BAD/SLASH": false, "nonascii-å": false, string(rune(127)): false,
	} {
		if got := isHTTPToken20(value); got != valid {
			t.Errorf("isHTTPToken20(%q) = %t, want %t", value, got, valid)
		}
	}
}

func TestValidationDefaultDiagnosticLimitIsNotNegative(t *testing.T) {
	t.Parallel()

	document := &Document{version: Version20, description20: &Description20{
		TargetNamespace: "urn:test",
	}}
	for _, diagnostic := range Validate(document, ValidationOptions{MaxDiagnostics: 0}) {
		if diagnostic.Code == "WSDL_OPTIONS" {
			t.Fatalf("default options produced %#v", diagnostic)
		}
	}
}

func TestHTTPQuerySeparatorAcceptsEveryLexicalBoundary(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 10}
	for _, value := range []string{"&", ";", "a", "9", "~", "/"} {
		validateHTTPQuerySeparator20(value, Location{}, &collector)
	}
	if len(collector.diagnostics) != 0 {
		t.Fatalf("valid separators produced diagnostics: %#v", collector.diagnostics)
	}
	for _, value := range []string{"", "ab", " ", "å"} {
		validateHTTPQuerySeparator20(value, Location{}, &collector)
	}
	if len(collector.diagnostics) != 4 {
		t.Fatalf("invalid separator diagnostics = %#v", collector.diagnostics)
	}
}

func TestWSDL11DefaultOperationMessageNames(t *testing.T) {
	t.Parallel()

	tests := map[OperationStyle11][2]string{
		OperationStyleOneWay:          {"Call", ""},
		OperationStyleRequestResponse: {"CallRequest", "CallResponse"},
		OperationStyleSolicitResponse: {"CallResponse", "CallSolicit"},
		OperationStyleNotification:    {"", "Call"},
	}
	for style, want := range tests {
		input, output := operationMessageNames11(Operation11{
			Name: "Call", Style: style,
			Input: &OperationMessage11{}, Output: &OperationMessage11{},
		})
		if input != want[0] || output != want[1] {
			t.Errorf("operationMessageNames11(%s) = (%q, %q)", style, input, output)
		}
	}
	input, output := operationMessageNames11(Operation11{
		Name: "Call", Style: OperationStyleRequestResponse,
		Input:  &OperationMessage11{Name: "CustomIn"},
		Output: &OperationMessage11{Name: "CustomOut"},
	})
	if input != "CustomIn" || output != "CustomOut" {
		t.Fatalf("explicit names = (%q, %q)", input, output)
	}
}

func TestWSDL20InitialMessageFollowsPredefinedMEPDirection(t *testing.T) {
	t.Parallel()

	input := InterfaceMessageReference20{MessageLabel: "In"}
	output := InterfaceMessageReference20{MessageLabel: "Out"}
	for _, test := range []struct {
		operation InterfaceOperation20
		want      string
	}{
		{operation: InterfaceOperation20{Pattern: MEPInOnly, Inputs: []InterfaceMessageReference20{input}}, want: "In"},
		{operation: InterfaceOperation20{Pattern: MEPOutOnly, Outputs: []InterfaceMessageReference20{output}}, want: "Out"},
		{operation: InterfaceOperation20{Pattern: MEPOutOnly}, want: ""},
		{operation: InterfaceOperation20{Pattern: "urn:custom", Outputs: []InterfaceMessageReference20{output}}, want: "Out"},
	} {
		message := initialMessage20(test.operation)
		if test.want == "" {
			if message != nil {
				t.Errorf("initialMessage20() = %#v, want nil", message)
			}
			continue
		}
		if message == nil || message.MessageLabel != test.want {
			t.Errorf("initialMessage20() = %#v, want %q", message, test.want)
		}
	}
	if message := initialMessage20(InterfaceOperation20{}); message != nil {
		t.Fatalf("initialMessage20(empty) = %#v", message)
	}
}

func TestRequiredExtensionTraversalCoversNestedFaultAndInputBranches(t *testing.T) {
	t.Parallel()

	required := Extension{
		Name:     QName{Namespace: "urn:unknown", Local: "Required"},
		Required: true, RequiredSet: true,
	}
	collector := diagnosticCollector{max: 10}
	validateRequiredExtensions(&Document{version: Version11, definitions11: &Definitions11{
		PortTypes: []PortType11{{Operations: []Operation11{{
			Input: &OperationMessage11{Extensibility: Extensibility{Extensions: []Extension{required}}},
		}}}},
	}}, nil, &collector)
	validateRequiredExtensions(&Document{version: Version20, description20: &Description20{
		Bindings: []Binding20{{Operations: []BindingOperation20{{
			InFaults: []BindingFaultReference20{{SOAP: &SOAPFaultReferenceBinding20{
				Modules: []SOAPModule20{{Extensibility: Extensibility{Extensions: []Extension{required}}}},
			}}},
		}}}},
	}}, nil, &collector)
	if countDiagnostics(collector.diagnostics, "WSDL_EXTENSION_REQUIRED") != 2 {
		t.Fatalf("required-extension diagnostics = %#v", collector.diagnostics)
	}
}

func TestRequiredExtensionValidationSkipsOptionalAndUnderstoodBeforeUnknown(t *testing.T) {
	t.Parallel()

	understoodName := QName{Namespace: "urn:known", Local: "Known"}
	unknownName := QName{Namespace: "urn:unknown", Local: "Unknown"}
	document := &Document{version: Version11, definitions11: &Definitions11{
		Extensions: []Extension{
			{Name: QName{Namespace: "urn:optional", Local: "Optional"}},
			{Name: understoodName, Required: true, RequiredSet: true},
			{Name: unknownName, Required: true, RequiredSet: true},
		},
	}}
	collector := diagnosticCollector{max: 10}
	validateRequiredExtensions(document, map[QName]struct{}{understoodName: {}}, &collector)
	if countDiagnostics(collector.diagnostics, "WSDL_EXTENSION_REQUIRED") != 1 ||
		collector.diagnostics[0].Message != "required extension {urn:unknown}Unknown is not understood" {
		t.Fatalf("diagnostics = %#v", collector.diagnostics)
	}
}

func TestWSDL11ValidationSkipsIrrelevantReferencesBeforeMissingReferences(t *testing.T) {
	t.Parallel()

	tns := func(local string) QName { return QName{Namespace: "urn:test", Local: local} }
	document := &Document{version: Version11, definitions11: &Definitions11{
		TargetNamespace: "urn:test",
		Bindings:        []Binding11{{Name: "Existing"}},
		Services: []Service11{{Name: "Service", Ports: []Port11{
			{Name: "External", Binding: QName{Namespace: "urn:external", Local: "Binding"}},
			{Name: "Existing", Binding: tns("Existing")},
			{Name: "Missing", Binding: tns("Missing")},
		}}},
	}}
	diagnostics := Validate(document, ValidationOptions{})
	if countDiagnostics(diagnostics, "WSDL11_BINDING_REFERENCE") != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestWSDL11OperationStyleReportsEachIndependentInvalidState(t *testing.T) {
	t.Parallel()

	input := &OperationMessage11{}
	output := &OperationMessage11{}
	for name, operation := range map[string]Operation11{
		"both unset": {Name: "Call", Style: ""},
		"unset":      {Name: "Call", Input: input, Style: ""},
		"unknown":    {Name: "Call", Style: OperationStyleOneWay},
		"mismatch":   {Name: "Call", Input: input, Output: output, Style: OperationStyleOneWay},
	} {
		collector := diagnosticCollector{max: 10}
		validateOperationStyle11("Port", operation, &collector)
		if countDiagnostics(collector.diagnostics, "WSDL11_OPERATION_STYLE") != 1 {
			t.Errorf("%s diagnostics = %#v", name, collector.diagnostics)
		}
	}
}

func TestWSDL11MatchingAndBodyValidationContinueToLaterCandidates(t *testing.T) {
	t.Parallel()

	operations := []Operation11{
		{Name: "Call", Style: OperationStyleRequestResponse, Output: &OperationMessage11{Name: "Wrong"}},
		{Name: "Call", Style: OperationStyleRequestResponse, Output: &OperationMessage11{Name: "Wanted"}},
	}
	match := matchingOperation11(BindingOperation11{
		Name: "Call", Output: &BindingMessage11{Name: "Wanted"},
	}, operations)
	if match != &operations[1] {
		t.Fatalf("matchingOperation11() = %#v", match)
	}
	message := QName{Namespace: "urn:test", Local: "Message"}
	collector := diagnosticCollector{max: 10}
	validateSOAPBindingMessage11(
		&BindingMessage11{SOAPBody: &SOAPBody11{Parts: []string{"known", "missing"}}},
		&OperationMessage11{Message: message},
		"urn:test",
		map[QName]map[string]struct{}{message: {"known": {}}},
		&collector,
	)
	if countDiagnostics(collector.diagnostics, "WSDL11_SOAP_BODY_PART") != 1 {
		t.Fatalf("diagnostics = %#v", collector.diagnostics)
	}
}

func TestWSDL20ValidationSkipsIrrelevantReferencesBeforeMissingReferences(t *testing.T) {
	t.Parallel()

	tns := func(local string) QName { return QName{Namespace: "urn:test", Local: local} }
	external := func(local string) QName { return QName{Namespace: "urn:external", Local: local} }
	document := &Document{version: Version20, description20: &Description20{
		TargetNamespace: "urn:test",
		Interfaces: []Interface20{{Name: "API", Faults: []InterfaceFault20{{Name: "KnownFault"}}, Operations: []InterfaceOperation20{{
			Name: "KnownOperation",
		}}}},
		Bindings: []Binding20{{Name: "Binding", Interface: tns("API"),
			Faults:     []BindingFault20{{Ref: external("Fault")}, {Ref: tns("KnownFault")}, {Ref: tns("MissingFault")}},
			Operations: []BindingOperation20{{Ref: external("Operation")}, {Ref: tns("KnownOperation")}, {Ref: tns("MissingOperation")}},
		}},
		Services: []Service20{{Name: "Service", Interface: tns("API"), Endpoints: []Endpoint20{
			{Name: "External", Binding: external("Binding")},
			{Name: "Existing", Binding: tns("Binding")},
			{Name: "Missing", Binding: tns("MissingBinding")},
		}}},
	}}
	diagnostics := Validate(document, ValidationOptions{})
	for _, code := range []string{
		"WSDL20_BINDING_FAULT", "WSDL20_OPERATION_REFERENCE", "WSDL20_BINDING_REFERENCE",
	} {
		if countDiagnostics(diagnostics, code) != 1 {
			t.Errorf("%s diagnostics = %#v", code, diagnostics)
		}
	}
}

func TestHTTPValidationDistinguishesUnsetValidAndInvalidProperties(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 30}
	validateHTTPBinding20(Binding20{
		HTTP: &HTTPBinding20{MethodDefault: "BAD METHOD", Version: "bad"},
		Faults: []BindingFault20{
			{},
			{HTTP: &HTTPFaultBinding20{Code: "invalid", CodeSet: true}},
		},
		Operations: []BindingOperation20{
			{HTTP: &HTTPOperationBinding20{Method: "BAD METHOD"}},
			{HTTP: &HTTPOperationBinding20{Method: "BAD METHOD", MethodSet: true}},
		},
	}, &collector)
	validateHTTPBinding20(Binding20{HTTP: &HTTPBinding20{
		MethodDefault: "GET", MethodDefaultSet: true, Version: "1.1", VersionSet: true,
	}}, &collector)
	validateHTTPBinding20(Binding20{HTTP: &HTTPBinding20{
		MethodDefault: "BAD METHOD", MethodDefaultSet: true,
		Version: "bad", VersionSet: true,
	}}, &collector)
	for code, want := range map[string]int{
		"WSDL20_HTTP_METHOD":      2,
		"WSDL20_HTTP_VERSION":     1,
		"WSDL20_HTTP_STATUS_CODE": 1,
	} {
		if got := countDiagnostics(collector.diagnostics, code); got != want {
			t.Errorf("%s diagnostics = %d, want %d: %#v", code, got, want, collector.diagnostics)
		}
	}
}

func TestHTTPEndpointValidationDistinguishesEveryAuthenticationState(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 20}
	for _, endpoint := range []Endpoint20{
		{HTTP: &HTTPEndpoint20{AuthenticationScheme: "invalid"}},
		{HTTP: &HTTPEndpoint20{AuthenticationScheme: "basic", AuthenticationSchemeSet: true}},
		{HTTP: &HTTPEndpoint20{AuthenticationScheme: "digest", AuthenticationSchemeSet: true}},
		{HTTP: &HTTPEndpoint20{AuthenticationScheme: "invalid", AuthenticationSchemeSet: true}},
		{HTTP: &HTTPEndpoint20{AuthenticationRealm: "realm"}},
		{HTTP: &HTTPEndpoint20{AuthenticationRealm: "realm", AuthenticationRealmSet: true, AuthenticationScheme: "basic", AuthenticationSchemeSet: true}},
		{HTTP: &HTTPEndpoint20{AuthenticationRealm: "realm", AuthenticationRealmSet: true}},
	} {
		validateHTTPEndpoint20(endpoint, &collector)
	}
	if countDiagnostics(collector.diagnostics, "WSDL20_HTTP_AUTHENTICATION_SCHEME") != 1 ||
		countDiagnostics(collector.diagnostics, "WSDL20_HTTP_AUTHENTICATION_REALM") != 1 {
		t.Fatalf("diagnostics = %#v", collector.diagnostics)
	}
}

func TestSOAPAndBindingReferenceValidationContinuesToLaterInvalidValues(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 30}
	validateSOAPBinding20(Binding20{Faults: []BindingFault20{
		{},
		{SOAP: &SOAPFaultBinding20{Modules: []SOAPModule20{{Ref: "relative"}}}},
	}}, &collector)
	validateSOAPHeaders20([]SOAPHeader20{
		{Element: QName{Namespace: "urn:test", Local: "Known"}}, {},
	}, &collector)
	validateBindingMessages20(
		"Binding", QName{Local: "Call"}, "input",
		[]BindingMessageReference20{{MessageLabel: "Known"}, {MessageLabel: "Missing"}},
		&InterfaceMessageReference20{MessageLabel: "Known"}, &collector,
	)
	knownFault := QName{Namespace: "urn:test", Local: "Known"}
	validateBindingFaults20(
		"Binding", QName{Local: "Call"},
		[]BindingFaultReference20{{Ref: knownFault}, {Ref: QName{Namespace: "urn:test", Local: "Missing"}}},
		[]InterfaceFaultReference20{{Ref: knownFault}}, &collector,
	)
	for code, want := range map[string]int{
		"WSDL20_SOAP_MODULE_IRI":           1,
		"WSDL20_SOAP_HEADER_ELEMENT":       1,
		"WSDL20_BINDING_MESSAGE_REFERENCE": 1,
		"WSDL20_BINDING_FAULT_REFERENCE":   1,
	} {
		if got := countDiagnostics(collector.diagnostics, code); got != want {
			t.Errorf("%s diagnostics = %d, want %d: %#v", code, got, want, collector.diagnostics)
		}
	}
}

func TestFaultLookupAndMatchingDistinguishEachIdentityField(t *testing.T) {
	t.Parallel()

	faults := []OperationMessage11{{Name: "First"}, {Name: "Second"}}
	if got := operationFault11(faults, "Second"); got != &faults[1] {
		t.Fatalf("operationFault11(Second) = %#v", got)
	}
	if got := operationFault11(faults, "Missing"); got != nil {
		t.Fatalf("operationFault11(Missing) = %#v", got)
	}
	known := QName{Namespace: "urn:test", Local: "Known"}
	other := QName{Namespace: "urn:test", Local: "Other"}
	interfaceReferences := []InterfaceFaultReference20{{Ref: known, MessageLabel: "KnownLabel"}}
	for name, test := range map[string]struct {
		reference BindingFaultReference20
		want      bool
	}{
		"empty label":      {reference: BindingFaultReference20{Ref: known}, want: true},
		"matching label":   {reference: BindingFaultReference20{Ref: known, MessageLabel: "KnownLabel"}, want: true},
		"mismatched label": {reference: BindingFaultReference20{Ref: known, MessageLabel: "OtherLabel"}},
		"mismatched fault": {reference: BindingFaultReference20{Ref: other, MessageLabel: "KnownLabel"}},
	} {
		if got := bindingFaultMatches20(test.reference, interfaceReferences); got != test.want {
			t.Errorf("%s match = %t, want %t", name, got, test.want)
		}
	}
}

func TestWSDL11FaultAndMIMEValidationUsesEverySemanticBranch(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 20}
	faultMessage := QName{Namespace: "urn:test", Local: "Fault"}
	validateBindingOperation11("Binding", BindingOperation11{
		Name: "Call", Faults: []BindingMessage11{{
			Name: "Failure", SOAPBody: &SOAPBody11{Parts: []string{"missing"}},
		}},
	}, PortType11{Operations: []Operation11{{
		Name: "Call", Faults: []OperationMessage11{{Name: "Failure", Message: faultMessage}},
	}}}, "urn:test", map[QName]map[string]struct{}{faultMessage: {}}, &collector)
	validateBindingProperties11(Binding11{Operations: []BindingOperation11{{
		Faults: []BindingMessage11{{SOAPFault: &SOAPFault11{Use: "invalid", UseSet: true}}},
	}}}, &collector)
	validateMIMEMessage11(&MIMEMessage11{
		Contents:  []MIMEContent11{{Part: "body", Type: "text/plain; invalid"}},
		Multipart: []MIMEMultipart11{{}, {Parts: []MIMEPart11{{}}}},
	}, map[string]struct{}{"body": {}}, Location{}, &collector)
	for code, want := range map[string]int{
		"WSDL11_SOAP_BODY_PART": 1,
		"WSDL11_SOAP_USE":       1,
		"WSDL11_MIME_TYPE":      1,
		"WSDL11_MIME_MULTIPART": 1,
	} {
		if got := countDiagnostics(collector.diagnostics, code); got != want {
			t.Errorf("%s diagnostics = %d, want %d: %#v", code, got, want, collector.diagnostics)
		}
	}
}

func TestMIMEMultipartEmptyAndNonemptyCasesAreDistinct(t *testing.T) {
	t.Parallel()

	empty := diagnosticCollector{max: 10}
	validateMIMEMessage11(&MIMEMessage11{
		Multipart: []MIMEMultipart11{{}},
	}, nil, Location{}, &empty)
	if countDiagnostics(empty.diagnostics, "WSDL11_MIME_MULTIPART") != 1 {
		t.Fatalf("empty multipart diagnostics = %#v", empty.diagnostics)
	}

	nonempty := diagnosticCollector{max: 10}
	validateMIMEMessage11(&MIMEMessage11{
		Multipart: []MIMEMultipart11{{Parts: []MIMEPart11{{}}}},
	}, nil, Location{}, &nonempty)
	if countDiagnostics(nonempty.diagnostics, "WSDL11_MIME_MULTIPART") != 0 {
		t.Fatalf("nonempty multipart diagnostics = %#v", nonempty.diagnostics)
	}
}

func TestRPCValidationHandlesAbsentMessageSides(t *testing.T) {
	t.Parallel()

	for name, operation := range map[string]InterfaceOperation20{
		"no input": {
			Name: "Call", RPCSignatureSet: true,
			Outputs: []InterfaceMessageReference20{{
				Element:             QName{Namespace: "urn:test", Local: "CallResponse"},
				MessageContentModel: MessageContentElement,
			}},
		},
		"no output": {
			Name: "Call", RPCSignatureSet: true,
			Inputs: []InterfaceMessageReference20{{
				Element:             QName{Namespace: "urn:test", Local: "Call"},
				MessageContentModel: MessageContentElement,
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			collector := diagnosticCollector{max: 10}
			validateRPCStyle20(operation, true, &collector)
		})
	}
}

func TestHTTPStatusTokenAndVersionBoundaries(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 20}
	validateHTTPBinding20(Binding20{Faults: []BindingFault20{
		{HTTP: &HTTPFaultBinding20{Code: "99", CodeSet: true}},
		{HTTP: &HTTPFaultBinding20{Code: "100", CodeSet: true}},
		{HTTP: &HTTPFaultBinding20{Code: "599", CodeSet: true}},
		{HTTP: &HTTPFaultBinding20{Code: "600", CodeSet: true}},
		{HTTP: &HTTPFaultBinding20{Code: "invalid", CodeSet: true}},
		{HTTP: &HTTPFaultBinding20{Code: "#any", CodeSet: true}},
	}}, &collector)
	if got := countDiagnostics(collector.diagnostics, "WSDL20_HTTP_STATUS_CODE"); got != 3 {
		t.Fatalf("status-code diagnostics = %d: %#v", got, collector.diagnostics)
	}
	for value, valid := range map[string]bool{
		string(rune(126)): true,
		string(rune(127)): false,
		string(rune(128)): false,
		string(rune(31)):  false,
		string(rune(32)):  false,
		string(rune(33)):  true,
	} {
		if got := isHTTPToken20(value); got != valid {
			t.Errorf("isHTTPToken20(%q) = %t, want %t", value, got, valid)
		}
	}
}

func TestSOAPInFaultModulesAndOptionalBindingFaultLabels(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 10}
	validateSOAPBinding20(Binding20{Operations: []BindingOperation20{{
		InFaults: []BindingFaultReference20{{SOAP: &SOAPFaultReferenceBinding20{
			Modules: []SOAPModule20{{Ref: "relative"}},
		}}},
	}}}, &collector)
	if countDiagnostics(collector.diagnostics, "WSDL20_SOAP_MODULE_IRI") != 1 {
		t.Fatalf("SOAP module diagnostics = %#v", collector.diagnostics)
	}

	collector = diagnosticCollector{max: 10}
	ref := QName{Namespace: "urn:test", Local: "Failure"}
	validateBindingFaults20("Binding", QName{Local: "Call"},
		[]BindingFaultReference20{{Ref: ref}},
		[]InterfaceFaultReference20{{Ref: ref, MessageLabel: "In"}},
		&collector,
	)
	if len(collector.diagnostics) != 0 {
		t.Fatalf("optional fault label diagnostics = %#v", collector.diagnostics)
	}
}

func countDiagnostics(diagnostics Diagnostics, code string) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}

func TestMIMEValidationReportsEveryNestedContractFailure(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 20}
	validateMIMEMessage11(&MIMEMessage11{
		Contents: []MIMEContent11{{Part: "missing", Type: "invalid"}},
		XML:      []MIMEXML11{{Part: "missing"}},
		Multipart: []MIMEMultipart11{
			{},
			{Parts: []MIMEPart11{{
				SOAPBody: &SOAPBody11{Use: "invalid", UseSet: true, Parts: []string{"missing"}},
				Contents: []MIMEContent11{{Part: "missing", Type: "invalid"}},
				XML:      []MIMEXML11{{Part: "missing"}},
			}}},
		},
	}, map[string]struct{}{}, Location{}, &collector)
	codes := make(map[string]int)
	for _, diagnostic := range collector.diagnostics {
		codes[diagnostic.Code]++
	}
	for _, code := range []string{
		"WSDL11_MIME_PART", "WSDL11_MIME_TYPE", "WSDL11_MIME_MULTIPART",
		"WSDL11_SOAP_USE", "WSDL11_SOAP_BODY_PART",
	} {
		if codes[code] == 0 {
			t.Errorf("diagnostics = %#v, missing %s", collector.diagnostics, code)
		}
	}
	before := len(collector.diagnostics)
	validateMIMEMessage11(nil, nil, Location{}, &collector)
	if len(collector.diagnostics) != before {
		t.Fatal("nil MIME message produced a diagnostic")
	}
}

func TestValidationHelpersCoverForeignAndOptionalReferenceBranches(t *testing.T) {
	t.Parallel()

	collector := diagnosticCollector{max: 100}
	required := Extension{
		Name:     QName{Namespace: "urn:unknown", Local: "Required"},
		Required: true, RequiredSet: true,
	}
	validateRequiredExtensions(&Document{version: Version11, definitions11: &Definitions11{
		Imports: []Import11{{Extensibility: Extensibility{Extensions: []Extension{required}}}},
		Bindings: []Binding11{{Operations: []BindingOperation11{{
			Output: &BindingMessage11{Extensibility: Extensibility{Extensions: []Extension{required}}},
		}}}},
	}}, nil, &collector)
	validateRequiredExtensions(&Document{version: Version20, description20: &Description20{
		Imports:  []Import20{{Extensibility: Extensibility{Extensions: []Extension{required}}}},
		Includes: []Include20{{Extensibility: Extensibility{Extensions: []Extension{required}}}},
	}}, nil, &collector)

	foreign := QName{Namespace: "urn:foreign", Local: "Foreign"}
	validateDefinitions11(Definitions11{
		TargetNamespace: "urn:test",
		Services: []Service11{{Ports: []Port11{{
			Binding: foreign, HTTPAddress: &HTTPAddress11{Location: "https://example.test"},
		}}}},
	}, &collector)
	validateOperationStyle11("Port", Operation11{
		Style: OperationStyleOneWay, Input: &OperationMessage11{},
		Faults: []OperationMessage11{{Name: "Failure"}},
	}, &collector)
	if operation := matchingOperation11(BindingOperation11{
		Name: "Call", Output: &BindingMessage11{Name: "Mismatch"},
	}, []Operation11{
		{Name: "Other"},
		{Name: "Call", Style: OperationStyleRequestResponse, Input: &OperationMessage11{}, Output: &OperationMessage11{}},
	}); operation != nil {
		t.Fatalf("matchingOperation11() = %#v", operation)
	}
	validateSOAPBindingMessage11(&BindingMessage11{
		SOAPBody: &SOAPBody11{Parts: []string{"unknown"}},
		SOAPHeaders: []SOAPHeader11{{
			Message:      foreign,
			HeaderFaults: []SOAPHeaderFault11{{Message: foreign}},
		}},
	}, &OperationMessage11{Message: QName{Namespace: "urn:test", Local: "Missing"}},
		"urn:test", nil, &collector)
	validateBindingProperties11(Binding11{Operations: []BindingOperation11{{
		SOAP: &SOAPOperation11{Style: "invalid", StyleSet: true},
	}}}, &collector)
	validateSOAPHeaderReference11(foreign, "part", Location{}, "urn:test", nil, &collector)
	known := QName{Namespace: "urn:test", Local: "Known"}
	validateSOAPHeaderReference11(known, "part", Location{}, "urn:test", map[QName]map[string]struct{}{
		known: {"part": {}},
	}, &collector)

	validateDescription20(Description20{
		TargetNamespace: "urn:test",
		Interfaces: []Interface20{{
			Name: "API",
			Operations: []InterfaceOperation20{{
				Name: "Call", InFaults: []InterfaceFaultReference20{{Ref: foreign}},
			}},
		}},
		Bindings: []Binding20{{
			Name: "Binding", Interface: foreign,
			HTTP:       &HTTPBinding20{MethodDefault: "bad method", MethodDefaultSet: true},
			Faults:     []BindingFault20{{Ref: foreign}},
			Operations: []BindingOperation20{{Ref: foreign}},
		}},
		Services: []Service20{{
			Name: "Service", Interface: foreign,
			Endpoints: []Endpoint20{{Binding: foreign}},
		}},
	}, &collector)
	validateElementOperationStyle20(StyleIRI, InterfaceOperation20{}, &collector)
	validateFaultReference20("API", "Call", InterfaceFaultReference20{Ref: foreign},
		"urn:test", nil, &collector)
	validateInterfaceReference20("owner", foreign, Location{}, "urn:test", nil, &collector)
	if len(collector.diagnostics) == 0 {
		t.Fatal("validation helpers produced no diagnostics")
	}
}
