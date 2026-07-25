// Package parse provides bounded strict and preserving OpenRPC JSON parsing.
package parse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrUnknownField reports a standard-looking field in strict mode.
	ErrUnknownField = errors.New("openrpc parse: unknown field")
	// ErrMethodLimit reports a methods array beyond the configured bound.
	ErrMethodLimit = errors.New("openrpc parse: method limit exceeded")
	// ErrComponentLimit reports more reusable components than allowed.
	ErrComponentLimit = errors.New("openrpc parse: component limit exceeded")
	// ErrParameterLimit reports too many parameters on one method.
	ErrParameterLimit = errors.New("openrpc parse: parameter limit exceeded")
	// ErrServerLimit reports too many servers in one array.
	ErrServerLimit = errors.New("openrpc parse: server limit exceeded")
	// ErrServerVariableLimit reports too many variables on one server.
	ErrServerVariableLimit = errors.New("openrpc parse: server variable limit exceeded")
	// ErrTagLimit reports too many tags in one array.
	ErrTagLimit = errors.New("openrpc parse: tag limit exceeded")
	// ErrErrorLimit reports too many errors in one array.
	ErrErrorLimit = errors.New("openrpc parse: error limit exceeded")
	// ErrLinkLimit reports too many links in one array.
	ErrLinkLimit = errors.New("openrpc parse: link limit exceeded")
	// ErrExampleLimit reports too many examples in one array.
	ErrExampleLimit = errors.New("openrpc parse: example limit exceeded")
	// ErrInvalidObject reports a field with the wrong structural JSON kind.
	ErrInvalidObject = errors.New("openrpc parse: invalid object structure")
	// ErrInvalidOptions reports non-positive parser-specific limits.
	ErrInvalidOptions = errors.New("openrpc parse: invalid options")
)

// UnknownFieldMode controls standard-looking fields not defined by OpenRPC.
type UnknownFieldMode uint8

const (
	// RejectUnknownFields makes unknown standard fields a parse error.
	RejectUnknownFields UnknownFieldMode = iota
	// PreserveUnknownFields retains unknown standard fields losslessly.
	PreserveUnknownFields
)

// Options controls parsing behavior and resource bounds.
type Options struct {
	JSON               jsonvalue.Policy
	UnknownFields      UnknownFieldMode
	MaxMethods         int
	MaxComponents      int
	MaxParameters      int
	MaxServers         int
	MaxServerVariables int
	MaxTags            int
	MaxErrors          int
	MaxLinks           int
	MaxExamples        int
}

// DefaultOptions returns strict parsing with explicit finite bounds.
func DefaultOptions() Options {
	return Options{
		JSON:               jsonvalue.DefaultPolicy(),
		UnknownFields:      RejectUnknownFields,
		MaxMethods:         10_000,
		MaxComponents:      100_000,
		MaxParameters:      10_000,
		MaxServers:         10_000,
		MaxServerVariables: 10_000,
		MaxTags:            10_000,
		MaxErrors:          10_000,
		MaxLinks:           10_000,
		MaxExamples:        10_000,
	}
}

// Error is a safe structural parsing error at an RFC 6901 JSON Pointer.
type Error struct {
	Pointer string
	Err     error
}

// Error implements error without document values.
func (err *Error) Error() string {
	return fmt.Sprintf("openrpc parse at %s: %v", err.Pointer, err.Err)
}

// Unwrap supports errors.Is for stable error categories.
func (err *Error) Unwrap() error { return err.Err }

// Result contains an immutable typed document and the exact accepted JSON.
type Result struct {
	document openrpc.Document
	source   jsonvalue.Value
}

// Document returns the immutable typed document.
func (result Result) Document() openrpc.Document { return result.document }

// PreservingJSON returns an owned copy of the exact accepted input.
func (result Result) PreservingJSON() []byte { return result.source.Bytes() }

