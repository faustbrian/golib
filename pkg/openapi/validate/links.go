package validate

import (
	"context"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/expression"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type linkValidator struct {
	version           string
	dialect           specversion.Dialect
	operationIDs      map[string]int
	operationPointers map[string][]string
	operations        map[string]struct{}
	ctx               context.Context
	resource          reference.Resource
	resolver          reference.Resolver
	limits            reference.Limits
	parameters        map[string]map[string]struct{}
	diagnostics       []Diagnostic
}

func validateLinks(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	switch document.SpecificationVersion().Dialect() {
	case specversion.DialectSwagger20:
		return nil
	}
	validator := linkValidator{
		version:           document.SpecificationVersion().String(),
		dialect:           document.SpecificationVersion().Dialect(),
		operationIDs:      make(map[string]int),
		operationPointers: make(map[string][]string),
		operations:        make(map[string]struct{}),
		ctx:               ctx,
		resource:          validationResource(document, options.ReferenceResourceURI),
		resolver:          options.ReferenceResolver,
		limits:            options.ReferenceLimits,
		parameters:        make(map[string]map[string]struct{}),
	}
	externalOperations := externalDocumentOperations(ctx, document, options)
	validator.collectOperationIDs(document.Raw())
	for _, operation := range externalOperations {
		validator.registerOperationID(operation)
	}
	validator.visitDocument(document.Raw())
	for _, operation := range externalOperations {
		validator.parameters[operation.pointer] = validator.declaredParametersFrom(
			operation.resource,
			operation.pathItem,
			operation.value,
		)
		validator.visitOperationResponses(operation)
	}
	return validator.diagnostics
}

func (validator *linkValidator) collectOperationIDs(root jsonvalue.Value) {
	validator.visitDocumentPathItems(root, validator.collectOperationID)
	components, ok := objectMember(root, "components")
	if !ok {
		return
	}
	callbacks, ok := objectMember(components, "callbacks")
	if !ok {
		return
	}
	members, _ := callbacks.Members()
	for _, member := range members {
		validator.collectCallbackOperationIDs(
			member.Value,
			"/components/callbacks/"+escapePointer(member.Name),
			validator.collectOperationID,
		)
	}
}

func (validator *linkValidator) collectOperationID(operation operationLocation) {
	validator.registerOperationID(operation)
	callbacks, ok := objectMember(operation.value, "callbacks")
	if !ok {
		return
	}
	members, _ := callbacks.Members()
	for _, member := range members {
		validator.collectCallbackOperationIDs(
			member.Value,
			operation.pointer+"/callbacks/"+escapePointer(member.Name),
			validator.collectOperationID,
		)
	}
}

func (validator *linkValidator) registerOperationID(
	operation operationLocation,
) {
	validator.operations[operation.pointer] = struct{}{}
	if identifier, ok := stringMember(operation.value, "operationId"); ok {
		pointers := append(
			validator.operationPointers[identifier],
			operation.pointer,
		)
		validator.operationPointers[identifier] = pointers
		validator.operationIDs[identifier] = len(pointers)
	}
}

func (validator *linkValidator) collectCallbackOperationIDs(
	callback jsonvalue.Value,
	pointer string,
	visit func(operationLocation),
) {
	members, valid := callback.Members()
	if !valid {
		return
	}
	for _, member := range members {
		validator.visitPathItem(
			member.Value,
			pointer+"/"+escapePointer(member.Name),
			visit,
		)
	}
}

func (validator *linkValidator) visitDocument(root jsonvalue.Value) {
	validator.visitDocumentPathItems(root, validator.visitOperation)
	components, ok := objectMember(root, "components")
	if !ok {
		return
	}
	validator.visitComponentMap(components, "callbacks", validator.visitCallback)
	validator.visitComponentMap(components, "links", validator.visitLink)
	validator.visitComponentMap(components, "responses", validator.visitResponse)
}

func (validator *linkValidator) visitDocumentPathItems(
	root jsonvalue.Value,
	visit func(operationLocation),
) {
	for _, collection := range []string{"paths", "webhooks"} {
		items, ok := objectMember(root, collection)
		if !ok {
			continue
		}
		members, _ := items.Members()
		for _, member := range members {
			validator.visitPathItem(
				member.Value,
				"/"+collection+"/"+escapePointer(member.Name),
				visit,
			)
		}
	}
	components, ok := objectMember(root, "components")
	if !ok {
		return
	}
	pathItems, ok := objectMember(components, "pathItems")
	if !ok {
		return
	}
	members, _ := pathItems.Members()
	for _, member := range members {
		validator.visitPathItem(
			member.Value,
			"/components/pathItems/"+escapePointer(member.Name),
			visit,
		)
	}
}

