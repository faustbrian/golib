package jsonapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Members contains registered extension or profile members attached to a
// JSON:API-defined object.
type Members map[string]any

// MemberScope identifies the JSON:API object where a custom member is valid.
type MemberScope uint8

const (
	// TopLevelMemberScope registers a top-level document member.
	TopLevelMemberScope MemberScope = iota + 1
	// ResourceMemberScope registers a resource object member.
	ResourceMemberScope
	// RelationshipMemberScope registers a relationship object member.
	RelationshipMemberScope
	// IdentifierMemberScope registers a resource identifier member.
	IdentifierMemberScope
	// JSONAPIMemberScope registers a JSON:API object member.
	JSONAPIMemberScope
	// ErrorMemberScope registers an error object member.
	ErrorMemberScope
	// ErrorSourceMemberScope registers an error source object member.
	ErrorSourceMemberScope
	// LinksObjectMemberScope registers a member of a links object.
	LinksObjectMemberScope
	// LinkObjectMemberScope registers a link object member.
	LinkObjectMemberScope
)

// MemberDefinition declares one extension member and its optional value
// validator.
type MemberDefinition struct {
	Scope    MemberScope
	Name     string
	Validate func(any) error
}

// ExtensionDefinition declares an applied JSON:API extension.
type ExtensionDefinition struct {
	URI       string
	Namespace string
	Members   []MemberDefinition
}

// ProfileDefinition declares an applied JSON:API profile and its optional
// document-level implementation-semantics validator.
type ProfileDefinition struct {
	URI              string
	ValidateDocument func(Document) error
}

// CodecOptions configures applied extensions and core validation context.
type CodecOptions struct {
	Extensions []ExtensionDefinition
	Profiles   []ProfileDefinition
	Validation ValidationOptions
	Limits     DecodeLimits
}

// Codec is a strict document codec with explicitly registered extension
// members.
type Codec struct {
	validation ValidationOptions
	members    map[MemberScope]map[string]MemberDefinition
	extensions []string
	profiles   []ProfileDefinition
	limits     DecodeLimits
}

// NewCodec validates all extension definitions before constructing a codec.
func NewCodec(options CodecOptions) (*Codec, error) {
	limits, err := normalizeDecodeLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	codec := &Codec{
		validation: options.Validation,
		members:    make(map[MemberScope]map[string]MemberDefinition),
		limits:     limits,
	}
	seenURIs := make(map[string]struct{}, len(options.Extensions))
	seenNamespaces := make(map[string]struct{}, len(options.Extensions))
	for _, extension := range options.Extensions {
		absolute, valid := parseURIReference(extension.URI)
		if extension.URI == "" || !valid || !absolute {
			return nil, fmt.Errorf("extension URI must be absolute: %q", extension.URI)
		}
		if _, exists := seenURIs[extension.URI]; exists {
			return nil, fmt.Errorf("duplicate extension URI: %q", extension.URI)
		}
		seenURIs[extension.URI] = struct{}{}
		codec.extensions = append(codec.extensions, extension.URI)
		if !validExtensionNamespace(extension.Namespace) {
			return nil, fmt.Errorf("invalid extension namespace: %q", extension.Namespace)
		}
		if _, exists := seenNamespaces[extension.Namespace]; exists {
			return nil, fmt.Errorf("duplicate extension namespace: %q", extension.Namespace)
		}
		seenNamespaces[extension.Namespace] = struct{}{}
		for _, definition := range extension.Members {
			if definition.Scope != TopLevelMemberScope &&
				definition.Scope != ResourceMemberScope &&
				definition.Scope != RelationshipMemberScope &&
				definition.Scope != IdentifierMemberScope &&
				definition.Scope != JSONAPIMemberScope &&
				definition.Scope != ErrorMemberScope &&
				definition.Scope != ErrorSourceMemberScope &&
				definition.Scope != LinksObjectMemberScope &&
				definition.Scope != LinkObjectMemberScope {
				return nil, fmt.Errorf("unsupported member scope: %d", definition.Scope)
			}
			prefix := extension.Namespace + ":"
			if !strings.HasPrefix(definition.Name, prefix) ||
				!validImplementationMemberName(strings.TrimPrefix(definition.Name, prefix)) {
				return nil, fmt.Errorf(
					"extension member %q must use namespace %q",
					definition.Name,
					extension.Namespace,
				)
			}
			if codec.members[definition.Scope] == nil {
				codec.members[definition.Scope] = make(map[string]MemberDefinition)
			}
			if _, exists := codec.members[definition.Scope][definition.Name]; exists {
				return nil, fmt.Errorf("duplicate registered member: %q", definition.Name)
			}
			codec.members[definition.Scope][definition.Name] = definition
		}
	}
	seenProfiles := make(map[string]struct{}, len(options.Profiles))
	for _, profile := range options.Profiles {
		absolute, valid := parseURIReference(profile.URI)
		if profile.URI == "" || !valid || !absolute {
			return nil, fmt.Errorf("profile URI must be absolute: %q", profile.URI)
		}
		if _, exists := seenProfiles[profile.URI]; exists {
			return nil, fmt.Errorf("duplicate profile URI: %q", profile.URI)
		}
		seenProfiles[profile.URI] = struct{}{}
		codec.profiles = append(codec.profiles, profile)
	}

	return codec, nil
}

