package jsonschema

import "fmt"

// Dialect identifies a released JSON Schema Core and Validation dialect.
type Dialect string

const (
	// Draft3 identifies JSON Schema Draft 3.
	Draft3 Dialect = "http://json-schema.org/draft-03/schema#"
	// Draft4 identifies JSON Schema Draft 4.
	Draft4 Dialect = "http://json-schema.org/draft-04/schema#"
	// Draft6 identifies JSON Schema Draft 6.
	Draft6 Dialect = "http://json-schema.org/draft-06/schema#"
	// Draft7 identifies JSON Schema Draft 7.
	Draft7 Dialect = "http://json-schema.org/draft-07/schema#"
	// Draft201909 identifies JSON Schema Draft 2019-09.
	Draft201909 Dialect = "https://json-schema.org/draft/2019-09/schema"
	// Draft202012 identifies JSON Schema Draft 2020-12.
	Draft202012 Dialect = "https://json-schema.org/draft/2020-12/schema"
)

func (dialect Dialect) validate() error {
	switch dialect {
	case Draft3, Draft4, Draft6, Draft7, Draft201909, Draft202012:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
}

func (dialect Dialect) supportsBooleanSchemas() bool {
	switch dialect {
	case Draft6, Draft7, Draft201909, Draft202012:
		return true
	default:
		return false
	}
}

func (dialect Dialect) usesMathematicalIntegers() bool {
	switch dialect {
	case Draft6, Draft7, Draft201909, Draft202012:
		return true
	default:
		return false
	}
}

func (dialect Dialect) referenceReplacesSiblings() bool {
	switch dialect {
	case Draft3, Draft4, Draft6, Draft7:
		return true
	default:
		return false
	}
}
