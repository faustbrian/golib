package wsdl

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func decodeDescription20(
	ctx context.Context,
	root *xmlNode,
	options ParseOptions,
) (Description20, error) {
	extensibility, err := decodeExtensibility(root, NamespaceWSDL20)
	if err != nil {
		return Description20{}, err
	}
	description := Description20{
		Extensibility:   extensibility,
		TargetNamespace: root.attribute("targetNamespace"),
		Location:        root.location,
	}
	interfaces := make(map[string]struct{})
	bindings := make(map[string]struct{})
	services := make(map[string]struct{})
	for _, child := range root.children {
		if documentation := child.documentation(); documentation != nil {
			description.Documentation = documentation
			continue
		}
		switch child.name {
		case xml.Name{Space: NamespaceWSDL20, Local: "import"}:
			extensibility, err := decodeExtensibility(child, NamespaceWSDL20)
			if err != nil {
				return Description20{}, err
			}
			location := child.attribute("location")
			uri, err := resolveURI(child.baseURI, location)
			if err != nil {
				return Description20{}, err
			}
			description.Imports = append(description.Imports, Import20{
				Extensibility: extensibility,
				Namespace:     child.attribute("namespace"),
				Location:      location,
				URI:           uri,
				Source:        child.location,
			})
		case xml.Name{Space: NamespaceWSDL20, Local: "include"}:
			extensibility, err := decodeExtensibility(child, NamespaceWSDL20)
			if err != nil {
				return Description20{}, err
			}
			location := child.attribute("location")
			uri, err := resolveURI(child.baseURI, location)
			if err != nil {
				return Description20{}, err
			}
			description.Includes = append(description.Includes, Include20{
				Extensibility: extensibility,
				Location:      location,
				URI:           uri,
				Source:        child.location,
			})
		case xml.Name{Space: NamespaceWSDL20, Local: "types"}:
			value, err := decodeTypes20(ctx, child, options)
			if err != nil {
				return Description20{}, err
			}
			description.Types = &value
		case xml.Name{Space: NamespaceWSDL20, Local: "interface"}:
			value, err := decodeInterface20(child)
			if err != nil {
				return Description20{}, err
			}
			if err := registerSymbol(interfaces, "interface", value.Name); err != nil {
				return Description20{}, err
			}
			description.Interfaces = append(description.Interfaces, value)
		case xml.Name{Space: NamespaceWSDL20, Local: "binding"}:
			value, err := decodeBinding20(child)
			if err != nil {
				return Description20{}, err
			}
			if err := registerSymbol(bindings, "binding", value.Name); err != nil {
				return Description20{}, err
			}
			description.Bindings = append(description.Bindings, value)
		case xml.Name{Space: NamespaceWSDL20, Local: "service"}:
			value, err := decodeService20(child)
			if err != nil {
				return Description20{}, err
			}
			if err := registerSymbol(services, "service", value.Name); err != nil {
				return Description20{}, err
			}
			description.Services = append(description.Services, value)
		}
	}
	return description, nil
}

func decodeTypes20(ctx context.Context, node *xmlNode, options ParseOptions) (Types20, error) {
	types := Types20{
		Extensibility: Extensibility{
			ExtensionAttributes: decodeExtensionAttributes(node, NamespaceWSDL20),
		},
		Location: node.location,
	}
	schemaCount := 0
	for _, child := range node.children {
		if child.name == (xml.Name{Space: NamespaceXMLSchema, Local: "import"}) {
			if schemaCount >= options.MaxSchemas {
				return Types20{}, fmt.Errorf(
					"%w: schema count exceeds %d",
					ErrLimitExceeded,
					options.MaxSchemas,
				)
			}
			location := child.attribute("schemaLocation")
			uri, err := resolveURI(child.baseURI, location)
			if err != nil {
				return Types20{}, fmt.Errorf("wsdl: resolve schema import: %w", err)
			}
			types.Imports = append(types.Imports, xsd.SchemaReference{
				Kind: xsd.ReferenceImport, Namespace: child.attribute("namespace"),
				Location: location, URI: uri,
			})
			schemaCount++
			continue
		}
		if child.name != (xml.Name{Space: NamespaceXMLSchema, Local: "schema"}) {
			if child.name.Space != NamespaceWSDL20 {
				extension, err := decodeExtension(child, NamespaceWSDL20)
				if err != nil {
					return Types20{}, err
				}
				types.Extensions = append(types.Extensions, extension)
			}
			continue
		}
		if schemaCount >= options.MaxSchemas {
			return Types20{}, fmt.Errorf(
				"%w: schema count exceeds %d",
				ErrLimitExceeded,
				options.MaxSchemas,
			)
		}
		source, err := marshalNode(child)
		if err != nil {
			return Types20{}, fmt.Errorf("wsdl: serialize inline schema: %w", err)
		}
		schema, err := xsd.Parse(ctx, source, xsd.ParseOptions{
			SystemID:         options.SystemID,
			MaxDocumentBytes: options.MaxDocumentBytes,
			MaxDepth:         options.MaxDepth,
			MaxElements:      options.MaxElements,
		})
		if err != nil {
			return Types20{}, fmt.Errorf("wsdl: parse inline schema: %w", err)
		}
		types.Schemas = append(types.Schemas, schema)
		schemaCount++
	}
	return types, nil
}