// Decode parses one OpenRPC JSON document without filesystem or network I/O.
func Decode(input []byte, options Options) (Result, error) {
	if options.MaxMethods <= 0 || options.MaxComponents <= 0 ||
		options.MaxParameters <= 0 || options.MaxServers <= 0 ||
		options.MaxServerVariables <= 0 ||
		options.MaxTags <= 0 || options.MaxErrors <= 0 ||
		options.MaxLinks <= 0 || options.MaxExamples <= 0 ||
		(options.UnknownFields != RejectUnknownFields &&
			options.UnknownFields != PreserveUnknownFields) {
		return Result{}, ErrInvalidOptions
	}

	source, err := jsonvalue.Parse(input, options.JSON)
	if err != nil {
		return Result{}, err
	}

	object, err := decodeObject(input)
	if err != nil {
		return Result{}, at("", err)
	}
	exts, unknown, err := collectFields(object, rootFields(), options, "", true)
	if err != nil {
		return Result{}, err
	}

	versionText, err := requiredString(object, "openrpc", "/openrpc")
	if err != nil {
		return Result{}, err
	}
	version, err := openrpc.ParseVersion(versionText)
	if err != nil {
		return Result{}, at("/openrpc", err)
	}

	infoRaw, err := requiredRaw(object, "info", "/info")
	if err != nil {
		return Result{}, err
	}
	info, err := parseInfo(infoRaw, options, "/info")
	if err != nil {
		return Result{}, err
	}

	methodsRaw, err := requiredArray(object, "methods", "/methods")
	if err != nil {
		return Result{}, err
	}
	if len(methodsRaw) > options.MaxMethods {
		return Result{}, at("/methods", ErrMethodLimit)
	}
	methods := make([]openrpc.MethodOrReference, len(methodsRaw))
	for index, raw := range methodsRaw {
		method, parseErr := parseMethodOrReference(
			raw,
			options,
			fmt.Sprintf("/methods/%d", index),
		)
		if parseErr != nil {
			return Result{}, parseErr
		}
		methods[index] = method
	}

	schemaURI, err := optionalStringField(object, "$schema", "/$schema")
	if err != nil {
		return Result{}, err
	}
	var externalDocs *openrpc.ExternalDocumentation
	if raw, exists := object["externalDocs"]; exists {
		parsed, parseErr := parseExternalDocumentation(raw, options, "/externalDocs")
		if parseErr != nil {
			return Result{}, parseErr
		}
		externalDocs = &parsed
	}
	servers, hasServers, err := parseOptionalServers(object, "servers", options, "/servers")
	if err != nil {
		return Result{}, err
	}
	var components *openrpc.Components
	if raw, exists := object["components"]; exists {
		parsed, parseErr := parseComponents(raw, options, "/components")
		if parseErr != nil {
			return Result{}, parseErr
		}
		components = &parsed
	}

	document, _ := openrpc.NewDocument(openrpc.DocumentInput{
		Version:       version,
		SchemaURI:     schemaURI,
		Info:          &info,
		ExternalDocs:  externalDocs,
		Servers:       servers,
		HasServers:    hasServers,
		Methods:       methods,
		Components:    components,
		Extensions:    exts,
		UnknownFields: unknown,
	})
	return Result{document: document, source: source}, nil
}

func parseInfo(raw json.RawMessage, options Options, pointer string) (openrpc.Info, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.Info{}, at(pointer, err)
	}
	exts, unknown, err := collectFields(object, infoFields(), options, pointer, true)
	if err != nil {
		return openrpc.Info{}, err
	}
	title, err := requiredString(object, "title", pointer+"/title")
	if err != nil {
		return openrpc.Info{}, err
	}
	version, err := requiredString(object, "version", pointer+"/version")
	if err != nil {
		return openrpc.Info{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.Info{}, err
	}
	terms, err := optionalStringField(object, "termsOfService", pointer+"/termsOfService")
	if err != nil {
		return openrpc.Info{}, err
	}

	var contact *openrpc.Contact
	if contactRaw, exists := object["contact"]; exists {
		parsed, parseErr := parseContact(contactRaw, options, pointer+"/contact")
		if parseErr != nil {
			return openrpc.Info{}, parseErr
		}
		contact = &parsed
	}
	var license *openrpc.License
	if licenseRaw, exists := object["license"]; exists {
		parsed, parseErr := parseLicense(licenseRaw, options, pointer+"/license")
		if parseErr != nil {
			return openrpc.Info{}, parseErr
		}
		license = &parsed
	}

	info, err := openrpc.NewInfo(openrpc.InfoInput{
		Title:          title,
		Version:        version,
		Description:    description,
		TermsOfService: terms,
		Contact:        contact,
		License:        license,
		Extensions:     exts,
		UnknownFields:  unknown,
	})
	if err != nil {
		return openrpc.Info{}, at(pointer, err)
	}
	return info, nil
}

func parseContact(raw json.RawMessage, options Options, pointer string) (openrpc.Contact, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.Contact{}, at(pointer, err)
	}
	exts, unknown, err := collectFields(object, stringSet("name", "email", "url"), options, pointer, true)
	if err != nil {
		return openrpc.Contact{}, err
	}
	name, err := optionalStringField(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.Contact{}, err
	}
	email, err := optionalStringField(object, "email", pointer+"/email")
	if err != nil {
		return openrpc.Contact{}, err
	}
	url, err := optionalStringField(object, "url", pointer+"/url")
	if err != nil {
		return openrpc.Contact{}, err
	}
	return openrpc.NewContact(openrpc.ContactInput{
		Name: name, Email: email, URL: url,
		Extensions: exts, UnknownFields: unknown,
	})
}

func parseLicense(raw json.RawMessage, options Options, pointer string) (openrpc.License, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.License{}, at(pointer, err)
	}
	exts, unknown, err := collectFields(object, stringSet("name", "url"), options, pointer, true)
	if err != nil {
		return openrpc.License{}, err
	}
	name, err := optionalStringField(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.License{}, err
	}
	url, err := optionalStringField(object, "url", pointer+"/url")
	if err != nil {
		return openrpc.License{}, err
	}
	return openrpc.NewLicense(openrpc.LicenseInput{
		Name: name, URL: url, Extensions: exts, UnknownFields: unknown,
	})
}

