package valkeystream

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey "github.com/valkey-io/valkey-go"
)

func TestNativeManagementRecordsListAndInspect(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	server.SetTime(now)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	require.NoError(t, err)
	transport := newNativeTransport(client, 100, 1024)
	worker := &Worker{opts: options{
		stream: "critical", failureStream: "critical-failures",
		deadLetterStream: "critical-dead",
		management: &management.StatusMetadata{
			ID: "worker-1", Version: "1.4.0", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		},
	}, transport: transport}
	t.Cleanup(func() { require.NoError(t, transport.Close()) })

	delivery := streamqueue.Delivery{ID: "source-1", Body: []byte("secret"), Attempts: 2}
	require.NoError(t, transport.RecordFailure(
		t.Context(), "critical-failures", "critical", "workers", delivery, testFailureMetadata(),
	))
	server.SetTime(now.Add(time.Second))
	deadFailure := testFailureMetadata()
	deadFailure.Code = "terminal_delivery"
	require.NoError(t, transport.AppendDeadLetter(
		t.Context(), "critical-dead", "critical", "workers", delivery, deadFailure,
	))

	failures, err := worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortDescending,
	})
	require.NoError(t, err)
	require.Len(t, failures.Items, 1)
	assert.Equal(t, management.RecordFailure, failures.Items[0].Kind)
	assert.Equal(t, "handler_failed", failures.Items[0].FailureCode)
	assert.Equal(t, int64(len("secret")), failures.Items[0].Payload.Size)
	assert.Empty(t, failures.Items[0].Payload.Data)

	deadLetters, err := worker.ListDeadLetters(t.Context(), management.PageRequest{
		Limit: 1, Search: "critical", Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, deadLetters.Items, 1)
	assert.Equal(t, management.RecordDeadLetter, deadLetters.Items[0].Kind)
	assert.Equal(t, "terminal_delivery", deadLetters.Items[0].FailureCode)
	assert.Equal(t, "1.4.0", deadLetters.Items[0].WorkerVersion)
	require.NotNil(t, deadLetters.Items[0].LastDeliveryAt)
	assert.Equal(t, deadLetters.Items[0].OccurredAt, *deadLetters.Items[0].LastDeliveryAt)

	record, err := worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordFailure, ID: failures.Items[0].ID,
		Visibility: management.PayloadRevealed,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), record.Payload.Data)
	assert.Equal(t, "application/octet-stream", record.Payload.ContentType)

	record, err = worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: deadLetters.Items[0].ID,
		Visibility: management.PayloadRedacted,
	})
	require.NoError(t, err)
	assert.Empty(t, record.Payload.Data)
	assert.Equal(t, management.PayloadRedacted, record.Payload.Visibility)

	nativeRecords, err := transport.ReadRecords(t.Context(), "critical-failures")
	require.NoError(t, err)
	require.Len(t, nativeRecords, 1)
	outcome, err := transport.RetryRecord(
		t.Context(), "critical-dead", deadLetters.Items[0].ID,
		"retried", "", "", false,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeRetryOK, outcome)
	retried, err := client.Do(t.Context(), client.B().Xrange().Key("retried").Start("-").End("+").Build()).AsXRange()
	require.NoError(t, err)
	require.Len(t, retried, 1)
	assert.Equal(t, "secret", retried[0].FieldValues[streamBodyField])
	deleted, err := transport.DeleteRecord(t.Context(), "critical-failures", nativeRecords[0].ID)
	require.NoError(t, err)
	assert.True(t, deleted)
	deleted, err = transport.DeleteRecord(t.Context(), "critical-failures", nativeRecords[0].ID)
	require.NoError(t, err)
	assert.False(t, deleted)
	require.NoError(t, transport.PurgeRecords(t.Context(), "critical-dead"))
	require.NoError(t, transport.PurgeRecords(t.Context(), "critical-dead"))

	_, err = worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 1, Sort: management.SortAttempts, Direction: management.SortAscending,
	})
	assert.ErrorIs(t, err, management.ErrInvalidFilter)
}

