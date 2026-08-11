// Package protobuf validates and canonicalizes Protocol Buffers source using
// Buf's maintained native Go compiler.
package protobuf

import (
	"context"
	"fmt"
	"maps"
	"math/bits"

	"github.com/bufbuild/protocompile"
	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Config provides an explicit root filename and immutable local import set.
type Config struct {
	Filename       string
	Imports        map[string]string
	MaxSchemaBytes int
	MaxImports     int
}

// Canonicalizer owns a copied local source set and performs no network access.
type Canonicalizer struct {
	filename       string
	imports        map[string]string
	maxSchemaBytes int
}

// New validates and copies Protobuf source configuration.
func New(config Config) (*Canonicalizer, error) {
	if config.Filename == "" || config.MaxSchemaBytes <= 0 || config.MaxImports <= 0 {
		return nil, fmt.Errorf("invalid Protobuf canonicalizer config")
	}
	if len(config.Imports) > config.MaxImports {
		return nil, fmt.Errorf("imports exceed %d", config.MaxImports)
	}
	imports := maps.Clone(config.Imports)
	if imports == nil {
		imports = make(map[string]string)
	}
	importBytes := uint64(0)
	for filename, source := range imports {
		if filename == "" || filename == config.Filename {
			return nil, fmt.Errorf("invalid Protobuf import %q", filename)
		}
		var withinLimit bool
		importBytes, withinLimit = boundedTextBytes(importBytes, len(filename), config.MaxSchemaBytes)
		if !withinLimit {
			return nil, fmt.Errorf("protobuf imports exceed %d bytes", config.MaxSchemaBytes)
		}
		importBytes, withinLimit = boundedTextBytes(importBytes, len(source), config.MaxSchemaBytes)
		if !withinLimit {
			return nil, fmt.Errorf("protobuf imports exceed %d bytes", config.MaxSchemaBytes)
		}
	}
	return &Canonicalizer{filename: config.Filename, imports: imports, maxSchemaBytes: config.MaxSchemaBytes}, nil
}

func boundedTextBytes(current uint64, additional, limit int) (uint64, bool) {
	total, carry := bits.Add64(current, uint64(additional), 0)
	return total, carry == 0 && total <= uint64(limit)
}

// Canonicalize compiles the root and explicit imports, then deterministically
// marshals the linked file descriptor set.
func (canonicalizer *Canonicalizer) Canonicalize(
	ctx context.Context,
	definition schemaregistry.Definition,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if definition.Format != schemaregistry.FormatProtobuf {
		return nil, fmt.Errorf("unsupported format %q", definition.Format)
	}
	if len(definition.Content) > canonicalizer.maxSchemaBytes {
		return nil, fmt.Errorf("schema exceeds %d bytes", canonicalizer.maxSchemaBytes)
	}
	sources := maps.Clone(canonicalizer.imports)
	sources[canonicalizer.filename] = string(definition.Content)
	compiler := protocompile.Compiler{
		Resolver:       &protocompile.SourceResolver{Accessor: protocompile.SourceAccessorFromMap(sources)},
		MaxParallelism: 1,
	}
	files, err := compiler.Compile(ctx, canonicalizer.filename)
	if err != nil {
		return nil, fmt.Errorf("compile Protobuf schema: %w", err)
	}
	set := &descriptorpb.FileDescriptorSet{File: make([]*descriptorpb.FileDescriptorProto, 0, len(files))}
	for _, file := range files {
		set.File = append(set.File, protodesc.ToFileDescriptorProto(file))
	}
	return marshalDescriptors(set)
}

func marshalDescriptors(set *descriptorpb.FileDescriptorSet) ([]byte, error) {
	// A linked FileDescriptorSet contains no required proto2 fields, so the
	// maintained protobuf implementation cannot reject this concrete message.
	canonical, _ := (proto.MarshalOptions{Deterministic: true}).Marshal(set)
	return canonical, nil
}
