// Package implicit resolves OpenAPI name-based connections across caller-owned
// parsed documents without performing reference I/O.
package implicit

import (
	"errors"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidInput reports malformed documents, names, or options.
	ErrInvalidInput = errors.New("invalid implicit connection input")
	// ErrLimitExceeded reports bounded traversal exhaustion.
	ErrLimitExceeded = errors.New("implicit connection limit exceeded")
	// ErrNotFound reports that no matching name exists in the selected scope.
	ErrNotFound = errors.New("implicit connection not found")
	// ErrAmbiguous reports that a name identifies more than one object.
	ErrAmbiguous = errors.New("ambiguous implicit connection")
)

// Document is one already-parsed OpenAPI document and its caller-defined
// identity. Resolution never loads the URI.
type Document struct {
	URI  string
	Root jsonvalue.Value
}

// Match identifies one resolved object without discarding its source.
type Match struct {
	DocumentURI string
	Pointer     string
	Value       jsonvalue.Value
}

// ComponentKind selects one fixed Components Object map.
type ComponentKind uint8

const (
	Schemas ComponentKind = iota
	Responses
	Parameters
	Examples
	RequestBodies
	Headers
	SecuritySchemes
	Links
	Callbacks
	PathItems
)

// Scope selects the document used for component and tag names.
type Scope uint8

const (
	// EntryDocument implements the OpenAPI interoperability recommendation.
	EntryDocument Scope = iota
	// CurrentDocument opts into referenced-document name resolution.
	CurrentDocument
)

// Limits bounds name lookup over untrusted parsed documents.
type Limits struct {
	MaxDocuments  int
	MaxNames      int
	MaxOperations int
	MaxDepth      int
}

// DefaultLimits returns conservative implicit-resolution limits.
func DefaultLimits() Limits {
	return Limits{
		MaxDocuments:  1_000,
		MaxNames:      100_000,
		MaxOperations: 100_000,
		MaxDepth:      128,
	}
}

// ComponentOptions controls component-name scope and lookup limits.
type ComponentOptions struct {
	Scope  Scope
	Limits Limits
}

// NameOptions controls tag-name scope and lookup limits.
type NameOptions struct {
	Scope  Scope
	Limits Limits
}

// OperationOptions controls cross-document operation-name traversal limits.
type OperationOptions struct {
	Limits Limits
}

// ResolveComponent resolves a Components Object name. The zero-value options
// resolve from the entry document, as recommended by OpenAPI.
func ResolveComponent(
	entry Document,
	current Document,
	kind ComponentKind,
	name string,
	options ComponentOptions,
) (Match, error) {
	section, valid := componentSection(kind)
	if !valid || name == "" || !validScope(options.Scope) {
		return Match{}, ErrInvalidInput
	}
	limits, err := effectiveLimits(options.Limits)
	if err != nil {
		return Match{}, err
	}
	document := selectedDocument(entry, current, options.Scope)
	if document.Root.Kind() != jsonvalue.ObjectKind {
		return Match{}, ErrInvalidInput
	}
	components, exists := document.Root.Lookup("components")
	if !exists {
		return Match{}, ErrNotFound
	}
	componentMaps, valid := components.Members()
	if !valid {
		return Match{}, ErrInvalidInput
	}
	var selected jsonvalue.Value
	foundSection := false
	for _, componentMap := range componentMaps {
		if componentMap.Name == section {
			selected = componentMap.Value
			foundSection = true
			break
		}
	}
	if !foundSection {
		return Match{}, ErrNotFound
	}
	members, valid := selected.Members()
	if !valid {
		return Match{}, ErrInvalidInput
	}
	if len(members) > limits.MaxNames {
		return Match{}, ErrLimitExceeded
	}
	for _, member := range members {
		if member.Name == name {
			return Match{
				DocumentURI: document.URI,
				Pointer:     "/components/" + section + "/" + escapePointer(name),
				Value:       member.Value,
			}, nil
		}
	}
	return Match{}, ErrNotFound
}

