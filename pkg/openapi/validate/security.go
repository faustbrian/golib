package validate

import (
	"context"
	"strconv"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

type securityScheme struct {
	kind     string
	scopes   map[string]struct{}
	resolved bool
}

type securityArray struct {
	value   jsonvalue.Value
	pointer string
}

func validateSecurity(
	ctx context.Context,
	document openapi.Document,
	options Options,
) []Diagnostic {
	dialect := document.SpecificationVersion().Dialect()
	version := document.SpecificationVersion().String()
	resource := validationResource(document, options.ReferenceResourceURI)
	schemes := securitySchemes(
		ctx,
		resource,
		dialect,
		options.ReferenceResolver,
		options.ReferenceLimits,
	)
	diagnostics := validateSecuritySchemeURLs(document)
	for _, located := range securityArrays(document) {
		requirements, _ := located.value.Elements()
		for index, requirement := range requirements {
			if requirement.Kind() != jsonvalue.ObjectKind {
				continue
			}
			members, _ := requirement.Members()
			for _, member := range members {
				pointer := located.pointer + "/" + strconv.Itoa(index) + "/" +
					escapePointer(member.Name)
				scheme, exists := schemes[member.Name]
				if !exists {
					if dialect == specversion.DialectOAS32 {
						var valid bool
						scheme, valid = securitySchemeURI(
							ctx,
							resource,
							member.Name,
							dialect,
							options.ReferenceResolver,
							options.ReferenceLimits,
						)
						if !valid {
							diagnostics = append(diagnostics, securityDiagnostic(
								version,
								"openapi.security.scheme-uri.invalid",
								pointer,
								"security requirement name is not a valid security scheme URI",
							))
						}
						exists = valid
					} else {
						diagnostics = append(diagnostics, securityDiagnostic(
							version,
							"openapi.security.scheme.unknown",
							pointer,
							"security requirement does not match a declared scheme",
						))
					}
					if !exists {
						continue
					}
				}
				scopes, ok := member.Value.Elements()
				if !ok {
					continue
				}
				if !scheme.resolved {
					continue
				}
				if scheme.kind != "oauth2" && scheme.kind != "openIdConnect" {
					if len(scopes) > 0 &&
						(dialect == specversion.DialectSwagger20 || dialect == specversion.DialectOAS30) {
						diagnostics = append(diagnostics, securityDiagnostic(
							version,
							"openapi.security.roles.not-allowed",
							pointer,
							"this specification version requires an empty role list",
						))
					}
					continue
				}
				if scheme.kind != "oauth2" {
					continue
				}
				for scopeIndex, rawScope := range scopes {
					scope, ok := rawScope.Text()
					if !ok {
						continue
					}
					if _, exists := scheme.scopes[scope]; !exists {
						diagnostics = append(diagnostics, securityDiagnostic(
							version,
							"openapi.security.oauth-scope.unknown",
							pointer+"/"+strconv.Itoa(scopeIndex),
							"OAuth2 scope is not declared by the security scheme",
						))
					}
				}
			}
		}
	}
	return diagnostics
}

func securitySchemeURI(
	ctx context.Context,
	resource reference.Resource,
	identifier string,
	dialect specversion.Dialect,
	resolver reference.Resolver,
	limits reference.Limits,
) (securityScheme, bool) {
	if identifier == "" || !validURIReference(identifier) {
		return securityScheme{}, false
	}
	if identifier[0] != '#' && resolver == nil {
		return securityScheme{}, true
	}
	referenceValue, _ := jsonvalue.String(identifier)
	referenceObject, _ := jsonvalue.Object([]jsonvalue.Member{
		{Name: "$ref", Value: referenceValue},
	})
	target, resolved := resolveReferencedObjectWithPolicy(
		ctx, resource, referenceObject, resolver, limits,
	)
	if !resolved {
		return securityScheme{}, false
	}
	return securitySchemeValue(target, dialect)
}

func validateSecuritySchemeURLs(document openapi.Document) []Diagnostic {
	root := document.Raw()
	dialect := document.SpecificationVersion().Dialect()
	version := document.SpecificationVersion().String()
	definitions, pointer, exists := securitySchemeDefinitions(root, dialect)
	if !exists || definitions.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	severity := SeverityError
	if dialect == specversion.DialectSwagger20 {
		severity = SeverityWarning
	}
	var diagnostics []Diagnostic
	members, _ := definitions.Members()
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		schemePointer := pointer + "/" + escapePointer(member.Name)
		if dialect == specversion.DialectOAS32 &&
			securityComponentNameLooksLikeURI(member.Name) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:                 "openapi.security.component-name.uri-like",
				Message:              "security scheme component names that resemble URIs are discouraged",
				Severity:             SeverityWarning,
				Source:               SourceDocument,
				InstanceLocation:     schemePointer,
				SpecificationVersion: version,
				SpecificationSection: "security-requirement-object",
			})
		}
		kind, _ := stringMember(member.Value, "type")
		if dialect == specversion.DialectSwagger20 {
			if kind == "oauth2" {
				diagnostics = append(diagnostics, validateSecurityURLFields(
					member.Value,
					schemePointer,
					version,
					severity,
					"authorizationUrl",
					"tokenUrl",
				)...)
			}
			continue
		}
		if kind == "http" {
			scheme, exists := stringMember(member.Value, "scheme")
			if exists && !isRegisteredHTTPAuthenticationScheme(scheme) {
				diagnostics = append(diagnostics, Diagnostic{
					Code:                 "openapi.security.http-scheme.unregistered",
					Message:              "HTTP authentication scheme should be registered with IANA",
					Severity:             SeverityWarning,
					Source:               SourceDocument,
					InstanceLocation:     schemePointer + "/scheme",
					SpecificationVersion: version,
					SpecificationSection: "security-scheme-object",
				})
			}
		}
		if kind == "openIdConnect" {
			diagnostics = append(diagnostics, validateSecurityURLFields(
				member.Value,
				schemePointer,
				version,
				severity,
				"openIdConnectUrl",
			)...)
		}
		if kind != "oauth2" {
			continue
		}
		flows, hasFlows := objectMember(member.Value, "flows")
		if !hasFlows {
			continue
		}
		flowMembers, _ := flows.Members()
		for _, flow := range flowMembers {
			diagnostics = append(diagnostics, validateSecurityURLFields(
				flow.Value,
				schemePointer+"/flows/"+escapePointer(flow.Name),
				version,
				severity,
				"authorizationUrl",
				"deviceAuthorizationUrl",
				"tokenUrl",
				"refreshUrl",
			)...)
		}
	}
	return diagnostics
}