func decodeInterface20(node *xmlNode) (Interface20, error) {
	extends, err := node.qnamesAttribute("extends")
	if err != nil {
		return Interface20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return Interface20{}, err
	}
	value := Interface20{
		Extensibility: extensibility,
		Name:          node.attribute("name"), Extends: extends,
		StyleDefault: splitSpaceSeparated(node.attribute("styleDefault")),
		Location:     node.location,
	}
	faults := make(map[string]struct{})
	operations := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			value.Documentation = documentation
			continue
		}
		switch child.name {
		case xml.Name{Space: NamespaceWSDL20, Local: "fault"}:
			fault, decodeErr := decodeInterfaceFault20(child)
			if decodeErr != nil {
				return Interface20{}, decodeErr
			}
			if err := registerSymbol(faults, "interface fault", fault.Name); err != nil {
				return Interface20{}, err
			}
			value.Faults = append(value.Faults, fault)
		case xml.Name{Space: NamespaceWSDL20, Local: "operation"}:
			operation, decodeErr := decodeInterfaceOperation20(child)
			if decodeErr != nil {
				return Interface20{}, decodeErr
			}
			if err := registerSymbol(
				operations,
				"interface operation",
				operation.Name,
			); err != nil {
				return Interface20{}, err
			}
			value.Operations = append(value.Operations, operation)
		}
	}
	return value, nil
}

func decodeInterfaceFault20(node *xmlNode) (InterfaceFault20, error) {
	element, contentModel, contentModelSet, err := decodeMessageContent20(node)
	if err != nil {
		return InterfaceFault20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return InterfaceFault20{}, err
	}
	value := InterfaceFault20{
		Extensibility: extensibility,
		Name:          node.attribute("name"), Element: element,
		MessageContentModel: contentModel, MessageContentModelSet: contentModelSet,
		Location: node.location,
	}
	value.Documentation = firstDocumentation(node)
	return value, nil
}

func decodeInterfaceOperation20(node *xmlNode) (InterfaceOperation20, error) {
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return InterfaceOperation20{}, err
	}
	operation := InterfaceOperation20{
		Extensibility: extensibility,
		Name:          node.attribute("name"),
		Pattern:       MessageExchangePattern(node.attribute("pattern")),
		Style:         splitSpaceSeparated(node.attribute("style")), Location: node.location,
	}
	safety, safetySet := node.namespacedAttribute(NamespaceWSDL20Extensions, "safe")
	legacySafety, legacySafetySet := node.namespacedAttribute("", "safe")
	if safetySet && legacySafetySet {
		return InterfaceOperation20{}, fmt.Errorf(
			"wsdl: operation safety is present in both adjunct and schema forms",
		)
	}
	if legacySafetySet {
		safety, safetySet = legacySafety, true
	}
	if safetySet {
		safe, valid := xmlBoolean(safety)
		if !valid {
			return InterfaceOperation20{}, fmt.Errorf(
				"wsdl: invalid operation safety value %q", safety,
			)
		}
		operation.Safe = safe
		operation.SafeSet = true
		consumeExtensibility20(
			&operation.Extensibility,
			[]QName{{Namespace: NamespaceWSDL20Extensions, Local: "safe"}},
			nil,
		)
	}
	if lexical, exists := node.namespacedAttribute(NamespaceWSDL20RPC, "signature"); exists {
		items := splitSpaceSeparated(lexical)
		if len(items)%2 != 0 {
			return InterfaceOperation20{}, fmt.Errorf(
				"wsdl: RPC signature must contain QName and direction pairs",
			)
		}
		for index := 0; index < len(items); index += 2 {
			name, parseErr := node.parseQName(items[index])
			if parseErr != nil {
				return InterfaceOperation20{}, parseErr
			}
			direction := RPCDirection(items[index+1])
			if !validRPCDirection20(direction) {
				return InterfaceOperation20{}, fmt.Errorf(
					"wsdl: invalid RPC signature direction %q", direction,
				)
			}
			operation.RPCSignature = append(operation.RPCSignature, RPCSignatureParameter20{
				Name: name, Direction: direction,
			})
		}
		operation.RPCSignatureSet = true
		consumeExtensibility20(
			&operation.Extensibility,
			[]QName{{Namespace: NamespaceWSDL20RPC, Local: "signature"}},
			nil,
		)
	}
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			operation.Documentation = documentation
			continue
		}
		switch child.name {
		case xml.Name{Space: NamespaceWSDL20, Local: "input"}:
			message, err := decodeInterfaceMessage20(child)
			if err != nil {
				return InterfaceOperation20{}, err
			}
			operation.Inputs = append(operation.Inputs, message)
		case xml.Name{Space: NamespaceWSDL20, Local: "output"}:
			message, err := decodeInterfaceMessage20(child)
			if err != nil {
				return InterfaceOperation20{}, err
			}
			operation.Outputs = append(operation.Outputs, message)
		case xml.Name{Space: NamespaceWSDL20, Local: "infault"}:
			fault, err := decodeInterfaceFaultReference20(child)
			if err != nil {
				return InterfaceOperation20{}, err
			}
			operation.InFaults = append(operation.InFaults, fault)
		case xml.Name{Space: NamespaceWSDL20, Local: "outfault"}:
			fault, err := decodeInterfaceFaultReference20(child)
			if err != nil {
				return InterfaceOperation20{}, err
			}
			operation.OutFaults = append(operation.OutFaults, fault)
		}
	}
	if len(operation.Inputs) > 0 {
		operation.Input = &operation.Inputs[0]
	}
	if len(operation.Outputs) > 0 {
		operation.Output = &operation.Outputs[0]
	}
	return operation, nil
}

