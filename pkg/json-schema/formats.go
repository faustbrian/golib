package jsonschema

import (
	"context"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// FormatChecker validates the string representation of a named format.
type FormatChecker interface {
	Valid(context.Context, string) (bool, error)
}

// FormatFunc adapts a function to FormatChecker.
type FormatFunc func(context.Context, string) (bool, error)

// Valid implements FormatChecker.
func (format FormatFunc) Valid(ctx context.Context, value string) (bool, error) {
	return format(ctx, value)
}

type simpleFormatFunc func(string) bool

type boundedRegexFormat struct {
	limits Limits
}

type customFormatChecker struct {
	checker FormatChecker
}

type idnaProfile interface {
	ToUnicode(string) (string, error)
	ToASCII(string) (string, error)
}

func (format simpleFormatFunc) Valid(ctx context.Context, value string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return format(value), nil
}

func (format boundedRegexFormat) Valid(ctx context.Context, value string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	return validRegexWithLimits(value, format.limits)
}

func (format customFormatChecker) Valid(ctx context.Context, value string) (bool, error) {
	return format.checker.Valid(ctx, value)
}

func standardFormats() map[string]FormatChecker {
	regexFormat := boundedRegexFormat{limits: DefaultLimits()}
	return map[string]FormatChecker{
		"color":                 simpleFormatFunc(validColor),
		"date":                  simpleFormatFunc(validDate),
		"date-time":             simpleFormatFunc(validDateTime),
		"duration":              simpleFormatFunc(validDuration),
		"email":                 simpleFormatFunc(validEmail),
		"host-name":             simpleFormatFunc(validHostname),
		"hostname":              simpleFormatFunc(validHostname),
		"idn-email":             simpleFormatFunc(validIDNEmail),
		"idn-hostname":          simpleFormatFunc(validIDNHostname),
		"ip-address":            simpleFormatFunc(validIPv4),
		"iri":                   simpleFormatFunc(validIRI),
		"iri-reference":         simpleFormatFunc(validIRIReference),
		"ipv4":                  simpleFormatFunc(validIPv4),
		"ipv6":                  simpleFormatFunc(validIPv6),
		"json-pointer":          simpleFormatFunc(validJSONPointer),
		"regex":                 regexFormat,
		"relative-json-pointer": simpleFormatFunc(validRelativeJSONPointer),
		"time":                  simpleFormatFunc(validTime),
		"uri":                   simpleFormatFunc(validURI),
		"uri-reference":         simpleFormatFunc(validURIReference),
		"uri-template":          simpleFormatFunc(validURITemplate),
		"uuid":                  simpleFormatFunc(validUUID),
	}
}

func applyStandardFormatLimits(formats map[string]FormatChecker, limits Limits) {
	if _, standard := formats["regex"].(boundedRegexFormat); standard {
		formats["regex"] = boundedRegexFormat{limits: limits}
	}
}

func standardFormatSupported(dialect Dialect, name string) bool {
	switch dialect {
	case Draft3:
		return formatNameIn(name,
			"color", "date", "date-time", "email", "host-name", "ip-address",
			"ipv6", "regex", "time", "uri",
		)
	case Draft4:
		return formatNameIn(name,
			"date-time", "email", "hostname", "ipv4", "ipv6", "uri",
		)
	case Draft6:
		return formatNameIn(name,
			"date-time", "email", "hostname", "ipv4", "ipv6", "json-pointer",
			"uri", "uri-reference", "uri-template",
		)
	case Draft7:
		return formatNameIn(name,
			"date", "date-time", "email", "hostname", "idn-email", "idn-hostname",
			"ipv4", "ipv6", "iri", "iri-reference", "json-pointer", "regex",
			"relative-json-pointer", "time", "uri", "uri-reference", "uri-template",
		)
	case Draft201909, Draft202012:
		return formatNameIn(name,
			"date", "date-time", "duration", "email", "hostname", "idn-email",
			"idn-hostname", "ipv4", "ipv6", "iri", "iri-reference", "json-pointer",
			"regex", "relative-json-pointer", "time", "uri", "uri-reference",
			"uri-template", "uuid",
		)
	default:
		return false
	}
}

func formatNameIn(name string, names ...string) bool {
	for _, candidate := range names {
		if name == candidate {
			return true
		}
	}
	return false
}

func cloneFormats(source map[string]FormatChecker) map[string]FormatChecker {
	result := make(map[string]FormatChecker, len(source))
	for name, checker := range source {
		result[name] = checker
	}

	return result
}

var (
	datePattern = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	timePattern = regexp.MustCompile(
		`^(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:[zZ]|[+-](\d{2}):(\d{2}))$`,
	)
	durationPattern = regexp.MustCompile(
		`^P(?:\d+W|(?:\d+Y(?:\d+M(?:\d+D)?)?|\d+M(?:\d+D)?|\d+D)(?:T(?:\d+H(?:\d+M(?:\d+S)?)?|\d+M(?:\d+S)?|\d+S))?|T(?:\d+H(?:\d+M(?:\d+S)?)?|\d+M(?:\d+S)?|\d+S))$`,
	)
	uuidPattern = regexp.MustCompile(
		`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`,
	)
	schemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)
)