func TestManagementRecordsPopulateSuppliedJobMetadata(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	message := job.NewTask(nil, job.AllowOption{Metadata: &job.Metadata{
		OriginalID: "job-123", PayloadSchemaVersion: "order.v2",
		ContentType: "application/json", EnqueuedAt: &enqueuedAt,
		RetryPolicy: "critical-v1", HandlerType: "CreateOrder",
		JobType: "order.created", Tags: map[string]string{"region": "eu"},
		TraceID: "trace-123", TenantID: "tenant-123", ProducerVersion: "1.2.3",
	}})
	worker := &Worker{opts: options{stream: "critical"}}
	records, err := worker.managementRecords([]nativeRecord{{
		ID: "1784278800000-0", OriginalID: "1784278799000-0", Body: message.Bytes(),
		Attempts: 2, OccurredAt: enqueuedAt.Add(time.Minute),
		EnvelopeVersion: management.CurrentEnvelopeVersion,
		Source:          "critical", Group: "workers",
		Classification: management.ClassificationPermanent, FailureCode: "invalid_order",
	}}, management.RecordDeadLetter, management.PayloadHidden)
	require.NoError(t, err)
	require.Len(t, records, 1)
	record := records[0]
	assert.Equal(t, "job-123", record.OriginalID)
	assert.Equal(t, "1784278799000-0", record.SourceRecordID)
	assert.Equal(t, "order.v2", record.PayloadSchemaVersion)
	assert.Equal(t, "application/json", record.Payload.ContentType)
	assert.Equal(t, enqueuedAt, *record.EnqueuedAt)
	assert.Equal(t, "critical-v1", record.RetryPolicy)
	assert.Equal(t, "CreateOrder", record.HandlerType)
	assert.Equal(t, "order.created", record.JobType)
	assert.Equal(t, map[string]string{"region": "eu"}, record.Tags)
	assert.Equal(t, "trace-123", record.TraceID)
	assert.Equal(t, "tenant-123", record.TenantID)
	assert.Equal(t, "1.2.3", record.ProducerVersion)
}

func TestNativeManagementRecordPaginationReachesEveryRetainedRecord(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	require.NoError(t, err)
	transport := newNativeTransport(client, 1, 1024)
	worker := &Worker{opts: options{
		stream: "jobs", failureStream: "failures",
	}, transport: transport}
	t.Cleanup(func() { require.NoError(t, transport.Close()) })

	const total = 1_001
	for index := range total {
		require.NoError(t, transport.RecordFailure(
			t.Context(), "failures", "jobs", "workers",
			streamqueue.Delivery{
				ID: fmt.Sprintf("%d-0", index+1), Body: []byte("payload"), Attempts: 1,
			},
			testFailureMetadata(),
		))
	}

	for _, direction := range []management.SortDirection{
		management.SortAscending,
		management.SortDescending,
	} {
		seen := make(map[string]struct{}, total)
		cursor := ""
		for {
			page, pageErr := worker.ListFailures(t.Context(), management.PageRequest{
				Cursor: cursor, Limit: management.MaxPageSize,
				Sort: management.SortOccurredAt, Direction: direction,
			})
			require.NoError(t, pageErr)
			for _, record := range page.Items {
				seen[record.ID] = struct{}{}
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}

		assert.Len(t, seen, total)
	}

	last, err := client.Do(t.Context(), client.B().Xrevrange().Key("failures").
		End("+").Start("-").Count(1).Build()).AsXRange()
	require.NoError(t, err)
	require.Len(t, last, 1)
	_, err = worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordFailure, ID: last[0].ID,
		Visibility: management.PayloadHidden,
	})
	require.NoError(t, err)
}