func validRPCDirection20(value RPCDirection) bool {
	switch value {
	case RPCDirectionIn, RPCDirectionOut, RPCDirectionInOut, RPCDirectionReturn:
		return true
	default:
		return false
	}
}

func decodeInterfaceMessage20(node *xmlNode) (InterfaceMessageReference20, error) {
	element, contentModel, contentModelSet, err := decodeMessageContent20(node)
	if err != nil {
		return InterfaceMessageReference20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return InterfaceMessageReference20{}, err
	}
	value := InterfaceMessageReference20{
		Extensibility: extensibility,
		MessageLabel:  node.attribute("messageLabel"), Element: element,
		MessageContentModel: contentModel, MessageContentModelSet: contentModelSet,
		Location: node.location,
	}
	value.Documentation = firstDocumentation(node)
	return value, nil
}

func decodeMessageContent20(
	node *xmlNode,
) (QName, MessageContentModel, bool, error) {
	if !node.hasAttribute("element") {
		return QName{}, MessageContentOther, false, nil
	}
	lexical := node.attribute("element")
	switch MessageContentModel(lexical) {
	case MessageContentAny, MessageContentNone, MessageContentOther:
		return QName{}, MessageContentModel(lexical), true, nil
	}
	element, err := node.parseQName(lexical)
	if err != nil {
		return QName{}, "", false, err
	}
	return element, MessageContentElement, true, nil
}

func decodeInterfaceFaultReference20(node *xmlNode) (InterfaceFaultReference20, error) {
	reference, err := node.qnameAttribute("ref")
	if err != nil {
		return InterfaceFaultReference20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return InterfaceFaultReference20{}, err
	}
	value := InterfaceFaultReference20{
		Extensibility: extensibility,
		Ref:           reference, MessageLabel: node.attribute("messageLabel"),
		Location: node.location,
	}
	value.Documentation = firstDocumentation(node)
	return value, nil
}

func decodeBinding20(node *xmlNode) (Binding20, error) {
	interfaceName, err := node.qnameAttribute("interface")
	if err != nil {
		return Binding20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return Binding20{}, err
	}
	soap, err := decodeSOAPBinding20(node)
	if err != nil {
		return Binding20{}, err
	}
	http, err := decodeHTTPBinding20(node)
	if err != nil {
		return Binding20{}, err
	}
	consumeExtensibility20(
		&extensibility,
		[]QName{
			{Namespace: NamespaceWSDL20SOAP, Local: "version"},
			{Namespace: NamespaceWSDL20SOAP, Local: "protocol"},
			{Namespace: NamespaceWSDL20SOAP, Local: "mepDefault"},
		},
		[]QName{{Namespace: NamespaceWSDL20SOAP, Local: "module"}},
	)
	consumeExtensibility20(
		&extensibility,
		httpBindingAttributeNames20(),
		nil,
	)
	binding := Binding20{
		Extensibility: extensibility,
		Name:          node.attribute("name"), Interface: interfaceName,
		Type: node.attribute("type"), SOAP: soap, HTTP: http, Location: node.location,
	}
	faults := make(map[string]struct{})
	operations := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			binding.Documentation = documentation
			continue
		}
		switch child.name {
		case xml.Name{Space: NamespaceWSDL20, Local: "fault"}:
			fault, decodeErr := decodeBindingFault20(child)
			if decodeErr != nil {
				return Binding20{}, decodeErr
			}
			if err := registerSymbol(
				faults, "binding fault", formatQName(fault.Ref),
			); err != nil {
				return Binding20{}, err
			}
			binding.Faults = append(binding.Faults, fault)
		case xml.Name{Space: NamespaceWSDL20, Local: "operation"}:
			operation, decodeErr := decodeBindingOperation20(child)
			if decodeErr != nil {
				return Binding20{}, decodeErr
			}
			if err := registerSymbol(
				operations, "binding operation", formatQName(operation.Ref),
			); err != nil {
				return Binding20{}, err
			}
			binding.Operations = append(binding.Operations, operation)
		}
	}
	return binding, nil
}