// Marshal validates and deterministically encodes a registered document.
func (codec *Codec) Marshal(document Document) ([]byte, error) {
	if err := validateDocumentMembers(document, codec.members); err != nil {
		return nil, err
	}
	if err := document.ValidateWith(codec.validation); err != nil {
		return nil, err
	}
	if err := codec.validateDeclarations(document); err != nil {
		return nil, err
	}
	if err := codec.validateProfiles(document); err != nil {
		return nil, err
	}

	return json.Marshal(document)
}

// Unmarshal strictly decodes registered extension members and the core
// document.
func (codec *Codec) Unmarshal(payload []byte) (Document, error) {
	if err := validateJSONPayload(payload, codec.limits); err != nil {
		return Document{}, err
	}
	if err := rejectDuplicateMembersWithLimits(payload, codec.limits); err != nil {
		return Document{}, err
	}
	root, err := decodeObject(payload, "")
	if err != nil {
		return Document{}, err
	}
	topMembers, err := codec.extractMembers(root, TopLevelMemberScope, "")
	if err != nil {
		return Document{}, err
	}
	var jsonapiMembers Members
	if raw, exists := root["jsonapi"]; exists {
		sanitized, extracted, sanitizeErr := codec.sanitizeObject(
			raw,
			JSONAPIMemberScope,
			"/jsonapi",
		)
		if sanitizeErr != nil {
			return Document{}, sanitizeErr
		}
		root["jsonapi"] = sanitized
		jsonapiMembers = extracted
	}
	var documentLinks linksMemberState
	if raw, exists := root["links"]; exists {
		sanitized, extracted, sanitizeErr := codec.sanitizeLinks(raw, "/links")
		if sanitizeErr != nil {
			return Document{}, sanitizeErr
		}
		root["links"] = sanitized
		documentLinks = extracted
	}

	var primaryMembers []resourceMemberState
	if raw, exists := root["data"]; exists {
		sanitized, extracted, sanitizeErr := codec.sanitizePrimaryData(raw, "/data")
		if sanitizeErr != nil {
			return Document{}, sanitizeErr
		}
		root["data"] = sanitized
		primaryMembers = extracted
	}
	var includedMembers []resourceMemberState
	if raw, exists := root["included"]; exists {
		sanitized, extracted, sanitizeErr := codec.sanitizeResourceArray(raw, "/included")
		if sanitizeErr != nil {
			return Document{}, sanitizeErr
		}
		root["included"] = sanitized
		includedMembers = extracted
	}
	var errorMembers []errorMemberState
	if raw, exists := root["errors"]; exists {
		sanitized, extracted, sanitizeErr := codec.sanitizeErrors(raw, "/errors")
		if sanitizeErr != nil {
			return Document{}, sanitizeErr
		}
		root["errors"] = sanitized
		errorMembers = extracted
	}
	// root contains only RawMessages from the already validated payload.
	sanitized, _ := json.Marshal(root)
	document, err := decodeDocument(sanitized)
	if err != nil {
		return Document{}, err
	}
	document.AdditionalMembers = topMembers
	if document.JSONAPI != nil {
		document.JSONAPI.AdditionalMembers = jsonapiMembers
	}
	attachLinkMembers(document.Links, documentLinks)
	attachPrimaryMembers(document.Data, primaryMembers)
	for index := range document.Included {
		if index < len(includedMembers) {
			attachResourceMembers(&document.Included[index], includedMembers[index])
		}
	}
	for index := range document.Errors {
		if index < len(errorMembers) {
			document.Errors[index].AdditionalMembers = errorMembers[index].members
			attachLinkMembers(document.Errors[index].Links, errorMembers[index].links)
			if document.Errors[index].Source != nil {
				document.Errors[index].Source.AdditionalMembers = errorMembers[index].source
			}
		}
	}
	if err := document.ValidateWith(codec.validation); err != nil {
		return Document{}, err
	}
	if err := codec.validateDeclarations(document); err != nil {
		return Document{}, err
	}
	if err := codec.validateProfiles(document); err != nil {
		return Document{}, err
	}

	return document, nil
}

