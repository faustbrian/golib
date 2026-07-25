// Package config loads deterministic layered configuration snapshots.
package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/internal/safeerror"
	"github.com/faustbrian/golib/pkg/config/merge"
	"github.com/faustbrian/golib/pkg/config/validation"
)

// SourceInfo describes a source without exposing its values.
type SourceInfo struct {
	Name      string
	Priority  int
	Sensitive bool
	Optional  bool
}

// Documented default source priorities, ordered from lowest to highest.
const (
	PriorityDefaults          = 10
	PriorityDiscoveredBase    = 20
	PriorityDiscoveredProfile = 30
	PriorityExplicitFiles     = 40
	PriorityDotenv            = 50
	PriorityEnvironment       = 60
	PriorityOverrides         = 70
)

// Document is the intermediate tree produced by a Source.
type Document struct {
	Tree    map[string]any
	Origins map[string]Origin
}

// TreeValueError reports a non-canonical value returned by a Source. It never
// formats the rejected value.
type TreeValueError struct {
	Path string
	Type string
}

// TreeCycleError reports a cyclic source tree without traversing it.
type TreeCycleError struct {
	Path string
}

func (e *TreeCycleError) Error() string {
	return fmt.Sprintf("config tree at %q: cyclic value", e.Path)
}

// TreeLimitError reports a source-tree structural bound without formatting a
// value.
type TreeLimitError struct {
	Path  string
	Kind  string
	Limit int
}

func (e *TreeLimitError) Error() string {
	return fmt.Sprintf("config tree at %q: %s exceeds %d limit", e.Path, e.Kind, e.Limit)
}

func (e *TreeValueError) Error() string {
	return fmt.Sprintf("config tree at %q: unsupported value type %s", e.Path, e.Type)
}

// SnapshotValueError reports typed state that cannot be copied without
// retaining caller-visible mutable references. It never formats the value.
type SnapshotValueError struct {
	Path string
	Type string
}

// SourceError identifies a failed source without exposing arbitrary source
// error text. Cause identity remains available through errors.Is.
type SourceError struct {
	Name  string
	Cause error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("load config source %q: source failed", e.Name)
}

func (e *SourceError) Unwrap() error {
	return safeerror.Redact(e.Cause, "config source cause redacted")
}

func (e *SourceError) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(e.Error()))
}

// MarshalText serializes only the redacted diagnostic message.
func (e *SourceError) MarshalText() ([]byte, error) {
	return []byte(e.Error()), nil
}

func (e *SnapshotValueError) Error() string {
	return fmt.Sprintf("config snapshot at %q: value type %s is not safely cloneable", e.Path, e.Type)
}

// Source loads one configuration document without mutating global state.
type Source interface {
	Info() SourceInfo
	Load(context.Context) (Document, error)
}

// ContextFS extends fs.FS with a cancellable open operation. Filesystem source
// constructors use it when implemented and otherwise call fs.FS.Open.
type ContextFS interface {
	fs.FS
	OpenContext(context.Context, string) (fs.File, error)
}

// ContextFile extends fs.File with cancellable read and metadata operations.
// Filesystem sources use these methods when implemented.
type ContextFile interface {
	fs.File
	ReadContext(context.Context, []byte) (int, error)
	StatContext(context.Context) (fs.FileInfo, error)
}

// ContextCloser exposes a cancellable close operation for remote resources.
// Filesystem sources call it with an independent bounded cleanup context and
// otherwise call Close.
type ContextCloser interface {
	CloseContext(context.Context) error
}

// GenerationFile exposes an opaque stable generation token. Filesystem
// sources compare it before and after reads to reject mixed generations.
type GenerationFile interface {
	fs.File
	GenerationContext(context.Context) (string, error)
}

// Plan is an immutable, inspectable low-to-high precedence source plan.
type Plan struct {
	sources []plannedSource
}

type plannedSource struct {
	source Source
	info   SourceInfo
}

