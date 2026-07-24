package wsdl

import (
	"encoding/xml"
	"strconv"
	"strings"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func (m marshalValue) description20(encoder tokenEncoder, value Description20) error {
	attributes := namespaceAttributes(m.prefixes)
	attributes = append(attributes, xml.Attr{
		Name: xml.Name{Local: "targetNamespace"}, Value: value.TargetNamespace,
	})
	attributes, err := m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: "description"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		for _, importValue := range value.Imports {
			attributes := []xml.Attr{
				{Name: xml.Name{Local: "namespace"}, Value: importValue.Namespace},
			}
			if importValue.Location != "" {
				attributes = append(attributes, xml.Attr{
					Name: xml.Name{Local: "location"}, Value: importValue.Location,
				})
			}
			attributes, err = m.extensionAttributes(attributes, importValue.Extensibility)
			if err != nil {
				return err
			}
			err = encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL20, Local: "import"}, Attr: attributes,
			}, func() error {
				if err := encodeDocumentation(encoder, importValue.Documentation, NamespaceWSDL20); err != nil {
					return err
				}
				return encodeExtensions(encoder, importValue.Extensions)
			})
			if err == nil {
				continue
			}
			return err
		}
		for _, include := range value.Includes {
			attributes := []xml.Attr{{Name: xml.Name{Local: "location"}, Value: include.Location}}
			attributes, err = m.extensionAttributes(attributes, include.Extensibility)
			if err != nil {
				return err
			}
			err = encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL20, Local: "include"},
				Attr: attributes,
			}, func() error {
				if err := encodeDocumentation(encoder, include.Documentation, NamespaceWSDL20); err != nil {
					return err
				}
				return encodeExtensions(encoder, include.Extensions)
			})
			if err == nil {
				continue
			}
			return err
		}
		if value.Types != nil {
			if err := m.types20(encoder, *value.Types); err != nil {
				return err
			}
		}
		for _, interfaceValue := range value.Interfaces {
			if err := m.interface20(encoder, interfaceValue); err != nil {
				return err
			}
		}
		for _, binding := range value.Bindings {
			if err := m.binding20(encoder, binding); err != nil {
				return err
			}
		}
		for _, service := range value.Services {
			if err := m.service20(encoder, service); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) types20(encoder tokenEncoder, value Types20) error {
	attributes, err := m.extensionAttributes(nil, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: "types"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		for _, importValue := range value.Imports {
			attributes := make([]xml.Attr, 0, 2)
			if importValue.Namespace != "" {
				attributes = append(attributes, xml.Attr{
					Name: xml.Name{Local: "namespace"}, Value: importValue.Namespace,
				})
			}
			if importValue.Location != "" {
				attributes = append(attributes, xml.Attr{
					Name:  xml.Name{Local: "schemaLocation"},
					Value: importValue.Location,
				})
			}
			if err := encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Space: NamespaceXMLSchema, Local: "import"},
				Attr: attributes,
			}, nil); err != nil {
				return err
			}
		}
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

