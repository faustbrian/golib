package wsdl

import (
	"context"
	"encoding/xml"
	"fmt"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func decodeDefinitions11(root *xmlNode, state *parseState) (Definitions11, error) {
	definitions := Definitions11{
		Name:                root.attribute("name"),
		TargetNamespace:     root.attribute("targetNamespace"),
		Location:            root.location,
		ExtensionAttributes: decodeExtensionAttributes(root, NamespaceWSDL11),
	}
	messages := make(map[string]struct{})
	portTypes := make(map[string]struct{})
	bindings := make(map[string]struct{})
	services := make(map[string]struct{})
	for _, child := range root.children {
		switch child.name {
		case xml.Name{Space: NamespaceWSDL11, Local: "documentation"}:
			definitions.Documentation = child.documentation()
		case xml.Name{Space: NamespaceWSDL11, Local: "import"}:
			importValue, err := decodeImport11(child)
			if err != nil {
				return Definitions11{}, err
			}
			definitions.Imports = append(definitions.Imports, importValue)
		case xml.Name{Space: NamespaceWSDL11, Local: "types"}:
			types, err := decodeTypes11(state.ctx, child, state.options)
			if err != nil {
				return Definitions11{}, err
			}
			definitions.Types = &types
		case xml.Name{Space: NamespaceWSDL11, Local: "message"}:
			message, err := decodeMessage11(child)
			if err != nil {
				return Definitions11{}, err
			}
			if err := registerSymbol(messages, "message", message.Name); err != nil {
				return Definitions11{}, err
			}
			definitions.Messages = append(definitions.Messages, message)
		case xml.Name{Space: NamespaceWSDL11, Local: "portType"}:
			portType, err := decodePortType11(child)
			if err != nil {
				return Definitions11{}, err
			}
			if err := registerSymbol(portTypes, "portType", portType.Name); err != nil {
				return Definitions11{}, err
			}
			definitions.PortTypes = append(definitions.PortTypes, portType)
		case xml.Name{Space: NamespaceWSDL11, Local: "binding"}:
			binding, err := decodeBinding11(child)
			if err != nil {
				return Definitions11{}, err
			}
			if err := registerSymbol(bindings, "binding", binding.Name); err != nil {
				return Definitions11{}, err
			}
			definitions.Bindings = append(definitions.Bindings, binding)
		case xml.Name{Space: NamespaceWSDL11, Local: "service"}:
			service, err := decodeService11(child)
			if err != nil {
				return Definitions11{}, err
			}
			if err := registerSymbol(services, "service", service.Name); err != nil {
				return Definitions11{}, err
			}
			definitions.Services = append(definitions.Services, service)
		default:
			if child.name.Space != NamespaceWSDL11 {
				extension, err := decodeExtension(child, NamespaceWSDL11)
				if err != nil {
					return Definitions11{}, err
				}
				definitions.Extensions = append(definitions.Extensions, extension)
			}
		}
	}
	return definitions, nil
}

func decodeImport11(node *xmlNode) (Import11, error) {
	extensibility, err := decodeExtensibility(node, NamespaceWSDL11)
	if err != nil {
		return Import11{}, err
	}
	location := node.attribute("location")
	uri, err := resolveURI(node.baseURI, location)
	if err != nil {
		return Import11{}, fmt.Errorf("wsdl: resolve import %q: %w", location, err)
	}
	value := Import11{
		Extensibility: extensibility,
		Namespace:     node.attribute("namespace"),
		Location:      location,
		URI:           uri,
		Source:        node.location,
	}
	value.Documentation = firstDocumentation(node)
	return value, nil
}

func decodeExtensionAttributes(node *xmlNode, coreNamespace string) []ExtensionAttribute {
	attributes := make([]ExtensionAttribute, 0)
	for _, attribute := range node.attributes {
		if attribute.Name.Space == "" || attribute.Name.Space == "xmlns" ||
			attribute.Name.Space == coreNamespace ||
			attribute.Name.Space == "http://www.w3.org/XML/1998/namespace" {
			continue
		}
		attributes = append(attributes, ExtensionAttribute{
			Name:  QName{Namespace: attribute.Name.Space, Local: attribute.Name.Local},
			Value: attribute.Value, Location: node.location,
		})
	}
	return attributes
}

func decodeExtension(node *xmlNode, coreNamespace string) (Extension, error) {
	payload, err := marshalNode(node)
	if err != nil {
		return Extension{}, fmt.Errorf("wsdl: preserve extension: %w", err)
	}
	extension := Extension{
		Name: QName{Namespace: node.name.Space, Local: node.name.Local},
		XML:  payload, Location: node.location,
	}
	for _, attribute := range node.attributes {
		if isNamespaceDeclarationAttribute(attribute) {
			continue
		}
		if attribute.Name == (xml.Name{Space: coreNamespace, Local: "required"}) {
			required, valid := xmlBoolean(attribute.Value)
			if !valid {
				return Extension{}, fmt.Errorf(
					"wsdl: invalid extension required value %q",
					attribute.Value,
				)
			}
			extension.Required = required
			extension.RequiredSet = true
			continue
		}
		extension.Attributes = append(extension.Attributes, ExtensionAttribute{
			Name:  QName{Namespace: attribute.Name.Space, Local: attribute.Name.Local},
			Value: attribute.Value, Location: node.location,
		})
	}
	return extension, nil
}

func xmlBoolean(value string) (bool, bool) {
	switch value {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	default:
		return false, false
	}
}

func decodeExtensibility(node *xmlNode, coreNamespace string) (Extensibility, error) {
	return decodeExtensibilityExcept(node, coreNamespace, nil)
}

func decodeExtensibilityExcept(
	node *xmlNode,
	coreNamespace string,
	skip func(*xmlNode) bool,
) (Extensibility, error) {
	value := Extensibility{
		ExtensionAttributes: decodeExtensionAttributes(node, coreNamespace),
	}
	for _, child := range node.children {
		if child.name.Space == coreNamespace || (skip != nil && skip(child)) {
			continue
		}
		extension, err := decodeExtension(child, coreNamespace)
		if err != nil {
			return Extensibility{}, err
		}
		value.Extensions = append(value.Extensions, extension)
	}
	return value, nil
}

func registerSymbol(symbols map[string]struct{}, kind, name string) error {
	if _, duplicate := symbols[name]; duplicate {
		return fmt.Errorf("%w: %s %q", ErrDuplicateSymbol, kind, name)
	}
	symbols[name] = struct{}{}
	return nil
}

func decodeTypes11(ctx context.Context, node *xmlNode, options ParseOptions) (Types11, error) {
	types := Types11{
		Extensibility: Extensibility{
			ExtensionAttributes: decodeExtensionAttributes(node, NamespaceWSDL11),
		},
		Location: node.location,
	}
	for _, child := range node.children {
		if child.name != (xml.Name{Space: NamespaceXMLSchema, Local: "schema"}) {
			if child.name.Space != NamespaceWSDL11 {
				extension, err := decodeExtension(child, NamespaceWSDL11)
				if err != nil {
					return Types11{}, err
				}
				types.Extensions = append(types.Extensions, extension)
			}
			continue
		}
		if len(types.Schemas) >= options.MaxSchemas {
			return Types11{}, fmt.Errorf(
				"%w: inline schema count exceeds %d",
				ErrLimitExceeded,
				options.MaxSchemas,
			)
		}
		source, err := marshalNode(child)
		if err != nil {
			return Types11{}, fmt.Errorf("wsdl: serialize inline schema: %w", err)
		}
		schema, err := xsd.Parse(ctx, source, xsd.ParseOptions{
			SystemID:         options.SystemID,
			MaxDocumentBytes: options.MaxDocumentBytes,
			MaxDepth:         options.MaxDepth,
			MaxElements:      options.MaxElements,
		})
		if err != nil {
			return Types11{}, fmt.Errorf("wsdl: parse inline schema: %w", err)
		}
		types.Schemas = append(types.Schemas, schema)
	}
	return types, nil
}

func decodeMessage11(node *xmlNode) (Message11, error) {
	extensibility, err := decodeExtensibility(node, NamespaceWSDL11)
	if err != nil {
		return Message11{}, err
	}
	message := Message11{
		Extensibility: extensibility, Name: node.attribute("name"), Location: node.location,
	}
	parts := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			message.Documentation = documentation
			continue
		}
		if child.name != (xml.Name{Space: NamespaceWSDL11, Local: "part"}) {
			continue
		}
		element, err := child.qnameAttribute("element")
		if err != nil {
			return Message11{}, err
		}
		typeName, err := child.qnameAttribute("type")
		if err != nil {
			return Message11{}, err
		}
		partExtensibility, err := decodeExtensibility(child, NamespaceWSDL11)
		if err != nil {
			return Message11{}, err
		}
		part := Part11{
			Extensibility: partExtensibility,
			Name:          child.attribute("name"), Element: element, Type: typeName,
			Location: child.location,
		}
		if err := registerSymbol(parts, "message part", part.Name); err != nil {
			return Message11{}, err
		}
		message.Parts = append(message.Parts, part)
	}
	return message, nil
}

