// Package diff performs deterministic semantic compatibility comparisons of
// OpenRPC documents without resolving external resources implicitly.
package diff

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
)

// Classification describes the compatibility impact of one change.
type Classification string

const (
	Additive    Classification = "additive"
	Compatible  Classification = "compatible"
	Conditional Classification = "conditionally-compatible"
	Breaking    Classification = "breaking"
)

// Code is a stable machine-readable change identifier.
type Code string

const (
	CodeMethodAdded            Code = "method.added"
	CodeMethodRemoved          Code = "method.removed"
	CodeParameterAddedRequired Code = "parameter.added.required"
	CodeParameterAddedOptional Code = "parameter.added.optional"
	CodeParameterRemoved       Code = "parameter.removed"
	CodeParameterOrderChanged  Code = "parameter.order.changed"
	CodeParameterRequired      Code = "parameter.required.changed"
	CodeParameterSchemaChanged Code = "parameter.schema.changed"
	CodeResultAdded            Code = "result.added"
	CodeResultRemoved          Code = "result.removed"
	CodeResultChanged          Code = "result.changed"
	CodeErrorsChanged          Code = "errors.changed"
	CodeServersChanged         Code = "servers.changed"
	CodeLinksChanged           Code = "links.changed"
	CodeExamplesChanged        Code = "examples.changed"
	CodeSchemaChanged          Code = "schema.changed"
	CodeComponentAdded         Code = "component.added"
	CodeComponentRemoved       Code = "component.removed"
	CodeComponentChanged       Code = "component.changed"
	CodeUnresolvedReference    Code = "reference.unresolved"
)

var ErrInvalidOptions = errors.New("diff: invalid options")

// Options bounds diff complexity and output.
type Options struct {
	MaxChanges    int
	MaxMethods    int
	MaxComponents int
}

// DefaultOptions returns finite comparison bounds.
func DefaultOptions() Options {
	return Options{MaxChanges: 10_000, MaxMethods: 10_000, MaxComponents: 100_000}
}

// Change is one stable compatibility finding.
type Change struct {
	Code           Code
	Classification Classification
	Pointer        string
	Message        string
}

// Report is one immutable bounded diff result.
type Report struct {
	changes   []Change
	truncated bool
	err       error
}

func (report Report) Changes() []Change { return append([]Change(nil), report.changes...) }
func (report Report) Truncated() bool   { return report.truncated }
func (report Report) Err() error        { return report.err }

// Compatible reports that comparison completed without breaking,
// conditional, or truncated findings. It fails closed when the available
// evidence cannot prove compatibility.
func (report Report) Compatible() bool {
	if report.err != nil || report.truncated {
		return false
	}
	for _, change := range report.changes {
		if change.Classification == Breaking || change.Classification == Conditional {
			return false
		}
	}
	return true
}

// Compare classifies method and parameter changes. Reference-dependent
// comparisons remain explicitly conditional until callers supply resolved
// documents.
func Compare(ctx context.Context, before openrpc.Document, after openrpc.Document, options Options) Report {
	if ctx == nil || options.MaxChanges <= 0 || options.MaxMethods <= 0 ||
		options.MaxComponents <= 0 {
		return Report{err: ErrInvalidOptions}
	}
	if err := ctx.Err(); err != nil {
		return Report{err: err}
	}
	beforeMethods, beforeReferences := methodsByName(before.Methods())
	afterMethods, afterReferences := methodsByName(after.Methods())
	if len(beforeMethods)+len(afterMethods)+beforeReferences+afterReferences > options.MaxMethods {
		return Report{err: ErrInvalidOptions}
	}
	changes := make([]Change, 0)
	beforeRawMethods := rawMethodsByName(before)
	afterRawMethods := rawMethodsByName(after)
	for _, name := range unionNames(beforeMethods, afterMethods) {
		if err := ctx.Err(); err != nil {
			return Report{err: err}
		}
		oldMethod, existed := beforeMethods[name]
		newMethod, exists := afterMethods[name]
		base := "#/methods/" + escape(name)
		switch {
		case !existed:
			changes = append(changes, Change{Code: CodeMethodAdded, Classification: Additive, Pointer: base, Message: "method was added"})
		case !exists:
			changes = append(changes, Change{Code: CodeMethodRemoved, Classification: Breaking, Pointer: base, Message: "method was removed"})
		default:
			changes = append(changes, compareParameters(base, oldMethod, newMethod)...)
			changes = append(changes, compareMethodSurfaces(
				base, beforeRawMethods[name], afterRawMethods[name],
			)...)
		}
	}
	rootChanges, componentCount := compareRootSurfaces(before, after)
	if componentCount > options.MaxComponents {
		return Report{err: ErrInvalidOptions}
	}
	changes = append(changes, rootChanges...)
	for range beforeReferences {
		changes = append(changes, Change{
			Code: CodeUnresolvedReference, Classification: Conditional,
			Pointer: "#/methods", Message: "method reference requires resolved comparison",
		})
	}
	for range afterReferences {
		changes = append(changes, Change{
			Code: CodeUnresolvedReference, Classification: Conditional,
			Pointer: "#/methods", Message: "method reference requires resolved comparison",
		})
	}
	sort.SliceStable(changes, func(left int, right int) bool {
		return strings.Compare(changes[left].Pointer, changes[right].Pointer) == -1
	})
	truncated := len(changes) > options.MaxChanges
	if truncated {
		changes = changes[:options.MaxChanges]
	}
	return Report{changes: changes, truncated: truncated}
}

