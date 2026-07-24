package openrpc

import (
	"errors"

	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

// ErrInvalidParamStructure reports a value outside the specification's closed
// parameter-structure set.
var ErrInvalidParamStructure = errors.New("openrpc: invalid parameter structure")

type optionalBool struct {
	value   bool
	present bool
}

func boolOption(value *bool) optionalBool {
	if value == nil {
		return optionalBool{}
	}
	return optionalBool{value: *value, present: true}
}

func (value optionalBool) get() (bool, bool) { return value.value, value.present }

type optionalValue struct {
	value   jsonvalue.Value
	present bool
}

func valueOption(value *jsonvalue.Value) optionalValue {
	if value == nil {
		return optionalValue{}
	}
	return optionalValue{value: *value, present: true}
}

func (value optionalValue) get() (jsonvalue.Value, bool) {
	return value.value, value.present
}

// Reference is an immutable OpenRPC Reference Object.
type Reference struct {
	ref string
}

// NewReference constructs a Reference Object.
func NewReference(ref string) (Reference, error) {
	if ref == "" {
		return Reference{}, missingField("$ref")
	}
	return Reference{ref: ref}, nil
}

// Ref returns the required reference string.
func (reference Reference) Ref() string { return reference.ref }

// ContentDescriptorInput supplies Content Descriptor Object fields.
type ContentDescriptorInput struct {
	Name          string
	Description   *string
	Summary       *string
	Schema        *jsonschema.Schema
	Required      *bool
	Deprecated    *bool
	Extensions    Fields
	UnknownFields Fields
}

// ContentDescriptor is an immutable OpenRPC Content Descriptor Object.
type ContentDescriptor struct {
	objectFields
	name        string
	description optionalString
	summary     optionalString
	schema      jsonschema.Schema
	required    optionalBool
	deprecated  optionalBool
}

// NewContentDescriptor constructs a Content Descriptor Object.
func NewContentDescriptor(input ContentDescriptorInput) (ContentDescriptor, error) {
	if input.Name == "" {
		return ContentDescriptor{}, missingField("name")
	}
	if input.Schema == nil {
		return ContentDescriptor{}, missingField("schema")
	}
	return ContentDescriptor{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		name:         input.Name,
		description:  stringOption(input.Description),
		summary:      stringOption(input.Summary),
		schema:       *input.Schema,
		required:     boolOption(input.Required),
		deprecated:   boolOption(input.Deprecated),
	}, nil
}

// Name returns the required content name.
func (descriptor ContentDescriptor) Name() string { return descriptor.name }

// Description returns the optional rich-text description.
func (descriptor ContentDescriptor) Description() (string, bool) {
	return descriptor.description.get()
}

// Summary returns the optional short summary.
func (descriptor ContentDescriptor) Summary() (string, bool) {
	return descriptor.summary.get()
}

// Schema returns the required Draft 7 schema.
func (descriptor ContentDescriptor) Schema() jsonschema.Schema { return descriptor.schema }

// Required returns the declared value and whether the field was present.
func (descriptor ContentDescriptor) Required() (bool, bool) { return descriptor.required.get() }

// RequiredOrDefault returns the effective value, whose default is false.
func (descriptor ContentDescriptor) RequiredOrDefault() bool { return descriptor.required.value }

// Deprecated returns the declared value and whether the field was present.
func (descriptor ContentDescriptor) Deprecated() (bool, bool) {
	return descriptor.deprecated.get()
}

// DeprecatedOrDefault returns the effective value, whose default is false.
func (descriptor ContentDescriptor) DeprecatedOrDefault() bool {
	return descriptor.deprecated.value
}

// ContentDescriptorOrReference is the union permitted in OpenRPC document
// locations that accept a descriptor or reusable reference.
type ContentDescriptorOrReference struct {
	descriptor ContentDescriptor
	reference  Reference
	kind       uint8
}

// ContentDescriptorValue constructs the descriptor union case.
func ContentDescriptorValue(value ContentDescriptor) ContentDescriptorOrReference {
	return ContentDescriptorOrReference{descriptor: value, kind: 1}
}

// ContentDescriptorReference constructs the reference union case.
func ContentDescriptorReference(value Reference) ContentDescriptorOrReference {
	return ContentDescriptorOrReference{reference: value, kind: 2}
}

// Descriptor returns the descriptor case and true.
func (value ContentDescriptorOrReference) Descriptor() (ContentDescriptor, bool) {
	return value.descriptor, value.kind == 1
}

// Reference returns the reference case and true.
func (value ContentDescriptorOrReference) Reference() (Reference, bool) {
	return value.reference, value.kind == 2
}

// TagInput supplies Tag Object fields.
type TagInput struct {
	Name          string
	Description   *string
	ExternalDocs  *ExternalDocumentation
	Extensions    Fields
	UnknownFields Fields
}

// Tag is an immutable OpenRPC Tag Object.
type Tag struct {
	objectFields
	name         string
	description  optionalString
	externalDocs ExternalDocumentation
	hasDocs      bool
}

// NewTag constructs a Tag Object.
func NewTag(input TagInput) (Tag, error) {
	if input.Name == "" {
		return Tag{}, missingField("name")
	}
	tag := Tag{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		name:         input.Name,
		description:  stringOption(input.Description),
	}
	if input.ExternalDocs != nil {
		tag.externalDocs = *input.ExternalDocs
		tag.hasDocs = true
	}
	return tag, nil
}

// Name returns the required tag name.
func (tag Tag) Name() string { return tag.name }

// Description returns the optional tag description.
func (tag Tag) Description() (string, bool) { return tag.description.get() }

// ExternalDocs returns the optional external documentation.
func (tag Tag) ExternalDocs() (ExternalDocumentation, bool) {
	return tag.externalDocs, tag.hasDocs
}

// TagOrReference is a Tag Object or Reference Object union.
type TagOrReference struct {
	tag       Tag
	reference Reference
	kind      uint8
}

// TagValue constructs the Tag Object union case.
func TagValue(value Tag) TagOrReference { return TagOrReference{tag: value, kind: 1} }

// TagReference constructs the Reference Object union case.
func TagReference(value Reference) TagOrReference {
	return TagOrReference{reference: value, kind: 2}
}

// Tag returns the Tag Object case and true.
func (value TagOrReference) Tag() (Tag, bool) { return value.tag, value.kind == 1 }

// Reference returns the Reference Object case and true.
func (value TagOrReference) Reference() (Reference, bool) {
	return value.reference, value.kind == 2
}

// ErrorInput supplies Error Object fields. Set HasMessage when representing a
// required but empty message.
type ErrorInput struct {
	Code          Integer
	Message       string
	HasMessage    bool
	Data          *jsonvalue.Value
	Extensions    Fields
	UnknownFields Fields
}

// Error is an immutable OpenRPC Error Object.
type Error struct {
	objectFields
	code    Integer
	message string
	data    optionalValue
}

// NewError constructs an Error Object without narrowing its integer code.
func NewError(input ErrorInput) (Error, error) {
	if input.Code.String() == "" {
		return Error{}, missingField("code")
	}
	if input.Message == "" && !input.HasMessage {
		return Error{}, missingField("message")
	}
	return Error{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		code:         input.Code,
		message:      input.Message,
		data:         valueOption(input.Data),
	}, nil
}

// Code returns the required arbitrary-precision integer code.
func (object Error) Code() Integer { return object.code }

// Message returns the required message.
func (object Error) Message() string { return object.message }

// Data returns the optional arbitrary JSON error data.
func (object Error) Data() (jsonvalue.Value, bool) { return object.data.get() }

// ErrorOrReference is an Error Object or Reference Object union.
type ErrorOrReference struct {
	object    Error
	reference Reference
	kind      uint8
}

// ErrorValue constructs the Error Object union case.
func ErrorValue(value Error) ErrorOrReference { return ErrorOrReference{object: value, kind: 1} }

// ErrorReference constructs the Reference Object union case.
func ErrorReference(value Reference) ErrorOrReference {
	return ErrorOrReference{reference: value, kind: 2}
}

// Error returns the Error Object case and true.
func (value ErrorOrReference) Error() (Error, bool) { return value.object, value.kind == 1 }

// Reference returns the Reference Object case and true.
func (value ErrorOrReference) Reference() (Reference, bool) {
	return value.reference, value.kind == 2
}

// LinkInput supplies Link Object fields.
type LinkInput struct {
	Name          *jsonvalue.Value
	Summary       *string
	Description   *string
	Method        *string
	Params        *jsonvalue.Value
	Server        *Server
	Extensions    Fields
	UnknownFields Fields
}

// Link is an immutable OpenRPC Link Object.
type Link struct {
	objectFields
	name        optionalValue
	summary     optionalString
	description optionalString
	method      optionalString
	params      optionalValue
	server      Server
	hasServer   bool
}

// NewLink constructs a Link Object.
func NewLink(input LinkInput) (Link, error) {
	link := Link{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		name:         valueOption(input.Name),
		summary:      stringOption(input.Summary),
		description:  stringOption(input.Description),
		method:       stringOption(input.Method),
		params:       valueOption(input.Params),
	}
	if input.Server != nil {
		link.server = *input.Server
		link.hasServer = true
	}
	return link, nil
}

// Name returns the optional lossless name value.
func (link Link) Name() (jsonvalue.Value, bool) { return link.name.get() }

// Summary returns the optional summary.
func (link Link) Summary() (string, bool) { return link.summary.get() }

// Description returns the optional rich-text description.
func (link Link) Description() (string, bool) { return link.description.get() }

// Method returns the optional target method name.
func (link Link) Method() (string, bool) { return link.method.get() }

// Params returns the optional lossless parameter map.
func (link Link) Params() (jsonvalue.Value, bool) { return link.params.get() }

// Server returns the optional target server.
func (link Link) Server() (Server, bool) { return link.server, link.hasServer }

// LinkOrReference is a Link Object or Reference Object union.
type LinkOrReference struct {
	link      Link
	reference Reference
	kind      uint8
}

// LinkValue constructs the Link Object union case.
func LinkValue(value Link) LinkOrReference { return LinkOrReference{link: value, kind: 1} }

// LinkReference constructs the Reference Object union case.
func LinkReference(value Reference) LinkOrReference {
	return LinkOrReference{reference: value, kind: 2}
}

// Link returns the Link Object case and true.
func (value LinkOrReference) Link() (Link, bool) { return value.link, value.kind == 1 }

// Reference returns the Reference Object case and true.
func (value LinkOrReference) Reference() (Reference, bool) {
	return value.reference, value.kind == 2
}

// ExampleInput supplies Example Object fields.
type ExampleInput struct {
	Name          string
	Summary       *string
	Description   *string
	Value         jsonvalue.Value
	Extensions    Fields
	UnknownFields Fields
}

// Example is an immutable OpenRPC Example Object.
type Example struct {
	objectFields
	name        string
	summary     optionalString
	description optionalString
	value       jsonvalue.Value
}

// NewExample constructs an Example Object. A JSON null Value remains a present
// required value.
func NewExample(input ExampleInput) (Example, error) {
	if input.Name == "" {
		return Example{}, missingField("name")
	}
	if len(input.Value.Bytes()) == 0 {
		return Example{}, missingField("value")
	}
	return Example{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		name:         input.Name,
		summary:      stringOption(input.Summary),
		description:  stringOption(input.Description),
		value:        input.Value,
	}, nil
}

// Name returns the required canonical example name.
func (example Example) Name() string { return example.name }

// Summary returns the optional summary.
func (example Example) Summary() (string, bool) { return example.summary.get() }

// Description returns the optional rich-text description.
func (example Example) Description() (string, bool) { return example.description.get() }

// Value returns the required arbitrary JSON value.
func (example Example) Value() jsonvalue.Value { return example.value }

// ExampleOrReference is an Example Object or Reference Object union.
type ExampleOrReference struct {
	example   Example
	reference Reference
	kind      uint8
}

// ExampleValue constructs the Example Object union case.
func ExampleValue(value Example) ExampleOrReference {
	return ExampleOrReference{example: value, kind: 1}
}

// ExampleReference constructs the Reference Object union case.
func ExampleReference(value Reference) ExampleOrReference {
	return ExampleOrReference{reference: value, kind: 2}
}

// Example returns the Example Object case and true.
func (value ExampleOrReference) Example() (Example, bool) {
	return value.example, value.kind == 1
}

// Reference returns the Reference Object case and true.
func (value ExampleOrReference) Reference() (Reference, bool) {
	return value.reference, value.kind == 2
}

// ExamplePairingInput supplies Example Pairing Object fields.
type ExamplePairingInput struct {
	Name          string
	Description   *string
	Params        []ExampleOrReference
	Result        *ExampleOrReference
	UnknownFields Fields
}

// ExamplePairing is an immutable OpenRPC Example Pairing Object.
type ExamplePairing struct {
	name        string
	description optionalString
	params      []ExampleOrReference
	result      ExampleOrReference
	hasResult   bool
	unknown     Fields
}

// NewExamplePairing constructs an Example Pairing Object. An absent Result
// represents notification usage.
func NewExamplePairing(input ExamplePairingInput) (ExamplePairing, error) {
	if input.Name == "" {
		return ExamplePairing{}, missingField("name")
	}
	if input.Params == nil {
		return ExamplePairing{}, missingField("params")
	}
	pairing := ExamplePairing{
		name:        input.Name,
		description: stringOption(input.Description),
		params:      append([]ExampleOrReference(nil), input.Params...),
		unknown:     input.UnknownFields,
	}
	if input.Result != nil {
		pairing.result = *input.Result
		pairing.hasResult = true
	}
	return pairing, nil
}

// Name returns the required pairing name.
func (pairing ExamplePairing) Name() string { return pairing.name }

// Description returns the optional pairing description.
func (pairing ExamplePairing) Description() (string, bool) {
	return pairing.description.get()
}

// Params returns an owned parameter example slice.
func (pairing ExamplePairing) Params() []ExampleOrReference {
	return append([]ExampleOrReference(nil), pairing.params...)
}

// Result returns the optional result example. Absence denotes a notification.
func (pairing ExamplePairing) Result() (ExampleOrReference, bool) {
	return pairing.result, pairing.hasResult
}

// UnknownFields returns fields retained by preserving parse mode.
func (pairing ExamplePairing) UnknownFields() Fields { return pairing.unknown }

// ExamplePairingOrReference is an Example Pairing or Reference Object union.
type ExamplePairingOrReference struct {
	pairing   ExamplePairing
	reference Reference
	kind      uint8
}

// ExamplePairingValue constructs the Example Pairing union case.
func ExamplePairingValue(value ExamplePairing) ExamplePairingOrReference {
	return ExamplePairingOrReference{pairing: value, kind: 1}
}

// ExamplePairingReference constructs the Reference Object union case.
func ExamplePairingReference(value Reference) ExamplePairingOrReference {
	return ExamplePairingOrReference{reference: value, kind: 2}
}

// ExamplePairing returns the pairing case and true.
func (value ExamplePairingOrReference) ExamplePairing() (ExamplePairing, bool) {
	return value.pairing, value.kind == 1
}

// Reference returns the Reference Object case and true.
func (value ExamplePairingOrReference) Reference() (Reference, bool) {
	return value.reference, value.kind == 2
}

// ParamStructure is the method parameter assignment structure.
type ParamStructure string

const (
	// ParamStructureByName assigns parameters by Content Descriptor name.
	ParamStructureByName ParamStructure = "by-name"
	// ParamStructureByPosition assigns parameters by array position.
	ParamStructureByPosition ParamStructure = "by-position"
	// ParamStructureEither permits either assignment structure and is default.
	ParamStructureEither ParamStructure = "either"
)

func validParamStructure(value ParamStructure) bool {
	return value == ParamStructureByName ||
		value == ParamStructureByPosition ||
		value == ParamStructureEither
}

// MethodInput supplies Method Object fields. Params must be non-nil; an empty
// slice represents a method without parameters.
type MethodInput struct {
	Name           string
	Description    *string
	Summary        *string
	Servers        []Server
	HasServers     bool
	Tags           []TagOrReference
	HasTags        bool
	Params         []ContentDescriptorOrReference
	ParamStructure *ParamStructure
	Result         *ContentDescriptorOrReference
	Errors         []ErrorOrReference
	HasErrors      bool
	Links          []LinkOrReference
	HasLinks       bool
	Examples       []ExamplePairingOrReference
	HasExamples    bool
	Deprecated     *bool
	ExternalDocs   *ExternalDocumentation
	Extensions     Fields
	UnknownFields  Fields
}

// Method is an immutable OpenRPC Method Object.
type Method struct {
	objectFields
	name           string
	description    optionalString
	summary        optionalString
	servers        []Server
	hasServers     bool
	tags           []TagOrReference
	hasTags        bool
	params         []ContentDescriptorOrReference
	paramStructure ParamStructure
	hasStructure   bool
	result         ContentDescriptorOrReference
	hasResult      bool
	errors         []ErrorOrReference
	hasErrors      bool
	links          []LinkOrReference
	hasLinks       bool
	examples       []ExamplePairingOrReference
	hasExamples    bool
	deprecated     optionalBool
	externalDocs   ExternalDocumentation
	hasDocs        bool
}

// NewMethod constructs a Method Object and owns every supplied collection.
func NewMethod(input MethodInput) (Method, error) {
	if input.Name == "" {
		return Method{}, missingField("name")
	}
	if input.Params == nil {
		return Method{}, missingField("params")
	}
	method := Method{
		objectFields:   newObjectFields(input.Extensions, input.UnknownFields),
		name:           input.Name,
		description:    stringOption(input.Description),
		summary:        stringOption(input.Summary),
		servers:        append([]Server(nil), input.Servers...),
		hasServers:     input.HasServers || input.Servers != nil,
		tags:           append([]TagOrReference(nil), input.Tags...),
		hasTags:        input.HasTags || input.Tags != nil,
		params:         append([]ContentDescriptorOrReference(nil), input.Params...),
		paramStructure: ParamStructureEither,
		errors:         append([]ErrorOrReference(nil), input.Errors...),
		hasErrors:      input.HasErrors || input.Errors != nil,
		links:          append([]LinkOrReference(nil), input.Links...),
		hasLinks:       input.HasLinks || input.Links != nil,
		examples:       append([]ExamplePairingOrReference(nil), input.Examples...),
		hasExamples:    input.HasExamples || input.Examples != nil,
		deprecated:     boolOption(input.Deprecated),
	}
	if input.ParamStructure != nil {
		if !validParamStructure(*input.ParamStructure) {
			return Method{}, ErrInvalidParamStructure
		}
		method.paramStructure = *input.ParamStructure
		method.hasStructure = true
	}
	if input.Result != nil {
		method.result = *input.Result
		method.hasResult = true
	}
	if input.ExternalDocs != nil {
		method.externalDocs = *input.ExternalDocs
		method.hasDocs = true
	}
	return method, nil
}

// Name returns the required canonical method name.
func (method Method) Name() string { return method.name }

// Description returns the optional rich-text description.
func (method Method) Description() (string, bool) { return method.description.get() }

// Summary returns the optional summary.
func (method Method) Summary() (string, bool) { return method.summary.get() }

// Servers returns an owned optional server slice.
func (method Method) Servers() ([]Server, bool) {
	return append([]Server(nil), method.servers...), method.hasServers
}

// Tags returns an owned optional tag slice.
func (method Method) Tags() ([]TagOrReference, bool) {
	return append([]TagOrReference(nil), method.tags...), method.hasTags
}

// Params returns an owned required parameter slice.
func (method Method) Params() []ContentDescriptorOrReference {
	return append([]ContentDescriptorOrReference(nil), method.params...)
}

// ParamStructure returns the effective value and whether it was explicit.
func (method Method) ParamStructure() (ParamStructure, bool) {
	return method.paramStructure, method.hasStructure
}

// Result returns the optional result. Absence makes the method notification
// only according to the specification.
func (method Method) Result() (ContentDescriptorOrReference, bool) {
	return method.result, method.hasResult
}

// Errors returns an owned optional error slice.
func (method Method) Errors() ([]ErrorOrReference, bool) {
	return append([]ErrorOrReference(nil), method.errors...), method.hasErrors
}

// Links returns an owned optional link slice.
func (method Method) Links() ([]LinkOrReference, bool) {
	return append([]LinkOrReference(nil), method.links...), method.hasLinks
}

// Examples returns an owned optional example-pairing slice.
func (method Method) Examples() ([]ExamplePairingOrReference, bool) {
	return append([]ExamplePairingOrReference(nil), method.examples...), method.hasExamples
}

// Deprecated returns the declared value and whether it was present.
func (method Method) Deprecated() (bool, bool) { return method.deprecated.get() }

// DeprecatedOrDefault returns the effective value, whose default is false.
func (method Method) DeprecatedOrDefault() bool { return method.deprecated.value }

// ExternalDocs returns optional external documentation.
func (method Method) ExternalDocs() (ExternalDocumentation, bool) {
	return method.externalDocs, method.hasDocs
}

// MethodOrReference is a Method Object or Reference Object union.
type MethodOrReference struct {
	method    Method
	reference Reference
	kind      uint8
}

// MethodValue constructs the Method Object union case.
func MethodValue(value Method) MethodOrReference {
	return MethodOrReference{method: value, kind: 1}
}

// MethodReference constructs the Reference Object union case.
func MethodReference(value Reference) MethodOrReference {
	return MethodOrReference{reference: value, kind: 2}
}

// Method returns the Method Object case and true.
func (value MethodOrReference) Method() (Method, bool) {
	return value.method, value.kind == 1
}

// Reference returns the Reference Object case and true.
func (value MethodOrReference) Reference() (Reference, bool) {
	return value.reference, value.kind == 2
}