func parseMethodOrReference(raw json.RawMessage, options Options, pointer string) (openrpc.MethodOrReference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.MethodOrReference{}, at(pointer, err)
	}
	if _, isReference := object["$ref"]; isReference {
		ref, refErr := parseReferenceObject(object, pointer)
		if refErr != nil {
			return openrpc.MethodOrReference{}, refErr
		}
		return openrpc.MethodReference(ref), nil
	}

	exts, unknown, err := collectFields(object, methodFields(), options, pointer, true)
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	name, err := requiredString(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	paramsRaw, err := requiredArray(object, "params", pointer+"/params")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	if len(paramsRaw) > options.MaxParameters {
		return openrpc.MethodOrReference{}, at(pointer+"/params", ErrParameterLimit)
	}
	params := make([]openrpc.ContentDescriptorOrReference, len(paramsRaw))
	for index, paramRaw := range paramsRaw {
		param, parseErr := parseDescriptorOrReference(
			paramRaw,
			options,
			fmt.Sprintf("%s/params/%d", pointer, index),
		)
		if parseErr != nil {
			return openrpc.MethodOrReference{}, parseErr
		}
		params[index] = param
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	summary, err := optionalStringField(object, "summary", pointer+"/summary")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	deprecated, err := optionalBoolField(object, "deprecated", pointer+"/deprecated")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	structure, err := optionalParamStructure(object, pointer)
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}

	var result *openrpc.ContentDescriptorOrReference
	if resultRaw, exists := object["result"]; exists {
		parsed, parseErr := parseDescriptorOrReference(resultRaw, options, pointer+"/result")
		if parseErr != nil {
			return openrpc.MethodOrReference{}, parseErr
		}
		result = &parsed
	}
	servers, hasServers, err := parseOptionalServers(object, "servers", options, pointer+"/servers")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	tags, hasTags, err := parseOptionalTags(object, options, pointer+"/tags")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	errorsList, hasErrors, err := parseOptionalErrors(object, options, pointer+"/errors")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	links, hasLinks, err := parseOptionalLinks(object, options, pointer+"/links")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	examples, hasExamples, err := parseOptionalExamplePairings(object, options, pointer+"/examples")
	if err != nil {
		return openrpc.MethodOrReference{}, err
	}
	var externalDocs *openrpc.ExternalDocumentation
	if raw, exists := object["externalDocs"]; exists {
		parsed, parseErr := parseExternalDocumentation(raw, options, pointer+"/externalDocs")
		if parseErr != nil {
			return openrpc.MethodOrReference{}, parseErr
		}
		externalDocs = &parsed
	}
	method, err := openrpc.NewMethod(openrpc.MethodInput{
		Name:           name,
		Description:    description,
		Summary:        summary,
		Servers:        servers,
		HasServers:     hasServers,
		Tags:           tags,
		HasTags:        hasTags,
		Params:         params,
		ParamStructure: structure,
		Result:         result,
		Errors:         errorsList,
		HasErrors:      hasErrors,
		Links:          links,
		HasLinks:       hasLinks,
		Examples:       examples,
		HasExamples:    hasExamples,
		Deprecated:     deprecated,
		ExternalDocs:   externalDocs,
		Extensions:     exts,
		UnknownFields:  unknown,
	})
	if err != nil {
		return openrpc.MethodOrReference{}, at(pointer, err)
	}
	return openrpc.MethodValue(method), nil
}

func parseDescriptorOrReference(raw json.RawMessage, options Options, pointer string) (openrpc.ContentDescriptorOrReference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, at(pointer, err)
	}
	if _, isReference := object["$ref"]; isReference {
		ref, refErr := parseReferenceObject(object, pointer)
		if refErr != nil {
			return openrpc.ContentDescriptorOrReference{}, refErr
		}
		return openrpc.ContentDescriptorReference(ref), nil
	}
	exts, unknown, err := collectFields(object, descriptorFields(), options, pointer, true)
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, err
	}
	name, err := requiredString(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, err
	}
	schemaRaw, err := requiredRaw(object, "schema", pointer+"/schema")
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, err
	}
	schema, err := jsonschema.Parse(schemaRaw, options.JSON)
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, at(pointer+"/schema", err)
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, err
	}
	summary, err := optionalStringField(object, "summary", pointer+"/summary")
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, err
	}
	required, err := optionalBoolField(object, "required", pointer+"/required")
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, err
	}
	deprecated, err := optionalBoolField(object, "deprecated", pointer+"/deprecated")
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, err
	}
	descriptor, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{
		Name: name, Description: description, Summary: summary, Schema: &schema,
		Required: required, Deprecated: deprecated,
		Extensions: exts, UnknownFields: unknown,
	})
	if err != nil {
		return openrpc.ContentDescriptorOrReference{}, at(pointer, err)
	}
	return openrpc.ContentDescriptorValue(descriptor), nil
}

func parseReferenceObject(object map[string]json.RawMessage, pointer string) (openrpc.Reference, error) {
	if len(object) != 1 {
		return openrpc.Reference{}, at(pointer, ErrInvalidObject)
	}
	ref, err := requiredString(object, "$ref", pointer+"/$ref")
	if err != nil {
		return openrpc.Reference{}, err
	}
	value, err := openrpc.NewReference(ref)
	if err != nil {
		return openrpc.Reference{}, at(pointer, err)
	}
	return value, nil
}

