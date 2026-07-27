package valkeystream

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey "github.com/valkey-io/valkey-go"
)

func TestPublisherQueuesWithoutCreatingConsumerGroup(t *testing.T) {
	server := miniredis.RunT(t)
	publisher, err := NewPublisherE(
		WithAddress(server.Addr()),
		WithStreamName("scheduled-jobs"),
		WithGroup("must-not-be-created"),
	)
	require.NoError(t, err)

	assert.Equal(t, "valkey-streams", publisher.BackendName())
	assert.Equal(t, "scheduled-jobs", publisher.QueueName())
	require.NoError(t, publisher.Queue(
		rawMessage("payload"),
		job.AllowOption{Timeout: job.Time(time.Minute)},
	))

	entries, err := server.Stream("scheduled-jobs")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, []string{"body"}, entries[0].Values[0:1])
	message, err := job.DecodeE([]byte(entries[0].Values[1]), job.DefaultMaxMessageBytes)
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), message.Payload())
	assert.Equal(t, time.Minute, message.Timeout)

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{server.Addr()},
		DisableCache: true,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	groups, err := client.Do(
		context.Background(),
		client.B().XinfoGroups().Key("scheduled-jobs").Build(),
	).ToArray()
	require.NoError(t, err)
	assert.Empty(t, groups)

	require.NoError(t, publisher.Shutdown())
	assert.ErrorIs(t, publisher.Shutdown(), queue.ErrQueueShutdown)
	assert.ErrorIs(t, publisher.Queue(rawMessage("late")), queue.ErrQueueShutdown)
}

func TestPublisherRejectsInvalidMessagesAndHonorsCapacity(t *testing.T) {
	server := miniredis.RunT(t)
	publisher, err := NewPublisherE(
		WithAddress(server.Addr()),
		WithStreamName("scheduled-jobs"),
		WithMaxLength(1),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = publisher.Shutdown() })

	assert.Error(t, publisher.Queue(nil))
	assert.ErrorIs(t, publisher.Queue(
		rawMessage("invalid"),
		job.AllowOption{Timeout: job.Time(0)},
	), job.ErrInvalidMessage)
	require.NoError(t, publisher.Queue(rawMessage("first")))
	assert.ErrorIs(t, publisher.Queue(rawMessage("second")), queue.ErrMaxCapacity)
}

func TestNewPublisherEReturnsConfigurationAndConnectionErrors(t *testing.T) {
	publisher, err := NewPublisherE(WithAddress(""))
	assert.Nil(t, publisher)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)

	publisher, err = NewPublisherE(WithAddress("127.0.0.1:1"))
	assert.Nil(t, publisher)
	assert.Error(t, err)
}

func TestPublisherConstructorClosesClientWhenPingFails(t *testing.T) {
	server := miniredis.RunT(t)
	original := newValkeyClient
	newValkeyClient = func(option valkey.ClientOption) (valkey.Client, error) {
		client, err := valkey.NewClient(option)
		if err != nil {
			return nil, err
		}
		server.Close()
		return client, nil
	}
	t.Cleanup(func() { newValkeyClient = original })

	publisher, err := NewPublisherE(WithAddress(server.Addr()))
	assert.Nil(t, publisher)
	assert.ErrorContains(t, err, "connect to server")
}