func (codec *Codec) validateDeclarations(document Document) error {
	if document.JSONAPI == nil {
		return nil
	}
	profileURIs := make([]string, len(codec.profiles))
	for index, profile := range codec.profiles {
		profileURIs[index] = profile.URI
	}
	validator := documentValidator{}
	validator.validateAppliedURIs(
		document.JSONAPI.Ext,
		codec.extensions,
		"/jsonapi/ext",
		"extension",
		false,
	)
	validator.validateAppliedURIs(
		document.JSONAPI.Profile,
		profileURIs,
		"/jsonapi/profile",
		"profile",
		true,
	)
	if len(validator.violations) == 0 {
		return nil
	}
	return &ValidationError{Violations: validator.violations}
}

func (codec *Codec) validateProfiles(document Document) error {
	for _, profile := range codec.profiles {
		if profile.ValidateDocument == nil {
			continue
		}
		before, err := json.Marshal(document)
		if err != nil {
			return err
		}
		if err := callApplicationCallback("profile", func() error {
			return profile.ValidateDocument(document)
		}); err != nil {
			return err
		}
		after, err := json.Marshal(document)
		if err != nil || !bytes.Equal(before, after) {
			return &CallbackError{
				Phase: "profile",
				Cause: fmt.Errorf("profile validator mutated the document"),
			}
		}
	}
	return nil
}

func (codec *Codec) sanitizeErrors(
	raw json.RawMessage,
	path string,
) (json.RawMessage, []errorMemberState, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, nil, decodeFailure(path, "type", "errors must be an array", err)
	}
	members := make([]errorMemberState, len(items))
	for index, item := range items {
		itemPath := path + "/" + fmt.Sprintf("%d", index)
		object, err := decodeObject(item, itemPath)
		if err != nil {
			return nil, nil, err
		}
		extracted, err := codec.extractMembers(object, ErrorMemberScope, itemPath)
		if err != nil {
			return nil, nil, err
		}
		state := errorMemberState{members: extracted}
		if rawLinks, exists := object["links"]; exists {
			sanitizedLinks, links, sanitizeErr := codec.sanitizeLinks(
				rawLinks,
				itemPath+"/links",
			)
			if sanitizeErr != nil {
				return nil, nil, sanitizeErr
			}
			object["links"] = sanitizedLinks
			state.links = links
		}
		if rawSource, exists := object["source"]; exists {
			sanitizedSource, sourceMembers, sanitizeErr := codec.sanitizeObject(
				rawSource,
				ErrorSourceMemberScope,
				itemPath+"/source",
			)
			if sanitizeErr != nil {
				return nil, nil, sanitizeErr
			}
			object["source"] = sanitizedSource
			state.source = sourceMembers
		}
		sanitized, _ := json.Marshal(object)
		items[index] = sanitized
		members[index] = state
	}
	sanitized, err := json.Marshal(items)
	return sanitized, members, err
}

type errorMemberState struct {
	members Members
	source  Members
	links   linksMemberState
}

type linksMemberState struct {
	members Members
	links   map[string]linkMemberState
}

type linkMemberState struct {
	members     Members
	describedBy *linkMemberState
}

