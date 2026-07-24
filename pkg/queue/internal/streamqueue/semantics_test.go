package streamqueue

import (
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemanticRequestsValidateBounds(t *testing.T) {
	validDelivery := Delivery{
		ID: "1700000000000-0", Body: []byte("body"), Attempts: 1,
		OriginalDeadLetterID: "dead-1", PriorDeadLetterID: "dead-2", ReplayGeneration: 2,
	}
	validFailure := FailureMetadata{
		Classification: management.ClassificationRetryable,
		Code:           "handler_failed",
	}
	tests := map[string]struct {
		validate func() error
	}{
		"add": {
			validate: func() error {
				return (AddRequest{Stream: "jobs", MaxLength: 100, Body: []byte("body")}).Validate(4)
			},
		},
		"read": {
			validate: func() error {
				return (ReadRequest{
					Stream: "jobs", Group: "workers", Consumer: "worker-1",
					Count: 16, Block: time.Second,
				}).Validate()
			},
		},
		"claim": {
			validate: func() error {
				return (ClaimRequest{
					Stream: "jobs", Group: "workers", Consumer: "worker-1",
					MinIdle: time.Second, Start: "0-0", Count: 16,
				}).Validate()
			},
		},
		"ack": {
			validate: func() error {
				return (AckRequest{Stream: "jobs", Group: "workers", ID: validDelivery.ID}).Validate()
			},
		},
		"dead letter": {
			validate: func() error {
				return (DeadLetterRequest{
					Source: "jobs", Destination: "jobs-dead", Group: "workers",
					Delivery: validDelivery, Failure: validFailure,
				}).Validate(job.DefaultMaxMessageBytes)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, test.validate())
		})
	}
}

