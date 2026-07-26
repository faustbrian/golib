package slug

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxSourceRunes is the Laravel-compatible source limit applied before
// transliteration.
const MaxSourceRunes = 250

// LaravelEnglish reproduces Laravel 13 Str::slug source normalization with
// the default English profile used by spatie/laravel-sluggable 4.0.2.
//
// The function is deterministic and safe for concurrent use. It may return an
// empty string when the source has no supported letters or digits. Database
// uniqueness and numeric suffix selection remain persistence concerns.
func LaravelEnglish(source string) string {
	source = truncateSource(source)

	var transliterated strings.Builder
	for offset := 0; offset < len(source); {
		if replacement, consumed := longestReplacement(source[offset:]); consumed > 0 {
			transliterated.WriteString(replacement)
			offset += consumed
			continue
		}

		character, size := utf8.DecodeRuneInString(source[offset:])
		switch {
		case character == '\r' || character == '\n' || character == '\t':
			transliterated.WriteByte(' ')
		case character >= 0x20 && character <= 0x7E:
			transliterated.WriteRune(character)
		}
		offset += size
	}

	normalized := strings.NewReplacer(
		"_", "-",
		"@", "-at-",
	).Replace(strings.ToLower(transliterated.String()))

	var result strings.Builder
	pendingSeparator := false
	for _, character := range normalized {
		switch {
		case character >= 'a' && character <= 'z',
			character >= '0' && character <= '9':
			if pendingSeparator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(character)
			pendingSeparator = false
		case character == '-' || unicode.IsSpace(character):
			pendingSeparator = result.Len() > 0
		}
	}

	return result.String()
}

func truncateSource(source string) string {
	offset := 0
	for range MaxSourceRunes {
		if offset == len(source) {
			return source
		}
		_, size := utf8.DecodeRuneInString(source[offset:])
		offset += size
	}

	return source[:offset]
}

func longestReplacement(source string) (string, int) {
	for _, candidate := range laravelEnglishReplacements[source[0]] {
		if !strings.HasPrefix(source, candidate.source) {
			continue
		}
		return candidate.value, len(candidate.source)
	}

	return "", 0
}