func (codec *Codec) sanitizeLinks(
	raw json.RawMessage,
	path string,
) (json.RawMessage, linksMemberState, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return nil, linksMemberState{}, err
	}
	members, err := codec.extractMembers(object, LinksObjectMemberScope, path)
	if err != nil {
		return nil, linksMemberState{}, err
	}
	states := make(map[string]linkMemberState)
	for name, rawLink := range object {
		trimmed := bytes.TrimSpace(rawLink)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			continue
		}
		linkPath := path + "/" + escapePointerToken(name)
		sanitized, state, sanitizeErr := codec.sanitizeLinkObject(rawLink, linkPath)
		if sanitizeErr != nil {
			return nil, linksMemberState{}, sanitizeErr
		}
		object[name] = sanitized
		if len(state.members) > 0 || state.describedBy != nil {
			states[name] = state
		}
	}
	sanitized, err := json.Marshal(object)
	return sanitized, linksMemberState{members: members, links: states}, err
}

func (codec *Codec) sanitizeLinkObject(
	raw json.RawMessage,
	path string,
) (json.RawMessage, linkMemberState, error) {
	// Callers only pass object-shaped RawMessages from a valid document.
	object, _ := decodeObject(raw, path)
	members, err := codec.extractMembers(object, LinkObjectMemberScope, path)
	if err != nil {
		return nil, linkMemberState{}, err
	}
	state := linkMemberState{members: members}
	if rawDescribedBy, exists := object["describedby"]; exists {
		trimmed := bytes.TrimSpace(rawDescribedBy)
		if len(trimmed) > 0 && trimmed[0] == '{' {
			sanitized, nested, sanitizeErr := codec.sanitizeLinkObject(
				rawDescribedBy,
				path+"/describedby",
			)
			if sanitizeErr != nil {
				return nil, linkMemberState{}, sanitizeErr
			}
			object["describedby"] = sanitized
			state.describedBy = &nested
		}
	}
	sanitized, err := json.Marshal(object)
	return sanitized, state, err
}

func (codec *Codec) sanitizeObject(
	raw json.RawMessage,
	scope MemberScope,
	path string,
) (json.RawMessage, Members, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return nil, nil, err
	}
	members, err := codec.extractMembers(object, scope, path)
	if err != nil {
		return nil, nil, err
	}
	sanitized, err := json.Marshal(object)
	return sanitized, members, err
}

func (codec *Codec) sanitizePrimaryData(
	raw json.RawMessage,
	path string,
) (json.RawMessage, []resourceMemberState, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return raw, nil, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		return codec.sanitizeResourceArray(raw, path)
	}
	sanitized, members, err := codec.sanitizeResource(raw, path)
	return sanitized, []resourceMemberState{members}, err
}

func (codec *Codec) sanitizeResourceArray(
	raw json.RawMessage,
	path string,
) (json.RawMessage, []resourceMemberState, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, nil, decodeFailure(path, "type", "value must be an array", err)
	}
	members := make([]resourceMemberState, len(items))
	for index, item := range items {
		sanitized, extracted, err := codec.sanitizeResource(
			item,
			path+"/"+fmt.Sprintf("%d", index),
		)
		if err != nil {
			return nil, nil, err
		}
		items[index] = sanitized
		members[index] = extracted
	}
	sanitized, err := json.Marshal(items)
	return sanitized, members, err
}

func (codec *Codec) sanitizeResource(
	raw json.RawMessage,
	path string,
) (json.RawMessage, resourceMemberState, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return nil, resourceMemberState{}, err
	}
	members, err := codec.extractMembers(object, ResourceMemberScope, path)
	if err != nil {
		return nil, resourceMemberState{}, err
	}
	state := resourceMemberState{members: members}
	if rawLinks, exists := object["links"]; exists {
		sanitized, links, sanitizeErr := codec.sanitizeLinks(rawLinks, path+"/links")
		if sanitizeErr != nil {
			return nil, resourceMemberState{}, sanitizeErr
		}
		object["links"] = sanitized
		state.links = links
	}
	if rawRelationships, exists := object["relationships"]; exists {
		sanitized, relationships, sanitizeErr := codec.sanitizeRelationships(
			rawRelationships,
			path+"/relationships",
		)
		if sanitizeErr != nil {
			return nil, resourceMemberState{}, sanitizeErr
		}
		object["relationships"] = sanitized
		state.relationships = relationships
	}
	sanitized, err := json.Marshal(object)
	return sanitized, state, err
}

type resourceMemberState struct {
	members       Members
	links         linksMemberState
	relationships map[string]relationshipMemberState
}

type relationshipMemberState struct {
	members     Members
	links       linksMemberState
	identifiers []Members
}

