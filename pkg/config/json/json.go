// Package json provides strict, bounded JSON configuration sources.
package json

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/internal/sourceio"
)

const (
	defaultMaxBytes = 1 << 20
	defaultMaxDepth = 64
	defaultMaxKeys  = 100_000
)

// Limits bounds parser resource use. Zero values select conservative defaults.
type Limits struct {
	MaxBytes int64
	MaxDepth int
	MaxKeys  int
}

// Options configures source metadata and parser bounds.
type Options struct {
	Name      string
	Priority  int
	Sensitive bool
	Optional  bool
	Limits    Limits
}

// DuplicateKeyError reports an ambiguous JSON object key.
type DuplicateKeyError struct {
	Path string
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("decode JSON config: duplicate key at %q", e.Path)
}

type source struct {
	info   config.SourceInfo
	input  sourceio.Input
	limits Limits
}

// Bytes constructs a repeatable source from an immutable copy of data.
func Bytes(data []byte, options Options) (config.Source, error) {
	info, limits, err := validate(options)
	if err != nil {
		return nil, err
	}

	return &source{info: info, input: sourceio.Bytes(data), limits: limits}, nil
}

// FromFS constructs a repeatable source for path in filesystem.
func FromFS(filesystem fs.FS, path string, options Options) (config.Source, error) {
	info, limits, err := validate(options)
	if err != nil {
		return nil, err
	}

	input, err := sourceio.FromFS(filesystem, path)
	if err != nil {
		return nil, err
	}
	return &source{info: info, input: input, limits: limits}, nil
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

	tree, err := parse(ctx, data, s.limits)
	if err != nil {
		return config.Document{}, err
	}

	return config.Document{Tree: tree}, nil
}

func validate(options Options) (config.SourceInfo, Limits, error) {
	if strings.TrimSpace(options.Name) == "" {
		return config.SourceInfo{}, Limits{}, errors.New("JSON source name must not be empty")
	}
	if options.Limits.MaxBytes < 0 || options.Limits.MaxDepth < 0 || options.Limits.MaxKeys < 0 {
		return config.SourceInfo{}, Limits{}, errors.New("JSON source limits must not be negative")
	}

	limits := options.Limits
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxKeys == 0 {
		limits.MaxKeys = defaultMaxKeys
	}

	return config.SourceInfo{
		Name:      options.Name,
		Priority:  options.Priority,
		Sensitive: options.Sensitive,
		Optional:  options.Optional,
	}, limits, nil
}

func parse(ctx context.Context, data []byte, limits Limits) (map[string]any, error) {
	decoder := stdjson.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	keys := 0
	value, err := parseValue(ctx, decoder, 1, "", limits, &keys)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode JSON config: multiple root values")
		}
		return nil, fmt.Errorf("decode JSON config: %w", err)
	}
	tree, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("decode JSON config: root must be an object")
	}
	return tree, nil
}

func parseValue(
	ctx context.Context,
	decoder *stdjson.Decoder,
	depth int,
	path string,
	limits Limits,
	keys *int,
) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > limits.MaxDepth {
		return nil, fmt.Errorf("decode JSON config: depth exceeds %d at %q", limits.MaxDepth, path)
	}

	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode JSON config at %q: %w", path, err)
	}
	delimiter, compound := token.(stdjson.Delim)
	if !compound {
		return scalar(token, path)
	}

	if delimiter == '{' {
		object := make(map[string]any)
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return nil, fmt.Errorf("decode JSON config at %q: %w", path, err)
			}
			name := nameToken.(string)
			childPath := join(path, name)
			if _, exists := object[name]; exists {
				return nil, &DuplicateKeyError{Path: childPath}
			}
			*keys++
			if *keys > limits.MaxKeys {
				return nil, fmt.Errorf("decode JSON config: keys exceed %d at %q", limits.MaxKeys, childPath)
			}
			value, err := parseValue(ctx, decoder, depth+1, childPath, limits, keys)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, fmt.Errorf("decode JSON config at %q: %w", path, err)
		}
		return object, nil
	}

	// At a value boundary, encoding/json only returns an opening object or
	// array delimiter. The object case is handled above, so this is an array.
	items := make([]any, 0)
	for index := 0; decoder.More(); index++ {
		value, err := parseValue(
			ctx,
			decoder,
			depth+1,
			fmt.Sprintf("%s[%d]", path, index),
			limits,
			keys,
		)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("decode JSON config at %q: %w", path, err)
	}
	return items, nil
}

func scalar(token any, path string) (any, error) {
	number, ok := token.(stdjson.Number)
	if !ok {
		return token, nil
	}
	text := number.String()
	if strings.ContainsAny(text, ".eE") {
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("decode JSON number at %q: invalid float", path)
		}
		return value, nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err == nil {
		return value, nil
	}
	unsigned, unsignedErr := strconv.ParseUint(text, 10, 64)
	if unsignedErr == nil {
		return unsigned, nil
	}
	return nil, fmt.Errorf("decode JSON number at %q: integer out of range", path)
}

func join(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
