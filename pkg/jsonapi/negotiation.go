package jsonapi

import (
	"errors"
	"fmt"
	"mime"
	"sort"
	"strings"
)

// MediaTypeJSONAPI is the registered JSON:API media type.
const MediaTypeJSONAPI = "application/vnd.api+json"

// MediaType describes extensions and profiles applied to a JSON:API payload.
type MediaType struct {
	Extensions []string
	Profiles   []string
}

// String returns the canonical JSON:API Content-Type value.
func (mediaType MediaType) String() string {
	parameters := make(map[string]string, 2)
	if len(mediaType.Extensions) > 0 {
		parameters["ext"] = strings.Join(uniqueSorted(mediaType.Extensions), " ")
	}
	if len(mediaType.Profiles) > 0 {
		parameters["profile"] = strings.Join(uniqueSorted(mediaType.Profiles), " ")
	}

	return mime.FormatMediaType(MediaTypeJSONAPI, parameters)
}

// NegotiatedMedia is the representation selected for an Accept header.
type NegotiatedMedia struct {
	MediaType   MediaType
	ContentType string
	VaryAccept  bool
}

// NegotiationError describes an HTTP content-negotiation failure without
// coupling the package to a particular HTTP framework.
type NegotiationError struct {
	Status  int
	Code    string
	Message string
}

// Error implements error.
func (err *NegotiationError) Error() string {
	return fmt.Sprintf("JSON:API negotiation failed (%d %s): %s", err.Status, err.Code, err.Message)
}

// Negotiator validates JSON:API request content types and selects response
// media types from Accept headers.
type Negotiator struct {
	extensions map[string]struct{}
	profiles   map[string]struct{}
	limits     NegotiationLimits
}

// NewNegotiator constructs a negotiator from supported extension and profile
// URIs. Invalid or duplicate configuration is rejected before serving traffic.
func NewNegotiator(extensions, profiles []string) (*Negotiator, error) {
	return NewNegotiatorWithLimits(extensions, profiles, NegotiationLimits{})
}

// NewNegotiatorWithLimits constructs a negotiator with explicit configuration
// and header-processing limits. Zero fields use production defaults.
func NewNegotiatorWithLimits(
	extensions, profiles []string,
	limits NegotiationLimits,
) (*Negotiator, error) {
	limits, err := normalizeNegotiationLimits(limits)
	if err != nil {
		return nil, err
	}
	if len(extensions)+len(profiles) > limits.MaxSupportedURIs {
		return nil, negotiationFailure(0, "limit", "supported URI configuration exceeds the limit")
	}
	for _, values := range [][]string{extensions, profiles} {
		for _, value := range values {
			if len(value) > limits.MaxURIBytes {
				return nil, negotiationFailure(0, "limit", "a supported URI exceeds the byte limit")
			}
		}
	}
	negotiator := &Negotiator{
		extensions: make(map[string]struct{}, len(extensions)),
		profiles:   make(map[string]struct{}, len(profiles)),
		limits:     limits,
	}
	if err := addSupportedURIs(negotiator.extensions, extensions, "extension"); err != nil {
		return nil, err
	}
	if err := addSupportedURIs(negotiator.profiles, profiles, "profile"); err != nil {
		return nil, err
	}

	return negotiator, nil
}

// CheckContentType validates the Content-Type of a JSON:API request payload.
// Unknown profiles are retained because profiles cannot change specification
// semantics; unsupported extensions fail with status 415.
func (negotiator *Negotiator) CheckContentType(header string) (MediaType, error) {
	if len(header) > negotiator.limits.MaxHeaderBytes {
		return MediaType{}, negotiationFailure(415, "limit", "Content-Type exceeds the byte limit")
	}
	if strings.TrimSpace(header) == "" {
		return MediaType{}, negotiationFailure(415, "unsupported-media-type", "Content-Type is required")
	}

	mediaName, parameters, err := mime.ParseMediaType(header)
	if err != nil || !strings.EqualFold(mediaName, MediaTypeJSONAPI) {
		return MediaType{}, negotiationFailure(415, "unsupported-media-type", "Content-Type must be application/vnd.api+json")
	}
	if unknown := unknownParameter(parameters, false); unknown != "" {
		return MediaType{}, negotiationFailure(415, "unsupported-parameter", "unsupported media type parameter: "+unknown)
	}

	mediaType, err := parseMediaTypeParameters(parameters, negotiator.limits)
	if err != nil {
		var limitErr *negotiationParameterLimitError
		if errors.As(err, &limitErr) {
			return MediaType{}, negotiationFailure(415, "limit", limitErr.Error())
		}
		return MediaType{}, negotiationFailure(415, "invalid-parameter", err.Error())
	}
	for _, extension := range mediaType.Extensions {
		if _, supported := negotiator.extensions[extension]; !supported {
			return MediaType{}, negotiationFailure(415, "unsupported-extension", "an unsupported extension was requested")
		}
	}

	return mediaType, nil
}

