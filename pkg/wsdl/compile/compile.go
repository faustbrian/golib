// Package compile resolves and compiles bounded WSDL document graphs.
package compile

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	"github.com/faustbrian/golib/pkg/wsdl/resolve"
	xsd "github.com/faustbrian/golib/pkg/xsd"
	xsdcompile "github.com/faustbrian/golib/pkg/xsd/compile"
	xsdresolve "github.com/faustbrian/golib/pkg/xsd/resolve"
)

var escapeSchemaText = xml.EscapeText

var (
	// ErrLimitExceeded identifies graph work beyond a configured bound.
	ErrLimitExceeded = errors.New("wsdl compile: resource limit exceeded")
	// ErrNamespace identifies an import or include namespace mismatch.
	ErrNamespace = errors.New("wsdl compile: namespace mismatch")
	// ErrVersion identifies a WSDL version mismatch in a graph edge.
	ErrVersion = errors.New("wsdl compile: version mismatch")
	// ErrResourceIdentity identifies an invalid resolver response identity.
	ErrResourceIdentity = errors.New("wsdl compile: resource identity mismatch")
	// ErrDuplicateComponent identifies a repeated graph component name.
	ErrDuplicateComponent = errors.New("wsdl compile: duplicate component")
	// ErrUnresolvedComponent identifies a broken compiled graph reference.
	ErrUnresolvedComponent = errors.New("wsdl compile: unresolved component")
	// ErrInvalidRPCStyle identifies a WSDL 2.0 RPC operation whose schemas or
	// signature violate the RPC style constraints.
	ErrInvalidRPCStyle = errors.New("wsdl compile: invalid RPC style")
	// ErrInvalidIRIStyle identifies a WSDL 2.0 IRI operation whose schema
	// violates the IRI style constraints.
	ErrInvalidIRIStyle = errors.New("wsdl compile: invalid IRI style")
	// ErrInvalidMultipartStyle identifies a WSDL 2.0 multipart operation whose
	// schema violates the multipart style constraints.
	ErrInvalidMultipartStyle = errors.New("wsdl compile: invalid multipart style")
)

const (
	defaultMaxDocuments  = 256
	defaultMaxDepth      = 64
	defaultMaxReferences = 4096
	defaultMaxBytes      = 64 << 20
	defaultMaxComponents = 100000
)

// Limits bounds graph resolution and component construction.
type Limits struct {
	MaxDocuments  int
	MaxDepth      int
	MaxReferences int
	MaxBytes      int64
	MaxComponents int
}

// Options configures an immutable Compiler. A nil Resolver denies every load.
type Options struct {
	Resolver       resolve.Resolver
	SchemaResolver xsdresolve.Resolver
	Limits         Limits
	SchemaLimits   xsdcompile.Limits
	Validation     wsdl.ValidationOptions
}

// Source is the caller-owned root WSDL resource.
type Source struct {
	URI     string
	Content []byte
}

// Compiler is immutable and safe for concurrent use.
type Compiler struct {
	resolver       resolve.Resolver
	schemaResolver xsdresolve.Resolver
	limits         Limits
	schemaLimits   xsdcompile.Limits
	validation     wsdl.ValidationOptions
}