func TestNativeTransportAtomicallyRetriesActiveFailure(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	require.NoError(t, err)
	transport := newNativeTransport(client, 100, 1024)
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	ctx := t.Context()
	require.NoError(t, transport.EnsureGroup(ctx, "jobs", "workers"))
	id, err := transport.Add(ctx, streamqueue.AddRequest{
		Stream: "jobs", MaxLength: 100, Body: []byte("payload"),
	})
	require.NoError(t, err)
	deliveries, err := transport.Read(ctx, streamqueue.ReadRequest{
		Stream: "jobs", Group: "workers", Consumer: "worker-1",
		Count: 1, Block: time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.NoError(t, transport.RecordFailure(
		ctx, "failures", "jobs", "workers", deliveries[0], testFailureMetadata(),
	))
	records, err := transport.ReadRecords(ctx, "failures")
	require.NoError(t, err)
	require.Len(t, records, 1)

	outcome, err := transport.RetryRecord(
		ctx, "failures", records[0].ID, "jobs", "jobs", "workers", true,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeRetryOK, outcome)
	state, err := transport.GroupState(ctx, "jobs", "workers")
	require.NoError(t, err)
	assert.Zero(t, state.Pending)
	outcome, err = transport.RetryRecord(
		ctx, "failures", records[0].ID, "jobs", "jobs", "workers", true,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeRetryNotFound, outcome)

	require.NoError(t, transport.RecordFailure(
		ctx, "failures", "jobs", "workers",
		streamqueue.Delivery{ID: id, Body: []byte("payload"), Attempts: 1},
		testFailureMetadata(),
	))
	records, err = transport.ReadRecords(ctx, "failures")
	require.NoError(t, err)
	require.Len(t, records, 1)
	outcome, err = transport.RetryRecord(
		ctx, "failures", records[0].ID, "jobs", "jobs", "workers", true,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeRetryStale, outcome)

	malformedID, err := client.Do(ctx, client.B().Xadd().Key("failures").Id("*").
		FieldValue().FieldValue("unexpected", "value").Build()).ToString()
	require.NoError(t, err)
	outcome, err = transport.RetryRecord(
		ctx, "failures", malformedID, "jobs", "jobs", "workers", true,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeRetryMalformed, outcome)

	require.NoError(t, transport.RecordFailure(
		ctx, "dead", "jobs", "workers",
		streamqueue.Delivery{ID: "source-dead", Body: []byte("terminal"), Attempts: 2},
		testFailureMetadata(),
	))
	deadRecords, err := transport.ReadRecords(ctx, "dead")
	require.NoError(t, err)
	require.Len(t, deadRecords, 1)
	outcome, err = transport.RetryRecord(
		ctx, "dead", deadRecords[0].ID, "retried-dead", "jobs", "workers", false,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeRetryOK, outcome)
	retried, err := client.Do(ctx, client.B().Xrange().Key("retried-dead").
		Start("-").End("+").Build()).AsXRange()
	require.NoError(t, err)
	require.Len(t, retried, 1)
	assert.Equal(t, deadRecords[0].ID, retried[0].FieldValues[replayOriginalDeadLetterField])
	assert.Equal(t, deadRecords[0].ID, retried[0].FieldValues[replayPriorDeadLetterField])
	assert.Equal(t, "1", retried[0].FieldValues[replayGenerationField])
}

func TestNativeTransportReplayPoliciesAndBoundedRegistry(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	require.NoError(t, err)
	transport := newNativeTransport(client, 1, 1024)
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	ctx := t.Context()
	require.NoError(t, transport.AppendDeadLetter(
		ctx, "dead", "jobs", "workers",
		streamqueue.Delivery{ID: "source-1", Body: []byte("one"), Attempts: 2},
		testFailureMetadata(),
	))
	records, err := transport.ReadRecords(ctx, "dead")
	require.NoError(t, err)
	require.Len(t, records, 1)
	recordID := records[0].ID

	outcome, err := transport.ReplayRecord(
		ctx, "dead", recordID, "archive", management.ReplayRejectDuplicate,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeReplayOK, outcome)
	outcome, err = transport.ReplayRecord(
		ctx, "dead", recordID, "archive", management.ReplayRejectDuplicate,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeReplayDuplicate, outcome)
	outcome, err = transport.ReplayRecord(
		ctx, "dead", recordID, "archive", management.ReplayReplaceDuplicate,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeReplayOK, outcome)

	replayed, err := client.Do(ctx, client.B().Xrange().Key("archive").Start("-").End("+").Build()).AsXRange()
	require.NoError(t, err)
	require.Len(t, replayed, 1)
	assert.Equal(t, "one", replayed[0].FieldValues[streamBodyField])
	assert.Equal(t, recordID, replayed[0].FieldValues[replayOriginalDeadLetterField])
	assert.Equal(t, recordID, replayed[0].FieldValues[replayPriorDeadLetterField])
	assert.Equal(t, "1", replayed[0].FieldValues[replayGenerationField])
	require.NoError(t, transport.EnsureGroup(ctx, "archive", "archive-workers"))
	replayedDeliveries, err := transport.Read(ctx, streamqueue.ReadRequest{
		Stream: "archive", Group: "archive-workers", Consumer: "worker-2",
		Count: 1, Block: time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, replayedDeliveries, 1)
	assert.Equal(t, recordID, replayedDeliveries[0].OriginalDeadLetterID)
	assert.Equal(t, recordID, replayedDeliveries[0].PriorDeadLetterID)
	assert.Equal(t, uint32(1), replayedDeliveries[0].ReplayGeneration)
	require.NoError(t, transport.AppendDeadLetter(
		ctx, "repeated-dead", "archive", "archive-workers",
		replayedDeliveries[0], testFailureMetadata(),
	))
	repeatedRecords, err := transport.ReadRecords(ctx, "repeated-dead")
	require.NoError(t, err)
	require.Len(t, repeatedRecords, 1)
	assert.Equal(t, recordID, repeatedRecords[0].OriginalDeadLetterID)
	assert.Equal(t, recordID, repeatedRecords[0].PriorDeadLetterID)
	assert.Equal(t, uint32(1), repeatedRecords[0].ReplayGeneration)
	repeatedWorker := &Worker{opts: options{stream: "archive"}, transport: transport}
	repeatedItems, err := repeatedWorker.managementRecords(
		repeatedRecords, management.RecordDeadLetter, management.PayloadHidden,
	)
	require.NoError(t, err)
	require.Len(t, repeatedItems, 1)
	assert.Equal(t, recordID, repeatedItems[0].OriginalDeadLetterID)
	assert.Equal(t, recordID, repeatedItems[0].PriorDeadLetterID)
	assert.Equal(t, uint32(1), repeatedItems[0].ReplayGeneration)
	records, err = transport.ReadRecords(ctx, "dead")
	require.NoError(t, err)
	require.Len(t, records, 1, "replay must preserve its source record")

	require.NoError(t, transport.AppendDeadLetter(
		ctx, "dead", "jobs", "workers",
		streamqueue.Delivery{ID: "source-2", Body: []byte("two"), Attempts: 2},
		testFailureMetadata(),
	))
	records, err = transport.ReadRecords(ctx, "dead")
	require.NoError(t, err)
	secondID := records[len(records)-1].ID
	outcome, err = transport.ReplayRecord(
		ctx, "dead", secondID, "archive", management.ReplayRejectDuplicate,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeReplayOK, outcome)
	registrySize, err := client.Do(ctx, client.B().Hlen().Key("archive:queue:replay-index").Build()).ToInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), registrySize)
	orderSize, err := client.Do(ctx, client.B().Zcard().Key("archive:queue:replay-order").Build()).ToInt64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), orderSize)

	outcome, err = transport.ReplayRecord(
		ctx, "dead", "9999999999999-0", "archive", management.ReplayRejectDuplicate,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeReplayNotFound, outcome)
	malformedID, err := client.Do(ctx, client.B().Xadd().Key("malformed").Id("*").
		FieldValue().FieldValue("unexpected", "value").Build()).ToString()
	require.NoError(t, err)
	outcome, err = transport.ReplayRecord(
		ctx, "malformed", malformedID, "archive", management.ReplayRejectDuplicate,
	)
	require.NoError(t, err)
	assert.Equal(t, nativeReplayMalformed, outcome)
}