func (codec *Codec) sanitizeRelationships(
	raw json.RawMessage,
	path string,
) (json.RawMessage, map[string]relationshipMemberState, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return nil, nil, err
	}
	states := make(map[string]relationshipMemberState)
	for name, rawRelationship := range object {
		relationshipPath := path + "/" + escapePointerToken(name)
		relationship, decodeErr := decodeObject(rawRelationship, relationshipPath)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		members, extractErr := codec.extractMembers(
			relationship,
			RelationshipMemberScope,
			relationshipPath,
		)
		if extractErr != nil {
			return nil, nil, extractErr
		}
		state := relationshipMemberState{members: members}
		if rawLinks, exists := relationship["links"]; exists {
			sanitizedLinks, links, sanitizeErr := codec.sanitizeLinks(
				rawLinks,
				relationshipPath+"/links",
			)
			if sanitizeErr != nil {
				return nil, nil, sanitizeErr
			}
			relationship["links"] = sanitizedLinks
			state.links = links
		}
		if rawData, exists := relationship["data"]; exists {
			sanitizedData, identifiers, sanitizeErr := codec.sanitizeIdentifierData(
				rawData,
				relationshipPath+"/data",
			)
			if sanitizeErr != nil {
				return nil, nil, sanitizeErr
			}
			relationship["data"] = sanitizedData
			state.identifiers = identifiers
		}
		sanitized, _ := json.Marshal(relationship)
		object[name] = sanitized
		if len(state.members) > 0 || len(state.links.members) > 0 ||
			len(state.links.links) > 0 || len(state.identifiers) > 0 {
			states[name] = state
		}
	}
	sanitized, err := json.Marshal(object)
	return sanitized, states, err
}

func (codec *Codec) sanitizeIdentifierData(
	raw json.RawMessage,
	path string,
) (json.RawMessage, []Members, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return raw, nil, nil
	}
	if trimmed[0] == '{' {
		sanitized, members, err := codec.sanitizeIdentifier(raw, path)
		return sanitized, []Members{members}, err
	}
	if trimmed[0] != '[' {
		return raw, nil, nil
	}
	var items []json.RawMessage
	_ = json.Unmarshal(raw, &items)
	states := make([]Members, len(items))
	for index, item := range items {
		sanitized, members, err := codec.sanitizeIdentifier(
			item,
			path+"/"+fmt.Sprintf("%d", index),
		)
		if err != nil {
			return nil, nil, err
		}
		items[index] = sanitized
		states[index] = members
	}
	sanitized, err := json.Marshal(items)
	return sanitized, states, err
}

func (codec *Codec) sanitizeIdentifier(
	raw json.RawMessage,
	path string,
) (json.RawMessage, Members, error) {
	object, err := decodeObject(raw, path)
	if err != nil {
		return nil, nil, err
	}
	members, err := codec.extractMembers(object, IdentifierMemberScope, path)
	if err != nil {
		return nil, nil, err
	}
	sanitized, err := json.Marshal(object)
	return sanitized, members, err
}

func (codec *Codec) extractMembers(
	object map[string]json.RawMessage,
	scope MemberScope,
	path string,
) (Members, error) {
	rules := codec.members[scope]
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}
	sort.Strings(names)
	var members Members
	for _, name := range names {
		rawValue, exists := object[name]
		if !exists {
			continue
		}
		value := stripAtMembers(decodeValidValue(rawValue))
		if validate := rules[name].Validate; validate != nil {
			if validationErr := callApplicationCallback("extension-member", func() error {
				return validate(value)
			}); validationErr != nil {
				return nil, memberValueError(path, name, validationErr)
			}
		}
		if members == nil {
			members = make(Members)
		}
		members[name] = value
		delete(object, name)
	}
	return members, nil
}

func attachPrimaryMembers(data *PrimaryData, members []resourceMemberState) {
	if data == nil {
		return
	}
	if data.kind == primaryDataOne && data.one != nil && len(members) > 0 {
		attachResourceMembers(data.one, members[0])
	}
	if data.kind == primaryDataMany {
		for index := range data.many {
			if index < len(members) {
				attachResourceMembers(&data.many[index], members[index])
			}
		}
	}
}