func (m marshalValue) interface20(encoder tokenEncoder, value Interface20) error {
	attributes := []xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}}
	if len(value.Extends) > 0 {
		parents := make([]string, 0, len(value.Extends))
		for _, parent := range value.Extends {
			lexical, err := m.qname(parent)
			if err != nil {
				return err
			}
			parents = append(parents, lexical)
		}
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "extends"}, Value: strings.Join(parents, " "),
		})
	}
	if len(value.StyleDefault) > 0 {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "styleDefault"}, Value: strings.Join(value.StyleDefault, " "),
		})
	}
	attributes, err := m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: "interface"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		for _, fault := range value.Faults {
			element, elementSet, err := m.messageContent20(
				fault.Element,
				fault.MessageContentModel,
				fault.MessageContentModelSet,
			)
			if err != nil {
				return err
			}
			attributes := []xml.Attr{{Name: xml.Name{Local: "name"}, Value: fault.Name}}
			if elementSet {
				attributes = append(attributes, xml.Attr{
					Name: xml.Name{Local: "element"}, Value: element,
				})
			}
			attributes, err = m.extensionAttributes(attributes, fault.Extensibility)
			if err != nil {
				return err
			}
			err = encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL20, Local: "fault"},
				Attr: attributes,
			}, func() error {
				if err := encodeDocumentation(encoder, fault.Documentation, NamespaceWSDL20); err != nil {
					return err
				}
				return encodeExtensions(encoder, fault.Extensions)
			})
			if err == nil {
				continue
			}
			return err
		}
		for _, operation := range value.Operations {
			if err := m.interfaceOperation20(encoder, operation); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) interfaceOperation20(
	encoder tokenEncoder,
	value InterfaceOperation20,
) error {
	attributes := []xml.Attr{
		{Name: xml.Name{Local: "name"}, Value: value.Name},
		{Name: xml.Name{Local: "pattern"}, Value: string(value.Pattern)},
	}
	if len(value.Style) > 0 {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "style"}, Value: strings.Join(value.Style, " "),
		})
	}
	if value.SafeSet {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "wsdlx:safe"}, Value: strconv.FormatBool(value.Safe),
		})
	}
	if value.RPCSignatureSet {
		items := make([]string, 0)
		for _, parameter := range value.RPCSignature {
			name, qnameErr := m.qname(parameter.Name)
			if qnameErr != nil {
				return qnameErr
			}
			items = append(items, name, string(parameter.Direction))
		}
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "wrpc:signature"}, Value: strings.Join(items, " "),
		})
	}
	attributes, err := m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: "operation"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		for _, message := range interfaceInputs20(value) {
			if err := m.interfaceMessage20(encoder, "input", message); err != nil {
				return err
			}
		}
		for _, message := range interfaceOutputs20(value) {
			if err := m.interfaceMessage20(encoder, "output", message); err != nil {
				return err
			}
		}
		for _, fault := range value.InFaults {
			if err := m.interfaceFaultReference20(encoder, "infault", fault); err != nil {
				return err
			}
		}
		for _, fault := range value.OutFaults {
			if err := m.interfaceFaultReference20(encoder, "outfault", fault); err != nil {
				return err
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) interfaceMessage20(
	encoder tokenEncoder,
	local string,
	value InterfaceMessageReference20,
) error {
	element, elementSet, err := m.messageContent20(
		value.Element, value.MessageContentModel, value.MessageContentModelSet,
	)
	if err != nil {
		return err
	}
	attributes := make([]xml.Attr, 0, 2)
	if value.MessageLabel != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "messageLabel"}, Value: value.MessageLabel,
		})
	}
	if elementSet {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "element"}, Value: element,
		})
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: local}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) messageContent20(
	element QName,
	model MessageContentModel,
	modelSet bool,
) (string, bool, error) {
	if element.Local != "" {
		lexical, err := m.qname(element)
		return lexical, true, err
	}
	if modelSet {
		return string(model), true, nil
	}
	return "", false, nil
}