func TestNativeManagementRecordsPaginationValidationAndFailures(t *testing.T) {
	worker := &Worker{}
	request := management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	}
	_, err := worker.ListFailures(context.Background(), request)
	assert.ErrorIs(t, err, ErrManagementRecordsDisabled)
	assert.ErrorIs(t, err, management.ErrUnsupportedCapability)
	_, err = worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "missing", Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, ErrManagementRecordsDisabled)
	assert.ErrorIs(t, err, management.ErrUnsupportedCapability)

	transport := &recordTransportStub{}
	worker = &Worker{opts: options{stream: "jobs", failureStream: "failures"}, transport: transport}
	_, err = worker.ListFailures(context.Background(), management.PageRequest{})
	assert.Error(t, err)
	_, err = worker.ListFailures(context.Background(), management.PageRequest{
		Limit: 1, Cursor: "invalid", Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	assert.ErrorIs(t, err, management.ErrMalformedCursor)

	transport.records = []nativeRecord{
		{ID: "2-0", OriginalID: "source-2", Body: []byte("two"), Attempts: 2, OccurredAt: time.UnixMilli(2)},
		{ID: "1-0", OriginalID: "source-1", Body: []byte("one"), Attempts: 1, OccurredAt: time.UnixMilli(1)},
	}
	first, err := worker.ListFailures(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	assert.NotEmpty(t, first.NextCursor)
	second, err := worker.ListFailures(context.Background(), management.PageRequest{
		Limit: 1, Cursor: first.NextCursor, Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	assert.Empty(t, second.NextCursor)
	assert.NotEqual(t, first.Items[0].ID, second.Items[0].ID)

	transport.err = assert.AnError
	_, err = worker.ListFailures(context.Background(), request)
	assert.ErrorIs(t, err, assert.AnError)
	transport.err = nil
	empty, err := worker.ListFailures(context.Background(), management.PageRequest{
		Limit: 1, Search: "absent", Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	require.NoError(t, err)
	assert.Empty(t, empty.Items)
	transport.records = []nativeRecord{{ID: "bad", Attempts: 0, OccurredAt: time.Unix(1, 0)}}
	_, err = worker.ListFailures(context.Background(), request)
	assert.Error(t, err)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = worker.ListFailures(cancelled, request)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNativeManagementRecordValidationAndSorting(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	worker := &Worker{opts: options{stream: "jobs"}}
	for name, records := range map[string][]nativeRecord{
		"zero attempts":  {{ID: "1-0", Attempts: 0, OccurredAt: now}},
		"large attempts": {{ID: "1-0", Attempts: math.MaxUint32 + 1, OccurredAt: now}},
		"invalid record": {{ID: "1-0", Attempts: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := worker.managementRecords(records, management.RecordFailure, management.PayloadHidden)
			assert.Error(t, err)
		})
	}

	items := []management.JobRecord{
		{ID: "b", Queue: "z", OccurredAt: now.Add(time.Second), Attempts: 1},
		{ID: "a", Queue: "a", OccurredAt: now, Attempts: 2},
	}
	for _, test := range []struct {
		field     management.SortField
		direction management.SortDirection
		want      string
	}{
		{management.SortOccurredAt, management.SortAscending, "a"},
		{management.SortOccurredAt, management.SortDescending, "b"},
		{management.SortQueue, management.SortAscending, "a"},
		{management.SortQueue, management.SortDescending, "b"},
		{management.SortAttempts, management.SortAscending, "b"},
		{management.SortAttempts, management.SortDescending, "a"},
	} {
		copyItems := append([]management.JobRecord(nil), items...)
		sortRecords(copyItems, test.field, test.direction)
		assert.Equal(t, test.want, copyItems[0].ID)
	}

	tied := append([]management.JobRecord(nil), items...)
	tied[0].Queue = "same"
	tied[1].Queue = "same"
	sortRecords(tied, management.SortQueue, management.SortAscending)
	assert.Equal(t, "a", tied[0].ID)
}

func TestNativeManagementRecordConversionAndCursors(t *testing.T) {
	valid := valkey.XRangeEntry{ID: "1000-0", FieldValues: map[string]string{
		streamBodyField: "body", originalIDField: "source",
		deliveryAttemptsField: "2",
	}}
	records, err := convertNativeRecords([]valkey.XRangeEntry{valid})
	require.NoError(t, err)
	assert.Equal(t, time.UnixMilli(1000).UTC(), records[0].OccurredAt)
	versioned := valid
	versioned.FieldValues = map[string]string{
		streamBodyField: "body", originalIDField: "source",
		deliveryAttemptsField: "2", envelopeVersionField: "1",
		classificationField: string(management.ClassificationPermanent),
		failureCodeField:    "invalid_order", consumerGroupField: "workers",
	}
	_, err = convertNativeRecords([]valkey.XRangeEntry{versioned})
	assert.Error(t, err)
	versioned.FieldValues[sourceStreamField] = "jobs"
	versioned.FieldValues[replayOriginalDeadLetterField] = "dead-1"
	_, err = convertNativeRecords([]valkey.XRangeEntry{versioned})
	assert.Error(t, err)

	for name, mutate := range map[string]func(*valkey.XRangeEntry){
		"missing body":     func(entry *valkey.XRangeEntry) { delete(entry.FieldValues, streamBodyField) },
		"missing original": func(entry *valkey.XRangeEntry) { delete(entry.FieldValues, originalIDField) },
		"empty original":   func(entry *valkey.XRangeEntry) { entry.FieldValues[originalIDField] = "" },
		"missing attempts": func(entry *valkey.XRangeEntry) { delete(entry.FieldValues, deliveryAttemptsField) },
		"invalid attempts": func(entry *valkey.XRangeEntry) { entry.FieldValues[deliveryAttemptsField] = "x" },
		"zero attempts":    func(entry *valkey.XRangeEntry) { entry.FieldValues[deliveryAttemptsField] = "0" },
		"invalid id":       func(entry *valkey.XRangeEntry) { entry.ID = "x" },
	} {
		t.Run(name, func(t *testing.T) {
			entry := valid
			entry.FieldValues = map[string]string{}
			for key, value := range valid.FieldValues {
				entry.FieldValues[key] = value
			}
			mutate(&entry)
			_, err := convertNativeRecords([]valkey.XRangeEntry{entry})
			assert.Error(t, err)
		})
	}
	_, err = recordTime("x-y")
	assert.Error(t, err)

	negative := base64.RawURLEncoding.EncodeToString([]byte("-1"))
	overflow := base64.RawURLEncoding.EncodeToString([]byte("999999999999999999999999"))
	for _, cursor := range []string{"%", negative, overflow} {
		_, err := decodeRecordCursor(cursor)
		assert.Error(t, err)
	}
}

func TestNativeManagementRecordInspectionErrors(t *testing.T) {
	transport := &recordTransportStub{records: []nativeRecord{{
		ID: "1-0", OriginalID: "source", Attempts: 1, OccurredAt: time.Unix(1, 0),
	}}}
	worker := &Worker{opts: options{
		stream: "jobs", failureStream: "failures", deadLetterStream: "dead",
	}, transport: transport}
	_, err := worker.Inspect(context.Background(), management.InspectRequest{})
	assert.Error(t, err)
	record, err := worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "1-0", Visibility: management.PayloadHidden,
	})
	require.NoError(t, err)
	assert.Equal(t, "1-0", record.ID)
	_, err = worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "missing", Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, ErrManagementRecordNotFound)
	assert.ErrorIs(t, err, management.ErrRecordNotFound)
	transport.err = assert.AnError
	_, err = worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: "1-0", Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, assert.AnError)
	transport.err = nil
	transport.records = []nativeRecord{{ID: "bad", Attempts: 0, OccurredAt: time.Unix(1, 0)}}
	_, err = worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "bad", Visibility: management.PayloadHidden,
	})
	assert.Error(t, err)
}

func TestNativeManagementRecordPageErrors(t *testing.T) {
	valid := nativeRecord{
		ID: "1-0", OriginalID: "source", Attempts: 1, OccurredAt: time.Unix(1, 0),
	}
	transport := &recordPageTransportStub{recordTransportStub: recordTransportStub{
		records: []nativeRecord{valid},
	}}
	worker := &Worker{opts: options{
		stream: "jobs", failureStream: "failures",
	}, transport: transport}
	request := management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	}

	request.Cursor = "%"
	_, err := worker.ListFailures(t.Context(), request)
	assert.ErrorIs(t, err, management.ErrMalformedCursor)

	request.Cursor = ""
	transport.err = assert.AnError
	_, err = worker.ListFailures(t.Context(), request)
	assert.ErrorIs(t, err, assert.AnError)

	transport.err = nil
	transport.records[0].Attempts = 0
	_, err = worker.ListFailures(t.Context(), request)
	assert.Error(t, err)

	transport.records = []nativeRecord{valid}
	transport.readErr = assert.AnError
	_, err = worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordFailure, ID: valid.ID,
		Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, assert.AnError)

	transport.readErr = nil
	transport.found = false
	_, err = worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordFailure, ID: valid.ID,
		Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, management.ErrRecordNotFound)

	transport.found = true
	transport.records[0].Attempts = 0
	_, err = worker.Inspect(t.Context(), management.InspectRequest{
		Kind: management.RecordFailure, ID: valid.ID,
		Visibility: management.PayloadHidden,
	})
	assert.Error(t, err)

	for _, cursor := range []string{
		"%",
		base64.RawURLEncoding.EncodeToString([]byte("invalid")),
		base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("1", management.MaxIdentityBytes+1))),
	} {
		_, err = decodeNativeRecordCursor(cursor)
		assert.ErrorIs(t, err, management.ErrMalformedCursor)
	}
}

