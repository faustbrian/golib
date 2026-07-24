package media

import (
	"errors"
	"fmt"
	"mime"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

var (
	// ErrInvalidPositionalEncoding reports invalid media-type values or bounds.
	ErrInvalidPositionalEncoding = errors.New("invalid positional encoding")
	// ErrPositionalEncodingLimit reports an item-count bound exhaustion.
	ErrPositionalEncodingLimit = errors.New("positional encoding limit exceeded")
	// ErrInvalidNamedEncoding reports invalid named-encoding inputs or bounds.
	ErrInvalidNamedEncoding = errors.New("invalid named encoding")
	// ErrNamedEncodingLimit reports an encoded-value bound exhaustion.
	ErrNamedEncodingLimit = errors.New("named encoding limit exceeded")
	// ErrInvalidMultipartName reports an invalid form-data part name or bound.
	ErrInvalidMultipartName = errors.New("invalid multipart form-data name")
	// ErrMultipartDispositionLimit reports an output-size bound exhaustion.
	ErrMultipartDispositionLimit = errors.New("multipart disposition limit exceeded")
	// ErrInvalidEncodingContentType reports invalid Encoding Object media types.
	ErrInvalidEncodingContentType = errors.New("invalid encoding content type")
	// ErrEncodingContentTypeSelection reports a missing or unmatched explicit
	// choice when an Encoding Object permits multiple media types.
	ErrEncodingContentTypeSelection = errors.New("encoding content type selection required")
	// ErrInvalidEncodingHeaders reports invalid Encoding Object header inputs.
	ErrInvalidEncodingHeaders = errors.New("invalid encoding headers")
	// ErrEncodingHeaderLimit reports an Encoding Object header-count bound.
	ErrEncodingHeaderLimit = errors.New("encoding header limit exceeded")
	// ErrInvalidEncodingSerialization reports invalid RFC6570-style Encoding
	// Object inputs.
	ErrInvalidEncodingSerialization = errors.New("invalid encoding serialization")
	// ErrEncodingSerializationLimit reports a serialized output bound.
	ErrEncodingSerializationLimit = errors.New("encoding serialization limit exceeded")
	// ErrInvalidFormURLEncoding reports invalid form fields or bounds.
	ErrInvalidFormURLEncoding = errors.New("invalid form URL encoding")
	// ErrFormURLEncodingLimit reports a form field or byte bound.
	ErrFormURLEncodingLimit = errors.New("form URL encoding limit exceeded")
	// ErrInvalidExternalExample reports invalid example bytes, media type, or
	// bounds.
	ErrInvalidExternalExample = errors.New("invalid external example text")
	// ErrExternalExampleLimit reports an external example byte bound.
	ErrExternalExampleLimit = errors.New("external example text limit exceeded")
	// ErrUnsupportedExternalExampleCharset reports an explicit character set
	// that this dependency-free text boundary cannot decode.
	ErrUnsupportedExternalExampleCharset = errors.New("unsupported external example charset")
)

// AppliedEncoding associates an immutable Encoding Object with an item index.
type AppliedEncoding struct {
	Index int
	Value jsonvalue.Value
}

// NamedEncodingValue associates one value with its form or multipart name and
// Encoding Object. ItemIndex is -1 for a top-level object.
type NamedEncodingValue struct {
	Name      string
	ItemIndex int
	Encoding  jsonvalue.Value
	Value     jsonvalue.Value
}

// EncodingHeader retains one applicable Header or Reference Object.
type EncodingHeader struct {
	Name  string
	Value jsonvalue.Value
}

// EncodingApplication is the bounded result of applying one Encoding Object.
// Nested fields are retained as immutable values for recursive application to
// the corresponding child data and schemas.
type EncodingApplication struct {
	Encoding             jsonvalue.Value
	ContentType          string
	Headers              []EncodingHeader
	Serialized           string
	SerializationApplied bool
	NamedEncoding        jsonvalue.Value
	PrefixEncoding       jsonvalue.Value
	ItemEncoding         jsonvalue.Value
}

// FormField is one already serialized name-value pair. Repeated names and
// caller order are preserved.
type FormField struct {
	Name  string
	Value string
}

// PositionalEncodings applies OpenAPI 3.2 prefixEncoding entries by position,
// ignores entries beyond itemCount, and applies itemEncoding to the remainder.
func PositionalEncodings(
	mediaType jsonvalue.Value,
	itemCount int,
	maxItems int,
) ([]AppliedEncoding, error) {
	if mediaType.Kind() != jsonvalue.ObjectKind {
		return nil, fmt.Errorf(
			"apply positional encodings: %w: media type is not an object",
			ErrInvalidPositionalEncoding,
		)
	}
	if itemCount < 0 || maxItems < 1 {
		return nil, fmt.Errorf(
			"apply positional encodings: %w: invalid item bounds",
			ErrInvalidPositionalEncoding,
		)
	}
	if itemCount > maxItems {
		return nil, ErrPositionalEncodingLimit
	}

	prefixCount := 0
	applied := make([]AppliedEncoding, 0, itemCount)
	if prefix, exists := mediaType.Lookup("prefixEncoding"); exists {
		encodings, valid := prefix.Elements()
		if !valid {
			return nil, fmt.Errorf(
				"apply positional encodings: %w: prefixEncoding is not an array",
				ErrInvalidPositionalEncoding,
			)
		}
		prefixCount = min(len(encodings), itemCount)
		for index := range prefixCount {
			if encodings[index].Kind() != jsonvalue.ObjectKind {
				return nil, fmt.Errorf(
					"apply positional encodings: %w: prefix entry is not an object",
					ErrInvalidPositionalEncoding,
				)
			}
			applied = append(applied, AppliedEncoding{
				Index: index, Value: encodings[index],
			})
		}
	}
	itemEncoding, hasItemEncoding := mediaType.Lookup("itemEncoding")
	if !hasItemEncoding {
		return applied, nil
	}
	if itemEncoding.Kind() != jsonvalue.ObjectKind {
		return nil, fmt.Errorf(
			"apply positional encodings: %w: itemEncoding is not an object",
			ErrInvalidPositionalEncoding,
		)
	}
	for offset := range itemCount - prefixCount {
		index := prefixCount + offset
		applied = append(applied, AppliedEncoding{
			Index: index, Value: itemEncoding,
		})
	}
	return applied, nil
}

// NamedEncodingValues applies OpenAPI 3.2 named Encoding Objects to a
// top-level object or array of objects using a caller-resolved properties map.
func NamedEncodingValues(
	properties jsonvalue.Value,
	encodings jsonvalue.Value,
	instance jsonvalue.Value,
	maxValues int,
) ([]NamedEncodingValue, error) {
	if properties.Kind() != jsonvalue.ObjectKind ||
		encodings.Kind() != jsonvalue.ObjectKind ||
		(instance.Kind() != jsonvalue.ObjectKind &&
			instance.Kind() != jsonvalue.ArrayKind) ||
		maxValues < 1 {
		return nil, fmt.Errorf(
			"apply named encodings: %w: invalid values or bounds",
			ErrInvalidNamedEncoding,
		)
	}
	members, _ := encodings.Members()
	applied := make([]NamedEncodingValue, 0)
	if instance.Kind() == jsonvalue.ObjectKind {
		for _, encoding := range members {
			property, exists := properties.Lookup(encoding.Name)
			if !exists {
				continue
			}
			if encoding.Value.Kind() != jsonvalue.ObjectKind {
				return nil, invalidNamedEncoding("encoding is not an object")
			}
			value, exists := instance.Lookup(encoding.Name)
			if !exists {
				continue
			}
			if schemaType(property, "array") {
				items, valid := value.Elements()
				if !valid {
					return nil, invalidNamedEncoding("array property value is not an array")
				}
				for _, item := range items {
					var err error
					applied, err = appendNamedEncoding(
						applied, encoding.Name, -1, encoding.Value, item, maxValues,
					)
					if err != nil {
						return nil, err
					}
				}
				continue
			}
			var err error
			applied, err = appendNamedEncoding(
				applied, encoding.Name, -1, encoding.Value, value, maxValues,
			)
			if err != nil {
				return nil, err
			}
		}
		return applied, nil
	}

	items, _ := instance.Elements()
	for itemIndex, item := range items {
		if item.Kind() != jsonvalue.ObjectKind {
			return nil, invalidNamedEncoding("top-level array item is not an object")
		}
		for _, encoding := range members {
			if _, exists := properties.Lookup(encoding.Name); !exists {
				continue
			}
			if encoding.Value.Kind() != jsonvalue.ObjectKind {
				return nil, invalidNamedEncoding("encoding is not an object")
			}
			value, exists := item.Lookup(encoding.Name)
			if !exists {
				continue
			}
			var err error
			applied, err = appendNamedEncoding(
				applied, encoding.Name, itemIndex, encoding.Value, value, maxValues,
			)
			if err != nil {
				return nil, err
			}
		}
	}
	return applied, nil
}

// SerializeNamedEncodingValues applies OpenAPI 3.2 named Encoding Objects and
// returns a bounded application/x-www-form-urlencoded body. Encoding keys are
// used as parameter names, including repeated names for array properties.
func SerializeNamedEncodingValues(
	properties jsonvalue.Value,
	encodings jsonvalue.Value,
	instance jsonvalue.Value,
	maxValues int,
	maxBytes int,
) (string, error) {
	if maxBytes < 1 {
		return "", ErrInvalidEncodingSerialization
	}
	version, _ := specversion.Parse("3.2.0")
	values, err := NamedEncodingValues(
		properties,
		encodings,
		instance,
		maxValues,
	)
	if err != nil {
		return "", err
	}
	var result strings.Builder
	for _, value := range values {
		encoded, _, encodeErr := serializeEncodingForVersion(
			version,
			value.Name,
			value.Value,
			value.Encoding,
			"application/x-www-form-urlencoded",
			maxBytes,
			true,
		)
		if encodeErr != nil {
			return "", encodeErr
		}
		required := len(encoded)
		if result.Len() > 0 {
			required++
		}
		if required > maxBytes-result.Len() {
			return "", ErrEncodingSerializationLimit
		}
		if result.Len() > 0 {
			result.WriteByte('&')
		}
		result.WriteString(encoded)
	}
	return result.String(), nil
}

func appendNamedEncoding(
	applied []NamedEncodingValue,
	name string,
	itemIndex int,
	encoding jsonvalue.Value,
	value jsonvalue.Value,
	maxValues int,
) ([]NamedEncodingValue, error) {
	if len(applied) >= maxValues {
		return nil, ErrNamedEncodingLimit
	}
	return append(applied, NamedEncodingValue{
		Name: name, ItemIndex: itemIndex, Encoding: encoding, Value: value,
	}), nil
}

func invalidNamedEncoding(message string) error {
	return fmt.Errorf("apply named encodings: %w: %s", ErrInvalidNamedEncoding, message)
}

func schemaType(schema jsonvalue.Value, wanted string) bool {
	typeValue, exists := schema.Lookup("type")
	if !exists {
		return false
	}
	if name, valid := typeValue.Text(); valid {
		return name == wanted
	}
	types, valid := typeValue.Elements()
	if !valid {
		return false
	}
	for _, candidate := range types {
		if name, valid := candidate.Text(); valid && name == wanted {
			return true
		}
	}
	return false
}

// MultipartFormDataDisposition maps a named encoding to the RFC 7578
// Content-Disposition value required for a multipart/form-data part.
func MultipartFormDataDisposition(name string, maxBytes int) (string, error) {
	if maxBytes < 1 || !utf8.ValidString(name) ||
		strings.ContainsAny(name, "\x00\r\n") {
		return "", ErrInvalidMultipartName
	}
	const prefix = `form-data; name="`
	const hexadecimal = "0123456789ABCDEF"
	var disposition strings.Builder
	disposition.WriteString(prefix)
	for index := range len(name) {
		character := name[index]
		switch {
		case multipartNameNeedsPercentEncoding(character):
			disposition.WriteByte('%')
			disposition.WriteByte(hexadecimal[character>>4])
			disposition.WriteByte(hexadecimal[character&0x0f])
		case multipartNameNeedsQuoting(character):
			disposition.WriteByte('\\')
			disposition.WriteByte(character)
		default:
			disposition.WriteByte(character)
		}
	}
	disposition.WriteByte('"')
	if disposition.Len() > maxBytes {
		return "", ErrMultipartDispositionLimit
	}
	return disposition.String(), nil
}

func multipartNameNeedsPercentEncoding(character byte) bool {
	const unescaped = " !\"#$&'()*+,-./0123456789:;<=>?@" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"
	return strings.IndexByte(unescaped, character) < 0
}

func multipartNameNeedsQuoting(character byte) bool {
	if character == '"' {
		return true
	}
	return character == '\\'
}

// SelectEncodingContentType returns the concrete media type an application
// selected for an encoded value. It never sniffs content. A selection is
// required when contentType lists multiple alternatives and is checked against
// specific and wildcard entries. With no explicit contentType, OpenAPI 3.2
// defaults are derived from the caller-resolved Schema Object.
func SelectEncodingContentType(
	encoding jsonvalue.Value,
	schema jsonvalue.Value,
	selected string,
) (string, error) {
	if encoding.Kind() != jsonvalue.ObjectKind ||
		schema.Kind() != jsonvalue.ObjectKind {
		return "", ErrInvalidEncodingContentType
	}
	contentType, explicit := encoding.Lookup("contentType")
	if !explicit {
		if selected != "" {
			return "", ErrEncodingContentTypeSelection
		}
		return defaultEncodingContentType(schema), nil
	}
	raw, valid := contentType.Text()
	if !valid {
		return "", ErrInvalidEncodingContentType
	}
	choices, err := parseEncodingContentTypes(raw)
	if err != nil {
		return "", err
	}
	if selected == "" {
		if len(choices) != 1 || strings.Contains(choices[0], "*") {
			return "", ErrEncodingContentTypeSelection
		}
		return choices[0], nil
	}
	mediaType, _, err := mime.ParseMediaType(selected)
	if err != nil || strings.Contains(mediaType, "*") {
		return "", ErrInvalidEncodingContentType
	}
	mediaType = strings.ToLower(mediaType)
	for _, choice := range choices {
		if encodingMediaTypeMatches(choice, mediaType) {
			return mediaType, nil
		}
	}
	return "", ErrEncodingContentTypeSelection
}

func parseEncodingContentTypes(raw string) ([]string, error) {
	var choices []string
	for _, part := range strings.Split(raw, ",") {
		mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil || mediaType == "" {
			return nil, ErrInvalidEncodingContentType
		}
		mediaType = strings.ToLower(mediaType)
		pieces := strings.Split(mediaType, "/")
		if len(pieces) != 2 || pieces[0] == "*" && pieces[1] != "*" {
			return nil, ErrInvalidEncodingContentType
		}
		choices = append(choices, mediaType)
	}
	return choices, nil
}

func encodingMediaTypeMatches(pattern string, concrete string) bool {
	if pattern == "*/*" || pattern == concrete {
		return true
	}
	patternParts := strings.Split(pattern, "/")
	concreteParts := strings.Split(concrete, "/")
	return len(patternParts) == 2 && len(concreteParts) == 2 &&
		patternParts[0] == concreteParts[0] && patternParts[1] == "*"
}

func defaultEncodingContentType(schema jsonvalue.Value) string {
	if _, exists := schema.Lookup("contentEncoding"); exists &&
		schemaType(schema, "string") {
		return "application/octet-stream"
	}
	for _, name := range []string{"object", "array"} {
		if schemaType(schema, name) {
			return "application/json"
		}
	}
	for _, name := range []string{"string", "number", "integer", "boolean"} {
		if schemaType(schema, name) {
			return "text/plain"
		}
	}
	return "application/octet-stream"
}

// EncodingHeaders returns headers applicable to one encoded multipart value.
// All headers are ignored for non-multipart media. Content-Type is always
// excluded case-insensitively because the Encoding Object describes it
// separately.
func EncodingHeaders(
	encoding jsonvalue.Value,
	multipart bool,
	maxHeaders int,
) ([]EncodingHeader, error) {
	if encoding.Kind() != jsonvalue.ObjectKind || maxHeaders < 1 {
		return nil, ErrInvalidEncodingHeaders
	}
	if !multipart {
		return nil, nil
	}
	headers, exists := encoding.Lookup("headers")
	if !exists {
		return nil, nil
	}
	members, valid := headers.Members()
	if !valid {
		return nil, ErrInvalidEncodingHeaders
	}
	result := make([]EncodingHeader, 0, min(len(members), maxHeaders))
	for _, member := range members {
		if strings.EqualFold(member.Name, "Content-Type") {
			continue
		}
		if member.Value.Kind() != jsonvalue.ObjectKind {
			return nil, ErrInvalidEncodingHeaders
		}
		if len(result) >= maxHeaders {
			return nil, ErrEncodingHeaderLimit
		}
		result = append(result, EncodingHeader{
			Name: member.Name, Value: member.Value,
		})
	}
	return result, nil
}

// ApplyEncoding combines OpenAPI 3.2 common Encoding Object fields with its
// optional RFC6570-style serialization fields. Nested Encoding Object fields
// are retained in the result so callers can apply the same operation
// recursively after mapping them to child values.
func ApplyEncoding(
	name string,
	value jsonvalue.Value,
	schema jsonvalue.Value,
	encoding jsonvalue.Value,
	mediaType string,
	selectedContentType string,
	maxHeaders int,
	maxBytes int,
) (EncodingApplication, error) {
	if encoding.Kind() != jsonvalue.ObjectKind {
		return EncodingApplication{}, ErrInvalidEncodingSerialization
	}
	base, _, err := mime.ParseMediaType(mediaType)
	major, subtype, structured := strings.Cut(base, "/")
	if err != nil || !structured || major == "" || subtype == "" {
		return EncodingApplication{}, ErrInvalidEncodingSerialization
	}
	contentType, err := SelectEncodingContentType(
		encoding,
		schema,
		selectedContentType,
	)
	if err != nil {
		return EncodingApplication{}, err
	}
	headers, err := EncodingHeaders(
		encoding,
		strings.HasPrefix(strings.ToLower(base), "multipart/"),
		maxHeaders,
	)
	if err != nil {
		return EncodingApplication{}, err
	}
	serialized, applied, err := SerializeEncoding(
		name,
		value,
		encoding,
		mediaType,
		maxBytes,
	)
	if err != nil {
		return EncodingApplication{}, err
	}
	result := EncodingApplication{
		Encoding: encoding, ContentType: contentType, Headers: headers,
		Serialized: serialized, SerializationApplied: applied,
	}
	result.NamedEncoding, _ = encoding.Lookup("encoding")
	result.PrefixEncoding, _ = encoding.Lookup("prefixEncoding")
	result.ItemEncoding, _ = encoding.Lookup("itemEncoding")
	return result, nil
}

// SerializeEncoding applies explicitly configured OpenAPI 3.2 RFC6570-style
// fields to one value. The boolean result is false when those fields are absent
// or ignored in the enclosing media type. Form-urlencoded output follows the
// WHATWG space-as-plus convention. Multipart form-data is never URI
// percent-encoded, and allowReserved therefore has no effect there.
func SerializeEncoding(
	name string,
	value jsonvalue.Value,
	encoding jsonvalue.Value,
	mediaType string,
	maxBytes int,
) (string, bool, error) {
	version, _ := specversion.Parse("3.2.0")
	return SerializeEncodingForVersion(
		version, name, value, encoding, mediaType, maxBytes,
	)
}

// SerializeEncodingForVersion applies explicit Encoding Object serialization
// semantics for an OpenAPI 3.x patch revision. OpenAPI 3.0 applies these fields
// only to form-urlencoded bodies; later lines also apply them to multipart.
func SerializeEncodingForVersion(
	version specversion.Version,
	name string,
	value jsonvalue.Value,
	encoding jsonvalue.Value,
	mediaType string,
	maxBytes int,
) (string, bool, error) {
	return serializeEncodingForVersion(
		version,
		name,
		value,
		encoding,
		mediaType,
		maxBytes,
		false,
	)
}

func serializeEncodingForVersion(
	version specversion.Version,
	name string,
	value jsonvalue.Value,
	encoding jsonvalue.Value,
	mediaType string,
	maxBytes int,
	applyDefaults bool,
) (string, bool, error) {
	if encoding.Kind() != jsonvalue.ObjectKind || maxBytes < 1 {
		return "", false, ErrInvalidEncodingSerialization
	}
	dialect := version.Dialect()
	switch dialect {
	case specversion.DialectOAS30,
		specversion.DialectOAS31,
		specversion.DialectOAS32:
	default:
		return "", false, ErrInvalidEncodingSerialization
	}
	base, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return "", false, ErrInvalidEncodingSerialization
	}
	base = strings.ToLower(base)
	switch dialect {
	case specversion.DialectOAS30:
		if base != "application/x-www-form-urlencoded" {
			return "", false, nil
		}
	default:
		if base != "application/x-www-form-urlencoded" &&
			base != "multipart/form-data" {
			return "", false, nil
		}
	}
	_, hasStyle := encoding.Lookup("style")
	_, hasExplode := encoding.Lookup("explode")
	_, hasReserved := encoding.Lookup("allowReserved")
	if !applyDefaults && !hasStyle && !hasExplode && !hasReserved {
		return "", false, nil
	}

	style := parameter.Form
	if field, exists := encoding.Lookup("style"); exists {
		text, valid := field.Text()
		if !valid {
			return "", false, ErrInvalidEncodingSerialization
		}
		style = parameter.Style(text)
	}
	explode := style == parameter.Form
	if field, exists := encoding.Lookup("explode"); exists {
		var valid bool
		explode, valid = field.Bool()
		if !valid {
			return "", false, ErrInvalidEncodingSerialization
		}
	}
	allowReserved := false
	if field, exists := encoding.Lookup("allowReserved"); exists {
		var valid bool
		allowReserved, valid = field.Bool()
		if !valid {
			return "", false, ErrInvalidEncodingSerialization
		}
	}
	if base == "multipart/form-data" {
		allowReserved = false
	}
	parameterMaximum := maxBytes
	if base == "multipart/form-data" ||
		base == "application/x-www-form-urlencoded" {
		parameterMaximum = scaledEncodingMaximum(maxBytes)
	}
	encoded, err := parameter.Encode(name, value, parameter.Options{
		Version: version, Location: parameter.Query, Style: style,
		Explode: explode, AllowReserved: allowReserved,
		Limits: parameter.Limits{MaxBytes: parameterMaximum},
	})
	if err != nil {
		if errors.Is(err, parameter.ErrLimitExceeded) {
			return "", false, ErrEncodingSerializationLimit
		}
		return "", false, fmt.Errorf(
			"%w: %v", ErrInvalidEncodingSerialization, err,
		)
	}
	if base == "application/x-www-form-urlencoded" {
		encoded = strings.ReplaceAll(encoded, "%20", "+")
	} else {
		// parameter.Encode produces only complete uppercase percent triples.
		encoded, _ = url.PathUnescape(encoded)
	}
	if len(encoded) > maxBytes {
		return "", false, ErrEncodingSerializationLimit
	}
	return encoded, true, nil
}