func compareMethodSurfaces(base string, before map[string]json.RawMessage, after map[string]json.RawMessage) []Change {
	changes := make([]Change, 0, 5)
	oldResult, hadResult := before["result"]
	newResult, hasResult := after["result"]
	switch {
	case !hadResult && hasResult:
		changes = append(changes, Change{Code: CodeResultAdded, Classification: Additive, Pointer: base + "/result", Message: "method result was added"})
	case hadResult && !hasResult:
		changes = append(changes, Change{Code: CodeResultRemoved, Classification: Breaking, Pointer: base + "/result", Message: "method became notification-only"})
	case hadResult && !sameJSON(oldResult, newResult):
		changes = append(changes, Change{Code: CodeResultChanged, Classification: Conditional, Pointer: base + "/result", Message: "method result contract changed"})
	}
	changes = appendCollectionChange(changes, before, after, "errors", CodeErrorsChanged, Conditional, base+"/errors", "method errors changed")
	changes = appendFieldChange(changes, before, after, "servers", CodeServersChanged, Breaking, base+"/servers", "method servers changed")
	changes = appendCollectionChange(changes, before, after, "links", CodeLinksChanged, Compatible, base+"/links", "method links changed")
	changes = appendCollectionChange(changes, before, after, "examples", CodeExamplesChanged, Compatible, base+"/examples", "method examples changed")
	return changes
}

func appendCollectionChange(
	changes []Change,
	before map[string]json.RawMessage,
	after map[string]json.RawMessage,
	field string,
	code Code,
	classification Classification,
	pointer string,
	message string,
) []Change {
	oldValue, oldPresent := before[field]
	newValue, newPresent := after[field]
	if oldPresent != newPresent || oldPresent && !sameJSONMultiset(oldValue, newValue) {
		changes = append(changes, Change{Code: code, Classification: classification, Pointer: pointer, Message: message})
	}
	return changes
}

func sameJSONMultiset(left []byte, right []byte) bool {
	canonicalItems := func(input []byte) ([]string, bool) {
		var items []json.RawMessage
		if json.Unmarshal(input, &items) != nil {
			return nil, false
		}
		values := make([]string, len(items))
		for index, item := range items {
			values[index] = string(canonicalJSON(item))
		}
		sort.Strings(values)
		return values, true
	}
	leftItems, leftOK := canonicalItems(left)
	rightItems, rightOK := canonicalItems(right)
	return leftOK && rightOK && sameStrings(leftItems, rightItems)
}

func appendFieldChange(
	changes []Change,
	before map[string]json.RawMessage,
	after map[string]json.RawMessage,
	field string,
	code Code,
	classification Classification,
	pointer string,
	message string,
) []Change {
	oldValue, oldPresent := before[field]
	newValue, newPresent := after[field]
	if oldPresent != newPresent || oldPresent && !sameJSON(oldValue, newValue) {
		changes = append(changes, Change{Code: code, Classification: classification, Pointer: pointer, Message: message})
	}
	return changes
}

func compareRootSurfaces(before openrpc.Document, after openrpc.Document) ([]Change, int) {
	changes := make([]Change, 0)
	if !sameStrings(serverURLs(before.EffectiveServers()), serverURLs(after.EffectiveServers())) {
		changes = append(changes, Change{Code: CodeServersChanged, Classification: Breaking, Pointer: "#/servers", Message: "document servers changed"})
	}
	beforeComponents := rawComponents(before)
	afterComponents := rawComponents(after)
	count := 0
	for _, kind := range unionNames(beforeComponents, afterComponents) {
		oldEntries := rawObject(beforeComponents[kind])
		newEntries := rawObject(afterComponents[kind])
		count += len(oldEntries) + len(newEntries)
		for _, name := range unionNames(oldEntries, newEntries) {
			oldValue, existed := oldEntries[name]
			newValue, exists := newEntries[name]
			pointer := "#/components/" + escape(kind) + "/" + escape(name)
			switch {
			case !existed:
				changes = append(changes, Change{Code: CodeComponentAdded, Classification: Additive, Pointer: pointer, Message: "component was added"})
			case !exists:
				changes = append(changes, Change{Code: CodeComponentRemoved, Classification: Conditional, Pointer: pointer, Message: "component was removed"})
			case !sameJSON(oldValue, newValue):
				code := CodeComponentChanged
				if kind == "schemas" {
					code = CodeSchemaChanged
				}
				changes = append(changes, Change{Code: code, Classification: Conditional, Pointer: pointer, Message: "component contract changed"})
			}
		}
	}
	return changes, count
}

