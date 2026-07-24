package wsdl

const (
	// NamespaceWSDL11 is the namespace of WSDL 1.1 core elements.
	NamespaceWSDL11 = "http://schemas.xmlsoap.org/wsdl/"
	// NamespaceXMLSchema is the namespace of XML Schema 1.0 and 1.1.
	NamespaceXMLSchema = "http://www.w3.org/2001/XMLSchema"
	// NamespaceSOAP11Binding is the WSDL 1.1 SOAP 1.1 extension namespace.
	NamespaceSOAP11Binding = "http://schemas.xmlsoap.org/wsdl/soap/"
	// NamespaceSOAP12Binding is the WSDL 1.1 SOAP 1.2 extension namespace.
	NamespaceSOAP12Binding = "http://schemas.xmlsoap.org/wsdl/soap12/"
	// NamespaceHTTPBinding is the WSDL 1.1 HTTP extension namespace.
	NamespaceHTTPBinding = "http://schemas.xmlsoap.org/wsdl/http/"
	// NamespaceMIMEBinding is the WSDL 1.1 MIME extension namespace.
	NamespaceMIMEBinding = "http://schemas.xmlsoap.org/wsdl/mime/"
)

// QName is an expanded XML qualified name.
type QName struct {
	Namespace string
	Local     string
}

// ExtensionAttribute preserves an attribute outside the owning WSDL
// vocabulary as an expanded name and lexical value.
type ExtensionAttribute struct {
	Name     QName
	Value    string
	Location Location
}

// Extension preserves one foreign-namespace extension element.
type Extension struct {
	Name        QName
	Attributes  []ExtensionAttribute
	Required    bool
	RequiredSet bool
	XML         []byte
	Location    Location
}

// Extensibility preserves foreign attributes and child elements attached to
// a WSDL component.
type Extensibility struct {
	Extensions          []Extension
	ExtensionAttributes []ExtensionAttribute
}

// Location identifies an XML source position.
type Location struct {
	SystemID string
	Line     int
	Column   int
	Offset   int64
}

// Documentation preserves a WSDL documentation element.
type Documentation struct {
	Language string
	Content  string
	Markup   string
	Location Location
}

// Import11 references a WSDL 1.1 document for another namespace. URI is the
// resolved identity; parsing never loads it.
type Import11 struct {
	Extensibility
	Namespace     string
	Location      string
	URI           string
	Documentation *Documentation
	Source        Location
}

// Part11 is one abstract WSDL 1.1 message part.
type Part11 struct {
	Extensibility
	Name     string
	Element  QName
	Type     QName
	Location Location
}

// Message11 is a named WSDL 1.1 abstract message.
type Message11 struct {
	Extensibility
	Name          string
	Documentation *Documentation
	Parts         []Part11
	Location      Location
}

// OperationMessage11 references a message used by an abstract operation.
type OperationMessage11 struct {
	Extensibility
	Name          string
	Message       QName
	Documentation *Documentation
	Location      Location
}

// OperationStyle11 identifies the message ordering of a WSDL 1.1 operation.
type OperationStyle11 string

const (
	OperationStyleOneWay          OperationStyle11 = "one-way"
	OperationStyleRequestResponse OperationStyle11 = "request-response"
	OperationStyleSolicitResponse OperationStyle11 = "solicit-response"
	OperationStyleNotification    OperationStyle11 = "notification"
)

// Operation11 is a WSDL 1.1 abstract operation.
type Operation11 struct {
	Extensibility
	Name           string
	Style          OperationStyle11
	ParameterOrder []string
	Documentation  *Documentation
	Input          *OperationMessage11
	Output         *OperationMessage11
	Faults         []OperationMessage11
	Location       Location
}

// PortType11 is a named WSDL 1.1 abstract interface.
type PortType11 struct {
	Extensibility
	Name          string
	Documentation *Documentation
	Operations    []Operation11
	Location      Location
}

// SOAPStyle is the SOAP RPC or document binding style.
type SOAPStyle string

const (
	SOAPStyleDocument SOAPStyle = "document"
	SOAPStyleRPC      SOAPStyle = "rpc"
)

// SOAPUse is the SOAP encoded or literal message use.
type SOAPUse string

const (
	SOAPUseLiteral SOAPUse = "literal"
	SOAPUseEncoded SOAPUse = "encoded"
)

// SOAPBinding11 configures a WSDL 1.1 SOAP binding.
type SOAPBinding11 struct {
	Version      Version
	Style        SOAPStyle
	StyleSet     bool
	Transport    string
	TransportSet bool
	Location     Location
}

// SOAPOperation11 configures a bound SOAP operation.
type SOAPOperation11 struct {
	Version           Version
	Action            string
	ActionSet         bool
	ActionRequired    bool
	ActionRequiredSet bool
	Style             SOAPStyle
	StyleSet          bool
	Location          Location
}