func scaledEncodingMaximum(maxBytes int) int {
	maximumInteger := int(^uint(0) >> 1)
	if maxBytes <= maximumInteger/3 {
		return maxBytes * 3
	}
	return maximumInteger
}

// FormURLEncode applies the WHATWG application/x-www-form-urlencoded percent
// encoding rules after callers have converted complex values to strings. It
// preserves field order and repeated names.
func FormURLEncode(
	fields []FormField,
	maxFields int,
	maxBytes int,
) (string, error) {
	if maxFields < 1 || maxBytes < 1 {
		return "", ErrInvalidFormURLEncoding
	}
	if len(fields) > maxFields {
		return "", ErrFormURLEncodingLimit
	}
	var encoded strings.Builder
	for index, field := range fields {
		if !utf8.ValidString(field.Name) || !utf8.ValidString(field.Value) {
			return "", ErrInvalidFormURLEncoding
		}
		if index > 0 && !writeFormByte(&encoded, '&', maxBytes) {
			return "", ErrFormURLEncodingLimit
		}
		if !writeFormComponent(&encoded, field.Name, maxBytes) ||
			!writeFormByte(&encoded, '=', maxBytes) ||
			!writeFormComponent(&encoded, field.Value, maxBytes) {
			return "", ErrFormURLEncodingLimit
		}
	}
	return encoded.String(), nil
}

