package job

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/appleboy/com/bytesconv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockMessage struct {
	message string
}

func (m mockMessage) Bytes() []byte {
	return bytesconv.StrToBytes(m.message)
}

func TestMessageEncodeDecode(t *testing.T) {
	enqueuedAt := time.Date(2026, time.July, 17, 8, 30, 0, 0, time.UTC)
	metadata := &Metadata{
		OriginalID: "job-123", PayloadSchemaVersion: "order.v2",
		ContentType: "application/json", EnqueuedAt: &enqueuedAt,
		RetryPolicy: "critical-v1", HandlerType: "CreateOrder",
		JobType: "order.created", Tags: map[string]string{"region": "eu"},
		TraceID: "trace-123", TenantID: "tenant-123", ProducerVersion: "1.2.3",
	}
	m := NewMessage(&mockMessage{
		message: "foo",
	},
		AllowOption{
			RetryCount:  Int64(100),
			RetryDelay:  Time(30 * time.Millisecond),
			Timeout:     Time(3 * time.Millisecond),
			RetryMin:    Time(200 * time.Millisecond),
			RetryMax:    Time(20 * time.Second),
			RetryFactor: Float64(4.0),
			Jitter:      Bool(true),
			Metadata:    metadata,
		},
	)
	metadata.Tags["region"] = "changed"
	metadata.OriginalID = "changed"

	out := Decode(m.Bytes())

	assert.Equal(t, int64(100), out.RetryCount)
	assert.Equal(t, 30*time.Millisecond, out.RetryDelay)
	assert.Equal(t, 3*time.Millisecond, out.Timeout)
	assert.Equal(t, "foo", string(out.Payload()))
	assert.Equal(t, 200*time.Millisecond, out.RetryMin)
	assert.Equal(t, 20*time.Second, out.RetryMax)
	assert.Equal(t, 4.0, out.RetryFactor)
	assert.True(t, out.Jitter)
	require.NotNil(t, out.Metadata)
	assert.Equal(t, "job-123", out.Metadata.OriginalID)
	assert.Equal(t, "eu", out.Metadata.Tags["region"])
	assert.Equal(t, enqueuedAt, *out.Metadata.EnqueuedAt)
}

func TestTaskCarriesAllExecutionOptions(t *testing.T) {
	task := NewTask(
		func(context.Context) error { return nil },
		AllowOption{
			RetryCount:  Int64(2),
			RetryDelay:  Time(time.Second),
			RetryMin:    Time(2 * time.Second),
			RetryMax:    Time(3 * time.Second),
			RetryFactor: Float64(3),
			Jitter:      Bool(true),
			Timeout:     Time(4 * time.Second),
		},
	)

	assert.NotNil(t, task.Task)
	assert.Equal(t, int64(2), task.RetryCount)
	assert.Equal(t, time.Second, task.RetryDelay)
	assert.Equal(t, 2*time.Second, task.RetryMin)
	assert.Equal(t, 3*time.Second, task.RetryMax)
	assert.Equal(t, float64(3), task.RetryFactor)
	assert.True(t, task.Jitter)
	assert.Equal(t, 4*time.Second, task.Timeout)
}

func TestMessageSettlementCallbacks(t *testing.T) {
	message := NewTask(nil)
	assert.False(t, message.AcknowledgementRequired())
	assert.NoError(t, message.Ack())
	assert.NoError(t, message.Nack())

	ackErr := errors.New("ack")
	nackErr := errors.New("nack")
	message.SetAcknowledgement(
		func() error { return ackErr },
		func() error { return nackErr },
	)

	assert.True(t, message.AcknowledgementRequired())
	assert.ErrorIs(t, message.Ack(), ackErr)
	assert.ErrorIs(t, message.Nack(), nackErr)
}

func TestMessageFailureSettlementReceivesHandlerErrorAndFallsBack(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("handler")
	legacyErr := errors.New("legacy nack")
	message := NewTask(nil)
	message.SetAcknowledgement(nil, func() error { return legacyErr })
	assert.ErrorIs(t, message.NackFailure(handlerErr), legacyErr)

	var received error
	failureErr := errors.New("failure nack")
	message.SetFailureAcknowledgement(nil, func(err error) error {
		received = err
		return failureErr
	})
	assert.True(t, message.AcknowledgementRequired())
	assert.ErrorIs(t, message.NackFailure(handlerErr), failureErr)
	assert.Equal(t, handlerErr, received)
	received = handlerErr
	assert.ErrorIs(t, message.Nack(), failureErr)
	assert.NoError(t, received)
}