func TestNativeFailureRecordingPrecedesRetryAndPropagatesErrors(t *testing.T) {
	message := job.NewMessage(rawMessage("payload"))
	transport := &recordTransportStub{}
	worker := &Worker{opts: options{
		failureStream: "failures", maxDeliveryAttempts: 3,
		commandTimeout: time.Second,
	}, transport: transport}
	decoded, err := worker.decode(streamqueue.Delivery{
		ID: "1-0", Body: message.Bytes(), Attempts: 1,
	})
	require.NoError(t, err)
	require.NoError(t, decoded.(*job.Message).Nack())
	assert.Equal(t, 1, transport.failureCalls)

	transport.recordErr = errors.New("record failed")
	message = job.NewMessage(rawMessage("payload"))
	decoded, err = worker.decode(streamqueue.Delivery{
		ID: "2-0", Body: message.Bytes(), Attempts: 1,
	})
	require.NoError(t, err)
	assert.ErrorIs(t, decoded.(*job.Message).Nack(), transport.recordErr)
}

type recordTransportStub struct {
	records      []nativeRecord
	err          error
	recordErr    error
	failureCalls int
}

type recordPageTransportStub struct {
	recordTransportStub
	readErr error
	found   bool
}

func (s *recordPageTransportStub) ReadRecordPage(
	context.Context, string, string, int64, management.SortDirection,
) ([]nativeRecord, error) {
	return s.records, s.err
}

