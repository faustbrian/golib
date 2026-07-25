// Package compose merges same-version, same-namespace WSDL documents.
package compose

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"sort"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	xsd "github.com/faustbrian/golib/pkg/xsd"
)

var marshalSchema = xsd.Marshal

var (
	ErrEmpty     = errors.New("wsdl compose: no documents")
	ErrVersion   = errors.New("wsdl compose: version mismatch")
	ErrNamespace = errors.New("wsdl compose: namespace mismatch")
	ErrConflict  = errors.New("wsdl compose: component conflict")
)

// Conflict identifies one duplicated top-level WSDL component.
type Conflict struct {
	Kind string
	Name string
}

// ConflictError reports all deterministic composition conflicts.
type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	if e == nil || len(e.Conflicts) == 0 {
		return ErrConflict.Error()
	}
	return fmt.Sprintf(
		"%s: %s %q",
		ErrConflict,
		e.Conflicts[0].Kind,
		e.Conflicts[0].Name,
	)
}

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// Merge composes documents independently of input order.
func Merge(documents ...*wsdl.Document) (*wsdl.Document, error) {
	if len(documents) == 0 {
		return nil, ErrEmpty
	}
	for _, document := range documents {
		if document == nil {
			return nil, errors.New("wsdl compose: document is nil")
		}
	}
	version := documents[0].Version()
	namespace := documentNamespace(documents[0])
	for _, document := range documents[1:] {
		if document.Version() != version {
			return nil, fmt.Errorf(
				"%w: %s and %s", ErrVersion, version, document.Version(),
			)
		}
		if documentNamespace(document) != namespace {
			return nil, fmt.Errorf(
				"%w: %q and %q",
				ErrNamespace,
				namespace,
				documentNamespace(document),
			)
		}
	}
	if version == wsdl.Version11 {
		return merge11(documents)
	}
	if version == wsdl.Version20 {
		return merge20(documents)
	}
	return nil, fmt.Errorf("%w: unsupported version %q", ErrVersion, version)
}

func merge20(documents []*wsdl.Document) (*wsdl.Document, error) {
	result := wsdl.Description20{TargetNamespace: documentNamespace(documents[0])}
	interfaces := make(map[string]struct{})
	bindings := make(map[string]struct{})
	services := make(map[string]struct{})
	conflicts := make([]Conflict, 0)
	for _, document := range documents {
		value, _ := document.Description20()
		result.Documentation = selectDocumentation(result.Documentation, value.Documentation)
		result.Imports = append(result.Imports, value.Imports...)
		result.Includes = append(result.Includes, value.Includes...)
		mergeTypes20(&result, value.Types)
		result.ExtensionAttributes = append(
			result.ExtensionAttributes, value.ExtensionAttributes...,
		)
		result.Extensions = append(result.Extensions, value.Extensions...)
		for _, interfaceValue := range value.Interfaces {
			if duplicate(interfaces, interfaceValue.Name) {
				conflicts = append(conflicts, Conflict{Kind: "interface", Name: interfaceValue.Name})
				continue
			}
			result.Interfaces = append(result.Interfaces, interfaceValue)
		}
		for _, binding := range value.Bindings {
			if duplicate(bindings, binding.Name) {
				conflicts = append(conflicts, Conflict{Kind: "binding", Name: binding.Name})
				continue
			}
			result.Bindings = append(result.Bindings, binding)
		}
		for _, service := range value.Services {
			if duplicate(services, service.Name) {
				conflicts = append(conflicts, Conflict{Kind: "service", Name: service.Name})
				continue
			}
			result.Services = append(result.Services, service)
		}
	}
	if err := reportConflicts(conflicts); err != nil {
		return nil, err
	}
	sort.Slice(result.Interfaces, func(i, j int) bool {
		return cmp.Compare(result.Interfaces[i].Name, result.Interfaces[j].Name) == -1
	})
	sort.Slice(result.Bindings, func(i, j int) bool {
		return cmp.Compare(result.Bindings[i].Name, result.Bindings[j].Name) == -1
	})
	sort.Slice(result.Services, func(i, j int) bool {
		return cmp.Compare(result.Services[i].Name, result.Services[j].Name) == -1
	})
	result.Imports = uniqueImports20(result.Imports)
	result.Includes = uniqueIncludes20(result.Includes)
	sortTypes20(result.Types)
	sortRootExtensibility(&result.Extensibility)
	return wsdl.NewDocument20(result, wsdl.ValidationOptions{})
}