func decodeBindingFault20(node *xmlNode) (BindingFault20, error) {
	reference, err := node.qnameAttribute("ref")
	if err != nil {
		return BindingFault20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return BindingFault20{}, err
	}
	soap, err := decodeSOAPFaultBinding20(node)
	if err != nil {
		return BindingFault20{}, err
	}
	http, err := decodeHTTPFaultBinding20(node)
	if err != nil {
		return BindingFault20{}, err
	}
	consumeExtensibility20(
		&extensibility,
		[]QName{
			{Namespace: NamespaceWSDL20SOAP, Local: "code"},
			{Namespace: NamespaceWSDL20SOAP, Local: "subcodes"},
		},
		[]QName{
			{Namespace: NamespaceWSDL20SOAP, Local: "module"},
			{Namespace: NamespaceWSDL20SOAP, Local: "header"},
		},
	)
	consumeExtensibility20(
		&extensibility,
		[]QName{
			{Namespace: NamespaceWSDL20HTTP, Local: "code"},
			{Namespace: NamespaceWSDL20HTTP, Local: "contentEncoding"},
			{Namespace: NamespaceWSDL20HTTP, Local: "transferCoding"},
		},
		[]QName{{Namespace: NamespaceWSDL20HTTP, Local: "header"}},
	)
	value := BindingFault20{
		Extensibility: extensibility, Ref: reference, SOAP: soap, HTTP: http,
		Location: node.location,
	}
	value.Documentation = firstDocumentation(node)
	return value, nil
}

func decodeBindingOperation20(node *xmlNode) (BindingOperation20, error) {
	reference, err := node.qnameAttribute("ref")
	if err != nil {
		return BindingOperation20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return BindingOperation20{}, err
	}
	soap, err := decodeSOAPOperationBinding20(node)
	if err != nil {
		return BindingOperation20{}, err
	}
	http, err := decodeHTTPOperationBinding20(node)
	if err != nil {
		return BindingOperation20{}, err
	}
	consumeExtensibility20(
		&extensibility,
		[]QName{
			{Namespace: NamespaceWSDL20SOAP, Local: "mep"},
			{Namespace: NamespaceWSDL20SOAP, Local: "action"},
		},
		[]QName{{Namespace: NamespaceWSDL20SOAP, Local: "module"}},
	)
	consumeExtensibility20(
		&extensibility,
		httpOperationAttributeNames20(),
		nil,
	)
	value := BindingOperation20{
		Extensibility: extensibility, Ref: reference, Location: node.location,
		Documentation: firstDocumentation(node), SOAP: soap, HTTP: http,
	}
	for _, child := range node.children {
		switch child.name {
		case xml.Name{Space: NamespaceWSDL20, Local: "input"}:
			message, decodeErr := decodeBindingMessage20(child)
			if decodeErr != nil {
				return BindingOperation20{}, decodeErr
			}
			value.Inputs = append(value.Inputs, message)
		case xml.Name{Space: NamespaceWSDL20, Local: "output"}:
			message, decodeErr := decodeBindingMessage20(child)
			if decodeErr != nil {
				return BindingOperation20{}, decodeErr
			}
			value.Outputs = append(value.Outputs, message)
		case xml.Name{Space: NamespaceWSDL20, Local: "infault"}:
			fault, decodeErr := decodeBindingFaultReference20(child)
			if decodeErr != nil {
				return BindingOperation20{}, decodeErr
			}
			value.InFaults = append(value.InFaults, fault)
		case xml.Name{Space: NamespaceWSDL20, Local: "outfault"}:
			fault, decodeErr := decodeBindingFaultReference20(child)
			if decodeErr != nil {
				return BindingOperation20{}, decodeErr
			}
			value.OutFaults = append(value.OutFaults, fault)
		}
	}
	return value, nil
}