func parseOptionalTags(object map[string]json.RawMessage, options Options, pointer string) ([]openrpc.TagOrReference, bool, error) {
	entries, present, err := optionalRawArray(object, "tags", options.MaxTags, ErrTagLimit, pointer)
	if err != nil || !present {
		return nil, present, err
	}
	values := make([]openrpc.TagOrReference, len(entries))
	for index, entry := range entries {
		value, parseErr := parseTagOrReference(entry, options, fmt.Sprintf("%s/%d", pointer, index))
		if parseErr != nil {
			return nil, false, parseErr
		}
		values[index] = value
	}
	return values, true, nil
}

func parseTagOrReference(raw json.RawMessage, options Options, pointer string) (openrpc.TagOrReference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.TagOrReference{}, at(pointer, err)
	}
	if _, isReference := object["$ref"]; isReference {
		reference, parseErr := parseReferenceObject(object, pointer)
		if parseErr != nil {
			return openrpc.TagOrReference{}, parseErr
		}
		return openrpc.TagReference(reference), nil
	}
	tag, err := parseTagObject(object, options, pointer)
	if err != nil {
		return openrpc.TagOrReference{}, err
	}
	return openrpc.TagValue(tag), nil
}

func parseTagObject(object map[string]json.RawMessage, options Options, pointer string) (openrpc.Tag, error) {
	exts, unknown, err := collectFields(
		object,
		stringSet("name", "description", "externalDocs"),
		options,
		pointer,
		true,
	)
	if err != nil {
		return openrpc.Tag{}, err
	}
	name, err := requiredString(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.Tag{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.Tag{}, err
	}
	var externalDocs *openrpc.ExternalDocumentation
	if raw, exists := object["externalDocs"]; exists {
		parsed, parseErr := parseExternalDocumentation(raw, options, pointer+"/externalDocs")
		if parseErr != nil {
			return openrpc.Tag{}, parseErr
		}
		externalDocs = &parsed
	}
	tag, err := openrpc.NewTag(openrpc.TagInput{
		Name: name, Description: description, ExternalDocs: externalDocs,
		Extensions: exts, UnknownFields: unknown,
	})
	if err != nil {
		return openrpc.Tag{}, at(pointer, err)
	}
	return tag, nil
}

func parseOptionalErrors(object map[string]json.RawMessage, options Options, pointer string) ([]openrpc.ErrorOrReference, bool, error) {
	entries, present, err := optionalRawArray(object, "errors", options.MaxErrors, ErrErrorLimit, pointer)
	if err != nil || !present {
		return nil, present, err
	}
	values := make([]openrpc.ErrorOrReference, len(entries))
	for index, entry := range entries {
		value, parseErr := parseErrorOrReference(entry, options, fmt.Sprintf("%s/%d", pointer, index))
		if parseErr != nil {
			return nil, false, parseErr
		}
		values[index] = value
	}
	return values, true, nil
}

func parseErrorOrReference(raw json.RawMessage, options Options, pointer string) (openrpc.ErrorOrReference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.ErrorOrReference{}, at(pointer, err)
	}
	if _, isReference := object["$ref"]; isReference {
		reference, parseErr := parseReferenceObject(object, pointer)
		if parseErr != nil {
			return openrpc.ErrorOrReference{}, parseErr
		}
		return openrpc.ErrorReference(reference), nil
	}
	value, err := parseErrorObject(object, options, pointer)
	if err != nil {
		return openrpc.ErrorOrReference{}, err
	}
	return openrpc.ErrorValue(value), nil
}

func parseErrorObject(object map[string]json.RawMessage, options Options, pointer string) (openrpc.Error, error) {
	exts, unknown, err := collectFields(
		object,
		stringSet("code", "message", "data"),
		options,
		pointer,
		true,
	)
	if err != nil {
		return openrpc.Error{}, err
	}
	codeRaw, err := requiredRaw(object, "code", pointer+"/code")
	if err != nil {
		return openrpc.Error{}, err
	}
	code, err := openrpc.ParseInteger(string(bytes.TrimSpace(codeRaw)))
	if err != nil {
		return openrpc.Error{}, at(pointer+"/code", err)
	}
	message, err := requiredString(object, "message", pointer+"/message")
	if err != nil {
		return openrpc.Error{}, err
	}
	data, _ := optionalJSONValue(object, "data", options, pointer+"/data")
	value, _ := openrpc.NewError(openrpc.ErrorInput{
		Code: code, Message: message, HasMessage: true, Data: data,
		Extensions: exts, UnknownFields: unknown,
	})
	return value, nil
}

func parseOptionalLinks(object map[string]json.RawMessage, options Options, pointer string) ([]openrpc.LinkOrReference, bool, error) {
	entries, present, err := optionalRawArray(object, "links", options.MaxLinks, ErrLinkLimit, pointer)
	if err != nil || !present {
		return nil, present, err
	}
	values := make([]openrpc.LinkOrReference, len(entries))
	for index, entry := range entries {
		value, parseErr := parseLinkOrReference(entry, options, fmt.Sprintf("%s/%d", pointer, index))
		if parseErr != nil {
			return nil, false, parseErr
		}
		values[index] = value
	}
	return values, true, nil
}