var cssColors = map[string]struct{}{
	"aqua": {}, "black": {}, "blue": {}, "fuchsia": {},
	"gray": {}, "green": {}, "lime": {}, "maroon": {},
	"navy": {}, "olive": {}, "orange": {}, "purple": {},
	"red": {}, "silver": {}, "teal": {}, "white": {},
	"yellow": {},
}

var idnaRegistration = idna.New(
	idna.MapForLookup(),
	idna.ValidateForRegistration(),
	idna.BidiRule(),
	idna.VerifyDNSLength(true),
)

func validColor(value string) bool {
	if strings.HasPrefix(value, "#") {
		if len(value) != 4 && len(value) != 7 {
			return false
		}
		for _, character := range value[1:] {
			if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
				return false
			}
		}
		return true
	}
	_, valid := cssColors[strings.ToLower(value)]
	return valid
}

func validDate(value string) bool {
	match := datePattern.FindStringSubmatch(value)
	if match == nil {
		return false
	}
	year, _ := strconv.Atoi(match[1])
	month, _ := strconv.Atoi(match[2])
	day, _ := strconv.Atoi(match[3])
	parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)

	return parsed.Year() == year && int(parsed.Month()) == month && parsed.Day() == day
}

func validTime(value string) bool {
	match := timePattern.FindStringSubmatch(value)
	if match == nil {
		return false
	}
	hour, _ := strconv.Atoi(match[1])
	minute, _ := strconv.Atoi(match[2])
	second, _ := strconv.Atoi(match[3])
	if hour > 23 || minute > 59 || second > 60 {
		return false
	}
	offset := 0
	if match[4] != "" {
		offsetHour, _ := strconv.Atoi(match[4])
		offsetMinute, _ := strconv.Atoi(match[5])
		if offsetHour > 23 || offsetMinute > 59 {
			return false
		}
		offset = offsetHour*60 + offsetMinute
		offsetMarker := strings.LastIndexAny(value, "+-")
		if value[offsetMarker] == '-' {
			offset = -offset
		}
	}
	if second == 60 {
		utcMinute := (hour*60 + minute - offset + 24*60) % (24 * 60)
		if utcMinute != 23*60+59 {
			return false
		}
	}

	return true
}

func validLegacyTime(value string) bool {
	if len(value) != len("00:00:00") || value[2] != ':' || value[5] != ':' {
		return false
	}
	hour, hourErr := strconv.Atoi(value[0:2])
	minute, minuteErr := strconv.Atoi(value[3:5])
	second, secondErr := strconv.Atoi(value[6:8])

	return hourErr == nil && minuteErr == nil && secondErr == nil &&
		hour <= 23 && minute <= 59 && second <= 59
}

func validDateTime(value string) bool {
	separator := strings.IndexAny(value, "Tt")
	return separator == len("0000-00-00") &&
		validDate(value[:separator]) && validTime(value[separator+1:])
}

func validDuration(value string) bool {
	return durationPattern.MatchString(value)
}

func validIPv4(value string) bool {
	address, err := netip.ParseAddr(value)
	return err == nil && address.Is4()
}

func validIPv6(value string) bool {
	if strings.Contains(value, "%") {
		return false
	}
	address, err := netip.ParseAddr(value)
	return err == nil && address.Is6()
}

func validJSONPointer(value string) bool {
	if value == "" {
		return true
	}
	if value[0] != '/' {
		return false
	}
	skip := false
	for index, character := range []byte(value) {
		if skip {
			skip = false
			continue
		}
		if character != '~' {
			continue
		}
		if index+1 >= len(value) || value[index+1] != '0' && value[index+1] != '1' {
			return false
		}
		skip = true
	}

	return true
}

func validRelativeJSONPointer(value string) bool {
	index := 0
	if value == "" || value[0] < '0' || value[0] > '9' {
		return false
	}
	if value[0] == '0' {
		index = 1
	} else {
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
	}
	if index == len(value) {
		return true
	}
	if value[index:] == "#" {
		return true
	}

	return validJSONPointer(value[index:])
}