func attachResourceMembers(resource *ResourceObject, state resourceMemberState) {
	resource.AdditionalMembers = state.members
	attachLinkMembers(resource.Links, state.links)
	for name, relationshipState := range state.relationships {
		relationship, exists := resource.Relationships[name]
		if !exists {
			continue
		}
		relationship.AdditionalMembers = relationshipState.members
		attachLinkMembers(relationship.Links, relationshipState.links)
		attachIdentifierMembers(relationship.Data, relationshipState.identifiers)
		resource.Relationships[name] = relationship
	}
}

func attachLinkMembers(links Links, state linksMemberState) {
	for name, value := range state.members {
		links[name] = ExtensionLinkValue(value)
	}
	for name, linkState := range state.links {
		link, exists := links[name]
		if !exists {
			continue
		}
		link.additionalMembers = linkState.members
		if link.describedBy != nil && linkState.describedBy != nil {
			attachLinkState(link.describedBy, *linkState.describedBy)
		}
		links[name] = link
	}
}

func attachLinkState(link *Link, state linkMemberState) {
	link.additionalMembers = state.members
	if link.describedBy != nil && state.describedBy != nil {
		attachLinkState(link.describedBy, *state.describedBy)
	}
}

func attachIdentifierMembers(data *RelationshipData, members []Members) {
	if data == nil {
		return
	}
	if data.kind == relationshipDataOne && data.one != nil && len(members) > 0 {
		data.one.AdditionalMembers = members[0]
	}
	if data.kind == relationshipDataMany {
		for index := range data.many {
			if index < len(members) {
				data.many[index].AdditionalMembers = members[index]
			}
		}
	}
}

func validateDocumentMembers(
	document Document,
	registry map[MemberScope]map[string]MemberDefinition,
) error {
	validator := documentValidator{}
	validateScopedMembers(
		&validator,
		document.AdditionalMembers,
		registry[TopLevelMemberScope],
		"",
	)
	if document.JSONAPI != nil {
		validateScopedMembers(
			&validator,
			document.JSONAPI.AdditionalMembers,
			registry[JSONAPIMemberScope],
			"/jsonapi",
		)
	}
	validateLinkDocumentMembers(
		&validator,
		document.Links,
		"/links",
		registry[LinksObjectMemberScope],
		registry[LinkObjectMemberScope],
	)
	for index, apiError := range document.Errors {
		path := "/errors/" + fmt.Sprintf("%d", index)
		validateScopedMembers(
			&validator,
			apiError.AdditionalMembers,
			registry[ErrorMemberScope],
			path,
		)
		validateLinkDocumentMembers(
			&validator,
			apiError.Links,
			path+"/links",
			registry[LinksObjectMemberScope],
			registry[LinkObjectMemberScope],
		)
		if apiError.Source != nil {
			validateScopedMembers(
				&validator,
				apiError.Source.AdditionalMembers,
				registry[ErrorSourceMemberScope],
				path+"/source",
			)
		}
	}
	for _, observation := range documentResources(document) {
		validateLinkDocumentMembers(
			&validator,
			observation.resource.Links,
			observation.path+"/links",
			registry[LinksObjectMemberScope],
			registry[LinkObjectMemberScope],
		)
		validateScopedMembers(
			&validator,
			observation.resource.AdditionalMembers,
			registry[ResourceMemberScope],
			observation.path,
		)
		for name, relationship := range observation.resource.Relationships {
			path := observation.path + "/relationships/" + escapePointerToken(name)
			validateLinkDocumentMembers(
				&validator,
				relationship.Links,
				path+"/links",
				registry[LinksObjectMemberScope],
				registry[LinkObjectMemberScope],
			)
			validateScopedMembers(
				&validator,
				relationship.AdditionalMembers,
				registry[RelationshipMemberScope],
				path,
			)
			validateIdentifierDocumentMembers(
				&validator,
				relationship.Data,
				path+"/data",
				registry[IdentifierMemberScope],
			)
		}
	}
	if len(validator.violations) == 0 {
		return nil
	}
	return &ValidationError{Violations: validator.violations, causes: validator.causes}
}

func validateLinkDocumentMembers(
	validator *documentValidator,
	links Links,
	path string,
	linksRegistry map[string]MemberDefinition,
	linkRegistry map[string]MemberDefinition,
) {
	for name, link := range links {
		linkPath := path + "/" + escapePointerToken(name)
		if link.extension {
			validateScopedMembers(
				validator,
				Members{name: link.extensionValue},
				linksRegistry,
				path,
			)
			continue
		}
		validateLinkStateMembers(
			validator,
			link,
			linkPath,
			linkRegistry,
		)
	}
}

