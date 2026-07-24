package validate

import (
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type serverArray struct {
	value   jsonvalue.Value
	pointer string
}

func validateServers(document openapi.Document) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	if dialect == specversion.DialectSwagger20 {
		return nil
	}
	version := document.SpecificationVersion().String()
	var diagnostics []Diagnostic
	for _, located := range serverArrays(document) {
		servers, _ := located.value.Elements()
		names := make(map[string]string)
		for index, server := range servers {
			if server.Kind() != jsonvalue.ObjectKind {
				continue
			}
			pointer := located.pointer + "/" + strconv.Itoa(index)
			if dialect == specversion.DialectOAS32 {
				if name, ok := stringMember(server, "name"); ok {
					if prior, duplicate := names[name]; duplicate {
						diagnostics = append(diagnostics, serverDiagnostic(
							version,
							SeverityError,
							"openapi.server.name.duplicate",
							pointer+"/name",
							"server name is already used at "+safeValue(prior),
						))
					} else {
						names[name] = pointer + "/name"
					}
				}
			}
			diagnostics = append(
				diagnostics,
				validateServer(server, pointer, version, dialect)...,
			)
		}
	}
	return diagnostics
}

func serverArrays(document openapi.Document) []serverArray {
	root := document.Raw()
	var arrays []serverArray
	if servers, exists := root.Lookup("servers"); exists &&
		servers.Kind() == jsonvalue.ArrayKind {
		arrays = append(arrays, serverArray{value: servers, pointer: "/servers"})
	}
	for _, pathItem := range documentPathItems(document) {
		if servers, exists := pathItem.value.Lookup("servers"); exists &&
			servers.Kind() == jsonvalue.ArrayKind {
			arrays = append(arrays, serverArray{
				value: servers, pointer: pathItem.pointer + "/servers",
			})
		}
	}
	for _, operation := range documentOperations(document) {
		if servers, exists := operation.value.Lookup("servers"); exists &&
			servers.Kind() == jsonvalue.ArrayKind {
			arrays = append(arrays, serverArray{
				value: servers, pointer: operation.pointer + "/servers",
			})
		}
	}
	return arrays
}

func validateServer(
	server jsonvalue.Value,
	pointer string,
	version string,
	dialect specversion.Dialect,
) []Diagnostic {
	url, ok := stringMember(server, "url")
	if !ok {
		return nil
	}
	var diagnostics []Diagnostic
	variables, valid := serverTemplateVariables(url)
	switch dialect {
	case specversion.DialectOAS32:
		valid = valid && validServerURLTemplate32(url)
	}
	if !valid {
		diagnostics = append(diagnostics, serverDiagnostic(
			version,
			SeverityError,
			"openapi.server.url.invalid-template",
			pointer+"/url",
			"server URL contains malformed variable braces",
		))
		return diagnostics
	}
	switch dialect {
	case specversion.DialectOAS31, specversion.DialectOAS32:
		if serverURLHasQueryOrFragment(url) {
			diagnostics = append(diagnostics, serverDiagnostic(
				version,
				SeverityError,
				"openapi.server.url.query-or-fragment",
				pointer+"/url",
				"server URL must not contain a query or fragment",
			))
		}
	}
	declared := make(map[string]jsonvalue.Value)
	var declaredNames []string
	variableMap, hasVariables := server.Lookup("variables")
	if hasVariables && variableMap.Kind() == jsonvalue.ObjectKind {
		members, _ := variableMap.Members()
		for _, member := range members {
			declared[member.Name] = member.Value
			declaredNames = append(declaredNames, member.Name)
		}
	}
	used := make(map[string]int)
	for _, name := range variables {
		used[name]++
		switch dialect {
		case specversion.DialectOAS32:
			if used[name] > 1 {
				diagnostics = append(diagnostics, serverDiagnostic(
					version,
					SeverityError,
					"openapi.server.variable.duplicate",
					pointer+"/url",
					"server variable appears more than once: "+safeValue(name),
				))
			}
		}
		if _, exists := declared[name]; !exists && used[name] == 1 {
			diagnostics = append(diagnostics, serverDiagnostic(
				version,
				SeverityError,
				"openapi.server.variable.missing",
				pointer+"/variables",
				"server URL variable is not declared: "+safeValue(name),
			))
		}
	}
	for _, name := range declaredNames {
		variable := declared[name]
		variablePointer := pointer + "/variables/" + escapePointer(name)
		if _, exists := used[name]; !exists {
			diagnostics = append(diagnostics, serverDiagnostic(
				version,
				SeverityWarning,
				"openapi.server.variable.unused",
				variablePointer,
				"server variable is absent from the URL template",
			))
		}
		diagnostics = append(
			diagnostics,
			validateServerVariable(variable, variablePointer, version, dialect)...,
		)
	}
	return diagnostics
}

