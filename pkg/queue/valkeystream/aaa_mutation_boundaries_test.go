package valkeystream

import (
	"crypto/tls"
	"encoding/base64"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey "github.com/valkey-io/valkey-go"
)

func TestNativeClientOptionsCloneTLSConfiguration(t *testing.T) {
	withoutTLS := nativeClientOptions(options{})
	assert.Nil(t, withoutTLS.TLSConfig)

	source := &tls.Config{MinVersion: tls.VersionTLS13}
	withTLS := nativeClientOptions(options{tlsConfig: source})
	require.NotNil(t, withTLS.TLSConfig)
	assert.NotSame(t, source, withTLS.TLSConfig)
	assert.Equal(t, uint16(tls.VersionTLS13), withTLS.TLSConfig.MinVersion)
}

func TestNativeResponseBoundaryParsersRejectMalformedValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		valid bool
	}{
		{name: "attempts have wrong type", value: []any{[]any{"1-0", "worker", int64(1), "1"}}},
		{name: "attempts are zero", value: []any{[]any{"1-0", "worker", int64(1), int64(0)}}},
		{name: "attempts are one", value: []any{[]any{"1-0", "worker", int64(1), int64(1)}}, valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts, err := parsePendingAttempts(test.value)
			if test.valid {
				require.NoError(t, err)
				assert.Equal(t, int64(1), attempts)
				return
			}
			assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
		})
	}

	fields, ok := alternatingFields([]any{"first", "one", "second", "two"})
	require.True(t, ok)
	assert.Equal(t, map[string]string{"first": "one", "second": "two"}, fields)
}

func TestNativeControllerPreservesRetryTargetKind(t *testing.T) {
	records := []nativeRecord{
		{ID: "1-0", Body: []byte("one"), Attempts: 1, OccurredAt: time.Unix(1, 0)},
		{ID: "2-0", Body: []byte("two"), Attempts: 1, OccurredAt: time.Unix(2, 0)},
	}
	transport := &mutationTransportStub{
		recordTransportStub: &recordTransportStub{records: records},
	}
	worker := controlledWorker(transport)

	failure := nativeCommand("failure", management.CommandRetry, management.TargetFailure, "1-0")
	result, err := worker.Execute(t.Context(), failure)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)

	deadLetter := nativeCommand("dead", management.CommandRetry, management.TargetDeadLetter, "2-0")
	result, err = worker.Execute(t.Context(), deadLetter)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	assert.Equal(t, []bool{true, false}, transport.failureTargets)

	transport.failureTargets = nil
	bulk := nativeCommand("bulk", management.CommandBulkRetry, management.TargetFailure, "failures")
	bulk.Confirmed = true
	bulk.Selection = &management.Selection{Limit: 2}
	result, err = worker.Execute(t.Context(), bulk)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	assert.Equal(t, []bool{true, true}, transport.failureTargets)
}

func TestNativeTransportAcceptsExactRecordBoundsAndOrdering(t *testing.T) {
	backendErr := assert.AnError
	transport, _ := faultTransport(t, stubResult{err: backendErr})
	err := transport.AppendDeadLetter(
		t.Context(), "dead", "jobs", "workers",
		streamqueue.Delivery{ID: "1-0", Body: []byte(strings.Repeat("x", 10)), Attempts: 1},
		testFailureMetadata(),
	)
	assert.ErrorIs(t, err, backendErr)
	assert.NotErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)

	transport, _ = faultTransport(t, stubResult{err: backendErr})
	_, err = transport.ReadRecordPage(t.Context(), "records", "", 1, management.SortAscending)
	assert.ErrorIs(t, err, backendErr)
	assert.NotErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
	_, err = transport.ReadRecordPage(t.Context(), "records", "", 0, management.SortAscending)
	assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)

	server := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	native := newNativeTransport(client, 10, 1024)
	for _, id := range []string{"source-1", "source-2"} {
		require.NoError(t, native.AppendDeadLetter(
			t.Context(), "records", "jobs", "workers",
			streamqueue.Delivery{ID: id, Body: []byte(id), Attempts: 1},
			testFailureMetadata(),
		))
	}
	ascending, err := native.ReadRecordPage(
		t.Context(), "records", "", 1, management.SortAscending,
	)
	require.NoError(t, err)
	descending, err := native.ReadRecordPage(
		t.Context(), "records", "", 1, management.SortDescending,
	)
	require.NoError(t, err)
	require.Len(t, ascending, 1)
	require.Len(t, descending, 1)
	assert.NotEqual(t, ascending[0].ID, descending[0].ID)
	assert.Equal(t, "source-1", ascending[0].OriginalID)
	assert.Equal(t, "source-2", descending[0].OriginalID)
}