func parseLinkOrReference(raw json.RawMessage, options Options, pointer string) (openrpc.LinkOrReference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.LinkOrReference{}, at(pointer, err)
	}
	if _, isReference := object["$ref"]; isReference {
		reference, parseErr := parseReferenceObject(object, pointer)
		if parseErr != nil {
			return openrpc.LinkOrReference{}, parseErr
		}
		return openrpc.LinkReference(reference), nil
	}
	value, err := parseLinkObject(object, options, pointer)
	if err != nil {
		return openrpc.LinkOrReference{}, err
	}
	return openrpc.LinkValue(value), nil
}

func parseLinkObject(object map[string]json.RawMessage, options Options, pointer string) (openrpc.Link, error) {
	exts, unknown, err := collectFields(
		object,
		stringSet("name", "summary", "description", "method", "params", "server"),
		options,
		pointer,
		true,
	)
	if err != nil {
		return openrpc.Link{}, err
	}
	name, _ := optionalJSONValue(object, "name", options, pointer+"/name")
	summary, err := optionalStringField(object, "summary", pointer+"/summary")
	if err != nil {
		return openrpc.Link{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.Link{}, err
	}
	method, err := optionalStringField(object, "method", pointer+"/method")
	if err != nil {
		return openrpc.Link{}, err
	}
	params, _ := optionalJSONValue(object, "params", options, pointer+"/params")
	var server *openrpc.Server
	if raw, exists := object["server"]; exists {
		parsed, parseErr := parseServer(raw, options, pointer+"/server")
		if parseErr != nil {
			return openrpc.Link{}, parseErr
		}
		server = &parsed
	}
	value, _ := openrpc.NewLink(openrpc.LinkInput{
		Name: name, Summary: summary, Description: description,
		Method: method, Params: params, Server: server,
		Extensions: exts, UnknownFields: unknown,
	})
	return value, nil
}

func parseOptionalExamplePairings(object map[string]json.RawMessage, options Options, pointer string) ([]openrpc.ExamplePairingOrReference, bool, error) {
	entries, present, err := optionalRawArray(object, "examples", options.MaxExamples, ErrExampleLimit, pointer)
	if err != nil || !present {
		return nil, present, err
	}
	values := make([]openrpc.ExamplePairingOrReference, len(entries))
	for index, entry := range entries {
		value, parseErr := parseExamplePairingOrReference(entry, options, fmt.Sprintf("%s/%d", pointer, index))
		if parseErr != nil {
			return nil, false, parseErr
		}
		values[index] = value
	}
	return values, true, nil
}

func parseExamplePairingOrReference(raw json.RawMessage, options Options, pointer string) (openrpc.ExamplePairingOrReference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.ExamplePairingOrReference{}, at(pointer, err)
	}
	if _, isReference := object["$ref"]; isReference {
		reference, parseErr := parseReferenceObject(object, pointer)
		if parseErr != nil {
			return openrpc.ExamplePairingOrReference{}, parseErr
		}
		return openrpc.ExamplePairingReference(reference), nil
	}
	value, err := parseExamplePairingObject(object, options, pointer)
	if err != nil {
		return openrpc.ExamplePairingOrReference{}, err
	}
	return openrpc.ExamplePairingValue(value), nil
}

func parseExamplePairingObject(object map[string]json.RawMessage, options Options, pointer string) (openrpc.ExamplePairing, error) {
	_, unknown, _ := collectFieldsAllowAdditional(
		object,
		stringSet("name", "description", "params", "result"),
		options,
		pointer,
		false,
	)
	name, err := requiredString(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.ExamplePairing{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.ExamplePairing{}, err
	}
	paramsRaw, err := requiredArray(object, "params", pointer+"/params")
	if err != nil {
		return openrpc.ExamplePairing{}, err
	}
	if len(paramsRaw) > options.MaxParameters {
		return openrpc.ExamplePairing{}, at(pointer+"/params", ErrParameterLimit)
	}
	params := make([]openrpc.ExampleOrReference, len(paramsRaw))
	for index, raw := range paramsRaw {
		value, parseErr := parseExampleOrReference(raw, options, fmt.Sprintf("%s/params/%d", pointer, index))
		if parseErr != nil {
			return openrpc.ExamplePairing{}, parseErr
		}
		params[index] = value
	}
	var result *openrpc.ExampleOrReference
	if raw, exists := object["result"]; exists {
		value, parseErr := parseExampleOrReference(raw, options, pointer+"/result")
		if parseErr != nil {
			return openrpc.ExamplePairing{}, parseErr
		}
		result = &value
	}
	value, err := openrpc.NewExamplePairing(openrpc.ExamplePairingInput{
		Name: name, Description: description, Params: params,
		Result: result, UnknownFields: unknown,
	})
	if err != nil {
		return openrpc.ExamplePairing{}, at(pointer, err)
	}
	return value, nil
}

func parseExampleOrReference(raw json.RawMessage, options Options, pointer string) (openrpc.ExampleOrReference, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.ExampleOrReference{}, at(pointer, err)
	}
	if _, isReference := object["$ref"]; isReference {
		reference, parseErr := parseReferenceObject(object, pointer)
		if parseErr != nil {
			return openrpc.ExampleOrReference{}, parseErr
		}
		return openrpc.ExampleReference(reference), nil
	}
	value, err := parseExampleObject(object, options, pointer)
	if err != nil {
		return openrpc.ExampleOrReference{}, err
	}
	return openrpc.ExampleValue(value), nil
}

