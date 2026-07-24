package wsdl

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/faustbrian/golib/pkg/wire/xmlwire"
)

const defaultMaxOutputBytes int64 = 8 << 20

// MarshalOptions controls deterministic WSDL serialization.
type MarshalOptions struct {
	MaxBytes      int64
	Indent        string
	IncludeHeader bool
}

// Marshal serializes a WSDL document deterministically without external I/O.
func Marshal(document *Document, options MarshalOptions) ([]byte, error) {
	if document == nil {
		return nil, errors.New("wsdl: document is nil")
	}
	if options.MaxBytes < 0 {
		return nil, errors.New("wsdl: maximum output bytes must not be negative")
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxOutputBytes
	}
	value, err := newMarshalValue(document)
	if err != nil {
		return nil, err
	}
	output, err := xmlwire.Encode(value, xmlwire.EncodeOptions{
		MaxBytes: maxBytes, Indent: options.Indent, IncludeHeader: options.IncludeHeader,
	})
	if err != nil {
		if errors.Is(err, xmlwire.ErrPayloadTooLarge) {
			return nil, fmt.Errorf("%w: output bytes exceed %d", ErrLimitExceeded, maxBytes)
		}
		return nil, fmt.Errorf("wsdl: marshal: %w", err)
	}
	return output, nil
}

type marshalValue struct {
	document *Document
	prefixes map[string]string
}

func newMarshalValue(document *Document) (marshalValue, error) {
	prefixes := map[string]string{
		NamespaceXMLSchema: "xs",
	}
	unknown := make(map[string]struct{})
	preferred := make(map[string][]string)
	if definitions, ok := document.Definitions11(); ok {
		collectProtocolPrefixes11(definitions, prefixes)
		if definitions.TargetNamespace != "" {
			prefixes[definitions.TargetNamespace] = "tns"
		}
		collectDefinition11Namespaces(definitions, unknown)
		if definitions.Types != nil {
			for _, schema := range definitions.Types.Schemas {
				if schema != nil {
					collectPreferredPrefixes(schema.Namespaces, preferred)
				}
			}
		}
		preferTargetPrefix(prefixes, preferred, definitions.TargetNamespace)
	} else if description, ok := document.Description20(); ok {
		prefixes[NamespaceWSDL20SOAP] = "wsoap"
		prefixes[NamespaceWSDL20HTTP] = "whttp"
		prefixes[NamespaceWSDL20Extensions] = "wsdlx"
		prefixes[NamespaceWSDL20RPC] = "wrpc"
		if description.TargetNamespace != "" {
			prefixes[description.TargetNamespace] = "tns"
		}
		collectDescription20Namespaces(description, unknown)
		if description.Types != nil {
			for _, schema := range description.Types.Schemas {
				if schema != nil {
					collectPreferredPrefixes(schema.Namespaces, preferred)
				}
			}
		}
		preferTargetPrefix(prefixes, preferred, description.TargetNamespace)
	} else {
		return marshalValue{}, errors.New("wsdl: document has no version model")
	}
	values := make([]string, 0, len(unknown))
	for namespace := range unknown {
		if namespace != "" {
			if _, exists := prefixes[namespace]; !exists {
				values = append(values, namespace)
			}
		}
	}
	sort.Strings(values)
	usedPrefixes := make(map[string]struct{}, len(prefixes))
	for _, prefix := range prefixes {
		usedPrefixes[prefix] = struct{}{}
	}
	for _, namespace := range values {
		candidates := preferred[namespace]
		sort.Strings(candidates)
		for _, prefix := range candidates {
			if prefix == "" || prefix == "xml" {
				continue
			}
			if _, exists := usedPrefixes[prefix]; exists {
				continue
			}
			prefixes[namespace] = prefix
			usedPrefixes[prefix] = struct{}{}
			break
		}
	}
	nextPrefix := 1
	for _, namespace := range values {
		if _, exists := prefixes[namespace]; exists {
			continue
		}
		for {
			prefix := fmt.Sprintf("ns%d", nextPrefix)
			nextPrefix++
			if _, exists := usedPrefixes[prefix]; exists {
				continue
			}
			prefixes[namespace] = prefix
			usedPrefixes[prefix] = struct{}{}
			break
		}
	}
	return marshalValue{document: document, prefixes: prefixes}, nil
}

func collectPreferredPrefixes(
	namespaces map[string]string,
	result map[string][]string,
) {
	for prefix, namespace := range namespaces {
		if prefix != "" && namespace != "" {
			result[namespace] = append(result[namespace], prefix)
		}
	}
}

