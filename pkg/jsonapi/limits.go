package jsonapi

import "fmt"

const (
	// DefaultMaxDocumentBytes bounds one encoded JSON:API document at 16 MiB.
	DefaultMaxDocumentBytes = 16 << 20
	// DefaultMaxNestingDepth bounds nested JSON arrays and objects.
	DefaultMaxNestingDepth = 64
	// DefaultMaxObjectMembers bounds members in any one JSON object.
	DefaultMaxObjectMembers = 10_000
	// DefaultMaxArrayItems bounds items in any one JSON array.
	DefaultMaxArrayItems = 100_000
	// DefaultMaxTotalValues bounds total JSON values visited during decoding.
	DefaultMaxTotalValues = 1_000_000
	// DefaultMaxQueryParameters bounds distinct decoded query names.
	DefaultMaxQueryParameters = 100
	// DefaultMaxQueryValues bounds decoded query values across all names.
	DefaultMaxQueryValues = 200
	// DefaultMaxQueryNameBytes bounds one decoded query parameter name.
	DefaultMaxQueryNameBytes = 1_024
	// DefaultMaxQueryValueBytes bounds one decoded query parameter value.
	DefaultMaxQueryValueBytes = 8_192
	// DefaultMaxQueryTotalBytes bounds decoded names and values in aggregate.
	DefaultMaxQueryTotalBytes = 64 << 10
	// DefaultMaxQuerySelectors bounds bracket selectors in one name.
	DefaultMaxQuerySelectors = 32
	// DefaultMaxQueryListItems bounds include, fields, or sort list entries.
	DefaultMaxQueryListItems = 1_000
	// DefaultMaxNegotiationHeaderBytes bounds one media type header value.
	DefaultMaxNegotiationHeaderBytes = 32 << 10
	// DefaultMaxAcceptCandidates bounds comma-separated Accept candidates.
	DefaultMaxAcceptCandidates = 100
	// DefaultMaxParameterURIs bounds URIs in one ext or profile parameter.
	DefaultMaxParameterURIs = 100
	// DefaultMaxNegotiationURIBytes bounds one extension or profile URI.
	DefaultMaxNegotiationURIBytes = 2_048
	// DefaultMaxSupportedURIs bounds configured extensions and profiles.
	DefaultMaxSupportedURIs = 1_000
)

// DecodeLimits bounds resource use before semantic document decoding. Zero
// fields use the production defaults; negative fields are invalid.
type DecodeLimits struct {
	MaxDocumentBytes int
	MaxNestingDepth  int
	MaxObjectMembers int
	MaxArrayItems    int
	MaxTotalValues   int
}

// NegotiationLimits bounds media type configuration and header processing.
type NegotiationLimits struct {
	MaxHeaderBytes      int
	MaxAcceptCandidates int
	MaxParameterURIs    int
	MaxURIBytes         int
	MaxSupportedURIs    int
}

// DefaultNegotiationLimits returns bounded media type defaults.
func DefaultNegotiationLimits() NegotiationLimits {
	return NegotiationLimits{
		MaxHeaderBytes:      DefaultMaxNegotiationHeaderBytes,
		MaxAcceptCandidates: DefaultMaxAcceptCandidates,
		MaxParameterURIs:    DefaultMaxParameterURIs,
		MaxURIBytes:         DefaultMaxNegotiationURIBytes,
		MaxSupportedURIs:    DefaultMaxSupportedURIs,
	}
}

// QueryLimits bounds work performed on decoded URL query values. The HTTP
// layer remains responsible for limiting the encoded request-target length.
type QueryLimits struct {
	MaxParameters int
	MaxValues     int
	MaxNameBytes  int
	MaxValueBytes int
	MaxTotalBytes int
	MaxSelectors  int
	MaxListItems  int
}

