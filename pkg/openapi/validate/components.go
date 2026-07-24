package validate

import (
	"regexp"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var componentNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

var commonComponentRegistries = []string{
	"schemas",
	"responses",
	"parameters",
	"examples",
	"requestBodies",
	"headers",
	"securitySchemes",
	"links",
	"callbacks",
}

func validateComponentNames(document openapi.Document) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	switch dialect {
	case specversion.DialectSwagger20:
		return nil
	}
	components, exists := objectMember(document.Raw(), "components")
	if !exists {
		return nil
	}
	registries := append([]string(nil), commonComponentRegistries...)
	switch dialect {
	case specversion.DialectOAS31:
		registries = append(registries, "pathItems")
	case specversion.DialectOAS32:
		registries = append(registries, "pathItems", "mediaTypes")
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, registryName := range registries {
		switch dialect {
		case specversion.DialectOAS32:
			if registryName == "securitySchemes" {
				continue
			}
		}
		registry, ok := objectMember(components, registryName)
		if !ok {
			continue
		}
		members, _ := registry.Members()
		for _, member := range members {
			if validComponentName(member.Name) {
				continue
			}
			diagnostics = append(diagnostics, Diagnostic{
				Code:                 "openapi.component.name.invalid",
				Message:              "component name may contain only ASCII letters, digits, period, hyphen, or underscore",
				Severity:             SeverityError,
				Source:               SourceDocument,
				InstanceLocation:     "/components/" + registryName + "/" + escapePointer(member.Name),
				SpecificationVersion: version,
				SpecificationSection: "components-object",
			})
		}
	}
	return diagnostics
}

func validComponentName(name string) bool {
	return componentNamePattern.MatchString(name)
}
