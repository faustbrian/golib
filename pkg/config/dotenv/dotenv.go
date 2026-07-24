// Package dotenv provides strict, bounded, optionally interpolated dotenv
// configuration sources. It parses data only; it never mutates os.Environ.
package dotenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/environment"
	"github.com/faustbrian/golib/pkg/config/internal/sourceio"
)

const (
	defaultMaxBytes         = 1 << 20
	defaultMaxLines         = 100_000
	defaultMaxLineBytes     = 64 << 10
	defaultMaxKeys          = 100_000
	defaultInterpolationMax = 32
	escapedDollar           = "\x00DOTENV_DOLLAR\x00"
)

// Limits bounds parser resource use. Zero values select conservative defaults.
type Limits struct {
	MaxBytes     int64
	MaxLines     int
	MaxLineBytes int
	MaxKeys      int
}

// Interpolation enables ${NAME} and ${NAME:-default} expansion.
type Interpolation struct {
	Variables        map[string]string
	IncludeFile      bool
	MaxDepth         int
	MaxExpandedBytes int
}

// Options configures source metadata, typed mapping, and parser behavior.
type Options struct {
	Name          string
	Priority      int
	Sensitive     bool
	Optional      bool
	Prefix        string
	Separator     string
	Case          environment.CaseMode
	Limits        Limits
	Interpolation *Interpolation
}

// DuplicateKeyError reports an ambiguous dotenv assignment.
type DuplicateKeyError struct {
	Name      string
	FirstLine int
	Line      int
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf(
		"decode dotenv: duplicate variable %q at line %d (first at line %d)",
		e.Name,
		e.Line,
		e.FirstLine,
	)
}

// SyntaxError reports location and category without printing source content.
type SyntaxError struct {
	Line   int
	Column int
	Reason string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("decode dotenv at %d:%d: %s", e.Line, e.Column, e.Reason)
}

// InterpolationError reports a safe variable name and failure category.
type InterpolationError struct {
	Name   string
	Reason string
}

func (e *InterpolationError) Error() string {
	return fmt.Sprintf("interpolate dotenv variable %q: %s", e.Name, e.Reason)
}

type mapper func(context.Context, []string) (config.Document, error)

type source struct {
	info          config.SourceInfo
	input         sourceio.Input
	limits        Limits
	interpolation *Interpolation
	mapValues     mapper
}

// BytesFor constructs a repeatable typed source from an immutable copy.
func BytesFor[T any](data []byte, options Options) (config.Source, error) {
	prepared, err := prepare[T](options)
	if err != nil {
		return nil, err
	}
	prepared.input = sourceio.Bytes(data)
	return prepared, nil
}

// FromFSFor constructs a repeatable typed source for path in filesystem.
func FromFSFor[T any](filesystem fs.FS, path string, options Options) (config.Source, error) {
	prepared, err := prepare[T](options)
	if err != nil {
		return nil, err
	}
	prepared.input, err = sourceio.FromFS(filesystem, path)
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func prepare[T any](options Options) (*source, error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, errors.New("dotenv source name must not be empty")
	}
	limits, err := normalizeLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	interpolation, err := normalizeInterpolation(options.Interpolation, limits)
	if err != nil {
		return nil, err
	}
	priority := options.Priority
	if priority == 0 {
		priority = config.PriorityDotenv
	}

	environmentOptions := environment.Options{
		Name: options.Name, Priority: priority, Sensitive: options.Sensitive,
		Optional: options.Optional, Prefix: options.Prefix,
		Separator: options.Separator, Case: options.Case,
	}
	prototype, err := environment.EnvironFor[T](nil, environmentOptions)
	if err != nil {
		return nil, err
	}

	return &source{
		info: prototype.Info(), limits: limits, interpolation: interpolation,
		mapValues: func(ctx context.Context, values []string) (config.Document, error) {
			// The schema and options were validated by the prototype above;
			// values do not participate in environment source construction.
			environmentSource, _ := environment.EnvironFor[T](values, environmentOptions)
			return environmentSource.Load(ctx)
		},
	}, nil
}

func (s *source) Info() config.SourceInfo { return s.info }

func (s *source) Load(ctx context.Context) (config.Document, error) {
	if err := ctx.Err(); err != nil {
		return config.Document{}, err
	}
	data, err := s.input.Read(ctx, s.limits.MaxBytes)
	if err != nil {
		return config.Document{}, err
	}

	records, err := parse(ctx, data, s.limits)
	if err != nil {
		return config.Document{}, err
	}
	if s.interpolation != nil {
		if err := interpolate(ctx, records, *s.interpolation); err != nil {
			return config.Document{}, err
		}
	}
	values := make([]string, len(records))
	for index, record := range records {
		values[index] = record.name + "=" + strings.ReplaceAll(record.value, escapedDollar, "$")
	}
	return s.mapValues(ctx, values)
}