func decodePortType11(node *xmlNode) (PortType11, error) {
	extensibility, err := decodeExtensibility(node, NamespaceWSDL11)
	if err != nil {
		return PortType11{}, err
	}
	portType := PortType11{
		Extensibility: extensibility, Name: node.attribute("name"), Location: node.location,
	}
	operations := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			portType.Documentation = documentation
			continue
		}
		if child.name != (xml.Name{Space: NamespaceWSDL11, Local: "operation"}) {
			continue
		}
		operation, err := decodeOperation11(child)
		if err != nil {
			return PortType11{}, err
		}
		if err := registerSymbol(
			operations, "portType operation", operationSignature11(operation),
		); err != nil {
			return PortType11{}, err
		}
		portType.Operations = append(portType.Operations, operation)
	}
	return portType, nil
}

func operationSignature11(operation Operation11) string {
	input, output := "", ""
	if operation.Input != nil {
		input = operation.Input.Name
	}
	if operation.Output != nil {
		output = operation.Output.Name
	}
	return operation.Name + "|" + input + "|" + output
}

func decodeOperation11(node *xmlNode) (Operation11, error) {
	extensibility, err := decodeExtensibility(node, NamespaceWSDL11)
	if err != nil {
		return Operation11{}, err
	}
	operation := Operation11{
		Extensibility: extensibility,
		Name:          node.attribute("name"), ParameterOrder: splitSpaceSeparated(node.attribute("parameterOrder")),
		Location: node.location,
	}
	messageOrder := make([]string, 0, 2)
	faults := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			operation.Documentation = documentation
			continue
		}
		if child.name.Space != NamespaceWSDL11 {
			continue
		}
		message, err := decodeOperationMessage11(child)
		if err != nil {
			return Operation11{}, err
		}
		switch child.name.Local {
		case "input":
			if operation.Input != nil {
				return Operation11{}, fmt.Errorf("wsdl: operation %q has duplicate input", operation.Name)
			}
			operation.Input = &message
			messageOrder = append(messageOrder, "input")
		case "output":
			if operation.Output != nil {
				return Operation11{}, fmt.Errorf("wsdl: operation %q has duplicate output", operation.Name)
			}
			operation.Output = &message
			messageOrder = append(messageOrder, "output")
		case "fault":
			if err := registerSymbol(faults, "operation fault", message.Name); err != nil {
				return Operation11{}, err
			}
			operation.Faults = append(operation.Faults, message)
		}
	}
	operation.Style = operationStyle11(operation.Input, operation.Output, messageOrder)
	return operation, nil
}

