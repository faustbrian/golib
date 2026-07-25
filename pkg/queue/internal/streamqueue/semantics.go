// Package streamqueue defines package-owned stream queue semantics shared by
// native backend adapters.
package streamqueue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

// MaxBatchSize bounds one read or reclaim operation.
const MaxBatchSize int64 = 256

var (
	// ErrInvalidSemanticRequest classifies invalid stream command requests.
	ErrInvalidSemanticRequest = errors.New("streamqueue: invalid semantic request")
	// ErrMalformedDelivery classifies invalid backend delivery metadata.
	ErrMalformedDelivery = errors.New("streamqueue: malformed delivery")
)

// RequestError identifies an invalid semantic field without including its
// potentially sensitive value.
type RequestError struct {
	Command string
	Field   string
	Cause   error
}

// Error returns value-free request validation text.
func (e *RequestError) Error() string {
	return fmt.Sprintf("streamqueue: invalid %s %s", e.Command, e.Field)
}

// Unwrap retains the stable classification and underlying cause.
func (e *RequestError) Unwrap() []error {
	return []error{ErrInvalidSemanticRequest, e.Cause}
}

// Delivery is the transport-neutral representation of one stream entry.
type Delivery struct {
	ID                   string
	Body                 []byte
	Attempts             int64
	Reclaimed            bool
	OriginalDeadLetterID string
	PriorDeadLetterID    string
	ReplayGeneration     uint32
}

// FailureMetadata is the bounded disposition persisted with a failure record.
type FailureMetadata struct {
	Classification management.Classification
	Code           string
}

// Validate rejects unknown classifications and unsafe codes.
func (m FailureMetadata) Validate() error {
	return management.NewFailure(m.Classification, m.Code, nil).Validate()
}

// AddRequest describes a bounded stream append.
type AddRequest struct {
	Stream    string
	MaxLength int64
	Body      []byte
}

// Validate checks append ownership and resource bounds.
func (r AddRequest) Validate(maxPayloadBytes int) error {
	switch {
	case strings.TrimSpace(r.Stream) == "":
		return invalidRequest("add", "stream", errors.New("stream is required"))
	case r.MaxLength <= 0:
		return invalidRequest("add", "maximum length", errors.New("maximum length must be positive"))
	case maxPayloadBytes <= 0:
		return invalidRequest("add", "payload limit", errors.New("payload limit must be positive"))
	case len(r.Body) > maxPayloadBytes:
		return invalidRequest("add", "payload", errors.New("payload exceeds limit"))
	default:
		return nil
	}
}

// ReadRequest describes one cancellation-aware consumer-group read.
type ReadRequest struct {
	Stream   string
	Group    string
	Consumer string
	Count    int64
	Block    time.Duration
}

// Validate checks consumer identity and command bounds.
func (r ReadRequest) Validate() error {
	if err := validateIdentity("read", r.Stream, r.Group, r.Consumer); err != nil {
		return err
	}
	if r.Count <= 0 || r.Count > MaxBatchSize {
		return invalidRequest("read", "batch size", errors.New("batch size is outside bounds"))
	}
	if r.Block <= 0 {
		return invalidRequest("read", "block time", errors.New("block time must be positive"))
	}
	return nil
}

// ClaimRequest describes one bounded stale-delivery reclaim scan.
type ClaimRequest struct {
	Stream   string
	Group    string
	Consumer string
	MinIdle  time.Duration
	Start    string
	Count    int64
}

// Validate checks reclaim ownership and scan bounds.
func (r ClaimRequest) Validate() error {
	if err := validateIdentity("claim", r.Stream, r.Group, r.Consumer); err != nil {
		return err
	}
	if r.MinIdle <= 0 {
		return invalidRequest("claim", "minimum idle time", errors.New("minimum idle time must be positive"))
	}
	if strings.TrimSpace(r.Start) == "" {
		return invalidRequest("claim", "cursor", errors.New("cursor is required"))
	}
	if r.Count <= 0 || r.Count > MaxBatchSize {
		return invalidRequest("claim", "batch size", errors.New("batch size is outside bounds"))
	}
	return nil
}

// ClaimResult contains reclaimed entries and the next bounded scan cursor.
type ClaimResult struct {
	Next       string
	Deliveries []Delivery
}

// AckRequest identifies one consumer-group delivery to settle.
type AckRequest struct {
	Stream string
	Group  string
	ID     string
}

// Validate checks acknowledgement identity.
func (r AckRequest) Validate() error {
	if strings.TrimSpace(r.Stream) == "" {
		return invalidRequest("ack", "stream", errors.New("stream is required"))
	}
	if strings.TrimSpace(r.Group) == "" {
		return invalidRequest("ack", "group", errors.New("group is required"))
	}
	if strings.TrimSpace(r.ID) == "" {
		return invalidRequest("ack", "identifier", errors.New("identifier is required"))
	}
	return nil
}

