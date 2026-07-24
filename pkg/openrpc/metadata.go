package openrpc

import (
	"errors"
	"fmt"
)

// ErrMissingRequiredField reports a required field absent from constructor
// input.
var ErrMissingRequiredField = errors.New("openrpc: missing required field")

// MissingRequiredFieldError identifies one absent required field without
// including document values.
type MissingRequiredFieldError struct {
	Field string
}

// Error implements error.
func (err *MissingRequiredFieldError) Error() string {
	return fmt.Sprintf("openrpc: missing required field %q", err.Field)
}

// Unwrap supports errors.Is with ErrMissingRequiredField.
func (err *MissingRequiredFieldError) Unwrap() error {
	return ErrMissingRequiredField
}

type optionalString struct {
	value   string
	present bool
}

func stringOption(value *string) optionalString {
	if value == nil {
		return optionalString{}
	}
	return optionalString{value: *value, present: true}
}

func (value optionalString) get() (string, bool) {
	return value.value, value.present
}

type objectFields struct {
	extensions Fields
	unknown    Fields
}

func newObjectFields(extensions Fields, unknown Fields) objectFields {
	return objectFields{extensions: extensions, unknown: unknown}
}

// Extensions returns immutable specification extension fields.
func (fields objectFields) Extensions() Fields {
	return fields.extensions
}

// UnknownFields returns immutable fields retained by preserving parse mode.
func (fields objectFields) UnknownFields() Fields {
	return fields.unknown
}

// ContactInput supplies optional Contact Object fields.
type ContactInput struct {
	Name          *string
	Email         *string
	URL           *string
	Extensions    Fields
	UnknownFields Fields
}

// Contact is an immutable OpenRPC Contact Object.
type Contact struct {
	objectFields
	name  optionalString
	email optionalString
	url   optionalString
}

// NewContact constructs a Contact Object with owned optional values.
func NewContact(input ContactInput) (Contact, error) {
	return Contact{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		name:         stringOption(input.Name),
		email:        stringOption(input.Email),
		url:          stringOption(input.URL),
	}, nil
}

// Name returns the optional identifying name.
func (contact Contact) Name() (string, bool) { return contact.name.get() }

// Email returns the optional contact email address.
func (contact Contact) Email() (string, bool) { return contact.email.get() }

// URL returns the optional contact URL.
func (contact Contact) URL() (string, bool) { return contact.url.get() }

// LicenseInput supplies optional License Object fields.
type LicenseInput struct {
	Name          *string
	URL           *string
	Extensions    Fields
	UnknownFields Fields
}

// License is an immutable OpenRPC License Object.
type License struct {
	objectFields
	name optionalString
	url  optionalString
}

// NewLicense constructs a License Object.
func NewLicense(input LicenseInput) (License, error) {
	return License{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		name:         stringOption(input.Name),
		url:          stringOption(input.URL),
	}, nil
}

// Name returns the optional license name.
func (license License) Name() (string, bool) { return license.name.get() }

// URL returns the optional license URL.
func (license License) URL() (string, bool) { return license.url.get() }

// InfoInput supplies Info Object fields. Title and Version must be non-empty.
type InfoInput struct {
	Title          string
	Version        string
	Description    *string
	TermsOfService *string
	Contact        *Contact
	License        *License
	Extensions     Fields
	UnknownFields  Fields
}

// Info is an immutable OpenRPC Info Object.
type Info struct {
	objectFields
	title          string
	version        string
	description    optionalString
	termsOfService optionalString
	contact        Contact
	hasContact     bool
	license        License
	hasLicense     bool
}

// NewInfo constructs an Info Object.
func NewInfo(input InfoInput) (Info, error) {
	if input.Title == "" {
		return Info{}, missingField("title")
	}
	if input.Version == "" {
		return Info{}, missingField("version")
	}
	info := Info{
		objectFields:   newObjectFields(input.Extensions, input.UnknownFields),
		title:          input.Title,
		version:        input.Version,
		description:    stringOption(input.Description),
		termsOfService: stringOption(input.TermsOfService),
	}
	if input.Contact != nil {
		info.contact = *input.Contact
		info.hasContact = true
	}
	if input.License != nil {
		info.license = *input.License
		info.hasLicense = true
	}
	return info, nil
}

// Title returns the required application title.
func (info Info) Title() string { return info.title }

// Version returns the required API document version.
func (info Info) Version() string { return info.version }

// Description returns the optional rich-text description.
func (info Info) Description() (string, bool) { return info.description.get() }

// TermsOfService returns the optional terms URL.
func (info Info) TermsOfService() (string, bool) { return info.termsOfService.get() }