func TestReplayLineageRequiresCompleteBoundedMetadata(t *testing.T) {
	valid := map[string]string{
		replayOriginalDeadLetterField: strings.Repeat("o", management.MaxIdentityBytes),
		replayPriorDeadLetterField:    strings.Repeat("p", management.MaxIdentityBytes),
		replayGenerationField:         "1",
	}
	original, prior, generation, err := parseReplayLineage(valid)
	require.NoError(t, err)
	assert.Len(t, original, management.MaxIdentityBytes)
	assert.Len(t, prior, management.MaxIdentityBytes)
	assert.Equal(t, uint32(1), generation)

	for name, mutate := range map[string]func(map[string]string){
		"missing original":   func(fields map[string]string) { delete(fields, replayOriginalDeadLetterField) },
		"missing prior":      func(fields map[string]string) { delete(fields, replayPriorDeadLetterField) },
		"missing generation": func(fields map[string]string) { delete(fields, replayGenerationField) },
		"blank original":     func(fields map[string]string) { fields[replayOriginalDeadLetterField] = " " },
		"blank prior":        func(fields map[string]string) { fields[replayPriorDeadLetterField] = " " },
		"long original":      func(fields map[string]string) { fields[replayOriginalDeadLetterField] += "x" },
		"long prior":         func(fields map[string]string) { fields[replayPriorDeadLetterField] += "x" },
	} {
		t.Run(name, func(t *testing.T) {
			fields := make(map[string]string, len(valid))
			for key, value := range valid {
				fields[key] = value
			}
			mutate(fields)
			_, _, _, err := parseReplayLineage(fields)
			assert.Error(t, err)
		})
	}

	original, prior, generation, err = parseReplayLineage(map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, original)
	assert.Empty(t, prior)
	assert.Zero(t, generation)
}

func TestNativeRecordPaginationAndConversionExactBounds(t *testing.T) {
	records := []nativeRecord{
		{ID: "1-0", OriginalID: "one", Attempts: 1, OccurredAt: time.UnixMilli(1)},
		{ID: "2-0", OriginalID: "two", Attempts: 2, OccurredAt: time.UnixMilli(2)},
	}
	transport := &recordPageTransportStub{recordTransportStub: recordTransportStub{records: records}}
	worker := &Worker{opts: options{stream: "jobs", failureStream: "failures"}, transport: transport}
	request := management.PageRequest{
		Limit: 1, Search: "1-0", Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	}
	page, err := worker.ListFailures(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, int64(valkeyRecordSearchFactor), transport.lastLimit)

	transport.records = records
	request.Search = ""
	page, err = worker.ListFailures(t.Context(), request)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "1-0", page.Items[0].ID)

	plain := &recordTransportStub{records: records}
	worker.transport = plain
	empty, err := worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 1, Cursor: encodeRecordCursor(len(records)),
		Sort: management.SortOccurredAt, Direction: management.SortAscending,
	})
	require.NoError(t, err)
	assert.Empty(t, empty.Items)
	bounded, err := worker.ListFailures(t.Context(), management.PageRequest{
		Limit: management.MaxPageSize, Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	require.NoError(t, err)
	assert.Len(t, bounded.Items, len(records))

	worker.transport = &fakeTransport{}
	_, err = worker.nativeRecords(t.Context(), "failures")
	assert.ErrorIs(t, err, ErrManagementRecordsDisabled)
	worker.transport = plain
	_, err = worker.nativeRecords(t.Context(), "")
	assert.ErrorIs(t, err, ErrManagementRecordsDisabled)

	for _, attempts := range []int64{1, math.MaxUint32} {
		items, err := worker.managementRecords([]nativeRecord{{
			ID: "1-0", OriginalID: "source", Attempts: attempts,
			OccurredAt: time.UnixMilli(1),
		}}, management.RecordFailure, management.PayloadHidden)
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, uint32(attempts), items[0].Attempts)
		assert.Equal(t, "handler_failed", items[0].FailureCode)
	}
	for _, attempts := range []int64{0, math.MaxUint32 + 1} {
		_, err := worker.managementRecords([]nativeRecord{{
			ID: "1-0", OriginalID: "source", Attempts: attempts,
			OccurredAt: time.UnixMilli(1),
		}}, management.RecordFailure, management.PayloadHidden)
		assert.EqualError(t, err, "valkeystream: invalid management record attempts")
	}
	deadLetters, err := worker.managementRecords([]nativeRecord{{
		ID: "1-0", OriginalID: "source", Attempts: 1, OccurredAt: time.UnixMilli(1),
	}}, management.RecordDeadLetter, management.PayloadHidden)
	require.NoError(t, err)
	assert.Equal(t, "terminal_delivery", deadLetters[0].FailureCode)
}