// NegotiateAccept selects a JSON:API response representation from an Accept
// header. Invalid candidates and candidates with unsupported extensions are
// ignored, while unknown profiles are ignored within otherwise valid choices.
func (negotiator *Negotiator) NegotiateAccept(header string) (NegotiatedMedia, error) {
	vary := len(negotiator.extensions) > 0 || len(negotiator.profiles) > 0
	if len(header) > negotiator.limits.MaxHeaderBytes {
		return NegotiatedMedia{}, negotiationFailure(406, "limit", "Accept exceeds the byte limit")
	}
	if strings.TrimSpace(header) == "" {
		return negotiated(MediaType{}, vary), nil
	}

	candidates := splitHeaderValues(header)
	if len(candidates) > negotiator.limits.MaxAcceptCandidates {
		return NegotiatedMedia{}, negotiationFailure(406, "limit", "Accept exceeds the candidate limit")
	}
	ranges := make([]acceptCandidate, 0, len(candidates))
	representations := make(map[string]acceptCandidate)
	for _, raw := range candidates {
		candidate, ok := negotiator.acceptCandidate(raw)
		if !ok {
			continue
		}
		ranges = append(ranges, candidate)
		if _, exists := representations[candidate.contentType]; !exists {
			representations[candidate.contentType] = candidate
		}
	}
	var selected *acceptCandidate
	for _, representation := range representations {
		quality, matched := qualityForRepresentation(representation, ranges)
		if !matched || quality == 0 {
			continue
		}
		representation.quality = quality
		if selected == nil || representation.quality > selected.quality ||
			representation.quality == selected.quality &&
				representation.contentType < selected.contentType {
			copy := representation
			selected = &copy
		}
	}
	if selected == nil {
		return NegotiatedMedia{}, negotiationFailure(406, "not-acceptable", "no acceptable JSON:API representation")
	}

	return negotiated(selected.mediaType, vary), nil
}

type acceptCandidate struct {
	mediaType   MediaType
	contentType string
	quality     float64
	specificity int
	matchesAll  bool
}

func (negotiator *Negotiator) acceptCandidate(raw string) (acceptCandidate, bool) {
	mediaName, parameters, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err != nil {
		return acceptCandidate{}, false
	}

	quality := 1.0
	if rawQuality, exists := parameters["q"]; exists {
		quality, err = parseQuality(rawQuality)
		if err != nil {
			return acceptCandidate{}, false
		}
		delete(parameters, "q")
	}
	if mediaName == "*/*" || strings.EqualFold(mediaName, "application/*") {
		if len(parameters) != 0 {
			return acceptCandidate{}, false
		}
		mediaType := MediaType{}
		specificity := 0
		if mediaName != "*/*" {
			specificity = 1
		}
		return acceptCandidate{
			mediaType: mediaType, contentType: mediaType.String(), quality: quality,
			specificity: specificity, matchesAll: true,
		}, true
	}
	if !strings.EqualFold(mediaName, MediaTypeJSONAPI) {
		return acceptCandidate{}, false
	}
	if unknownParameter(parameters, false) != "" {
		return acceptCandidate{}, false
	}

	requested, err := parseMediaTypeParameters(parameters, negotiator.limits)
	if err != nil {
		return acceptCandidate{}, false
	}
	for _, extension := range requested.Extensions {
		if _, supported := negotiator.extensions[extension]; !supported {
			return acceptCandidate{}, false
		}
	}

	mediaType := MediaType{Extensions: requested.Extensions}
	for _, profile := range requested.Profiles {
		if _, supported := negotiator.profiles[profile]; supported {
			mediaType.Profiles = append(mediaType.Profiles, profile)
		}
	}
	mediaType.Extensions = uniqueSorted(mediaType.Extensions)
	mediaType.Profiles = uniqueSorted(mediaType.Profiles)
	parameterCount := 0
	if len(mediaType.Extensions) > 0 {
		parameterCount++
	}
	if len(mediaType.Profiles) > 0 {
		parameterCount++
	}

	return acceptCandidate{
		mediaType: mediaType, contentType: mediaType.String(), quality: quality,
		specificity: 2 + parameterCount, matchesAll: parameterCount == 0,
	}, true
}