func decodeBindingMessage20(node *xmlNode) (BindingMessageReference20, error) {
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return BindingMessageReference20{}, err
	}
	soap, err := decodeSOAPMessageBinding20(node)
	if err != nil {
		return BindingMessageReference20{}, err
	}
	http, err := decodeHTTPMessageBinding20(node)
	if err != nil {
		return BindingMessageReference20{}, err
	}
	consumeExtensibility20(
		&extensibility,
		nil,
		[]QName{
			{Namespace: NamespaceWSDL20SOAP, Local: "module"},
			{Namespace: NamespaceWSDL20SOAP, Local: "header"},
		},
	)
	consumeExtensibility20(
		&extensibility,
		[]QName{
			{Namespace: NamespaceWSDL20HTTP, Local: "contentEncoding"},
			{Namespace: NamespaceWSDL20HTTP, Local: "transferCoding"},
		},
		[]QName{{Namespace: NamespaceWSDL20HTTP, Local: "header"}},
	)
	return BindingMessageReference20{
		Extensibility: extensibility,
		MessageLabel:  node.attribute("messageLabel"),
		Documentation: firstDocumentation(node),
		SOAP:          soap,
		HTTP:          http,
		Location:      node.location,
	}, nil
}

func decodeBindingFaultReference20(node *xmlNode) (BindingFaultReference20, error) {
	reference, err := node.qnameAttribute("ref")
	if err != nil {
		return BindingFaultReference20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return BindingFaultReference20{}, err
	}
	soap, err := decodeSOAPFaultReferenceBinding20(node)
	if err != nil {
		return BindingFaultReference20{}, err
	}
	http := decodeHTTPFaultReferenceBinding20(node)
	consumeExtensibility20(
		&extensibility,
		nil,
		[]QName{{Namespace: NamespaceWSDL20SOAP, Local: "module"}},
	)
	consumeExtensibility20(
		&extensibility,
		[]QName{{Namespace: NamespaceWSDL20HTTP, Local: "transferCoding"}},
		nil,
	)
	return BindingFaultReference20{
		Extensibility: extensibility,
		Ref:           reference,
		MessageLabel:  node.attribute("messageLabel"),
		Documentation: firstDocumentation(node),
		SOAP:          soap,
		HTTP:          http,
		Location:      node.location,
	}, nil
}

func decodeHTTPBinding20(node *xmlNode) (*HTTPBinding20, error) {
	value := HTTPBinding20{}
	value.MethodDefault, value.MethodDefaultSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "methodDefault",
	)
	value.Version, value.VersionSet = node.namespacedAttribute(NamespaceWSDL20HTTP, "version")
	value.QueryParameterSeparatorDefault, value.QueryParameterSeparatorDefaultSet =
		node.namespacedAttribute(NamespaceWSDL20HTTP, "queryParameterSeparatorDefault")
	value.ContentEncodingDefault, value.ContentEncodingDefaultSet =
		node.namespacedAttribute(NamespaceWSDL20HTTP, "contentEncodingDefault")
	value.DefaultTransferCoding, value.DefaultTransferCodingSet =
		node.namespacedAttribute(NamespaceWSDL20HTTP, "defaultTransferCoding")
	if err := decodeNamespacedBoolean20(
		node, NamespaceWSDL20HTTP, "cookies", &value.Cookies, &value.CookiesSet,
	); err != nil {
		return nil, err
	}
	if !value.MethodDefaultSet && !value.VersionSet &&
		!value.QueryParameterSeparatorDefaultSet && !value.ContentEncodingDefaultSet &&
		!value.DefaultTransferCodingSet && !value.CookiesSet {
		return nil, nil
	}
	return &value, nil
}

func decodeHTTPFaultBinding20(node *xmlNode) (*HTTPFaultBinding20, error) {
	value := HTTPFaultBinding20{}
	value.Code, value.CodeSet = node.namespacedAttribute(NamespaceWSDL20HTTP, "code")
	value.ContentEncoding, value.ContentEncodingSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "contentEncoding",
	)
	value.TransferCoding, value.TransferCodingSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "transferCoding",
	)
	headers, err := decodeHTTPHeaders20(node)
	if err != nil {
		return nil, err
	}
	value.Headers = headers
	if !value.CodeSet && !value.ContentEncodingSet && !value.TransferCodingSet &&
		len(value.Headers) == 0 {
		return nil, nil
	}
	return &value, nil
}

