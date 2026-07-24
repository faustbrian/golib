package wsdl

import (
	"fmt"
	"mime"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const defaultMaxDiagnostics = 1000

// ValidationOptions controls semantic validation of one parsed document.
type ValidationOptions struct {
	MaxDiagnostics       int
	UnderstoodExtensions []QName
}

// Validate checks the semantic constraints and component references in a
// parsed WSDL document. It never resolves imports or reads external resources.
func Validate(document *Document, options ValidationOptions) Diagnostics {
	if options.MaxDiagnostics < 0 {
		collector := diagnosticCollector{max: 1}
		collector.add(Diagnostic{
			Code: "WSDL_OPTIONS", Severity: SeverityError,
			Message: "maximum diagnostics must not be negative",
		})
		return collector.diagnostics
	}
	maxDiagnostics := options.MaxDiagnostics
	if maxDiagnostics == 0 {
		maxDiagnostics = defaultMaxDiagnostics
	}
	collector := diagnosticCollector{max: maxDiagnostics}
	if document == nil {
		collector.add(Diagnostic{
			Code: "WSDL_DOCUMENT", Severity: SeverityError,
			Message: "document is nil",
		})
		return collector.diagnostics
	}
	understood := make(map[QName]struct{}, len(options.UnderstoodExtensions))
	for _, name := range options.UnderstoodExtensions {
		understood[name] = struct{}{}
	}
	validateRequiredExtensions(document, understood, &collector)
	if definitions, ok := document.Definitions11(); ok {
		validateDefinitions11(definitions, &collector)
	}
	if description, ok := document.Description20(); ok {
		validateDescription20(description, &collector)
	}
	return collector.diagnostics
}

func validateRequiredExtensions(
	document *Document,
	understood map[QName]struct{},
	collector *diagnosticCollector,
) {
	check := func(value Extensibility) {
		for _, extension := range value.Extensions {
			if !extension.RequiredSet || !extension.Required {
				continue
			}
			if _, ok := understood[extension.Name]; ok {
				continue
			}
			collector.add(Diagnostic{
				Code: "WSDL_EXTENSION_REQUIRED", Severity: SeverityError,
				Message: fmt.Sprintf(
					"required extension %s is not understood",
					formatQName(extension.Name),
				),
				Location: extension.Location,
			})
		}
	}
	checkSOAPModules := func(values []SOAPModule20) {
		for _, value := range values {
			check(value.Extensibility)
		}
	}
	checkSOAPHeaders := func(values []SOAPHeader20) {
		for _, value := range values {
			check(value.Extensibility)
		}
	}
	checkHTTPHeaders := func(values []HTTPHeader20) {
		for _, value := range values {
			check(value.Extensibility)
		}
	}
	if definitions, ok := document.Definitions11(); ok {
		check(Extensibility{
			Extensions:          definitions.Extensions,
			ExtensionAttributes: definitions.ExtensionAttributes,
		})
		for _, importValue := range definitions.Imports {
			check(importValue.Extensibility)
		}
		if definitions.Types != nil {
			check(definitions.Types.Extensibility)
		}
		for _, message := range definitions.Messages {
			check(message.Extensibility)
			for _, part := range message.Parts {
				check(part.Extensibility)
			}
		}
		for _, portType := range definitions.PortTypes {
			check(portType.Extensibility)
			for _, operation := range portType.Operations {
				check(operation.Extensibility)
				if operation.Input != nil {
					check(operation.Input.Extensibility)
				}
				if operation.Output != nil {
					check(operation.Output.Extensibility)
				}
				for _, fault := range operation.Faults {
					check(fault.Extensibility)
				}
			}
		}
		for _, binding := range definitions.Bindings {
			check(binding.Extensibility)
			for _, operation := range binding.Operations {
				check(operation.Extensibility)
				if operation.Input != nil {
					check(operation.Input.Extensibility)
				}
				if operation.Output != nil {
					check(operation.Output.Extensibility)
				}
				for _, fault := range operation.Faults {
					check(fault.Extensibility)
				}
			}
		}
		for _, service := range definitions.Services {
			check(service.Extensibility)
			for _, port := range service.Ports {
				check(port.Extensibility)
			}
		}
		return
	}
	description, _ := document.Description20()
	check(description.Extensibility)
	for _, importValue := range description.Imports {
		check(importValue.Extensibility)
	}
	for _, include := range description.Includes {
		check(include.Extensibility)
	}
	if description.Types != nil {
		check(description.Types.Extensibility)
	}
	for _, interfaceValue := range description.Interfaces {
		check(interfaceValue.Extensibility)
		for _, fault := range interfaceValue.Faults {
			check(fault.Extensibility)
		}
		for _, operation := range interfaceValue.Operations {
			check(operation.Extensibility)
			for _, message := range interfaceInputs20(operation) {
				check(message.Extensibility)
			}
			for _, message := range interfaceOutputs20(operation) {
				check(message.Extensibility)
			}
			for _, fault := range operation.InFaults {
				check(fault.Extensibility)
			}
			for _, fault := range operation.OutFaults {
				check(fault.Extensibility)
			}
		}
	}
	for _, binding := range description.Bindings {
		check(binding.Extensibility)
		if binding.SOAP != nil {
			checkSOAPModules(binding.SOAP.Modules)
		}
		for _, fault := range binding.Faults {
			check(fault.Extensibility)
			if fault.SOAP != nil {
				checkSOAPModules(fault.SOAP.Modules)
				checkSOAPHeaders(fault.SOAP.Headers)
			}
			if fault.HTTP != nil {
				checkHTTPHeaders(fault.HTTP.Headers)
			}
		}
		for _, operation := range binding.Operations {
			check(operation.Extensibility)
			if operation.SOAP != nil {
				checkSOAPModules(operation.SOAP.Modules)
			}
			for _, message := range operation.Inputs {
				check(message.Extensibility)
				if message.SOAP != nil {
					checkSOAPModules(message.SOAP.Modules)
					checkSOAPHeaders(message.SOAP.Headers)
				}
				if message.HTTP != nil {
					checkHTTPHeaders(message.HTTP.Headers)
				}
			}
			for _, message := range operation.Outputs {
				check(message.Extensibility)
				if message.SOAP != nil {
					checkSOAPModules(message.SOAP.Modules)
					checkSOAPHeaders(message.SOAP.Headers)
				}
				if message.HTTP != nil {
					checkHTTPHeaders(message.HTTP.Headers)
				}
			}
			for _, fault := range operation.InFaults {
				check(fault.Extensibility)
				if fault.SOAP != nil {
					checkSOAPModules(fault.SOAP.Modules)
				}
			}
			for _, fault := range operation.OutFaults {
				check(fault.Extensibility)
				if fault.SOAP != nil {
					checkSOAPModules(fault.SOAP.Modules)
				}
			}
		}
	}
	for _, service := range description.Services {
		check(service.Extensibility)
		for _, endpoint := range service.Endpoints {
			check(endpoint.Extensibility)
		}
	}
}

type diagnosticCollector struct {
	max         int
	diagnostics Diagnostics
}

func (c *diagnosticCollector) add(diagnostic Diagnostic) {
	if len(c.diagnostics) >= c.max {
		return
	}
	c.diagnostics = append(c.diagnostics, diagnostic)
}

func validateDefinitions11(definitions Definitions11, collector *diagnosticCollector) {
	messages := make(map[QName]struct{}, len(definitions.Messages))
	messageParts := make(map[QName]map[string]struct{}, len(definitions.Messages))
	for _, message := range definitions.Messages {
		name := QName{Namespace: definitions.TargetNamespace, Local: message.Name}
		messages[name] = struct{}{}
		parts := make(map[string]struct{}, len(message.Parts))
		messageParts[name] = parts
		for _, part := range message.Parts {
			parts[part.Name] = struct{}{}
			if (part.Element.Local == "") == (part.Type.Local == "") {
				collector.add(Diagnostic{
					Code: "WSDL11_PART_CONTENT", Severity: SeverityError,
					Message: fmt.Sprintf(
						"message %q part %q must specify exactly one of element or type",
						message.Name,
						part.Name,
					),
					Path:     "/definitions/message[@name='" + message.Name + "']/part[@name='" + part.Name + "']",
					Location: part.Location,
				})
			}
		}
	}

	portTypes := make(map[QName]struct{}, len(definitions.PortTypes))
	portTypeValues := make(map[QName]PortType11, len(definitions.PortTypes))
	for _, portType := range definitions.PortTypes {
		name := QName{Namespace: definitions.TargetNamespace, Local: portType.Name}
		portTypes[name] = struct{}{}
		portTypeValues[name] = portType
		operationSignatures := make(map[string]struct{}, len(portType.Operations))
		for _, operation := range portType.Operations {
			input, output := operationMessageNames11(operation)
			signature := operation.Name + "\x00" + input + "\x00" + output
			if _, exists := operationSignatures[signature]; exists {
				collector.add(Diagnostic{
					Code: "WSDL11_OPERATION_DUPLICATE", Severity: SeverityError,
					Message: fmt.Sprintf(
						"port type %q repeats operation signature %q (%q, %q)",
						portType.Name, operation.Name, input, output,
					),
					Location: operation.Location,
				})
			} else {
				operationSignatures[signature] = struct{}{}
			}
			validateOperationStyle11(portType.Name, operation, collector)
			validateOperationMessage11(
				definitions.TargetNamespace,
				portType.Name,
				operation.Name,
				operation.Input,
				messages,
				collector,
			)
			validateOperationMessage11(
				definitions.TargetNamespace,
				portType.Name,
				operation.Name,
				operation.Output,
				messages,
				collector,
			)
			for index := range operation.Faults {
				validateOperationMessage11(
					definitions.TargetNamespace,
					portType.Name,
					operation.Name,
					&operation.Faults[index],
					messages,
					collector,
				)
			}
		}
	}

	bindings := make(map[QName]struct{}, len(definitions.Bindings))
	for _, binding := range definitions.Bindings {
		bindings[QName{Namespace: definitions.TargetNamespace, Local: binding.Name}] = struct{}{}
		validateBindingProperties11(binding, collector)
		if isLocalQName(binding.Type, definitions.TargetNamespace) {
			if _, exists := portTypes[binding.Type]; !exists {
				collector.add(Diagnostic{
					Code: "WSDL11_PORT_TYPE_REFERENCE", Severity: SeverityError,
					Message: fmt.Sprintf(
						"binding %q references unknown port type %s",
						binding.Name,
						formatQName(binding.Type),
					),
					Path:     "/definitions/binding[@name='" + binding.Name + "']",
					Location: binding.Location,
				})
			}
		}
		if portType, exists := portTypeValues[binding.Type]; exists {
			for _, operation := range binding.Operations {
				validateBindingOperation11(
					binding.Name,
					operation,
					portType,
					definitions.TargetNamespace,
					messageParts,
					collector,
				)
			}
		}
	}

	for _, service := range definitions.Services {
		for _, port := range service.Ports {
			validateEndpointAddresses11(port, collector)
			if !isLocalQName(port.Binding, definitions.TargetNamespace) {
				continue
			}
			if _, exists := bindings[port.Binding]; exists {
				continue
			}
			collector.add(Diagnostic{
				Code: "WSDL11_BINDING_REFERENCE", Severity: SeverityError,
				Message: fmt.Sprintf(
					"service %q port %q references unknown binding %s",
					service.Name,
					port.Name,
					formatQName(port.Binding),
				),
				Path:     "/definitions/service[@name='" + service.Name + "']/port[@name='" + port.Name + "']",
				Location: port.Location,
			})
		}
	}
}

func validateOperationStyle11(
	portType string,
	operation Operation11,
	collector *diagnosticCollector,
) {
	expected := operationStyle11(operation.Input, operation.Output, nil)
	if operation.Style == "" || expected == "" || operation.Style != expected {
		collector.add(Diagnostic{
			Code: "WSDL11_OPERATION_STYLE", Severity: SeverityError,
			Message: fmt.Sprintf(
				"port type %q operation %q has style %q but its messages imply %q",
				portType, operation.Name, operation.Style, expected,
			),
			Location: operation.Location,
		})
	}
	if len(operation.Faults) > 0 &&
		(operation.Style == OperationStyleOneWay || operation.Style == OperationStyleNotification) {
		collector.add(Diagnostic{
			Code: "WSDL11_OPERATION_FAULT", Severity: SeverityError,
			Message: fmt.Sprintf(
				"port type %q operation %q cannot declare faults for style %q",
				portType, operation.Name, operation.Style,
			),
			Location: operation.Location,
		})
	}
}

func validateBindingOperation11(
	bindingName string,
	bound BindingOperation11,
	portType PortType11,
	targetNamespace string,
	messageParts map[QName]map[string]struct{},
	collector *diagnosticCollector,
) {
	abstract := matchingOperation11(bound, portType.Operations)
	if abstract == nil {
		collector.add(Diagnostic{
			Code: "WSDL11_BINDING_OPERATION_REFERENCE", Severity: SeverityError,
			Message: fmt.Sprintf(
				"binding %q operation %q has no matching port type operation",
				bindingName,
				bound.Name,
			),
			Location: bound.Location,
		})
		return
	}
	validateSOAPBindingMessage11(
		bound.Input,
		abstract.Input,
		targetNamespace,
		messageParts,
		collector,
	)
	validateSOAPBindingMessage11(
		bound.Output,
		abstract.Output,
		targetNamespace,
		messageParts,
		collector,
	)
	for index := range bound.Faults {
		fault := &bound.Faults[index]
		var abstractFault *OperationMessage11
		for abstractIndex := range abstract.Faults {
			if abstract.Faults[abstractIndex].Name == fault.Name {
				abstractFault = &abstract.Faults[abstractIndex]
				break
			}
		}
		validateSOAPBindingMessage11(
			fault,
			abstractFault,
			targetNamespace,
			messageParts,
			collector,
		)
		if fault.SOAPFault != nil && fault.SOAPFault.Name != fault.Name {
			collector.add(Diagnostic{
				Code: "WSDL11_SOAP_FAULT_NAME", Severity: SeverityError,
				Message: fmt.Sprintf(
					"bound fault %q uses SOAP fault name %q",
					fault.Name,
					fault.SOAPFault.Name,
				),
				Location: fault.SOAPFault.Location,
			})
		}
	}
}

func matchingOperation11(bound BindingOperation11, operations []Operation11) *Operation11 {
	candidates := make([]*Operation11, 0, 1)
	for index := range operations {
		operation := &operations[index]
		if operation.Name != bound.Name {
			continue
		}
		input, output := operationMessageNames11(*operation)
		if bound.Input != nil && bound.Input.Name != "" && bound.Input.Name != input {
			continue
		}
		if bound.Output != nil && bound.Output.Name != "" && bound.Output.Name != output {
			continue
		}
		candidates = append(candidates, operation)
	}
	if len(candidates) != 1 {
		return nil
	}
	return candidates[0]
}

func operationMessageNames11(operation Operation11) (string, string) {
	input, output := "", ""
	if operation.Input != nil {
		input = operation.Input.Name
	}
	if operation.Output != nil {
		output = operation.Output.Name
	}
	switch operation.Style {
	case OperationStyleOneWay:
		if input == "" {
			input = operation.Name
		}
	case OperationStyleRequestResponse:
		if input == "" {
			input = operation.Name + "Request"
		}
		if output == "" {
			output = operation.Name + "Response"
		}
	case OperationStyleSolicitResponse:
		if input == "" {
			input = operation.Name + "Response"
		}
		if output == "" {
			output = operation.Name + "Solicit"
		}
	case OperationStyleNotification:
		if output == "" {
			output = operation.Name
		}
	}
	return input, output
}

func validateSOAPBindingMessage11(
	bound *BindingMessage11,
	abstract *OperationMessage11,
	targetNamespace string,
	messageParts map[QName]map[string]struct{},
	collector *diagnosticCollector,
) {
	if bound == nil {
		return
	}
	if bound.SOAPBody != nil && abstract != nil {
		parts, knownMessage := messageParts[abstract.Message]
		if !knownMessage {
			parts = nil
		}
		for _, part := range bound.SOAPBody.Parts {
			if !knownMessage {
				continue
			}
			if _, exists := parts[part]; exists {
				continue
			}
			collector.add(Diagnostic{
				Code: "WSDL11_SOAP_BODY_PART", Severity: SeverityError,
				Message: fmt.Sprintf(
					"SOAP body references unknown message part %q",
					part,
				),
				Location: bound.SOAPBody.Location,
			})
		}
	}
	if bound.SOAPBody != nil {
		validateSOAPUse11(bound.SOAPBody.Use, bound.SOAPBody.UseSet, bound.SOAPBody.Location, collector)
	}
	if abstract != nil {
		validateMIMEMessage11(bound.MIME, messageParts[abstract.Message], bound.Location, collector)
	}
	for _, header := range bound.SOAPHeaders {
		validateSOAPUse11(header.Use, header.UseSet, header.Location, collector)
		validateSOAPHeaderReference11(
			header.Message,
			header.Part,
			header.Location,
			targetNamespace,
			messageParts,
			collector,
		)
		for _, fault := range header.HeaderFaults {
			validateSOAPUse11(fault.Use, fault.UseSet, fault.Location, collector)
			validateSOAPHeaderReference11(
				fault.Message,
				fault.Part,
				fault.Location,
				targetNamespace,
				messageParts,
				collector,
			)
		}
	}
}

func validateBindingProperties11(binding Binding11, collector *diagnosticCollector) {
	if binding.SOAP != nil && binding.HTTP != nil {
		collector.add(Diagnostic{
			Code: "WSDL11_BINDING_PROTOCOL", Severity: SeverityError,
			Message:  fmt.Sprintf("binding %q declares both SOAP and HTTP protocols", binding.Name),
			Location: binding.Location,
		})
	}
	if binding.SOAP != nil {
		if binding.SOAP.StyleSet && binding.SOAP.Style != SOAPStyleDocument &&
			binding.SOAP.Style != SOAPStyleRPC {
			collector.add(Diagnostic{
				Code: "WSDL11_SOAP_STYLE", Severity: SeverityError,
				Message:  fmt.Sprintf("binding %q has invalid SOAP style %q", binding.Name, binding.SOAP.Style),
				Location: binding.SOAP.Location,
			})
		}
		if !binding.SOAP.TransportSet || !isAbsoluteIRI(binding.SOAP.Transport) {
			collector.add(Diagnostic{
				Code: "WSDL11_SOAP_TRANSPORT", Severity: SeverityError,
				Message:  fmt.Sprintf("binding %q SOAP transport must be an absolute URI", binding.Name),
				Location: binding.SOAP.Location,
			})
		}
	}
	if binding.HTTP != nil && !isHTTPToken20(binding.HTTP.Verb) {
		collector.add(Diagnostic{
			Code: "WSDL11_HTTP_VERB", Severity: SeverityError,
			Message:  fmt.Sprintf("binding %q HTTP verb is not a valid token", binding.Name),
			Location: binding.HTTP.Location,
		})
	}
	for _, operation := range binding.Operations {
		if operation.SOAP != nil && operation.SOAP.StyleSet &&
			operation.SOAP.Style != SOAPStyleDocument && operation.SOAP.Style != SOAPStyleRPC {
			collector.add(Diagnostic{
				Code: "WSDL11_SOAP_STYLE", Severity: SeverityError,
				Message:  fmt.Sprintf("binding operation %q has invalid SOAP style %q", operation.Name, operation.SOAP.Style),
				Location: operation.SOAP.Location,
			})
		}
		for _, message := range operation.Faults {
			if message.SOAPFault != nil {
				validateSOAPUse11(message.SOAPFault.Use, message.SOAPFault.UseSet, message.SOAPFault.Location, collector)
			}
		}
	}
}

func validateSOAPUse11(
	use SOAPUse,
	set bool,
	location Location,
	collector *diagnosticCollector,
) {
	if set && use != SOAPUseLiteral && use != SOAPUseEncoded {
		collector.add(Diagnostic{
			Code: "WSDL11_SOAP_USE", Severity: SeverityError,
			Message:  fmt.Sprintf("SOAP use %q must be literal or encoded", use),
			Location: location,
		})
	}
}

func validateMIMEMessage11(
	value *MIMEMessage11,
	parts map[string]struct{},
	location Location,
	collector *diagnosticCollector,
) {
	if value == nil {
		return
	}
	validateContents := func(contents []MIMEContent11) {
		for _, content := range contents {
			if _, exists := parts[content.Part]; !exists {
				collector.add(Diagnostic{
					Code: "WSDL11_MIME_PART", Severity: SeverityError,
					Message:  fmt.Sprintf("MIME content references unknown message part %q", content.Part),
					Location: content.Location,
				})
			}
			mediaType, _, err := mime.ParseMediaType(content.Type)
			if err != nil || !strings.Contains(mediaType, "/") {
				collector.add(Diagnostic{
					Code: "WSDL11_MIME_TYPE", Severity: SeverityError,
					Message:  fmt.Sprintf("MIME content type %q is invalid", content.Type),
					Location: content.Location,
				})
			}
		}
	}
	validateXML := func(values []MIMEXML11) {
		for _, value := range values {
			if _, exists := parts[value.Part]; !exists {
				collector.add(Diagnostic{
					Code: "WSDL11_MIME_PART", Severity: SeverityError,
					Message:  fmt.Sprintf("MIME XML references unknown message part %q", value.Part),
					Location: value.Location,
				})
			}
		}
	}
	validateContents(value.Contents)
	validateXML(value.XML)
	for _, multipart := range value.Multipart {
		if len(multipart.Parts) == 0 {
			collector.add(Diagnostic{
				Code: "WSDL11_MIME_MULTIPART", Severity: SeverityError,
				Message: "MIME multipart/related has no parts", Location: location,
			})
		}
		for _, part := range multipart.Parts {
			if part.SOAPBody != nil {
				validateSOAPUse11(
					part.SOAPBody.Use, part.SOAPBody.UseSet,
					part.SOAPBody.Location, collector,
				)
				for _, name := range part.SOAPBody.Parts {
					if _, exists := parts[name]; !exists {
						collector.add(Diagnostic{
							Code: "WSDL11_SOAP_BODY_PART", Severity: SeverityError,
							Message:  fmt.Sprintf("SOAP body references unknown message part %q", name),
							Location: part.SOAPBody.Location,
						})
					}
				}
			}
			validateContents(part.Contents)
			validateXML(part.XML)
		}
	}
}

func validateEndpointAddresses11(port Port11, collector *diagnosticCollector) {
	var addresses []struct {
		value    string
		location Location
	}
	if port.SOAPAddress != nil {
		addresses = append(addresses, struct {
			value    string
			location Location
		}{port.SOAPAddress.Location, port.SOAPAddress.Source})
	}
	if port.HTTPAddress != nil {
		addresses = append(addresses, struct {
			value    string
			location Location
		}{port.HTTPAddress.Location, port.HTTPAddress.Source})
	}
	for _, address := range addresses {
		if !isAbsoluteIRI(address.value) {
			collector.add(Diagnostic{
				Code: "WSDL11_ENDPOINT_ADDRESS", Severity: SeverityError,
				Message:  fmt.Sprintf("endpoint address %q is not an absolute URI", address.value),
				Location: address.location,
			})
		}
	}
}

func validateSOAPHeaderReference11(
	message QName,
	part string,
	location Location,
	targetNamespace string,
	messageParts map[QName]map[string]struct{},
	collector *diagnosticCollector,
) {
	if !isLocalQName(message, targetNamespace) {
		return
	}
	parts, exists := messageParts[message]
	if !exists {
		collector.add(Diagnostic{
			Code: "WSDL11_SOAP_HEADER_MESSAGE", Severity: SeverityError,
			Message: fmt.Sprintf(
				"SOAP header references unknown message %s",
				formatQName(message),
			),
			Location: location,
		})
		return
	}
	if _, exists := parts[part]; exists {
		return
	}
	collector.add(Diagnostic{
		Code: "WSDL11_SOAP_HEADER_PART", Severity: SeverityError,
		Message: fmt.Sprintf(
			"SOAP header references unknown part %q in message %s",
			part,
			formatQName(message),
		),
		Location: location,
	})
}

func validateOperationMessage11(
	targetNamespace string,
	portType string,
	operation string,
	message *OperationMessage11,
	messages map[QName]struct{},
	collector *diagnosticCollector,
) {
	if message == nil || !isLocalQName(message.Message, targetNamespace) {
		return
	}
	if _, exists := messages[message.Message]; exists {
		return
	}
	collector.add(Diagnostic{
		Code: "WSDL11_MESSAGE_REFERENCE", Severity: SeverityError,
		Message: fmt.Sprintf(
			"port type %q operation %q references unknown message %s",
			portType,
			operation,
			formatQName(message.Message),
		),
		Location: message.Location,
	})
}

func isLocalQName(name QName, targetNamespace string) bool {
	return name.Local != "" && (targetNamespace == "" || name.Namespace == targetNamespace)
}

func formatQName(name QName) string {
	return "{" + name.Namespace + "}" + name.Local
}

func validateDescription20(description Description20, collector *diagnosticCollector) {
	interfaces := make(map[QName]struct{}, len(description.Interfaces))
	interfaceValues := make(map[QName]Interface20, len(description.Interfaces))
	operations := make(map[QName]struct{})
	faults := make(map[QName]struct{})
	for _, interfaceValue := range description.Interfaces {
		interfaceName := QName{
			Namespace: description.TargetNamespace, Local: interfaceValue.Name,
		}
		interfaces[interfaceName] = struct{}{}
		interfaceValues[interfaceName] = interfaceValue
		for _, fault := range interfaceValue.Faults {
			faults[QName{
				Namespace: description.TargetNamespace, Local: fault.Name,
			}] = struct{}{}
		}
		for _, operation := range interfaceValue.Operations {
			operations[QName{
				Namespace: description.TargetNamespace, Local: operation.Name,
			}] = struct{}{}
		}
	}
	validateInterfaceGraph20(description, interfaceValues, interfaces, collector)
	for _, interfaceValue := range description.Interfaces {
		for _, operation := range interfaceValue.Operations {
			validateOperationStyles20(interfaceValue, operation, collector)
			if !isAbsoluteIRI(string(operation.Pattern)) {
				collector.add(Diagnostic{
					Code: "WSDL20_PATTERN_IRI", Severity: SeverityError,
					Message: fmt.Sprintf(
						"interface %q operation %q pattern %q is not an absolute IRI",
						interfaceValue.Name,
						operation.Name,
						operation.Pattern,
					),
					Location: operation.Location,
				})
			}
			validateMessageExchangePattern20(interfaceValue.Name, operation, collector)
			for _, fault := range operation.InFaults {
				validateFaultReference20(
					interfaceValue.Name,
					operation.Name,
					fault,
					description.TargetNamespace,
					faults,
					collector,
				)
			}
			for _, fault := range operation.OutFaults {
				validateFaultReference20(
					interfaceValue.Name,
					operation.Name,
					fault,
					description.TargetNamespace,
					faults,
					collector,
				)
			}
		}
	}

	bindings := make(map[QName]struct{}, len(description.Bindings))
	for _, binding := range description.Bindings {
		bindings[QName{
			Namespace: description.TargetNamespace, Local: binding.Name,
		}] = struct{}{}
		validateInterfaceReference20(
			"binding "+fmt.Sprintf("%q", binding.Name),
			binding.Interface,
			binding.Location,
			description.TargetNamespace,
			interfaces,
			collector,
		)
		validateSOAPBinding20(binding, collector)
		validateHTTPBinding20(binding, collector)
		boundInterface, hasBoundInterface := interfaceValues[binding.Interface]
		boundFaults := make(map[QName]struct{}, len(boundInterface.Faults))
		boundOperations := make(map[QName]InterfaceOperation20, len(boundInterface.Operations))
		if hasBoundInterface {
			for _, fault := range boundInterface.Faults {
				boundFaults[QName{
					Namespace: description.TargetNamespace, Local: fault.Name,
				}] = struct{}{}
			}
			for _, operation := range boundInterface.Operations {
				boundOperations[QName{
					Namespace: description.TargetNamespace, Local: operation.Name,
				}] = operation
			}
		}
		for _, fault := range binding.Faults {
			if !hasBoundInterface || !isLocalQName(fault.Ref, description.TargetNamespace) {
				continue
			}
			if _, exists := boundFaults[fault.Ref]; exists {
				continue
			}
			collector.add(Diagnostic{
				Code: "WSDL20_BINDING_FAULT", Severity: SeverityError,
				Message: fmt.Sprintf(
					"binding %q references unknown interface fault %s",
					binding.Name,
					formatQName(fault.Ref),
				),
				Location: fault.Location,
			})
		}
		for _, operation := range binding.Operations {
			if !isLocalQName(operation.Ref, description.TargetNamespace) {
				continue
			}
			if _, exists := operations[operation.Ref]; exists {
				if abstractOperation, found := boundOperations[operation.Ref]; found {
					validateBindingOperation20(
						binding.Name, operation, abstractOperation, collector,
					)
				}
				continue
			}
			collector.add(Diagnostic{
				Code: "WSDL20_OPERATION_REFERENCE", Severity: SeverityError,
				Message: fmt.Sprintf(
					"binding %q references unknown interface operation %s",
					binding.Name,
					formatQName(operation.Ref),
				),
				Location: operation.Location,
			})
		}
	}

	for _, service := range description.Services {
		validateInterfaceReference20(
			"service "+fmt.Sprintf("%q", service.Name),
			service.Interface,
			service.Location,
			description.TargetNamespace,
			interfaces,
			collector,
		)
		for _, endpoint := range service.Endpoints {
			if endpoint.Address != "" && !isAbsoluteIRI(endpoint.Address) {
				collector.add(Diagnostic{
					Code: "WSDL20_ENDPOINT_ADDRESS", Severity: SeverityError,
					Message:  fmt.Sprintf("endpoint address %q is not an absolute IRI", endpoint.Address),
					Location: endpoint.Location,
				})
			}
			validateHTTPEndpoint20(endpoint, collector)
			if !isLocalQName(endpoint.Binding, description.TargetNamespace) {
				continue
			}
			if _, exists := bindings[endpoint.Binding]; exists {
				continue
			}
			collector.add(Diagnostic{
				Code: "WSDL20_BINDING_REFERENCE", Severity: SeverityError,
				Message: fmt.Sprintf(
					"service %q endpoint %q references unknown binding %s",
					service.Name,
					endpoint.Name,
					formatQName(endpoint.Binding),
				),
				Location: endpoint.Location,
			})
		}
	}
}

func validateInterfaceGraph20(
	description Description20,
	values map[QName]Interface20,
	names map[QName]struct{},
	collector *diagnosticCollector,
) {
	for _, value := range description.Interfaces {
		name := QName{Namespace: description.TargetNamespace, Local: value.Name}
		for _, parent := range value.Extends {
			if isLocalQName(parent, description.TargetNamespace) {
				if _, exists := names[parent]; !exists {
					collector.add(Diagnostic{
						Code: "WSDL20_INTERFACE_EXTENDS", Severity: SeverityError,
						Message:  fmt.Sprintf("interface %q extends unknown interface %s", name.Local, formatQName(parent)),
						Location: value.Location,
					})
				}
			}
		}
		for _, style := range value.StyleDefault {
			validateStyleIRI20(style, value.Location, collector)
		}
	}
	state := make(map[QName]uint8, len(values))
	var visit func(QName)
	visit = func(name QName) {
		if state[name] == 2 {
			return
		}
		if state[name] == 1 {
			value := values[name]
			collector.add(Diagnostic{
				Code: "WSDL20_INTERFACE_CYCLE", Severity: SeverityError,
				Message:  fmt.Sprintf("interface inheritance cycle includes %s", formatQName(name)),
				Location: value.Location,
			})
			return
		}
		value, exists := values[name]
		if !exists {
			return
		}
		state[name] = 1
		for _, parent := range value.Extends {
			if isLocalQName(parent, description.TargetNamespace) {
				visit(parent)
			}
		}
		state[name] = 2
	}
	for _, value := range description.Interfaces {
		visit(QName{Namespace: description.TargetNamespace, Local: value.Name})
	}
}

func validateOperationStyles20(
	interfaceValue Interface20,
	operation InterfaceOperation20,
	collector *diagnosticCollector,
) {
	styles := operation.Style
	if len(styles) == 0 {
		styles = interfaceValue.StyleDefault
	}
	rpc := false
	for _, style := range styles {
		validateStyleIRI20(style, operation.Location, collector)
		switch style {
		case StyleIRI:
			validateElementOperationStyle20("IRI", operation, collector)
		case StyleMultipart:
			validateElementOperationStyle20("MULTIPART", operation, collector)
		case StyleRPC:
			rpc = true
			if operation.Pattern != MEPInOut && operation.Pattern != MEPInOnly {
				collector.add(Diagnostic{
					Code: "WSDL20_RPC_STYLE_PATTERN", Severity: SeverityError,
					Message: fmt.Sprintf(
						"RPC operation %q must use the in-only or in-out message exchange pattern",
						operation.Name,
					),
					Location: operation.Location,
				})
			}
		}
	}
	validateRPCStyle20(operation, rpc, collector)
}

func validateElementOperationStyle20(
	style string,
	operation InterfaceOperation20,
	collector *diagnosticCollector,
) {
	message := initialMessage20(operation)
	if message == nil {
		return
	}
	if message.MessageContentModel != MessageContentElement {
		collector.add(Diagnostic{
			Code: "WSDL20_" + style + "_MESSAGE_CONTENT", Severity: SeverityError,
			Message:  style + " style requires an #element initial message",
			Location: message.Location,
		})
		return
	}
	if message.Element.Local != operation.Name {
		collector.add(Diagnostic{
			Code: "WSDL20_" + style + "_INITIAL_NAME", Severity: SeverityError,
			Message: fmt.Sprintf(
				"%s style initial element %s must have local name %q",
				style, formatQName(message.Element), operation.Name,
			),
			Location: message.Location,
		})
	}
}

func initialMessage20(operation InterfaceOperation20) *InterfaceMessageReference20 {
	inputs := interfaceInputs20(operation)
	outputs := interfaceOutputs20(operation)
	switch operation.Pattern {
	case MEPOutOnly, MEPRobustOutOnly, MEPOutIn, MEPOutOptionalIn:
		if len(outputs) > 0 {
			return &outputs[0]
		}
	default:
		if len(inputs) > 0 {
			return &inputs[0]
		}
	}
	if len(outputs) > 0 {
		return &outputs[0]
	}
	return nil
}

func validateRPCStyle20(
	operation InterfaceOperation20,
	rpc bool,
	collector *diagnosticCollector,
) {
	if !rpc {
		if operation.RPCSignatureSet {
			collector.add(Diagnostic{
				Code: "WSDL20_RPC_SIGNATURE_STYLE", Severity: SeverityError,
				Message:  fmt.Sprintf("operation %q has an RPC signature without RPC style", operation.Name),
				Location: operation.Location,
			})
		}
		return
	}
	if !operation.RPCSignatureSet {
		collector.add(Diagnostic{
			Code: "WSDL20_RPC_SIGNATURE_REQUIRED", Severity: SeverityError,
			Message:  fmt.Sprintf("RPC operation %q requires an RPC signature", operation.Name),
			Location: operation.Location,
		})
	}
	inputs := interfaceInputs20(operation)
	outputs := interfaceOutputs20(operation)
	for _, message := range append(append(
		[]InterfaceMessageReference20(nil), inputs...), outputs...,
	) {
		if message.MessageContentModel != MessageContentElement {
			collector.add(Diagnostic{
				Code: "WSDL20_RPC_MESSAGE_CONTENT", Severity: SeverityError,
				Message:  "RPC messages must use the #element content model",
				Location: message.Location,
			})
		}
	}
	if len(inputs) > 0 && inputs[0].Element.Local != operation.Name {
		collector.add(Diagnostic{
			Code: "WSDL20_RPC_INPUT_NAME", Severity: SeverityError,
			Message: fmt.Sprintf(
				"RPC input element %s must have local name %q",
				formatQName(inputs[0].Element), operation.Name,
			),
			Location: inputs[0].Location,
		})
	}
	if len(inputs) > 0 && len(outputs) > 0 &&
		inputs[0].Element.Namespace != outputs[0].Element.Namespace {
		collector.add(Diagnostic{
			Code: "WSDL20_RPC_MESSAGE_NAMESPACE", Severity: SeverityError,
			Message:  "RPC input and output elements must use the same namespace",
			Location: outputs[0].Location,
		})
	}
	seen := make(map[QName]struct{}, len(operation.RPCSignature))
	for _, parameter := range operation.RPCSignature {
		if !validRPCDirection20(parameter.Direction) {
			collector.add(Diagnostic{
				Code: "WSDL20_RPC_SIGNATURE_DIRECTION", Severity: SeverityError,
				Message:  fmt.Sprintf("invalid RPC signature direction %q", parameter.Direction),
				Location: operation.Location,
			})
		}
		if parameter.Name.Local == "" {
			collector.add(Diagnostic{
				Code: "WSDL20_RPC_SIGNATURE_NAME", Severity: SeverityError,
				Message:  "RPC signature parameter requires a QName",
				Location: operation.Location,
			})
		}
		if _, exists := seen[parameter.Name]; exists {
			collector.add(Diagnostic{
				Code: "WSDL20_RPC_SIGNATURE_DUPLICATE", Severity: SeverityError,
				Message:  fmt.Sprintf("duplicate RPC signature parameter %s", formatQName(parameter.Name)),
				Location: operation.Location,
			})
		}
		seen[parameter.Name] = struct{}{}
	}
}

func validateStyleIRI20(value string, location Location, collector *diagnosticCollector) {
	if isAbsoluteIRI(value) {
		return
	}
	collector.add(Diagnostic{
		Code: "WSDL20_STYLE_IRI", Severity: SeverityError,
		Message:  fmt.Sprintf("operation style %q is not an absolute IRI", value),
		Location: location,
	})
}

type predefinedMEP20 struct {
	input, output     bool
	inFault, outFault bool
}

func validateMessageExchangePattern20(
	interfaceName string,
	operation InterfaceOperation20,
	collector *diagnosticCollector,
) {
	patterns := map[MessageExchangePattern]predefinedMEP20{
		MEPInOnly:        {input: true},
		MEPRobustInOnly:  {input: true, outFault: true},
		MEPInOut:         {input: true, output: true, outFault: true},
		MEPInOptionalOut: {input: true, output: true, outFault: true},
		MEPOutOnly:       {output: true},
		MEPRobustOutOnly: {output: true, inFault: true},
		MEPOutIn:         {input: true, output: true, inFault: true},
		MEPOutOptionalIn: {input: true, output: true, inFault: true},
	}
	pattern, known := patterns[operation.Pattern]
	if !known {
		validateCustomMessageLabels20(operation, collector)
		return
	}
	path := "/description/interface[@name='" + interfaceName +
		"']/operation[@name='" + operation.Name + "']"
	validateMEPMessages20(path, "input", interfaceInputs20(operation), pattern.input, collector)
	validateMEPMessages20(path, "output", interfaceOutputs20(operation), pattern.output, collector)
	if len(operation.InFaults) > 0 && !pattern.inFault {
		collector.add(Diagnostic{
			Code: "WSDL20_MEP_INFAULT", Severity: SeverityError,
			Message: fmt.Sprintf("message exchange pattern %q does not allow infault", operation.Pattern),
			Path:    path, Location: operation.Location,
		})
	}
	if len(operation.OutFaults) > 0 && !pattern.outFault {
		collector.add(Diagnostic{
			Code: "WSDL20_MEP_OUTFAULT", Severity: SeverityError,
			Message: fmt.Sprintf("message exchange pattern %q does not allow outfault", operation.Pattern),
			Path:    path, Location: operation.Location,
		})
	}
}

func validateMEPMessages20(
	path string,
	direction string,
	messages []InterfaceMessageReference20,
	required bool,
	collector *diagnosticCollector,
) {
	if len(messages) == 0 {
		if required {
			collector.add(Diagnostic{
				Code: "WSDL20_MEP_" + strings.ToUpper(direction), Severity: SeverityError,
				Message: "message exchange pattern requires an " + direction + " message",
				Path:    path,
			})
		}
		return
	}
	if !required || len(messages) != 1 {
		location := messages[0].Location
		collector.add(Diagnostic{
			Code: "WSDL20_MEP_" + strings.ToUpper(direction), Severity: SeverityError,
			Message: fmt.Sprintf("message exchange pattern requires exactly %d %s message", btoi(required), direction),
			Path:    path, Location: location,
		})
		return
	}
	message := messages[0]
	want := "In"
	if direction == "output" {
		want = "Out"
	}
	if message.MessageLabel != "" && message.MessageLabel != want {
		collector.add(Diagnostic{
			Code: "WSDL20_MESSAGE_LABEL", Severity: SeverityError,
			Message: fmt.Sprintf(
				"%s message label %q does not match predefined label %q",
				direction, message.MessageLabel, want,
			),
			Path: path, Location: message.Location,
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func validateCustomMessageLabels20(
	operation InterfaceOperation20,
	collector *diagnosticCollector,
) {
	seen := make(map[string]struct{})
	for _, group := range [][]InterfaceMessageReference20{
		interfaceInputs20(operation), interfaceOutputs20(operation),
	} {
		multiple := len(group) > 1
		for _, message := range group {
			if multiple && message.MessageLabel == "" {
				collector.add(Diagnostic{
					Code: "WSDL20_MESSAGE_LABEL", Severity: SeverityError,
					Message:  "multiple messages in one direction require explicit labels",
					Location: message.Location,
				})
				continue
			}
			if message.MessageLabel == "" {
				continue
			}
			if _, exists := seen[message.MessageLabel]; exists {
				collector.add(Diagnostic{
					Code: "WSDL20_MESSAGE_LABEL", Severity: SeverityError,
					Message:  fmt.Sprintf("duplicate message label %q", message.MessageLabel),
					Location: message.Location,
				})
			}
			seen[message.MessageLabel] = struct{}{}
		}
	}
}

func validateHTTPBinding20(binding Binding20, collector *diagnosticCollector) {
	if binding.HTTP != nil {
		if binding.HTTP.MethodDefaultSet && !isHTTPToken20(binding.HTTP.MethodDefault) {
			addHTTPDiagnostic20(
				"WSDL20_HTTP_METHOD", "HTTP method default is not a valid token",
				binding.Location, collector,
			)
		}
		if binding.HTTP.VersionSet && !isHTTPVersion20(binding.HTTP.Version) {
			addHTTPDiagnostic20(
				"WSDL20_HTTP_VERSION", "HTTP version must have major.minor form",
				binding.Location, collector,
			)
		}
		if binding.HTTP.QueryParameterSeparatorDefaultSet {
			validateHTTPQuerySeparator20(
				binding.HTTP.QueryParameterSeparatorDefault,
				binding.Location,
				collector,
			)
		}
	}
	for _, fault := range binding.Faults {
		if fault.HTTP == nil {
			continue
		}
		if fault.HTTP.CodeSet && fault.HTTP.Code != "#any" {
			code, err := strconv.Atoi(fault.HTTP.Code)
			if err != nil || code < 100 || code > 599 {
				addHTTPDiagnostic20(
					"WSDL20_HTTP_STATUS_CODE",
					"HTTP fault status code must be #any or an integer from 100 to 599",
					fault.Location,
					collector,
				)
			}
		}
		validateHTTPHeaders20(fault.HTTP.Headers, collector)
	}
	for _, operation := range binding.Operations {
		if operation.HTTP != nil {
			if operation.HTTP.MethodSet && !isHTTPToken20(operation.HTTP.Method) {
				addHTTPDiagnostic20(
					"WSDL20_HTTP_METHOD", "HTTP method is not a valid token",
					operation.Location, collector,
				)
			}
			if operation.HTTP.QueryParameterSeparatorSet {
				validateHTTPQuerySeparator20(
					operation.HTTP.QueryParameterSeparator,
					operation.Location,
					collector,
				)
			}
		}
		for _, message := range operation.Inputs {
			if message.HTTP != nil {
				validateHTTPHeaders20(message.HTTP.Headers, collector)
			}
		}
		for _, message := range operation.Outputs {
			if message.HTTP != nil {
				validateHTTPHeaders20(message.HTTP.Headers, collector)
			}
		}
	}
}

func validateHTTPEndpoint20(endpoint Endpoint20, collector *diagnosticCollector) {
	if endpoint.HTTP == nil {
		return
	}
	if endpoint.HTTP.AuthenticationSchemeSet &&
		endpoint.HTTP.AuthenticationScheme != "basic" &&
		endpoint.HTTP.AuthenticationScheme != "digest" {
		addHTTPDiagnostic20(
			"WSDL20_HTTP_AUTHENTICATION_SCHEME",
			"HTTP authentication scheme must be basic or digest",
			endpoint.Location,
			collector,
		)
	}
	if endpoint.HTTP.AuthenticationRealmSet &&
		!endpoint.HTTP.AuthenticationSchemeSet {
		addHTTPDiagnostic20(
			"WSDL20_HTTP_AUTHENTICATION_REALM",
			"HTTP authentication realm requires an authentication scheme",
			endpoint.Location,
			collector,
		)
	}
}

func validateHTTPHeaders20(values []HTTPHeader20, collector *diagnosticCollector) {
	for _, value := range values {
		if !isHTTPToken20(value.Name) {
			addHTTPDiagnostic20(
				"WSDL20_HTTP_HEADER_NAME", "HTTP header name is not a valid token",
				value.Location, collector,
			)
		}
		if value.Type.Local == "" {
			addHTTPDiagnostic20(
				"WSDL20_HTTP_HEADER_TYPE", "HTTP header does not reference a type",
				value.Location, collector,
			)
		}
	}
}

func validateHTTPQuerySeparator20(
	value string,
	location Location,
	collector *diagnosticCollector,
) {
	const allowed = "&;abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"0123456789-._~!$'():@/?*+,"
	if utf8.RuneCountInString(value) == 1 && strings.ContainsRune(allowed, []rune(value)[0]) {
		return
	}
	addHTTPDiagnostic20(
		"WSDL20_HTTP_QUERY_SEPARATOR",
		"HTTP query parameter separator must be one permitted character",
		location,
		collector,
	)
}

func isHTTPVersion20(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func isHTTPToken20(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character >= 127 || character <= 32 ||
			strings.ContainsRune("()<>@,;:\\\"/[]?={}", character) {
			return false
		}
	}
	return true
}

func addHTTPDiagnostic20(
	code string,
	message string,
	location Location,
	collector *diagnosticCollector,
) {
	collector.add(Diagnostic{
		Code: code, Severity: SeverityError, Message: message, Location: location,
	})
}

func validateSOAPBinding20(binding Binding20, collector *diagnosticCollector) {
	if binding.Type == NamespaceWSDL20SOAP {
		if binding.SOAP == nil || !binding.SOAP.ProtocolSet {
			collector.add(Diagnostic{
				Code: "WSDL20_SOAP_PROTOCOL_REQUIRED", Severity: SeverityError,
				Message: fmt.Sprintf(
					"SOAP binding %q does not declare wsoap:protocol",
					binding.Name,
				),
				Location: binding.Location,
			})
		}
	}
	if binding.SOAP != nil {
		if binding.SOAP.VersionSet && binding.SOAP.Version != "1.1" &&
			binding.SOAP.Version != "1.2" {
			collector.add(Diagnostic{
				Code: "WSDL20_SOAP_VERSION", Severity: SeverityError,
				Message: fmt.Sprintf(
					"SOAP binding %q has unsupported version %q",
					binding.Name,
					binding.SOAP.Version,
				),
				Location: binding.Location,
			})
		}
		if binding.SOAP.ProtocolSet {
			validateSOAPIRI20(
				"WSDL20_SOAP_PROTOCOL_IRI",
				"SOAP protocol",
				binding.SOAP.Protocol,
				binding.Location,
				collector,
			)
		}
		if binding.SOAP.MEPDefaultSet {
			validateSOAPIRI20(
				"WSDL20_SOAP_MEP_IRI",
				"SOAP default message exchange pattern",
				binding.SOAP.MEPDefault,
				binding.Location,
				collector,
			)
		}
		validateSOAPModules20(binding.SOAP.Modules, collector)
	}
	for _, fault := range binding.Faults {
		if fault.SOAP == nil {
			continue
		}
		validateSOAPModules20(fault.SOAP.Modules, collector)
		validateSOAPHeaders20(fault.SOAP.Headers, collector)
	}
	for _, operation := range binding.Operations {
		if operation.SOAP != nil {
			if operation.SOAP.MEPSet {
				validateSOAPIRI20(
					"WSDL20_SOAP_MEP_IRI",
					"SOAP message exchange pattern",
					operation.SOAP.MEP,
					operation.Location,
					collector,
				)
			}
			if operation.SOAP.ActionSet {
				validateSOAPIRI20(
					"WSDL20_SOAP_ACTION_IRI",
					"SOAP action",
					operation.SOAP.Action,
					operation.Location,
					collector,
				)
			}
			validateSOAPModules20(operation.SOAP.Modules, collector)
		}
		for _, message := range operation.Inputs {
			validateSOAPMessage20(message.SOAP, collector)
		}
		for _, message := range operation.Outputs {
			validateSOAPMessage20(message.SOAP, collector)
		}
		for _, reference := range operation.InFaults {
			if reference.SOAP != nil {
				validateSOAPModules20(reference.SOAP.Modules, collector)
			}
		}
		for _, reference := range operation.OutFaults {
			if reference.SOAP != nil {
				validateSOAPModules20(reference.SOAP.Modules, collector)
			}
		}
	}
}

func validateSOAPMessage20(
	value *SOAPMessageBinding20,
	collector *diagnosticCollector,
) {
	if value == nil {
		return
	}
	validateSOAPModules20(value.Modules, collector)
	validateSOAPHeaders20(value.Headers, collector)
}

func validateSOAPModules20(values []SOAPModule20, collector *diagnosticCollector) {
	for _, value := range values {
		validateSOAPIRI20(
			"WSDL20_SOAP_MODULE_IRI",
			"SOAP module reference",
			value.Ref,
			value.Location,
			collector,
		)
	}
}

func validateSOAPHeaders20(values []SOAPHeader20, collector *diagnosticCollector) {
	for _, value := range values {
		if value.Element.Local != "" {
			continue
		}
		collector.add(Diagnostic{
			Code: "WSDL20_SOAP_HEADER_ELEMENT", Severity: SeverityError,
			Message:  "SOAP header does not reference an element",
			Location: value.Location,
		})
	}
}

func validateSOAPIRI20(
	code string,
	property string,
	value string,
	location Location,
	collector *diagnosticCollector,
) {
	if isAbsoluteIRI(value) {
		return
	}
	collector.add(Diagnostic{
		Code: code, Severity: SeverityError,
		Message:  fmt.Sprintf("%s %q is not an absolute IRI", property, value),
		Location: location,
	})
}

func validateBindingOperation20(
	bindingName string,
	bindingOperation BindingOperation20,
	interfaceOperation InterfaceOperation20,
	collector *diagnosticCollector,
) {
	validateBindingMessages20(
		bindingName, bindingOperation.Ref, "input", bindingOperation.Inputs,
		interfaceOperation.Input, collector,
	)
	validateBindingMessages20(
		bindingName, bindingOperation.Ref, "output", bindingOperation.Outputs,
		interfaceOperation.Output, collector,
	)
	validateBindingFaults20(
		bindingName, bindingOperation.Ref, bindingOperation.InFaults,
		interfaceOperation.InFaults, collector,
	)
	validateBindingFaults20(
		bindingName, bindingOperation.Ref, bindingOperation.OutFaults,
		interfaceOperation.OutFaults, collector,
	)
}

func validateBindingMessages20(
	bindingName string,
	operationName QName,
	direction string,
	references []BindingMessageReference20,
	message *InterfaceMessageReference20,
	collector *diagnosticCollector,
) {
	for _, reference := range references {
		if message != nil &&
			(reference.MessageLabel == "" || reference.MessageLabel == message.MessageLabel) {
			continue
		}
		collector.add(Diagnostic{
			Code: "WSDL20_BINDING_MESSAGE_REFERENCE", Severity: SeverityError,
			Message: fmt.Sprintf(
				"binding %q operation %s has unknown %s message label %q",
				bindingName,
				formatQName(operationName),
				direction,
				reference.MessageLabel,
			),
			Location: reference.Location,
		})
	}
}

func validateBindingFaults20(
	bindingName string,
	operationName QName,
	references []BindingFaultReference20,
	interfaceReferences []InterfaceFaultReference20,
	collector *diagnosticCollector,
) {
	for _, reference := range references {
		matched := false
		for _, interfaceReference := range interfaceReferences {
			if reference.Ref == interfaceReference.Ref &&
				(reference.MessageLabel == "" ||
					reference.MessageLabel == interfaceReference.MessageLabel) {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		collector.add(Diagnostic{
			Code: "WSDL20_BINDING_FAULT_REFERENCE", Severity: SeverityError,
			Message: fmt.Sprintf(
				"binding %q operation %s references unknown interface fault %s",
				bindingName,
				formatQName(operationName),
				formatQName(reference.Ref),
			),
			Location: reference.Location,
		})
	}
}

func validateFaultReference20(
	interfaceName string,
	operationName string,
	fault InterfaceFaultReference20,
	targetNamespace string,
	faults map[QName]struct{},
	collector *diagnosticCollector,
) {
	if !isLocalQName(fault.Ref, targetNamespace) {
		return
	}
	if _, exists := faults[fault.Ref]; exists {
		return
	}
	collector.add(Diagnostic{
		Code: "WSDL20_FAULT_REFERENCE", Severity: SeverityError,
		Message: fmt.Sprintf(
			"interface %q operation %q references unknown fault %s",
			interfaceName,
			operationName,
			formatQName(fault.Ref),
		),
		Location: fault.Location,
	})
}

func validateInterfaceReference20(
	owner string,
	reference QName,
	location Location,
	targetNamespace string,
	interfaces map[QName]struct{},
	collector *diagnosticCollector,
) {
	if !isLocalQName(reference, targetNamespace) {
		return
	}
	if _, exists := interfaces[reference]; exists {
		return
	}
	collector.add(Diagnostic{
		Code: "WSDL20_INTERFACE_REFERENCE", Severity: SeverityError,
		Message: fmt.Sprintf(
			"%s references unknown interface %s",
			owner,
			formatQName(reference),
		),
		Location: location,
	})
}

func isAbsoluteIRI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs()
}