func (validator *linkValidator) visitPathItem(
	pathItem jsonvalue.Value,
	pointer string,
	visit func(operationLocation),
) {
	if pathItem.Kind() != jsonvalue.ObjectKind || isReference(pathItem) {
		return
	}
	for _, operation := range operationsAt(pathItem, pointer, validator.dialect) {
		validator.parameters[operation.pointer] = validator.declaredParameters(
			pathItem,
			operation.value,
		)
		visit(operation)
	}
}

func (validator *linkValidator) declaredParameters(
	pathItem jsonvalue.Value,
	operation jsonvalue.Value,
) map[string]struct{} {
	return validator.declaredParametersFrom(
		validator.resource,
		pathItem,
		operation,
	)
}

func (validator *linkValidator) declaredParametersFrom(
	resource reference.Resource,
	pathItem jsonvalue.Value,
	operation jsonvalue.Value,
) map[string]struct{} {
	result := make(map[string]struct{})
	for _, owner := range []jsonvalue.Value{pathItem, operation} {
		parameters, exists := owner.Lookup("parameters")
		if !exists || parameters.Kind() != jsonvalue.ArrayKind {
			continue
		}
		elements, _ := parameters.Elements()
		for _, parameter := range elements {
			resolved, _, ok := validator.resolveObjectFrom(resource, parameter)
			if !ok {
				continue
			}
			location, hasLocation := stringMember(resolved, "in")
			name, hasName := stringMember(resolved, "name")
			if !hasLocation || !hasName {
				continue
			}
			result[requestParameterKey(location, name)] = struct{}{}
		}
	}
	return result
}

func (validator *linkValidator) visitOperation(operation operationLocation) {
	callbacks, ok := objectMember(operation.value, "callbacks")
	if ok {
		members, _ := callbacks.Members()
		for _, member := range members {
			validator.visitCallback(
				member.Value,
				operation.pointer+"/callbacks/"+escapePointer(member.Name),
			)
		}
	}
	validator.visitOperationResponses(operation)
}

func (validator *linkValidator) visitOperationResponses(
	operation operationLocation,
) {
	responses, ok := objectMember(operation.value, "responses")
	if !ok {
		return
	}
	members, _ := responses.Members()
	for _, member := range members {
		validator.visitResponseWithParameters(
			member.Value,
			operation.pointer+"/responses/"+escapePointer(member.Name),
			validator.parameters[operation.pointer],
		)
	}
}

func (validator *linkValidator) visitCallback(callback jsonvalue.Value, pointer string) {
	if callback.Kind() != jsonvalue.ObjectKind || isReference(callback) {
		return
	}
	members, _ := callback.Members()
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		template, err := expression.ParseTemplate(member.Name)
		if err != nil || !singleExpression(template) {
			validator.add(
				"openapi.callback.expression.invalid",
				memberPointer,
				"callback key is not a valid runtime expression template",
				"callback-object",
			)
		}
		validator.visitPathItem(member.Value, memberPointer, validator.visitOperation)
	}
}

func (validator *linkValidator) visitResponse(response jsonvalue.Value, pointer string) {
	validator.visitResponseWithParameters(response, pointer, nil)
}

func (validator *linkValidator) visitResponseWithParameters(
	response jsonvalue.Value,
	pointer string,
	parameters map[string]struct{},
) {
	if response.Kind() != jsonvalue.ObjectKind {
		return
	}
	if isReference(response) {
		if parameters == nil {
			return
		}
		resolved, resource, ok := validator.resolveObjectFrom(
			validator.resource,
			response,
		)
		if !ok {
			return
		}
		validator.validateResponseLinkExpressionsAtUse(
			resolved,
			resource,
			pointer+"/$ref",
			parameters,
		)
		return
	}
	links, ok := objectMember(response, "links")
	if !ok {
		return
	}
	members, _ := links.Members()
	for _, member := range members {
		memberPointer := pointer + "/links/" + escapePointer(member.Name)
		if isReference(member.Value) {
			resolved, _, ok := validator.resolveObjectFrom(
				validator.resource,
				member.Value,
			)
			if ok {
				validator.validateLinkExpressionsAtUse(
					resolved,
					memberPointer+"/$ref",
					parameters,
				)
			}
		}
		validator.visitLinkWithParameters(
			member.Value,
			memberPointer,
			parameters,
		)
	}
}

func (validator *linkValidator) validateResponseLinkExpressionsAtUse(
	response jsonvalue.Value,
	resource reference.Resource,
	pointer string,
	parameters map[string]struct{},
) {
	links, exists := objectMember(response, "links")
	if !exists {
		return
	}
	members, _ := links.Members()
	for _, member := range members {
		resolved, _, ok := validator.resolveObjectFrom(
			resource,
			member.Value,
		)
		if !ok {
			continue
		}
		validator.validateLinkExpressionsAtUse(
			resolved,
			pointer,
			parameters,
		)
	}
}