// isRegisteredHTTPAuthenticationScheme reflects the IANA HTTP Authentication
// Scheme Registry snapshot pinned in specification/registries/iana.
func isRegisteredHTTPAuthenticationScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "basic", "bearer", "concealed", "digest", "dpop", "gnap", "hoba",
		"mutual", "negotiate", "oauth", "privatetoken", "scram-sha-1",
		"scram-sha-256", "vapid":
		return true
	default:
		return false
	}
}

func securityComponentNameLooksLikeURI(name string) bool {
	return strings.ContainsAny(name, ":/?#") && validURIReference(name)
}

func securitySchemeDefinitions(
	root jsonvalue.Value,
	dialect specversion.Dialect,
) (jsonvalue.Value, string, bool) {
	if dialect == specversion.DialectSwagger20 {
		definitions, exists := root.Lookup("securityDefinitions")
		return definitions, "/securityDefinitions", exists
	}
	components, exists := objectMember(root, "components")
	if !exists {
		return jsonvalue.Value{}, "", false
	}
	definitions, exists := components.Lookup("securitySchemes")
	return definitions, "/components/securitySchemes", exists
}

func validateSecurityURLFields(
	container jsonvalue.Value,
	pointer string,
	version string,
	severity Severity,
	fields ...string,
) []Diagnostic {
	if container.Kind() != jsonvalue.ObjectKind {
		return nil
	}
	var diagnostics []Diagnostic
	for _, field := range fields {
		target, exists := stringMember(container, field)
		if !exists || validURIReference(target) {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:                 "openapi.security.url.invalid",
			Message:              field + " must be a valid URI reference",
			Severity:             severity,
			Source:               SourceDocument,
			InstanceLocation:     pointer + "/" + field,
			SpecificationVersion: version,
			SpecificationSection: "security-scheme-object",
		})
	}
	return diagnostics
}