func (s *recordPageTransportStub) ReadRecord(
	context.Context, string, string,
) (nativeRecord, bool, error) {
	if s.readErr != nil || !s.found {
		return nativeRecord{}, false, s.readErr
	}
	return s.records[0], true, nil
}

func (s *recordTransportStub) ReadRecords(context.Context, string) ([]nativeRecord, error) {
	return s.records, s.err
}
func (s *recordTransportStub) RecordFailure(
	context.Context, string, string, string, streamqueue.Delivery, streamqueue.FailureMetadata,
) error {
	s.failureCalls++
	return s.recordErr
}
func (*recordTransportStub) AppendDeadLetter(
	context.Context, string, string, string, streamqueue.Delivery, streamqueue.FailureMetadata,
) error {
	return nil
}
func (*recordTransportStub) EnsureGroup(context.Context, string, string) error { return nil }
func (*recordTransportStub) Add(context.Context, streamqueue.AddRequest) (string, error) {
	return "", nil
}
func (*recordTransportStub) Read(context.Context, streamqueue.ReadRequest) ([]streamqueue.Delivery, error) {
	return nil, nil
}
func (*recordTransportStub) Claim(context.Context, streamqueue.ClaimRequest) (streamqueue.ClaimResult, error) {
	return streamqueue.ClaimResult{}, nil
}
func (*recordTransportStub) Ack(context.Context, streamqueue.AckRequest) error { return nil }
func (*recordTransportStub) DeadLetter(context.Context, streamqueue.DeadLetterRequest) error {
	return nil
}
func (*recordTransportStub) GroupState(context.Context, string, string) (streamqueue.GroupState, error) {
	return streamqueue.GroupState{}, nil
}
func (*recordTransportStub) Close() error { return nil }

func testFailureMetadata() streamqueue.FailureMetadata {
	return streamqueue.FailureMetadata{
		Classification: management.ClassificationRetryable,
		Code:           "handler_failed",
	}
}