func preferTargetPrefix(
	prefixes map[string]string,
	preferred map[string][]string,
	targetNamespace string,
) {
	if targetNamespace == "" {
		return
	}
	candidates := append([]string(nil), preferred[targetNamespace]...)
	sort.Strings(candidates)
	for _, candidate := range candidates {
		if candidate == "" || candidate == "xml" {
			continue
		}
		available := true
		for namespace, prefix := range prefixes {
			if namespace != targetNamespace && prefix == candidate {
				available = false
				break
			}
		}
		if available {
			prefixes[targetNamespace] = candidate
			return
		}
	}
}

func collectProtocolPrefixes11(definitions Definitions11, prefixes map[string]string) {
	addSOAP := func(version Version) {
		if version == Version12 {
			prefixes[NamespaceSOAP12Binding] = "soap12"
			return
		}
		prefixes[NamespaceSOAP11Binding] = "soap"
	}
	collectMessage := func(message *BindingMessage11) {
		if message == nil {
			return
		}
		if message.SOAPBody != nil {
			addSOAP(message.SOAPBody.Version)
		}
		for _, header := range message.SOAPHeaders {
			addSOAP(header.Version)
			for _, fault := range header.HeaderFaults {
				addSOAP(fault.Version)
			}
		}
		if message.SOAPFault != nil {
			addSOAP(message.SOAPFault.Version)
		}
		if message.HTTP != nil {
			prefixes[NamespaceHTTPBinding] = "http"
		}
		if message.MIME != nil {
			prefixes[NamespaceMIMEBinding] = "mime"
			for _, multipart := range message.MIME.Multipart {
				for _, part := range multipart.Parts {
					if part.SOAPBody != nil {
						addSOAP(part.SOAPBody.Version)
					}
				}
			}
		}
	}
	for _, binding := range definitions.Bindings {
		if binding.SOAP != nil {
			addSOAP(binding.SOAP.Version)
		}
		if binding.HTTP != nil {
			prefixes[NamespaceHTTPBinding] = "http"
		}
		for _, operation := range binding.Operations {
			if operation.SOAP != nil {
				addSOAP(operation.SOAP.Version)
			}
			if operation.HTTP != nil {
				prefixes[NamespaceHTTPBinding] = "http"
			}
			collectMessage(operation.Input)
			collectMessage(operation.Output)
			for index := range operation.Faults {
				collectMessage(&operation.Faults[index])
			}
		}
	}
	for _, service := range definitions.Services {
		for _, port := range service.Ports {
			if port.SOAPAddress != nil {
				addSOAP(port.SOAPAddress.Version)
			}
			if port.HTTPAddress != nil {
				prefixes[NamespaceHTTPBinding] = "http"
			}
		}
	}
}

func (m marshalValue) MarshalXML(encoder *xml.Encoder, _ xml.StartElement) error {
	if definitions, ok := m.document.Definitions11(); ok {
		return m.definitions11(encoder, definitions)
	}
	description, ok := m.document.Description20()
	if !ok {
		return errors.New("wsdl: document has no version model")
	}
	return m.description20(encoder, description)
}

type tokenEncoder interface {
	EncodeToken(xml.Token) error
}

func (m marshalValue) qname(name QName) (string, error) {
	if name.Local == "" {
		return "", nil
	}
	if name.Namespace == "" {
		return name.Local, nil
	}
	prefix, exists := m.prefixes[name.Namespace]
	if !exists || prefix == "" {
		return "", fmt.Errorf("wsdl: no prefix for QName {%s}%s", name.Namespace, name.Local)
	}
	return prefix + ":" + name.Local, nil
}

func namespaceAttributes(prefixes map[string]string) []xml.Attr {
	type binding struct {
		prefix    string
		namespace string
	}
	bindings := make([]binding, 0, len(prefixes))
	for namespace, prefix := range prefixes {
		if namespace == "" || prefix == "" {
			continue
		}
		bindings = append(bindings, binding{prefix: prefix, namespace: namespace})
	}
	sort.Slice(bindings, func(left, right int) bool {
		return cmp.Compare(bindings[left].prefix, bindings[right].prefix) == -1
	})
	attributes := make([]xml.Attr, 0, len(bindings))
	for _, binding := range bindings {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "xmlns:" + binding.prefix}, Value: binding.namespace,
		})
	}
	return attributes
}

func (m marshalValue) extensionAttributes(
	attributes []xml.Attr,
	value Extensibility,
) ([]xml.Attr, error) {
	extensions := append([]ExtensionAttribute(nil), value.ExtensionAttributes...)
	sort.Slice(extensions, func(left, right int) bool {
		if extensions[left].Name.Namespace != extensions[right].Name.Namespace {
			return cmp.Compare(
				extensions[left].Name.Namespace,
				extensions[right].Name.Namespace,
			) == -1
		}
		return cmp.Compare(extensions[left].Name.Local, extensions[right].Name.Local) == -1
	})
	for _, attribute := range extensions {
		name, err := m.qname(attribute.Name)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: name}, Value: attribute.Value,
		})
	}
	return attributes, nil
}

