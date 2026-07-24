package wsdl

const (
	// NamespaceWSDL20 is the namespace of WSDL 2.0 core elements.
	NamespaceWSDL20 = "http://www.w3.org/ns/wsdl"
	// NamespaceWSDL20SOAP is the WSDL 2.0 SOAP binding namespace.
	NamespaceWSDL20SOAP = "http://www.w3.org/ns/wsdl/soap"
	// NamespaceWSDL20HTTP is the WSDL 2.0 HTTP binding namespace.
	NamespaceWSDL20HTTP = "http://www.w3.org/ns/wsdl/http"
	// NamespaceWSDL20Extensions contains the WSDL 2.0 predefined extensions.
	NamespaceWSDL20Extensions = "http://www.w3.org/ns/wsdl-extensions"
	// NamespaceWSDL20RPC contains the WSDL 2.0 RPC style extension.
	NamespaceWSDL20RPC = "http://www.w3.org/ns/wsdl/rpc"
)

// MessageContentModel identifies how a WSDL 2.0 message payload is described.
type MessageContentModel string

const (
	MessageContentAny     MessageContentModel = "#any"
	MessageContentNone    MessageContentModel = "#none"
	MessageContentOther   MessageContentModel = "#other"
	MessageContentElement MessageContentModel = "#element"
)

// RPCDirection identifies one WSDL 2.0 RPC signature parameter direction.
type RPCDirection string

const (
	RPCDirectionIn     RPCDirection = "#in"
	RPCDirectionOut    RPCDirection = "#out"
	RPCDirectionInOut  RPCDirection = "#inout"
	RPCDirectionReturn RPCDirection = "#return"
)

// RPCSignatureParameter20 is one qualified parameter and direction pair.
type RPCSignatureParameter20 struct {
	Name      QName
	Direction RPCDirection
}

// MessageExchangePattern identifies the message flow of a WSDL 2.0 operation.
type MessageExchangePattern string

const (
	MEPInOnly        MessageExchangePattern = "http://www.w3.org/ns/wsdl/in-only"
	MEPRobustInOnly  MessageExchangePattern = "http://www.w3.org/ns/wsdl/robust-in-only"
	MEPInOut         MessageExchangePattern = "http://www.w3.org/ns/wsdl/in-out"
	MEPInOptionalOut MessageExchangePattern = "http://www.w3.org/ns/wsdl/in-opt-out"
	MEPOutOnly       MessageExchangePattern = "http://www.w3.org/ns/wsdl/out-only"
	MEPRobustOutOnly MessageExchangePattern = "http://www.w3.org/ns/wsdl/robust-out-only"
	MEPOutIn         MessageExchangePattern = "http://www.w3.org/ns/wsdl/out-in"
	MEPOutOptionalIn MessageExchangePattern = "http://www.w3.org/ns/wsdl/out-opt-in"
)

const (
	// StyleIRI identifies the WSDL 2.0 IRI operation style.
	StyleIRI = "http://www.w3.org/ns/wsdl/style/iri"
	// StyleMultipart identifies the WSDL 2.0 multipart operation style.
	StyleMultipart = "http://www.w3.org/ns/wsdl/style/multipart"
	// StyleRPC identifies the WSDL 2.0 RPC operation style.
	StyleRPC = "http://www.w3.org/ns/wsdl/style/rpc"
)

// Import20 references a WSDL 2.0 description in another namespace.
type Import20 struct {
	Extensibility
	Namespace     string
	Location      string
	URI           string
	Documentation *Documentation
	Source        Location
}

// Include20 references a WSDL 2.0 description in the same namespace.
type Include20 struct {
	Extensibility
	Location      string
	URI           string
	Documentation *Documentation
	Source        Location
}

// InterfaceFault20 declares a fault available to interface operations.
type InterfaceFault20 struct {
	Extensibility
	Name                   string
	Element                QName
	MessageContentModel    MessageContentModel
	MessageContentModelSet bool
	Documentation          *Documentation
	Location               Location
}

// InterfaceMessageReference20 describes one operation message.
type InterfaceMessageReference20 struct {
	Extensibility
	MessageLabel           string
	Element                QName
	MessageContentModel    MessageContentModel
	MessageContentModelSet bool
	Documentation          *Documentation
	Location               Location
}