func parseExampleObject(object map[string]json.RawMessage, options Options, pointer string) (openrpc.Example, error) {
	exts, unknown, _ := collectFieldsAllowAdditional(
		object,
		stringSet("name", "summary", "description", "value"),
		options,
		pointer,
		true,
	)
	name, err := requiredString(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.Example{}, err
	}
	valueRaw, err := requiredRaw(object, "value", pointer+"/value")
	if err != nil {
		return openrpc.Example{}, err
	}
	value, _ := jsonvalue.Parse(valueRaw, options.JSON)
	summary, err := optionalStringField(object, "summary", pointer+"/summary")
	if err != nil {
		return openrpc.Example{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.Example{}, err
	}
	example, err := openrpc.NewExample(openrpc.ExampleInput{
		Name: name, Summary: summary, Description: description, Value: value,
		Extensions: exts, UnknownFields: unknown,
	})
	if err != nil {
		return openrpc.Example{}, at(pointer, err)
	}
	return example, nil
}

func parseExternalDocumentation(raw json.RawMessage, options Options, pointer string) (openrpc.ExternalDocumentation, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.ExternalDocumentation{}, at(pointer, err)
	}
	exts, unknown, err := collectFields(
		object,
		stringSet("url", "description"),
		options,
		pointer,
		true,
	)
	if err != nil {
		return openrpc.ExternalDocumentation{}, err
	}
	url, err := requiredString(object, "url", pointer+"/url")
	if err != nil {
		return openrpc.ExternalDocumentation{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.ExternalDocumentation{}, err
	}
	value, err := openrpc.NewExternalDocumentation(openrpc.ExternalDocumentationInput{
		URL: url, Description: description,
		Extensions: exts, UnknownFields: unknown,
	})
	if err != nil {
		return openrpc.ExternalDocumentation{}, at(pointer, err)
	}
	return value, nil
}

func parseOptionalServers(object map[string]json.RawMessage, name string, options Options, pointer string) ([]openrpc.Server, bool, error) {
	raw, exists := object[name]
	if !exists {
		return nil, false, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, false, at(pointer, ErrInvalidObject)
	}
	if len(entries) > options.MaxServers {
		return nil, false, at(pointer, ErrServerLimit)
	}
	servers := make([]openrpc.Server, len(entries))
	for index, entry := range entries {
		server, err := parseServer(entry, options, fmt.Sprintf("%s/%d", pointer, index))
		if err != nil {
			return nil, false, err
		}
		servers[index] = server
	}
	return servers, true, nil
}

func parseServer(raw json.RawMessage, options Options, pointer string) (openrpc.Server, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.Server{}, at(pointer, err)
	}
	exts, unknown, err := collectFields(
		object,
		stringSet("url", "name", "description", "summary", "variables"),
		options,
		pointer,
		true,
	)
	if err != nil {
		return openrpc.Server{}, err
	}
	url, err := requiredString(object, "url", pointer+"/url")
	if err != nil {
		return openrpc.Server{}, err
	}
	name, err := optionalStringField(object, "name", pointer+"/name")
	if err != nil {
		return openrpc.Server{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.Server{}, err
	}
	summary, err := optionalStringField(object, "summary", pointer+"/summary")
	if err != nil {
		return openrpc.Server{}, err
	}
	variables, hasVariables, err := parseServerVariables(object, options, pointer+"/variables")
	if err != nil {
		return openrpc.Server{}, err
	}
	server, err := openrpc.NewServer(openrpc.ServerInput{
		URL: url, Name: name, Description: description, Summary: summary,
		Variables: variables, HasVariables: hasVariables,
		Extensions: exts, UnknownFields: unknown,
	})
	if err != nil {
		return openrpc.Server{}, at(pointer, err)
	}
	return server, nil
}

func parseServerVariables(object map[string]json.RawMessage, options Options, pointer string) (map[string]openrpc.ServerVariable, bool, error) {
	raw, exists := object["variables"]
	if !exists {
		return nil, false, nil
	}
	entries, err := decodeObject(raw)
	if err != nil {
		return nil, false, at(pointer, err)
	}
	if len(entries) > options.MaxServerVariables {
		return nil, false, at(pointer, ErrServerVariableLimit)
	}
	variables := make(map[string]openrpc.ServerVariable, len(entries))
	for name, entry := range entries {
		variable, parseErr := parseServerVariable(
			entry,
			options,
			pointer+"/"+escape(name),
		)
		if parseErr != nil {
			return nil, false, parseErr
		}
		variables[name] = variable
	}
	return variables, true, nil
}

func parseServerVariable(raw json.RawMessage, options Options, pointer string) (openrpc.ServerVariable, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.ServerVariable{}, at(pointer, err)
	}
	_, unknown, _ := collectFieldsAllowAdditional(
		object,
		stringSet("default", "description", "enum"),
		options,
		pointer,
		false,
	)
	defaultValue, err := requiredString(object, "default", pointer+"/default")
	if err != nil {
		return openrpc.ServerVariable{}, err
	}
	description, err := optionalStringField(object, "description", pointer+"/description")
	if err != nil {
		return openrpc.ServerVariable{}, err
	}
	values, hasEnum, err := optionalStringArray(object, "enum", pointer+"/enum")
	if err != nil {
		return openrpc.ServerVariable{}, err
	}
	variable, _ := openrpc.NewServerVariable(openrpc.ServerVariableInput{
		Default: &defaultValue, Description: description,
		Enum: values, HasEnum: hasEnum, UnknownFields: unknown,
	})
	return variable, nil
}