func (m marshalValue) qualifiedAttribute(
	attributes []xml.Attr,
	name QName,
	value string,
) ([]xml.Attr, error) {
	lexical, err := m.qname(name)
	if err != nil {
		return nil, err
	}
	return append(attributes, xml.Attr{
		Name: xml.Name{Local: lexical}, Value: value,
	}), nil
}

func encodeElement(
	encoder tokenEncoder,
	start xml.StartElement,
	content func() error,
) error {
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	if content != nil {
		if err := content(); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(start.End())
}

func encodeRawXML(encoder tokenEncoder, payload []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, directive := token.(xml.Directive); directive {
			return ErrDTDForbidden
		}
		if start, ok := token.(xml.StartElement); ok {
			attributes := make([]xml.Attr, 0, len(start.Attr))
			for _, attribute := range start.Attr {
				if attribute.Name.Space == "" {
					if attribute.Name.Local == "xmlns" {
						continue
					}
				}
				if attribute.Name.Space == "xmlns" {
					attribute.Name = xml.Name{Local: "xmlns:" + attribute.Name.Local}
				}
				attributes = append(attributes, attribute)
			}
			start.Attr = attributes
			token = start
		}
		if err := encoder.EncodeToken(token); err != nil {
			return err
		}
	}
}

func collectDefinition11Namespaces(definitions Definitions11, result map[string]struct{}) {
	add := func(name QName) {
		if name.Namespace != "" {
			result[name.Namespace] = struct{}{}
		}
	}
	collectExtensibilityNamespaces(Extensibility{
		Extensions:          definitions.Extensions,
		ExtensionAttributes: definitions.ExtensionAttributes,
	}, result)
	for _, importValue := range definitions.Imports {
		collectExtensibilityNamespaces(importValue.Extensibility, result)
	}
	if definitions.Types != nil {
		collectExtensibilityNamespaces(definitions.Types.Extensibility, result)
	}
	for _, message := range definitions.Messages {
		collectExtensibilityNamespaces(message.Extensibility, result)
		for _, part := range message.Parts {
			collectExtensibilityNamespaces(part.Extensibility, result)
			add(part.Element)
			add(part.Type)
		}
	}
	for _, portType := range definitions.PortTypes {
		collectExtensibilityNamespaces(portType.Extensibility, result)
		for _, operation := range portType.Operations {
			collectExtensibilityNamespaces(operation.Extensibility, result)
			if operation.Input != nil {
				collectExtensibilityNamespaces(operation.Input.Extensibility, result)
				add(operation.Input.Message)
			}
			if operation.Output != nil {
				collectExtensibilityNamespaces(operation.Output.Extensibility, result)
				add(operation.Output.Message)
			}
			for _, fault := range operation.Faults {
				collectExtensibilityNamespaces(fault.Extensibility, result)
				add(fault.Message)
			}
		}
	}
	for _, binding := range definitions.Bindings {
		collectExtensibilityNamespaces(binding.Extensibility, result)
		add(binding.Type)
		collectBindingMessage := func(message *BindingMessage11) {
			if message == nil {
				return
			}
			collectExtensibilityNamespaces(message.Extensibility, result)
			for _, header := range message.SOAPHeaders {
				add(header.Message)
				for _, fault := range header.HeaderFaults {
					add(fault.Message)
				}
			}
		}
		for _, operation := range binding.Operations {
			collectExtensibilityNamespaces(operation.Extensibility, result)
			collectBindingMessage(operation.Input)
			collectBindingMessage(operation.Output)
			for index := range operation.Faults {
				collectBindingMessage(&operation.Faults[index])
			}
		}
	}
	for _, service := range definitions.Services {
		collectExtensibilityNamespaces(service.Extensibility, result)
		for _, port := range service.Ports {
			collectExtensibilityNamespaces(port.Extensibility, result)
			add(port.Binding)
		}
	}
}