// InterfaceFaultReference20 connects an operation to an interface fault.
type InterfaceFaultReference20 struct {
	Extensibility
	Ref           QName
	MessageLabel  string
	Documentation *Documentation
	Location      Location
}

// InterfaceOperation20 describes one abstract WSDL 2.0 operation.
type InterfaceOperation20 struct {
	Extensibility
	Name            string
	Pattern         MessageExchangePattern
	Style           []string
	Safe            bool
	SafeSet         bool
	RPCSignature    []RPCSignatureParameter20
	RPCSignatureSet bool
	Documentation   *Documentation
	Inputs          []InterfaceMessageReference20
	Outputs         []InterfaceMessageReference20
	// Input and Output retain the first message for source compatibility.
	Input     *InterfaceMessageReference20
	Output    *InterfaceMessageReference20
	InFaults  []InterfaceFaultReference20
	OutFaults []InterfaceFaultReference20
	Location  Location
}

// Interface20 is a named WSDL 2.0 abstract interface.
type Interface20 struct {
	Extensibility
	Name          string
	Extends       []QName
	StyleDefault  []string
	Documentation *Documentation
	Faults        []InterfaceFault20
	Operations    []InterfaceOperation20
	Location      Location
}

// SOAPModule20 declares a SOAP module required by a binding component.
type SOAPModule20 struct {
	Extensibility
	Ref           string
	Required      bool
	RequiredSet   bool
	Documentation *Documentation
	Location      Location
}

// SOAPHeader20 declares an element serialized as a SOAP header block.
type SOAPHeader20 struct {
	Extensibility
	Element           QName
	MustUnderstand    bool
	MustUnderstandSet bool
	Required          bool
	RequiredSet       bool
	Documentation     *Documentation
	Location          Location
}

// SOAPBinding20 contains SOAP properties attached to a binding.
type SOAPBinding20 struct {
	Version       string
	VersionSet    bool
	Protocol      string
	ProtocolSet   bool
	MEPDefault    string
	MEPDefaultSet bool
	Modules       []SOAPModule20
}

// SOAPFaultBinding20 contains SOAP properties attached to a binding fault.
type SOAPFaultBinding20 struct {
	Code        QName
	CodeAny     bool
	CodeSet     bool
	Subcodes    []QName
	SubcodesAny bool
	SubcodesSet bool
	Modules     []SOAPModule20
	Headers     []SOAPHeader20
}

// SOAPOperationBinding20 contains SOAP properties attached to an operation.
type SOAPOperationBinding20 struct {
	MEP       string
	MEPSet    bool
	Action    string
	ActionSet bool
	Modules   []SOAPModule20
}

// SOAPMessageBinding20 contains SOAP properties attached to a message reference.
type SOAPMessageBinding20 struct {
	Modules []SOAPModule20
	Headers []SOAPHeader20
}

// SOAPFaultReferenceBinding20 contains SOAP modules attached to a fault reference.
type SOAPFaultReferenceBinding20 struct {
	Modules []SOAPModule20
}

// HTTPHeader20 declares one HTTP header field used by a binding message.
type HTTPHeader20 struct {
	Extensibility
	Name          string
	Type          QName
	Required      bool
	RequiredSet   bool
	Documentation *Documentation
	Location      Location
}

// HTTPBinding20 contains HTTP properties attached to a binding.
type HTTPBinding20 struct {
	MethodDefault                     string
	MethodDefaultSet                  bool
	Version                           string
	VersionSet                        bool
	QueryParameterSeparatorDefault    string
	QueryParameterSeparatorDefaultSet bool
	ContentEncodingDefault            string
	ContentEncodingDefaultSet         bool
	DefaultTransferCoding             string
	DefaultTransferCodingSet          bool
	Cookies                           bool
	CookiesSet                        bool
}

// HTTPFaultBinding20 contains HTTP properties attached to a binding fault.
type HTTPFaultBinding20 struct {
	Code               string
	CodeSet            bool
	ContentEncoding    string
	ContentEncodingSet bool
	TransferCoding     string
	TransferCodingSet  bool
	Headers            []HTTPHeader20
}