func qualityForRepresentation(
	representation acceptCandidate,
	ranges []acceptCandidate,
) (float64, bool) {
	bestSpecificity := -1
	quality := 0.0
	matched := false
	for _, candidate := range ranges {
		if !candidate.matchesAll && candidate.contentType != representation.contentType {
			continue
		}
		if candidate.specificity > bestSpecificity {
			bestSpecificity = candidate.specificity
			quality = candidate.quality
			matched = true
			continue
		}
		if candidate.specificity == bestSpecificity && candidate.quality > quality {
			quality = candidate.quality
		}
	}
	return quality, matched
}

func parseQuality(value string) (float64, error) {
	if value == "0" {
		return 0, nil
	}
	if value == "1" {
		return 1, nil
	}
	if len(value) < 2 || len(value) > 5 || value[1] != '.' ||
		(value[0] != '0' && value[0] != '1') {
		return 0, fmt.Errorf("invalid HTTP quality value")
	}
	fraction := value[2:]
	quality := 0.0
	place := 0.1
	for _, digit := range fraction {
		if digit < '0' || digit > '9' || value[0] == '1' && digit != '0' {
			return 0, fmt.Errorf("invalid HTTP quality value")
		}
		quality += float64(digit-'0') * place
		place /= 10
	}
	if value[0] == '1' {
		return 1, nil
	}
	return quality, nil
}

func negotiated(mediaType MediaType, vary bool) NegotiatedMedia {
	return NegotiatedMedia{
		MediaType:   mediaType,
		ContentType: mediaType.String(),
		VaryAccept:  vary,
	}
}

func parseMediaTypeParameters(
	parameters map[string]string,
	limits NegotiationLimits,
) (MediaType, error) {
	var mediaType MediaType
	var err error
	if value, exists := parameters["ext"]; exists {
		mediaType.Extensions, err = parseURIList(value, "ext", limits)
		if err != nil {
			return MediaType{}, err
		}
	}
	if value, exists := parameters["profile"]; exists {
		mediaType.Profiles, err = parseURIList(value, "profile", limits)
		if err != nil {
			return MediaType{}, err
		}
	}

	return mediaType, nil
}

func parseURIList(value, parameter string, limits NegotiationLimits) ([]string, error) {
	items := strings.Split(value, " ")
	if len(items) > limits.MaxParameterURIs {
		return nil, &negotiationParameterLimitError{
			message: parameter + " parameter exceeds the URI count limit",
		}
	}
	if len(items) == 0 || len(items) == 1 && items[0] == "" {
		return nil, fmt.Errorf("%s parameter must contain at least one URI", parameter)
	}
	for _, item := range items {
		if item == "" {
			return nil, fmt.Errorf("%s parameter contains an empty URI", parameter)
		}
		if len(item) > limits.MaxURIBytes {
			return nil, &negotiationParameterLimitError{
				message: parameter + " parameter contains a URI over the byte limit",
			}
		}
		absolute, valid := parseURIReference(item)
		if !valid || !absolute {
			return nil, fmt.Errorf("%s parameter contains an invalid URI", parameter)
		}
	}

	return uniqueSorted(items), nil
}

type negotiationParameterLimitError struct {
	message string
}

func (err *negotiationParameterLimitError) Error() string {
	return err.message
}

func unknownParameter(parameters map[string]string, allowQuality bool) string {
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		if name == "ext" || name == "profile" || allowQuality && name == "q" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}

	return names[0]
}

func addSupportedURIs(target map[string]struct{}, values []string, kind string) error {
	for _, value := range values {
		absolute, valid := parseURIReference(value)
		if !valid || !absolute {
			return fmt.Errorf("invalid supported %s URI: %q", kind, value)
		}
		if _, exists := target[value]; exists {
			return fmt.Errorf("duplicate supported %s URI: %q", kind, value)
		}
		target[value] = struct{}{}
	}

	return nil
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)

	return result
}

func splitHeaderValues(header string) []string {
	var values []string
	start := 0
	quoted := false
	escaped := false
	for index, character := range header {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && quoted {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if character == ',' && !quoted {
			values = append(values, header[start:index])
			start = index + 1
		}
	}
	values = append(values, header[start:])

	return values
}

func negotiationFailure(status int, code, message string) *NegotiationError {
	return &NegotiationError{Status: status, Code: code, Message: message}
}