func validateServerVariable(
	variable jsonvalue.Value,
	pointer string,
	version string,
	dialect specversion.Dialect,
) []Diagnostic {
	if variable.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	enumeration, hasEnumeration := variable.Lookup("enum")
	if !hasEnumeration || enumeration.Kind() != jsonvalue.ArrayKind {
		return nil
	}
	severity := SeverityError
	if dialect == specversion.DialectOAS30 {
		severity = SeverityWarning
	}
	elements, _ := enumeration.Elements()
	var diagnostics []Diagnostic
	if len(elements) == 0 {
		diagnostics = append(diagnostics, serverDiagnostic(
			version,
			severity,
			"openapi.server.variable.empty-enum",
			pointer+"/enum",
			"server variable enum must not be empty",
		))
	}
	defaultValue, hasDefault := stringMember(variable, "default")
	if !hasDefault {
		return diagnostics
	}
	for _, element := range elements {
		if value, ok := element.Text(); ok && value == defaultValue {
			return diagnostics
		}
	}
	return append(diagnostics, serverDiagnostic(
		version,
		severity,
		"openapi.server.variable.default-not-in-enum",
		pointer+"/default",
		"server variable default is absent from its enum",
	))
}

func serverTemplateVariables(value string) ([]string, bool) {
	var variables []string
	for index := 0; index < len(value); {
		switch value[index] {
		case '}':
			return nil, false
		case '{':
			closing := strings.IndexByte(value[index+1:], '}')
			if closing == -1 {
				return nil, false
			}
			closing += index + 1
			name := value[index+1 : closing]
			if name == "" || strings.ContainsAny(name, "{}") {
				return nil, false
			}
			variables = append(variables, name)
			index = closing + 1
		default:
			index++
		}
	}
	return variables, true
}

func serverURLHasQueryOrFragment(value string) bool {
	insideVariable := false
	for _, character := range value {
		switch character {
		case '{':
			insideVariable = true
		case '}':
			insideVariable = false
		case '?', '#':
			if !insideVariable {
				return true
			}
		}
	}
	return false
}

func validServerURLTemplate32(value string) bool {
	if value == "" {
		return false
	}
	insideVariable := false
	for index, character := range value {
		switch character {
		case '{':
			insideVariable = true
			continue
		case '}':
			insideVariable = false
			continue
		}
		if insideVariable {
			continue
		}
		if character == '%' {
			if index+2 >= len(value) || !hexDigit(value[index+1]) || !hexDigit(value[index+2]) {
				return false
			}
			continue
		}
		if !validServerLiteralRune(character) {
			return false
		}
	}
	return !insideVariable
}

type serverRuneInterval struct {
	first rune
	last  rune
}

var serverLiteralRuneIntervals = [...]serverRuneInterval{
	{0x21, 0x21}, {0x23, 0x24}, {0x26, 0x3b}, {0x3d, 0x3d},
	{0x3f, 0x5b}, {0x5d, 0x5d}, {0x5f, 0x5f}, {0x61, 0x7a},
	{0x7e, 0x7e}, {0xa0, 0xd7ff}, {0xe000, 0xf8ff}, {0xf900, 0xfdcf},
	{0xfdf0, 0xffef}, {0x10000, 0x1fffd}, {0x20000, 0x2fffd},
	{0x30000, 0x3fffd}, {0x40000, 0x4fffd}, {0x50000, 0x5fffd},
	{0x60000, 0x6fffd}, {0x70000, 0x7fffd}, {0x80000, 0x8fffd},
	{0x90000, 0x9fffd}, {0xa0000, 0xafffd}, {0xb0000, 0xbfffd},
	{0xc0000, 0xcfffd}, {0xd0000, 0xdfffd}, {0xe1000, 0xefffd},
	{0xf0000, 0xffffd}, {0x100000, 0x10fffd},
}

func validServerLiteralRune(character rune) bool {
	for _, interval := range serverLiteralRuneIntervals {
		if character < interval.first {
			return false
		}
		if character <= interval.last {
			return true
		}
	}
	return false
}

func hexDigit(value byte) bool {
	return value >= '0' && value <= '9' ||
		value >= 'a' && value <= 'f' ||
		value >= 'A' && value <= 'F'
}

func serverDiagnostic(
	version string,
	severity Severity,
	code string,
	pointer string,
	message string,
) Diagnostic {
	return Diagnostic{
		Code:                 code,
		Message:              message,
		Severity:             severity,
		Source:               SourceDocument,
		InstanceLocation:     pointer,
		SpecificationVersion: version,
		SpecificationSection: "server-object",
	}
}
