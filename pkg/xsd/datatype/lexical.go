package datatype

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	floatLexical = regexp.MustCompile(
		`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`,
	)
	languageLexical = regexp.MustCompile(`^[A-Za-z]{1,8}(?:-[A-Za-z0-9]{1,8})*$`)
)

// ValidateBuiltInLexical checks the lexical space of built-in datatypes whose
// value spaces do not require an instance namespace context.
func ValidateBuiltInLexical(name string, lexical string) error {
	valid, known := builtInLexicalValid(name, lexical)
	if !known {
		return fmt.Errorf("%w: %s", ErrUnknownType, name)
	}
	if !valid {
		return fmt.Errorf("%w: %q is not %s", ErrInvalidLexical, lexical, name)
	}
	return nil
}

func builtInLexicalValid(name string, lexical string) (bool, bool) {
	switch name {
	case "string", "normalizedString", "token", "anyURI":
		return utf8.ValidString(lexical), true
	case "float", "double":
		return lexical == "INF" || lexical == "-INF" || lexical == "NaN" ||
			floatLexical.MatchString(lexical), true
	case "hexBinary":
		if len(lexical)%2 != 0 {
			return false, true
		}
		_, err := hex.DecodeString(lexical)
		return err == nil, true
	case "base64Binary":
		compact, ok := removeXMLWhitespace(lexical)
		if !ok {
			return false, true
		}
		_, err := base64.StdEncoding.Strict().DecodeString(compact)
		return err == nil, true
	case "language":
		return languageLexical.MatchString(lexical), true
	case "Name":
		return validXMLName(lexical, true), true
	case "NCName", "ID", "IDREF", "ENTITY":
		return validXMLName(lexical, false), true
	case "NMTOKEN":
		return validNMTOKEN(lexical), true
	case "IDREFS", "ENTITIES":
		return validNameList(lexical, false), true
	case "NMTOKENS":
		return validNameList(lexical, true), true
	case "QName", "NOTATION":
		return validQName(lexical), true
	case "dateTime", "time", "date", "gYearMonth", "gYear", "gMonthDay",
		"gDay", "gMonth":
		return validCalendarLexical(name, lexical), true
	case "duration":
		return validDuration(lexical), true
	default:
		return false, false
	}
}

func removeXMLWhitespace(value string) (string, bool) {
	var result strings.Builder
	result.Grow(len(value))
	for _, character := range value {
		switch character {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			if character > utf8.RuneSelf {
				return "", false
			}
			result.WriteRune(character)
		}
	}
	return result.String(), true
}

func validNameList(value string, nmtoken bool) bool {
	items := strings.Fields(value)
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if nmtoken && !validNMTOKEN(item) || !nmtoken && !validXMLName(item, false) {
			return false
		}
	}
	return true
}

func validQName(value string) bool {
	if strings.Count(value, ":") > 1 {
		return false
	}
	parts := strings.Split(value, ":")
	for _, part := range parts {
		if !validXMLName(part, false) {
			return false
		}
	}
	return true
}

func validXMLName(value string, allowColon bool) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for index, character := range value {
		if index == 0 {
			if !xmlNameStart(character, allowColon) {
				return false
			}
			continue
		}
		if !xmlNameCharacter(character, allowColon) {
			return false
		}
	}
	return true
}

func validNMTOKEN(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if !xmlNameCharacter(character, true) {
			return false
		}
	}
	return true
}

func xmlNameStart(character rune, allowColon bool) bool {
	return allowColon && character == ':' || character == '_' ||
		character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= 0xC0 && character <= 0xD6 ||
		character >= 0xD8 && character <= 0xF6 ||
		character >= 0xF8 && character <= 0x2FF ||
		character >= 0x370 && character <= 0x37D ||
		character >= 0x37F && character <= 0x1FFF ||
		character >= 0x200C && character <= 0x200D ||
		character >= 0x2070 && character <= 0x218F ||
		character >= 0x2C00 && character <= 0x2FEF ||
		character >= 0x3001 && character <= 0xD7FF ||
		character >= 0xF900 && character <= 0xFDCF ||
		character >= 0xFDF0 && character <= 0xFFFD ||
		character >= 0x10000 && character <= 0xEFFFF
}

func xmlNameCharacter(character rune, allowColon bool) bool {
	return xmlNameStart(character, allowColon) || character == '-' ||
		character == '.' || character >= '0' && character <= '9' ||
		character == 0xB7 || character >= 0x0300 && character <= 0x036F ||
		character >= 0x203F && character <= 0x2040
}
