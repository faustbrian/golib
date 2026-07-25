package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// ByteLength requires a byte length within the inclusive bounds.
func ByteLength(minimum, maximum int) validation.Validator[string] {
	return stringLength("byte_length", minimum, maximum, func(value string) int {
		return len(value)
	})
}

// RuneLength requires a Unicode code-point length within the inclusive bounds.
func RuneLength(minimum, maximum int) validation.Validator[string] {
	return stringLength("rune_length", minimum, maximum, utf8.RuneCountInString)
}

func stringLength(code string, minimum, maximum int,
	length func(string) int,
) validation.Validator[string] {
	return validation.ValidatorFunc[string](func(
		ctx validation.Context, value string,
	) validation.Report {
		if report, oversized := rejectOversizedString(ctx, value); oversized {
			return report
		}
		actual := length(value)
		if actual >= minimum && actual <= maximum {
			return pass(ctx)
		}
		return fail(ctx, code, map[string]string{
			"minimum": strconv.Itoa(minimum), "maximum": strconv.Itoa(maximum),
		})
	})
}

// Pattern compiles a bounded RE2 expression once and returns its validator.
func Pattern(expression string,
	limits validation.Limits,
) (validation.Validator[string], error) {
	if len(expression) > limits.MaxRegexPatternLength {
		return nil, fmt.Errorf("%w: regex pattern length", validation.ErrLimitExceeded)
	}
	compiled, err := regexp.Compile(expression)
	if err != nil {
		return nil, fmt.Errorf("compile validation pattern: %w", err)
	}
	return validation.ValidatorFunc[string](func(
		ctx validation.Context, value string,
	) validation.Report {
		if report, oversized := rejectOversizedString(ctx, value); oversized {
			return report
		}
		if compiled.MatchString(value) {
			return pass(ctx)
		}
		return fail(ctx, "pattern", nil)
	}), nil
}

// Prefix requires a literal prefix.
func Prefix(prefix string) validation.Validator[string] {
	return validation.ValidatorFunc[string](func(
		ctx validation.Context, value string,
	) validation.Report {
		if report, oversized := rejectOversizedString(ctx, value); oversized {
			return report
		}
		if strings.HasPrefix(value, prefix) {
			return pass(ctx)
		}
		return fail(ctx, "prefix", nil)
	})
}

// Suffix requires a literal suffix.
func Suffix(suffix string) validation.Validator[string] {
	return validation.ValidatorFunc[string](func(
		ctx validation.Context, value string,
	) validation.Report {
		if report, oversized := rejectOversizedString(ctx, value); oversized {
			return report
		}
		if strings.HasSuffix(value, suffix) {
			return pass(ctx)
		}
		return fail(ctx, "suffix", nil)
	})
}

// OneOf requires membership in a fixed string set.
func OneOf(values ...string) validation.Validator[string] {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}
	return validation.ValidatorFunc[string](func(
		ctx validation.Context, value string,
	) validation.Report {
		if report, oversized := rejectOversizedString(ctx, value); oversized {
			return report
		}
		if _, ok := allowed[value]; ok {
			return pass(ctx)
		}
		return fail(ctx, "one_of", nil)
	})
}