// SOAPBody11 configures a SOAP message body.
type SOAPBody11 struct {
	Version          Version
	Use              SOAPUse
	UseSet           bool
	Namespace        string
	NamespaceSet     bool
	EncodingStyle    []string
	EncodingStyleSet bool
	Parts            []string
	PartsSet         bool
	Location         Location
}

// SOAPHeaderFault11 binds a fault related to one SOAP header block.
type SOAPHeaderFault11 struct {
	Version          Version
	Message          QName
	Part             string
	Use              SOAPUse
	UseSet           bool
	Namespace        string
	NamespaceSet     bool
	EncodingStyle    []string
	EncodingStyleSet bool
	Location         Location
}

// SOAPHeader11 binds one message part to a SOAP header block.
type SOAPHeader11 struct {
	Version          Version
	Message          QName
	Part             string
	Use              SOAPUse
	UseSet           bool
	Namespace        string
	NamespaceSet     bool
	EncodingStyle    []string
	EncodingStyleSet bool
	HeaderFaults     []SOAPHeaderFault11
	Location         Location
}

// SOAPFault11 binds one WSDL fault to a SOAP fault.
type SOAPFault11 struct {
	Version          Version
	Name             string
	Use              SOAPUse
	UseSet           bool
	Namespace        string
	NamespaceSet     bool
	EncodingStyle    []string
	EncodingStyleSet bool
	Location         Location
}

// HTTPBinding11 configures the verb for a WSDL 1.1 HTTP binding.
type HTTPBinding11 struct {
	Verb     string
	Location Location
}

// HTTPOperation11 configures the relative URI template of an HTTP operation.
type HTTPOperation11 struct {
	Location string
	Source   Location
}

// HTTPMessageMode identifies WSDL 1.1 HTTP input serialization.
type HTTPMessageMode string

const (
	HTTPURLEncoded     HTTPMessageMode = "urlEncoded"
	HTTPURLReplacement HTTPMessageMode = "urlReplacement"
)

// HTTPMessage11 configures WSDL 1.1 HTTP input serialization.
type HTTPMessage11 struct {
	Mode     HTTPMessageMode
	Location Location
}

// MIMEContent11 binds one message part to an Internet media type.
type MIMEContent11 struct {
	Part     string
	Type     string
	Location Location
}

// MIMEXML11 binds one message part as XML.
type MIMEXML11 struct {
	Part     string
	Location Location
}

// MIMEPart11 is one part of a multipart/related binding.
type MIMEPart11 struct {
	Contents []MIMEContent11
	XML      []MIMEXML11
	SOAPBody *SOAPBody11
	Location Location
}

// MIMEMultipart11 describes a multipart/related message.
type MIMEMultipart11 struct {
	Parts    []MIMEPart11
	Location Location
}

// MIMEMessage11 contains MIME extensions applied to a bound message.
type MIMEMessage11 struct {
	Contents  []MIMEContent11
	XML       []MIMEXML11
	Multipart []MIMEMultipart11
}

// BindingMessage11 applies concrete extensions to an operation message.
type BindingMessage11 struct {
	Extensibility
	Name          string
	Documentation *Documentation
	SOAPBody      *SOAPBody11
	SOAPHeaders   []SOAPHeader11
	SOAPFault     *SOAPFault11
	HTTP          *HTTPMessage11
	MIME          *MIMEMessage11
	Location      Location
}

// BindingOperation11 binds one abstract WSDL 1.1 operation.
type BindingOperation11 struct {
	Extensibility
	Name          string
	Documentation *Documentation
	SOAP          *SOAPOperation11
	HTTP          *HTTPOperation11
	Input         *BindingMessage11
	Output        *BindingMessage11
	Faults        []BindingMessage11
	Location      Location
}

// Binding11 binds an abstract port type to a concrete protocol and format.
type Binding11 struct {
	Extensibility
	Name          string
	Type          QName
	Documentation *Documentation
	SOAP          *SOAPBinding11
	HTTP          *HTTPBinding11
	Operations    []BindingOperation11
	Location      Location
}

// SOAPAddress11 is the endpoint address supplied by a SOAP binding.
type SOAPAddress11 struct {
	Version  Version
	Location string
	Source   Location
}

// HTTPAddress11 is the endpoint address supplied by an HTTP binding.
type HTTPAddress11 struct {
	Location string
	Source   Location
}

// Port11 is one named endpoint in a WSDL 1.1 service.
type Port11 struct {
	Extensibility
	Name          string
	Binding       QName
	Documentation *Documentation
	SOAPAddress   *SOAPAddress11
	HTTPAddress   *HTTPAddress11
	Location      Location
}

// Service11 groups related WSDL 1.1 endpoints.
type Service11 struct {
	Extensibility
	Name          string
	Documentation *Documentation
	Ports         []Port11
	Location      Location
}