func merge11(documents []*wsdl.Document) (*wsdl.Document, error) {
	result := wsdl.Definitions11{TargetNamespace: documentNamespace(documents[0])}
	messages := make(map[string]struct{})
	portTypes := make(map[string]struct{})
	bindings := make(map[string]struct{})
	services := make(map[string]struct{})
	conflicts := make([]Conflict, 0)
	for _, document := range documents {
		value, _ := document.Definitions11()
		if result.Name == "" || value.Name != "" && cmp.Compare(value.Name, result.Name) == -1 {
			result.Name = value.Name
		}
		result.Documentation = selectDocumentation(result.Documentation, value.Documentation)
		result.Imports = append(result.Imports, value.Imports...)
		mergeTypes11(&result, value.Types)
		result.ExtensionAttributes = append(
			result.ExtensionAttributes, value.ExtensionAttributes...,
		)
		result.Extensions = append(result.Extensions, value.Extensions...)
		appendNamed11(value.Messages, messages, "message", &result.Messages, &conflicts)
		appendNamed11(value.PortTypes, portTypes, "port type", &result.PortTypes, &conflicts)
		appendNamed11(value.Bindings, bindings, "binding", &result.Bindings, &conflicts)
		appendNamed11(value.Services, services, "service", &result.Services, &conflicts)
	}
	if err := reportConflicts(conflicts); err != nil {
		return nil, err
	}
	sort.Slice(result.Messages, func(i, j int) bool {
		return cmp.Compare(result.Messages[i].Name, result.Messages[j].Name) == -1
	})
	sort.Slice(result.PortTypes, func(i, j int) bool {
		return cmp.Compare(result.PortTypes[i].Name, result.PortTypes[j].Name) == -1
	})
	sort.Slice(result.Bindings, func(i, j int) bool {
		return cmp.Compare(result.Bindings[i].Name, result.Bindings[j].Name) == -1
	})
	sort.Slice(result.Services, func(i, j int) bool {
		return cmp.Compare(result.Services[i].Name, result.Services[j].Name) == -1
	})
	result.Imports = uniqueImports11(result.Imports)
	sortTypes11(result.Types)
	root := wsdl.Extensibility{
		Extensions: result.Extensions, ExtensionAttributes: result.ExtensionAttributes,
	}
	sortRootExtensibility(&root)
	result.Extensions = root.Extensions
	result.ExtensionAttributes = root.ExtensionAttributes
	return wsdl.NewDocument11(result, wsdl.ValidationOptions{})
}

func appendNamed11[T any](
	values []T,
	names map[string]struct{},
	kind string,
	result *[]T,
	conflicts *[]Conflict,
) {
	for _, value := range values {
		name := componentName11(any(value))
		if duplicate(names, name) {
			*conflicts = append(*conflicts, Conflict{Kind: kind, Name: name})
			continue
		}
		*result = append(*result, value)
	}
}

func componentName11(value any) string {
	switch value := value.(type) {
	case wsdl.Message11:
		return value.Name
	case wsdl.PortType11:
		return value.Name
	case wsdl.Binding11:
		return value.Name
	case wsdl.Service11:
		return value.Name
	default:
		return ""
	}
}

func mergeTypes20(result *wsdl.Description20, value *wsdl.Types20) {
	if value == nil {
		return
	}
	if result.Types == nil {
		result.Types = &wsdl.Types20{}
	}
	result.Types.Imports = append(result.Types.Imports, value.Imports...)
	result.Types.Schemas = append(result.Types.Schemas, value.Schemas...)
	result.Types.ExtensionAttributes = append(
		result.Types.ExtensionAttributes, value.ExtensionAttributes...,
	)
	result.Types.Extensions = append(result.Types.Extensions, value.Extensions...)
}

func mergeTypes11(result *wsdl.Definitions11, value *wsdl.Types11) {
	if value == nil {
		return
	}
	if result.Types == nil {
		result.Types = &wsdl.Types11{}
	}
	result.Types.Schemas = append(result.Types.Schemas, value.Schemas...)
	result.Types.ExtensionAttributes = append(
		result.Types.ExtensionAttributes, value.ExtensionAttributes...,
	)
	result.Types.Extensions = append(result.Types.Extensions, value.Extensions...)
}