func normalizeLimits(limits Limits) (Limits, error) {
	if limits.MaxBytes < 0 || limits.MaxLines < 0 || limits.MaxLineBytes < 0 || limits.MaxKeys < 0 {
		return Limits{}, errors.New("dotenv source limits must not be negative")
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	if limits.MaxLines == 0 {
		limits.MaxLines = defaultMaxLines
	}
	if limits.MaxLineBytes == 0 {
		limits.MaxLineBytes = defaultMaxLineBytes
	}
	if limits.MaxKeys == 0 {
		limits.MaxKeys = defaultMaxKeys
	}
	return limits, nil
}

func normalizeInterpolation(value *Interpolation, limits Limits) (*Interpolation, error) {
	if value == nil {
		return nil, nil
	}
	clone := *value
	if clone.MaxDepth < 0 || clone.MaxExpandedBytes < 0 {
		return nil, errors.New("dotenv interpolation limits must not be negative")
	}
	if clone.MaxDepth == 0 {
		clone.MaxDepth = defaultInterpolationMax
	}
	if clone.MaxExpandedBytes == 0 {
		clone.MaxExpandedBytes = int(limits.MaxBytes)
	}
	clone.Variables = cloneStrings(value.Variables)
	return &clone, nil
}

type record struct {
	name        string
	value       string
	line        int
	interpolate bool
}

func parse(ctx context.Context, data []byte, limits Limits) ([]record, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, &SyntaxError{Line: 1, Column: 1, Reason: "NUL byte is forbidden"}
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) > limits.MaxLines+1 || (len(lines) == limits.MaxLines+1 && lines[len(lines)-1] != "") {
		return nil, fmt.Errorf("dotenv input exceeds %d line limit", limits.MaxLines)
	}
	for index, line := range lines {
		if len(line) > limits.MaxLineBytes {
			return nil, fmt.Errorf("dotenv line %d exceeds %d byte limit", index+1, limits.MaxLineBytes)
		}
	}

	records := make([]record, 0, len(lines))
	firstLines := make(map[string]int)
	for index := 0; index < len(lines); index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNumber := index + 1
		line := strings.TrimSpace(lines[index])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export") {
			if len(line) == len("export") || !unicode.IsSpace(rune(line[len("export")])) {
				return nil, &SyntaxError{Line: lineNumber, Column: 1, Reason: "invalid export prefix"}
			}
			line = strings.TrimSpace(line[len("export"):])
		}

		equals := strings.IndexByte(line, '=')
		if equals < 0 {
			return nil, &SyntaxError{Line: lineNumber, Column: len(line) + 1, Reason: "missing equals sign"}
		}
		name := strings.TrimSpace(line[:equals])
		if !validName(name) {
			return nil, &SyntaxError{Line: lineNumber, Column: 1, Reason: "invalid variable name"}
		}
		if first, exists := firstLines[name]; exists {
			return nil, &DuplicateKeyError{Name: name, FirstLine: first, Line: lineNumber}
		}
		if len(records) >= limits.MaxKeys {
			return nil, fmt.Errorf("dotenv input exceeds %d key limit", limits.MaxKeys)
		}
		firstLines[name] = lineNumber

		remainder := strings.TrimLeftFunc(line[equals+1:], unicode.IsSpace)
		value, interpolationAllowed, lastLine, err := parseValue(lines, index, remainder)
		if err != nil {
			return nil, err
		}
		records = append(records, record{
			name: name, value: value, line: lineNumber, interpolate: interpolationAllowed,
		})
		index = lastLine
	}
	return records, nil
}

func parseValue(lines []string, start int, input string) (string, bool, int, error) {
	if input == "" {
		return "", true, start, nil
	}
	switch input[0] {
	case '\'':
		value, last, rest, err := quoted(lines, start, input[1:], '\'', false)
		if err != nil {
			return "", false, start, err
		}
		if !onlyComment(rest) {
			return "", false, start, &SyntaxError{Line: last + 1, Column: 1, Reason: "content after quoted value"}
		}
		return value, false, last, nil
	case '"':
		value, last, rest, err := quoted(lines, start, input[1:], '"', true)
		if err != nil {
			return "", false, start, err
		}
		if !onlyComment(rest) {
			return "", false, start, &SyntaxError{Line: last + 1, Column: 1, Reason: "content after quoted value"}
		}
		return value, true, last, nil
	default:
		value, err := unquoted(input, start+1)
		return value, true, start, err
	}
}