func TestSemanticRequestsRejectUnsafeValues(t *testing.T) {
	tests := map[string]func() error{
		"empty add stream": func() error {
			return (AddRequest{MaxLength: 1, Body: []byte("x")}).Validate(1)
		},
		"invalid max length": func() error {
			return (AddRequest{Stream: "jobs", Body: []byte("x")}).Validate(1)
		},
		"invalid payload limit": func() error {
			return (AddRequest{Stream: "jobs", MaxLength: 1}).Validate(0)
		},
		"oversized payload": func() error {
			return (AddRequest{Stream: "jobs", MaxLength: 1, Body: []byte("xx")}).Validate(1)
		},
		"empty read group": func() error {
			return (ReadRequest{Stream: "jobs", Consumer: "worker", Count: 1, Block: time.Second}).Validate()
		},
		"oversized read": func() error {
			return (ReadRequest{Stream: "jobs", Group: "group", Consumer: "worker", Count: MaxBatchSize + 1, Block: time.Second}).Validate()
		},
		"invalid read block": func() error {
			return (ReadRequest{Stream: "jobs", Group: "group", Consumer: "worker", Count: 1}).Validate()
		},
		"invalid claim idle": func() error {
			return (ClaimRequest{Stream: "jobs", Group: "group", Consumer: "worker", Start: "0-0", Count: 1}).Validate()
		},
		"empty claim stream": func() error {
			return (ClaimRequest{Group: "group", Consumer: "worker", MinIdle: time.Second, Start: "0-0", Count: 1}).Validate()
		},
		"empty claim cursor": func() error {
			return (ClaimRequest{Stream: "jobs", Group: "group", Consumer: "worker", MinIdle: time.Second, Count: 1}).Validate()
		},
		"invalid claim batch": func() error {
			return (ClaimRequest{Stream: "jobs", Group: "group", Consumer: "worker", MinIdle: time.Second, Start: "0-0"}).Validate()
		},
		"empty acknowledgement id": func() error {
			return (AckRequest{Stream: "jobs", Group: "group"}).Validate()
		},
		"empty acknowledgement stream": func() error {
			return (AckRequest{Group: "group", ID: "1-0"}).Validate()
		},
		"empty acknowledgement group": func() error {
			return (AckRequest{Stream: "jobs", ID: "1-0"}).Validate()
		},
		"empty dead letter source": func() error {
			return (DeadLetterRequest{Destination: "dead", Group: "group", Delivery: Delivery{ID: "1-0", Body: []byte("x"), Attempts: 1}}).Validate(1)
		},
		"empty dead letter destination": func() error {
			return (DeadLetterRequest{Source: "jobs", Group: "group", Delivery: Delivery{ID: "1-0", Body: []byte("x"), Attempts: 1}}).Validate(1)
		},
		"same dead letter stream": func() error {
			return (DeadLetterRequest{Source: "jobs", Destination: "jobs", Group: "group", Delivery: Delivery{ID: "1-0", Body: []byte("x"), Attempts: 1}}).Validate(1)
		},
		"empty dead letter group": func() error {
			return (DeadLetterRequest{Source: "jobs", Destination: "dead", Delivery: Delivery{ID: "1-0", Body: []byte("x"), Attempts: 1}}).Validate(1)
		},
		"empty dead letter id": func() error {
			return (DeadLetterRequest{Source: "jobs", Destination: "dead", Group: "group", Delivery: Delivery{Body: []byte("x"), Attempts: 1}}).Validate(1)
		},
		"invalid delivery attempts": func() error {
			return (DeadLetterRequest{Source: "jobs", Destination: "dead", Group: "group", Delivery: Delivery{ID: "1-0", Body: []byte("x")}}).Validate(1)
		},
		"partial replay lineage": func() error {
			return (DeadLetterRequest{
				Source: "jobs", Destination: "dead", Group: "group",
				Delivery: Delivery{
					ID: "1-0", Body: []byte("x"), Attempts: 1,
					OriginalDeadLetterID: "dead-1",
				},
			}).Validate(1)
		},
		"invalid dead letter payload limit": func() error {
			return (DeadLetterRequest{Source: "jobs", Destination: "dead", Group: "group", Delivery: Delivery{ID: "1-0", Body: []byte("x"), Attempts: 1}}).Validate(0)
		},
		"oversized dead letter payload": func() error {
			return (DeadLetterRequest{Source: "jobs", Destination: "dead", Group: "group", Delivery: Delivery{ID: "1-0", Body: []byte("xx"), Attempts: 1}}).Validate(1)
		},
	}

	for name, validate := range tests {
		t.Run(name, func(t *testing.T) {
			err := validate()
			assert.ErrorIs(t, err, ErrInvalidSemanticRequest)
			var requestErr *RequestError
			require.ErrorAs(t, err, &requestErr)
			assert.NotEmpty(t, requestErr.Command)
			assert.NotEmpty(t, requestErr.Field)
			assert.NotContains(t, err.Error(), "xx")
		})
	}
}

func TestGroupStateReportsKnownAndUnknownDepth(t *testing.T) {
	assert.Equal(t, Stats{Depth: 7, Pending: 2, Lag: 5, LagKnown: true},
		(GroupState{Pending: 2, Lag: 5}).Stats())
	assert.Equal(t, Stats{Depth: -1, Pending: 2, Lag: -1},
		(GroupState{Pending: 2, Lag: -1}).Stats())
}

func TestMessageAgeParsesStreamIdentifier(t *testing.T) {
	now := time.UnixMilli(1_700_000_001_000)
	age, err := MessageAge("1700000000000-3", now)
	require.NoError(t, err)
	assert.Equal(t, time.Second, age)

	age, err = MessageAge("1700000002000-0", now)
	require.NoError(t, err)
	assert.Zero(t, age)

	for _, id := range []string{"invalid", "nope-0", "1-nope"} {
		_, err = MessageAge(id, now)
		assert.ErrorIs(t, err, ErrMalformedDelivery)
		assert.Equal(t, "streamqueue: malformed delivery: invalid stream identifier", err.Error())
	}
}

func TestRequestErrorRetainsClassificationAndCause(t *testing.T) {
	cause := errors.New("cause")
	err := invalidRequest("read", "stream", cause)

	assert.Equal(t, "streamqueue: invalid read stream", err.Error())
	assert.ErrorIs(t, err, ErrInvalidSemanticRequest)
	assert.ErrorIs(t, err, cause)
}