// ResolveTag resolves a top-level Tag Object by name. The zero-value options
// resolve from the entry document, as recommended by OpenAPI.
func ResolveTag(
	entry Document,
	current Document,
	name string,
	options NameOptions,
) (Match, error) {
	if name == "" || !validScope(options.Scope) {
		return Match{}, ErrInvalidInput
	}
	limits, err := effectiveLimits(options.Limits)
	if err != nil {
		return Match{}, err
	}
	document := selectedDocument(entry, current, options.Scope)
	if document.Root.Kind() != jsonvalue.ObjectKind {
		return Match{}, ErrInvalidInput
	}
	tagsValue, exists := document.Root.Lookup("tags")
	if !exists {
		return Match{}, ErrNotFound
	}
	tags, valid := tagsValue.Elements()
	if !valid {
		return Match{}, ErrInvalidInput
	}
	if len(tags) > limits.MaxNames {
		return Match{}, ErrLimitExceeded
	}
	for index, tag := range tags {
		tagName, valid := stringMember(tag, "name")
		if !valid {
			return Match{}, ErrInvalidInput
		}
		if tagName == name {
			return Match{
				DocumentURI: document.URI,
				Pointer:     "/tags/" + strconv.Itoa(index),
				Value:       tag,
			}, nil
		}
	}
	return Match{}, ErrNotFound
}

// ResolveOperationID considers Operation Objects in every supplied parsed
// document. Multiple matches are reported as ambiguous.
func ResolveOperationID(
	documents []Document,
	operationID string,
	options OperationOptions,
) (Match, error) {
	if operationID == "" {
		return Match{}, ErrInvalidInput
	}
	limits, err := effectiveLimits(options.Limits)
	if err != nil {
		return Match{}, err
	}
	if len(documents) == 0 {
		return Match{}, ErrNotFound
	}
	if len(documents) > limits.MaxDocuments {
		return Match{}, ErrLimitExceeded
	}

	var result Match
	matches := 0
	state := operationScan{limits: limits, target: operationID}
	for _, document := range documents {
		if document.Root.Kind() != jsonvalue.ObjectKind {
			return Match{}, ErrInvalidInput
		}
		found, scanErr := state.document(document)
		if scanErr != nil {
			return Match{}, scanErr
		}
		for _, match := range found {
			result = match
			matches++
			if matches > 1 {
				return Match{}, ErrAmbiguous
			}
		}
	}
	if matches == 0 {
		return Match{}, ErrNotFound
	}
	return result, nil
}

type operationScan struct {
	limits     Limits
	target     string
	names      int
	operations int
}

type pathItemLocation struct {
	value   jsonvalue.Value
	pointer string
	depth   int
}

func (scan *operationScan) document(document Document) ([]Match, error) {
	queue, err := rootPathItems(document.Root)
	if err != nil {
		return nil, err
	}
	matches := make([]Match, 0, 1)
	for len(queue) > 0 {
		location := queue[0]
		queue = queue[1:]
		if location.depth > scan.limits.MaxDepth {
			return nil, ErrLimitExceeded
		}
		members, valid := location.value.Members()
		if !valid {
			return nil, ErrInvalidInput
		}
		for _, member := range members {
			if !isHTTPMethod(member.Name) {
				continue
			}
			scan.operations++
			if scan.operations > scan.limits.MaxOperations {
				return nil, ErrLimitExceeded
			}
			operationPointer := location.pointer + "/" + member.Name
			operationMembers, valid := member.Value.Members()
			if !valid {
				return nil, ErrInvalidInput
			}
			if identifierValue, exists := member.Value.Lookup("operationId"); exists {
				identifier, valid := identifierValue.Text()
				if !valid {
					return nil, ErrInvalidInput
				}
				scan.names++
				if scan.names > scan.limits.MaxNames {
					return nil, ErrLimitExceeded
				}
				if identifier == scan.target {
					matches = append(matches, Match{
						DocumentURI: document.URI,
						Pointer:     operationPointer,
						Value:       member.Value,
					})
				}
			}
			for _, operationMember := range operationMembers {
				if operationMember.Name != "callbacks" {
					continue
				}
				callbackMembers, valid := operationMember.Value.Members()
				if !valid {
					return nil, ErrInvalidInput
				}
				for _, callbackMember := range callbackMembers {
					callbacks, callbackErr := callbackPathItems(
						callbackMember.Value,
						operationPointer+"/callbacks/"+
							escapePointer(callbackMember.Name),
						location.depth+1,
					)
					if callbackErr != nil {
						return nil, callbackErr
					}
					queue = append(queue, callbacks...)
				}
			}
		}
	}
	return matches, nil
}