func parseComponents(raw json.RawMessage, options Options, pointer string) (openrpc.Components, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return openrpc.Components{}, at(pointer, err)
	}
	_, unknown, _ := collectFieldsAllowAdditional(
		object,
		stringSet("schemas", "links", "errors", "examples", "examplePairings", "contentDescriptors", "tags"),
		options,
		pointer,
		false,
	)
	schemas, hasSchemas, err := parseSchemaMap(object, options, pointer+"/schemas")
	if err != nil {
		return openrpc.Components{}, err
	}
	links, hasLinks, err := parseComponentMap(object, "links", options, pointer+"/links", parseLinkObject)
	if err != nil {
		return openrpc.Components{}, err
	}
	errorsMap, hasErrors, err := parseComponentMap(object, "errors", options, pointer+"/errors", parseErrorObject)
	if err != nil {
		return openrpc.Components{}, err
	}
	examples, hasExamples, err := parseComponentMap(object, "examples", options, pointer+"/examples", parseExampleObject)
	if err != nil {
		return openrpc.Components{}, err
	}
	pairings, hasPairings, err := parseComponentMap(object, "examplePairings", options, pointer+"/examplePairings", parseExamplePairingObject)
	if err != nil {
		return openrpc.Components{}, err
	}
	descriptors, hasDescriptors, err := parseComponentMap(object, "contentDescriptors", options, pointer+"/contentDescriptors", parseContentDescriptorObject)
	if err != nil {
		return openrpc.Components{}, err
	}
	tags, hasTags, err := parseComponentMap(object, "tags", options, pointer+"/tags", parseTagObject)
	if err != nil {
		return openrpc.Components{}, err
	}
	total := len(schemas) + len(links) + len(errorsMap) + len(examples) +
		len(pairings) + len(descriptors) + len(tags)
	if total > options.MaxComponents {
		return openrpc.Components{}, at(pointer, ErrComponentLimit)
	}
	input := openrpc.ComponentsInput{UnknownFields: unknown}
	if hasSchemas {
		input.Schemas = schemas
	}
	if hasLinks {
		input.Links = links
	}
	if hasErrors {
		input.Errors = errorsMap
	}
	if hasExamples {
		input.Examples = examples
	}
	if hasPairings {
		input.ExamplePairings = pairings
	}
	if hasDescriptors {
		input.ContentDescriptors = descriptors
	}
	if hasTags {
		input.Tags = tags
	}
	return openrpc.NewComponents(input)
}

func parseComponentMap[T any](object map[string]json.RawMessage, name string, options Options, pointer string, parser func(map[string]json.RawMessage, Options, string) (T, error)) (map[string]T, bool, error) {
	raw, exists := object[name]
	if !exists {
		return nil, false, nil
	}
	entries, err := decodeObject(raw)
	if err != nil {
		return nil, false, at(pointer, err)
	}
	if len(entries) > options.MaxComponents {
		return nil, false, at(pointer, ErrComponentLimit)
	}
	names := make([]string, 0, len(entries))
	for entryName := range entries {
		names = append(names, entryName)
	}
	sort.Strings(names)
	values := make(map[string]T, len(entries))
	for _, entryName := range names {
		entryObject, decodeErr := decodeObject(entries[entryName])
		if decodeErr != nil {
			return nil, false, at(pointer+"/"+escape(entryName), decodeErr)
		}
		value, parseErr := parser(entryObject, options, pointer+"/"+escape(entryName))
		if parseErr != nil {
			return nil, false, parseErr
		}
		values[entryName] = value
	}
	return values, true, nil
}

func parseContentDescriptorObject(object map[string]json.RawMessage, options Options, pointer string) (openrpc.ContentDescriptor, error) {
	raw, _ := json.Marshal(object)
	value, err := parseDescriptorOrReference(raw, options, pointer)
	if err != nil {
		return openrpc.ContentDescriptor{}, err
	}
	descriptor, ok := value.Descriptor()
	if !ok {
		return openrpc.ContentDescriptor{}, at(pointer, ErrInvalidObject)
	}
	return descriptor, nil
}

