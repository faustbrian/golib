// Package serialize emits bounded deterministic representations of immutable
// OpenAPI semantic values.
package serialize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// ErrLimitExceeded reports an output byte or nesting limit.
var ErrLimitExceeded = errors.New("serialization limit exceeded")

// Mode selects member ordering policy across deterministic representations.
type Mode uint8

const (
	// Preserve retains the semantic object's source member order.
	Preserve Mode = iota
	// Canonical sorts every object's member names by UTF-8 byte order.
	Canonical
	// JSONPreserve is retained as an explicit JSON spelling of Preserve.
	JSONPreserve = Preserve
	// JSONCanonical is retained as an explicit JSON spelling of Canonical.
	JSONCanonical = Canonical
)

// Options defines deterministic output policy and resource limits.
type Options struct {
	Mode     Mode
	MaxBytes int
	MaxDepth int
	MaxNodes int
}

// DefaultOptions returns bounded preserving-mode policy.
func DefaultOptions() Options {
	return Options{
		Mode:     Preserve,
		MaxBytes: 64 << 20,
		MaxDepth: 512,
		MaxNodes: 1_000_000,
	}
}

// Source exposes one complete immutable JSON semantic value.
type Source interface {
	Raw() jsonvalue.Value
}

// JSON writes compact deterministic JSON. Canonical mode is a package-defined
// reproducibility policy, not an OpenAPI conformance representation.
func JSON(
	ctx context.Context,
	writer io.Writer,
	source Source,
	options Options,
) error {
	if ctx == nil {
		return errors.New("serialize JSON: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if writerIsNil(writer) {
		return errors.New("serialize JSON: nil writer")
	}
	if sourceIsNil(source) {
		return errors.New("serialize JSON: nil source")
	}
	if options.Mode != Preserve && options.Mode != Canonical {
		return fmt.Errorf("serialize JSON: invalid mode %d", options.Mode)
	}
	if options.MaxBytes < 1 || options.MaxDepth < 1 || options.MaxNodes < 1 {
		return fmt.Errorf("%w: limits must be positive", ErrLimitExceeded)
	}
	value := source.Raw()
	if value.Kind() == jsonvalue.InvalidKind {
		return errors.New("serialize JSON: source contains an invalid root value")
	}
	emitter := jsonEmitter{
		ctx:            ctx,
		writer:         writer,
		remaining:      options.MaxBytes,
		maxDepth:       options.MaxDepth,
		remainingNodes: options.MaxNodes,
		mode:           options.Mode,
	}
	if err := emitter.value(value, 0); err != nil {
		return fmt.Errorf("serialize JSON: %w", err)
	}
	return nil
}

type jsonEmitter struct {
	ctx            context.Context
	writer         io.Writer
	remaining      int
	maxDepth       int
	mode           Mode
	remainingNodes int
}

func (emitter *jsonEmitter) value(value jsonvalue.Value, depth int) error {
	if err := emitter.ctx.Err(); err != nil {
		return err
	}
	if emitter.remainingNodes == 0 {
		return fmt.Errorf("%w: semantic nodes", ErrLimitExceeded)
	}
	emitter.remainingNodes--
	childCount, _ := value.Length()
	if !serializationChildrenFit(
		childCount, depth, emitter.remainingNodes, emitter.maxDepth,
	) {
		return fmt.Errorf("%w: semantic children", ErrLimitExceeded)
	}
	switch value.Kind() {
	case jsonvalue.NullKind:
		return emitter.writeString("null")
	case jsonvalue.BooleanKind:
		boolean, _ := value.Bool()
		if boolean {
			return emitter.writeString("true")
		}
		return emitter.writeString("false")
	case jsonvalue.NumberKind:
		number, _ := value.NumberText()
		return emitter.writeString(number)
	case jsonvalue.StringKind:
		text, _ := value.Text()
		raw, _ := json.Marshal(text)
		return emitter.write(raw)
	case jsonvalue.ArrayKind:
		return emitter.array(value, depth)
	case jsonvalue.ObjectKind:
		return emitter.object(value, depth)
	default:
		return jsonvalue.ErrInvalidValue
	}
}

func serializationChildrenFit(
	childCount int,
	depth int,
	remainingNodes int,
	maxDepth int,
) bool {
	if childCount == 0 {
		return true
	}
	if depth >= maxDepth {
		return false
	}
	return childCount <= remainingNodes
}

func (emitter *jsonEmitter) array(value jsonvalue.Value, depth int) error {
	if err := emitter.writeString("["); err != nil {
		return err
	}
	elements, _ := value.Elements()
	for index, element := range elements {
		if index > 0 {
			if err := emitter.writeString(","); err != nil {
				return err
			}
		}
		if err := emitter.value(element, depth+1); err != nil {
			return err
		}
	}
	return emitter.writeString("]")
}

func (emitter *jsonEmitter) object(value jsonvalue.Value, depth int) error {
	if err := emitter.writeString("{"); err != nil {
		return err
	}
	members, _ := value.Members()
	if emitter.mode == Canonical {
		slices.SortStableFunc(members, func(left, right jsonvalue.Member) int {
			return strings.Compare(left.Name, right.Name)
		})
	}
	for index, member := range members {
		if index > 0 {
			if err := emitter.writeString(","); err != nil {
				return err
			}
		}
		name, _ := json.Marshal(member.Name)
		if err := emitter.write(name); err != nil {
			return err
		}
		if err := emitter.writeString(":"); err != nil {
			return err
		}
		if err := emitter.value(member.Value, depth+1); err != nil {
			return err
		}
	}
	return emitter.writeString("}")
}

func (emitter *jsonEmitter) writeString(value string) error {
	return emitter.write([]byte(value))
}

func (emitter *jsonEmitter) write(value []byte) error {
	if err := emitter.ctx.Err(); err != nil {
		return err
	}
	if len(value) > emitter.remaining {
		if emitter.remaining != 0 {
			if err := writeAll(emitter.writer, value[:emitter.remaining]); err != nil {
				return err
			}
			emitter.remaining = 0
		}
		return ErrLimitExceeded
	}
	if err := writeAll(emitter.writer, value); err != nil {
		return err
	}
	emitter.remaining -= len(value)
	return nil
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if written < 0 || written > len(value) {
			return io.ErrShortWrite
		}
		value = value[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func writerIsNil(writer io.Writer) bool {
	if writer == nil {
		return true
	}
	value := reflect.ValueOf(writer)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func sourceIsNil(source Source) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