// NewPlan validates sources and orders them from lowest to highest priority.
// Sources with equal priority retain caller order.
func NewPlan(sources ...Source) (Plan, error) {
	planned := make([]plannedSource, 0, len(sources))
	names := make(map[string]struct{}, len(sources))

	for index, source := range sources {
		if source == nil || isNilSource(source) {
			return Plan{}, fmt.Errorf("config plan source %d: nil source", index)
		}

		info := source.Info()
		if strings.TrimSpace(info.Name) == "" {
			return Plan{}, fmt.Errorf("config plan source %d: empty name", index)
		}
		if _, exists := names[info.Name]; exists {
			return Plan{}, fmt.Errorf("config plan: duplicate source name %q", info.Name)
		}
		names[info.Name] = struct{}{}
		planned = append(planned, plannedSource{source: source, info: info})
	}

	sort.SliceStable(planned, func(left, right int) bool {
		return planned[left].info.Priority < planned[right].info.Priority
	})

	return Plan{sources: planned}, nil
}

func isNilSource(source Source) bool {
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Sources returns safe metadata in resolved precedence order.
func (p Plan) Sources() []SourceInfo {
	result := make([]SourceInfo, len(p.sources))
	for index, source := range p.sources {
		result[index] = source.info
	}

	return result
}

// Origin identifies the winning source for a configuration path.
type Origin struct {
	Source     string
	Location   string
	Sensitive  bool
	Deprecated bool
	Present    bool
	State      Presence
}

// Snapshot is an immutable configuration tree and its safe provenance.
type Snapshot[T any] struct {
	value   T
	origins map[string]Origin
}

// Value returns an independent copy of the loaded value.
func (s *Snapshot[T]) Value() T {
	return cloneTyped(s.value)
}

// Origin returns provenance without exposing the field value.
func (s *Snapshot[T]) Origin(path string) (Origin, bool) {
	origin, ok := s.origins[path]
	return origin, ok
}

// LoadTree resolves a Plan atomically. Any source or merge failure returns no
// snapshot.
func LoadTree(ctx context.Context, plan Plan) (*Snapshot[map[string]any], error) {
	merged := make(map[string]any)
	origins := make(map[string]Origin)

	for _, planned := range plan.sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		document, err := planned.source.Load(ctx)
		if err != nil {
			if planned.info.Optional && errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, &SourceError{
				Name:  planned.info.Name,
				Cause: safeerror.Redact(err, "config source cause redacted"),
			}
		}
		tree, err := canonicalTree(ctx, document.Tree)
		if err != nil {
			return nil, fmt.Errorf("load config source %q: %w", planned.info.Name, err)
		}

		next, err := merge.Trees(merged, tree)
		if err != nil {
			return nil, fmt.Errorf("merge config source %q: %w", planned.info.Name, err)
		}
		merged = next
		applyOrigins(origins, tree, document.Origins, "", planned.info)
	}

	return &Snapshot[map[string]any]{value: cloneMap(merged), origins: cloneOrigins(origins)}, nil
}

// Load resolves and strictly decodes a Plan into an immutable typed snapshot.
func Load[T any](ctx context.Context, plan Plan) (*Snapshot[T], error) {
	return LoadWithValidators[T](ctx, plan)
}

// LoadWithValidators resolves, decodes, and validates a Plan atomically.
func LoadWithValidators[T any](
	ctx context.Context,
	plan Plan,
	validators ...validation.Validator[T],
) (*Snapshot[T], error) {
	tree, err := LoadTree(ctx, plan)
	if err != nil {
		return nil, err
	}

	var value T
	if err := decode.IntoContext(ctx, tree.value, &value); err != nil {
		annotateDecodeError(err, tree.origins)
		return nil, err
	}
	applyPresence(reflect.ValueOf(&value).Elem(), "", tree.origins)
	applyFieldMetadata(reflect.TypeFor[T](), "", tree.origins)
	if err := validateSnapshotValue(reflect.ValueOf(value), "", make(map[cloneVisit]bool)); err != nil {
		return nil, err
	}
	if err := validation.Run(ctx, cloneTyped(value), validators...); err != nil {
		return nil, err
	}

	return &Snapshot[T]{value: cloneTyped(value), origins: cloneOrigins(tree.origins)}, nil
}

