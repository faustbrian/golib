package validate

import (
	"context"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type operationInventory struct {
	dialect    specversion.Dialect
	operations []operationLocation
	pathItems  []pathItemLocation
}

type pathItemLocation struct {
	value   jsonvalue.Value
	pointer string
}

func documentOperations(document openapi.Document) []operationLocation {
	return documentOperationInventory(document).operations
}

func documentPathItems(document openapi.Document) []pathItemLocation {
	return documentOperationInventory(document).pathItems
}

func externalDocumentOperations(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []operationLocation {
	collector := externalOperationCollector{
		ctx:      ctx,
		dialect:  document.SpecificationVersion().Dialect(),
		resource: validationResource(document, options.ReferenceResourceURI),
		resolver: options.ReferenceResolver,
		limits:   options.ReferenceLimits,
		seen:     make(map[string]struct{}),
	}
	for _, pathItem := range documentPathItems(document) {
		collector.pathItemReference(
			collector.resource,
			pathItem.value,
			pathItem.pointer,
		)
	}
	for _, operation := range documentOperations(document) {
		collector.operationCallbacks(
			collector.resource,
			operation.value,
			operation.pointer,
		)
	}
	return collector.operations
}

type externalOperationCollector struct {
	ctx        context.Context
	dialect    specversion.Dialect
	resource   reference.Resource
	resolver   reference.Resolver
	limits     reference.Limits
	seen       map[string]struct{}
	operations []operationLocation
}

func (collector *externalOperationCollector) pathItemReference(
	resource reference.Resource,
	value jsonvalue.Value,
	pointer string,
) {
	rawReference, referenced := stringMember(value, "$ref")
	if !referenced || strings.HasPrefix(rawReference, "#") {
		return
	}
	target, ok := collector.resolve(resource, rawReference)
	if !ok || collector.visited(target) {
		return
	}
	for _, operation := range operationsAt(
		target.Value,
		pointer,
		collector.dialect,
	) {
		operation.pathItem = target.Value
		operation.resource = target.Resource
		collector.operations = append(collector.operations, operation)
		collector.operationCallbacks(
			target.Resource,
			operation.value,
			operation.pointer,
		)
	}
}

func (collector *externalOperationCollector) operationCallbacks(
	resource reference.Resource,
	operation jsonvalue.Value,
	pointer string,
) {
	callbacks, exists := objectMember(operation, "callbacks")
	if !exists {
		return
	}
	members, _ := callbacks.Members()
	for _, member := range members {
		collector.callback(
			resource,
			member.Value,
			pointer+"/callbacks/"+escapePointer(member.Name),
		)
	}
}

func (collector *externalOperationCollector) callback(
	resource reference.Resource,
	value jsonvalue.Value,
	pointer string,
) {
	callback := value
	callbackResource := resource
	if rawReference, referenced := stringMember(value, "$ref"); referenced {
		if strings.HasPrefix(rawReference, "#") {
			return
		}
		target, ok := collector.resolve(resource, rawReference)
		if !ok || collector.visited(target) {
			return
		}
		callback = target.Value
		callbackResource = target.Resource
	}
	if callback.Kind() != jsonvalue.ObjectKind {
		return
	}
	members, _ := callback.Members()
	for _, member := range members {
		memberPointer := pointer + "/" + escapePointer(member.Name)
		if isReference(member.Value) {
			collector.pathItemReference(
				callbackResource,
				member.Value,
				memberPointer,
			)
			continue
		}
		for _, operation := range operationsAt(
			member.Value,
			memberPointer,
			collector.dialect,
		) {
			operation.pathItem = member.Value
			operation.resource = callbackResource
			collector.operations = append(collector.operations, operation)
			collector.operationCallbacks(
				callbackResource,
				operation.value,
				operation.pointer,
			)
		}
	}
}

func (collector *externalOperationCollector) resolve(
	resource reference.Resource,
	rawReference string,
) (reference.Target, bool) {
	chain, err := reference.ResolveChain(
		collector.ctx,
		resource,
		rawReference,
		collector.resolver,
		collector.limits,
	)
	if err != nil || chain.Circular() {
		return reference.Target{}, false
	}
	targets := chain.Targets()
	target := targets[len(targets)-1]
	if target.Value.Kind() != jsonvalue.ObjectKind {
		return reference.Target{}, false
	}
	return target, true
}

func (collector *externalOperationCollector) visited(
	target reference.Target,
) bool {
	resource := target.Resource.CanonicalURI
	if resource == "" {
		resource = target.Resource.RetrievalURI
	}
	if resource == "" {
		resource = target.RequestedURI
	}
	fragment := target.Fragment.Pointer().String()
	if target.Fragment.Kind() == reference.FragmentAnchor {
		fragment = target.Fragment.Anchor()
	}
	identity := resource + "\x00" +
		strconv.Itoa(int(target.Fragment.Kind())) + "\x00" + fragment
	if _, exists := collector.seen[identity]; exists {
		return true
	}
	collector.seen[identity] = struct{}{}
	return false
}

func documentOperationInventory(document openapi.Document) operationInventory {
	inventory := operationInventory{
		dialect: document.SpecificationVersion().Dialect(),
	}
	inventory.collection(document.Raw(), "paths", "/paths")
	inventory.collection(document.Raw(), "webhooks", "/webhooks")
	components, ok := objectMember(document.Raw(), "components")
	if !ok {
		return inventory
	}
	inventory.collection(components, "pathItems", "/components/pathItems")
	callbacks, ok := objectMember(components, "callbacks")
	if !ok {
		return inventory
	}
	members, _ := callbacks.Members()
	for _, member := range members {
		inventory.callback(
			member.Value,
			"/components/callbacks/"+escapePointer(member.Name),
		)
	}
	return inventory
}

func (inventory *operationInventory) collection(
	container jsonvalue.Value,
	name string,
	pointer string,
) {
	items, ok := objectMember(container, name)
	if !ok {
		return
	}
	members, _ := items.Members()
	for _, member := range members {
		inventory.pathItem(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}

func (inventory *operationInventory) pathItem(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind {
		return
	}
	inventory.pathItems = append(inventory.pathItems, pathItemLocation{
		value: value, pointer: pointer,
	})
	if isReference(value) {
		return
	}
	for _, operation := range operationsAt(value, pointer, inventory.dialect) {
		inventory.operations = append(inventory.operations, operation)
		callbacks, ok := objectMember(operation.value, "callbacks")
		if !ok {
			continue
		}
		members, _ := callbacks.Members()
		for _, member := range members {
			inventory.callback(
				member.Value,
				operation.pointer+"/callbacks/"+escapePointer(member.Name),
			)
		}
	}
}

func (inventory *operationInventory) callback(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	members, _ := value.Members()
	for _, member := range members {
		inventory.pathItem(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}
