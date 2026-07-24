package xsd

import (
	"fmt"
	"io"
	"reflect"
)

const (
	defaultMaxMarshalOutputBytes int64 = 64 << 20
	defaultMaxMarshalDepth             = 256
	defaultMaxMarshalComponents        = 1_000_000
)

// MarshalOptions bounds deterministic schema serialization. Zero values use
// conservative defaults; negative values are invalid.
type MarshalOptions struct {
	MaxOutputBytes int64
	MaxDepth       int
	MaxComponents  int
}

type marshalLimits struct {
	MaxOutputBytes int64
	MaxDepth       int
	MaxComponents  int
}

func normalizeMarshalOptions(options MarshalOptions) (marshalLimits, error) {
	if options.MaxOutputBytes < 0 || options.MaxDepth < 0 || options.MaxComponents < 0 {
		return marshalLimits{}, fmt.Errorf("xsd: marshal limits must not be negative")
	}
	limits := marshalLimits(options)
	if limits.MaxOutputBytes == 0 {
		limits.MaxOutputBytes = defaultMaxMarshalOutputBytes
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxMarshalDepth
	}
	if limits.MaxComponents == 0 {
		limits.MaxComponents = defaultMaxMarshalComponents
	}
	return limits, nil
}

type marshalLimitWriter struct {
	writer    io.Writer
	remaining int64
	maximum   int64
}

func (w *marshalLimitWriter) Write(value []byte) (int, error) {
	if int64(len(value)) <= w.remaining {
		written, err := w.writer.Write(value)
		w.remaining -= int64(written)
		return written, err
	}
	allowed := int(w.remaining)
	written, err := w.writer.Write(value[:allowed])
	w.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	return written, fmt.Errorf(
		"%w: serialized output exceeds %d bytes",
		ErrLimitExceeded,
		w.maximum,
	)
}

type marshalPointer struct {
	typeOf  reflect.Type
	pointer uintptr
}

type marshalBudget struct {
	limits marshalLimits
	work   int
	active map[marshalPointer]struct{}
}

func checkMarshalDocument(document *Document, limits marshalLimits) error {
	budget := &marshalBudget{
		limits: limits,
		active: make(map[marshalPointer]struct{}),
	}
	return budget.value(reflect.ValueOf(document), 0)
}

func (b *marshalBudget) value(value reflect.Value, depth int) error {
	if !value.IsValid() {
		return nil
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		pointer := marshalPointer{typeOf: value.Type(), pointer: value.Pointer()}
		if _, exists := b.active[pointer]; exists {
			return fmt.Errorf("%w: cyclic schema model", ErrLimitExceeded)
		}
		b.active[pointer] = struct{}{}
		defer delete(b.active, pointer)
		return b.value(value.Elem(), depth)
	case reflect.Struct:
		if depth > b.limits.MaxDepth {
			return fmt.Errorf(
				"%w: schema model depth exceeds %d",
				ErrLimitExceeded,
				b.limits.MaxDepth,
			)
		}
		if err := b.add(1); err != nil {
			return err
		}
		for index := 0; index < value.NumField(); index++ {
			if err := b.value(value.Field(index), depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if err := b.add(value.Len()); err != nil {
			return err
		}
		for index := 0; index < value.Len(); index++ {
			if err := b.value(value.Index(index), depth); err != nil {
				return err
			}
		}
	case reflect.Map:
		return b.add(value.Len())
	}
	return nil
}

func (b *marshalBudget) add(amount int) error {
	if amount > b.limits.MaxComponents-b.work {
		return fmt.Errorf(
			"%w: schema model components exceed %d",
			ErrLimitExceeded,
			b.limits.MaxComponents,
		)
	}
	b.work += amount
	return nil
}