// Contact returns the optional Contact Object.
func (info Info) Contact() (Contact, bool) { return info.contact, info.hasContact }

// License returns the optional License Object.
func (info Info) License() (License, bool) { return info.license, info.hasLicense }

// ExternalDocumentationInput supplies External Documentation Object fields.
type ExternalDocumentationInput struct {
	URL           string
	Description   *string
	Extensions    Fields
	UnknownFields Fields
}

// ExternalDocumentation is an immutable External Documentation Object.
type ExternalDocumentation struct {
	objectFields
	url         string
	description optionalString
}

// NewExternalDocumentation constructs an External Documentation Object.
func NewExternalDocumentation(input ExternalDocumentationInput) (ExternalDocumentation, error) {
	if input.URL == "" {
		return ExternalDocumentation{}, missingField("url")
	}
	return ExternalDocumentation{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		url:          input.URL,
		description:  stringOption(input.Description),
	}, nil
}

// URL returns the required target URL.
func (documentation ExternalDocumentation) URL() string { return documentation.url }

// Description returns the optional rich-text description.
func (documentation ExternalDocumentation) Description() (string, bool) {
	return documentation.description.get()
}

// ServerVariableInput supplies Server Variable Object fields. Default uses a
// pointer because the required value may legally be an empty string.
type ServerVariableInput struct {
	Default       *string
	Description   *string
	Enum          []string
	HasEnum       bool
	UnknownFields Fields
}

// ServerVariable is an immutable OpenRPC Server Variable Object.
type ServerVariable struct {
	defaultValue string
	description  optionalString
	enum         []string
	hasEnum      bool
	unknown      Fields
}

// NewServerVariable constructs a Server Variable Object.
func NewServerVariable(input ServerVariableInput) (ServerVariable, error) {
	if input.Default == nil {
		return ServerVariable{}, missingField("default")
	}
	return ServerVariable{
		defaultValue: *input.Default,
		description:  stringOption(input.Description),
		enum:         append([]string(nil), input.Enum...),
		hasEnum:      input.HasEnum || input.Enum != nil,
		unknown:      input.UnknownFields,
	}, nil
}

// Default returns the required substitution default.
func (variable ServerVariable) Default() string { return variable.defaultValue }

// Description returns the optional rich-text description.
func (variable ServerVariable) Description() (string, bool) {
	return variable.description.get()
}

// Enum returns an owned copy of the optional allowed values.
func (variable ServerVariable) Enum() ([]string, bool) {
	return append([]string(nil), variable.enum...), variable.hasEnum
}

// UnknownFields returns fields retained by preserving parse mode.
func (variable ServerVariable) UnknownFields() Fields { return variable.unknown }

// ServerInput supplies Server Object fields.
type ServerInput struct {
	URL           string
	Name          *string
	Description   *string
	Summary       *string
	Variables     map[string]ServerVariable
	HasVariables  bool
	Extensions    Fields
	UnknownFields Fields
}

// Server is an immutable OpenRPC Server Object.
type Server struct {
	objectFields
	url          string
	name         optionalString
	description  optionalString
	summary      optionalString
	variables    map[string]ServerVariable
	hasVariables bool
}

// NewServer constructs a Server Object and owns its variables map.
func NewServer(input ServerInput) (Server, error) {
	if input.URL == "" {
		return Server{}, missingField("url")
	}
	variables := make(map[string]ServerVariable, len(input.Variables))
	for name, variable := range input.Variables {
		variables[name] = variable
	}
	return Server{
		objectFields: newObjectFields(input.Extensions, input.UnknownFields),
		url:          input.URL,
		name:         stringOption(input.Name),
		description:  stringOption(input.Description),
		summary:      stringOption(input.Summary),
		variables:    variables,
		hasVariables: input.HasVariables || input.Variables != nil,
	}, nil
}

// URL returns the required server URL template.
func (server Server) URL() string { return server.url }

// Name returns the optional server name.
func (server Server) Name() (string, bool) { return server.name.get() }

// Description returns the optional rich-text server description.
func (server Server) Description() (string, bool) { return server.description.get() }

// Summary returns the optional short server summary.
func (server Server) Summary() (string, bool) { return server.summary.get() }

// Variables returns an owned map of optional server variables.
func (server Server) Variables() (map[string]ServerVariable, bool) {
	variables := make(map[string]ServerVariable, len(server.variables))
	for name, variable := range server.variables {
		variables[name] = variable
	}
	return variables, server.hasVariables
}

func missingField(field string) error {
	return &MissingRequiredFieldError{Field: field}
}
