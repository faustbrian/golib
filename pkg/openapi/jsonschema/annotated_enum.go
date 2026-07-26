package jsonschema

import (
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/model"
)

const defaultAnnotatedEnumMaxCases = 10_000

var (
	// ErrInvalidAnnotatedEnum reports a malformed annotated-enum candidate.
	ErrInvalidAnnotatedEnum = errors.New("invalid annotated enum")
	// ErrInvalidAnnotatedEnumOptions reports invalid recognition bounds.
	ErrInvalidAnnotatedEnumOptions = errors.New("invalid annotated enum options")
	// ErrAnnotatedEnumLimit reports that the candidate exceeds the case limit.
	ErrAnnotatedEnumLimit = errors.New("annotated enum case limit exceeded")
)

// AnnotatedEnumOptions bounds optional annotated-enum recognition.
type AnnotatedEnumOptions struct {
	// MaxCases limits the number of alternatives. Zero uses a safe default.
	MaxCases int
}

// AnnotatedEnumCase is one lossless const value and its optional annotations.
type AnnotatedEnumCase struct {
	Value       jsonvalue.Value
	Title       model.Field[string]
	Description model.Field[string]
}

// RecognizeAnnotatedEnum recognizes the optional oneOf or anyOf const pattern.
// Alternatives containing keywords other than const, title, and description
// are deliberately declined instead of being interpreted as enum cases.
func RecognizeAnnotatedEnum(
	schema jsonvalue.Value,
	options AnnotatedEnumOptions,
) ([]AnnotatedEnumCase, bool, error) {
	maxCases, err := annotatedEnumMaxCases(options)
	if err != nil {
		return nil, false, err
	}
	if schema.Kind() != jsonvalue.ObjectKind {
		return nil, false, fmt.Errorf("%w: schema must be an object", ErrInvalidAnnotatedEnum)
	}

	oneOf, hasOneOf := schema.Lookup("oneOf")
	anyOf, hasAnyOf := schema.Lookup("anyOf")
	if hasOneOf == hasAnyOf {
		return nil, false, nil
	}
	applicator := oneOf
	if hasAnyOf {
		applicator = anyOf
	}
	alternatives, valid := applicator.Elements()
	if !valid || len(alternatives) == 0 {
		return nil, false, fmt.Errorf("%w: applicator must be a non-empty array", ErrInvalidAnnotatedEnum)
	}
	if len(alternatives) > maxCases {
		return nil, false, fmt.Errorf("%w: maximum is %d", ErrAnnotatedEnumLimit, maxCases)
	}

	cases := make([]AnnotatedEnumCase, 0, len(alternatives))
	for _, alternative := range alternatives {
		annotatedCase, recognized, caseErr := recognizeAnnotatedEnumCase(alternative)
		if caseErr != nil {
			return nil, false, caseErr
		}
		if !recognized {
			return nil, false, nil
		}
		cases = append(cases, annotatedCase)
	}

	return cases, true, nil
}

func annotatedEnumMaxCases(options AnnotatedEnumOptions) (int, error) {
	if options.MaxCases < 0 {
		return 0, fmt.Errorf("%w: MaxCases cannot be negative", ErrInvalidAnnotatedEnumOptions)
	}
	if options.MaxCases == 0 {
		return defaultAnnotatedEnumMaxCases, nil
	}
	return options.MaxCases, nil
}

func recognizeAnnotatedEnumCase(
	alternative jsonvalue.Value,
) (AnnotatedEnumCase, bool, error) {
	members, valid := alternative.Members()
	if !valid {
		return AnnotatedEnumCase{}, false, nil
	}
	annotatedCase := AnnotatedEnumCase{
		Title:       model.Absent[string](),
		Description: model.Absent[string](),
	}
	hasConst := false
	for _, member := range members {
		switch member.Name {
		case "const":
			annotatedCase.Value = member.Value
			hasConst = true
		case "title":
			title, text := member.Value.Text()
			if !text {
				return AnnotatedEnumCase{}, false,
					fmt.Errorf("%w: title must be a string", ErrInvalidAnnotatedEnum)
			}
			annotatedCase.Title = model.Present(title)
		case "description":
			description, text := member.Value.Text()
			if !text {
				return AnnotatedEnumCase{}, false,
					fmt.Errorf("%w: description must be a string", ErrInvalidAnnotatedEnum)
			}
			annotatedCase.Description = model.Present(description)
		default:
			return AnnotatedEnumCase{}, false, nil
		}
	}
	if !hasConst {
		return AnnotatedEnumCase{}, false, nil
	}
	return annotatedCase, true, nil
}