func rootPathItems(root jsonvalue.Value) ([]pathItemLocation, error) {
	queue := make([]pathItemLocation, 0)
	for _, section := range []string{"paths", "webhooks"} {
		value, exists := root.Lookup(section)
		if !exists {
			continue
		}
		members, valid := value.Members()
		if !valid {
			return nil, ErrInvalidInput
		}
		for _, member := range members {
			queue = append(queue, pathItemLocation{
				value: member.Value, pointer: "/" + section + "/" + escapePointer(member.Name), depth: 1,
			})
		}
	}
	components, exists := root.Lookup("components")
	if !exists {
		return queue, nil
	}
	if _, valid := components.Members(); !valid {
		return nil, ErrInvalidInput
	}
	pathItems, exists := components.Lookup("pathItems")
	if exists {
		members, valid := pathItems.Members()
		if !valid {
			return nil, ErrInvalidInput
		}
		for _, member := range members {
			queue = append(queue, pathItemLocation{
				value:   member.Value,
				pointer: "/components/pathItems/" + escapePointer(member.Name),
				depth:   1,
			})
		}
	}
	callbacks, exists := components.Lookup("callbacks")
	if !exists {
		return queue, nil
	}
	members, valid := callbacks.Members()
	if !valid {
		return nil, ErrInvalidInput
	}
	for _, member := range members {
		locations, err := callbackPathItems(
			member.Value,
			"/components/callbacks/"+escapePointer(member.Name),
			1,
		)
		if err != nil {
			return nil, err
		}
		queue = append(queue, locations...)
	}
	return queue, nil
}

func callbackPathItems(
	callback jsonvalue.Value,
	pointer string,
	depth int,
) ([]pathItemLocation, error) {
	if _, reference := callback.Lookup("$ref"); reference {
		return nil, nil
	}
	members, valid := callback.Members()
	if !valid {
		return nil, ErrInvalidInput
	}
	locations := make([]pathItemLocation, 0, len(members))
	for _, member := range members {
		locations = append(locations, pathItemLocation{
			value:   member.Value,
			pointer: pointer + "/" + escapePointer(member.Name),
			depth:   depth,
		})
	}
	return locations, nil
}

func componentSection(kind ComponentKind) (string, bool) {
	sections := [...]string{
		"schemas", "responses", "parameters", "examples", "requestBodies",
		"headers", "securitySchemes", "links", "callbacks", "pathItems",
	}
	if int(kind) >= len(sections) {
		return "", false
	}
	return sections[kind], true
}

func effectiveLimits(limits Limits) (Limits, error) {
	if limits.MaxDocuments < 0 || limits.MaxNames < 0 ||
		limits.MaxOperations < 0 || limits.MaxDepth < 0 {
		return Limits{}, ErrInvalidInput
	}
	defaults := DefaultLimits()
	if limits.MaxDocuments == 0 {
		limits.MaxDocuments = defaults.MaxDocuments
	}
	if limits.MaxNames == 0 {
		limits.MaxNames = defaults.MaxNames
	}
	if limits.MaxOperations == 0 {
		limits.MaxOperations = defaults.MaxOperations
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaults.MaxDepth
	}
	return limits, nil
}

func selectedDocument(entry, current Document, scope Scope) Document {
	if scope == CurrentDocument {
		return current
	}
	return entry
}

func validScope(scope Scope) bool {
	return scope == EntryDocument || scope == CurrentDocument
}

func stringMember(value jsonvalue.Value, name string) (string, bool) {
	member, exists := value.Lookup(name)
	if !exists {
		return "", false
	}
	return member.Text()
}

func isHTTPMethod(name string) bool {
	switch name {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	default:
		return false
	}
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