func rawMethodsByName(document openrpc.Document) map[string]map[string]json.RawMessage {
	root := rawDocument(document)
	var methods []map[string]json.RawMessage
	_ = json.Unmarshal(root["methods"], &methods)
	result := make(map[string]map[string]json.RawMessage)
	for _, method := range methods {
		var name string
		if json.Unmarshal(method["name"], &name) == nil && name != "" {
			result[name] = method
		}
	}
	return result
}

func rawComponents(document openrpc.Document) map[string]json.RawMessage {
	root := rawDocument(document)
	return rawObject(root["components"])
}

func rawDocument(document openrpc.Document) map[string]json.RawMessage {
	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		return nil
	}
	return rawObject(encoded)
}

func rawObject(input []byte) map[string]json.RawMessage {
	var object map[string]json.RawMessage
	_ = json.Unmarshal(input, &object)
	return object
}

func serverURLs(servers []openrpc.Server) []string {
	values := make([]string, len(servers))
	for index, server := range servers {
		values[index] = server.URL()
	}
	return values
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func compareParameters(base string, before openrpc.Method, after openrpc.Method) []Change {
	beforeStructure, _ := before.ParamStructure()
	afterStructure, _ := after.ParamStructure()
	if beforeStructure == openrpc.ParamStructureByPosition || afterStructure == openrpc.ParamStructureByPosition {
		if !sameParameterOrder(before.Params(), after.Params()) {
			return []Change{{
				Code: CodeParameterOrderChanged, Classification: Breaking,
				Pointer: base + "/params", Message: "positional parameter order changed",
			}}
		}
	}
	beforeParams, beforeRefs := descriptorsByName(before.Params())
	afterParams, afterRefs := descriptorsByName(after.Params())
	changes := make([]Change, 0)
	for _, name := range unionNames(beforeParams, afterParams) {
		oldParam, existed := beforeParams[name]
		newParam, exists := afterParams[name]
		pointer := base + "/params/" + escape(name)
		switch {
		case !existed:
			if newParam.RequiredOrDefault() {
				changes = append(changes, Change{Code: CodeParameterAddedRequired, Classification: Breaking, Pointer: pointer, Message: "required parameter was added"})
			} else {
				changes = append(changes, Change{Code: CodeParameterAddedOptional, Classification: Additive, Pointer: pointer, Message: "optional parameter was added"})
			}
		case !exists:
			changes = append(changes, Change{Code: CodeParameterRemoved, Classification: Breaking, Pointer: pointer, Message: "parameter was removed"})
		default:
			if !oldParam.RequiredOrDefault() && newParam.RequiredOrDefault() {
				changes = append(changes, Change{Code: CodeParameterRequired, Classification: Breaking, Pointer: pointer + "/required", Message: "parameter became required"})
			}
			if !sameJSON(oldParam.Schema().Bytes(), newParam.Schema().Bytes()) {
				changes = append(changes, Change{Code: CodeParameterSchemaChanged, Classification: Conditional, Pointer: pointer + "/schema", Message: "parameter schema changed"})
			}
		}
	}
	for range beforeRefs + afterRefs {
		changes = append(changes, Change{Code: CodeUnresolvedReference, Classification: Conditional, Pointer: base + "/params", Message: "parameter reference requires resolved comparison"})
	}
	return changes
}

func sameJSON(left []byte, right []byte) bool {
	return bytes.Equal(canonicalJSON(left), canonicalJSON(right))
}

func canonicalJSON(input []byte) []byte {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	encoded, _ := json.Marshal(value)
	return encoded
}

func methodsByName(values []openrpc.MethodOrReference) (map[string]openrpc.Method, int) {
	methods := make(map[string]openrpc.Method)
	references := 0
	for _, value := range values {
		if method, ok := value.Method(); ok {
			methods[method.Name()] = method
		} else {
			references++
		}
	}
	return methods, references
}

func descriptorsByName(values []openrpc.ContentDescriptorOrReference) (map[string]openrpc.ContentDescriptor, int) {
	descriptors := make(map[string]openrpc.ContentDescriptor)
	references := 0
	for _, value := range values {
		if descriptor, ok := value.Descriptor(); ok {
			descriptors[descriptor.Name()] = descriptor
		} else {
			references++
		}
	}
	return descriptors, references
}

func sameParameterOrder(before []openrpc.ContentDescriptorOrReference, after []openrpc.ContentDescriptorOrReference) bool {
	if len(before) != len(after) {
		return false
	}
	for index := range before {
		oldDescriptor, oldOK := before[index].Descriptor()
		newDescriptor, newOK := after[index].Descriptor()
		if !oldOK || !newOK || oldDescriptor.Name() != newDescriptor.Name() {
			return false
		}
	}
	return true
}

func unionNames[T any](left map[string]T, right map[string]T) []string {
	names := make(map[string]struct{})
	for name := range left {
		names[name] = struct{}{}
	}
	for name := range right {
		names[name] = struct{}{}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
