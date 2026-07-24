package validate

import (
	"net/mail"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateMetadata(document openapi.Document) []Diagnostic {
	info, exists := objectMember(document.Raw(), "info")
	if !exists {
		return nil
	}
	version := document.SpecificationVersion().String()
	dialect := document.SpecificationVersion().Dialect()
	var diagnostics []Diagnostic
	if dialect != specversion.DialectSwagger20 {
		if terms, ok := stringMember(info, "termsOfService"); ok &&
			!validMetadataURL(terms, dialect) {
			diagnostics = append(diagnostics, metadataDiagnostic(
				version,
				"openapi.info.terms-of-service.invalid",
				"/info/termsOfService",
				"termsOfService must be a valid URI reference",
			))
		}
	}
	if contact, ok := objectMember(info, "contact"); ok {
		if target, exists := stringMember(contact, "url"); exists &&
			!validMetadataURL(target, dialect) {
			diagnostics = append(diagnostics, metadataDiagnostic(
				version,
				"openapi.contact.url.invalid",
				"/info/contact/url",
				"contact URL must satisfy its version's URI requirements",
			))
		}
		if email, exists := stringMember(contact, "email"); exists &&
			!validEmailAddress(email) {
			diagnostics = append(diagnostics, metadataDiagnostic(
				version,
				"openapi.contact.email.invalid",
				"/info/contact/email",
				"contact email must be an email address without a display name",
			))
		}
	}
	license, hasLicense := objectMember(info, "license")
	if !hasLicense {
		return diagnostics
	}
	if target, exists := stringMember(license, "url"); exists &&
		!validMetadataURL(target, dialect) {
		diagnostics = append(diagnostics, metadataDiagnostic(
			version,
			"openapi.license.url.invalid",
			"/info/license/url",
			"license URL must satisfy its version's URI requirements",
		))
	}
	if dialect == specversion.DialectOAS31 || dialect == specversion.DialectOAS32 {
		_, hasIdentifier := license.Lookup("identifier")
		_, hasURL := license.Lookup("url")
		if hasIdentifier && hasURL {
			diagnostics = append(diagnostics, metadataDiagnostic(
				version,
				"openapi.license.identifier-and-url",
				"/info/license",
				"license identifier and URL are mutually exclusive",
			))
		}
	}
	return diagnostics
}

func validMetadataURL(value string, dialect specversion.Dialect) bool {
	if dialect == specversion.DialectSwagger20 {
		return validAbsoluteURI(value)
	}
	return validURIReference(value)
}

func validEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value
}

func metadataDiagnostic(
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
		SpecificationSection: "info-object",
	}
}
