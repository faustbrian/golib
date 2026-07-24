package expression

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// ErrUnavailable reports a runtime expression whose source value is absent.
var ErrUnavailable = errors.New("runtime expression value unavailable")

// ErrAmbiguousContext reports multiple case-insensitive header matches.
var ErrAmbiguousContext = errors.New("ambiguous runtime expression context")

// ErrInvalidContext reports malformed caller-owned context or options.
var ErrInvalidContext = errors.New("invalid runtime expression context")

// ErrLimitExceeded reports a template result beyond its configured bound.
var ErrLimitExceeded = errors.New("runtime expression limit exceeded")

// Message contains typed caller-owned values from one HTTP message. Header
// names are matched case-insensitively; query and path names are case-sensitive.
type Message struct {
	Headers map[string]string
	Query   map[string]jsonvalue.Value
	Path    map[string]jsonvalue.Value
	Body    jsonvalue.Value
}

// Context contains the runtime request and response values available to an
// expression. Evaluation reads but never mutates the supplied maps or values.
type Context struct {
	URL        string
	Method     string
	StatusCode int
	Request    Message
	Response   Message
}

// EvaluationOptions bounds embedded template evaluation.
type EvaluationOptions struct {
	// MaxOutputBytes bounds the rendered template. Zero uses the default.
	MaxOutputBytes int
}

// DefaultEvaluationOptions returns conservative bounds for untrusted data.
func DefaultEvaluationOptions() EvaluationOptions {
	return EvaluationOptions{MaxOutputBytes: 1 << 20}
}

// Evaluate resolves an expression while preserving its JSON semantic type.
func (expression Expression) Evaluate(context Context) (jsonvalue.Value, error) {
	switch expression.kind {
	case URL:
		return contextString(context.URL, "URL")
	case Method:
		return contextString(context.Method, "method")
	case StatusCode:
		if context.StatusCode == 0 {
			return jsonvalue.Value{}, unavailable("status code")
		}
		if context.StatusCode < 100 || context.StatusCode > 999 {
			return jsonvalue.Value{}, fmt.Errorf("%w: status code", ErrInvalidContext)
		}
		value, _ := jsonvalue.Number(strconv.Itoa(context.StatusCode))
		return value, nil
	case Request:
		return expression.evaluateMessage(context.Request, "request")
	case Response:
		return expression.evaluateMessage(context.Response, "response")
	default:
		return jsonvalue.Value{}, fmt.Errorf("%w: unevaluable expression", ErrInvalid)
	}
}

func (expression Expression) evaluateMessage(message Message, label string) (jsonvalue.Value, error) {
	switch expression.source {
	case Header:
		return headerValue(message.Headers, expression.name, label)
	case Query:
		return namedValue(message.Query, expression.name, label+" query")
	case Path:
		return namedValue(message.Path, expression.name, label+" path")
	case Body:
		if message.Body.Kind() == jsonvalue.InvalidKind {
			return jsonvalue.Value{}, unavailable(label + " body")
		}
		value, err := expression.pointer.Evaluate(message.Body)
		if err != nil {
			return jsonvalue.Value{}, fmt.Errorf("%w: %s body: %w", ErrUnavailable, label, err)
		}
		return value, nil
	default:
		return jsonvalue.Value{}, fmt.Errorf("%w: message source", ErrInvalid)
	}
}

func contextString(raw string, label string) (jsonvalue.Value, error) {
	if raw == "" {
		return jsonvalue.Value{}, unavailable(label)
	}
	value, err := jsonvalue.String(raw)
	if err != nil {
		return jsonvalue.Value{}, fmt.Errorf("%w: %s", ErrInvalidContext, label)
	}
	return value, nil
}

func headerValue(headers map[string]string, name string, label string) (jsonvalue.Value, error) {
	var found string
	matches := 0
	for candidate, value := range headers {
		if strings.EqualFold(candidate, name) {
			found = value
			matches++
		}
	}
	if matches == 0 {
		return jsonvalue.Value{}, unavailable(label + " header " + name)
	}
	if matches > 1 {
		return jsonvalue.Value{}, fmt.Errorf(
			"%w: %s header %s: %w",
			ErrUnavailable,
			label,
			name,
			ErrAmbiguousContext,
		)
	}
	value, err := jsonvalue.String(found)
	if err != nil {
		return jsonvalue.Value{}, fmt.Errorf("%w: %s header %s", ErrInvalidContext, label, name)
	}
	return value, nil
}

func namedValue(values map[string]jsonvalue.Value, name string, label string) (jsonvalue.Value, error) {
	value, exists := values[name]
	if !exists {
		return jsonvalue.Value{}, unavailable(label + " " + name)
	}
	if value.Kind() == jsonvalue.InvalidKind {
		return jsonvalue.Value{}, fmt.Errorf("%w: %s %s", ErrInvalidContext, label, name)
	}
	return value, nil
}

func unavailable(label string) error {
	return fmt.Errorf("%w: %s", ErrUnavailable, label)
}

// Evaluate renders a template, converting non-string semantic values to their
// deterministic JSON representation.
func (template Template) Evaluate(context Context, options EvaluationOptions) (string, error) {
	maximum := options.MaxOutputBytes
	if maximum < 0 {
		return "", fmt.Errorf("%w: negative output limit", ErrInvalidContext)
	}
	if maximum == 0 {
		maximum = DefaultEvaluationOptions().MaxOutputBytes
	}
	var result strings.Builder
	for _, part := range template.parts {
		piece := part.literal
		if part.dynamic {
			value, err := part.expression.Evaluate(context)
			if err != nil {
				return "", err
			}
			piece = templateText(value)
		}
		if len(piece) > maximum-result.Len() {
			return "", ErrLimitExceeded
		}
		result.WriteString(piece)
	}
	return result.String(), nil
}

func templateText(value jsonvalue.Value) string {
	if value.Kind() == jsonvalue.StringKind {
		text, _ := value.Text()
		return text
	}
	encoded, _ := value.MarshalJSON()
	return string(encoded)
}