// DeadLetterRequest describes an append-before-ack terminal transfer.
type DeadLetterRequest struct {
	Source      string
	Destination string
	Group       string
	Delivery    Delivery
	Failure     FailureMetadata
}

// Validate checks terminal transfer identity and payload bounds.
func (r DeadLetterRequest) Validate(maxPayloadBytes int) error {
	if strings.TrimSpace(r.Source) == "" {
		return invalidRequest("dead letter", "source", errors.New("source is required"))
	}
	if strings.TrimSpace(r.Destination) == "" || r.Destination == r.Source {
		return invalidRequest("dead letter", "destination", errors.New("destination must be distinct"))
	}
	if strings.TrimSpace(r.Group) == "" {
		return invalidRequest("dead letter", "group", errors.New("group is required"))
	}
	if strings.TrimSpace(r.Delivery.ID) == "" {
		return invalidRequest("dead letter", "identifier", errors.New("identifier is required"))
	}
	if r.Delivery.Attempts < 1 {
		return invalidRequest("dead letter", "attempts", errors.New("attempts must be positive"))
	}
	if err := validateReplayLineage(r.Delivery); err != nil {
		return invalidRequest("dead letter", "lineage", err)
	}
	if err := r.Failure.Validate(); err != nil {
		return invalidRequest("dead letter", "failure", err)
	}
	return (AddRequest{
		Stream: r.Destination, MaxLength: 1, Body: r.Delivery.Body,
	}).Validate(maxPayloadBytes)
}

func validateReplayLineage(delivery Delivery) error {
	hasAny := delivery.OriginalDeadLetterID != "" || delivery.PriorDeadLetterID != "" ||
		delivery.ReplayGeneration != 0
	if !hasAny {
		return nil
	}
	if strings.TrimSpace(delivery.OriginalDeadLetterID) == "" ||
		strings.TrimSpace(delivery.PriorDeadLetterID) == "" ||
		len(delivery.OriginalDeadLetterID) > management.MaxIdentityBytes ||
		len(delivery.PriorDeadLetterID) > management.MaxIdentityBytes ||
		delivery.ReplayGeneration == 0 {
		return errors.New("replay lineage must be complete and bounded")
	}
	return nil
}

// GroupState contains server-reported consumer-group depth components.
type GroupState struct {
	Pending         int64
	Lag             int64
	OldestPendingID string
}

// Stats is the common honest consumer-group depth snapshot.
type Stats struct {
	Depth    int64
	Pending  int64
	Lag      int64
	LagKnown bool
}

// Stats derives depth only when the server reports group lag.
func (s GroupState) Stats() Stats {
	stats := Stats{Depth: -1, Pending: s.Pending, Lag: s.Lag}
	if s.Lag >= 0 {
		stats.Depth = s.Pending + s.Lag
		stats.LagKnown = true
	}
	return stats
}

// Transport is the semantic boundary implemented by each native stream
// client adapter. It intentionally contains queue operations rather than a
// union of arbitrary datastore commands.
type Transport interface {
	EnsureGroup(context.Context, string, string) error
	Add(context.Context, AddRequest) (string, error)
	Read(context.Context, ReadRequest) ([]Delivery, error)
	Claim(context.Context, ClaimRequest) (ClaimResult, error)
	Ack(context.Context, AckRequest) error
	DeadLetter(context.Context, DeadLetterRequest) error
	GroupState(context.Context, string, string) (GroupState, error)
	Close() error
}

// MessageAge derives an entry age from a server-generated stream identifier.
func MessageAge(id string, now time.Time) (time.Duration, error) {
	milliseconds, sequence, ok := strings.Cut(id, "-")
	if !ok {
		return 0, malformedIdentifier()
	}
	timestamp, err := strconv.ParseInt(milliseconds, 10, 64)
	if err != nil {
		return 0, malformedIdentifier()
	}
	if _, err = strconv.ParseUint(sequence, 10, 64); err != nil {
		return 0, malformedIdentifier()
	}
	age := now.Sub(time.UnixMilli(timestamp))
	if age < 0 {
		return 0, nil
	}
	return age, nil
}

func validateIdentity(command, stream, group, consumer string) error {
	for _, value := range []struct {
		name  string
		value string
	}{
		{"stream", stream},
		{"group", group},
		{"consumer", consumer},
	} {
		if strings.TrimSpace(value.value) == "" {
			return invalidRequest(command, value.name, errors.New(value.name+" is required"))
		}
	}
	return nil
}

func invalidRequest(command, field string, cause error) *RequestError {
	return &RequestError{Command: command, Field: field, Cause: cause}
}

func malformedIdentifier() error {
	return fmt.Errorf("%w: invalid stream identifier", ErrMalformedDelivery)
}
