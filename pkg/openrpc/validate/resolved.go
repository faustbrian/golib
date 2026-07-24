package validate

import (
	"context"
	"errors"
	"strconv"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

const (
	// CodeReferenceResolution reports a failed bounded reference operation.
	CodeReferenceResolution Code = "reference.resolution.failed"
	// CodeInvalidResolvedDocument reports a target whose replacement violates
	// the OpenRPC object shape required at its reference location.
	CodeInvalidResolvedDocument Code = "reference.target.invalid"
)

// ResolvedOptions controls bounded dereferencing, structural decoding, and
// semantic validation through Reference Objects.
type ResolvedOptions struct {
	Validation Options
	Transform  reference.TransformPolicy
	Parse      openrpcparse.Options
}

// DefaultResolvedOptions returns strict, bounded, offline-safe defaults.
func DefaultResolvedOptions() ResolvedOptions {
	return ResolvedOptions{
		Validation: DefaultOptions(),
		Transform:  reference.DefaultTransformPolicy(),
		Parse:      openrpcparse.DefaultOptions(),
	}
}

// ResolvedDocument dereferences a document through the caller-supplied
// resolver and then applies the same semantic rules used for inline objects.
// It performs no I/O beyond the resolver's explicitly configured Store.
func ResolvedDocument(
	ctx context.Context,
	document openrpc.Document,
	base string,
	resolver *reference.Resolver,
	options ResolvedOptions,
) Report {
	_, report := ResolveDocument(ctx, document, base, resolver, options)
	return report
}

// ResolveDocument returns the fully dereferenced OpenRPC object graph and its
// semantic validation report. Draft 7 schema references remain schemas and are
// compiled against explicitly loaded external resources.
func ResolveDocument(
	ctx context.Context,
	document openrpc.Document,
	base string,
	resolver *reference.Resolver,
	options ResolvedOptions,
) (openrpc.Document, Report) {
	if ctx == nil || resolver == nil {
		return openrpc.Document{}, resolutionFailure("#")
	}
	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		return openrpc.Document{}, resolvedDocumentFailure()
	}
	root, err := jsonvalue.Parse(encoded, options.Parse.JSON)
	if err != nil {
		return openrpc.Document{}, resolvedDocumentFailure()
	}
	resolved, err := reference.DereferenceSelected(
		ctx, resolver, root, base, options.Transform,
		reference.SelectorFunc(openRPCReferencePath),
	)
	if err != nil {
		pointer := "#"
		var transformError *reference.TransformError
		if errors.As(err, &transformError) {
			pointer = transformError.Pointer
		}
		return openrpc.Document{}, resolutionFailure(pointer)
	}
	parsed, err := openrpcparse.Decode(resolved.Bytes(), options.Parse)
	if err != nil {
		return openrpc.Document{}, resolvedDocumentFailure()
	}
	semantic := Document(ctx, parsed.Document(), options.Validation)
	resolvedSchemas := validateResolvedSchemas(
		ctx, parsed.Document(), resolved, base, resolver, options.Validation,
	)
	return parsed.Document(), mergeValidationReports(semantic, resolvedSchemas, options.Validation)
}

func openRPCReferencePath(path []string) bool {
	if len(path) >= 2 && path[0] == "methods" && arrayIndex(path[1]) {
		switch len(path) {
		case 2:
			return true
		case 3:
			return path[2] == "result"
		case 4:
			return arrayIndex(path[3]) && oneOf(path[2], "tags", "params", "errors", "links", "examples")
		case 5:
			return path[2] == "examples" && arrayIndex(path[3]) && path[4] == "result"
		case 6:
			return path[2] == "examples" && arrayIndex(path[3]) &&
				path[4] == "params" && arrayIndex(path[5])
		}
	}
	if len(path) >= 4 && path[0] == "components" && path[1] == "examplePairings" {
		if len(path) == 4 {
			return path[3] == "result"
		}
		return len(path) == 5 && path[3] == "params" && arrayIndex(path[4])
	}
	return false
}

func arrayIndex(value string) bool {
	index, err := strconv.Atoi(value)
	return err == nil && index >= 0 && strconv.Itoa(index) == value
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func resolutionFailure(pointer string) Report {
	return Report{diagnostics: []Diagnostic{{
		Code:          CodeReferenceResolution,
		Pointer:       pointer,
		Severity:      SeverityError,
		Specification: "https://spec.open-rpc.org/#reference-object",
		Message:       "reference resolution failed",
	}}}
}

func resolvedDocumentFailure() Report {
	return Report{diagnostics: []Diagnostic{{
		Code:          CodeInvalidResolvedDocument,
		Pointer:       "#",
		Severity:      SeverityError,
		Specification: "https://spec.open-rpc.org/#reference-object",
		Message:       "resolved document is structurally invalid",
	}}}
}