func decodeHTTPOperationBinding20(node *xmlNode) (*HTTPOperationBinding20, error) {
	value := HTTPOperationBinding20{}
	value.Location, value.LocationSet = node.namespacedAttribute(NamespaceWSDL20HTTP, "location")
	value.Method, value.MethodSet = node.namespacedAttribute(NamespaceWSDL20HTTP, "method")
	value.InputSerialization, value.InputSerializationSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "inputSerialization",
	)
	value.OutputSerialization, value.OutputSerializationSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "outputSerialization",
	)
	value.FaultSerialization, value.FaultSerializationSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "faultSerialization",
	)
	value.QueryParameterSeparator, value.QueryParameterSeparatorSet =
		node.namespacedAttribute(NamespaceWSDL20HTTP, "queryParameterSeparator")
	value.ContentEncodingDefault, value.ContentEncodingDefaultSet =
		node.namespacedAttribute(NamespaceWSDL20HTTP, "contentEncodingDefault")
	value.DefaultTransferCoding, value.DefaultTransferCodingSet =
		node.namespacedAttribute(NamespaceWSDL20HTTP, "defaultTransferCoding")
	if err := decodeNamespacedBoolean20(
		node,
		NamespaceWSDL20HTTP,
		"ignoreUncited",
		&value.IgnoreUncited,
		&value.IgnoreUncitedSet,
	); err != nil {
		return nil, err
	}
	if !value.LocationSet && !value.MethodSet && !value.InputSerializationSet &&
		!value.OutputSerializationSet && !value.FaultSerializationSet &&
		!value.QueryParameterSeparatorSet && !value.ContentEncodingDefaultSet &&
		!value.DefaultTransferCodingSet && !value.IgnoreUncitedSet {
		return nil, nil
	}
	return &value, nil
}

func decodeHTTPMessageBinding20(node *xmlNode) (*HTTPMessageBinding20, error) {
	value := HTTPMessageBinding20{}
	value.ContentEncoding, value.ContentEncodingSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "contentEncoding",
	)
	value.TransferCoding, value.TransferCodingSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "transferCoding",
	)
	headers, err := decodeHTTPHeaders20(node)
	if err != nil {
		return nil, err
	}
	value.Headers = headers
	if !value.ContentEncodingSet && !value.TransferCodingSet && len(value.Headers) == 0 {
		return nil, nil
	}
	return &value, nil
}

func decodeHTTPFaultReferenceBinding20(node *xmlNode) *HTTPFaultReferenceBinding20 {
	value, exists := node.namespacedAttribute(NamespaceWSDL20HTTP, "transferCoding")
	if !exists {
		return nil
	}
	return &HTTPFaultReferenceBinding20{TransferCoding: value, TransferCodingSet: true}
}

func decodeHTTPEndpoint20(node *xmlNode) *HTTPEndpoint20 {
	value := HTTPEndpoint20{}
	value.AuthenticationScheme, value.AuthenticationSchemeSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "authenticationScheme",
	)
	value.AuthenticationRealm, value.AuthenticationRealmSet = node.namespacedAttribute(
		NamespaceWSDL20HTTP, "authenticationRealm",
	)
	if !value.AuthenticationSchemeSet && !value.AuthenticationRealmSet {
		return nil
	}
	return &value
}

func decodeHTTPHeaders20(node *xmlNode) ([]HTTPHeader20, error) {
	headers := make([]HTTPHeader20, 0)
	for _, child := range node.children {
		if child.name != (xml.Name{Space: NamespaceWSDL20HTTP, Local: "header"}) {
			continue
		}
		typeName, err := child.qnameAttribute("type")
		if err != nil {
			return nil, err
		}
		extensibility, err := decodeExtensibility(child, NamespaceWSDL20)
		if err != nil {
			return nil, err
		}
		header := HTTPHeader20{
			Extensibility: extensibility,
			Name:          child.attribute("name"),
			Type:          typeName,
			Documentation: firstDocumentation(child),
			Location:      child.location,
		}
		if err := decodeSOAPBoolean20(
			child, "required", &header.Required, &header.RequiredSet,
		); err != nil {
			return nil, err
		}
		headers = append(headers, header)
	}
	return headers, nil
}

func decodeNamespacedBoolean20(
	node *xmlNode,
	namespace string,
	local string,
	value *bool,
	set *bool,
) error {
	lexical, exists := node.namespacedAttribute(namespace, local)
	if !exists {
		return nil
	}
	decoded, valid := xmlBoolean(lexical)
	if !valid {
		return fmt.Errorf("wsdl: invalid HTTP %s value %q", local, lexical)
	}
	*value = decoded
	*set = true
	return nil
}

func httpBindingAttributeNames20() []QName {
	locals := []string{
		"methodDefault", "version", "queryParameterSeparatorDefault",
		"contentEncodingDefault", "defaultTransferCoding", "cookies",
	}
	return extensionAttributeNames20(NamespaceWSDL20HTTP, locals)
}