// New validates options and creates a reusable compiler.
func New(options Options) (*Compiler, error) {
	limits := options.Limits
	if limits.MaxDocuments < 0 || limits.MaxDepth < 0 ||
		limits.MaxReferences < 0 || limits.MaxBytes < 0 ||
		limits.MaxComponents < 0 {
		return nil, errors.New("wsdl compile: limits must not be negative")
	}
	if limits.MaxDocuments == 0 {
		limits.MaxDocuments = defaultMaxDocuments
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxReferences == 0 {
		limits.MaxReferences = defaultMaxReferences
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	if limits.MaxComponents == 0 {
		limits.MaxComponents = defaultMaxComponents
	}
	resolver := options.Resolver
	if resolver == nil {
		resolver = resolve.Deny()
	}
	schemaResolver := options.SchemaResolver
	if schemaResolver == nil {
		schemaResolver = xsdresolve.Deny()
	}
	if _, err := xsdcompile.New(xsdcompile.Options{Limits: options.SchemaLimits}); err != nil {
		return nil, fmt.Errorf("wsdl compile: schema limits: %w", err)
	}
	return &Compiler{
		resolver: resolver, schemaResolver: schemaResolver,
		limits: limits, schemaLimits: options.SchemaLimits,
		validation: cloneValidationOptions(options.Validation),
	}, nil
}

func cloneValidationOptions(value wsdl.ValidationOptions) wsdl.ValidationOptions {
	value.UnderstoodExtensions = append([]wsdl.QName(nil), value.UnderstoodExtensions...)
	return value
}

// Document describes one resolved WSDL resource.
type Document struct {
	URI          string
	Namespace    string
	Version      wsdl.Version
	Dependencies []string
}

// Part is one immutable compiled WSDL 1.1 message part.
type Part struct {
	Name    string
	Element wsdl.QName
	Type    wsdl.QName
}

// Message is one immutable compiled operation message.
type Message struct {
	Label        string
	Name         wsdl.QName
	Element      wsdl.QName
	ContentModel wsdl.MessageContentModel
	Parts        []Part
}

// Fault is one immutable compiled operation fault reference.
type Fault struct {
	Name         wsdl.QName
	Label        string
	Direction    string
	Message      wsdl.QName
	Element      wsdl.QName
	ContentModel wsdl.MessageContentModel
}

// Operation is one immutable compiled interface operation.
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

// Interface is one immutable compiled abstract interface.
type Interface struct {
	Name       wsdl.QName
	Extends    []wsdl.QName
	Operations []Operation
	Faults     []wsdl.QName
}

// OperationReference identifies one bound operation, including WSDL 1.1
// overload-disambiguating input and output names.
type OperationReference struct {
	Name   string
	Input  string
	Output string
}

// Binding is one immutable compiled interface binding.
type Binding struct {
	Name                wsdl.QName
	Interface           wsdl.QName
	Type                string
	Operations          []string
	OperationReferences []OperationReference
}

// Endpoint is one immutable compiled service endpoint.
type Endpoint struct {
	Name    string
	Binding wsdl.QName
	Address string
}

// Service is one immutable compiled service.
type Service struct {
	Name      wsdl.QName
	Interface wsdl.QName
	Endpoints []Endpoint
}

// Set is an immutable, concurrency-safe WSDL component graph.
type Set struct {
	documents  []Document
	interfaces []Interface
	bindings   []Binding
	services   []Service
	schemas    *xsdcompile.Set
}

// Schemas returns the immutable compiled XML Schema component graph.
func (s *Set) Schemas() *xsdcompile.Set {
	if s == nil {
		return nil
	}
	return s.schemas
}

// Documents returns an owned, deterministically ordered graph inventory.
func (s *Set) Documents() []Document {
	if s == nil {
		return nil
	}
	result := append([]Document(nil), s.documents...)
	for index := range result {
		result[index].Dependencies = append([]string(nil), result[index].Dependencies...)
	}
	return result
}

// Interfaces returns owned compiled interface values in expanded-name order.
func (s *Set) Interfaces() []Interface {
	if s == nil {
		return nil
	}
	result := append([]Interface(nil), s.interfaces...)
	for index := range result {
		result[index].Operations = cloneOperations(result[index].Operations)
		result[index].Extends = append([]wsdl.QName(nil), result[index].Extends...)
		result[index].Faults = append([]wsdl.QName(nil), result[index].Faults...)
	}
	return result
}

// Interface returns an owned interface value by expanded name.
func (s *Set) Interface(name wsdl.QName) (Interface, bool) {
	if s == nil {
		return Interface{}, false
	}
	index := sort.Search(len(s.interfaces), func(index int) bool {
		return !lessQName(s.interfaces[index].Name, name)
	})
	if index == len(s.interfaces) || s.interfaces[index].Name != name {
		return Interface{}, false
	}
	return cloneInterface(s.interfaces[index]), true
}

// Operation returns one owned operation from a named interface.
func (s *Set) Operation(interfaceName wsdl.QName, name string) (Operation, bool) {
	interfaceValue, ok := s.Interface(interfaceName)
	if !ok {
		return Operation{}, false
	}
	index := sort.Search(len(interfaceValue.Operations), func(index int) bool {
		return interfaceValue.Operations[index].Name >= name
	})
	if index == len(interfaceValue.Operations) || interfaceValue.Operations[index].Name != name ||
		(index+1 < len(interfaceValue.Operations) && interfaceValue.Operations[index+1].Name == name) {
		return Operation{}, false
	}
	return cloneOperation(interfaceValue.Operations[index]), true
}

// OperationBySignature returns one owned operation by its complete WSDL 1.1
// overload identity. WSDL 2.0 callers normally use Operation instead.
func (s *Set) OperationBySignature(
	interfaceName wsdl.QName,
	name string,
	input string,
	output string,
) (Operation, bool) {
	interfaceValue, ok := s.Interface(interfaceName)
	if !ok {
		return Operation{}, false
	}
	for _, operation := range interfaceValue.Operations {
		identity := operationIdentity(operation)
		if identity.Name == name && identity.Input == input && identity.Output == output {
			return cloneOperation(operation), true
		}
	}
	return Operation{}, false
}

// Bindings returns owned compiled bindings in expanded-name order.
func (s *Set) Bindings() []Binding {
	if s == nil {
		return nil
	}
	result := append([]Binding(nil), s.bindings...)
	for index := range result {
		result[index].Operations = append([]string(nil), result[index].Operations...)
		result[index].OperationReferences = append(
			[]OperationReference(nil), result[index].OperationReferences...,
		)
	}
	return result
}

// Binding returns an owned binding value by expanded name.
func (s *Set) Binding(name wsdl.QName) (Binding, bool) {
	if s == nil {
		return Binding{}, false
	}
	index := sort.Search(len(s.bindings), func(index int) bool {
		return !lessQName(s.bindings[index].Name, name)
	})
	if index == len(s.bindings) || s.bindings[index].Name != name {
		return Binding{}, false
	}
	return cloneBinding(s.bindings[index]), true
}

// Services returns owned compiled services in expanded-name order.
func (s *Set) Services() []Service {
	if s == nil {
		return nil
	}
	result := append([]Service(nil), s.services...)
	for index := range result {
		result[index].Endpoints = append([]Endpoint(nil), result[index].Endpoints...)
	}
	return result
}

// Service returns an owned service value by expanded name.
func (s *Set) Service(name wsdl.QName) (Service, bool) {
	if s == nil {
		return Service{}, false
	}
	index := sort.Search(len(s.services), func(index int) bool {
		return !lessQName(s.services[index].Name, name)
	})
	if index == len(s.services) || s.services[index].Name != name {
		return Service{}, false
	}
	return cloneService(s.services[index]), true
}

func cloneInterface(value Interface) Interface {
	value.Extends = append([]wsdl.QName(nil), value.Extends...)
	value.Operations = cloneOperations(value.Operations)
	value.Faults = append([]wsdl.QName(nil), value.Faults...)
	return value
}

func cloneOperations(values []Operation) []Operation {
	result := append([]Operation(nil), values...)
	for index := range result {
		result[index] = cloneOperation(result[index])
	}
	return result
}

func cloneOperation(value Operation) Operation {
	value.Styles = append([]string(nil), value.Styles...)
	value.RPCSignature = append([]wsdl.RPCSignatureParameter20(nil), value.RPCSignature...)
	value.Faults = append([]Fault(nil), value.Faults...)
	value.Inputs = cloneMessages(value.Inputs)
	value.Outputs = cloneMessages(value.Outputs)
	value.Input = cloneMessage(value.Input)
	value.Output = cloneMessage(value.Output)
	return value
}

func cloneMessages(values []Message) []Message {
	result := append([]Message(nil), values...)
	for index := range result {
		result[index].Parts = append([]Part(nil), result[index].Parts...)
	}
	return result
}

func cloneMessage(value *Message) *Message {
	if value == nil {
		return nil
	}
	result := *value
	result.Parts = append([]Part(nil), value.Parts...)
	return &result
}

func cloneBinding(value Binding) Binding {
	value.Operations = append([]string(nil), value.Operations...)
	value.OperationReferences = append([]OperationReference(nil), value.OperationReferences...)
	return value
}

func cloneService(value Service) Service {
	value.Endpoints = append([]Endpoint(nil), value.Endpoints...)
	return value
}

type resourceDocument struct {
	document     *wsdl.Document
	dependencies []string
}

type inlineSchemaSource struct {
	uri       string
	namespace string
	content   []byte
}

type compileState struct {
	compiler   *Compiler
	resources  map[string]*resourceDocument
	bytes      int64
	references int
}

// Compile parses, resolves, validates, and compiles one bounded WSDL graph.
func (c *Compiler) Compile(ctx context.Context, root Source) (*Set, error) {
	if c == nil {
		return nil, errors.New("wsdl compile: compiler is nil")
	}
	if err := validateIdentity(root.URI); err != nil {
		return nil, err
	}
	if int64(len(root.Content)) > c.limits.MaxBytes {
		return nil, fmt.Errorf("%w: WSDL bytes exceed %d", ErrLimitExceeded, c.limits.MaxBytes)
	}
	state := compileState{
		compiler: c, resources: make(map[string]*resourceDocument),
		bytes: int64(len(root.Content)),
	}
	document, err := wsdl.Parse(ctx, root.Content, wsdl.ParseOptions{
		SystemID: root.URI, MaxDocumentBytes: c.limits.MaxBytes,
	})
	if err != nil {
		return nil, err
	}
	state.resources[root.URI] = &resourceDocument{document: document}
	if err := state.resolveDocument(ctx, root.URI, 1); err != nil {
		return nil, err
	}
	return state.buildSet(ctx)
}

type reference struct {
	uri       string
	namespace string
	kind      resolve.Kind
	version   wsdl.Version
	include   bool
}

func (s *compileState) resolveDocument(ctx context.Context, identity string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > s.compiler.limits.MaxDepth {
		return fmt.Errorf("%w: graph depth exceeds %d", ErrLimitExceeded, s.compiler.limits.MaxDepth)
	}
	resource := s.resources[identity]
	references := documentReferences(resource.document)
	for _, reference := range references {
		s.references++
		if s.references > s.compiler.limits.MaxReferences {
			return fmt.Errorf("%w: references exceed %d", ErrLimitExceeded, s.compiler.limits.MaxReferences)
		}
		if reference.uri == "" {
			return fmt.Errorf("%w: reference has no absolute URI", ErrResourceIdentity)
		}
		resource.dependencies = append(resource.dependencies, reference.uri)
		if existing, ok := s.resources[reference.uri]; ok {
			if err := validateReference(reference, existing.document, resource.document); err != nil {
				return err
			}
			continue
		}
		if len(s.resources) >= s.compiler.limits.MaxDocuments {
			return fmt.Errorf("%w: documents exceed %d", ErrLimitExceeded, s.compiler.limits.MaxDocuments)
		}
		resolved, err := s.compiler.resolver.Resolve(ctx, resolve.Request{
			URI: reference.uri, Namespace: reference.namespace,
			Kind: reference.kind, Version: string(reference.version),
		})
		if err != nil {
			return err
		}
		if resolved.URI != reference.uri {
			return fmt.Errorf(
				"%w: requested %q, received %q",
				ErrResourceIdentity,
				reference.uri,
				resolved.URI,
			)
		}
		if err := validateIdentity(resolved.URI); err != nil {
			return err
		}
		s.bytes += int64(len(resolved.Content))
		if s.bytes > s.compiler.limits.MaxBytes {
			return fmt.Errorf("%w: WSDL bytes exceed %d", ErrLimitExceeded, s.compiler.limits.MaxBytes)
		}
		document, err := wsdl.Parse(ctx, resolved.Content, wsdl.ParseOptions{
			SystemID: resolved.URI, MaxDocumentBytes: s.compiler.limits.MaxBytes,
		})
		if err != nil {
			return err
		}
		if err := validateReference(reference, document, resource.document); err != nil {
			return err
		}
		s.resources[resolved.URI] = &resourceDocument{document: document}
		if err := s.resolveDocument(ctx, resolved.URI, depth+1); err != nil {
			return err
		}
	}
	sort.Strings(resource.dependencies)
	return nil
}

func documentReferences(document *wsdl.Document) []reference {
	if definitions, ok := document.Definitions11(); ok {
		result := make([]reference, 0, len(definitions.Imports))
		for _, importValue := range definitions.Imports {
			result = append(result, reference{
				uri: importValue.URI, namespace: importValue.Namespace,
				kind: resolve.KindImport, version: wsdl.Version11,
			})
		}
		return result
	}
	description, _ := document.Description20()
	result := make([]reference, 0, len(description.Imports)+len(description.Includes))
	for _, importValue := range description.Imports {
		result = append(result, reference{
			uri: importValue.URI, namespace: importValue.Namespace,
			kind: resolve.KindImport, version: wsdl.Version20,
		})
	}
	for _, include := range description.Includes {
		result = append(result, reference{
			uri: include.URI, namespace: description.TargetNamespace,
			kind: resolve.KindInclude, version: wsdl.Version20, include: true,
		})
	}
	return result
}

func validateReference(reference reference, child, parent *wsdl.Document) error {
	if child.Version() != reference.version {
		return fmt.Errorf("%w: expected %s, received %s", ErrVersion, reference.version, child.Version())
	}
	if reference.include && parent.Version() != child.Version() {
		return fmt.Errorf("%w: include changed WSDL version", ErrVersion)
	}
	if namespace(child) != reference.namespace {
		return fmt.Errorf(
			"%w: expected %q, received %q",
			ErrNamespace,
			reference.namespace,
			namespace(child),
		)
	}
	return nil
}

func namespace(document *wsdl.Document) string {
	if definitions, ok := document.Definitions11(); ok {
		return definitions.TargetNamespace
	}
	description, _ := document.Description20()
	return description.TargetNamespace
}

func validateIdentity(identity string) error {
	uri, err := url.Parse(identity)
	if err != nil || !uri.IsAbs() || uri.Fragment != "" {
		return fmt.Errorf("%w: invalid resource URI %q", ErrResourceIdentity, identity)
	}
	return nil
}

func (s *compileState) buildSet(ctx context.Context) (*Set, error) {
	set := &Set{}
	interfaceNames := make(map[wsdl.QName]struct{})
	bindingNames := make(map[wsdl.QName]struct{})
	serviceNames := make(map[wsdl.QName]struct{})
	identities := make([]string, 0, len(s.resources))
	for identity := range s.resources {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	messages11 := compileMessages11(s.resources)
	components := 0
	addComponents := func(count int) error {
		if count > s.compiler.limits.MaxComponents-components {
			return fmt.Errorf(
				"%w: components exceed %d",
				ErrLimitExceeded,
				s.compiler.limits.MaxComponents,
			)
		}
		components += count
		return nil
	}
	for _, identity := range identities {
		resource := s.resources[identity]
		set.documents = append(set.documents, Document{
			URI: identity, Namespace: namespace(resource.document),
			Version:      resource.document.Version(),
			Dependencies: append([]string(nil), resource.dependencies...),
		})
		if definitions, ok := resource.document.Definitions11(); ok {
			for _, message := range definitions.Messages {
				if err := addComponents(1 + len(message.Parts)); err != nil {
					return nil, err
				}
			}
			for _, portType := range definitions.PortTypes {
				if err := addComponents(1 + len(portType.Operations)); err != nil {
					return nil, err
				}
				name := wsdl.QName{Namespace: definitions.TargetNamespace, Local: portType.Name}
				operations := make([]Operation, 0, len(portType.Operations))
				for _, operation := range portType.Operations {
					operations = append(operations, compileOperation11(operation, messages11))
				}
				sortOperations(operations)
				set.interfaces = append(set.interfaces, Interface{Name: name, Operations: operations})
				if err := addName(interfaceNames, "interface", name); err != nil {
					return nil, err
				}
			}
			for _, binding := range definitions.Bindings {
				if err := addComponents(1 + len(binding.Operations)); err != nil {
					return nil, err
				}
				operations := make([]string, 0, len(binding.Operations))
				references := make([]OperationReference, 0, len(binding.Operations))
				for _, operation := range binding.Operations {
					operations = append(operations, operation.Name)
					references = append(references, compileBindingOperationReference11(
						operation, binding.Type, s.resources,
					))
				}
				sort.Strings(operations)
				sortOperationReferences(references)
				name := wsdl.QName{Namespace: definitions.TargetNamespace, Local: binding.Name}
				set.bindings = append(set.bindings, Binding{
					Name: name, Interface: binding.Type, Operations: operations,
					OperationReferences: references,
				})
				if err := addName(bindingNames, "binding", name); err != nil {
					return nil, err
				}
			}
			for _, service := range definitions.Services {
				if err := addComponents(1 + len(service.Ports)); err != nil {
					return nil, err
				}
				endpoints := make([]Endpoint, 0, len(service.Ports))
				for _, port := range service.Ports {
					address := ""
					if port.SOAPAddress != nil {
						address = port.SOAPAddress.Location
					} else if port.HTTPAddress != nil {
						address = port.HTTPAddress.Location
					}
					endpoints = append(endpoints, Endpoint{
						Name: port.Name, Binding: port.Binding, Address: address,
					})
				}
				sortEndpoints(endpoints)
				name := wsdl.QName{Namespace: definitions.TargetNamespace, Local: service.Name}
				set.services = append(set.services, Service{Name: name, Endpoints: endpoints})
				if err := addName(serviceNames, "service", name); err != nil {
					return nil, err
				}
			}
		} else {
			description, _ := resource.document.Description20()
			for _, interfaceValue := range description.Interfaces {
				if err := addComponents(countInterface20Components(interfaceValue)); err != nil {
					return nil, err
				}
				operations := make([]Operation, 0, len(interfaceValue.Operations))
				faultElements := interfaceFaults20(description.TargetNamespace, interfaceValue.Faults)
				for _, operation := range interfaceValue.Operations {
					operations = append(operations, compileOperation20(
						operation, interfaceValue.StyleDefault, faultElements,
					))
				}
				sortOperations(operations)
				faults := make([]wsdl.QName, 0, len(interfaceValue.Faults))
				for _, fault := range interfaceValue.Faults {
					faults = append(faults, wsdl.QName{
						Namespace: description.TargetNamespace, Local: fault.Name,
					})
				}
				sort.Slice(faults, func(left, right int) bool {
					return lessQName(faults[left], faults[right])
				})
				name := wsdl.QName{Namespace: description.TargetNamespace, Local: interfaceValue.Name}
				set.interfaces = append(set.interfaces, Interface{
					Name: name, Extends: append([]wsdl.QName(nil), interfaceValue.Extends...),
					Operations: operations, Faults: faults,
				})
				if err := addName(interfaceNames, "interface", name); err != nil {
					return nil, err
				}
			}
			for _, binding := range description.Bindings {
				if err := addComponents(countBinding20Components(binding)); err != nil {
					return nil, err
				}
				operations := make([]string, 0, len(binding.Operations))
				references := make([]OperationReference, 0, len(binding.Operations))
				for _, operation := range binding.Operations {
					operations = append(operations, operation.Ref.Local)
					references = append(references, OperationReference{Name: operation.Ref.Local})
				}
				sort.Strings(operations)
				sortOperationReferences(references)
				name := wsdl.QName{Namespace: description.TargetNamespace, Local: binding.Name}
				set.bindings = append(set.bindings, Binding{
					Name: name, Interface: binding.Interface,
					Type: binding.Type, Operations: operations,
					OperationReferences: references,
				})
				if err := addName(bindingNames, "binding", name); err != nil {
					return nil, err
				}
			}
			for _, service := range description.Services {
				if err := addComponents(1 + len(service.Endpoints)); err != nil {
					return nil, err
				}
				endpoints := make([]Endpoint, 0, len(service.Endpoints))
				for _, endpoint := range service.Endpoints {
					endpoints = append(endpoints, Endpoint{
						Name: endpoint.Name, Binding: endpoint.Binding,
						Address: endpoint.Address,
					})
				}
				sortEndpoints(endpoints)
				name := wsdl.QName{Namespace: description.TargetNamespace, Local: service.Name}
				set.services = append(set.services, Service{
					Name: name, Interface: service.Interface, Endpoints: endpoints,
				})
				if err := addName(serviceNames, "service", name); err != nil {
					return nil, err
				}
			}
		}
	}
	sortInterfaces(set.interfaces)
	sortBindings(set.bindings)
	sortServices(set.services)
	inherited, err := expandInterfaceInheritance(set.interfaces)
	if err != nil {
		return nil, err
	}
	if err := addComponents(inherited); err != nil {
		return nil, err
	}
	if err := validateGraph(set, interfaceNames, bindingNames); err != nil {
		return nil, err
	}
	for _, identity := range identities {
		if err := wsdl.Validate(s.resources[identity].document, s.compiler.validation).Err(); err != nil {
			return nil, fmt.Errorf("wsdl compile: validate %s: %w", identity, err)
		}
	}
	schemas, err := s.compileSchemas(ctx, identities)
	if err != nil {
		return nil, err
	}
	set.schemas = schemas
	if err := validateSchemaReferences20(s.resources, schemas); err != nil {
		return nil, err
	}
	if err := validateRPCSchemas20(s.resources, schemas); err != nil {
		return nil, err
	}
	if err := validateOperationStyleSchemas20(s.resources, schemas); err != nil {
		return nil, err
	}
	return set, nil
}

func compileMessages11(resources map[string]*resourceDocument) map[wsdl.QName]Message {
	result := make(map[wsdl.QName]Message)
	for _, resource := range resources {
		definitions, ok := resource.document.Definitions11()
		if !ok {
			continue
		}
		for _, message := range definitions.Messages {
			name := wsdl.QName{Namespace: definitions.TargetNamespace, Local: message.Name}
			parts := make([]Part, 0, len(message.Parts))
			for _, part := range message.Parts {
				parts = append(parts, Part{
					Name: part.Name, Element: part.Element, Type: part.Type,
				})
			}
			result[name] = Message{Name: name, Parts: parts}
		}
	}
	return result
}

func compileOperation11(operation wsdl.Operation11, messages map[wsdl.QName]Message) Operation {
	result := Operation{Name: operation.Name, Style: string(operation.Style)}
	result.Input = compileMessage11(operation.Input, messages)
	result.Output = compileMessage11(operation.Output, messages)
	input, output := operationMessageNames11(operation)
	if result.Input != nil {
		result.Input.Label = input
	}
	if result.Output != nil {
		result.Output.Label = output
	}
	if result.Input != nil {
		result.Inputs = []Message{*cloneMessage(result.Input)}
	}
	if result.Output != nil {
		result.Outputs = []Message{*cloneMessage(result.Output)}
	}
	for _, fault := range operation.Faults {
		direction := "out"
		if operation.Style == wsdl.OperationStyleSolicitResponse {
			direction = "in"
		}
		result.Faults = append(result.Faults, Fault{
			Name:  wsdl.QName{Namespace: fault.Message.Namespace, Local: fault.Name},
			Label: fault.Name, Direction: direction, Message: fault.Message,
		})
	}
	return result
}

func compileBindingOperationReference11(
	bound wsdl.BindingOperation11,
	portTypeName wsdl.QName,
	resources map[string]*resourceDocument,
) OperationReference {
	candidates := make([]OperationReference, 0, 1)
	for _, resource := range resources {
		definitions, ok := resource.document.Definitions11()
		if !ok || definitions.TargetNamespace != portTypeName.Namespace {
			continue
		}
		for _, portType := range definitions.PortTypes {
			if portType.Name != portTypeName.Local {
				continue
			}
			for _, operation := range portType.Operations {
				if operation.Name != bound.Name {
					continue
				}
				input, output := operationMessageNames11(operation)
				if bound.Input != nil && bound.Input.Name != "" && bound.Input.Name != input {
					continue
				}
				if bound.Output != nil && bound.Output.Name != "" && bound.Output.Name != output {
					continue
				}
				candidates = append(candidates, OperationReference{
					Name: operation.Name, Input: input, Output: output,
				})
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	result := OperationReference{Name: bound.Name}
	if bound.Input != nil {
		result.Input = bound.Input.Name
	}
	if bound.Output != nil {
		result.Output = bound.Output.Name
	}
	return result
}

func operationMessageNames11(operation wsdl.Operation11) (string, string) {
	input, output := "", ""
	if operation.Input != nil {
		input = operation.Input.Name
	}
	if operation.Output != nil {
		output = operation.Output.Name
	}
	switch operation.Style {
	case wsdl.OperationStyleOneWay:
		if input == "" {
			input = operation.Name
		}
	case wsdl.OperationStyleRequestResponse:
		if input == "" {
			input = operation.Name + "Request"
		}
		if output == "" {
			output = operation.Name + "Response"
		}
	case wsdl.OperationStyleSolicitResponse:
		if input == "" {
			input = operation.Name + "Response"
		}
		if output == "" {
			output = operation.Name + "Solicit"
		}
	case wsdl.OperationStyleNotification:
		if output == "" {
			output = operation.Name
		}
	}
	return input, output
}

func compileMessage11(reference *wsdl.OperationMessage11, messages map[wsdl.QName]Message) *Message {
	if reference == nil {
		return nil
	}
	result := messages[reference.Message]
	result.Name = reference.Message
	result.Label = reference.Name
	result.Parts = append([]Part(nil), result.Parts...)
	return &result
}

func interfaceFaults20(
	targetNamespace string,
	values []wsdl.InterfaceFault20,
) map[wsdl.QName]wsdl.InterfaceFault20 {
	result := make(map[wsdl.QName]wsdl.InterfaceFault20, len(values))
	for _, value := range values {
		result[wsdl.QName{Namespace: targetNamespace, Local: value.Name}] = value
	}
	return result
}

func compileOperation20(
	operation wsdl.InterfaceOperation20,
	defaultStyles []string,
	faults map[wsdl.QName]wsdl.InterfaceFault20,
) Operation {
	styles := operation.Style
	if len(styles) == 0 {
		styles = defaultStyles
	}
	result := Operation{
		Name: operation.Name, Pattern: string(operation.Pattern),
		Styles: append([]string(nil), styles...), Safe: operation.Safe,
		RPCSignature:    append([]wsdl.RPCSignatureParameter20(nil), operation.RPCSignature...),
		RPCSignatureSet: operation.RPCSignatureSet,
	}
	for index, message := range interfaceMessages20(operation.Inputs, operation.Input) {
		result.Inputs = append(result.Inputs, *compileMessage20(&message, defaultMessageLabel20("In", index)))
	}
	for index, message := range interfaceMessages20(operation.Outputs, operation.Output) {
		result.Outputs = append(result.Outputs, *compileMessage20(&message, defaultMessageLabel20("Out", index)))
	}
	if len(result.Inputs) > 0 {
		result.Input = cloneMessage(&result.Inputs[0])
	}
	if len(result.Outputs) > 0 {
		result.Output = cloneMessage(&result.Outputs[0])
	}
	for _, reference := range operation.InFaults {
		result.Faults = append(result.Faults, compileFault20(reference, "in", faults))
	}
	for _, reference := range operation.OutFaults {
		result.Faults = append(result.Faults, compileFault20(reference, "out", faults))
	}
	return result
}

func interfaceMessages20(
	values []wsdl.InterfaceMessageReference20,
	legacy *wsdl.InterfaceMessageReference20,
) []wsdl.InterfaceMessageReference20 {
	if len(values) > 0 {
		return values
	}
	if legacy != nil {
		return []wsdl.InterfaceMessageReference20{*legacy}
	}
	return nil
}

func defaultMessageLabel20(direction string, index int) string {
	if index == 0 {
		return direction
	}
	return ""
}

func compileMessage20(reference *wsdl.InterfaceMessageReference20, defaultLabel string) *Message {
	if reference == nil {
		return nil
	}
	label := reference.MessageLabel
	if label == "" {
		label = defaultLabel
	}
	return &Message{
		Label: label, Element: reference.Element,
		ContentModel: reference.MessageContentModel,
	}
}

func compileFault20(
	reference wsdl.InterfaceFaultReference20,
	direction string,
	faults map[wsdl.QName]wsdl.InterfaceFault20,
) Fault {
	fault := faults[reference.Ref]
	return Fault{
		Name: reference.Ref, Label: reference.MessageLabel, Direction: direction,
		Element: fault.Element, ContentModel: fault.MessageContentModel,
	}
}

func (s *compileState) compileSchemas(
	ctx context.Context,
	identities []string,
) (*xsdcompile.Set, error) {
	sources := make([]inlineSchemaSource, 0)
	imports := make([]xsd.SchemaReference, 0)
	for _, identity := range identities {
		document := s.resources[identity].document
		var schemas []*xsd.Document
		if definitions, ok := document.Definitions11(); ok && definitions.Types != nil {
			schemas = definitions.Types.Schemas
		} else if description, ok := document.Description20(); ok && description.Types != nil {
			schemas = description.Types.Schemas
			imports = append(imports, description.Types.Imports...)
		}
		for index, schema := range schemas {
			content, err := xsd.Marshal(schema)
			if err != nil {
				return nil, fmt.Errorf("wsdl compile: marshal inline schema: %w", err)
			}
			uri, err := inlineSchemaURI(identity, index)
			if err != nil {
				return nil, err
			}
			sources = append(sources, inlineSchemaSource{
				uri: uri, namespace: schema.TargetNamespace, content: content,
			})
		}
	}
	if len(sources) == 0 && len(imports) == 0 {
		return nil, nil
	}
	resources := make(map[string][]byte, len(sources))
	for _, source := range sources {
		resources[source.uri] = source.content
	}
	memory, err := xsdresolve.NewMemory(resources)
	if err != nil {
		return nil, fmt.Errorf("wsdl compile: inline schema resolver: %w", err)
	}
	compiler, err := xsdcompile.New(xsdcompile.Options{
		Resolver: xsdresolve.Chain(memory, s.compiler.schemaResolver),
		Limits:   s.compiler.schemaLimits,
	})
	if err != nil {
		return nil, fmt.Errorf("wsdl compile: create schema compiler: %w", err)
	}
	wrapper, err := schemaWrapper(sources, imports)
	if err != nil {
		return nil, err
	}
	set, err := compiler.Compile(ctx, xsdcompile.Source{
		URI: "urn:wsdl:compiled-schema-set", Content: wrapper,
	})
	if err != nil {
		return nil, fmt.Errorf("wsdl compile: compile schemas: %w", err)
	}
	return set, nil
}

func inlineSchemaURI(owner string, index int) (string, error) {
	identity, err := url.Parse(owner)
	if err != nil {
		return "", fmt.Errorf("%w: invalid schema owner URI %q", ErrResourceIdentity, owner)
	}
	query := identity.Query()
	query.Set("wsdl-inline-schema", strconv.Itoa(index+1))
	identity.RawQuery = query.Encode()
	return identity.String(), nil
}

func schemaWrapper(
	sources []inlineSchemaSource,
	imports []xsd.SchemaReference,
) ([]byte, error) {
	var output bytes.Buffer
	output.WriteString(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`)
	for _, source := range sources {
		if source.namespace == "" {
			output.WriteString(`<xs:include schemaLocation="`)
		} else {
			output.WriteString(`<xs:import namespace="`)
			if err := escapeSchemaText(&output, []byte(source.namespace)); err != nil {
				return nil, err
			}
			output.WriteString(`" schemaLocation="`)
		}
		if err := escapeSchemaText(&output, []byte(source.uri)); err != nil {
			return nil, err
		}
		output.WriteString(`"/>`)
	}
	for _, importValue := range imports {
		output.WriteString(`<xs:import`)
		if importValue.Namespace != "" {
			output.WriteString(` namespace="`)
			if err := escapeSchemaText(&output, []byte(importValue.Namespace)); err != nil {
				return nil, err
			}
			output.WriteString(`"`)
		}
		if importValue.URI != "" {
			output.WriteString(` schemaLocation="`)
			if err := escapeSchemaText(&output, []byte(importValue.URI)); err != nil {
				return nil, err
			}
			output.WriteString(`"`)
		}
		output.WriteString(`/>`)
	}
	output.WriteString(`</xs:schema>`)
	return output.Bytes(), nil
}

func validateSchemaReferences20(
	resources map[string]*resourceDocument,
	schemas *xsdcompile.Set,
) error {
	elements := make(map[wsdl.QName]struct{})
	types := make(map[wsdl.QName]struct{})
	if schemas != nil {
		for _, name := range schemas.ElementNames() {
			elements[wsdl.QName{Namespace: name.Namespace, Local: name.Local}] = struct{}{}
		}
		for _, name := range schemas.SimpleTypeNames() {
			types[wsdl.QName{Namespace: name.Namespace, Local: name.Local}] = struct{}{}
		}
		for _, name := range schemas.ComplexTypeNames() {
			types[wsdl.QName{Namespace: name.Namespace, Local: name.Local}] = struct{}{}
		}
	}
	for _, resource := range resources {
		if definitions, ok := resource.document.Definitions11(); ok {
			for _, message := range definitions.Messages {
				for _, part := range message.Parts {
					if part.Element.Local != "" {
						if _, exists := elements[part.Element]; !exists {
							return unresolvedSchemaComponent("element", part.Element)
						}
					}
					if part.Type.Local != "" && part.Type.Namespace != wsdl.NamespaceXMLSchema {
						if _, exists := types[part.Type]; !exists {
							return unresolvedSchemaComponent("type", part.Type)
						}
					}
				}
			}
			continue
		}
		description, _ := resource.document.Description20()
		for _, interfaceValue := range description.Interfaces {
			for _, fault := range interfaceValue.Faults {
				if fault.MessageContentModel == wsdl.MessageContentElement {
					if _, exists := elements[fault.Element]; !exists {
						return unresolvedSchemaComponent("element", fault.Element)
					}
				}
			}
			for _, operation := range interfaceValue.Operations {
				messages := append(
					interfaceMessages20(operation.Inputs, operation.Input),
					interfaceMessages20(operation.Outputs, operation.Output)...,
				)
				for _, message := range messages {
					if message.MessageContentModel != wsdl.MessageContentElement {
						continue
					}
					if _, exists := elements[message.Element]; !exists {
						return unresolvedSchemaComponent("element", message.Element)
					}
				}
			}
		}
		for _, binding := range description.Bindings {
			for _, fault := range binding.Faults {
				if fault.SOAP != nil {
					for _, header := range fault.SOAP.Headers {
						if _, exists := elements[header.Element]; !exists {
							return unresolvedSchemaComponent("element", header.Element)
						}
					}
				}
				if fault.HTTP != nil {
					if err := validateHTTPHeaderTypes20(fault.HTTP.Headers, types); err != nil {
						return err
					}
				}
			}
			for _, operation := range binding.Operations {
				for _, message := range operation.Inputs {
					if err := validateBindingMessageSchema20(message, elements, types); err != nil {
						return err
					}
				}
				for _, message := range operation.Outputs {
					if err := validateBindingMessageSchema20(message, elements, types); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

type rpcMessageShape20 struct {
	elements map[wsdl.QName]xsd.QName
	wildcard bool
}

func validateRPCSchemas20(
	resources map[string]*resourceDocument,
	schemas *xsdcompile.Set,
) error {
	for _, resource := range resources {
		description, ok := resource.document.Description20()
		if !ok {
			continue
		}
		for _, interfaceValue := range description.Interfaces {
			for _, operation := range interfaceValue.Operations {
				if !operationUsesRPCStyle20(interfaceValue, operation) {
					continue
				}
				inputs := interfaceMessages20(operation.Inputs, operation.Input)
				outputs := interfaceMessages20(operation.Outputs, operation.Output)
				input, err := rpcMessageShape(operation.Name, "input", inputs, schemas)
				if err != nil {
					return err
				}
				output, err := rpcMessageShape(operation.Name, "output", outputs, schemas)
				if err != nil {
					return err
				}
				if err := validateRPCSignature20(operation, input, output); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateOperationStyleSchemas20(
	resources map[string]*resourceDocument,
	schemas *xsdcompile.Set,
) error {
	for _, resource := range resources {
		description, ok := resource.document.Description20()
		if !ok {
			continue
		}
		for _, interfaceValue := range description.Interfaces {
			for _, operation := range interfaceValue.Operations {
				styles := operation.Style
				if len(styles) == 0 {
					styles = interfaceValue.StyleDefault
				}
				for _, style := range styles {
					if style != wsdl.StyleIRI && style != wsdl.StyleMultipart {
						continue
					}
					message := initialOperationMessage20(operation)
					if message == nil {
						continue
					}
					if err := validateOperationStyleSchema20(style, operation.Name, *message, schemas); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func initialOperationMessage20(
	operation wsdl.InterfaceOperation20,
) *wsdl.InterfaceMessageReference20 {
	inputs := interfaceMessages20(operation.Inputs, operation.Input)
	outputs := interfaceMessages20(operation.Outputs, operation.Output)
	switch operation.Pattern {
	case wsdl.MEPOutOnly, wsdl.MEPRobustOutOnly, wsdl.MEPOutIn, wsdl.MEPOutOptionalIn:
		if len(outputs) > 0 {
			return &outputs[0]
		}
	default:
		if len(inputs) > 0 {
			return &inputs[0]
		}
	}
	if len(outputs) > 0 {
		return &outputs[0]
	}
	return nil
}

func validateOperationStyleSchema20(
	style string,
	operation string,
	message wsdl.InterfaceMessageReference20,
	schemas *xsdcompile.Set,
) error {
	invalid := invalidIRIStyle
	if style == wsdl.StyleMultipart {
		invalid = invalidMultipartStyle
	}
	if schemas == nil {
		return invalid(operation, "initial message schema is unavailable")
	}
	name := xsd.QName{Namespace: message.Element.Namespace, Local: message.Element.Local}
	element, exists := schemas.Element(name)
	if !exists {
		return invalid(operation, "initial wrapper element is unresolved")
	}
	typeDefinition, exists := rpcComplexType(element, schemas)
	if !exists || typeDefinition.Content == nil ||
		typeDefinition.Content.Compositor != xsd.Sequence || typeDefinition.SimpleContent {
		return invalid(operation, "wrapper must use a complex sequence")
	}
	if complexTypeDeclaresAttributes(typeDefinition) {
		return invalid(operation, "wrapper must not declare attributes")
	}
	seen := make(map[string]struct{})
	for _, particle := range typeDefinition.Content.Particles {
		if particle.Element == nil || particle.Element.Ref.Local != "" || particle.Element.Name == "" {
			return invalid(operation, "sequence must contain only local elements")
		}
		child := *particle.Element
		if style == wsdl.StyleMultipart {
			if particle.MinOccurs != 1 || particle.MaxOccurs != 1 || particle.Unbounded {
				return invalid(operation, "child elements must occur exactly once")
			}
			if _, duplicate := seen[child.Name]; duplicate {
				return invalid(operation, "sequence repeats child element "+child.Name)
			}
			seen[child.Name] = struct{}{}
			if childComplexType, complex := elementComplexType(child, schemas); complex && complexTypeDeclaresAttributes(childComplexType) {
				return invalid(operation, "child elements must not declare attributes")
			}
			continue
		}
		if !iriSimpleElementAllowed(child, schemas) {
			return invalid(operation, "child element "+child.Name+" must use an allowed simple type")
		}
	}
	return nil
}

func complexTypeDeclaresAttributes(value xsd.ComplexType) bool {
	return len(value.Attributes) > 0 || len(value.AttributeGroupRefs) > 0 ||
		len(value.AttributeGroupReferences) > 0 || value.AttributeWildcard != nil
}

func elementComplexType(element xsd.Element, schemas *xsdcompile.Set) (xsd.ComplexType, bool) {
	if element.InlineComplexType != nil {
		return *element.InlineComplexType, true
	}
	if element.Type.Local == "" || schemas == nil {
		return xsd.ComplexType{}, false
	}
	return schemas.ComplexType(element.Type)
}

func iriSimpleElementAllowed(element xsd.Element, schemas *xsdcompile.Set) bool {
	if element.InlineComplexType != nil {
		return false
	}
	if element.InlineSimpleType != nil {
		return !iriSimpleTypeForbidden(*element.InlineSimpleType, schemas, make(map[xsd.QName]struct{}))
	}
	if element.Type.Local == "" {
		return false
	}
	if element.Type.Namespace == xsd.Namespace {
		return element.Type.Local != "anyType" && !forbiddenIRIPrimitive(element.Type)
	}
	definition, exists := schemas.SimpleType(element.Type)
	if !exists {
		return false
	}
	return !iriSimpleTypeForbidden(definition, schemas, map[xsd.QName]struct{}{element.Type: {}})
}

func iriSimpleTypeForbidden(
	definition xsd.SimpleType,
	schemas *xsdcompile.Set,
	seen map[xsd.QName]struct{},
) bool {
	if definition.InlineBase != nil && iriSimpleTypeForbidden(*definition.InlineBase, schemas, seen) {
		return true
	}
	if definition.Base.Local == "" {
		return false
	}
	if forbiddenIRIPrimitive(definition.Base) {
		return true
	}
	if definition.Base.Namespace == xsd.Namespace || schemas == nil {
		return false
	}
	if _, exists := seen[definition.Base]; exists {
		return false
	}
	seen[definition.Base] = struct{}{}
	base, exists := schemas.SimpleType(definition.Base)
	return exists && iriSimpleTypeForbidden(base, schemas, seen)
}

func forbiddenIRIPrimitive(name xsd.QName) bool {
	if name.Namespace != xsd.Namespace {
		return false
	}
	switch name.Local {
	case "QName", "NOTATION", "hexBinary", "base64Binary":
		return true
	default:
		return false
	}
}

func invalidIRIStyle(operation string, message string) error {
	return fmt.Errorf("%w: operation %q %s", ErrInvalidIRIStyle, operation, message)
}

func invalidMultipartStyle(operation string, message string) error {
	return fmt.Errorf("%w: operation %q %s", ErrInvalidMultipartStyle, operation, message)
}

func operationUsesRPCStyle20(
	interfaceValue wsdl.Interface20,
	operation wsdl.InterfaceOperation20,
) bool {
	styles := operation.Style
	if len(styles) == 0 {
		styles = interfaceValue.StyleDefault
	}
	for _, style := range styles {
		if style == wsdl.StyleRPC {
			return true
		}
	}
	return false
}

func rpcMessageShape(
	operation string,
	direction string,
	messages []wsdl.InterfaceMessageReference20,
	schemas *xsdcompile.Set,
) (rpcMessageShape20, error) {
	shape := rpcMessageShape20{elements: make(map[wsdl.QName]xsd.QName)}
	if len(messages) == 0 {
		return shape, nil
	}
	if len(messages) != 1 || schemas == nil {
		return shape, invalidRPC(operation, direction+" message schema is unavailable")
	}
	name := xsd.QName{Namespace: messages[0].Element.Namespace, Local: messages[0].Element.Local}
	element, exists := schemas.Element(name)
	if !exists {
		return shape, invalidRPC(operation, direction+" wrapper element is unresolved")
	}
	typeDefinition, exists := rpcComplexType(element, schemas)
	if !exists || typeDefinition.Content == nil ||
		typeDefinition.Content.Compositor != xsd.Sequence || typeDefinition.SimpleContent {
		return shape, invalidRPC(operation, direction+" wrapper must use a complex sequence")
	}
	if len(typeDefinition.Attributes) > 0 || len(typeDefinition.AttributeGroupRefs) > 0 ||
		len(typeDefinition.AttributeGroupReferences) > 0 {
		return shape, invalidRPC(operation, direction+" wrapper must not declare local attributes")
	}
	particles := typeDefinition.Content.Particles
	for index, particle := range particles {
		if particle.Element != nil {
			if particle.Element.Ref.Local != "" || particle.Element.Name == "" {
				return shape, invalidRPC(operation, direction+" sequence must contain local elements")
			}
			child := wsdl.QName{Namespace: particle.Element.Namespace, Local: particle.Element.Name}
			if _, duplicate := shape.elements[child]; duplicate {
				return shape, invalidRPC(operation, direction+" sequence repeats element "+formatRPCQName(child))
			}
			shape.elements[child] = particle.Element.Type
			continue
		}
		if particle.Wildcard != nil && direction == "input" && !shape.wildcard &&
			index == len(particles)-1 {
			shape.wildcard = true
			continue
		}
		return shape, invalidRPC(operation, direction+" sequence contains a forbidden particle")
	}
	return shape, nil
}

func rpcComplexType(
	element xsd.Element,
	schemas *xsdcompile.Set,
) (xsd.ComplexType, bool) {
	if element.InlineComplexType != nil {
		return *element.InlineComplexType, true
	}
	if element.Type.Local == "" {
		return xsd.ComplexType{}, false
	}
	return schemas.ComplexType(element.Type)
}

func validateRPCSignature20(
	operation wsdl.InterfaceOperation20,
	input rpcMessageShape20,
	output rpcMessageShape20,
) error {
	signature := make(map[wsdl.QName]wsdl.RPCDirection, len(operation.RPCSignature))
	for _, parameter := range operation.RPCSignature {
		signature[parameter.Name] = parameter.Direction
		_, in := input.elements[parameter.Name]
		_, out := output.elements[parameter.Name]
		valid := false
		switch parameter.Direction {
		case wsdl.RPCDirectionIn:
			valid = in && !out
		case wsdl.RPCDirectionOut, wsdl.RPCDirectionReturn:
			valid = out && !in
		case wsdl.RPCDirectionInOut:
			valid = in && out
		}
		if !valid {
			return invalidRPC(operation.Name, "signature direction does not match "+formatRPCQName(parameter.Name))
		}
	}
	for name, inputType := range input.elements {
		if _, exists := signature[name]; !exists {
			return invalidRPC(operation.Name, "signature omits input element "+formatRPCQName(name))
		}
		if outputType, exists := output.elements[name]; exists &&
			(inputType.Local == "" || inputType != outputType) {
			return invalidRPC(operation.Name, "shared element uses different named types "+formatRPCQName(name))
		}
	}
	for name := range output.elements {
		if _, exists := signature[name]; !exists {
			return invalidRPC(operation.Name, "signature omits output element "+formatRPCQName(name))
		}
	}
	return nil
}

func invalidRPC(operation string, message string) error {
	return fmt.Errorf("%w: operation %q %s", ErrInvalidRPCStyle, operation, message)
}

func formatRPCQName(name wsdl.QName) string {
	return "{" + name.Namespace + "}" + name.Local
}

func validateBindingMessageSchema20(
	message wsdl.BindingMessageReference20,
	elements map[wsdl.QName]struct{},
	types map[wsdl.QName]struct{},
) error {
	if message.SOAP != nil {
		for _, header := range message.SOAP.Headers {
			if _, exists := elements[header.Element]; !exists {
				return unresolvedSchemaComponent("element", header.Element)
			}
		}
	}
	if message.HTTP != nil {
		return validateHTTPHeaderTypes20(message.HTTP.Headers, types)
	}
	return nil
}

func validateHTTPHeaderTypes20(
	headers []wsdl.HTTPHeader20,
	types map[wsdl.QName]struct{},
) error {
	for _, header := range headers {
		if header.Type.Namespace == wsdl.NamespaceXMLSchema {
			continue
		}
		if _, exists := types[header.Type]; !exists {
			return unresolvedSchemaComponent("type", header.Type)
		}
	}
	return nil
}

func unresolvedSchemaComponent(kind string, name wsdl.QName) error {
	return fmt.Errorf(
		"%w: XML Schema %s {%s}%s",
		ErrUnresolvedComponent,
		kind,
		name.Namespace,
		name.Local,
	)
}

func countInterface20Components(value wsdl.Interface20) int {
	count := 1 + len(value.Faults)
	for _, operation := range value.Operations {
		count++
		count += len(interfaceMessages20(operation.Inputs, operation.Input))
		count += len(interfaceMessages20(operation.Outputs, operation.Output))
		count += len(operation.InFaults) + len(operation.OutFaults)
	}
	return count
}

func countBinding20Components(value wsdl.Binding20) int {
	count := 1 + len(value.Faults)
	for _, operation := range value.Operations {
		count++
		count += len(operation.Inputs) + len(operation.Outputs)
		count += len(operation.InFaults) + len(operation.OutFaults)
	}
	return count
}

func addName(names map[wsdl.QName]struct{}, kind string, name wsdl.QName) error {
	if _, exists := names[name]; exists {
		return fmt.Errorf("%w: %s {%s}%s", ErrDuplicateComponent, kind, name.Namespace, name.Local)
	}
	names[name] = struct{}{}
	return nil
}

func expandInterfaceInheritance(values []Interface) (int, error) {
	indexes := make(map[wsdl.QName]int, len(values))
	for index, value := range values {
		indexes[value.Name] = index
	}
	states := make([]uint8, len(values))
	added := 0
	var expand func(int) error
	expand = func(index int) error {
		if states[index] == 2 {
			return nil
		}
		if states[index] == 1 {
			return fmt.Errorf(
				"%w: interface inheritance cycle at {%s}%s",
				ErrUnresolvedComponent, values[index].Name.Namespace, values[index].Name.Local,
			)
		}
		states[index] = 1
		for _, parentName := range values[index].Extends {
			parentIndex, exists := indexes[parentName]
			if !exists {
				return fmt.Errorf(
					"%w: extended interface {%s}%s",
					ErrUnresolvedComponent, parentName.Namespace, parentName.Local,
				)
			}
			if err := expand(parentIndex); err != nil {
				return err
			}
			for _, operation := range values[parentIndex].Operations {
				existing := operationIndex(values[index].Operations, operation.Name)
				if existing >= 0 {
					if !reflect.DeepEqual(values[index].Operations[existing], operation) {
						return fmt.Errorf(
							"%w: inherited operation {%s}%s#%s",
							ErrDuplicateComponent, values[index].Name.Namespace,
							values[index].Name.Local, operation.Name,
						)
					}
					continue
				}
				values[index].Operations = append(values[index].Operations, cloneOperation(operation))
				added++
			}
			for _, fault := range values[parentIndex].Faults {
				if qnameIndex(values[index].Faults, fault) >= 0 {
					continue
				}
				values[index].Faults = append(values[index].Faults, fault)
				added++
			}
		}
		sortOperations(values[index].Operations)
		sort.Slice(values[index].Faults, func(left, right int) bool {
			return lessQName(values[index].Faults[left], values[index].Faults[right])
		})
		states[index] = 2
		return nil
	}
	for index := range values {
		if err := expand(index); err != nil {
			return 0, err
		}
	}
	return added, nil
}

func operationIndex(values []Operation, name string) int {
	for index := range values {
		if values[index].Name == name {
			return index
		}
	}
	return -1
}

func qnameIndex(values []wsdl.QName, name wsdl.QName) int {
	for index := range values {
		if values[index] == name {
			return index
		}
	}
	return -1
}

func validateGraph(
	set *Set,
	interfaces map[wsdl.QName]struct{},
	bindings map[wsdl.QName]struct{},
) error {
	operations := make(map[wsdl.QName]map[OperationReference]struct{}, len(set.interfaces))
	operationNames := make(map[wsdl.QName]map[string]struct{}, len(set.interfaces))
	for _, interfaceValue := range set.interfaces {
		identities := make(map[OperationReference]struct{}, len(interfaceValue.Operations))
		names := make(map[string]struct{}, len(interfaceValue.Operations))
		for _, operation := range interfaceValue.Operations {
			identities[operationIdentity(operation)] = struct{}{}
			names[operation.Name] = struct{}{}
		}
		operations[interfaceValue.Name] = identities
		operationNames[interfaceValue.Name] = names
	}
	for _, binding := range set.bindings {
		if _, exists := interfaces[binding.Interface]; !exists {
			return fmt.Errorf("%w: binding interface {%s}%s", ErrUnresolvedComponent, binding.Interface.Namespace, binding.Interface.Local)
		}
		for _, operation := range binding.OperationReferences {
			_, exists := operations[binding.Interface][operation]
			if operation.Input == "" && operation.Output == "" {
				_, exists = operationNames[binding.Interface][operation.Name]
			}
			if !exists {
				return fmt.Errorf(
					"%w: binding operation {%s}%s#%s|%s|%s",
					ErrUnresolvedComponent,
					binding.Interface.Namespace,
					binding.Interface.Local,
					operation.Name,
					operation.Input,
					operation.Output,
				)
			}
		}
	}
	for _, service := range set.services {
		if service.Interface.Local != "" {
			if _, exists := interfaces[service.Interface]; !exists {
				return fmt.Errorf("%w: service interface {%s}%s", ErrUnresolvedComponent, service.Interface.Namespace, service.Interface.Local)
			}
		}
		for _, endpoint := range service.Endpoints {
			if _, exists := bindings[endpoint.Binding]; !exists {
				return fmt.Errorf("%w: endpoint binding {%s}%s", ErrUnresolvedComponent, endpoint.Binding.Namespace, endpoint.Binding.Local)
			}
		}
	}
	return nil
}

func sortInterfaces(values []Interface) {
	sort.Slice(values, func(left, right int) bool { return lessQName(values[left].Name, values[right].Name) })
}

func sortOperations(values []Operation) {
	sort.Slice(values, func(left, right int) bool {
		leftIdentity := operationIdentity(values[left])
		rightIdentity := operationIdentity(values[right])
		if leftIdentity.Name != rightIdentity.Name {
			return cmp.Compare(leftIdentity.Name, rightIdentity.Name) == -1
		}
		if leftIdentity.Input != rightIdentity.Input {
			return cmp.Compare(leftIdentity.Input, rightIdentity.Input) == -1
		}
		return cmp.Compare(leftIdentity.Output, rightIdentity.Output) == -1
	})
}

func operationIdentity(value Operation) OperationReference {
	result := OperationReference{Name: value.Name}
	if value.Input != nil {
		result.Input = value.Input.Label
	}
	if value.Output != nil {
		result.Output = value.Output.Label
	}
	return result
}

func sortOperationReferences(values []OperationReference) {
	sort.Slice(values, func(left, right int) bool {
		if values[left].Name != values[right].Name {
			return cmp.Compare(values[left].Name, values[right].Name) == -1
		}
		if values[left].Input != values[right].Input {
			return cmp.Compare(values[left].Input, values[right].Input) == -1
		}
		return cmp.Compare(values[left].Output, values[right].Output) == -1
	})
}

func sortEndpoints(values []Endpoint) {
	sort.Slice(values, func(left, right int) bool {
		return cmp.Compare(values[left].Name, values[right].Name) == -1
	})
}

func sortBindings(values []Binding) {
	sort.Slice(values, func(left, right int) bool { return lessQName(values[left].Name, values[right].Name) })
}

func sortServices(values []Service) {
	sort.Slice(values, func(left, right int) bool { return lessQName(values[left].Name, values[right].Name) })
}

func lessQName(left, right wsdl.QName) bool {
	if left.Namespace != right.Namespace {
		return cmp.Compare(left.Namespace, right.Namespace) == -1
	}
	return cmp.Compare(left.Local, right.Local) == -1
}
