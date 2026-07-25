package validate

import (
	"regexp"
	"strings"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func validateResponses(document openapi.Document) []Diagnostic {
	version := document.SpecificationVersion().String()
	dialect := document.SpecificationVersion().Dialect()
	var diagnostics []Diagnostic
	for _, operation := range documentOperations(document) {
		responses, exists := objectMember(operation.value, "responses")
		if !exists {
			continue
		}
		members, _ := responses.Members()
		for _, member := range members {
			switch member.Name {
			case "default":
				continue
			}
			if strings.HasPrefix(member.Name, "x-") {
				continue
			}
			location := operation.pointer + "/responses/" + escapePointer(member.Name)
			if !validResponseCode(member.Name, dialect) {
				diagnostics = append(diagnostics, Diagnostic{
					Code:                 "openapi.responses.code.invalid",
					Message:              "response key must be an HTTP status code, range, default, or extension",
					Severity:             SeverityError,
					Source:               SourceDocument,
					InstanceLocation:     location,
					SpecificationVersion: version,
					SpecificationSection: "responses-object",
				})
				continue
			}
			if recommendsRegisteredStatusCodes(version) &&
				!isRegisteredHTTPStatusCode(member.Name) &&
				member.Name[1:] != "XX" {
				diagnostics = append(diagnostics, Diagnostic{
					Code:                 "openapi.responses.code.unregistered",
					Message:              "response status code should be registered with IANA",
					Severity:             SeverityWarning,
					Source:               SourceDocument,
					InstanceLocation:     location,
					SpecificationVersion: version,
					SpecificationSection: "http-status-codes",
				})
			}
		}
		if !hasResponseCode(responses, dialect) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:                 "openapi.responses.code.missing",
				Message:              "responses must contain at least one response code or default response",
				Severity:             SeverityError,
				Source:               SourceDocument,
				InstanceLocation:     operation.pointer + "/responses",
				SpecificationVersion: version,
				SpecificationSection: "responses-object",
			})
			continue
		}
		if !hasSuccessfulResponse(responses, dialect) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:                 "openapi.responses.success.missing",
				Message:              "responses should contain a successful response code",
				Severity:             SeverityWarning,
				Source:               SourceDocument,
				InstanceLocation:     operation.pointer + "/responses",
				SpecificationVersion: version,
				SpecificationSection: "responses-object",
			})
		}
	}
	return diagnostics
}

func recommendsRegisteredStatusCodes(version string) bool {
	switch version {
	case "3.0.4", "3.1.1", "3.1.2", "3.2.0":
		return true
	default:
		return false
	}
}

// isRegisteredHTTPStatusCode reflects the IANA HTTP Status Code Registry
// snapshot pinned in specification/registries/iana/http-status-codes-1.csv.
func isRegisteredHTTPStatusCode(code string) bool {
	switch code {
	case "100", "101", "102", "103", "104",
		"200", "201", "202", "203", "204", "205", "206", "207", "208", "226",
		"300", "301", "302", "303", "304", "305", "306", "307", "308",
		"400", "401", "402", "403", "404", "405", "406", "407", "408", "409",
		"410", "411", "412", "413", "414", "415", "416", "417", "418", "421",
		"422", "423", "424", "425", "426", "428", "429", "431", "451",
		"500", "501", "502", "503", "504", "505", "506", "507", "508", "510", "511":
		return true
	default:
		return false
	}
}

func hasSuccessfulResponse(
	responses jsonvalue.Value,
	dialect specversion.Dialect,
) bool {
	members, _ := responses.Members()
	for _, member := range members {
		if successfulResponseCodePattern.MatchString(member.Name) {
			return true
		}
		switch dialect {
		case specversion.DialectOAS30, specversion.DialectOAS31, specversion.DialectOAS32:
			if member.Name == "2XX" {
				return true
			}
		}
	}
	return false
}

var (
	exactResponseCodePattern      = regexp.MustCompile(`^[1-5][0-9]{2}$`)
	rangeResponseCodePattern      = regexp.MustCompile(`^[1-5]XX$`)
	successfulResponseCodePattern = regexp.MustCompile(`^2[0-9]{2}$`)
)

func hasResponseCode(responses jsonvalue.Value, dialect specversion.Dialect) bool {
	members, _ := responses.Members()
	for _, member := range members {
		if member.Name == "default" || validResponseCode(member.Name, dialect) {
			return true
		}
	}
	return false
}

func validResponseCode(code string, dialect specversion.Dialect) bool {
	if exactResponseCodePattern.MatchString(code) {
		return true
	}
	switch dialect {
	case specversion.DialectOAS30, specversion.DialectOAS31, specversion.DialectOAS32:
		return rangeResponseCodePattern.MatchString(code)
	default:
		return false
	}
}