// HTTPOperationBinding20 contains HTTP properties attached to an operation.
type HTTPOperationBinding20 struct {
	Location                   string
	LocationSet                bool
	Method                     string
	MethodSet                  bool
	InputSerialization         string
	InputSerializationSet      bool
	OutputSerialization        string
	OutputSerializationSet     bool
	FaultSerialization         string
	FaultSerializationSet      bool
	QueryParameterSeparator    string
	QueryParameterSeparatorSet bool
	ContentEncodingDefault     string
	ContentEncodingDefaultSet  bool
	DefaultTransferCoding      string
	DefaultTransferCodingSet   bool
	IgnoreUncited              bool
	IgnoreUncitedSet           bool
}

// HTTPMessageBinding20 contains HTTP properties attached to a message reference.
type HTTPMessageBinding20 struct {
	ContentEncoding    string
	ContentEncodingSet bool
	TransferCoding     string
	TransferCodingSet  bool
	Headers            []HTTPHeader20
}

// HTTPFaultReferenceBinding20 contains HTTP transfer properties on a fault reference.
type HTTPFaultReferenceBinding20 struct {
	TransferCoding    string
	TransferCodingSet bool
}

// HTTPEndpoint20 contains HTTP authentication properties on an endpoint.
type HTTPEndpoint20 struct {
	AuthenticationScheme    string
	AuthenticationSchemeSet bool
	AuthenticationRealm     string
	AuthenticationRealmSet  bool
}

// BindingFault20 binds one WSDL 2.0 interface fault.
type BindingFault20 struct {
	Extensibility
	Ref           QName
	Documentation *Documentation
	SOAP          *SOAPFaultBinding20
	HTTP          *HTTPFaultBinding20
	Location      Location
}

// BindingMessageReference20 binds one message in an interface operation.
type BindingMessageReference20 struct {
	Extensibility
	MessageLabel  string
	Documentation *Documentation
	SOAP          *SOAPMessageBinding20
	HTTP          *HTTPMessageBinding20
	Location      Location
}

// BindingFaultReference20 binds one fault in an interface operation.
type BindingFaultReference20 struct {
	Extensibility
	Ref           QName
	MessageLabel  string
	Documentation *Documentation
	SOAP          *SOAPFaultReferenceBinding20
	HTTP          *HTTPFaultReferenceBinding20
	Location      Location
}

// BindingOperation20 binds one WSDL 2.0 interface operation.
type BindingOperation20 struct {
	Extensibility
	Ref           QName
	Documentation *Documentation
	SOAP          *SOAPOperationBinding20
	HTTP          *HTTPOperationBinding20
	Inputs        []BindingMessageReference20
	Outputs       []BindingMessageReference20
	InFaults      []BindingFaultReference20
	OutFaults     []BindingFaultReference20
	Location      Location
}

// Binding20 binds an interface to a concrete protocol.
type Binding20 struct {
	Extensibility
	Name          string
	Interface     QName
	Type          string
	Documentation *Documentation
	SOAP          *SOAPBinding20
	HTTP          *HTTPBinding20
	Faults        []BindingFault20
	Operations    []BindingOperation20
	Location      Location
}

// Endpoint20 is one concrete WSDL 2.0 service endpoint.
type Endpoint20 struct {
	Extensibility
	Name          string
	Binding       QName
	Address       string
	Documentation *Documentation
	HTTP          *HTTPEndpoint20
	Location      Location
}

// Service20 groups endpoints implementing one interface.
type Service20 struct {
	Extensibility
	Name          string
	Interface     QName
	Documentation *Documentation
	Endpoints     []Endpoint20
	Location      Location
}

func interfaceInputs20(value InterfaceOperation20) []InterfaceMessageReference20 {
	if len(value.Inputs) > 0 {
		return value.Inputs
	}
	if value.Input != nil {
		return []InterfaceMessageReference20{*value.Input}
	}
	return nil
}

func interfaceOutputs20(value InterfaceOperation20) []InterfaceMessageReference20 {
	if len(value.Outputs) > 0 {
		return value.Outputs
	}
	if value.Output != nil {
		return []InterfaceMessageReference20{*value.Output}
	}
	return nil
}
