// Package format renders exact Money values for display. Currency identity
// remains the international Code carried by Money; formatting never changes
// arithmetic values or contexts.
package format

import (
	"errors"
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/money"
	textcurrency "golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// MaxFormattedBytes bounds display output and diagnostic amplification.
const MaxFormattedBytes = 2_048

var ErrInvalidFormat = errors.New("money format: invalid value or locale")

// Options controls display-only currency labeling.
type Options struct {
	// Symbol requests the locale-specific CLDR currency symbol. The default is
	// the unambiguous ISO alphabetic code.
	Symbol bool
}

// Exact returns context-preserving decimal text and the ISO currency code.
func Exact(value money.Money) string {
	if !value.Valid() {
		return ""
	}

	return value.Amount().String() + " " + value.Currency().String()
}

// Locale formats exact decimal text with CLDR digits and separators selected
// by a validated international locale. It never converts the amount to a
// float or asks x/text to round it.
func Locale(value money.Money, tag locale.Tag, options Options) (string, error) {
	if !value.Valid() || tag.IsZero() {
		return "", ErrInvalidFormat
	}
	parsed := language.Make(tag.String())
	printer := message.NewPrinter(parsed)
	digits := localizedDigits(printer)
	decimalSeparator := inferDecimalSeparator(printer, digits)
	groupSeparator, primaryGrouping, secondaryGrouping := inferGrouping(printer, digits)
	minus := inferMinus(printer, digits)
	number := localizeNumber(
		value.Amount().String(),
		digits,
		decimalSeparator,
		groupSeparator,
		minus,
		primaryGrouping,
		secondaryGrouping,
	)
	label := value.Currency().String()
	if options.Symbol {
		unit, parseErr := textcurrency.ParseISO(value.Currency().String())
		if parseErr == nil {
			label = printer.Sprintf("%v", textcurrency.Symbol(unit))
		}
	}
	return boundedFormat(label + " " + number)
}

func boundedFormat(result string) (string, error) {
	if len(result) > MaxFormattedBytes {
		return "", ErrInvalidFormat
	}

	return result, nil
}

func localizedDigits(printer *message.Printer) [10]string {
	var digits [10]string
	for digit := range digits {
		digits[digit] = printer.Sprintf("%d", digit)
	}

	return digits
}

func inferDecimalSeparator(printer *message.Printer, digits [10]string) string {
	sample := delocalizeDigits(printer.Sprintf("%v", number.Decimal(1, number.Scale(1))), digits)
	return decimalSeparator(sample)
}

func decimalSeparator(sample string) string {
	separator := strings.TrimSuffix(strings.TrimPrefix(sample, "1"), "0")
	if separator == "" {
		return "."
	}

	return separator
}

func inferGrouping(printer *message.Printer, digits [10]string) (string, int, int) {
	sample := delocalizeDigits(printer.Sprintf("%d", 1000), digits)
	expanded := delocalizeDigits(printer.Sprintf("%d", 123456789), digits)
	return groupingPattern(sample, expanded)
}

func groupingPattern(sample, expanded string) (string, int, int) {
	separator := strings.TrimSuffix(strings.TrimPrefix(sample, "1"), "000")
	if separator == "" {
		return ",", 3, 3
	}
	groups := strings.Split(expanded, separator)
	if len(groups) < 2 {
		return separator, 3, 3
	}
	primary := len(groups[len(groups)-1])
	secondary := primary
	if len(groups) > 2 {
		secondary = len(groups[len(groups)-2])
	}

	return separator, primary, secondary
}

func inferMinus(printer *message.Printer, digits [10]string) string {
	sample := delocalizeDigits(printer.Sprintf("%d", -1), digits)
	return minusSign(sample)
}

func minusSign(sample string) string {
	minus := strings.TrimSuffix(sample, "1")
	if minus == "" {
		return "-"
	}

	return minus
}

func delocalizeDigits(input string, digits [10]string) string {
	for digit, localized := range digits {
		input = strings.ReplaceAll(input, localized, fmt.Sprintf("%d", digit))
	}

	return input
}

func localizeNumber(
	input string,
	digits [10]string,
	decimalSeparator string,
	groupSeparator string,
	minus string,
	primaryGrouping int,
	secondaryGrouping int,
) string {
	negative := strings.HasPrefix(input, "-")
	input = strings.TrimPrefix(input, "-")
	integer, fraction, found := strings.Cut(input, ".")
	integer = group(integer, groupSeparator, primaryGrouping, secondaryGrouping)
	result := integer
	if found {
		result += decimalSeparator + fraction
	}
	var builder strings.Builder
	if negative {
		builder.WriteString(minus)
	}
	for _, character := range result {
		if character >= '0' && character <= '9' {
			builder.WriteString(digits[character-'0'])
		} else {
			builder.WriteRune(character)
		}
	}

	return builder.String()
}

func group(integer, separator string, primary, secondary int) string {
	if len(integer) <= primary {
		return integer
	}
	prefixLength := len(integer) - primary
	first := prefixLength % secondary
	if first == 0 {
		first = secondary
	}
	var builder strings.Builder
	builder.WriteString(integer[:first])
	for index := first; index < prefixLength; index += secondary {
		builder.WriteString(separator)
		builder.WriteString(integer[index : index+secondary])
	}
	builder.WriteString(separator)
	builder.WriteString(integer[prefixLength:])

	return builder.String()
}
