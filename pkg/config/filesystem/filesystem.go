// Package filesystem dispatches explicit and discovered files to strict format
// sources while preserving path provenance.
package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/discover"
	"github.com/faustbrian/golib/pkg/config/internal/sourceio"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
	tomlsource "github.com/faustbrian/golib/pkg/config/toml"
	yamlsource "github.com/faustbrian/golib/pkg/config/yaml"
)

const defaultMaxBytes int64 = 1 << 20

// Format identifies a structured configuration parser.
type Format uint8

const (
	FormatAuto Format = iota
	FormatJSON
	FormatYAML
	FormatTOML
)

// Limits are applied consistently to every structured format.
type Limits struct {
	MaxBytes int64
	MaxDepth int
	MaxKeys  int
}

// Options configures source metadata, format selection, and parser bounds.
type Options struct {
	Name      string
	Priority  int
	Sensitive bool
	Optional  bool
	Format    Format
	Limits    Limits
}

// OpenFunc returns a new reader for each load. The reader receives the load
// context and must not be reused after Close.
type OpenFunc func(context.Context) (io.ReadCloser, error)

// Reader constructs a repeatable context-aware reader source. Format must be
// explicit because a reader has no filename extension.
func Reader(open OpenFunc, options Options) (config.Source, error) {
	if open == nil {
		return nil, errors.New("filesystem reader factory must not be nil")
	}
	if strings.TrimSpace(options.Name) == "" {
		return nil, errors.New("filesystem source name must not be empty")
	}
	if options.Format == FormatAuto || options.Format > FormatTOML {
		return nil, errors.New("filesystem reader requires an explicit format")
	}
	if options.Limits.MaxBytes < 0 || options.Limits.MaxDepth < 0 || options.Limits.MaxKeys < 0 {
		return nil, errors.New("filesystem source limits must not be negative")
	}
	return &readerSource{
		open: open,
		info: config.SourceInfo{
			Name: options.Name, Priority: options.Priority,
			Sensitive: options.Sensitive, Optional: options.Optional,
		},
		options: options,
	}, nil
}

// FromPath constructs a source that reopens path on every load.
func FromPath(path string, options Options) (config.Source, error) {
	return fromPath(path, options, filepath.Abs)
}

func fromPath(
	path string,
	options Options,
	absolutePath func(string) (string, error),
) (config.Source, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	return fromFS(
		os.DirFS(filepath.Dir(absolute)),
		filepath.Base(absolute),
		absolute,
		options,
	)
}

// FromFS constructs a source for path in filesystem.
func FromFS(filesystem fs.FS, path string, options Options) (config.Source, error) {
	return fromFS(filesystem, path, path, options)
}

// FromDiscovered opens the canonical discovered target and records the lexical
// discovered path in snapshot provenance.
func FromDiscovered(result discover.Result, options Options) (config.Source, error) {
	return fromDiscovered(result, options, filepath.Abs)
}

func fromDiscovered(
	result discover.Result,
	options Options,
	absolutePath func(string) (string, error),
) (config.Source, error) {
	openPath := result.ResolvedPath
	if openPath == "" {
		openPath = result.Path
	}
	if openPath == "" || result.Path == "" {
		return nil, errors.New("discovered source requires path and resolved path")
	}
	absolute, err := absolutePath(openPath)
	if err != nil {
		return nil, err
	}
	return fromFS(
		os.DirFS(filepath.Dir(absolute)),
		filepath.Base(absolute),
		result.Path,
		options,
	)
}

func fromFS(filesystem fs.FS, path, location string, options Options) (config.Source, error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, errors.New("filesystem source name must not be empty")
	}
	if filesystem == nil {
		return nil, errors.New("filesystem source filesystem must not be nil")
	}
	format, err := resolveFormat(path, options.Format)
	if err != nil {
		return nil, err
	}

	var source config.Source
	switch format {
	case FormatJSON:
		source, err = jsonsource.FromFS(filesystem, path, jsonsource.Options{
			Name: options.Name, Priority: options.Priority,
			Sensitive: options.Sensitive, Optional: options.Optional,
			Limits: jsonsource.Limits{
				MaxBytes: options.Limits.MaxBytes,
				MaxDepth: options.Limits.MaxDepth,
				MaxKeys:  options.Limits.MaxKeys,
			},
		})
	case FormatYAML:
		source, err = yamlsource.FromFS(filesystem, path, yamlsource.Options{
			Name: options.Name, Priority: options.Priority,
			Sensitive: options.Sensitive, Optional: options.Optional,
			Limits: yamlsource.Limits{
				MaxBytes: options.Limits.MaxBytes,
				MaxDepth: options.Limits.MaxDepth,
				MaxKeys:  options.Limits.MaxKeys,
			},
		})
	case FormatTOML:
		source, err = tomlsource.FromFS(filesystem, path, tomlsource.Options{
			Name: options.Name, Priority: options.Priority,
			Sensitive: options.Sensitive, Optional: options.Optional,
			Limits: tomlsource.Limits{
				MaxBytes: options.Limits.MaxBytes,
				MaxDepth: options.Limits.MaxDepth,
				MaxKeys:  options.Limits.MaxKeys,
			},
		})
	}
	if err != nil {
		return nil, err
	}
	return locationSource{source: source, location: location}, nil
}