func TestNativeRecordVersionAndCursorBoundaries(t *testing.T) {
	base := map[string]string{
		streamBodyField: "body", originalIDField: "source", deliveryAttemptsField: "1",
		envelopeVersionField: "1", classificationField: string(management.ClassificationPermanent),
		failureCodeField: "invalid_order", sourceStreamField: "jobs", consumerGroupField: "workers",
	}
	valid := valkey.XRangeEntry{ID: "1-0", FieldValues: base}
	records, err := convertNativeRecords([]valkey.XRangeEntry{
		{ID: "0-0", FieldValues: map[string]string{
			streamBodyField: "legacy", originalIDField: "legacy", deliveryAttemptsField: "1",
		}},
		valid,
	})
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Zero(t, records[0].EnvelopeVersion)
	assert.Equal(t, management.CurrentEnvelopeVersion, records[1].EnvelopeVersion)

	for name, mutate := range map[string]func(map[string]string){
		"invalid version": func(fields map[string]string) { fields[envelopeVersionField] = "x" },
		"wrong version":   func(fields map[string]string) { fields[envelopeVersionField] = "2" },
		"bad failure":     func(fields map[string]string) { fields[failureCodeField] = "" },
		"missing source":  func(fields map[string]string) { fields[sourceStreamField] = "" },
		"missing group":   func(fields map[string]string) { fields[consumerGroupField] = "" },
	} {
		t.Run(name, func(t *testing.T) {
			fields := make(map[string]string, len(base))
			for key, value := range base {
				fields[key] = value
			}
			mutate(fields)
			_, err := convertNativeRecords([]valkey.XRangeEntry{{ID: "1-0", FieldValues: fields}})
			assert.Error(t, err)
		})
	}

	assertRecordCursorRoundTrip(t, 0)
	assertRecordCursorRoundTrip(t, 1)
	for _, cursor := range []string{"MB", base64.RawURLEncoding.EncodeToString([]byte("-1"))} {
		_, err := decodeRecordCursor(cursor)
		assert.ErrorIs(t, err, management.ErrMalformedCursor)
	}
	exactID := "1-" + strings.Repeat("0", management.MaxIdentityBytes-2)
	encoded := encodeNativeRecordCursor(exactID)
	decoded, err := decodeNativeRecordCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, exactID, decoded)
	canonical := encodeNativeRecordCursor("1-00")
	nonCanonical := canonical[:len(canonical)-1] + "B"
	decodedBytes, decodeErr := base64.RawURLEncoding.DecodeString(nonCanonical)
	require.NoError(t, decodeErr)
	assert.Equal(t, "1-00", string(decodedBytes))
	_, err = decodeNativeRecordCursor(nonCanonical)
	assert.ErrorIs(t, err, management.ErrMalformedCursor)
}

func TestSortRecordsPreservesStableTies(t *testing.T) {
	items := []management.JobRecord{
		{ID: "same", Queue: "same", Attempts: 1, Payload: management.Payload{Size: 1}},
		{ID: "same", Queue: "same", Attempts: 1, Payload: management.Payload{Size: 2}},
	}
	for _, direction := range []management.SortDirection{
		management.SortAscending, management.SortDescending,
	} {
		copyItems := append([]management.JobRecord(nil), items...)
		sortRecords(copyItems, management.SortAttempts, direction)
		assert.Equal(t, int64(1), copyItems[0].Payload.Size)
		assert.Equal(t, int64(2), copyItems[1].Payload.Size)
	}
}

func TestWorkerStatusRequiresMetadataAndStartTimeIndependently(t *testing.T) {
	metadata := &management.StatusMetadata{
		ID: "worker-1", Version: "v1", Concurrency: 1,
		Protocol: management.ProtocolVersion{Major: 1},
	}
	for _, worker := range []*Worker{
		{opts: options{management: metadata}},
		{startedAt: time.Now()},
	} {
		_, err := worker.ObserveWorker(t.Context())
		assert.ErrorIs(t, err, ErrManagementStatusDisabled)
	}
}

func assertRecordCursorRoundTrip(t *testing.T, offset int) {
	t.Helper()
	decoded, err := decodeRecordCursor(encodeRecordCursor(offset))
	require.NoError(t, err)
	assert.Equal(t, offset, decoded)
}