func parseSchemaMap(object map[string]json.RawMessage, options Options, pointer string) (map[string]jsonschema.Schema, bool, error) {
	raw, exists := object["schemas"]
	if !exists {
		return nil, false, nil
	}
	entries, err := decodeObject(raw)
	if err != nil {
		return nil, false, at(pointer, err)
	}
	if len(entries) > options.MaxComponents {
		return nil, false, at(pointer, ErrComponentLimit)
	}
	schemas := make(map[string]jsonschema.Schema, len(entries))
	for name, entry := range entries {
		schema, parseErr := jsonschema.Parse(entry, options.JSON)
		if parseErr != nil {
			return nil, false, at(pointer+"/"+escape(name), parseErr)
		}
		schemas[name] = schema
	}
	return schemas, true, nil
}

func collectFields(object map[string]json.RawMessage, known map[string]struct{}, options Options, pointer string, extensible bool) (openrpc.Fields, openrpc.Fields, error) {
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	extensions := make([]openrpc.Field, 0)
	unknown := make([]openrpc.Field, 0)
	for _, name := range names {
		if _, ok := known[name]; ok {
			continue
		}
		value, _ := jsonvalue.Parse(object[name], options.JSON)
		field := openrpc.Field{Name: name, Value: value}
		if extensible && strings.HasPrefix(name, "x-") {
			extensions = append(extensions, field)
			continue
		}
		if options.UnknownFields == RejectUnknownFields {
			return openrpc.Fields{}, openrpc.Fields{}, at(pointer+"/"+escape(name), ErrUnknownField)
		}
		unknown = append(unknown, field)
	}
	exts, _ := openrpc.NewExtensions(extensions...)
	unknownFields, _ := openrpc.NewUnknownFields(unknown...)
	return exts, unknownFields, nil
}

func collectFieldsAllowAdditional(
	object map[string]json.RawMessage,
	known map[string]struct{},
	options Options,
	pointer string,
	extensible bool,
) (openrpc.Fields, openrpc.Fields, error) {
	options.UnknownFields = PreserveUnknownFields
	return collectFields(object, known, options, pointer, extensible)
}

func decodeObject(raw []byte) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, ErrInvalidObject
	}
	return object, nil
}

func requiredRaw(object map[string]json.RawMessage, name string, pointer string) (json.RawMessage, error) {
	raw, ok := object[name]
	if !ok {
		return nil, at(pointer, openrpc.ErrMissingRequiredField)
	}
	return raw, nil
}

func requiredString(object map[string]json.RawMessage, name string, pointer string) (string, error) {
	raw, err := requiredRaw(object, name, pointer)
	if err != nil {
		return "", err
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", at(pointer, ErrInvalidObject)
	}
	return value, nil
}

func optionalStringField(object map[string]json.RawMessage, name string, pointer string) (*string, error) {
	raw, ok := object[name]
	if !ok {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, at(pointer, ErrInvalidObject)
	}
	return &value, nil
}

func optionalBoolField(object map[string]json.RawMessage, name string, pointer string) (*bool, error) {
	raw, ok := object[name]
	if !ok {
		return nil, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, at(pointer, ErrInvalidObject)
	}
	return &value, nil
}

func optionalJSONValue(object map[string]json.RawMessage, name string, options Options, pointer string) (*jsonvalue.Value, error) {
	raw, exists := object[name]
	if !exists {
		return nil, nil
	}
	value, _ := jsonvalue.Parse(raw, options.JSON)
	return &value, nil
}

func optionalRawArray(object map[string]json.RawMessage, name string, limit int, limitError error, pointer string) ([]json.RawMessage, bool, error) {
	raw, exists := object[name]
	if !exists {
		return nil, false, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, false, at(pointer, ErrInvalidObject)
	}
	if len(values) > limit {
		return nil, false, at(pointer, limitError)
	}
	return values, true, nil
}

func optionalStringArray(object map[string]json.RawMessage, name string, pointer string) ([]string, bool, error) {
	raw, exists := object[name]
	if !exists {
		return nil, false, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, false, at(pointer, ErrInvalidObject)
	}
	return values, true, nil
}

func requiredArray(object map[string]json.RawMessage, name string, pointer string) ([]json.RawMessage, error) {
	raw, err := requiredRaw(object, name, pointer)
	if err != nil {
		return nil, err
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, at(pointer, ErrInvalidObject)
	}
	return values, nil
}

func optionalParamStructure(object map[string]json.RawMessage, pointer string) (*openrpc.ParamStructure, error) {
	value, err := optionalStringField(object, "paramStructure", pointer+"/paramStructure")
	if err != nil || value == nil {
		return nil, err
	}
	structure := openrpc.ParamStructure(*value)
	return &structure, nil
}

func at(pointer string, err error) error {
	if pointer == "" {
		pointer = "#"
	} else {
		pointer = "#" + pointer
	}
	return &Error{Pointer: pointer, Err: err}
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func rootFields() map[string]struct{} {
	return stringSet("openrpc", "$schema", "info", "externalDocs", "servers", "methods", "components")
}

func infoFields() map[string]struct{} {
	return stringSet("title", "description", "termsOfService", "version", "contact", "license")
}

func methodFields() map[string]struct{} {
	return stringSet(
		"name", "description", "summary", "servers", "tags", "params",
		"paramStructure", "result", "errors", "links", "examples",
		"deprecated", "externalDocs",
	)
}

func descriptorFields() map[string]struct{} {
	return stringSet("name", "description", "summary", "schema", "required", "deprecated")
}