func applyFieldMetadata(typeOf reflect.Type, path string, origins map[string]Origin) {
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() != reflect.Struct {
		return
	}
	for index := 0; index < typeOf.NumField(); index++ {
		definition := typeOf.Field(index)
		if !definition.IsExported() {
			continue
		}
		name := definition.Tag.Get("config")
		options := ""
		if comma := strings.IndexByte(name, ','); comma >= 0 {
			options = name[comma+1:]
			name = name[:comma]
		}
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(definition.Name)
		}
		fieldPath := name
		if path != "" {
			fieldPath = path + "." + name
		}
		sensitive := metadataOption(options, "secret")
		deprecated := metadataOption(options, "deprecated")
		if sensitive || deprecated {
			markOriginMetadata(origins, fieldPath, sensitive, deprecated)
		}
		applyFieldMetadata(definition.Type, fieldPath, origins)
	}
}

func metadataOption(options, wanted string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == wanted {
			return true
		}
	}
	return false
}

func markOriginMetadata(
	origins map[string]Origin,
	path string,
	sensitive bool,
	deprecated bool,
) {
	prefix := path + "."
	for candidate, origin := range origins {
		if candidate != path && !strings.HasPrefix(candidate, prefix) {
			continue
		}
		origin.Sensitive = origin.Sensitive || sensitive
		origin.Deprecated = origin.Deprecated || deprecated
		origins[candidate] = origin
	}
}

type presenceSetter interface {
	setConfigPresence(Presence)
}

func applyPresence(value reflect.Value, path string, origins map[string]Origin) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.CanAddr() {
		if setter, ok := value.Addr().Interface().(presenceSetter); ok {
			state := Absent
			if origin, exists := origins[path]; exists {
				state = origin.State
			}
			setter.setConfigPresence(state)
			return
		}
	}
	if value.Kind() != reflect.Struct {
		return
	}
	for index := 0; index < value.NumField(); index++ {
		definition := value.Type().Field(index)
		if !definition.IsExported() {
			continue
		}
		name := definition.Tag.Get("config")
		if comma := strings.IndexByte(name, ','); comma >= 0 {
			name = name[:comma]
		}
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(definition.Name)
		}
		fieldPath := name
		if path != "" {
			fieldPath = path + "." + name
		}
		applyPresence(value.Field(index), fieldPath, origins)
	}
}

func annotateDecodeError(err error, origins map[string]Origin) {
	var failures *decode.Errors
	if errors.As(err, &failures) {
		for _, failure := range failures.Fields {
			annotateFieldError(failure, origins)
		}
		return
	}

	var failure *decode.FieldError
	if errors.As(err, &failure) {
		annotateFieldError(failure, origins)
	}
}

func annotateFieldError(failure *decode.FieldError, origins map[string]Origin) {
	origin, ok := nearestOrigin(failure.Path, origins)
	if !ok {
		return
	}
	failure.Source = origin.Source
	failure.Location = origin.Location
}

func nearestOrigin(path string, origins map[string]Origin) (Origin, bool) {
	candidate := path
	for candidate != "" {
		if origin, ok := origins[candidate]; ok {
			return origin, true
		}
		if bracket := strings.LastIndexByte(candidate, '['); bracket >= 0 &&
			strings.HasSuffix(candidate, "]") {
			candidate = candidate[:bracket]
			continue
		}
		if dot := strings.LastIndexByte(candidate, '.'); dot >= 0 {
			candidate = candidate[:dot]
			continue
		}
		break
	}
	return Origin{}, false
}

// ErrNotFound is returned by sources that are absent. Only optional sources
// suppress this error.
var ErrNotFound = errors.New("configuration source not found")

// ErrSourceChanged indicates that a filesystem source changed while it was
// being read, so no candidate snapshot was accepted.
var ErrSourceChanged = errors.New("configuration source changed during read")