func httpOperationAttributeNames20() []QName {
	locals := []string{
		"location", "method", "inputSerialization", "outputSerialization",
		"faultSerialization", "queryParameterSeparator", "contentEncodingDefault",
		"defaultTransferCoding", "ignoreUncited",
	}
	return extensionAttributeNames20(NamespaceWSDL20HTTP, locals)
}

func extensionAttributeNames20(namespace string, locals []string) []QName {
	result := make([]QName, 0, len(locals))
	for _, local := range locals {
		result = append(result, QName{Namespace: namespace, Local: local})
	}
	return result
}

func decodeSOAPBinding20(node *xmlNode) (*SOAPBinding20, error) {
	value := SOAPBinding20{}
	value.Version, value.VersionSet = node.namespacedAttribute(NamespaceWSDL20SOAP, "version")
	value.Protocol, value.ProtocolSet = node.namespacedAttribute(NamespaceWSDL20SOAP, "protocol")
	value.MEPDefault, value.MEPDefaultSet = node.namespacedAttribute(
		NamespaceWSDL20SOAP, "mepDefault",
	)
	modules, err := decodeSOAPModules20(node)
	if err != nil {
		return nil, err
	}
	value.Modules = modules
	if !value.VersionSet && !value.ProtocolSet && !value.MEPDefaultSet &&
		len(value.Modules) == 0 {
		return nil, nil
	}
	return &value, nil
}

func decodeSOAPFaultBinding20(node *xmlNode) (*SOAPFaultBinding20, error) {
	value := SOAPFaultBinding20{}
	if lexical, exists := node.namespacedAttribute(NamespaceWSDL20SOAP, "code"); exists {
		value.CodeSet = true
		if lexical == "#any" {
			value.CodeAny = true
		} else {
			code, err := node.parseQName(lexical)
			if err != nil {
				return nil, err
			}
			value.Code = code
		}
	}
	if lexical, exists := node.namespacedAttribute(NamespaceWSDL20SOAP, "subcodes"); exists {
		value.SubcodesSet = true
		if strings.TrimSpace(lexical) == "#any" {
			value.SubcodesAny = true
		} else {
			for _, item := range splitSpaceSeparated(lexical) {
				code, err := node.parseQName(item)
				if err != nil {
					return nil, err
				}
				value.Subcodes = append(value.Subcodes, code)
			}
		}
	}
	modules, err := decodeSOAPModules20(node)
	if err != nil {
		return nil, err
	}
	value.Modules = modules
	headers, err := decodeSOAPHeaders20(node)
	if err != nil {
		return nil, err
	}
	value.Headers = headers
	if !value.CodeSet && !value.SubcodesSet && len(value.Modules) == 0 &&
		len(value.Headers) == 0 {
		return nil, nil
	}
	return &value, nil
}

func decodeSOAPOperationBinding20(node *xmlNode) (*SOAPOperationBinding20, error) {
	value := SOAPOperationBinding20{}
	value.MEP, value.MEPSet = node.namespacedAttribute(NamespaceWSDL20SOAP, "mep")
	value.Action, value.ActionSet = node.namespacedAttribute(NamespaceWSDL20SOAP, "action")
	modules, err := decodeSOAPModules20(node)
	if err != nil {
		return nil, err
	}
	value.Modules = modules
	if !value.MEPSet && !value.ActionSet && len(value.Modules) == 0 {
		return nil, nil
	}
	return &value, nil
}

func decodeSOAPMessageBinding20(node *xmlNode) (*SOAPMessageBinding20, error) {
	modules, err := decodeSOAPModules20(node)
	if err != nil {
		return nil, err
	}
	headers, err := decodeSOAPHeaders20(node)
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 && len(headers) == 0 {
		return nil, nil
	}
	return &SOAPMessageBinding20{Modules: modules, Headers: headers}, nil
}

func decodeSOAPFaultReferenceBinding20(
	node *xmlNode,
) (*SOAPFaultReferenceBinding20, error) {
	modules, err := decodeSOAPModules20(node)
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 {
		return nil, nil
	}
	return &SOAPFaultReferenceBinding20{Modules: modules}, nil
}

