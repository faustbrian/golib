package wsdl

import (
	"encoding/xml"
	"strconv"
	"strings"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func (m marshalValue) definitions11(encoder tokenEncoder, value Definitions11) error {
	attributes := namespaceAttributes(m.prefixes)
	if value.Name != "" {
		attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "name"}, Value: value.Name})
	}
	if value.TargetNamespace != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "targetNamespace"}, Value: value.TargetNamespace,
		})
	}
	attributes, err := m.extensionAttributes(attributes, Extensibility{
		Extensions:          value.Extensions,
		ExtensionAttributes: value.ExtensionAttributes,
	})
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "definitions"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		for _, importValue := range value.Imports {
			if err := m.import11(encoder, importValue); err != nil {
				return err
			}
		}
		if value.Types != nil {
			if err := m.types11(encoder, *value.Types); err != nil {
				return err
			}
		}
		for _, message := range value.Messages {
			if err := m.message11(encoder, message); err != nil {
				return err
			}
		}
		for _, portType := range value.PortTypes {
			if err := m.portType11(encoder, portType); err != nil {
				return err
			}
		}
		for _, binding := range value.Bindings {
			if err := m.binding11(encoder, binding); err != nil {
				return err
			}
		}
		for _, service := range value.Services {
			if err := m.service11(encoder, service); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) import11(encoder tokenEncoder, value Import11) error {
	attributes := []xml.Attr{
		{Name: xml.Name{Local: "namespace"}, Value: value.Namespace},
		{Name: xml.Name{Local: "location"}, Value: value.Location},
	}
	attributes, err := m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "import"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) types11(encoder tokenEncoder, value Types11) error {
	attributes, err := m.extensionAttributes(nil, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "types"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		for _, schema := range value.Schemas {
			payload, err := xsd.Marshal(schema)
			if err != nil {
				return err
			}
			if err := encodeRawXML(encoder, payload); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) message11(encoder tokenEncoder, value Message11) error {
	attributes, err := m.extensionAttributes(
		[]xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}},
		value.Extensibility,
	)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "message"},
		Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		for _, part := range value.Parts {
			attributes := []xml.Attr{{Name: xml.Name{Local: "name"}, Value: part.Name}}
			if part.Element.Local != "" {
				lexical, err := m.qname(part.Element)
				if err != nil {
					return err
				}
				attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "element"}, Value: lexical})
			}
			if part.Type.Local != "" {
				lexical, err := m.qname(part.Type)
				if err != nil {
					return err
				}
				attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "type"}, Value: lexical})
			}
			attributes, err = m.extensionAttributes(attributes, part.Extensibility)
			if err != nil {
				return err
			}
			err = encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL11, Local: "part"}, Attr: attributes,
			}, func() error {
				return encodeExtensions(encoder, part.Extensions)
			})
			if err == nil {
				continue
			}
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) portType11(encoder tokenEncoder, value PortType11) error {
	attributes, err := m.extensionAttributes(
		[]xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}},
		value.Extensibility,
	)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "portType"},
		Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		for _, operation := range value.Operations {
			if err := m.operation11(encoder, operation); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) operation11(encoder tokenEncoder, value Operation11) error {
	attributes := []xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}}
	if len(value.ParameterOrder) > 0 {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "parameterOrder"}, Value: strings.Join(value.ParameterOrder, " "),
		})
	}
	attributes, err := m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "operation"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		encodeInput := func() error {
			if value.Input == nil {
				return nil
			}
			return m.operationMessage11(encoder, "input", *value.Input)
		}
		encodeOutput := func() error {
			if value.Output == nil {
				return nil
			}
			return m.operationMessage11(encoder, "output", *value.Output)
		}
		if value.Style == OperationStyleSolicitResponse {
			if err := encodeOutput(); err != nil {
				return err
			}
			if err := encodeInput(); err != nil {
				return err
			}
		} else {
			if err := encodeInput(); err != nil {
				return err
			}
			if err := encodeOutput(); err != nil {
				return err
			}
		}
		for _, fault := range value.Faults {
			if err := m.operationMessage11(encoder, "fault", fault); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) operationMessage11(
	encoder tokenEncoder,
	local string,
	value OperationMessage11,
) error {
	message, err := m.qname(value.Message)
	if err != nil {
		return err
	}
	attributes := make([]xml.Attr, 0, 2)
	if value.Name != "" {
		attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "name"}, Value: value.Name})
	}
	attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "message"}, Value: message})
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: local}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) binding11(encoder tokenEncoder, value Binding11) error {
	typeName, err := m.qname(value.Type)
	if err != nil {
		return err
	}
	attributes := []xml.Attr{
		{Name: xml.Name{Local: "name"}, Value: value.Name},
		{Name: xml.Name{Local: "type"}, Value: typeName},
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "binding"},
		Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		if value.SOAP != nil {
			prefix := soapPrefix(value.SOAP.Version)
			attributes := make([]xml.Attr, 0, 2)
			if value.SOAP.TransportSet || value.SOAP.Transport != "" {
				attributes = append(attributes, xml.Attr{
					Name: xml.Name{Local: "transport"}, Value: value.SOAP.Transport,
				})
			}
			if value.SOAP.StyleSet || value.SOAP.Style != "" {
				attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "style"}, Value: string(value.SOAP.Style)})
			}
			if err := encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Local: prefix + ":binding"}, Attr: attributes,
			}, nil); err != nil {
				return err
			}
		}
		if value.HTTP != nil {
			if err := encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Local: "http:binding"},
				Attr: []xml.Attr{{Name: xml.Name{Local: "verb"}, Value: value.HTTP.Verb}},
			}, nil); err != nil {
				return err
			}
		}
		for _, operation := range value.Operations {
			if err := m.bindingOperation11(encoder, operation); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) bindingOperation11(
	encoder tokenEncoder,
	value BindingOperation11,
) error {
	attributes, err := m.extensionAttributes(
		[]xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}},
		value.Extensibility,
	)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "operation"},
		Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		if value.SOAP != nil {
			attributes := make([]xml.Attr, 0, 3)
			if value.SOAP.ActionSet || value.SOAP.Action != "" {
				attributes = append(attributes, xml.Attr{
					Name: xml.Name{Local: "soapAction"}, Value: value.SOAP.Action,
				})
			}
			if value.SOAP.ActionRequiredSet {
				attributes = append(attributes, xml.Attr{
					Name:  xml.Name{Local: "soapActionRequired"},
					Value: strconv.FormatBool(value.SOAP.ActionRequired),
				})
			}
			if value.SOAP.StyleSet || value.SOAP.Style != "" {
				attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "style"}, Value: string(value.SOAP.Style)})
			}
			if err := encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Local: soapPrefix(value.SOAP.Version) + ":operation"},
				Attr: attributes,
			}, nil); err != nil {
				return err
			}
		}
		if value.HTTP != nil {
			if err := encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Local: "http:operation"},
				Attr: []xml.Attr{{Name: xml.Name{Local: "location"}, Value: value.HTTP.Location}},
			}, nil); err != nil {
				return err
			}
		}
		if value.Input != nil {
			if err := m.bindingMessage11(encoder, "input", *value.Input); err != nil {
				return err
			}
		}
		if value.Output != nil {
			if err := m.bindingMessage11(encoder, "output", *value.Output); err != nil {
				return err
			}
		}
		for _, fault := range value.Faults {
			if err := m.bindingMessage11(encoder, "fault", fault); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) bindingMessage11(
	encoder tokenEncoder,
	local string,
	value BindingMessage11,
) error {
	attributes := make([]xml.Attr, 0, 1)
	if value.Name != "" {
		attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "name"}, Value: value.Name})
	}
	attributes, err := m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: local}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		if value.SOAPBody != nil {
			if err := encodeSOAPBody11(encoder, *value.SOAPBody); err != nil {
				return err
			}
		}
		for _, header := range value.SOAPHeaders {
			if err := m.soapHeader11(encoder, header); err != nil {
				return err
			}
		}
		if value.SOAPFault != nil {
			if err := encodeSOAPFault11(encoder, *value.SOAPFault); err != nil {
				return err
			}
		}
		if value.HTTP != nil {
			if err := encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Local: "http:" + string(value.HTTP.Mode)},
			}, nil); err != nil {
				return err
			}
		}
		if value.MIME != nil {
			if err := encodeMIMEMessage11(encoder, *value.MIME); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func encodeSOAPBody11(encoder tokenEncoder, value SOAPBody11) error {
	attributes := make([]xml.Attr, 0, 4)
	if value.UseSet || value.Use != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "use"}, Value: string(value.Use),
		})
	}
	if value.NamespaceSet || value.Namespace != "" {
		attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "namespace"}, Value: value.Namespace})
	}
	if value.EncodingStyleSet || len(value.EncodingStyle) > 0 {
		attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "encodingStyle"}, Value: strings.Join(value.EncodingStyle, " ")})
	}
	if value.PartsSet || len(value.Parts) > 0 {
		attributes = append(attributes, xml.Attr{Name: xml.Name{Local: "parts"}, Value: strings.Join(value.Parts, " ")})
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Local: soapPrefix(value.Version) + ":body"}, Attr: attributes,
	}, nil)
}

