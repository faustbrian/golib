// Package apiquerypgx translates reviewed plans into bounded PostgreSQL query
// primitives. It does not execute queries, own joins, or accept raw SQL.
package apiquerypgx

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

// ErrInvalid reports an incomplete or unsafe application-owned mapping.
var ErrInvalid = errors.New("PostgreSQL query mapping is invalid")

// Mapping binds public capability names to reviewed PostgreSQL identifiers.
type Mapping struct {
	Fields      map[string]string
	Filters     map[string]string
	Sorts       map[string]string
	Constraints map[string]string
}

// Compiler is an immutable allowlisted PostgreSQL primitive compiler.
type Compiler struct{ mapping Mapping }

// QueryParts contains fragments for an application-owned SQL statement.
// Arguments remain typed and must be bound positionally in the listed order.
type QueryParts struct {
	Projection string
	Where      string
	OrderBy    string
	Arguments  []apiquery.Value
}

// NewCompiler validates and snapshots every mapped identifier.
func NewCompiler(mapping Mapping) (*Compiler, error) {
	cloned := Mapping{Fields: cloneMap(mapping.Fields), Filters: cloneMap(mapping.Filters),
		Sorts: cloneMap(mapping.Sorts), Constraints: cloneMap(mapping.Constraints)}
	for _, values := range []map[string]string{cloned.Fields, cloned.Filters, cloned.Sorts, cloned.Constraints} {
		for name, identifier := range values {
			if name == "" || !validIdentifier(identifier) {
				return nil, ErrInvalid
			}
		}
	}
	return &Compiler{mapping: cloned}, nil
}

// Compile translates all execution fields, mandatory constraints, client
// filters, and sorts. A missing mapping fails closed.
func (c *Compiler) Compile(plan *apiquery.Plan) (QueryParts, error) {
	if c == nil || plan == nil {
		return QueryParts{}, ErrInvalid
	}
	parts := QueryParts{}
	projection := make([]string, 0, len(plan.ExecutionFields()))
	for _, name := range plan.ExecutionFields() {
		identifier, exists := c.mapping.Fields[name]
		if !exists {
			return QueryParts{}, ErrInvalid
		}
		projection = append(projection, quoteIdentifier(identifier))
	}
	parts.Projection = strings.Join(projection, ", ")

	where := make([]string, 0, 2)
	constraints := make([]string, 0, len(plan.MandatoryConstraints()))
	for _, constraint := range plan.MandatoryConstraints() {
		identifier, exists := c.mapping.Constraints[constraint.Name]
		if !exists {
			return QueryParts{}, ErrInvalid
		}
		parts.Arguments = append(parts.Arguments, constraint.Value)
		constraints = append(constraints, fmt.Sprintf("%s = $%d",
			quoteIdentifier(identifier), len(parts.Arguments)))
	}
	if len(constraints) > 0 {
		where = append(where, "("+strings.Join(constraints, " AND ")+")")
	}
	if filter := plan.Filter(); filter != nil {
		sql, err := c.compileFilter(filter, &parts.Arguments)
		if err != nil {
			return QueryParts{}, err
		}
		where = append(where, "("+sql+")")
	}
	parts.Where = strings.Join(where, " AND ")

	order := make([]string, 0, len(plan.Sorts()))
	for _, sort := range plan.Sorts() {
		identifier, exists := c.mapping.Sorts[sort.Name]
		if !exists {
			return QueryParts{}, ErrInvalid
		}
		term := quoteIdentifier(identifier) + " " + strings.ToUpper(string(sort.Direction))
		if sort.Nulls != "" {
			term += " NULLS " + strings.ToUpper(string(sort.Nulls))
		}
		order = append(order, term)
	}
	parts.OrderBy = strings.Join(order, ", ")
	return parts, nil
}

func (c *Compiler) compileFilter(expression *apiquery.FilterExpr, arguments *[]apiquery.Value) (string, error) {
	if expression.Predicate != nil {
		predicate := expression.Predicate
		identifier, exists := c.mapping.Filters[predicate.Name]
		if !exists {
			return "", ErrInvalid
		}
		column := quoteIdentifier(identifier)
		switch predicate.Operator {
		case apiquery.OpIsNull:
			return column + " IS NULL", nil
		case apiquery.OpBetween:
			first := addArgument(arguments, predicate.Values[0])
			second := addArgument(arguments, predicate.Values[1])
			return fmt.Sprintf("%s BETWEEN $%d AND $%d", column, first, second), nil
		case apiquery.OpIn, apiquery.OpNotIn:
			placeholders := make([]string, len(predicate.Values))
			for index, value := range predicate.Values {
				placeholders[index] = fmt.Sprintf("$%d", addArgument(arguments, value))
			}
			operator := " IN "
			if predicate.Operator == apiquery.OpNotIn {
				operator = " NOT IN "
			}
			return column + operator + "(" + strings.Join(placeholders, ", ") + ")", nil
		case apiquery.OpContains, apiquery.OpStartsWith, apiquery.OpEndsWith:
			pattern := escapeLike(predicate.Values[0].String())
			if predicate.Operator != apiquery.OpStartsWith {
				pattern = "%" + pattern
			}
			if predicate.Operator != apiquery.OpEndsWith {
				pattern += "%"
			}
			position := addArgument(arguments, apiquery.StringValue(pattern))
			return fmt.Sprintf("%s LIKE $%d ESCAPE '\\'", column, position), nil
		case apiquery.OpEqual, apiquery.OpNotEqual, apiquery.OpLess, apiquery.OpLessOrEqual,
			apiquery.OpGreater, apiquery.OpGreaterOrEqual:
			operator, _ := comparisonOperator(predicate.Operator)
			position := addArgument(arguments, predicate.Values[0])
			return fmt.Sprintf("%s %s $%d", column, operator, position), nil
		default:
			return "", ErrInvalid
		}
	}
	children := make([]string, 0, len(expression.Children))
	for index := range expression.Children {
		child, err := c.compileFilter(&expression.Children[index], arguments)
		if err != nil {
			return "", err
		}
		children = append(children, "("+child+")")
	}
	switch expression.Logic {
	case apiquery.LogicOr:
		return strings.Join(children, " OR "), nil
	case apiquery.LogicNot:
		return "NOT " + children[0], nil
	case apiquery.LogicAnd:
		return strings.Join(children, " AND "), nil
	default:
		return "", ErrInvalid
	}
}

func comparisonOperator(operator apiquery.Operator) (string, bool) {
	operators := map[apiquery.Operator]string{apiquery.OpEqual: "=", apiquery.OpNotEqual: "<>",
		apiquery.OpLess: "<", apiquery.OpLessOrEqual: "<=", apiquery.OpGreater: ">",
		apiquery.OpGreaterOrEqual: ">="}
	value, exists := operators[operator]
	return value, exists
}

func addArgument(arguments *[]apiquery.Value, value apiquery.Value) int {
	*arguments = append(*arguments, value)
	return len(*arguments)
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func validIdentifier(identifier string) bool {
	parts := strings.Split(identifier, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for index, char := range part {
			if char != '_' && !unicode.IsLetter(char) && (index == 0 || !unicode.IsDigit(char)) {
				return false
			}
		}
	}
	return true
}

func quoteIdentifier(identifier string) string {
	parts := strings.Split(identifier, ".")
	for index := range parts {
		parts[index] = `"` + parts[index] + `"`
	}
	return strings.Join(parts, ".")
}

func cloneMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