func uniqueImports20(values []wsdl.Import20) []wsdl.Import20 {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Namespace != values[j].Namespace {
			return cmp.Compare(values[i].Namespace, values[j].Namespace) == -1
		}
		return cmp.Compare(values[i].Location, values[j].Location) == -1
	})
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1].Namespace != value.Namespace ||
			result[len(result)-1].Location != value.Location {
			result = append(result, value)
		}
	}
	return result
}

func uniqueIncludes20(values []wsdl.Include20) []wsdl.Include20 {
	sort.Slice(values, func(i, j int) bool {
		return cmp.Compare(values[i].Location, values[j].Location) == -1
	})
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1].Location != value.Location {
			result = append(result, value)
		}
	}
	return result
}

func uniqueImports11(values []wsdl.Import11) []wsdl.Import11 {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Namespace != values[j].Namespace {
			return cmp.Compare(values[i].Namespace, values[j].Namespace) == -1
		}
		return cmp.Compare(values[i].Location, values[j].Location) == -1
	})
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1].Namespace != value.Namespace ||
			result[len(result)-1].Location != value.Location {
			result = append(result, value)
		}
	}
	return result
}

func sortRootExtensibility(value *wsdl.Extensibility) {
	sort.Slice(value.ExtensionAttributes, func(i, j int) bool {
		left, right := value.ExtensionAttributes[i].Name, value.ExtensionAttributes[j].Name
		if left.Namespace != right.Namespace {
			return cmp.Compare(left.Namespace, right.Namespace) == -1
		}
		return cmp.Compare(left.Local, right.Local) == -1
	})
	sort.Slice(value.Extensions, func(i, j int) bool {
		left, right := value.Extensions[i], value.Extensions[j]
		if left.Name.Namespace != right.Name.Namespace {
			return cmp.Compare(left.Name.Namespace, right.Name.Namespace) == -1
		}
		if left.Name.Local != right.Name.Local {
			return cmp.Compare(left.Name.Local, right.Name.Local) == -1
		}
		return bytes.Compare(left.XML, right.XML) == -1
	})
}

func sortTypes20(value *wsdl.Types20) {
	if value == nil {
		return
	}
	sort.Slice(value.Imports, func(i, j int) bool {
		if value.Imports[i].Namespace != value.Imports[j].Namespace {
			return cmp.Compare(value.Imports[i].Namespace, value.Imports[j].Namespace) == -1
		}
		return cmp.Compare(value.Imports[i].Location, value.Imports[j].Location) == -1
	})
	sortSchemas(value.Schemas)
	sortRootExtensibility(&value.Extensibility)
}

func sortTypes11(value *wsdl.Types11) {
	if value == nil {
		return
	}
	sortSchemas(value.Schemas)
	sortRootExtensibility(&value.Extensibility)
}

func sortSchemas(values []*xsd.Document) {
	sort.Slice(values, func(i, j int) bool {
		left, right := schemaKey(values[i]), schemaKey(values[j])
		return cmp.Compare(left, right) == -1
	})
}

func schemaKey(value *xsd.Document) string {
	if value == nil {
		return ""
	}
	payload, err := marshalSchema(value)
	if err != nil {
		return value.TargetNamespace + "\x00" + value.SystemID
	}
	return value.TargetNamespace + "\x00" + string(payload)
}

func selectDocumentation(left, right *wsdl.Documentation) *wsdl.Documentation {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	leftKey := left.Language + "\x00" + left.Content + "\x00" + left.Markup
	rightKey := right.Language + "\x00" + right.Content + "\x00" + right.Markup
	if cmp.Compare(rightKey, leftKey) == -1 {
		return right
	}
	return left
}

func reportConflicts(values []Conflict) error {
	if len(values) == 0 {
		return nil
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Kind != values[j].Kind {
			return cmp.Compare(values[i].Kind, values[j].Kind) == -1
		}
		return cmp.Compare(values[i].Name, values[j].Name) == -1
	})
	return &ConflictError{Conflicts: values}
}

func duplicate(names map[string]struct{}, name string) bool {
	if _, exists := names[name]; exists {
		return true
	}
	names[name] = struct{}{}
	return false
}

func documentNamespace(document *wsdl.Document) string {
	if value, ok := document.Definitions11(); ok {
		return value.TargetNamespace
	}
	value, _ := document.Description20()
	return value.TargetNamespace
}