func (validator *linkValidator) validateLinkExpressionsAtUse(
	link jsonvalue.Value,
	pointer string,
	parameters map[string]struct{},
) {
	if linkParameters, exists := objectMember(link, "parameters"); exists {
		members, _ := linkParameters.Members()
		for _, member := range members {
			validator.validateLinkExpression(member.Value, pointer, parameters)
		}
	}
	if requestBody, exists := link.Lookup("requestBody"); exists {
		validator.validateLinkExpression(requestBody, pointer, parameters)
	}
}

func (validator *linkValidator) resolveObjectFrom(
	resource reference.Resource,
	value jsonvalue.Value,
) (jsonvalue.Value, reference.Resource, bool) {
	if value.Kind() != jsonvalue.ObjectKind {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	referenceValue, exists := value.Lookup("$ref")
	if !exists {
		return value, resource, true
	}
	rawReference, ok := referenceValue.Text()
	if !ok {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	chain, err := reference.ResolveChain(
		validator.ctx,
		resource,
		rawReference,
		validator.resolver,
		validator.limits,
	)
	if err != nil || chain.Circular() {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	targets := chain.Targets()
	if targets[len(targets)-1].Value.Kind() != jsonvalue.ObjectKind {
		return jsonvalue.Value{}, reference.Resource{}, false
	}
	target := targets[len(targets)-1]
	return target.Value, target.Resource, true
}

func (validator *linkValidator) visitLink(link jsonvalue.Value, pointer string) {
	validator.visitLinkWithParameters(link, pointer, nil)
}

func (validator *linkValidator) visitLinkWithParameters(
	link jsonvalue.Value,
	pointer string,
	parameters map[string]struct{},
) {
	if link.Kind() != jsonvalue.ObjectKind || isReference(link) {
		return
	}
	operationRef, hasOperationRef := stringMember(link, "operationRef")
	operationID, hasOperationID := stringMember(link, "operationId")
	switch {
	case hasOperationRef && hasOperationID:
		validator.add(
			"openapi.link.operation.conflict",
			pointer,
			"link must not define both operationRef and operationId",
			"link-object",
		)
	case !hasOperationRef && !hasOperationID:
		validator.add(
			"openapi.link.operation.missing",
			pointer,
			"link must define operationRef or operationId",
			"link-object",
		)
	}
	if hasOperationID {
		switch validator.operationIDs[operationID] {
		case 0:
			validator.add(
				"openapi.link.operation-id.unknown",
				pointer+"/operationId",
				"operationId does not identify an operation in this document",
				"link-object",
			)
		case 1:
		default:
			validator.add(
				"openapi.link.operation-id.ambiguous",
				pointer+"/operationId",
				"operationId identifies more than one operation in this document",
				"link-object",
			)
		}
	}
	if hasOperationRef && (operationRef == "" || !validURIReference(operationRef)) {
		validator.add(
			"openapi.link.operation-ref.invalid",
			pointer+"/operationRef",
			"operationRef is not a valid URI reference",
			"link-object",
		)
	} else if hasOperationRef && strings.HasPrefix(operationRef, "#") {
		exists := false
		if operationPointer, valid := internalOperationPointer(operationRef); valid {
			_, exists = validator.operations[operationPointer]
		}
		if !exists {
			validator.add(
				"openapi.link.operation-ref.invalid",
				pointer+"/operationRef",
				"operationRef does not identify an operation in this document",
				"link-object",
			)
		}
	}
	targetParameters, targetKnown := validator.linkTargetParameters(
		operationRef,
		hasOperationRef,
		operationID,
		hasOperationID,
	)
	validator.visitLinkExpressions(
		link,
		pointer,
		parameters,
		targetParameters,
		targetKnown,
	)
}

func (validator *linkValidator) linkTargetParameters(
	operationRef string,
	hasOperationRef bool,
	operationID string,
	hasOperationID bool,
) (map[string]struct{}, bool) {
	if hasOperationID && validator.operationIDs[operationID] == 1 {
		pointer := validator.operationPointers[operationID][0]
		return validator.parameters[pointer], true
	}
	if !hasOperationRef || !strings.HasPrefix(operationRef, "#") {
		return nil, false
	}
	pointer, valid := internalOperationPointer(operationRef)
	if !valid {
		return nil, false
	}
	if _, exists := validator.operations[pointer]; !exists {
		return nil, false
	}
	return validator.parameters[pointer], true
}

func internalOperationPointer(operationRef string) (string, bool) {
	fragment, err := reference.ParseFragment(strings.TrimPrefix(operationRef, "#"))
	if err != nil {
		return "", false
	}
	if fragment.Kind() != reference.FragmentPointer {
		return "", false
	}
	return fragment.Pointer().String(), true
}

func (validator *linkValidator) visitLinkExpressions(
	link jsonvalue.Value,
	pointer string,
	sourceParameters map[string]struct{},
	targetParameters map[string]struct{},
	targetKnown bool,
) {
	if linkParameters, ok := objectMember(link, "parameters"); ok {
		members, _ := linkParameters.Members()
		for _, member := range members {
			validator.validateLinkParameterName(
				member.Name,
				pointer+"/parameters/"+escapePointer(member.Name),
				targetParameters,
				targetKnown,
			)
			validator.validateLinkExpression(
				member.Value,
				pointer+"/parameters/"+escapePointer(member.Name),
				sourceParameters,
			)
		}
	}
	if requestBody, exists := link.Lookup("requestBody"); exists {
		validator.validateLinkExpression(
			requestBody,
			pointer+"/requestBody",
			sourceParameters,
		)
	}
}

func (validator *linkValidator) validateLinkParameterName(
	name string,
	pointer string,
	targetParameters map[string]struct{},
	targetKnown bool,
) {
	if !targetKnown {
		return
	}
	if qualified, ok := qualifiedLinkParameterKey(name); ok {
		if _, exists := targetParameters[qualified]; exists {
			return
		}
	}
	matched := false
	for parameter := range targetParameters {
		location, candidate, _ := strings.Cut(parameter, "\x00")
		if !linkParameterNameMatches(location, candidate, name) {
			continue
		}
		if matched {
			validator.add(
				"openapi.link.parameter.ambiguous",
				pointer,
				"link parameter name matches more than one target parameter",
				"link-object",
			)
			return
		}
		matched = true
	}
}

func linkParameterNameMatches(location, candidate, name string) bool {
	if candidate == name {
		return true
	}
	switch location {
	case "header":
		return strings.EqualFold(candidate, name)
	default:
		return false
	}
}

func qualifiedLinkParameterKey(name string) (string, bool) {
	for _, location := range []string{"path", "query", "header", "cookie", "querystring"} {
		if parameterName, found := strings.CutPrefix(name, location+"."); found {
			return requestParameterKey(location, parameterName), true
		}
	}
	return "", false
}

func (validator *linkValidator) validateLinkExpression(
	value jsonvalue.Value,
	pointer string,
	parameters map[string]struct{},
) {
	raw, ok := value.Text()
	if !ok || !strings.HasPrefix(raw, "$") {
		return
	}
	if _, err := expression.Parse(raw); err != nil {
		validator.add(
			"openapi.link.expression.invalid",
			pointer,
			"link value is not a valid runtime expression",
			"link-object",
		)
		return
	}
	location, name, requestParameter := requestParameterExpression(raw)
	if !requestParameter || parameters == nil {
		return
	}
	if _, declared := parameters[requestParameterKey(location, name)]; !declared {
		validator.add(
			"openapi.link.expression.parameter-undeclared",
			pointer,
			"link expression reads an undeclared request parameter",
			"link-object",
		)
	}
}

func requestParameterExpression(raw string) (string, string, bool) {
	for _, location := range []string{"path", "query", "header"} {
		prefix := "$request." + location + "."
		if name, found := strings.CutPrefix(raw, prefix); found {
			return location, name, true
		}
	}
	return "", "", false
}

func requestParameterKey(location string, name string) string {
	if location == "header" {
		name = strings.ToLower(name)
	}
	return location + "\x00" + name
}

func (validator *linkValidator) visitComponentMap(
	components jsonvalue.Value,
	name string,
	visit func(jsonvalue.Value, string),
) {
	values, ok := objectMember(components, name)
	if !ok {
		return
	}
	members, _ := values.Members()
	for _, member := range members {
		visit(
			member.Value,
			"/components/"+name+"/"+escapePointer(member.Name),
		)
	}
}

func (validator *linkValidator) add(
	code string,
	pointer string,
	message string,
	section string,
) {
	validator.diagnostics = append(validator.diagnostics, Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             SeverityError,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: validator.version,
		SpecificationSection: section,
	})
}

func objectMember(value jsonvalue.Value, name string) (jsonvalue.Value, bool) {
	member, exists := value.Lookup(name)
	return member, exists && member.Kind() == jsonvalue.ObjectKind
}

func isReference(value jsonvalue.Value) bool {
	_, exists := value.Lookup("$ref")
	return exists
}

func singleExpression(template expression.Template) bool {
	parts := template.Parts()
	return len(parts) == 1 && parts[0].Dynamic()
}