func operationStyle11(
	input *OperationMessage11,
	output *OperationMessage11,
	order []string,
) OperationStyle11 {
	if input == nil {
		if output != nil {
			return OperationStyleNotification
		}
		return ""
	}
	if output == nil {
		return OperationStyleOneWay
	}
	if len(order) > 0 && order[0] == "output" {
		return OperationStyleSolicitResponse
	}
	return OperationStyleRequestResponse
}

func decodeOperationMessage11(node *xmlNode) (OperationMessage11, error) {
	message, err := node.qnameAttribute("message")
	if err != nil {
		return OperationMessage11{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL11)
	if err != nil {
		return OperationMessage11{}, err
	}
	value := OperationMessage11{
		Extensibility: extensibility,
		Name:          node.attribute("name"), Message: message, Location: node.location,
	}
	value.Documentation = firstDocumentation(node)
	return value, nil
}

func isNamespaceDeclarationAttribute(attribute xml.Attr) bool {
	if attribute.Name.Space == "xmlns" {
		return true
	}
	return attribute.Name.Space == "" && attribute.Name.Local == "xmlns"
}

func decodeBinding11(node *xmlNode) (Binding11, error) {
	typeName, err := node.qnameAttribute("type")
	if err != nil {
		return Binding11{}, err
	}
	extensibility, err := decodeExtensibilityExcept(
		node,
		NamespaceWSDL11,
		func(child *xmlNode) bool {
			return isSOAPExtension(child, "binding") ||
				child.name == (xml.Name{Space: NamespaceHTTPBinding, Local: "binding"})
		},
	)
	if err != nil {
		return Binding11{}, err
	}
	binding := Binding11{
		Extensibility: extensibility,
		Name:          node.attribute("name"), Type: typeName, Location: node.location,
	}
	operations := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			binding.Documentation = documentation
			continue
		}
		if isSOAPExtension(child, "binding") {
			binding.SOAP = &SOAPBinding11{
				Version:      soapVersion(child.name.Space),
				Style:        SOAPStyle(child.attribute("style")),
				StyleSet:     child.hasAttribute("style"),
				Transport:    child.attribute("transport"),
				TransportSet: child.hasAttribute("transport"),
				Location:     child.location,
			}
			continue
		}
		if child.name == (xml.Name{Space: NamespaceHTTPBinding, Local: "binding"}) {
			binding.HTTP = &HTTPBinding11{
				Verb: child.attribute("verb"), Location: child.location,
			}
			continue
		}
		if child.name == (xml.Name{Space: NamespaceWSDL11, Local: "operation"}) {
			operation, decodeErr := decodeBindingOperation11(child)
			if decodeErr != nil {
				return Binding11{}, decodeErr
			}
			if err := registerSymbol(
				operations, "binding operation", bindingOperationSignature11(operation),
			); err != nil {
				return Binding11{}, err
			}
			binding.Operations = append(binding.Operations, operation)
		}
	}
	return binding, nil
}

