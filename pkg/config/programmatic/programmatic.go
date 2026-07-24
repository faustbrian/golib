// Package programmatic provides immutable map-backed configuration sources.
package programmatic

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	config "github.com/faustbrian/golib/pkg/config"
)

type source struct {
	info      config.SourceInfo
	tree      map[string]any
	defaulted bool
}

// Map creates a source at an explicit priority.
func Map(name string, priority int, tree map[string]any) (config.Source, error) {
	return newSource(name, priority, tree, false)
}

// Defaults creates a lowest-precedence source whose provenance is defaulted.
func Defaults(name string, tree map[string]any) (config.Source, error) {
	return newSource(name, config.PriorityDefaults, tree, true)
}

// Overrides creates a highest-precedence explicit override source.
func Overrides(name string, tree map[string]any) (config.Source, error) {
	return newSource(name, config.PriorityOverrides, tree, false)
}

func newSource(name string, priority int, tree map[string]any, defaulted bool) (config.Source, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("programmatic source name must not be empty")
	}
	normalized, err := normalizeMap(tree)
	if err != nil {
		return nil, err
	}
	return &source{
		info:      config.SourceInfo{Name: name, Priority: priority},
		tree:      normalized,
		defaulted: defaulted,
	}, nil
}

func normalizeMap(value map[string]any) (map[string]any, error) {
	normalized := make(map[string]any, len(value))
	for key, item := range value {
		converted, err := normalizeValue(item, key)
		if err != nil {
			return nil, err
		}
		normalized[key] = converted
	}
	return normalized, nil
}

func normalizeValue(value any, path string) (any, error) {
	if value == nil {
		return nil, nil
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Map:
		if reflected.IsNil() {
			return nil, nil
		}
		if reflected.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("programmatic config at %q: maps require string keys", path)
		}
		object := make(map[string]any, reflected.Len())
		iterator := reflected.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			childPath := path + "." + key
			converted, err := normalizeValue(iterator.Value().Interface(), childPath)
			if err != nil {
				return nil, err
			}
			object[key] = converted
		}
		return object, nil
	case reflect.Slice:
		if reflected.IsNil() {
			return nil, nil
		}
		fallthrough
	case reflect.Array:
		items := make([]any, reflected.Len())
		for index := 0; index < reflected.Len(); index++ {
			converted, err := normalizeValue(
				reflected.Index(index).Interface(),
				fmt.Sprintf("%s[%d]", path, index),
			)
			if err != nil {
				return nil, err
			}
			items[index] = converted
		}
		return items, nil
	default:
		switch reflected.Kind() {
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
			reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
			reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64,
			reflect.String:
			return value, nil
		default:
			return nil, fmt.Errorf(
				"programmatic config at %q: unsupported value type %s",
				path,
				reflected.Type(),
			)
		}
	}
}

func (s *source) Info() config.SourceInfo { return s.info }

func (s *source) Load(ctx context.Context) (config.Document, error) {
	if err := ctx.Err(); err != nil {
		return config.Document{}, err
	}
	tree := cloneMap(s.tree)
	document := config.Document{Tree: tree}
	if s.defaulted {
		document.Origins = make(map[string]config.Origin)
		markDefaulted(document.Origins, tree, "")
	}
	return document, nil
}

func markDefaulted(origins map[string]config.Origin, tree map[string]any, parent string) {
	for key, value := range tree {
		path := key
		if parent != "" {
			path = parent + "." + key
		}
		origins[path] = config.Origin{Present: true, State: config.Defaulted}
		if object, ok := value.(map[string]any); ok {
			markDefaulted(origins, object, path)
		}
	}
}

func cloneMap(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = cloneValue(item)
	}
	return clone
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case []any:
		clone := make([]any, len(value))
		for index, item := range value {
			clone[index] = cloneValue(item)
		}
		return clone
	default:
		return value
	}
}