func applyOrigins(
	origins map[string]Origin,
	tree map[string]any,
	overrides map[string]Origin,
	parent string,
	source SourceInfo,
) {
	for key, value := range tree {
		path := key
		if parent != "" {
			path = parent + "." + key
		}

		if _, deleted := value.(merge.Delete); deleted {
			delete(origins, path)
			deleteDescendants(origins, path)
			continue
		}

		origin := Origin{
			Source: source.Name, Sensitive: source.Sensitive, Present: true, State: Present,
		}
		if value == nil {
			origin.State = Null
		}
		if override, exists := overrides[path]; exists {
			if override.Source != "" {
				origin.Source = override.Source
			}
			origin.Sensitive = origin.Sensitive || override.Sensitive
			origin.Location = override.Location
			origin.Present = override.Present
			origin.State = override.State
		}
		origins[path] = origin
		if object, ok := value.(map[string]any); ok {
			applyOrigins(origins, object, overrides, path, source)
		} else {
			deleteDescendants(origins, path)
		}
	}
}

func deleteDescendants(origins map[string]Origin, path string) {
	prefix := path + "."
	for candidate := range origins {
		if strings.HasPrefix(candidate, prefix) {
			delete(origins, candidate)
		}
	}
}

func cloneOrigins(origins map[string]Origin) map[string]Origin {
	clone := make(map[string]Origin, len(origins))
	for path, origin := range origins {
		clone[path] = origin
	}
	return clone
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

const (
	maxCanonicalTreeDepth = 64
	maxCanonicalTreeKeys  = 100_000
	maxCanonicalTreeItems = 100_000
)

type treeVisit struct {
	kind    reflect.Kind
	pointer uintptr
}

type treeCanonicalizer struct {
	ctx      context.Context
	visiting map[treeVisit]bool
	keys     int
	items    int
}

func canonicalTree(ctx context.Context, tree map[string]any) (map[string]any, error) {
	canonicalizer := treeCanonicalizer{
		ctx: ctx, visiting: make(map[treeVisit]bool),
	}
	return canonicalizer.object(tree, "", 1)
}

func (c *treeCanonicalizer) object(
	tree map[string]any,
	parent string,
	depth int,
) (map[string]any, error) {
	if err := c.check(depth, parent); err != nil {
		return nil, err
	}
	visit := treeVisit{kind: reflect.Map, pointer: reflect.ValueOf(tree).Pointer()}
	if c.visiting[visit] {
		return nil, &TreeCycleError{Path: parent}
	}
	c.visiting[visit] = true
	defer delete(c.visiting, visit)

	keys := make([]string, 0, len(tree))
	for key := range tree {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	canonical := make(map[string]any, len(tree))
	for _, key := range keys {
		if err := c.ctx.Err(); err != nil {
			return nil, err
		}
		path := key
		if parent != "" {
			path = parent + "." + key
		}
		c.keys++
		if c.keys > maxCanonicalTreeKeys {
			return nil, &TreeLimitError{
				Path: path, Kind: "keys", Limit: maxCanonicalTreeKeys,
			}
		}
		converted, err := c.value(tree[key], path, depth+1)
		if err != nil {
			return nil, err
		}
		canonical[key] = converted
	}
	return canonical, nil
}

func (c *treeCanonicalizer) value(value any, path string, depth int) (any, error) {
	if err := c.check(depth, path); err != nil {
		return nil, err
	}
	switch value := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return c.object(value, path, depth)
	case []any:
		visit := treeVisit{kind: reflect.Slice, pointer: reflect.ValueOf(value).Pointer()}
		if c.visiting[visit] {
			return nil, &TreeCycleError{Path: path}
		}
		if len(value) > maxCanonicalTreeItems-c.items {
			return nil, &TreeLimitError{
				Path: path, Kind: "items", Limit: maxCanonicalTreeItems,
			}
		}
		c.items += len(value)
		c.visiting[visit] = true
		defer delete(c.visiting, visit)
		items := make([]any, len(value))
		for index, item := range value {
			converted, err := c.value(item, fmt.Sprintf("%s[%d]", path, index), depth+1)
			if err != nil {
				return nil, err
			}
			items[index] = converted
		}
		return items, nil
	case merge.Delete:
		return value, nil
	}

	typeOf := reflect.TypeOf(value)
	switch typeOf.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64,
		reflect.String:
		return value, nil
	default:
		return nil, &TreeValueError{Path: path, Type: typeOf.String()}
	}
}

func (c *treeCanonicalizer) check(depth int, path string) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if depth > maxCanonicalTreeDepth {
		return &TreeLimitError{
			Path: path, Kind: "depth", Limit: maxCanonicalTreeDepth,
		}
	}
	return nil
}

