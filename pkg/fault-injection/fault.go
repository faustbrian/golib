package faultinject

import (
	"reflect"
	"time"
)

// Fault is an immutable-after-construction fault description copied into an
// Injector. Adapters apply only kinds whose semantics they explicitly define.
type Fault struct {
	Kind       Kind
	phase      Phase
	err        error
	delay      time.Duration
	panicValue string
	limit      int
	mask       byte
}

// ErrorFault returns err at phase.
func ErrorFault(phase Phase, err error) Fault {
	return Fault{Kind: KindError, phase: phase, err: err}
}

// LatencyFault delays at phase by a bounded duration.
func LatencyFault(phase Phase, delay time.Duration) Fault {
	return Fault{Kind: KindLatency, phase: phase, delay: delay}
}

// CancelFault makes an adapter expose context cancellation at phase.
func CancelFault(phase Phase) Fault { return Fault{Kind: KindCancel, phase: phase} }

// DeadlineFault makes an adapter expose deadline expiry at phase.
func DeadlineFault(phase Phase) Fault { return Fault{Kind: KindDeadline, phase: phase} }

// PanicFault panics with a bounded caller-supplied safe string at phase.
func PanicFault(phase Phase, value string) Fault {
	return Fault{Kind: KindPanic, phase: phase, panicValue: value}
}

// ByteFault configures a byte or stream fault. limit is required by truncate,
// reorder, short-read, short-write, and interruption. Corruption uses mask.
func ByteFault(kind Kind, phase Phase, limit int, mask byte) Fault {
	return Fault{Kind: kind, phase: phase, limit: limit, mask: mask}
}

// Phase returns when the fault applies.
func (f Fault) Phase() Phase { return f.phase }

// Error returns the configured injected error, if any.
func (f Fault) Error() error { return f.err }

// Delay returns the configured latency.
func (f Fault) Delay() time.Duration { return f.delay }

// PanicValue returns the bounded panic string.
func (f Fault) PanicValue() string { return f.panicValue }

// Limit returns the byte or operation bound.
func (f Fault) Limit() int { return f.limit }

// Mask returns the byte corruption mask.
func (f Fault) Mask() byte { return f.mask }

func validateFault(field string, fault Fault, maxLatency time.Duration, maxBytes int) error {
	if fault.phase < PhaseBefore || fault.phase > PhaseAfter {
		return invalid(field+".phase", "must be declared")
	}
	switch fault.Kind {
	case KindError:
		if nilError(fault.err) {
			return invalid(field+".error", "must be non-nil")
		}
	case KindLatency:
		if fault.delay < time.Nanosecond || fault.delay > maxLatency {
			return invalid(field+".delay", "must be positive and within MaxLatency")
		}
	case KindCancel, KindDeadline, KindDrop, KindTemporary,
		KindPermanent, KindReset, KindHalfClose:
	case KindPanic:
		if !validIdentity(fault.panicValue) {
			return invalid(field+".panic", "must be a bounded safe value")
		}
	case KindDuplicate, KindTruncate, KindReorder, KindShortRead, KindShortWrite, KindInterrupt:
		if fault.limit < 1 || fault.limit > maxBytes {
			return invalid(field+".limit", "must be positive and within MaxBytes")
		}
	case KindCorrupt:
		if fault.limit < 1 || fault.limit > maxBytes {
			return invalid(field+".corruption", "requires a bounded limit")
		}
		if fault.mask == 0 {
			return invalid(field+".corruption", "requires a non-zero mask and bounded limit")
		}
	default:
		return invalid(field+".kind", "is unsupported")
	}
	return nil
}

func nilError(err error) bool {
	if err == nil {
		return true
	}
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
