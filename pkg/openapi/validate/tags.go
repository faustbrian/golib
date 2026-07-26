package validate

import (
	"strconv"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type tagDefinition struct {
	name    string
	parent  string
	pointer string
}

func validateTags(document openapi.Document) []Diagnostic {
	tags, exists := document.Raw().Lookup("tags")
	if !exists || tags.Kind() != jsonvalue.ArrayKind {
		return nil
	}
	version := document.SpecificationVersion().String()
	elements, _ := tags.Elements()
	definitions := make(map[string]tagDefinition)
	ordered := make([]tagDefinition, 0, len(elements))
	var diagnostics []Diagnostic
	for index, element := range elements {
		if element.Kind() != jsonvalue.ObjectKind {
			continue
		}
		name, ok := stringMember(element, "name")
		if !ok {
			continue
		}
		pointer := "/tags/" + strconv.Itoa(index)
		if prior, duplicate := definitions[name]; duplicate {
			diagnostics = append(diagnostics, tagDiagnostic(
				version,
				"openapi.tag.name.duplicate",
				pointer+"/name",
				"tag name is already used at "+safeValue(prior.pointer+"/name"),
			))
			continue
		}
		parent, _ := stringMember(element, "parent")
		definition := tagDefinition{name: name, parent: parent, pointer: pointer}
		definitions[name] = definition
		ordered = append(ordered, definition)
	}
	if document.SpecificationVersion().Dialect() != specversion.DialectOAS32 {
		return diagnostics
	}
	for _, definition := range ordered {
		if definition.parent == "" {
			continue
		}
		if _, exists := definitions[definition.parent]; !exists {
			diagnostics = append(diagnostics, tagDiagnostic(
				version,
				"openapi.tag.parent.unknown",
				definition.pointer+"/parent",
				"tag parent does not name a declared tag",
			))
			continue
		}
		if tagParentCycle(definition.name, definitions) {
			diagnostics = append(diagnostics, tagDiagnostic(
				version,
				"openapi.tag.parent.cycle",
				definition.pointer+"/parent",
				"tag parent hierarchy contains a cycle",
			))
		}
	}
	return diagnostics
}

func tagParentCycle(name string, definitions map[string]tagDefinition) bool {
	seen := make(map[string]struct{}, len(definitions))
	current := name
	for current != "" {
		if _, duplicate := seen[current]; duplicate {
			return true
		}
		seen[current] = struct{}{}
		definition, exists := definitions[current]
		if !exists {
			return false
		}
		current = definition.parent
	}
	return false
}

func tagDiagnostic(
	version string,
	code string,
	pointer string,
	message string,
) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             SeverityError,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "tag-object",
	}
}