func decodeSOAPModules20(node *xmlNode) ([]SOAPModule20, error) {
	modules := make([]SOAPModule20, 0)
	for _, child := range node.children {
		if child.name != (xml.Name{Space: NamespaceWSDL20SOAP, Local: "module"}) {
			continue
		}
		extensibility, err := decodeExtensibility(child, NamespaceWSDL20)
		if err != nil {
			return nil, err
		}
		module := SOAPModule20{
			Extensibility: extensibility,
			Ref:           child.attribute("ref"),
			Documentation: firstDocumentation(child),
			Location:      child.location,
		}
		if lexical, exists := child.namespacedAttribute("", "required"); exists {
			required, valid := xmlBoolean(lexical)
			if !valid {
				return nil, fmt.Errorf("wsdl: invalid SOAP module required value %q", lexical)
			}
			module.Required = required
			module.RequiredSet = true
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func decodeSOAPHeaders20(node *xmlNode) ([]SOAPHeader20, error) {
	headers := make([]SOAPHeader20, 0)
	for _, child := range node.children {
		if child.name != (xml.Name{Space: NamespaceWSDL20SOAP, Local: "header"}) {
			continue
		}
		element, err := child.qnameAttribute("element")
		if err != nil {
			return nil, err
		}
		extensibility, err := decodeExtensibility(child, NamespaceWSDL20)
		if err != nil {
			return nil, err
		}
		header := SOAPHeader20{
			Extensibility: extensibility,
			Element:       element,
			Documentation: firstDocumentation(child),
			Location:      child.location,
		}
		if err := decodeSOAPBoolean20(
			child, "mustUnderstand", &header.MustUnderstand, &header.MustUnderstandSet,
		); err != nil {
			return nil, err
		}
		if err := decodeSOAPBoolean20(
			child, "required", &header.Required, &header.RequiredSet,
		); err != nil {
			return nil, err
		}
		headers = append(headers, header)
	}
	return headers, nil
}

func decodeSOAPBoolean20(
	node *xmlNode,
	local string,
	value *bool,
	set *bool,
) error {
	lexical, exists := node.namespacedAttribute("", local)
	if !exists {
		return nil
	}
	decoded, valid := xmlBoolean(lexical)
	if !valid {
		return fmt.Errorf("wsdl: invalid SOAP %s value %q", local, lexical)
	}
	*value = decoded
	*set = true
	return nil
}

func consumeExtensibility20(
	value *Extensibility,
	attributes []QName,
	elements []QName,
) {
	attributeNames := make(map[QName]struct{}, len(attributes))
	for _, name := range attributes {
		attributeNames[name] = struct{}{}
	}
	filteredAttributes := value.ExtensionAttributes[:0]
	for _, attribute := range value.ExtensionAttributes {
		if _, consumed := attributeNames[attribute.Name]; !consumed {
			filteredAttributes = append(filteredAttributes, attribute)
		}
	}
	value.ExtensionAttributes = filteredAttributes

	elementNames := make(map[QName]struct{}, len(elements))
	for _, name := range elements {
		elementNames[name] = struct{}{}
	}
	filteredElements := value.Extensions[:0]
	for _, element := range value.Extensions {
		if _, consumed := elementNames[element.Name]; !consumed {
			filteredElements = append(filteredElements, element)
		}
	}
	value.Extensions = filteredElements
}

func firstDocumentation(node *xmlNode) *Documentation {
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			return documentation
		}
	}
	return nil
}

func decodeService20(node *xmlNode) (Service20, error) {
	interfaceName, err := node.qnameAttribute("interface")
	if err != nil {
		return Service20{}, err
	}
	extensibility, err := decodeExtensibility(node, NamespaceWSDL20)
	if err != nil {
		return Service20{}, err
	}
	service := Service20{
		Extensibility: extensibility,
		Name:          node.attribute("name"), Interface: interfaceName, Location: node.location,
	}
	endpoints := make(map[string]struct{})
	for _, child := range node.children {
		if documentation := child.documentation(); documentation != nil {
			service.Documentation = documentation
			continue
		}
		if child.name != (xml.Name{Space: NamespaceWSDL20, Local: "endpoint"}) {
			continue
		}
		binding, decodeErr := child.qnameAttribute("binding")
		if decodeErr != nil {
			return Service20{}, decodeErr
		}
		name := child.attribute("name")
		if err := registerSymbol(endpoints, "service endpoint", name); err != nil {
			return Service20{}, err
		}
		extensibility, decodeErr := decodeExtensibility(child, NamespaceWSDL20)
		if decodeErr != nil {
			return Service20{}, decodeErr
		}
		http := decodeHTTPEndpoint20(child)
		consumeExtensibility20(
			&extensibility,
			[]QName{
				{Namespace: NamespaceWSDL20HTTP, Local: "authenticationScheme"},
				{Namespace: NamespaceWSDL20HTTP, Local: "authenticationRealm"},
			},
			nil,
		)
		endpoint := Endpoint20{
			Extensibility: extensibility,
			Name:          name, Binding: binding,
			Address: child.attribute("address"), HTTP: http, Location: child.location,
		}
		endpoint.Documentation = firstDocumentation(child)
		service.Endpoints = append(service.Endpoints, endpoint)
	}
	return service, nil
}