func TestEncodeAndDecode(t *testing.T) {
	message := NewTask(nil)
	encoded := Encode(&message)
	assert.Equal(t, message.Timeout, Decode(encoded).Timeout)
	assert.Panics(t, func() { Decode([]byte("not-json")) })
}

func TestDecodeERejectsInvalidAndOversizedMessages(t *testing.T) {
	message := NewTask(nil)
	encoded := Encode(&message)

	decoded, err := DecodeE(encoded, len(encoded))
	assert.NoError(t, err)
	assert.Equal(t, message.Timeout, decoded.Timeout)

	decoded, err = DecodeE([]byte("not-json"), DefaultMaxMessageBytes)
	assert.Nil(t, decoded)
	assert.Error(t, err)

	decoded, err = DecodeE(encoded, len(encoded)-1)
	assert.Nil(t, decoded)
	assert.ErrorIs(t, err, ErrMessageTooLarge)

	decoded, err = DecodeE(encoded, 0)
	assert.Nil(t, decoded)
	assert.ErrorIs(t, err, ErrInvalidMessageLimit)

	unsafe := NewTask(nil)
	unsafe.RetryCount = MaxRetryCount + 1
	decoded, err = DecodeE(Encode(&unsafe), DefaultMaxMessageBytes)
	assert.Nil(t, decoded)
	assert.ErrorIs(t, err, ErrInvalidMessage)
}

func TestMessageValidationRejectsUnsafeExecutionState(t *testing.T) {
	tests := map[string]Message{
		"non-positive timeout": {Timeout: 0},
		"negative retries":     {Timeout: time.Second, RetryCount: -1},
		"excessive retries": {
			Timeout: time.Second, RetryCount: MaxRetryCount + 1,
		},
		"negative fixed delay": {
			Timeout: time.Second, RetryDelay: -time.Second,
		},
		"small backoff factor": {
			Timeout: time.Second, RetryCount: 1,
			RetryFactor: .5, RetryMin: time.Second, RetryMax: time.Second,
		},
		"non-finite backoff factor": {
			Timeout: time.Second, RetryCount: 1,
			RetryFactor: math.Inf(1), RetryMin: time.Second, RetryMax: time.Second,
		},
		"non-positive backoff minimum": {
			Timeout: time.Second, RetryCount: 1,
			RetryFactor: 2, RetryMin: 0, RetryMax: time.Second,
		},
		"backoff maximum below minimum": {
			Timeout: time.Second, RetryCount: 1,
			RetryFactor: 2, RetryMin: time.Second, RetryMax: time.Millisecond,
		},
	}

	for name, message := range tests {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, message.Validate(), ErrInvalidMessage)
		})
	}

	valid := NewTask(nil, AllowOption{RetryCount: Int64(MaxRetryCount)})
	assert.NoError(t, valid.Validate())
}

func TestMessageMetadataValidationRejectsUnboundedValues(t *testing.T) {
	t.Parallel()

	tooLong := strings.Repeat("x", MaxMetadataValueBytes+1)
	tooManyTags := make(map[string]string, MaxMetadataTags+1)
	for index := 0; index <= MaxMetadataTags; index++ {
		tooManyTags[fmt.Sprintf("tag-%d", index)] = "value"
	}
	zero := time.Time{}

	tests := map[string]*Metadata{
		"blank value":       {OriginalID: " "},
		"oversized value":   {TraceID: tooLong},
		"too many tags":     {Tags: tooManyTags},
		"blank tag key":     {Tags: map[string]string{" ": "value"}},
		"oversized tag":     {Tags: map[string]string{"key": tooLong}},
		"zero enqueue time": {EnqueuedAt: &zero},
	}

	for name, metadata := range tests {
		t.Run(name, func(t *testing.T) {
			message := NewTask(nil)
			message.Metadata = metadata

			assert.ErrorIs(t, message.Validate(), ErrInvalidMessage)
		})
	}

	message := NewTask(nil, AllowOption{Metadata: &Metadata{
		OriginalID: "job-1", Tags: map[string]string{"region": "eu"},
	}})
	assert.NoError(t, message.Validate())
}

func TestEncodingPanicsWhenJSONMarshallingFails(t *testing.T) {
	original := marshalJSON
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	t.Cleanup(func() { marshalJSON = original })

	message := NewTask(nil)
	assert.Panics(t, func() { message.Bytes() })
	assert.Panics(t, func() { Encode(&message) })
}
