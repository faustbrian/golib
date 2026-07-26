// Package wsdl parses, validates, compiles, and emits WSDL descriptions.
package wsdl

import (
	"context"
	"fmt"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

// Version identifies a supported WSDL language version.
type Version string

const (
	// Version11 identifies WSDL 1.1 documents.
	Version11 Version = "1.1"
	// Version20 identifies WSDL 2.0 documents.
	Version20 Version = "2.0"
)

// Definitions11 is the root of a WSDL 1.1 description.
type Definitions11 struct {
	Name                string
	TargetNamespace     string
	Documentation       *Documentation
	Imports             []Import11
	Types               *Types11
	Messages            []Message11
	PortTypes           []PortType11
	Bindings            []Binding11
	Services            []Service11
	Extensions          []Extension
	ExtensionAttributes []ExtensionAttribute
	Location            Location
}

// Types11 contains the XML Schema documents embedded in WSDL 1.1 types.
// XML Schema parsing and compilation remain owned by xsd.
type Types11 struct {
	Extensibility
	Schemas  []*xsd.Document
	Location Location
}

// Description20 is the root of a WSDL 2.0 description.
type Description20 struct {
	Extensibility
	TargetNamespace string
	Documentation   *Documentation
	Imports         []Import20
	Includes        []Include20
	Types           *Types20
	Interfaces      []Interface20
	Bindings        []Binding20
	Services        []Service20
	Location        Location
}

// Types20 contains the XML Schema documents embedded in WSDL 2.0 types.
// XML Schema parsing and compilation remain owned by xsd.
type Types20 struct {
	Extensibility
	Imports  []xsd.SchemaReference
	Schemas  []*xsd.Document
	Location Location
}

// Document contains exactly one version-specific WSDL document model.
type Document struct {
	version       Version
	definitions11 *Definitions11
	description20 *Description20
}

var (
	canonicalMarshal  = Marshal
	canonicalParse    = Parse
	canonicalValidate = Validate
)

// NewDocument11 canonicalizes and validates a caller-owned WSDL 1.1 model.
func NewDocument11(
	value Definitions11,
	validation ValidationOptions,
) (*Document, error) {
	return canonicalDocument(
		&Document{version: Version11, definitions11: &value},
		validation,
	)
}

// NewDocument20 canonicalizes and validates a caller-owned WSDL 2.0 model.
func NewDocument20(
	value Description20,
	validation ValidationOptions,
) (*Document, error) {
	return canonicalDocument(
		&Document{version: Version20, description20: &value},
		validation,
	)
}

func canonicalDocument(
	document *Document,
	validation ValidationOptions,
) (*Document, error) {
	diagnostics := canonicalValidate(document, validation)
	if err := diagnostics.Err(); err != nil {
		return nil, fmt.Errorf("wsdl: validate model: %w", err)
	}
	payload, err := canonicalMarshal(document, MarshalOptions{})
	if err != nil {
		return nil, fmt.Errorf("wsdl: canonicalize model: %w", err)
	}
	canonical, err := canonicalParse(context.Background(), payload, ParseOptions{})
	if err != nil {
		return nil, fmt.Errorf("wsdl: canonicalize model: %w", err)
	}
	diagnostics = canonicalValidate(canonical, validation)
	if err := diagnostics.Err(); err != nil {
		return nil, fmt.Errorf("wsdl: validate model: %w", err)
	}
	return canonical, nil
}

// Description20 returns the WSDL 2.0 model when this is a WSDL 2.0 document.
func (d *Document) Description20() (Description20, bool) {
	if d == nil || d.description20 == nil {
		return Description20{}, false
	}
	return *d.description20, true
}

// Version reports the parsed WSDL version.
func (d *Document) Version() Version {
	if d == nil {
		return ""
	}
	return d.version
}

// Definitions11 returns the WSDL 1.1 model when this is a WSDL 1.1 document.
func (d *Document) Definitions11() (Definitions11, bool) {
	if d == nil || d.definitions11 == nil {
		return Definitions11{}, false
	}
	return *d.definitions11, true
}
