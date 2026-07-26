package international

import (
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// LowercaseUnicode applies locale-neutral full Unicode lowercasing within a
// caller-owned byte budget. The transformation may expand text and therefore
// checks both input and output sizes. Each call owns its Caser, so callers may
// invoke the function concurrently without sharing state.
func LowercaseUnicode(input string, maximumBytes int) (string, error) {
	if maximumBytes <= 0 || len(input) > maximumBytes {
		return "", ErrResourceLimit
	}
	if !utf8.ValidString(input) {
		return "", NewParseError("text", "malformed UTF-8")
	}
	output := cases.Lower(language.Und).String(input)
	if len(output) > maximumBytes {
		return "", ErrResourceLimit
	}
	return output, nil
}
