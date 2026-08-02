package bulkhead

import "errors"

var (
	// ErrInvalidConfig identifies an invalid or unbounded construction policy.
	ErrInvalidConfig = errors.New("invalid bulkhead configuration")
	// ErrInvalidWeight identifies a non-positive or over-capacity request.
	ErrInvalidWeight = errors.New("invalid bulkhead weight")
	// ErrRejected identifies immediate rejection because capacity is exhausted.
	ErrRejected = errors.New("bulkhead rejected")
	// ErrQueueFull identifies rejection because the bounded waiter queue is full.
	ErrQueueFull = errors.New("bulkhead queue full")
	// ErrWaitTimeout identifies expiration of the configured maximum queue wait.
	ErrWaitTimeout = errors.New("bulkhead wait timeout")
	// ErrCallerCanceled identifies cancellation by the acquiring caller.
	ErrCallerCanceled = errors.New("bulkhead caller canceled")
	// ErrClosed identifies admission after drain has started.
	ErrClosed = errors.New("bulkhead closed")
	// ErrDrainIncomplete identifies a caller-bounded drain that ended with live work.
	ErrDrainIncomplete = errors.New("bulkhead drain incomplete")
	// ErrInvalidOperation identifies a nil protected operation.
	ErrInvalidOperation = errors.New("invalid bulkhead operation")
	// ErrPermitReleased identifies a duplicate permit release.
	ErrPermitReleased = errors.New("bulkhead permit already released")
	// ErrReentrant identifies an Execute call recursively acquiring the same policy.
	ErrReentrant = errors.New("bulkhead reentrant acquisition")
	// ErrPartitionLimit identifies exhaustion of a registry's fixed cardinality.
	ErrPartitionLimit = errors.New("bulkhead partition limit reached")
	// ErrPartitionExists identifies duplicate resource registration.
	ErrPartitionExists = errors.New("bulkhead partition already exists")
	// ErrPartitionBusy identifies removal of a live, queued, or open partition.
	ErrPartitionBusy = errors.New("bulkhead partition is busy")
	// ErrPartitionNotFound identifies lookup or removal of an unknown resource.
	ErrPartitionNotFound = errors.New("bulkhead partition not found")
)
