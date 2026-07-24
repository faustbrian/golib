// Package codegen builds a bounded, transport-neutral generation model from
// an immutable compiled WSDL graph. It does not generate source or perform I/O.
package codegen

import (
	"errors"
	"fmt"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
)

// ErrLimitExceeded identifies a generation model beyond configured bounds.
var ErrLimitExceeded = errors.New("wsdl codegen: resource limit exceeded")

const defaultModelLimit = 100000

// Limits bounds materialized generation-model collections.
type Limits struct {
	MaxInterfaces int
	MaxOperations int
	MaxParts      int
	MaxFaults     int
	MaxBindings   int
	MaxServices   int
	MaxEndpoints  int
	MaxTypes      int
	MaxElements   int
}

// Options configures generation-model construction.
type Options struct {
	Limits Limits
}

// Part is one generated message field candidate.
type Part struct {
	Name    string
	Element wsdl.QName
	Type    wsdl.QName
}

// Message is one generated operation payload candidate.
type Message struct {
	Label        string
	Name         wsdl.QName
	Element      wsdl.QName
	ContentModel wsdl.MessageContentModel
	Parts        []Part
}

// Fault is one generated operation error candidate.
type Fault struct {
	Name         wsdl.QName
	Label        string
	Direction    string
	Message      wsdl.QName
	Element      wsdl.QName
	ContentModel wsdl.MessageContentModel
}

// Operation is one generated client or server method candidate.
type Operation struct {
	Name            string
	Pattern         string
	Style           string
	Styles          []string
	Safe            bool
	RPCSignature    []wsdl.RPCSignatureParameter20
	RPCSignatureSet bool
	Inputs          []Message
	Outputs         []Message
	Input           *Message
	Output          *Message
	Faults          []Fault
}

// Interface groups generated operation candidates.
type Interface struct {
	Name       wsdl.QName
	Extends    []wsdl.QName
	Operations []Operation
}

// Binding preserves the protocol association used by generated adapters.
type Binding struct {
	Name                wsdl.QName
	Interface           wsdl.QName
	Type                string
	OperationReferences []wsdlcompile.OperationReference
}

// Endpoint preserves generated client endpoint metadata without transport behavior.
type Endpoint struct {
	Name    string
	Binding wsdl.QName
	Address string
}

// Service groups generated endpoint candidates.
type Service struct {
	Name      wsdl.QName
	Interface wsdl.QName
	Endpoints []Endpoint
}

// Model is an owned deterministic input for optional source generators.
type Model struct {
	Interfaces []Interface
	Bindings   []Binding
	Services   []Service
	Types      []wsdl.QName
	Elements   []wsdl.QName
}