// DefaultQueryLimits returns the package's bounded query defaults.
func DefaultQueryLimits() QueryLimits {
	return QueryLimits{
		MaxParameters: DefaultMaxQueryParameters,
		MaxValues:     DefaultMaxQueryValues,
		MaxNameBytes:  DefaultMaxQueryNameBytes,
		MaxValueBytes: DefaultMaxQueryValueBytes,
		MaxTotalBytes: DefaultMaxQueryTotalBytes,
		MaxSelectors:  DefaultMaxQuerySelectors,
		MaxListItems:  DefaultMaxQueryListItems,
	}
}

// DefaultDecodeLimits returns the package's bounded production defaults.
func DefaultDecodeLimits() DecodeLimits {
	return DecodeLimits{
		MaxDocumentBytes: DefaultMaxDocumentBytes,
		MaxNestingDepth:  DefaultMaxNestingDepth,
		MaxObjectMembers: DefaultMaxObjectMembers,
		MaxArrayItems:    DefaultMaxArrayItems,
		MaxTotalValues:   DefaultMaxTotalValues,
	}
}

func normalizeDecodeLimits(limits DecodeLimits) (DecodeLimits, error) {
	defaults := DefaultDecodeLimits()
	fields := []struct {
		name       string
		value      *int
		defaultVal int
	}{
		{"MaxDocumentBytes", &limits.MaxDocumentBytes, defaults.MaxDocumentBytes},
		{"MaxNestingDepth", &limits.MaxNestingDepth, defaults.MaxNestingDepth},
		{"MaxObjectMembers", &limits.MaxObjectMembers, defaults.MaxObjectMembers},
		{"MaxArrayItems", &limits.MaxArrayItems, defaults.MaxArrayItems},
		{"MaxTotalValues", &limits.MaxTotalValues, defaults.MaxTotalValues},
	}
	for _, field := range fields {
		if *field.value < 0 {
			return DecodeLimits{}, fmt.Errorf("decode limit %s must not be negative", field.name)
		}
		if *field.value == 0 {
			*field.value = field.defaultVal
		}
	}
	return limits, nil
}

func normalizeQueryLimits(limits QueryLimits) (QueryLimits, error) {
	defaults := DefaultQueryLimits()
	fields := []struct {
		name       string
		value      *int
		defaultVal int
	}{
		{"MaxParameters", &limits.MaxParameters, defaults.MaxParameters},
		{"MaxValues", &limits.MaxValues, defaults.MaxValues},
		{"MaxNameBytes", &limits.MaxNameBytes, defaults.MaxNameBytes},
		{"MaxValueBytes", &limits.MaxValueBytes, defaults.MaxValueBytes},
		{"MaxTotalBytes", &limits.MaxTotalBytes, defaults.MaxTotalBytes},
		{"MaxSelectors", &limits.MaxSelectors, defaults.MaxSelectors},
		{"MaxListItems", &limits.MaxListItems, defaults.MaxListItems},
	}
	for _, field := range fields {
		if *field.value < 0 {
			return QueryLimits{}, fmt.Errorf("query limit %s must not be negative", field.name)
		}
		if *field.value == 0 {
			*field.value = field.defaultVal
		}
	}
	return limits, nil
}

func normalizeNegotiationLimits(limits NegotiationLimits) (NegotiationLimits, error) {
	defaults := DefaultNegotiationLimits()
	fields := []struct {
		name       string
		value      *int
		defaultVal int
	}{
		{"MaxHeaderBytes", &limits.MaxHeaderBytes, defaults.MaxHeaderBytes},
		{"MaxAcceptCandidates", &limits.MaxAcceptCandidates, defaults.MaxAcceptCandidates},
		{"MaxParameterURIs", &limits.MaxParameterURIs, defaults.MaxParameterURIs},
		{"MaxURIBytes", &limits.MaxURIBytes, defaults.MaxURIBytes},
		{"MaxSupportedURIs", &limits.MaxSupportedURIs, defaults.MaxSupportedURIs},
	}
	for _, field := range fields {
		if *field.value < 0 {
			return NegotiationLimits{}, fmt.Errorf(
				"negotiation limit %s must not be negative",
				field.name,
			)
		}
		if *field.value == 0 {
			*field.value = field.defaultVal
		}
	}
	return limits, nil
}
