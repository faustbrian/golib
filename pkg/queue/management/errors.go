package management

import "errors"

var (
	// ErrRecordNotFound reports that the selected management record is absent.
	ErrRecordNotFound = errors.New("management: record not found")
	// ErrUnsupportedCapability reports an operation the backend did not advertise.
	ErrUnsupportedCapability = errors.New("management: unsupported capability")
	// ErrManagementUnavailable reports a temporarily unreachable data plane.
	ErrManagementUnavailable = errors.New("management: unavailable")
	// ErrMalformedCursor reports opaque pagination state that cannot be decoded.
	ErrMalformedCursor = errors.New("management: malformed cursor")
	// ErrInvalidFilter reports filtering the backend cannot safely honor.
	ErrInvalidFilter = errors.New("management: invalid filter")
	// ErrStaleRecord reports a record changed after it was selected or inspected.
	ErrStaleRecord = errors.New("management: stale record")
	// ErrMutationConflict reports an incompatible concurrent or duplicate mutation.
	ErrMutationConflict = errors.New("management: mutation conflict")
	// ErrPartialMutation reports a mutation with both confirmed and failed effects.
	ErrPartialMutation = errors.New("management: partial mutation")
	// ErrUnknownMutation reports a mutation whose durable outcome is ambiguous.
	ErrUnknownMutation = errors.New("management: unknown mutation outcome")
)