// Build creates an owned, bounded generation model without performing I/O.
func Build(set *wsdlcompile.Set, options Options) (*Model, error) {
	if set == nil {
		return nil, errors.New("wsdl codegen: compiled set is nil")
	}
	limits, err := normalizeLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	model := &Model{}
	interfaces := set.Interfaces()
	if err := within("interfaces", len(interfaces), limits.MaxInterfaces); err != nil {
		return nil, err
	}
	operations, parts, faults := 0, 0, 0
	for _, value := range interfaces {
		item := Interface{
			Name: value.Name, Extends: append([]wsdl.QName(nil), value.Extends...),
		}
		for _, operation := range value.Operations {
			operations++
			for _, message := range operation.Inputs {
				parts += len(message.Parts)
			}
			for _, message := range operation.Outputs {
				parts += len(message.Parts)
			}
			faults += len(operation.Faults)
			item.Operations = append(item.Operations, copyOperation(operation))
		}
		model.Interfaces = append(model.Interfaces, item)
	}
	counts := []struct {
		name           string
		count, maximum int
	}{
		{name: "operations", count: operations, maximum: limits.MaxOperations},
		{name: "parts", count: parts, maximum: limits.MaxParts},
		{name: "faults", count: faults, maximum: limits.MaxFaults},
	}
	for _, value := range counts {
		if err := within(value.name, value.count, value.maximum); err != nil {
			return nil, err
		}
	}
	bindings := set.Bindings()
	if err := within("bindings", len(bindings), limits.MaxBindings); err != nil {
		return nil, err
	}
	for _, value := range bindings {
		model.Bindings = append(model.Bindings, Binding{
			Name: value.Name, Interface: value.Interface, Type: value.Type,
			OperationReferences: append(
				[]wsdlcompile.OperationReference(nil), value.OperationReferences...,
			),
		})
	}
	services := set.Services()
	if err := within("services", len(services), limits.MaxServices); err != nil {
		return nil, err
	}
	endpointCount := 0
	for _, value := range services {
		service := Service{Name: value.Name, Interface: value.Interface}
		for _, endpoint := range value.Endpoints {
			endpointCount++
			service.Endpoints = append(service.Endpoints, Endpoint{
				Name: endpoint.Name, Binding: endpoint.Binding, Address: endpoint.Address,
			})
		}
		model.Services = append(model.Services, service)
	}
	if err := within("endpoints", endpointCount, limits.MaxEndpoints); err != nil {
		return nil, err
	}
	if schemas := set.Schemas(); schemas != nil {
		for _, value := range append(schemas.SimpleTypeNames(), schemas.ComplexTypeNames()...) {
			model.Types = append(model.Types, wsdl.QName{Namespace: value.Namespace, Local: value.Local})
		}
		for _, value := range schemas.ElementNames() {
			model.Elements = append(model.Elements, wsdl.QName{Namespace: value.Namespace, Local: value.Local})
		}
	}
	if err := within("types", len(model.Types), limits.MaxTypes); err != nil {
		return nil, err
	}
	if err := within("elements", len(model.Elements), limits.MaxElements); err != nil {
		return nil, err
	}
	return model, nil
}

func normalizeLimits(value Limits) (Limits, error) {
	values := []*int{
		&value.MaxInterfaces, &value.MaxOperations, &value.MaxParts,
		&value.MaxFaults, &value.MaxBindings, &value.MaxServices,
		&value.MaxEndpoints, &value.MaxTypes, &value.MaxElements,
	}
	for _, limit := range values {
		if *limit < 0 {
			return Limits{}, errors.New("wsdl codegen: limits must not be negative")
		}
		if *limit == 0 {
			*limit = defaultModelLimit
		}
	}
	return value, nil
}

func within(name string, count, maximum int) error {
	if count > maximum {
		return fmt.Errorf("%w: %s exceed %d", ErrLimitExceeded, name, maximum)
	}
	return nil
}

func copyOperation(value wsdlcompile.Operation) Operation {
	result := Operation{
		Name: value.Name, Pattern: value.Pattern, Style: value.Style,
		Styles: append([]string(nil), value.Styles...), Safe: value.Safe,
		RPCSignature:    append([]wsdl.RPCSignatureParameter20(nil), value.RPCSignature...),
		RPCSignatureSet: value.RPCSignatureSet,
		Input:           copyMessage(value.Input), Output: copyMessage(value.Output),
	}
	for index := range value.Inputs {
		result.Inputs = append(result.Inputs, *copyMessage(&value.Inputs[index]))
	}
	for index := range value.Outputs {
		result.Outputs = append(result.Outputs, *copyMessage(&value.Outputs[index]))
	}
	for _, fault := range value.Faults {
		result.Faults = append(result.Faults, Fault{
			Name: fault.Name, Label: fault.Label, Direction: fault.Direction,
			Message: fault.Message, Element: fault.Element,
			ContentModel: fault.ContentModel,
		})
	}
	return result
}

func copyMessage(value *wsdlcompile.Message) *Message {
	if value == nil {
		return nil
	}
	result := &Message{
		Label: value.Label, Name: value.Name, Element: value.Element,
		ContentModel: value.ContentModel,
	}
	for _, part := range value.Parts {
		result.Parts = append(result.Parts, Part{
			Name: part.Name, Element: part.Element, Type: part.Type,
		})
	}
	return result
}