func bindingOperationSignature11(operation BindingOperation11) string {
	input, output := "", ""
	if operation.Input != nil {
		input = operation.Input.Name
	}
	if operation.Output != nil {
		output = operation.Output.Name
	}
	return operation.Name + "|" + input + "|" + output
}

func decodeBindingOperation11(node *xmlNode) (BindingOperation11, error) {
	extensibility, err := decodeExtensibilityExcept(
		node,
		NamespaceWSDL11,
		func(child *xmlNode) bool {
			return isSOAPExtension(child, "operation") ||
				child.name == (xml.Name{Space: NamespaceHTTPBinding, Local: "operation"})
		},
	)
	if err != nil {
		return BindingOperation11{}, err
	}
	operation := BindingOperation11{
		Extensibility: extensibility, Name: node.attribute("name"), Location: node.location,
	}
	faults := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			operation.Documentation = documentation
			continue
		}
		if isSOAPExtension(child, "operation") {
			soapOperation := SOAPOperation11{
				Version:   soapVersion(child.name.Space),
				Action:    child.attribute("soapAction"),
				ActionSet: child.hasAttribute("soapAction"),
				Style:     SOAPStyle(child.attribute("style")),
				StyleSet:  child.hasAttribute("style"),
				Location:  child.location,
			}
			if soapOperation.Version == Version12 && child.hasAttribute("soapActionRequired") {
				required, valid := xmlBoolean(child.attribute("soapActionRequired"))
				if !valid {
					return BindingOperation11{}, fmt.Errorf(
						"wsdl: invalid SOAP action required value %q",
						child.attribute("soapActionRequired"),
					)
				}
				soapOperation.ActionRequired = required
				soapOperation.ActionRequiredSet = true
			}
			operation.SOAP = &soapOperation
			continue
		}
		if child.name == (xml.Name{Space: NamespaceHTTPBinding, Local: "operation"}) {
			operation.HTTP = &HTTPOperation11{
				Location: child.attribute("location"), Source: child.location,
			}
			continue
		}
		if child.name == (xml.Name{Space: NamespaceWSDL11, Local: "input"}) {
			message, decodeErr := decodeBindingMessage11(child)
			if decodeErr != nil {
				return BindingOperation11{}, decodeErr
			}
			operation.Input = &message
			continue
		}
		if child.name == (xml.Name{Space: NamespaceWSDL11, Local: "output"}) {
			message, decodeErr := decodeBindingMessage11(child)
			if decodeErr != nil {
				return BindingOperation11{}, decodeErr
			}
			operation.Output = &message
			continue
		}
		if child.name == (xml.Name{Space: NamespaceWSDL11, Local: "fault"}) {
			message, decodeErr := decodeBindingMessage11(child)
			if decodeErr != nil {
				return BindingOperation11{}, decodeErr
			}
			if err := registerSymbol(faults, "binding operation fault", message.Name); err != nil {
				return BindingOperation11{}, err
			}
			operation.Faults = append(operation.Faults, message)
		}
	}
	return operation, nil
}