func quoted(
	lines []string,
	start int,
	input string,
	quote byte,
	escapes bool,
) (string, int, string, error) {
	var result strings.Builder
	for lineIndex := start; lineIndex < len(lines); lineIndex++ {
		line := input
		if lineIndex > start {
			line = lines[lineIndex]
			result.WriteByte('\n')
		}
		for index := 0; index < len(line); index++ {
			character := line[index]
			if character == quote {
				return result.String(), lineIndex, line[index+1:], nil
			}
			if escapes && character == '\\' {
				if index+1 >= len(line) {
					return "", start, "", &SyntaxError{Line: lineIndex + 1, Column: index + 1, Reason: "trailing escape"}
				}
				index++
				switch line[index] {
				case 'n':
					result.WriteByte('\n')
				case 'r':
					result.WriteByte('\r')
				case 't':
					result.WriteByte('\t')
				case '\\', '"':
					result.WriteByte(line[index])
				case '$':
					result.WriteString(escapedDollar)
				default:
					return "", start, "", &SyntaxError{Line: lineIndex + 1, Column: index + 1, Reason: "unsupported escape"}
				}
				continue
			}
			result.WriteByte(character)
		}
	}
	return "", start, "", &SyntaxError{Line: start + 1, Column: 1, Reason: "unterminated quoted value"}
}

func unquoted(input string, line int) (string, error) {
	var result strings.Builder
	for index := 0; index < len(input); index++ {
		character := input[index]
		if character == '#' && (index == 0 || unicode.IsSpace(rune(input[index-1]))) {
			break
		}
		if character == '\\' {
			if index+1 >= len(input) {
				return "", &SyntaxError{Line: line, Column: index + 1, Reason: "trailing escape"}
			}
			index++
			if input[index] == '$' {
				result.WriteString(escapedDollar)
			} else {
				result.WriteByte(input[index])
			}
			continue
		}
		result.WriteByte(character)
	}
	return strings.TrimRightFunc(result.String(), unicode.IsSpace), nil
}

func onlyComment(input string) bool {
	trimmed := strings.TrimSpace(input)
	return trimmed == "" || strings.HasPrefix(trimmed, "#")
}

func interpolate(ctx context.Context, records []record, options Interpolation) error {
	file := make(map[string]*record, len(records))
	for index := range records {
		file[records[index].name] = &records[index]
	}
	resolved := make(map[string]string, len(records))
	var resolve func(string, []string, int) (string, error)
	resolve = func(name string, stack []string, depth int) (string, error) {
		if value, exists := resolved[name]; exists {
			return value, nil
		}
		if depth > options.MaxDepth {
			return "", &InterpolationError{Name: name, Reason: "maximum depth exceeded"}
		}
		for _, ancestor := range stack {
			if ancestor == name {
				return "", &InterpolationError{Name: name, Reason: "cycle detected"}
			}
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}

		if record, exists := file[name]; options.IncludeFile && exists {
			value := record.value
			if record.interpolate {
				var err error
				value, err = expand(value, name, append(stack, name), depth, options, resolve)
				if err != nil {
					return "", err
				}
			}
			resolved[name] = value
			return value, nil
		}
		if value, exists := options.Variables[name]; exists {
			resolved[name] = value
			return value, nil
		}
		return "", &InterpolationError{Name: name, Reason: "variable is absent"}
	}

	for index := range records {
		if !records[index].interpolate {
			continue
		}
		value, err := expand(records[index].value, records[index].name, []string{records[index].name}, 1, options, resolve)
		if err != nil {
			return err
		}
		records[index].value = value
		resolved[records[index].name] = value
	}
	return nil
}

func expand(
	input string,
	owner string,
	stack []string,
	depth int,
	options Interpolation,
	resolve func(string, []string, int) (string, error),
) (string, error) {
	var result strings.Builder
	for index := 0; index < len(input); {
		if strings.HasPrefix(input[index:], escapedDollar) {
			result.WriteString(escapedDollar)
			index += len(escapedDollar)
			continue
		}
		if !strings.HasPrefix(input[index:], "${") {
			result.WriteByte(input[index])
			index++
			continue
		}
		end := strings.IndexByte(input[index+2:], '}')
		if end < 0 {
			return "", &InterpolationError{Name: owner, Reason: "unterminated expression"}
		}
		expression := input[index+2 : index+2+end]
		name, fallback, hasFallback := strings.Cut(expression, ":-")
		if !validName(name) {
			return "", &InterpolationError{Name: owner, Reason: "invalid variable name"}
		}
		value, err := resolve(name, stack, depth+1)
		if err != nil {
			var missing *InterpolationError
			if !hasFallback || !errors.As(err, &missing) || missing.Reason != "variable is absent" {
				return "", err
			}
			value, err = expand(fallback, owner, stack, depth+1, options, resolve)
			if err != nil {
				return "", err
			}
		}
		result.WriteString(value)
		if result.Len() > options.MaxExpandedBytes {
			return "", &InterpolationError{Name: owner, Reason: "expanded value exceeds limit"}
		}
		index += end + 3
	}
	if result.Len() > options.MaxExpandedBytes {
		return "", &InterpolationError{Name: owner, Reason: "expanded value exceeds limit"}
	}
	return result.String(), nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character == '_' || unicode.IsLetter(character) || (index > 0 && unicode.IsDigit(character)) {
			continue
		}
		return false
	}
	return true
}

func cloneStrings(value map[string]string) map[string]string {
	clone := make(map[string]string, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}