func (m marshalValue) soapHeader11(encoder tokenEncoder, value SOAPHeader11) error {
	message, err := m.qname(value.Message)
	if err != nil {
		return err
	}
	attributes := soapMessageAttributes(
		value.Part,
		value.Use,
		value.UseSet,
		value.Namespace,
		value.NamespaceSet,
		value.EncodingStyle,
		value.EncodingStyleSet,
	)
	attributes = append([]xml.Attr{
		{Name: xml.Name{Local: "message"}, Value: message},
	}, attributes...)
	start := xml.StartElement{
		Name: xml.Name{Local: soapPrefix(value.Version) + ":header"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		for _, fault := range value.HeaderFaults {
			faultMessage, err := m.qname(fault.Message)
			if err != nil {
				return err
			}
			attributes := soapMessageAttributes(
				fault.Part,
				fault.Use,
				fault.UseSet,
				fault.Namespace,
				fault.NamespaceSet,
				fault.EncodingStyle,
				fault.EncodingStyleSet,
			)
			attributes = append([]xml.Attr{
				{Name: xml.Name{Local: "message"}, Value: faultMessage},
			}, attributes...)
			if err := encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Local: soapPrefix(fault.Version) + ":headerfault"},
				Attr: attributes,
			}, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func soapMessageAttributes(
	part string,
	use SOAPUse,
	useSet bool,
	namespace string,
	namespaceSet bool,
	encodingStyle []string,
	encodingStyleSet bool,
) []xml.Attr {
	attributes := []xml.Attr{{Name: xml.Name{Local: "part"}, Value: part}}
	if useSet || use != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "use"}, Value: string(use),
		})
	}
	if namespaceSet || namespace != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "namespace"}, Value: namespace,
		})
	}
	if encodingStyleSet || len(encodingStyle) > 0 {
		attributes = append(attributes, xml.Attr{
			Name:  xml.Name{Local: "encodingStyle"},
			Value: strings.Join(encodingStyle, " "),
		})
	}
	return attributes
}

