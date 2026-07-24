package parameter

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

// OptionsFor derives parameter codec options from an OpenAPI Parameter Object,
// applying the specification's location-specific style and explode defaults.
func OptionsFor(
	version specversion.Version,
	value jsonvalue.Value,
) (Options, error) {
	options, err := optionsFor(version, value)
	if err != nil {
		return Options{}, err
	}
	schema, exists := value.Lookup("schema")
	return optionsWithSchema(options, schema, exists), nil
}

// OptionsForResolvedSchema derives parameter codec options using a
// caller-resolved Schema Object. It allows reference-aware callers to apply
// allowEmptyValue only when the resolved shape has defined serialization for
// the selected style.
func OptionsForResolvedSchema(
	version specversion.Version,
	value jsonvalue.Value,
	schema jsonvalue.Value,
) (Options, error) {
	if schema.Kind() != jsonvalue.ObjectKind &&
		schema.Kind() != jsonvalue.BooleanKind {
		return Options{}, fmt.Errorf("%w: resolved parameter schema", ErrInvalidOptions)
	}
	options, err := optionsFor(version, value)
	if err != nil {
		return Options{}, err
	}
	return optionsWithSchema(options, schema, true), nil
}

func optionsFor(
	version specversion.Version,
	value jsonvalue.Value,
) (Options, error) {
	dialect := version.Dialect()
	if value.Kind() != jsonvalue.ObjectKind ||
		(dialect != specversion.DialectOAS30 &&
			dialect != specversion.DialectOAS31 &&
			dialect != specversion.DialectOAS32) {
		return Options{}, fmt.Errorf("%w: unsupported Parameter Object", ErrInvalidOptions)
	}
	locationValue, exists := value.Lookup("in")
	locationText, valid := locationValue.Text()
	if !exists || !valid {
		return Options{}, fmt.Errorf("%w: parameter location", ErrInvalidOptions)
	}
	location := Location(locationText)
	style := defaultStyle(location)
	if style == "" {
		return Options{}, fmt.Errorf("%w: parameter location", ErrInvalidOptions)
	}
	if styleValue, exists := value.Lookup("style"); exists {
		styleText, valid := styleValue.Text()
		if !valid {
			return Options{}, fmt.Errorf("%w: parameter style", ErrInvalidOptions)
		}
		style = Style(styleText)
	}
	if !styleAllowed(location, style, dialect) {
		return Options{}, fmt.Errorf("%w: parameter style", ErrInvalidOptions)
	}
	explode := style == Form
	if explodeValue, exists := value.Lookup("explode"); exists {
		explicit, valid := explodeValue.Bool()
		if !valid {
			return Options{}, fmt.Errorf("%w: parameter explode", ErrInvalidOptions)
		}
		explode = explicit
	}
	allowReserved := false
	if reservedValue, exists := value.Lookup("allowReserved"); exists {
		explicit, valid := reservedValue.Bool()
		if !valid || location != Query {
			return Options{}, fmt.Errorf("%w: parameter allowReserved", ErrInvalidOptions)
		}
		allowReserved = explicit
	}
	emptyDecoding := RejectEmptyAmbiguity
	if emptyValue, exists := value.Lookup("allowEmptyValue"); exists {
		explicit, valid := emptyValue.Bool()
		if !valid || location != Query {
			return Options{}, fmt.Errorf("%w: parameter allowEmptyValue", ErrInvalidOptions)
		}
		if explicit {
			emptyDecoding = allowEmptyDecoding(version)
		}
	}
	return Options{
		Version: version, Location: location, Style: style,
		Explode: explode, AllowReserved: allowReserved,
		EmptyDecoding: emptyDecoding,
	}, nil
}

func optionsWithSchema(
	options Options,
	schema jsonvalue.Value,
	exists bool,
) Options {
	if options.EmptyDecoding != RejectEmptyAmbiguity &&
		!allowEmptyValueApplies(schema, exists, options) {
		options.EmptyDecoding = RejectEmptyAmbiguity
	}
	return options
}

func allowEmptyValueApplies(
	schema jsonvalue.Value,
	exists bool,
	options Options,
) bool {
	if !exists || schema.Kind() != jsonvalue.ObjectKind {
		return true
	}
	typeValue, exists := schema.Lookup("type")
	if !exists {
		return true
	}
	typeName, valid := typeValue.Text()
	if !valid {
		return true
	}
	kind := jsonvalue.StringKind
	switch typeName {
	case "array":
		kind = jsonvalue.ArrayKind
	case "object":
		kind = jsonvalue.ObjectKind
	case "boolean", "integer", "number", "string":
	default:
		return true
	}
	return validOptions(kind, options)
}

func allowEmptyDecoding(version specversion.Version) EmptyDecoding {
	switch version.String() {
	case "3.0.4", "3.1.1", "3.1.2", "3.2.0":
		return EmptyAsNull
	default:
		return EmptyAsValue
	}
}

func defaultStyle(location Location) Style {
	switch location {
	case Query, CookieLocation:
		return Form
	case Path, Header:
		return Simple
	default:
		return ""
	}
}

func styleAllowed(
	location Location,
	style Style,
	dialect specversion.Dialect,
) bool {
	switch location {
	case Path:
		return style == Matrix || style == Label || style == Simple
	case Query:
		return style == Form || style == SpaceDelimited ||
			style == PipeDelimited || style == DeepObject
	case Header:
		return style == Simple
	case CookieLocation:
		return style == Form ||
			(dialect == specversion.DialectOAS32 && style == Cookie)
	default:
		return false
	}
}