func (m marshalValue) interfaceFaultReference20(
	encoder tokenEncoder,
	local string,
	value InterfaceFaultReference20,
) error {
	reference, err := m.qname(value.Ref)
	if err != nil {
		return err
	}
	attributes := []xml.Attr{{Name: xml.Name{Local: "ref"}, Value: reference}}
	if value.MessageLabel != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "messageLabel"}, Value: value.MessageLabel,
		})
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: local}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) binding20(encoder tokenEncoder, value Binding20) error {
	attributes := []xml.Attr{{Name: xml.Name{Local: "name"}, Value: value.Name}}
	var err error
	if value.Interface.Local != "" {
		interfaceName, err := m.qname(value.Interface)
		if err != nil {
			return err
		}
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "interface"}, Value: interfaceName,
		})
	}
	attributes = append(attributes, xml.Attr{
		Name: xml.Name{Local: "type"}, Value: value.Type,
	})
	if value.SOAP != nil {
		if value.SOAP.VersionSet {
			attributes, err = m.qualifiedAttribute(attributes, QName{
				Namespace: NamespaceWSDL20SOAP, Local: "version",
			}, value.SOAP.Version)
			if err != nil {
				return err
			}
		}
		if value.SOAP.ProtocolSet {
			attributes, err = m.qualifiedAttribute(attributes, QName{
				Namespace: NamespaceWSDL20SOAP, Local: "protocol",
			}, value.SOAP.Protocol)
			if err != nil {
				return err
			}
		}
		if value.SOAP.MEPDefaultSet {
			attributes, err = m.qualifiedAttribute(attributes, QName{
				Namespace: NamespaceWSDL20SOAP, Local: "mepDefault",
			}, value.SOAP.MEPDefault)
			if err != nil {
				return err
			}
		}
	}
	if value.HTTP != nil {
		attributes, err = m.typedAttributes20(attributes, NamespaceWSDL20HTTP, []typedAttribute20{
			{local: "methodDefault", value: value.HTTP.MethodDefault, set: value.HTTP.MethodDefaultSet},
			{local: "version", value: value.HTTP.Version, set: value.HTTP.VersionSet},
			{local: "queryParameterSeparatorDefault", value: value.HTTP.QueryParameterSeparatorDefault, set: value.HTTP.QueryParameterSeparatorDefaultSet},
			{local: "contentEncodingDefault", value: value.HTTP.ContentEncodingDefault, set: value.HTTP.ContentEncodingDefaultSet},
			{local: "defaultTransferCoding", value: value.HTTP.DefaultTransferCoding, set: value.HTTP.DefaultTransferCodingSet},
			{local: "cookies", value: strconv.FormatBool(value.HTTP.Cookies), set: value.HTTP.CookiesSet},
		})
		if err != nil {
			return err
		}
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: "binding"}, Attr: attributes,
	}
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		if value.SOAP != nil {
			for _, module := range value.SOAP.Modules {
				if err := m.soapModule20(encoder, module); err != nil {
					return err
				}
			}
		}
		for _, fault := range value.Faults {
			if err := m.bindingFault20(encoder, fault); err != nil {
				return err
			}
		}
		for _, operation := range value.Operations {
			reference, err := m.qname(operation.Ref)
			if err != nil {
				return err
			}
			attributes := []xml.Attr{{Name: xml.Name{Local: "ref"}, Value: reference}}
			if operation.SOAP != nil {
				if operation.SOAP.MEPSet {
					attributes, err = m.qualifiedAttribute(attributes, QName{
						Namespace: NamespaceWSDL20SOAP, Local: "mep",
					}, operation.SOAP.MEP)
					if err != nil {
						return err
					}
				}
				if operation.SOAP.ActionSet {
					attributes, err = m.qualifiedAttribute(attributes, QName{
						Namespace: NamespaceWSDL20SOAP, Local: "action",
					}, operation.SOAP.Action)
					if err != nil {
						return err
					}
				}
			}
			if operation.HTTP != nil {
				attributes, err = m.typedAttributes20(
					attributes,
					NamespaceWSDL20HTTP,
					[]typedAttribute20{
						{local: "location", value: operation.HTTP.Location, set: operation.HTTP.LocationSet},
						{local: "method", value: operation.HTTP.Method, set: operation.HTTP.MethodSet},
						{local: "inputSerialization", value: operation.HTTP.InputSerialization, set: operation.HTTP.InputSerializationSet},
						{local: "outputSerialization", value: operation.HTTP.OutputSerialization, set: operation.HTTP.OutputSerializationSet},
						{local: "faultSerialization", value: operation.HTTP.FaultSerialization, set: operation.HTTP.FaultSerializationSet},
						{local: "queryParameterSeparator", value: operation.HTTP.QueryParameterSeparator, set: operation.HTTP.QueryParameterSeparatorSet},
						{local: "contentEncodingDefault", value: operation.HTTP.ContentEncodingDefault, set: operation.HTTP.ContentEncodingDefaultSet},
						{local: "defaultTransferCoding", value: operation.HTTP.DefaultTransferCoding, set: operation.HTTP.DefaultTransferCodingSet},
						{local: "ignoreUncited", value: strconv.FormatBool(operation.HTTP.IgnoreUncited), set: operation.HTTP.IgnoreUncitedSet},
					},
				)
				if err != nil {
					return err
				}
			}
			attributes, err = m.extensionAttributes(attributes, operation.Extensibility)
			if err != nil {
				return err
			}
			err = encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL20, Local: "operation"},
				Attr: attributes,
			}, func() error {
				if err := encodeDocumentation(encoder, operation.Documentation, NamespaceWSDL20); err != nil {
					return err
				}
				if operation.SOAP != nil {
					for _, module := range operation.SOAP.Modules {
						if err := m.soapModule20(encoder, module); err != nil {
							return err
						}
					}
				}
				for _, message := range operation.Inputs {
					if err := m.bindingMessage20(encoder, "input", message); err != nil {
						return err
					}
				}
				for _, message := range operation.Outputs {
					if err := m.bindingMessage20(encoder, "output", message); err != nil {
						return err
					}
				}
				for _, fault := range operation.InFaults {
					if err := m.bindingFaultReference20(encoder, "infault", fault); err != nil {
						return err
					}
				}
				for _, fault := range operation.OutFaults {
					if err := m.bindingFaultReference20(encoder, "outfault", fault); err != nil {
						return err
					}
				}
				return encodeExtensions(encoder, operation.Extensions)
			})
			if err == nil {
				continue
			}
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) bindingFault20(encoder tokenEncoder, value BindingFault20) error {
	reference, err := m.qname(value.Ref)
	if err != nil {
		return err
	}
	attributes := []xml.Attr{{Name: xml.Name{Local: "ref"}, Value: reference}}
	if value.SOAP != nil {
		if value.SOAP.CodeSet {
			code := "#any"
			if !value.SOAP.CodeAny {
				code, err = m.qname(value.SOAP.Code)
				if err != nil {
					return err
				}
			}
			attributes, err = m.qualifiedAttribute(attributes, QName{
				Namespace: NamespaceWSDL20SOAP, Local: "code",
			}, code)
			if err != nil {
				return err
			}
		}
		if value.SOAP.SubcodesSet {
			subcodes := "#any"
			if !value.SOAP.SubcodesAny {
				values := make([]string, 0, len(value.SOAP.Subcodes))
				for _, subcode := range value.SOAP.Subcodes {
					lexical, qnameErr := m.qname(subcode)
					if qnameErr != nil {
						return qnameErr
					}
					values = append(values, lexical)
				}
				subcodes = strings.Join(values, " ")
			}
			attributes, err = m.qualifiedAttribute(attributes, QName{
				Namespace: NamespaceWSDL20SOAP, Local: "subcodes",
			}, subcodes)
			if err != nil {
				return err
			}
		}
	}
	if value.HTTP != nil {
		attributes, err = m.typedAttributes20(attributes, NamespaceWSDL20HTTP, []typedAttribute20{
			{local: "code", value: value.HTTP.Code, set: value.HTTP.CodeSet},
			{local: "contentEncoding", value: value.HTTP.ContentEncoding, set: value.HTTP.ContentEncodingSet},
			{local: "transferCoding", value: value.HTTP.TransferCoding, set: value.HTTP.TransferCodingSet},
		})
		if err != nil {
			return err
		}
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: "fault"}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		if value.SOAP != nil {
			for _, module := range value.SOAP.Modules {
				if err := m.soapModule20(encoder, module); err != nil {
					return err
				}
			}
			for _, header := range value.SOAP.Headers {
				if err := m.soapHeader20(encoder, header); err != nil {
					return err
				}
			}
		}
		if value.HTTP != nil {
			for _, header := range value.HTTP.Headers {
				if err := m.httpHeader20(encoder, header); err != nil {
					return err
				}
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) bindingMessage20(
	encoder tokenEncoder,
	local string,
	value BindingMessageReference20,
) error {
	attributes := make([]xml.Attr, 0, 1)
	if value.MessageLabel != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "messageLabel"}, Value: value.MessageLabel,
		})
	}
	var err error
	if value.HTTP != nil {
		attributes, err = m.typedAttributes20(attributes, NamespaceWSDL20HTTP, []typedAttribute20{
			{local: "contentEncoding", value: value.HTTP.ContentEncoding, set: value.HTTP.ContentEncodingSet},
			{local: "transferCoding", value: value.HTTP.TransferCoding, set: value.HTTP.TransferCodingSet},
		})
		if err != nil {
			return err
		}
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: local}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		if value.SOAP != nil {
			for _, module := range value.SOAP.Modules {
				if err := m.soapModule20(encoder, module); err != nil {
					return err
				}
			}
			for _, header := range value.SOAP.Headers {
				if err := m.soapHeader20(encoder, header); err != nil {
					return err
				}
			}
		}
		if value.HTTP != nil {
			for _, header := range value.HTTP.Headers {
				if err := m.httpHeader20(encoder, header); err != nil {
					return err
				}
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) bindingFaultReference20(
	encoder tokenEncoder,
	local string,
	value BindingFaultReference20,
) error {
	reference, err := m.qname(value.Ref)
	if err != nil {
		return err
	}
	attributes := []xml.Attr{{Name: xml.Name{Local: "ref"}, Value: reference}}
	if value.MessageLabel != "" {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "messageLabel"}, Value: value.MessageLabel,
		})
	}
	if value.HTTP != nil && value.HTTP.TransferCodingSet {
		attributes, err = m.qualifiedAttribute(attributes, QName{
			Namespace: NamespaceWSDL20HTTP, Local: "transferCoding",
		}, value.HTTP.TransferCoding)
		if err != nil {
			return err
		}
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: local}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		if value.SOAP != nil {
			for _, module := range value.SOAP.Modules {
				if err := m.soapModule20(encoder, module); err != nil {
					return err
				}
			}
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) soapModule20(encoder tokenEncoder, value SOAPModule20) error {
	attributes := []xml.Attr{{Name: xml.Name{Local: "ref"}, Value: value.Ref}}
	if value.RequiredSet {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "required"}, Value: strconv.FormatBool(value.Required),
		})
	}
	attributes, err := m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20SOAP, Local: "module"}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) soapHeader20(encoder tokenEncoder, value SOAPHeader20) error {
	element, err := m.qname(value.Element)
	if err != nil {
		return err
	}
	attributes := []xml.Attr{{Name: xml.Name{Local: "element"}, Value: element}}
	if value.MustUnderstandSet {
		attributes = append(attributes, xml.Attr{
			Name:  xml.Name{Local: "mustUnderstand"},
			Value: strconv.FormatBool(value.MustUnderstand),
		})
	}
	if value.RequiredSet {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "required"}, Value: strconv.FormatBool(value.Required),
		})
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20SOAP, Local: "header"}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