func validateLinkStateMembers(
	validator *documentValidator,
	link Link,
	path string,
	registry map[string]MemberDefinition,
) {
	validateLinkStateMembersAt(
		validator,
		link,
		path,
		registry,
		0,
		make(map[*Link]struct{}),
	)
}

func validateLinkStateMembersAt(
	validator *documentValidator,
	link Link,
	path string,
	registry map[string]MemberDefinition,
	depth int,
	ancestors map[*Link]struct{},
) {
	validateScopedMembers(validator, link.additionalMembers, registry, path)
	if link.describedBy == nil {
		return
	}
	describedByPath := path + "/describedby"
	if depth >= DefaultMaxNestingDepth {
		validator.add(
			describedByPath,
			"limit",
			"constructed link exceeds the nesting depth limit",
		)
		return
	}
	if _, cyclic := ancestors[link.describedBy]; cyclic {
		validator.add(describedByPath, "cycle", "constructed link contains a cycle")
		return
	}
	ancestors[link.describedBy] = struct{}{}
	validateLinkStateMembersAt(
		validator,
		*link.describedBy,
		describedByPath,
		registry,
		depth+1,
		ancestors,
	)
	delete(ancestors, link.describedBy)
}

func validateIdentifierDocumentMembers(
	validator *documentValidator,
	data *RelationshipData,
	path string,
	registry map[string]MemberDefinition,
) {
	if data == nil {
		return
	}
	if data.kind == relationshipDataOne && data.one != nil {
		validateScopedMembers(validator, data.one.AdditionalMembers, registry, path)
	}
	if data.kind == relationshipDataMany {
		for index, identifier := range data.many {
			validateScopedMembers(
				validator,
				identifier.AdditionalMembers,
				registry,
				path+"/"+fmt.Sprintf("%d", index),
			)
		}
	}
}

func validateScopedMembers(
	validator *documentValidator,
	members Members,
	registry map[string]MemberDefinition,
	path string,
) {
	for name, value := range members {
		if strings.HasPrefix(name, "@") {
			if !validMemberName(name) {
				validator.add(
					path+"/"+escapePointerToken(name),
					"member-name",
					"@-Member name is invalid",
				)
			}
			continue
		}
		definition, exists := registry[name]
		if !exists {
			validator.add(
				path+"/"+escapePointerToken(name),
				"unregistered-member",
				"member is not registered for this object scope",
			)
			continue
		}
		if definition.Validate != nil {
			if err := callApplicationCallback("extension-member", func() error {
				return definition.Validate(value)
			}); err != nil {
				validator.violations = append(
					validator.violations,
					memberValueViolation(path, name, err),
				)
				validator.causes = append(validator.causes, err)
			}
		}
	}
}

func memberValueError(path, name string, err error) error {
	return &ValidationError{Violations: []Violation{
		memberValueViolation(path, name, err),
	}, causes: []error{err}}
}

func memberValueViolation(path, name string, _ error) Violation {
	return Violation{
		Path:    path + "/" + escapePointerToken(name),
		Code:    "member-value",
		Message: "registered extension member failed validation",
	}
}

func marshalObjectWithMembers(core any, members Members) ([]byte, error) {
	payload, err := json.Marshal(core)
	if err != nil || len(members) == 0 {
		return payload, err
	}
	var existing map[string]json.RawMessage
	if err := json.Unmarshal(payload, &existing); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(members))
	for name := range members {
		if _, collision := existing[name]; collision {
			return nil, fmt.Errorf("additional member %q conflicts with a core member", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	buffer := bytes.NewBuffer(make([]byte, 0, len(payload)+len(names)*16))
	buffer.Write(payload[:len(payload)-1])
	hasMembers := len(existing) > 0
	for _, name := range names {
		nameJSON, _ := json.Marshal(name)
		valueJSON, err := json.Marshal(members[name])
		if err != nil {
			return nil, err
		}
		if hasMembers {
			buffer.WriteByte(',')
		}
		buffer.Write(nameJSON)
		buffer.WriteByte(':')
		buffer.Write(valueJSON)
		hasMembers = true
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}