func decodeBindingMessage11(node *xmlNode) (BindingMessage11, error) {
	extensibility, err := decodeExtensibilityExcept(
		node,
		NamespaceWSDL11,
		func(child *xmlNode) bool {
			return isSOAPExtension(child, "body") ||
				isSOAPExtension(child, "header") ||
				isSOAPExtension(child, "fault") ||
				child.name == (xml.Name{
					Space: NamespaceHTTPBinding, Local: "urlEncoded",
				}) ||
				child.name == (xml.Name{
					Space: NamespaceHTTPBinding, Local: "urlReplacement",
				}) ||
				child.name == (xml.Name{
					Space: NamespaceMIMEBinding, Local: "content",
				}) ||
				child.name == (xml.Name{
					Space: NamespaceMIMEBinding, Local: "mimeXml",
				}) ||
				child.name == (xml.Name{
					Space: NamespaceMIMEBinding, Local: "multipartRelated",
				})
		},
	)
	if err != nil {
		return BindingMessage11{}, err
	}
	message := BindingMessage11{
		Extensibility: extensibility, Name: node.attribute("name"), Location: node.location,
	}
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			message.Documentation = documentation
			continue
		}
		if isSOAPExtension(child, "body") {
			body := decodeSOAPBody11(child)
			message.SOAPBody = &body
			continue
		}
		if isSOAPExtension(child, "header") {
			header, err := decodeSOAPHeader11(child)
			if err != nil {
				return BindingMessage11{}, err
			}
			message.SOAPHeaders = append(message.SOAPHeaders, header)
			continue
		}
		if isSOAPExtension(child, "fault") {
			message.SOAPFault = &SOAPFault11{
				Version:          soapVersion(child.name.Space),
				Name:             child.attribute("name"),
				Use:              SOAPUse(child.attribute("use")),
				UseSet:           child.hasAttribute("use"),
				Namespace:        child.attribute("namespace"),
				NamespaceSet:     child.hasAttribute("namespace"),
				EncodingStyle:    soapEncodingStyles(child),
				EncodingStyleSet: child.hasAttribute("encodingStyle"),
				Location:         child.location,
			}
			continue
		}
		switch child.name {
		case xml.Name{Space: NamespaceHTTPBinding, Local: "urlEncoded"}:
			message.HTTP = &HTTPMessage11{
				Mode: HTTPURLEncoded, Location: child.location,
			}
		case xml.Name{Space: NamespaceHTTPBinding, Local: "urlReplacement"}:
			message.HTTP = &HTTPMessage11{
				Mode: HTTPURLReplacement, Location: child.location,
			}
		case xml.Name{Space: NamespaceMIMEBinding, Local: "content"}:
			mime := ensureMIMEMessage11(&message)
			mime.Contents = append(mime.Contents, decodeMIMEContent11(child))
		case xml.Name{Space: NamespaceMIMEBinding, Local: "mimeXml"}:
			mime := ensureMIMEMessage11(&message)
			mime.XML = append(mime.XML, MIMEXML11{
				Part: child.attribute("part"), Location: child.location,
			})
		case xml.Name{Space: NamespaceMIMEBinding, Local: "multipartRelated"}:
			mime := ensureMIMEMessage11(&message)
			mime.Multipart = append(mime.Multipart, decodeMIMEMultipart11(child))
		}
	}
	return message, nil
}