func collectDescription20Namespaces(description Description20, result map[string]struct{}) {
	add := func(name QName) {
		if name.Namespace != "" {
			result[name.Namespace] = struct{}{}
		}
	}
	collectExtensibilityNamespaces(description.Extensibility, result)
	for _, importValue := range description.Imports {
		collectExtensibilityNamespaces(importValue.Extensibility, result)
	}
	for _, include := range description.Includes {
		collectExtensibilityNamespaces(include.Extensibility, result)
	}
	if description.Types != nil {
		collectExtensibilityNamespaces(description.Types.Extensibility, result)
	}
	for _, interfaceValue := range description.Interfaces {
		collectExtensibilityNamespaces(interfaceValue.Extensibility, result)
		for _, parent := range interfaceValue.Extends {
			add(parent)
		}
		for _, fault := range interfaceValue.Faults {
			collectExtensibilityNamespaces(fault.Extensibility, result)
			add(fault.Element)
		}
		for _, operation := range interfaceValue.Operations {
			collectExtensibilityNamespaces(operation.Extensibility, result)
			for _, parameter := range operation.RPCSignature {
				add(parameter.Name)
			}
			for _, message := range interfaceInputs20(operation) {
				collectExtensibilityNamespaces(message.Extensibility, result)
				add(message.Element)
			}
			for _, message := range interfaceOutputs20(operation) {
				collectExtensibilityNamespaces(message.Extensibility, result)
				add(message.Element)
			}
			for _, fault := range operation.InFaults {
				collectExtensibilityNamespaces(fault.Extensibility, result)
				add(fault.Ref)
			}
			for _, fault := range operation.OutFaults {
				collectExtensibilityNamespaces(fault.Extensibility, result)
				add(fault.Ref)
			}
		}
	}
	for _, binding := range description.Bindings {
		collectExtensibilityNamespaces(binding.Extensibility, result)
		add(binding.Interface)
		if binding.SOAP != nil {
			collectSOAPModules20Namespaces(binding.SOAP.Modules, result)
		}
		for _, fault := range binding.Faults {
			collectExtensibilityNamespaces(fault.Extensibility, result)
			add(fault.Ref)
			if fault.SOAP != nil {
				if fault.SOAP.CodeSet && !fault.SOAP.CodeAny {
					add(fault.SOAP.Code)
				}
				for _, subcode := range fault.SOAP.Subcodes {
					add(subcode)
				}
				collectSOAPModules20Namespaces(fault.SOAP.Modules, result)
				collectSOAPHeaders20Namespaces(fault.SOAP.Headers, result)
			}
			if fault.HTTP != nil {
				collectHTTPHeaders20Namespaces(fault.HTTP.Headers, result)
			}
		}
		for _, operation := range binding.Operations {
			collectExtensibilityNamespaces(operation.Extensibility, result)
			add(operation.Ref)
			if operation.SOAP != nil {
				collectSOAPModules20Namespaces(operation.SOAP.Modules, result)
			}
			for _, message := range operation.Inputs {
				collectExtensibilityNamespaces(message.Extensibility, result)
				if message.SOAP != nil {
					collectSOAPModules20Namespaces(message.SOAP.Modules, result)
					collectSOAPHeaders20Namespaces(message.SOAP.Headers, result)
				}
				if message.HTTP != nil {
					collectHTTPHeaders20Namespaces(message.HTTP.Headers, result)
				}
			}
			for _, message := range operation.Outputs {
				collectExtensibilityNamespaces(message.Extensibility, result)
				if message.SOAP != nil {
					collectSOAPModules20Namespaces(message.SOAP.Modules, result)
					collectSOAPHeaders20Namespaces(message.SOAP.Headers, result)
				}
				if message.HTTP != nil {
					collectHTTPHeaders20Namespaces(message.HTTP.Headers, result)
				}
			}
			for _, fault := range operation.InFaults {
				collectExtensibilityNamespaces(fault.Extensibility, result)
				add(fault.Ref)
				if fault.SOAP != nil {
					collectSOAPModules20Namespaces(fault.SOAP.Modules, result)
				}
			}
			for _, fault := range operation.OutFaults {
				collectExtensibilityNamespaces(fault.Extensibility, result)
				add(fault.Ref)
				if fault.SOAP != nil {
					collectSOAPModules20Namespaces(fault.SOAP.Modules, result)
				}
			}
		}
	}
	for _, service := range description.Services {
		collectExtensibilityNamespaces(service.Extensibility, result)
		add(service.Interface)
		for _, endpoint := range service.Endpoints {
			collectExtensibilityNamespaces(endpoint.Extensibility, result)
			add(endpoint.Binding)
		}
	}
}

func collectSOAPModules20Namespaces(values []SOAPModule20, result map[string]struct{}) {
	for _, value := range values {
		collectExtensibilityNamespaces(value.Extensibility, result)
	}
}

func collectSOAPHeaders20Namespaces(values []SOAPHeader20, result map[string]struct{}) {
	for _, value := range values {
		collectExtensibilityNamespaces(value.Extensibility, result)
		if value.Element.Namespace != "" {
			result[value.Element.Namespace] = struct{}{}
		}
	}
}

func collectHTTPHeaders20Namespaces(values []HTTPHeader20, result map[string]struct{}) {
	for _, value := range values {
		collectExtensibilityNamespaces(value.Extensibility, result)
		if value.Type.Namespace != "" {
			result[value.Type.Namespace] = struct{}{}
		}
	}
}

func collectExtensibilityNamespaces(value Extensibility, result map[string]struct{}) {
	for _, attribute := range value.ExtensionAttributes {
		if attribute.Name.Namespace != "" {
			result[attribute.Name.Namespace] = struct{}{}
		}
	}
}