func validRegex(value string) bool {
	valid, _ := validRegexWithLimits(value, DefaultLimits())
	return valid
}

func validRegexWithLimits(value string, limits Limits) (bool, error) {
	if len(value) > limits.MaxRegexBytes {
		return false, &LimitError{
			Resource: "regular expression bytes",
			Limit:    limits.MaxRegexBytes,
		}
	}
	if strings.Contains(value, "(?P<") || strings.Contains(value, "(?P=") {
		return false, nil
	}
	escaped := false
	for _, character := range []byte(value) {
		if escaped {
			escaped = false
			if character == '\\' {
				continue
			}
			if isASCIIAlpha(character) && !strings.ContainsRune("bfnrtvcdDsSwWpPkux", rune(character)) {
				return false, nil
			}
			continue
		}
		if character == '\\' {
			escaped = true
		}
	}
	syntaxPattern := strings.ReplaceAll(value, "[^]", "(?s:.)")
	for _, assertion := range []string{"(?<=", "(?<!", "(?=", "(?!"} {
		syntaxPattern = strings.ReplaceAll(syntaxPattern, assertion, "(?:")
	}
	_, err := compilePatternWithLimits(syntaxPattern, limits)
	return err == nil, nil
}

func validEmail(value string) bool {
	return validMailbox(value, false)
}

func validIDNEmail(value string) bool {
	return validMailbox(value, true)
}

func validMailbox(value string, international bool) bool {
	separator := lastUnquotedAt(value)
	if separator <= 0 {
		return false
	}
	local, domain := value[:separator], value[separator+1:]
	if domain == "" {
		return false
	}
	if len(local) > 64 || !validLocalPart(local, international) {
		return false
	}
	if strings.HasPrefix(domain, "[") && strings.HasSuffix(domain, "]") {
		literal := domain[1 : len(domain)-1]
		if strings.HasPrefix(strings.ToLower(literal), "ipv6:") {
			return validIPv6(literal[5:])
		}
		return validIPv4(literal)
	}
	if international {
		return validIDNHostname(domain)
	}
	return validHostname(domain)
}

func lastUnquotedAt(value string) int {
	quoted, escaped, separator := false, false, -1
	for index, character := range value {
		if escaped {
			escaped = false
			continue
		}
		if quoted && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' {
			quoted = !quoted
			continue
		}
		if character == '@' && !quoted {
			if separator >= 0 {
				return -1
			}
			separator = index
		}
	}
	if quoted || escaped {
		return -1
	}
	return separator
}

func validLocalPart(value string, international bool) bool {
	if strings.HasPrefix(value, "\"") {
		if len(value) < 2 || value[len(value)-1] != '"' {
			return false
		}
		escaped := false
		for _, character := range value[1 : len(value)-1] {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' || invalidEmailRune(character, international) {
				return false
			}
		}
		return !escaped
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") ||
		strings.Contains(value, "..") {
		return false
	}
	for _, atom := range strings.Split(value, ".") {
		for _, character := range atom {
			if !isASCII(string(character)) {
				if !international || invalidEmailRune(character, true) {
					return false
				}
				continue
			}
			if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&'*+-/=?^_`{|}~", character) {
				return false
			}
		}
	}
	return true
}

func invalidEmailRune(character rune, international bool) bool {
	if character == utf8.RuneError || character == '\r' || character == '\n' ||
		character == 0 || unicode.IsControl(character) {
		return true
	}
	return !isASCII(string(character)) && !international
}

func validHostname(value string) bool {
	if value == "" || len(value) > 253 || !isASCII(value) ||
		strings.ContainsAny(value, "。．｡") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validASCIILabel(label) {
			return false
		}
		if strings.HasPrefix(strings.ToLower(label), "xn--") {
			if !validPunycodeLabel(label, idna.Lookup, idnaRegistration) {
				return false
			}
		}
	}
	return true
}

func validPunycodeLabel(label string, lookup, registration idnaProfile) bool {
	decoded, err := lookup.ToUnicode(label)
	if err != nil || !validIDNLabel(decoded) {
		return false
	}
	encoded, err := registration.ToASCII(decoded)
	return err == nil && strings.EqualFold(encoded, label)
}

func validASCIILabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, character := range label {
		if !isASCII(string(character)) {
			return false
		}
		alpha := character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z'
		if character != '-' && !alpha &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validIDNHostname(value string) bool {
	if value == "" || !utf8.ValidString(value) ||
		strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	normalized, err := idna.Lookup.ToUnicode(value)
	if err != nil {
		return false
	}
	ascii, err := idnaRegistration.ToASCII(normalized)
	if err != nil {
		return false
	}
	return validIDNHostnameLabels(normalized, ascii)
}

func validIDNHostnameLabels(normalized, ascii string) bool {
	unicodeLabels := strings.Split(normalized, ".")
	asciiLabels := strings.Split(ascii, ".")
	if len(unicodeLabels) != len(asciiLabels) {
		return false
	}
	for index, label := range asciiLabels {
		if !validASCIILabel(label) {
			return false
		}
		if !validIDNLabel(unicodeLabels[index]) {
			return false
		}
	}
	return true
}

func validIDNLabel(label string) bool {
	runes := []rune(label)
	if len(runes) == 0 {
		return false
	}
	hasJapanese := false
	hasArabicIndic, hasExtendedArabicIndic := false, false
	for _, character := range runes {
		if !validIDNRune(character) {
			return false
		}
		if unicode.In(character, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			hasJapanese = true
		}
		if character >= '\u0660' && character <= '\u0669' {
			hasArabicIndic = true
		}
		if character >= '\u06F0' && character <= '\u06F9' {
			hasExtendedArabicIndic = true
		}
		if strings.ContainsRune("\u0640\u07FA\u302E\u302F\u3031\u3032\u3033\u3034\u3035\u303B", character) {
			return false
		}
	}
	if hasArabicIndic && hasExtendedArabicIndic {
		return false
	}
	for index, character := range runes {
		switch character {
		case '\u00B7':
			if index == 0 || index+1 == len(runes) ||
				unicode.ToLower(runes[index-1]) != 'l' || unicode.ToLower(runes[index+1]) != 'l' {
				return false
			}
		case '\u0375':
			if index+1 == len(runes) || !unicode.In(runes[index+1], unicode.Greek) {
				return false
			}
		case '\u05F3', '\u05F4':
			if index == 0 || !unicode.In(runes[index-1], unicode.Hebrew) {
				return false
			}
		case '\u30FB':
			if !hasJapanese {
				return false
			}
		}
	}
	return true
}

func validIDNRune(character rune) bool {
	if character == '-' || unicode.IsLetter(character) || unicode.IsDigit(character) ||
		unicode.Is(unicode.Mn, character) || unicode.Is(unicode.Mc, character) {
		return true
	}
	return strings.ContainsRune(
		"\u00B7\u0375\u05F3\u05F4\u06FD\u06FE\u0F0B\u200C\u200D\u3007\u30FB",
		character,
	)
}

func validURI(value string) bool {
	parsed, valid := parseIdentifier(value, false)
	return valid && parsed.Scheme != ""
}

func validURIReference(value string) bool {
	_, valid := parseIdentifier(value, false)
	return valid
}

func validIRI(value string) bool {
	parsed, valid := parseIdentifier(value, true)
	return valid && parsed.Scheme != ""
}

func validIRIReference(value string) bool {
	_, valid := parseIdentifier(value, true)
	return valid
}

func parseIdentifier(value string, international bool) (*url.URL, bool) {
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\\\"<>[]{}^`| \t\r\n") {
		// Brackets are permitted only as the delimiters of an IP literal.
		if !strings.Contains(value, "[") || strings.ContainsAny(value, "\\\"<>{}^`| \t\r\n") {
			return nil, false
		}
	}
	if !validPercentEncoding(value) || !international && !isASCII(value) {
		return nil, false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "" && !schemePattern.MatchString(parsed.Scheme) {
		return nil, false
	}
	if parsed.Host != "" && !validIdentifierAuthority(parsed, international) {
		return nil, false
	}
	return parsed, true
}

func validIdentifierAuthority(parsed *url.URL, international bool) bool {
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	if strings.Contains(parsed.Host, ":") && !strings.Contains(parsed.Host, "[") {
		if strings.Count(parsed.Host, ":") > 1 {
			return false
		}
		port := parsed.Port()
		if port == "" {
			return false
		}
	}
	if strings.HasPrefix(parsed.Host, "[") {
		return validIPv6(host)
	}
	if international {
		return validIDNHostname(host)
	}
	return validHostname(host)
}

func validPercentEncoding(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
			return false
		}
		index += 2
	}
	return true
}

func isHex(character byte) bool {
	return character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'f' ||
		character >= 'A' && character <= 'F'
}

func isASCII(value string) bool {
	for _, character := range value {
		if character > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func validURITemplate(value string) bool {
	depth := 0
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '{':
			if depth != 0 {
				return false
			}
			depth = 1
		case '}':
			if depth == 0 {
				return false
			}
			depth = 0
		}
	}
	return depth == 0
}

func validUUID(value string) bool {
	return uuidPattern.MatchString(value)
}
