package diff

import (
	"context"
	"errors"
	"sort"
	"strings"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

// ErrResolvedComparison reports a document that could not be resolved and
// semantically validated for comparison.
var ErrResolvedComparison = errors.New("diff: resolved document is invalid")

// CompareResolved resolves both documents under caller-owned policy before
// comparing them. Resolution failures remain conditional changes at their
// original source pointers; other invalid documents return an execution error.
func CompareResolved(
	ctx context.Context,
	before openrpc.Document,
	after openrpc.Document,
	beforeBase string,
	afterBase string,
	resolver *reference.Resolver,
	resolvedOptions validate.ResolvedOptions,
	options Options,
) Report {
	if ctx == nil || options.MaxChanges <= 0 || options.MaxMethods <= 0 ||
		options.MaxComponents <= 0 {
		return Report{err: ErrInvalidOptions}
	}
	resolvedBefore, beforeReport := validate.ResolveDocument(
		ctx, before, beforeBase, resolver, resolvedOptions,
	)
	resolvedAfter, afterReport := validate.ResolveDocument(
		ctx, after, afterBase, resolver, resolvedOptions,
	)
	unresolved := append(
		resolutionChanges("before", beforeReport),
		resolutionChanges("after", afterReport)...,
	)
	if len(unresolved) != 0 {
		sort.SliceStable(unresolved, func(left int, right int) bool {
			return strings.Compare(unresolved[left].Pointer, unresolved[right].Pointer) == -1
		})
		if len(unresolved) > options.MaxChanges {
			return Report{changes: unresolved[:options.MaxChanges], truncated: true}
		}
		return Report{changes: unresolved}
	}
	if !beforeReport.Valid() || !afterReport.Valid() {
		return Report{err: ErrResolvedComparison}
	}
	return Compare(ctx, resolvedBefore, resolvedAfter, options)
}

func resolutionChanges(side string, report validate.Report) []Change {
	changes := make([]Change, 0)
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != validate.CodeReferenceResolution {
			continue
		}
		changes = append(changes, Change{
			Code: CodeUnresolvedReference, Classification: Conditional,
			Pointer: diagnostic.Pointer,
			Message: side + " reference could not be resolved",
		})
	}
	return changes
}