func encodeSOAPFault11(encoder tokenEncoder, value SOAPFault11) error {
	attributes := []xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}}
	if value.UseSet || value.Use != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "use"}, Value: string(value.Use),
		})
	}
	if value.NamespaceSet || value.Namespace != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "namespace"}, Value: value.Namespace,
		})
	}
	if value.EncodingStyleSet || len(value.EncodingStyle) > 0 {
		attributes = append(attributes, xml.Attr{
			Name:  xml.Name{Local: "encodingStyle"},
			Value: strings.Join(value.EncodingStyle, " "),
		})
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Local: soapPrefix(value.Version) + ":fault"}, Attr: attributes,
	}, nil)
}

func soapPrefix(version Version) string {
	if version == Version12 {
		return "soap12"
	}
	return "soap"
}

func encodeMIMEMessage11(encoder tokenEncoder, value MIMEMessage11) error {
	for _, content := range value.Contents {
		if err := encodeMIMEContent11(encoder, content); err != nil {
			return err
		}
	}
	for _, xmlValue := range value.XML {
		if err := encodeElement(encoder, xml.StartElement{
			Name: xml.Name{Local: "mime:mimeXml"},
			Attr: []xml.Attr{{Name: xml.Name{Local: "part"}, Value: xmlValue.Part}},
		}, nil); err != nil {
			return err
		}
	}
	for _, multipart := range value.Multipart {
		start := xml.StartElement{Name: xml.Name{Local: "mime:multipartRelated"}}
		err := encodeElement(encoder, start, func() error {
			for _, part := range multipart.Parts {
				partStart := xml.StartElement{Name: xml.Name{Local: "mime:part"}}
				err := encodeElement(encoder, partStart, func() error {
					if part.SOAPBody != nil {
						if err := encodeSOAPBody11(encoder, *part.SOAPBody); err != nil {
							return err
						}
					}
					for _, content := range part.Contents {
						if err := encodeMIMEContent11(encoder, content); err != nil {
							return err
						}
					}
					for _, xmlValue := range part.XML {
						if err := encodeElement(encoder, xml.StartElement{
							Name: xml.Name{Local: "mime:mimeXml"},
							Attr: []xml.Attr{{Name: xml.Name{Local: "part"}, Value: xmlValue.Part}},
						}, nil); err != nil {
							return err
						}
					}
					return nil
				})
				if err == nil {
					continue
				}
				return err
			}
			return nil
		})
		if err == nil {
			continue
		}
		return err
	}
	return nil
}