func decodeSOAPBody11(node *xmlNode) SOAPBody11 {
	return SOAPBody11{
		Version:          soapVersion(node.name.Space),
		Use:              SOAPUse(node.attribute("use")),
		UseSet:           node.hasAttribute("use"),
		Namespace:        node.attribute("namespace"),
		NamespaceSet:     node.hasAttribute("namespace"),
		EncodingStyle:    soapEncodingStyles(node),
		EncodingStyleSet: node.hasAttribute("encodingStyle"),
		Parts:            splitSpaceSeparated(node.attribute("parts")),
		PartsSet:         node.hasAttribute("parts"),
		Location:         node.location,
	}
}

func decodeSOAPHeader11(node *xmlNode) (SOAPHeader11, error) {
	message, err := node.qnameAttribute("message")
	if err != nil {
		return SOAPHeader11{}, err
	}
	header := SOAPHeader11{
		Version:          soapVersion(node.name.Space),
		Message:          message,
		Part:             node.attribute("part"),
		Use:              SOAPUse(node.attribute("use")),
		UseSet:           node.hasAttribute("use"),
		Namespace:        node.attribute("namespace"),
		NamespaceSet:     node.hasAttribute("namespace"),
		EncodingStyle:    soapEncodingStyles(node),
		EncodingStyleSet: node.hasAttribute("encodingStyle"),
		Location:         node.location,
	}
	for _, child := range node.children {
		if !isSOAPExtension(child, "headerfault") {
			continue
		}
		faultMessage, decodeErr := child.qnameAttribute("message")
		if decodeErr != nil {
			return SOAPHeader11{}, decodeErr
		}
		header.HeaderFaults = append(header.HeaderFaults, SOAPHeaderFault11{
			Version:          soapVersion(child.name.Space),
			Message:          faultMessage,
			Part:             child.attribute("part"),
			Use:              SOAPUse(child.attribute("use")),
			UseSet:           child.hasAttribute("use"),
			Namespace:        child.attribute("namespace"),
			NamespaceSet:     child.hasAttribute("namespace"),
			EncodingStyle:    soapEncodingStyles(child),
			EncodingStyleSet: child.hasAttribute("encodingStyle"),
			Location:         child.location,
		})
	}
	return header, nil
}

