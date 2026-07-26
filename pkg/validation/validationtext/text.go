// Package validationtext applies application-supplied message catalogs.
package validationtext

import (
	"html"
	"unicode/utf8"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// Catalog looks up application-facing prose without changing rule semantics.
type Catalog interface {
	Lookup(locale, code string, parameters map[string]string) (string, bool)
}

// Message pairs immutable machine identity with optional localized prose.
type Message struct {
	Path string
	Code string
	Text string
}

// Messages translates findings in report order. Missing entries leave Text
// empty rather than embedding core fallback prose.
func Messages(ctx validation.Context, report validation.Report,
	catalog Catalog,
) []Message {
	violations := report.Violations()
	messages := make([]Message, 0, len(violations))
	for _, violation := range violations {
		text := safeLookup(catalog, ctx.Locale(), violation.Code(),
			violation.Parameters(), ctx.Limits().MaxStringLength)
		messages = append(messages, Message{Path: violation.Path().String(),
			Code: violation.Code(), Text: text})
	}
	return messages
}

func safeLookup(catalog Catalog, locale, code string,
	parameters map[string]string, maximum int,
) (text string) {
	if catalog == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	text, found := catalog.Lookup(locale, code, parameters)
	if !found || len(text) > maximum || !utf8.ValidString(text) {
		return ""
	}
	for _, character := range text {
		if character < ' ' || character == 0x7f {
			return ""
		}
	}
	return html.EscapeString(text)
}