func encodeMIMEContent11(encoder tokenEncoder, value MIMEContent11) error {
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Local: "mime:content"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "part"}, Value: value.Part},
			{Name: xml.Name{Local: "type"}, Value: value.Type},
		},
	}, nil)
}

func (m marshalValue) service11(encoder tokenEncoder, value Service11) error {
	attributes, err := m.extensionAttributes(
		[]xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}},
		value.Extensibility,
	)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL11, Local: "service"},
		Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL11); err != nil {
			return err
		}
		for _, port := range value.Ports {
			binding, err := m.qname(port.Binding)
			if err != nil {
				return err
			}
			attributes := []xml.Attr{
				{Name: xml.Name{Local: "name"}, Value: port.Name},
				{Name: xml.Name{Local: "binding"}, Value: binding},
			}
			attributes, err = m.extensionAttributes(attributes, port.Extensibility)
			if err != nil {
				return err
			}
			portStart := xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL11, Local: "port"},
				Attr: attributes,
			}
			err = encodeElement(encoder, portStart, func() error {
				if err := encodeDocumentation(encoder, port.Documentation, NamespaceWSDL11); err != nil {
					return err
				}
				if port.SOAPAddress != nil {
					prefix := "soap"
					if port.SOAPAddress.Version == Version12 {
						prefix = "soap12"
					}
					if err := encodeElement(encoder, xml.StartElement{
						Name: xml.Name{Local: prefix + ":address"},
						Attr: []xml.Attr{{Name: xml.Name{Local: "location"}, Value: port.SOAPAddress.Location}},
					}, nil); err != nil {
						return err
					}
				}
				if port.HTTPAddress != nil {
					if err := encodeElement(encoder, xml.StartElement{
						Name: xml.Name{Local: "http:address"},
						Attr: []xml.Attr{{Name: xml.Name{Local: "location"}, Value: port.HTTPAddress.Location}},
					}, nil); err != nil {
						return err
					}
				}
				return encodeExtensions(encoder, port.Extensions)
			})
			if err == nil {
				continue
			}
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func encodeDocumentation(
	encoder tokenEncoder,
	documentation *Documentation,
	namespace string,
) error {
	if documentation == nil {
		return nil
	}
	attributes := make([]xml.Attr, 0, 1)
	if documentation.Language != "" {
		attributes = append(attributes, xml.Attr{
			Name:  xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "lang"},
			Value: documentation.Language,
		})
	}
	start := xml.StartElement{
		Name: xml.Name{Space: namespace, Local: "documentation"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		return encoder.EncodeToken(xml.CharData(documentation.Content))
	})
}

func encodeExtensions(encoder tokenEncoder, extensions []Extension) error {
	for _, extension := range extensions {
		if err := encodeRawXML(encoder, extension.XML); err != nil {
			return err
		}
	}
	return nil
}