type typedAttribute20 struct {
	local string
	value string
	set   bool
}

func (m marshalValue) typedAttributes20(
	attributes []xml.Attr,
	namespace string,
	values []typedAttribute20,
) ([]xml.Attr, error) {
	var err error
	for _, value := range values {
		if !value.set {
			continue
		}
		attributes, err = m.qualifiedAttribute(attributes, QName{
			Namespace: namespace, Local: value.local,
		}, value.value)
		if err != nil {
			return nil, err
		}
	}
	return attributes, nil
}

func (m marshalValue) httpHeader20(encoder tokenEncoder, value HTTPHeader20) error {
	typeName, err := m.qname(value.Type)
	if err != nil {
		return err
	}
	attributes := []xml.Attr{
		{Name: xml.Name{Local: "name"}, Value: value.Name},
		{Name: xml.Name{Local: "type"}, Value: typeName},
	}
	if value.RequiredSet {
		attributes = append(attributes, xml.Attr{
			Name: xml.Name{Local: "required"}, Value: strconv.FormatBool(value.Required),
		})
	}
	attributes, err = m.extensionAttributes(attributes, value.Extensibility)
	if err != nil {
		return err
	}
	return encodeElement(encoder, xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20HTTP, Local: "header"}, Attr: attributes,
	}, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}