func resolveFormat(path string, configured Format) (Format, error) {
	if configured > FormatTOML {
		return FormatAuto, errors.New("filesystem source format is invalid")
	}
	if configured != FormatAuto {
		return configured, nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON, nil
	case ".yaml", ".yml":
		return FormatYAML, nil
	case ".toml":
		return FormatTOML, nil
	default:
		return FormatAuto, fmt.Errorf("filesystem source path %q has unsupported extension", path)
	}
}

type locationSource struct {
	source   config.Source
	location string
}

func (s locationSource) Info() config.SourceInfo { return s.source.Info() }

func (s locationSource) Load(ctx context.Context) (config.Document, error) {
	document, err := s.source.Load(ctx)
	if err != nil {
		return config.Document{}, err
	}
	if document.Origins == nil {
		document.Origins = make(map[string]config.Origin)
	}
	markLocation(document.Origins, document.Tree, "", s.location)
	return document, nil
}

func markLocation(origins map[string]config.Origin, tree map[string]any, parent, location string) {
	for key, value := range tree {
		path := key
		if parent != "" {
			path = parent + "." + key
		}
		origin, exists := origins[path]
		if !exists {
			origin = config.Origin{Present: true, State: config.Present}
			if value == nil {
				origin.State = config.Null
			}
		}
		origin.Location = location
		origins[path] = origin
		if object, ok := value.(map[string]any); ok {
			markLocation(origins, object, path, location)
		}
	}
}

type readerSource struct {
	open    OpenFunc
	info    config.SourceInfo
	options Options
}

func (s *readerSource) Info() config.SourceInfo { return s.info }

func (s *readerSource) Load(ctx context.Context) (config.Document, error) {
	if err := ctx.Err(); err != nil {
		return config.Document{}, err
	}
	reader, err := s.open(ctx)
	if err != nil {
		return config.Document{}, err
	}
	if reader == nil {
		return config.Document{}, errors.New("filesystem reader factory returned nil")
	}
	maxBytes := s.options.Limits.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxBytes
	}
	data, readErr := sourceio.Read(ctx, reader, maxBytes)
	closeErr := reader.Close()
	if readErr != nil {
		return config.Document{}, readErr
	}
	if closeErr != nil {
		return config.Document{}, closeErr
	}

	var parser config.Source
	switch s.options.Format {
	case FormatJSON:
		parser, _ = jsonsource.Bytes(data, jsonsource.Options{
			Name: s.info.Name, Priority: s.info.Priority,
			Sensitive: s.info.Sensitive, Optional: s.info.Optional,
			Limits: jsonsource.Limits{
				MaxBytes: maxBytes, MaxDepth: s.options.Limits.MaxDepth,
				MaxKeys: s.options.Limits.MaxKeys,
			},
		})
	case FormatYAML:
		parser, _ = yamlsource.Bytes(data, yamlsource.Options{
			Name: s.info.Name, Priority: s.info.Priority,
			Sensitive: s.info.Sensitive, Optional: s.info.Optional,
			Limits: yamlsource.Limits{
				MaxBytes: maxBytes, MaxDepth: s.options.Limits.MaxDepth,
				MaxKeys: s.options.Limits.MaxKeys,
			},
		})
	case FormatTOML:
		parser, _ = tomlsource.Bytes(data, tomlsource.Options{
			Name: s.info.Name, Priority: s.info.Priority,
			Sensitive: s.info.Sensitive, Optional: s.info.Optional,
			Limits: tomlsource.Limits{
				MaxBytes: maxBytes, MaxDepth: s.options.Limits.MaxDepth,
				MaxKeys: s.options.Limits.MaxKeys,
			},
		})
	default:
		return config.Document{}, errors.New("filesystem reader format is invalid")
	}
	// Reader validates the format and limits before constructing this source,
	// so the corresponding in-memory parser cannot reject its options.
	return parser.Load(ctx)
}
