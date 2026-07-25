package validate

import (
	"context"
	"mime"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type mediaTypeLocation struct {
	value       jsonvalue.Value
	pointer     string
	name        string
	requestBody bool
	reusable    bool
	resource    reference.Resource
}

type mediaTypeCollector struct {
	ctx               context.Context
	dialect           specversion.Dialect
	resource          reference.Resource
	resolver          reference.Resolver
	limits            reference.Limits
	resolveReferences bool
	locations         []mediaTypeLocation
}

func validateMediaTypes(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	if dialect == specversion.DialectSwagger20 {
		return nil
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, mediaType := range mediaTypeObjectsWithOptions(ctx, document, options) {
		mediaResource := mediaType.resource
		if dialect == specversion.DialectOAS32 &&
			(baseMediaType(mediaType.name) == "application/linkset" ||
				baseMediaType(mediaType.name) == "application/linkset+json") &&
			!validLinksetMediaTypeSchema(
				ctx,
				mediaResource,
				mediaType.value,
				options,
			) {
			diagnostics = append(diagnostics, mediaTypeDiagnostic(
				version,
				"openapi.media-type.linkset.schema",
				mediaType.pointer+"/schema",
				"linkset media types require a schema describing the RFC 9264 JSON structure",
			))
		}
		if !mediaType.reusable && baseMediaType(mediaType.name) == "" {
			diagnostics = append(diagnostics, mediaTypeDiagnostic(
				version,
				"openapi.media-type.name.invalid",
				mediaType.pointer,
				"content name must be an RFC 6838 media type or media range",
			))
		}
		_, hasSchema := mediaType.value.Lookup("schema")
		_, hasItemSchema := mediaType.value.Lookup("itemSchema")
		if mediaType.requestBody && hasSchema &&
			!isMultipartMediaType(mediaType.name) &&
			describesMultipleBinaryFiles(
				ctx,
				mediaResource,
				mediaType.value,
				options,
			) {
			diagnostics = append(diagnostics, mediaTypeDiagnostic(
				version,
				"openapi.media-type.multiple-files.non-multipart",
				mediaType.pointer,
				"multiple binary files require a multipart request media type",
			))
		}
		switch dialect {
		case specversion.DialectSwagger20, specversion.DialectOAS30, specversion.DialectOAS31:
			if mediaType.requestBody && isMultipartMediaType(mediaType.name) && !hasSchema {
				diagnostics = append(diagnostics, mediaTypeDiagnostic(
					version,
					"openapi.media-type.multipart.schema-missing",
					mediaType.pointer,
					"multipart request media types require a schema",
				))
			}
		}
		_, hasExample := mediaType.value.Lookup("example")
		_, hasExamples := mediaType.value.Lookup("examples")
		if hasExample && hasExamples {
			diagnostics = append(diagnostics, mediaTypeDiagnostic(
				version,
				"openapi.media-type.examples.conflict",
				mediaType.pointer,
				"media type must not define both example and examples",
			))
		}
		encoding, hasEncoding := objectMember(mediaType.value, "encoding")
		_, hasPrefix := mediaType.value.Lookup("prefixEncoding")
		_, hasItem := mediaType.value.Lookup("itemEncoding")
		switch dialect {
		case specversion.DialectOAS32:
			if baseMediaType(mediaType.name) == "multipart/form-data" &&
				(hasPrefix || hasItem) {
				diagnostics = append(
					diagnostics,
					validateFormDataPositionalContentDisposition(
						mediaType.value, mediaType.pointer, version,
					)...,
				)
			}
		}
		if dialect == specversion.DialectOAS32 && (hasPrefix || hasItem) &&
			!hasItemSchema {
			arraySchema, known := mediaTypeHasArraySchema(
				ctx, mediaResource, mediaType.value, options,
			)
			if known && !arraySchema {
				field := "itemEncoding"
				if hasPrefix {
					field = "prefixEncoding"
				}
				diagnostics = append(diagnostics, mediaTypeDiagnostic(
					version,
					"openapi.media-type.positional-encoding.schema-missing",
					mediaType.pointer+"/"+field,
					"positional encoding requires itemSchema or an array schema",
				))
			}
		}
		if dialect == specversion.DialectOAS32 && hasEncoding &&
			(hasPrefix || hasItem) {
			diagnostics = append(diagnostics, mediaTypeDiagnostic(
				version,
				"openapi.media-type.encoding.conflict",
				mediaType.pointer,
				"encoding must not be combined with prefixEncoding or itemEncoding",
			))
		}
		if hasEncoding && dialect != specversion.DialectOAS30 {
			members, _ := encoding.Members()
			for _, member := range members {
				_, hasContentType := member.Value.Lookup("contentType")
				if !hasContentType || !hasEncodingSerializationOverride(member.Value) {
					continue
				}
				diagnostic := mediaTypeDiagnostic(
					version,
					"openapi.encoding.content-type.ignored",
					mediaType.pointer+"/encoding/"+
						escapePointer(member.Name)+"/contentType",
					"contentType is ignored when encoding serialization fields are explicit",
				)
				diagnostic.Severity = SeverityWarning
				diagnostics = append(diagnostics, diagnostic)
			}
		}
		if hasEncoding && dialect == specversion.DialectOAS30 &&
			baseMediaType(mediaType.name) != "application/x-www-form-urlencoded" {
			members, _ := encoding.Members()
			for _, member := range members {
				for _, field := range []string{"style", "explode", "allowReserved"} {
					if _, exists := member.Value.Lookup(field); !exists {
						continue
					}
					diagnostic := mediaTypeDiagnostic(
						version,
						"openapi.encoding.serialization.ignored",
						mediaType.pointer+"/encoding/"+
							escapePointer(member.Name)+"/"+field,
						field+" is ignored outside form-urlencoded media types",
					)
					diagnostic.Severity = SeverityWarning
					diagnostics = append(diagnostics, diagnostic)
				}
			}
		}
		if !mediaType.reusable {
			invalidNamedContext := !supportsNamedEncoding(mediaType.name)
			switch dialect {
			case specversion.DialectSwagger20, specversion.DialectOAS30, specversion.DialectOAS31:
				invalidNamedContext = invalidNamedContext || !mediaType.requestBody
			}
			if hasEncoding && invalidNamedContext {
				message := "named encoding is ignored outside form or multipart media types"
				switch dialect {
				case specversion.DialectSwagger20, specversion.DialectOAS30, specversion.DialectOAS31:
					message = "named encoding is ignored outside form or multipart request content"
				}
				diagnostic := mediaTypeDiagnostic(
					version,
					"openapi.media-type.encoding.invalid-context",
					mediaType.pointer+"/encoding",
					message,
				)
				diagnostic.Severity = SeverityWarning
				diagnostics = append(diagnostics, diagnostic)
			}
			if dialect == specversion.DialectOAS32 && (hasPrefix || hasItem) &&
				!isMultipartMediaType(mediaType.name) {
				field := "itemEncoding"
				if hasPrefix {
					field = "prefixEncoding"
				}
				diagnostic := mediaTypeDiagnostic(
					version,
					"openapi.media-type.encoding.invalid-context",
					mediaType.pointer+"/"+field,
					"positional encoding is ignored outside multipart content",
				)
				diagnostic.Severity = SeverityWarning
				diagnostics = append(diagnostics, diagnostic)
			}
			if hasEncoding && !isMultipartMediaType(mediaType.name) {
				members, _ := encoding.Members()
				for _, member := range members {
					if _, hasHeaders := member.Value.Lookup("headers"); !hasHeaders {
						continue
					}
					diagnostic := mediaTypeDiagnostic(
						version,
						"openapi.encoding.headers.ignored",
						mediaType.pointer+"/encoding/"+
							escapePointer(member.Name)+"/headers",
						"encoding headers are ignored outside multipart media types",
					)
					diagnostic.Severity = SeverityWarning
					diagnostics = append(diagnostics, diagnostic)
				}
			}
		}
		if hasEncoding {
			diagnostics = append(
				diagnostics,
				validateEncodingProperties(
					ctx,
					mediaResource,
					mediaType,
					encoding,
					version,
					options,
				)...,
			)
		}
	}
	return diagnostics
}

func validateFormDataPositionalContentDisposition(
	mediaType jsonvalue.Value,
	pointer string,
	version string,
) []Diagnostic {
	var diagnostics []Diagnostic
	if prefix, exists := mediaType.Lookup("prefixEncoding"); exists {
		encodings, _ := prefix.Elements()
		for index, encoding := range encodings {
			if encoding.Kind() == jsonvalue.ObjectKind &&
				!encodingHasHeader(encoding, "Content-Disposition") {
				diagnostics = append(diagnostics, mediaTypeDiagnostic(
					version,
					"openapi.encoding.content-disposition.missing",
					pointer+"/prefixEncoding/"+strconv.Itoa(index)+"/headers",
					"positional form-data encoding must provide a Content-Disposition header",
				))
			}
		}
	}
	if item, exists := mediaType.Lookup("itemEncoding"); exists &&
		item.Kind() == jsonvalue.ObjectKind &&
		!encodingHasHeader(item, "Content-Disposition") {
		diagnostics = append(diagnostics, mediaTypeDiagnostic(
			version,
			"openapi.encoding.content-disposition.missing",
			pointer+"/itemEncoding/headers",
			"positional form-data encoding must provide a Content-Disposition header",
		))
	}
	return diagnostics
}

func encodingHasHeader(encoding jsonvalue.Value, wanted string) bool {
	headers, exists := objectMember(encoding, "headers")
	if !exists {
		return false
	}
	members, _ := headers.Members()
	for _, member := range members {
		if strings.EqualFold(member.Name, wanted) {
			return true
		}
	}
	return false
}

func mediaTypeHasArraySchema(
	ctx context.Context,
	resource reference.Resource,
	mediaType jsonvalue.Value,
	options Options,
) (bool, bool) {
	schema, exists := mediaType.Lookup("schema")
	if !exists {
		return false, true
	}
	resolved, _, ok := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		resource,
		schema,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	if !ok {
		return false, false
	}
	if schemaHasType(resolved, "array") {
		return true, true
	}
	for _, keyword := range []string{
		"prefixItems", "items", "contains", "minItems", "maxItems", "uniqueItems",
	} {
		if _, exists := resolved.Lookup(keyword); exists {
			return true, true
		}
	}
	return false, true
}

func describesMultipleBinaryFiles(
	ctx context.Context,
	resource reference.Resource,
	mediaType jsonvalue.Value,
	options Options,
) bool {
	schema, exists := mediaType.Lookup("schema")
	if !exists || schema.Kind() != jsonvalue.ObjectKind {
		return false
	}
	visitedReferences := make(map[string]struct{})
	remaining := options.ReferenceLimits.MaxTraversalNodes
	return schemaContainsBinaryFileArray(
		ctx,
		resource,
		schema,
		options.ReferenceResolver,
		options.ReferenceLimits,
		visitedReferences,
		&remaining,
		0,
	)
}

func schemaContainsBinaryFileArray(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	resolver reference.Resolver,
	limits reference.Limits,
	visitedReferences map[string]struct{},
	remaining *int,
	depth int,
) bool {
	if *remaining < 1 {
		return false
	}
	if depth >= limits.MaxTraversalDepth {
		return false
	}
	if schema.Kind() != jsonvalue.ObjectKind {
		return false
	}
	(*remaining)--
	if rawReference, hasReference := stringMember(schema, "$ref"); hasReference {
		identity := resource.CanonicalURI
		switch identity {
		case "":
			identity = resource.RetrievalURI
		}
		identity += "\x00" + rawReference
		if _, visited := visitedReferences[identity]; visited {
			return false
		}
		visitedReferences[identity] = struct{}{}
	}
	resolved, resolvedResource, ok := resolveReferencedObjectResourceWithPolicy(
		ctx, resource, schema, resolver, limits,
	)
	if !ok {
		return false
	}
	if schemaHasType(resolved, "array") {
		items, exists := resolved.Lookup("items")
		if exists {
			item, itemResource, itemOK := resolveReferencedObjectResourceWithPolicy(
				ctx, resolvedResource, items, resolver, limits,
			)
			if itemOK && schemaHasType(item, "string") {
				format, hasFormat := stringMember(item, "format")
				if hasFormat && format == "binary" {
					return true
				}
			}
			if schemaContainsBinaryFileArray(
				ctx, itemResource, item, resolver, limits,
				visitedReferences, remaining, nextSchemaTraversalDepth(depth),
			) {
				return true
			}
		}
	}
	for _, name := range []string{"properties", "$defs", "definitions"} {
		members, exists := objectMember(resolved, name)
		if !exists {
			continue
		}
		entries, _ := members.Members()
		for _, member := range entries {
			if schemaContainsBinaryFileArray(
				ctx, resolvedResource, member.Value, resolver, limits,
				visitedReferences, remaining, nextSchemaTraversalDepth(depth),
			) {
				return true
			}
		}
	}
	for _, name := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
		values, exists := resolved.Lookup(name)
		if !exists || values.Kind() != jsonvalue.ArrayKind {
			continue
		}
		elements, _ := values.Elements()
		for _, element := range elements {
			if schemaContainsBinaryFileArray(
				ctx, resolvedResource, element, resolver, limits,
				visitedReferences, remaining, nextSchemaTraversalDepth(depth),
			) {
				return true
			}
		}
	}
	return false
}

func nextSchemaTraversalDepth(depth int) int {
	return depth + 1
}

func hasEncodingSerializationOverride(encoding jsonvalue.Value) bool {
	for _, field := range []string{"style", "explode", "allowReserved"} {
		if _, exists := encoding.Lookup(field); exists {
			return true
		}
	}
	return false
}

func mediaTypeObjects(document openapi.Document) []mediaTypeLocation {
	collector := mediaTypeCollector{
		dialect: document.SpecificationVersion().Dialect(),
	}
	collector.document(document.Raw())
	return collector.locations
}

func mediaTypeObjectsWithOptions(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []mediaTypeLocation {
	collector := mediaTypeCollector{
		ctx:               ctx,
		dialect:           document.SpecificationVersion().Dialect(),
		resource:          validationResource(document, options.ReferenceResourceURI),
		resolver:          options.ReferenceResolver,
		limits:            options.ReferenceLimits,
		resolveReferences: true,
	}
	collector.document(document.Raw())
	return collector.locations
}

func validateEncodingProperties(
	ctx context.Context,
	resource reference.Resource,
	mediaType mediaTypeLocation,
	encoding jsonvalue.Value,
	version string,
	options Options,
) []Diagnostic {
	schema, exists := mediaType.value.Lookup("schema")
	if !exists || schema.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	properties, complete := encodingSchemaProperties(
		ctx, resource, schema, options,
	)
	members, _ := encoding.Members()
	var diagnostics []Diagnostic
	for _, member := range members {
		property, exists := properties[member.Name]
		if exists {
			if version == "3.1.1" || version == "3.1.2" ||
				version == "3.2.0" {
				resolvedProperty, resolved := resolveReferencedObjectWithPolicy(
					ctx,
					property.resource,
					property.value,
					options.ReferenceResolver,
					options.ReferenceLimits,
				)
				if resolved {
					if _, hasContentMediaType := resolvedProperty.Lookup(
						"contentMediaType",
					); hasContentMediaType {
						diagnostic := mediaTypeDiagnostic(
							version,
							"openapi.encoding.content-media-type.nonportable",
							mediaType.pointer+"/schema/properties/"+
								escapePointer(member.Name)+"/contentMediaType",
							"contentMediaType is ignored when an Encoding Object applies",
						)
						diagnostic.Severity = SeverityWarning
						diagnostics = append(diagnostics, diagnostic)
					}
				}
			}
			continue
		}
		if !complete {
			continue
		}
		diagnostics = append(diagnostics, mediaTypeDiagnostic(
			version,
			"openapi.media-type.encoding.property-missing",
			mediaType.pointer+"/encoding/"+escapePointer(member.Name),
			"encoding name does not match a schema property",
		))
	}
	return diagnostics
}

type encodingSchemaProperty struct {
	value    jsonvalue.Value
	resource reference.Resource
}

func encodingSchemaProperties(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	options Options,
) (map[string]encodingSchemaProperty, bool) {
	properties := make(map[string]encodingSchemaProperty)
	remaining := options.ReferenceLimits.MaxTraversalNodes
	complete := true
	collectEncodingSchemaProperties(
		ctx,
		resource,
		schema,
		options.ReferenceResolver,
		options.ReferenceLimits,
		make(map[string]struct{}),
		&remaining,
		0,
		properties,
		&complete,
	)
	return properties, complete
}

func collectEncodingSchemaProperties(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	resolver reference.Resolver,
	limits reference.Limits,
	visitedReferences map[string]struct{},
	remaining *int,
	depth int,
	properties map[string]encodingSchemaProperty,
	complete *bool,
) {
	if *remaining < 1 || depth >= limits.MaxTraversalDepth {
		*complete = false
		return
	}
	if schema.Kind() == jsonvalue.BooleanKind {
		return
	}
	if schema.Kind() != jsonvalue.ObjectKind {
		*complete = false
		return
	}
	(*remaining)--
	if direct, exists := objectMember(schema, "properties"); exists {
		members, _ := direct.Members()
		for _, member := range members {
			if _, known := properties[member.Name]; known {
				continue
			}
			properties[member.Name] = encodingSchemaProperty{
				value: member.Value, resource: resource,
			}
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		composition, exists := schema.Lookup(keyword)
		if !exists || composition.Kind() != jsonvalue.ArrayKind {
			continue
		}
		elements, _ := composition.Elements()
		for _, element := range elements {
			collectEncodingSchemaProperties(
				ctx,
				resource,
				element,
				resolver,
				limits,
				visitedReferences,
				remaining,
				nextSchemaTraversalDepth(depth),
				properties,
				complete,
			)
		}
	}
	rawReference, referenced := stringMember(schema, "$ref")
	if !referenced {
		return
	}
	identity := resource.CanonicalURI
	if identity == "" {
		identity = resource.RetrievalURI
	}
	identity += "\x00" + rawReference
	if _, visited := visitedReferences[identity]; visited {
		return
	}
	visitedReferences[identity] = struct{}{}
	resolved, resolvedResource, ok := resolveReferencedObjectResourceWithPolicy(
		ctx,
		resource,
		schema,
		resolver,
		limits,
	)
	if !ok {
		*complete = false
		return
	}
	collectEncodingSchemaProperties(
		ctx,
		resolvedResource,
		resolved,
		resolver,
		limits,
		visitedReferences,
		remaining,
		nextSchemaTraversalDepth(depth),
		properties,
		complete,
	)
}

func supportsNamedEncoding(value string) bool {
	return isMultipartMediaType(value) || baseMediaType(value) == "application/x-www-form-urlencoded"
}

func isMultipartMediaType(value string) bool {
	return strings.HasPrefix(baseMediaType(value), "multipart/")
}

func validLinksetMediaTypeSchema(
	ctx context.Context,
	resource reference.Resource,
	mediaType jsonvalue.Value,
	options Options,
) bool {
	schema, exists := mediaType.Lookup("schema")
	if !exists {
		return false
	}
	root, rootResource, ok := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		resource,
		schema,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	if !ok || !schemaHasType(root, "object") ||
		!resolvedSchemaRequiresProperty(ctx, rootResource, root, "linkset", options) {
		return false
	}
	rootProperties, complete := encodingSchemaProperties(
		ctx,
		rootResource,
		root,
		options,
	)
	linkset, exists := rootProperties["linkset"]
	if !complete || !exists {
		return false
	}
	linksetSchema, linksetResource, ok :=
		resolveReferencedSchemaResourceWithPolicy(
			ctx,
			linkset.resource,
			linkset.value,
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
	if !ok || !schemaHasType(linksetSchema, "array") {
		return false
	}
	contexts, exists := linksetSchema.Lookup("items")
	if !exists {
		return false
	}
	contextSchema, contextResource, ok :=
		resolveReferencedSchemaResourceWithPolicy(
			ctx,
			linksetResource,
			contexts,
			options.ReferenceResolver,
			options.ReferenceLimits,
		)
	if !ok || !schemaHasType(contextSchema, "object") {
		return false
	}
	properties, complete := encodingSchemaProperties(
		ctx,
		contextResource,
		contextSchema,
		options,
	)
	if !complete {
		return false
	}
	hasRelation := false
	for name, property := range properties {
		if name == "anchor" {
			resolved, _, resolvedOK := resolveReferencedSchemaResourceWithPolicy(
				ctx,
				property.resource,
				property.value,
				options.ReferenceResolver,
				options.ReferenceLimits,
			)
			if !resolvedOK || !schemaHasType(resolved, "string") {
				return false
			}
			continue
		}
		hasRelation = true
		if !validLinksetRelationSchema(
			ctx,
			property.resource,
			property.value,
			options,
		) {
			return false
		}
	}
	if additional, exists := contextSchema.Lookup("additionalProperties"); exists &&
		additional.Kind() != jsonvalue.BooleanKind {
		hasRelation = true
		if !validLinksetRelationSchema(
			ctx,
			contextResource,
			additional,
			options,
		) {
			return false
		}
	}
	return hasRelation
}

func validLinksetRelationSchema(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	options Options,
) bool {
	relation, relationResource, ok := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		resource,
		schema,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	if !ok || !schemaHasType(relation, "array") {
		return false
	}
	items, exists := relation.Lookup("items")
	if !exists {
		return false
	}
	target, targetResource, ok := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		relationResource,
		items,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	if !ok || !schemaHasType(target, "object") ||
		!resolvedSchemaRequiresProperty(ctx, targetResource, target, "href", options) {
		return false
	}
	properties, complete := encodingSchemaProperties(
		ctx,
		targetResource,
		target,
		options,
	)
	href, exists := properties["href"]
	if !complete || !exists {
		return false
	}
	resolved, _, ok := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		href.resource,
		href.value,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	return ok && schemaHasType(resolved, "string")
}

func resolvedSchemaRequiresProperty(
	ctx context.Context,
	resource reference.Resource,
	schema jsonvalue.Value,
	name string,
	options Options,
) bool {
	resolved, _, ok := resolveReferencedSchemaResourceWithPolicy(
		ctx,
		resource,
		schema,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	if !ok {
		return false
	}
	required, exists := resolved.Lookup("required")
	if !exists || required.Kind() != jsonvalue.ArrayKind {
		return false
	}
	members, _ := required.Elements()
	for _, member := range members {
		value, valid := member.Text()
		if valid && value == name {
			return true
		}
	}
	return false
}

func baseMediaType(value string) string {
	parsed, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed)
}

func mediaTypeDiagnostic(
	version string,
	code string,
	pointer string,
	message string,
) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             SeverityError,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "media-type-object",
	}
}

func (collector *mediaTypeCollector) document(root jsonvalue.Value) {
	for _, collection := range []string{"paths", "webhooks"} {
		collector.mapObjects(root, collection, "/"+collection, collector.pathItem)
	}
	components, exists := objectMember(root, "components")
	if !exists {
		return
	}
	collector.mapObjects(components, "parameters", "/components/parameters", collector.parameter)
	collector.mapObjects(components, "headers", "/components/headers", collector.parameter)
	collector.mapObjects(components, "requestBodies", "/components/requestBodies", collector.requestBody)
	collector.mapObjects(components, "responses", "/components/responses", collector.response)
	collector.mapObjects(components, "callbacks", "/components/callbacks", collector.callback)
	collector.mapObjects(components, "pathItems", "/components/pathItems", collector.pathItem)
	if collector.dialect == specversion.DialectOAS32 {
		mediaTypes, ok := objectMember(components, "mediaTypes")
		if ok {
			members, _ := mediaTypes.Members()
			for _, member := range members {
				collector.mediaType(
					member.Value,
					"/components/mediaTypes/"+escapePointer(member.Name),
					"",
					false,
					true,
				)
			}
		}
	}
}

func (collector *mediaTypeCollector) pathItem(value jsonvalue.Value, pointer string) {
	if isReference(value) {
		return
	}
	collector.parameters(value, pointer+"/parameters")
	for _, operation := range operationsAt(value, pointer, collector.dialect) {
		collector.operation(
			operation.value,
			operation.pointer,
			operation.method,
		)
	}
}

func (collector *mediaTypeCollector) operation(
	value jsonvalue.Value,
	pointer string,
	method string,
) {
	collector.parameters(value, pointer+"/parameters")
	if requestBody, exists := objectMember(value, "requestBody"); exists &&
		!consumerIgnoresRequestBody(collector.dialect, method) {
		collector.requestBody(requestBody, pointer+"/requestBody")
	}
	if responses, exists := objectMember(value, "responses"); exists {
		members, _ := responses.Members()
		for _, member := range members {
			collector.response(
				member.Value,
				pointer+"/responses/"+escapePointer(member.Name),
			)
		}
	}
	if callbacks, exists := objectMember(value, "callbacks"); exists {
		members, _ := callbacks.Members()
		for _, member := range members {
			collector.callback(
				member.Value,
				pointer+"/callbacks/"+escapePointer(member.Name),
			)
		}
	}
}

func (collector *mediaTypeCollector) parameters(value jsonvalue.Value, pointer string) {
	parameters, exists := value.Lookup("parameters")
	if !exists {
		return
	}
	if parameters.Kind() != jsonvalue.ArrayKind {
		return
	}
	elements, _ := parameters.Elements()
	for index, parameter := range elements {
		collector.parameter(
			parameter,
			pointer+"/"+strconv.Itoa(index),
		)
	}
}

func (collector *mediaTypeCollector) parameter(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind {
		return
	}
	if isReference(value) {
		return
	}
	if ignoredHeaderParameterObject(value) {
		return
	}
	collector.content(value, pointer+"/content", false)
}

func (collector *mediaTypeCollector) requestBody(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	collector.content(value, pointer+"/content", true)
}

func (collector *mediaTypeCollector) response(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind || isReference(value) {
		return
	}
	collector.content(value, pointer+"/content", false)
	collector.mapObjects(value, "headers", pointer+"/headers", collector.parameter)
}

func (collector *mediaTypeCollector) content(
	value jsonvalue.Value,
	pointer string,
	requestBody bool,
) {
	content, exists := objectMember(value, "content")
	if !exists {
		return
	}
	members, _ := content.Members()
	for _, member := range members {
		collector.mediaType(
			member.Value,
			pointer+"/"+escapePointer(member.Name),
			member.Name,
			requestBody,
			false,
		)
	}
}

func (collector *mediaTypeCollector) mediaType(
	value jsonvalue.Value,
	pointer string,
	name string,
	requestBody bool,
	reusable bool,
) {
	if value.Kind() != jsonvalue.ObjectKind {
		return
	}
	resource := collector.resource
	if isReference(value) {
		if !collector.resolveReferences {
			return
		}
		resolved, resolvedResource, ok := resolveReferencedObjectResourceWithPolicy(
			collector.ctx,
			resource,
			value,
			collector.resolver,
			collector.limits,
		)
		if !ok {
			return
		}
		value = resolved
		resource = resolvedResource
	}
	collector.locations = append(collector.locations, mediaTypeLocation{
		value: value, pointer: pointer, name: name,
		requestBody: requestBody, reusable: reusable, resource: resource,
	})
	encodings, exists := objectMember(value, "encoding")
	if !exists {
		return
	}
	members, _ := encodings.Members()
	for _, member := range members {
		collector.mapObjects(
			member.Value,
			"headers",
			pointer+"/encoding/"+escapePointer(member.Name)+"/headers",
			collector.parameter,
		)
	}
}

func (collector *mediaTypeCollector) callback(value jsonvalue.Value, pointer string) {
	if value.Kind() != jsonvalue.ObjectKind {
		return
	}
	if isReference(value) {
		return
	}
	members, _ := value.Members()
	for _, member := range members {
		collector.pathItem(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}

func (collector *mediaTypeCollector) mapObjects(
	container jsonvalue.Value,
	field string,
	pointer string,
	visit func(jsonvalue.Value, string),
) {
	objects, exists := objectMember(container, field)
	if !exists {
		return
	}
	members, _ := objects.Members()
	for _, member := range members {
		visit(member.Value, pointer+"/"+escapePointer(member.Name))
	}
}