func (m marshalValue) service20(encoder tokenEncoder, value Service20) error {
	interfaceName, err := m.qname(value.Interface)
	if err != nil {
		return err
	}
	start := xml.StartElement{
		Name: xml.Name{Space: NamespaceWSDL20, Local: "service"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "name"}, Value: value.Name},
			{Name: xml.Name{Local: "interface"}, Value: interfaceName},
		},
	}
	attributes, err := m.extensionAttributes(start.Attr, value.Extensibility)
	if err != nil {
		return err
	}
	start.Attr = attributes
	return encodeElement(encoder, start, func() error {
		if err := encodeDocumentation(encoder, value.Documentation, NamespaceWSDL20); err != nil {
			return err
		}
		for _, endpoint := range value.Endpoints {
			binding, err := m.qname(endpoint.Binding)
			if err != nil {
				return err
			}
			attributes := []xml.Attr{
				{Name: xml.Name{Local: "name"}, Value: endpoint.Name},
				{Name: xml.Name{Local: "binding"}, Value: binding},
			}
			if endpoint.Address != "" {
				attributes = append(attributes, xml.Attr{
					Name: xml.Name{Local: "address"}, Value: endpoint.Address,
				})
			}
			if endpoint.HTTP != nil {
				attributes, err = m.typedAttributes20(attributes, NamespaceWSDL20HTTP, []typedAttribute20{
					{local: "authenticationScheme", value: endpoint.HTTP.AuthenticationScheme, set: endpoint.HTTP.AuthenticationSchemeSet},
					{local: "authenticationRealm", value: endpoint.HTTP.AuthenticationRealm, set: endpoint.HTTP.AuthenticationRealmSet},
				})
				if err != nil {
					return err
				}
			}
			attributes, err = m.extensionAttributes(attributes, endpoint.Extensibility)
			if err != nil {
				return err
			}
			err = encodeElement(encoder, xml.StartElement{
				Name: xml.Name{Space: NamespaceWSDL20, Local: "endpoint"}, Attr: attributes,
			}, func() error {
				if err := encodeDocumentation(encoder, endpoint.Documentation, NamespaceWSDL20); err != nil {
					return err
				}
				return encodeExtensions(encoder, endpoint.Extensions)
			})
			if err == nil {
				continue
			}
			return err
		}
		return encodeExtensions(encoder, value.Extensions)
	})
}