func cloneTyped[T any](value T) T {
	cloned := cloneReflect(reflect.ValueOf(value))
	if !cloned.IsValid() {
		var zero T
		return zero
	}
	return cloned.Interface().(T)
}

type cloneVisit struct {
	typeOf  reflect.Type
	pointer uintptr
}

func validateSnapshotValue(
	value reflect.Value,
	path string,
	visiting map[cloneVisit]bool,
) error {
	if !value.IsValid() {
		return nil
	}
	if (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface ||
		value.Kind() == reflect.Map || value.Kind() == reflect.Slice) && value.IsNil() {
		return nil
	}
	if value.CanInterface() {
		if _, ok := value.Interface().(interface{ cloneConfigValue() any }); ok {
			return nil
		}
		if _, ok := value.Interface().(time.Time); ok {
			return nil
		}
	}

	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		visit := cloneVisit{typeOf: value.Type(), pointer: value.Pointer()}
		if visiting[visit] {
			return snapshotValueError(path, value.Type())
		}
		visiting[visit] = true
		defer delete(visiting, visit)
	}

	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		return validateSnapshotValue(value.Elem(), path, visiting)
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateSnapshotValue(iterator.Key(), path, visiting); err != nil {
				return err
			}
			if err := validateSnapshotValue(iterator.Value(), path, visiting); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if err := validateSnapshotValue(value.Index(index), path, visiting); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			definition := value.Type().Field(index)
			fieldPath := joinConfigPath(path, definition)
			if !definition.IsExported() {
				if typeContainsMutableReferences(definition.Type) {
					return snapshotValueError(fieldPath, definition.Type)
				}
				continue
			}
			if err := validateSnapshotValue(value.Field(index), fieldPath, visiting); err != nil {
				return err
			}
		}
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return snapshotValueError(path, value.Type())
	}
	return nil
}

func snapshotValueError(path string, typeOf reflect.Type) error {
	return &SnapshotValueError{Path: path, Type: typeOf.String()}
}

func typeContainsMutableReferences(typeOf reflect.Type) bool {
	switch typeOf.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface,
		reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return typeContainsMutableReferences(typeOf.Elem())
	case reflect.Struct:
		for index := 0; index < typeOf.NumField(); index++ {
			if typeContainsMutableReferences(typeOf.Field(index).Type) {
				return true
			}
		}
	}
	return false
}

func joinConfigPath(parent string, definition reflect.StructField) string {
	name := definition.Tag.Get("config")
	if comma := strings.IndexByte(name, ','); comma >= 0 {
		name = name[:comma]
	}
	if name == "" {
		name = strings.ToLower(definition.Name)
	}
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func cloneReflect(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) && value.IsNil() {
		return reflect.Zero(value.Type())
	}
	if value.CanInterface() {
		if cloner, ok := value.Interface().(interface{ cloneConfigValue() any }); ok {
			cloned := reflect.ValueOf(cloner.cloneConfigValue())
			if cloned.IsValid() && cloned.Type().AssignableTo(value.Type()) {
				return cloned
			}
			if value.Kind() == reflect.Pointer && cloned.IsValid() &&
				cloned.Type().AssignableTo(value.Type().Elem()) {
				pointer := reflect.New(value.Type().Elem())
				pointer.Elem().Set(cloned)
				return pointer
			}
		}
	}

	switch value.Kind() {
	case reflect.Pointer:
		clone := reflect.New(value.Type().Elem())
		clone.Elem().Set(cloneReflect(value.Elem()))
		return clone
	case reflect.Interface:
		clone := cloneReflect(value.Elem())
		result := reflect.New(value.Type()).Elem()
		result.Set(clone)
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			clone.SetMapIndex(cloneReflect(iterator.Key()), cloneReflect(iterator.Value()))
		}
		return clone
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clone := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			clone.Index(index).Set(cloneReflect(value.Index(index)))
		}
		return clone
	case reflect.Struct:
		clone := reflect.New(value.Type()).Elem()
		clone.Set(value)
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).IsExported() {
				clone.Field(index).Set(cloneReflect(value.Field(index)))
			}
		}
		return clone
	default:
		return value
	}
}