func writeFormComponent(
	encoded *strings.Builder,
	value string,
	maxBytes int,
) bool {
	const hexadecimal = "0123456789ABCDEF"
	for index := range len(value) {
		character := value[index]
		if character == ' ' {
			if !writeFormByte(encoded, '+', maxBytes) {
				return false
			}
			continue
		}
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("*-._", rune(character)) {
			if !writeFormByte(encoded, character, maxBytes) {
				return false
			}
			continue
		}
		if encoded.Len()+3 > maxBytes {
			return false
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexadecimal[character>>4])
		encoded.WriteByte(hexadecimal[character&0x0f])
	}
	return true
}

func writeFormByte(encoded *strings.Builder, value byte, maxBytes int) bool {
	if encoded.Len() >= maxBytes {
		return false
	}
	encoded.WriteByte(value)
	return true
}

// ExternalExampleText converts bounded external example bytes to Unicode.
// When the media type does not explicitly identify a character set, UTF-8 is
// assumed as required by OpenAPI 3.2. UTF-8 and US-ASCII are supported without
// hidden transcoding dependencies.
func ExternalExampleText(
	raw []byte,
	contentType string,
	maxBytes int,
) (string, error) {
	if maxBytes < 1 {
		return "", ErrInvalidExternalExample
	}
	if len(raw) > maxBytes {
		return "", ErrExternalExampleLimit
	}
	charset := "utf-8"
	if contentType != "" {
		_, parameters, err := mime.ParseMediaType(contentType)
		if err != nil {
			return "", ErrInvalidExternalExample
		}
		if explicit, exists := parameters["charset"]; exists {
			charset = strings.ToLower(explicit)
		}
	}
	switch charset {
	case "utf-8", "utf8":
		if !utf8.Valid(raw) {
			return "", ErrInvalidExternalExample
		}
	case "us-ascii", "ascii":
		for _, character := range raw {
			if character > 0x7f {
				return "", ErrInvalidExternalExample
			}
		}
	default:
		return "", ErrUnsupportedExternalExampleCharset
	}
	return string(raw), nil
}