func soapEncodingStyles(node *xmlNode) []string {
	if !node.hasAttribute("encodingStyle") {
		return nil
	}
	if soapVersion(node.name.Space) == Version12 {
		return []string{node.attribute("encodingStyle")}
	}
	return splitSpaceSeparated(node.attribute("encodingStyle"))
}

func ensureMIMEMessage11(message *BindingMessage11) *MIMEMessage11 {
	if message.MIME == nil {
		message.MIME = &MIMEMessage11{}
	}
	return message.MIME
}

func decodeMIMEContent11(node *xmlNode) MIMEContent11 {
	return MIMEContent11{
		Part: node.attribute("part"), Type: node.attribute("type"),
		Location: node.location,
	}
}

func decodeMIMEMultipart11(node *xmlNode) MIMEMultipart11 {
	multipart := MIMEMultipart11{Location: node.location}
	for _, child := range node.children {
		if child.name != (xml.Name{Space: NamespaceMIMEBinding, Local: "part"}) {
			continue
		}
		part := MIMEPart11{Location: child.location}
		for _, content := range child.children {
			switch content.name {
			case xml.Name{Space: NamespaceMIMEBinding, Local: "content"}:
				part.Contents = append(part.Contents, decodeMIMEContent11(content))
			case xml.Name{Space: NamespaceMIMEBinding, Local: "mimeXml"}:
				part.XML = append(part.XML, MIMEXML11{
					Part: content.attribute("part"), Location: content.location,
				})
			default:
				if isSOAPExtension(content, "body") {
					body := decodeSOAPBody11(content)
					part.SOAPBody = &body
				}
			}
		}
		multipart.Parts = append(multipart.Parts, part)
	}
	return multipart
}

func decodeService11(node *xmlNode) (Service11, error) {
	extensibility, err := decodeExtensibility(node, NamespaceWSDL11)
	if err != nil {
		return Service11{}, err
	}
	service := Service11{
		Extensibility: extensibility, Name: node.attribute("name"), Location: node.location,
	}
	ports := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			service.Documentation = documentation
			continue
		}
		if child.name != (xml.Name{Space: NamespaceWSDL11, Local: "port"}) {
			continue
		}
		binding, err := child.qnameAttribute("binding")
		if err != nil {
			return Service11{}, err
		}
		portExtensibility, err := decodeExtensibilityExcept(
			child,
			NamespaceWSDL11,
			func(extension *xmlNode) bool {
				return isSOAPExtension(extension, "address") ||
					extension.name == (xml.Name{Space: NamespaceHTTPBinding, Local: "address"})
			},
		)
		if err != nil {
			return Service11{}, err
		}
		port := Port11{
			Extensibility: portExtensibility,
			Name:          child.attribute("name"), Binding: binding, Location: child.location,
		}
		if err := registerSymbol(ports, "service port", port.Name); err != nil {
			return Service11{}, err
		}
		for _, extension := range child.children {
			if documentation := extension.documentation(); documentation != nil {
				port.Documentation = documentation
			} else if isSOAPExtension(extension, "address") {
				port.SOAPAddress = &SOAPAddress11{
					Version:  soapVersion(extension.name.Space),
					Location: extension.attribute("location"), Source: extension.location,
				}
			} else if extension.name == (xml.Name{
				Space: NamespaceHTTPBinding, Local: "address",
			}) {
				port.HTTPAddress = &HTTPAddress11{
					Location: extension.attribute("location"), Source: extension.location,
				}
			}
		}
		service.Ports = append(service.Ports, port)
	}
	return service, nil
}

func isSOAPExtension(node *xmlNode, local string) bool {
	return node.name.Local == local &&
		(node.name.Space == NamespaceSOAP11Binding || node.name.Space == NamespaceSOAP12Binding)
}