func securitySchemes(
	ctx context.Context,
	resource reference.Resource,
	dialect specversion.Dialect,
	resolver reference.Resolver,
	limits reference.Limits,
) map[string]securityScheme {
	result := make(map[string]securityScheme)
	definitions, _, exists := securitySchemeDefinitions(resource.Root, dialect)
	if !exists || definitions.Kind() != jsonvalue.ObjectKind {
		return result
	}
	members, _ := definitions.Members()
	for _, member := range members {
		if member.Value.Kind() != jsonvalue.ObjectKind {
			continue
		}
		result[member.Name] = securityScheme{}
		resolved, ok := resolveReferencedObjectWithPolicy(
			ctx, resource, member.Value, resolver, limits,
		)
		if !ok {
			continue
		}
		scheme, ok := securitySchemeValue(resolved, dialect)
		if !ok {
			continue
		}
		result[member.Name] = scheme
	}
	return result
}

func securitySchemeValue(
	value jsonvalue.Value,
	dialect specversion.Dialect,
) (securityScheme, bool) {
	kind, ok := stringMember(value, "type")
	if !ok || !validSecuritySchemeType(kind, dialect) {
		return securityScheme{}, false
	}
	return securityScheme{
		kind: kind, scopes: oauthScopes(value, dialect), resolved: true,
	}, true
}

func validSecuritySchemeType(kind string, dialect specversion.Dialect) bool {
	switch kind {
	case "basic":
		return dialect == specversion.DialectSwagger20
	case "apiKey", "oauth2":
		return true
	case "http", "openIdConnect":
		return dialect != specversion.DialectSwagger20
	case "mutualTLS":
		return dialect == specversion.DialectOAS31 ||
			dialect == specversion.DialectOAS32
	default:
		return false
	}
}

func oauthScopes(
	scheme jsonvalue.Value,
	dialect specversion.Dialect,
) map[string]struct{} {
	result := make(map[string]struct{})
	if dialect == specversion.DialectSwagger20 {
		collectScopeNames(result, scheme)
		return result
	}
	flows, exists := scheme.Lookup("flows")
	if !exists || flows.Kind() != jsonvalue.ObjectKind {
		return result
	}
	members, _ := flows.Members()
	for _, flow := range members {
		collectScopeNames(result, flow.Value)
	}
	return result
}

func collectScopeNames(result map[string]struct{}, container jsonvalue.Value) {
	if container.Kind() != jsonvalue.ObjectKind {
		return
	}
	scopes, exists := container.Lookup("scopes")
	if !exists || scopes.Kind() != jsonvalue.ObjectKind {
		return
	}
	members, _ := scopes.Members()
	for _, member := range members {
		result[member.Name] = struct{}{}
	}
}

func securityArrays(document openapi.Document) []securityArray {
	root := document.Raw()
	var result []securityArray
	if security, exists := root.Lookup("security"); exists &&
		security.Kind() == jsonvalue.ArrayKind {
		result = append(result, securityArray{value: security, pointer: "/security"})
	}
	for _, operation := range documentOperations(document) {
		security, exists := operation.value.Lookup("security")
		if exists && security.Kind() == jsonvalue.ArrayKind {
			result = append(result, securityArray{
				value: security, pointer: operation.pointer + "/security",
			})
		}
	}
	return result
}

func securityDiagnostic(
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
		SpecificationSection: "security-requirement-object",
	}
}
