// Package builder constructs validated WSDL documents without XML parsing.
package builder

import (
	"errors"
	"fmt"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

var ErrDuplicateComponent = errors.New("wsdl builder: duplicate component")

// Description20 constructs one WSDL 2.0 description.
type Description20 struct {
	value      wsdl.Description20
	interfaces map[string]struct{}
	bindings   map[string]struct{}
	services   map[string]struct{}
}

// New20 starts a WSDL 2.0 description for targetNamespace.
func New20(targetNamespace string) *Description20 {
	return &Description20{
		value:      wsdl.Description20{TargetNamespace: targetNamespace},
		interfaces: make(map[string]struct{}),
		bindings:   make(map[string]struct{}),
		services:   make(map[string]struct{}),
	}
}

// SetDocumentation replaces the root documentation value.
func (b *Description20) SetDocumentation(value wsdl.Documentation) error {
	if b == nil {
		return errors.New("wsdl builder: description is nil")
	}
	b.value.Documentation = &value
	return nil
}

// SetTypes replaces the root XML Schema declarations.
func (b *Description20) SetTypes(value wsdl.Types20) error {
	if b == nil {
		return errors.New("wsdl builder: description is nil")
	}
	b.value.Types = &value
	return nil
}

// AddImport appends a WSDL 2.0 import.
func (b *Description20) AddImport(value wsdl.Import20) error {
	if b == nil {
		return errors.New("wsdl builder: description is nil")
	}
	b.value.Imports = append(b.value.Imports, value)
	return nil
}

// AddInclude appends a WSDL 2.0 include.
func (b *Description20) AddInclude(value wsdl.Include20) error {
	if b == nil {
		return errors.New("wsdl builder: description is nil")
	}
	b.value.Includes = append(b.value.Includes, value)
	return nil
}

// AddInterface adds one uniquely named interface.
func (b *Description20) AddInterface(value wsdl.Interface20) error {
	if b == nil {
		return errors.New("wsdl builder: description is nil")
	}
	if err := addName(b.interfaces, "interface", value.Name); err != nil {
		return err
	}
	b.value.Interfaces = append(b.value.Interfaces, value)
	return nil
}

// AddBinding adds one uniquely named binding.
func (b *Description20) AddBinding(value wsdl.Binding20) error {
	if b == nil {
		return errors.New("wsdl builder: description is nil")
	}
	if err := addName(b.bindings, "binding", value.Name); err != nil {
		return err
	}
	b.value.Bindings = append(b.value.Bindings, value)
	return nil
}

// AddService adds one uniquely named service.
func (b *Description20) AddService(value wsdl.Service20) error {
	if b == nil {
		return errors.New("wsdl builder: description is nil")
	}
	if err := addName(b.services, "service", value.Name); err != nil {
		return err
	}
	b.value.Services = append(b.value.Services, value)
	return nil
}

// Build canonicalizes and validates the complete description.
func (b *Description20) Build(options wsdl.ValidationOptions) (*wsdl.Document, error) {
	if b == nil {
		return nil, errors.New("wsdl builder: description is nil")
	}
	return wsdl.NewDocument20(b.value, options)
}

// Definitions11 constructs one WSDL 1.1 definitions document.
type Definitions11 struct {
	value     wsdl.Definitions11
	messages  map[string]struct{}
	portTypes map[string]struct{}
	bindings  map[string]struct{}
	services  map[string]struct{}
}

// New11 starts a WSDL 1.1 definitions document.
func New11(name, targetNamespace string) *Definitions11 {
	return &Definitions11{
		value:    wsdl.Definitions11{Name: name, TargetNamespace: targetNamespace},
		messages: make(map[string]struct{}), portTypes: make(map[string]struct{}),
		bindings: make(map[string]struct{}), services: make(map[string]struct{}),
	}
}

// SetTypes replaces the root XML Schema declarations.
func (b *Definitions11) SetTypes(value wsdl.Types11) error {
	if b == nil {
		return errors.New("wsdl builder: definitions is nil")
	}
	b.value.Types = &value
	return nil
}

// AddImport appends a WSDL 1.1 import.
func (b *Definitions11) AddImport(value wsdl.Import11) error {
	if b == nil {
		return errors.New("wsdl builder: definitions is nil")
	}
	b.value.Imports = append(b.value.Imports, value)
	return nil
}

// AddMessage adds one uniquely named message.
func (b *Definitions11) AddMessage(value wsdl.Message11) error {
	if b == nil {
		return errors.New("wsdl builder: definitions is nil")
	}
	if err := addName(b.messages, "message", value.Name); err != nil {
		return err
	}
	b.value.Messages = append(b.value.Messages, value)
	return nil
}

// AddPortType adds one uniquely named port type.
func (b *Definitions11) AddPortType(value wsdl.PortType11) error {
	if b == nil {
		return errors.New("wsdl builder: definitions is nil")
	}
	if err := addName(b.portTypes, "port type", value.Name); err != nil {
		return err
	}
	b.value.PortTypes = append(b.value.PortTypes, value)
	return nil
}

// AddBinding adds one uniquely named binding.
func (b *Definitions11) AddBinding(value wsdl.Binding11) error {
	if b == nil {
		return errors.New("wsdl builder: definitions is nil")
	}
	if err := addName(b.bindings, "binding", value.Name); err != nil {
		return err
	}
	b.value.Bindings = append(b.value.Bindings, value)
	return nil
}

// AddService adds one uniquely named service.
func (b *Definitions11) AddService(value wsdl.Service11) error {
	if b == nil {
		return errors.New("wsdl builder: definitions is nil")
	}
	if err := addName(b.services, "service", value.Name); err != nil {
		return err
	}
	b.value.Services = append(b.value.Services, value)
	return nil
}

// Build canonicalizes and validates the complete definitions document.
func (b *Definitions11) Build(options wsdl.ValidationOptions) (*wsdl.Document, error) {
	if b == nil {
		return nil, errors.New("wsdl builder: definitions is nil")
	}
	return wsdl.NewDocument11(b.value, options)
}

func addName(names map[string]struct{}, kind, name string) error {
	if names == nil {
		return errors.New("wsdl builder: builder was not initialized")
	}
	if _, exists := names[name]; exists {
		return fmt.Errorf("%w: %s %q", ErrDuplicateComponent, kind, name)
	}
	names[name] = struct{}{}
	return nil
}
