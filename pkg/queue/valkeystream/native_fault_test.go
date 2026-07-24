package valkeystream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey "github.com/valkey-io/valkey-go"
)

func TestNativeResponseParsersRejectMalformedData(t *testing.T) {
	validEntry := []any{"1-0", []any{streamBodyField, "body"}}
	claimCases := map[string]any{
		"not an array":         "invalid",
		"too short":            []any{"0-0"},
		"invalid cursor":       []any{int64(1), []any{}},
		"empty cursor":         []any{"", []any{}},
		"invalid entries":      []any{"0-0", "invalid"},
		"invalid entry":        []any{"0-0", []any{"invalid"}},
		"invalid entry length": []any{"0-0", []any{[]any{"1-0"}}},
		"invalid identifier":   []any{"0-0", []any{[]any{int64(1), []any{streamBodyField, "body"}}}},
		"empty identifier":     []any{"0-0", []any{[]any{"", []any{streamBodyField, "body"}}}},
		"invalid fields":       []any{"0-0", []any{[]any{"1-0", "invalid"}}},
		"missing body":         []any{"0-0", []any{[]any{"1-0", []any{"other", "value"}}}},
		"invalid lineage": []any{"0-0", []any{[]any{"1-0", []any{
			streamBodyField, "body", replayOriginalDeadLetterField, "dead-1",
		}}}},
	}
	for name, value := range claimCases {
		t.Run("claim "+name, func(t *testing.T) {
			_, err := parseClaimResponse(value)
			assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
		})
	}
	claim, err := parseClaimResponse([]any{"0-0", []any{validEntry}})
	require.NoError(t, err)
	require.Len(t, claim.Deliveries, 1)
	assert.Equal(t, []byte("body"), claim.Deliveries[0].Body)
	assert.True(t, claim.Deliveries[0].Reclaimed)

	_, err = convertEntries([]valkey.XRangeEntry{{
		ID: "1-0", FieldValues: map[string]string{
			streamBodyField: "body", replayGenerationField: "invalid",
		},
	}}, 1, false)
	assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	_, _, _, err = parseReplayLineage(map[string]string{
		replayOriginalDeadLetterField: "dead-1",
		replayPriorDeadLetterField:    "dead-1",
		replayGenerationField:         "0",
	})
	assert.Error(t, err)

	pendingCases := []any{
		"invalid",
		[]any{},
		[]any{int64(1)},
		[]any{[]any{"1-0"}},
		[]any{[]any{"1-0", "worker", int64(1), "invalid"}},
		[]any{[]any{"1-0", "worker", int64(1), int64(0)}},
	}
	for _, value := range pendingCases {
		_, err = parsePendingAttempts(value)
		assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	}
	attempts, err := parsePendingAttempts([]any{[]any{"1-0", "worker", int64(1), int64(3)}})
	require.NoError(t, err)
	assert.Equal(t, int64(3), attempts)

	oldestPendingCases := []any{
		"invalid",
		[]any{},
		[]any{[]any{"1-0"}},
		[]any{[]any{int64(1), "worker", int64(1), int64(1)}},
		[]any{[]any{"", "worker", int64(1), int64(1)}},
	}
	for _, value := range oldestPendingCases {
		_, err = parseOldestPendingID(value)
		assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	}
	id, err := parseOldestPendingID([]any{[]any{"1-0", "worker", int64(1), int64(3)}})
	require.NoError(t, err)
	assert.Equal(t, "1-0", id)

	groupCases := []any{
		"invalid",
		[]any{"invalid"},
		[]any{map[string]any{"name": int64(1)}},
		[]any{map[string]any{"name": "workers", "pending": "invalid", "lag": int64(0)}},
		[]any{map[string]any{"name": "workers", "pending": int64(-1), "lag": int64(0)}},
		[]any{map[string]any{"name": "workers", "pending": int64(0), "lag": int64(-2)}},
	}
	for _, value := range groupCases {
		_, err = parseGroupState(value, "workers")
		assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	}
	state, err := parseGroupState([]any{
		map[string]any{"name": "other", "pending": int64(9), "lag": int64(9)},
		map[string]any{"name": "workers", "pending": int64(2), "lag": int64(-1)},
	}, "workers")
	require.NoError(t, err)
	assert.Equal(t, streamqueue.GroupState{Pending: 2, Lag: -1}, state)
	_, err = parseGroupState([]any{map[string]any{
		"name": "other", "pending": int64(0), "lag": int64(0),
	}}, "workers")
	assert.ErrorContains(t, err, "does not exist")
}

func TestAlternatingFieldsRequireStringPairs(t *testing.T) {
	tests := []any{
		"invalid",
		[]any{"field"},
		[]any{int64(1), "value"},
		[]any{"", "value"},
		[]any{"field", int64(1)},
	}
	for _, value := range tests {
		_, ok := alternatingFields(value)
		assert.False(t, ok)
	}
	fields, ok := alternatingFields([]any{"field", "value"})
	assert.True(t, ok)
	assert.Equal(t, map[string]string{"field": "value"}, fields)
}

func TestNativeTransportPropagatesValidationAndBackendFailures(t *testing.T) {
	backendErr := errors.New("backend failed")
	ctx := context.Background()

	t.Run("ensure group", func(t *testing.T) {
		transport, client := faultTransport(t, stubResult{err: backendErr})
		assert.ErrorIs(t, transport.EnsureGroup(ctx, "", "group"), streamqueue.ErrInvalidSemanticRequest)
		assert.ErrorIs(t, transport.EnsureGroup(ctx, "stream", "group"), backendErr)
		assert.False(t, client.closed)
	})

	t.Run("add", func(t *testing.T) {
		transport, _ := faultTransport(t, stubResult{err: backendErr})
		_, err := transport.Add(ctx, streamqueue.AddRequest{})
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.Add(ctx, streamqueue.AddRequest{Stream: "stream", MaxLength: 1, Body: []byte("x")})
		assert.ErrorIs(t, err, backendErr)
		assert.NotContains(t, err.Error(), backendErr.Error())
	})

	t.Run("read", func(t *testing.T) {
		transport, client := faultTransport(t, stubResult{err: backendErr})
		_, err := transport.Read(ctx, streamqueue.ReadRequest{})
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		request := streamqueue.ReadRequest{Stream: "stream", Group: "group", Consumer: "worker", Count: 1, Block: time.Millisecond}
		_, err = transport.Read(ctx, request)
		assert.ErrorIs(t, err, backendErr)
		client.results = []stubResult{{streams: map[string][]valkey.XRangeEntry{}}}
		deliveries, err := transport.Read(ctx, request)
		require.NoError(t, err)
		assert.Empty(t, deliveries)
	})

	t.Run("claim", func(t *testing.T) {
		transport, client := faultTransport(t, stubResult{err: backendErr})
		_, err := transport.Claim(ctx, streamqueue.ClaimRequest{})
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		request := streamqueue.ClaimRequest{
			Stream: "stream", Group: "group", Consumer: "worker",
			MinIdle: time.Second, Start: "0-0", Count: 1,
		}
		_, err = transport.Claim(ctx, request)
		assert.ErrorIs(t, err, backendErr)
		client.results = []stubResult{{value: []any{}}}
		_, err = transport.Claim(ctx, request)
		assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
		client.results = []stubResult{
			{value: []any{"0-0", []any{[]any{"1-0", []any{streamBodyField, "body"}}}}},
			{err: backendErr},
		}
		_, err = transport.Claim(ctx, request)
		assert.ErrorIs(t, err, backendErr)
	})

	t.Run("ack", func(t *testing.T) {
		transport, client := faultTransport(t, stubResult{err: backendErr})
		assert.ErrorIs(t, transport.Ack(ctx, streamqueue.AckRequest{}), streamqueue.ErrInvalidSemanticRequest)
		request := streamqueue.AckRequest{Stream: "stream", Group: "group", ID: "1-0"}
		assert.ErrorIs(t, transport.Ack(ctx, request), backendErr)
		client.results = []stubResult{{integer: 0}}
		resolution := management.ResolveFailure(transport.Ack(ctx, request))
		assert.Equal(t, management.ClassificationInfrastructure, resolution.Classification)
		assert.Equal(t, management.FailureCodeLeaseLost, resolution.Code)
	})

	t.Run("dead letter", func(t *testing.T) {
		request := streamqueue.DeadLetterRequest{
			Source: "stream", Destination: "dead", Group: "group",
			Delivery: streamqueue.Delivery{ID: "1-0", Body: []byte("x"), Attempts: 2},
			Failure:  testFailureMetadata(),
		}
		transport, client := faultTransport(t, stubResult{err: backendErr})
		assert.ErrorIs(t, transport.DeadLetter(ctx, streamqueue.DeadLetterRequest{}), streamqueue.ErrInvalidSemanticRequest)
		assert.ErrorIs(t, transport.DeadLetter(ctx, request), backendErr)
		client.results = []stubResult{{}, {integer: 0}}
		assert.ErrorContains(t, transport.DeadLetter(ctx, request), "settle dead letter source")
	})

	t.Run("group state and close", func(t *testing.T) {
		transport, client := faultTransport(t, stubResult{err: backendErr})
		_, err := transport.GroupState(ctx, "", "group")
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.GroupState(ctx, "stream", "group")
		assert.ErrorIs(t, err, backendErr)
		client.results = []stubResult{
			{value: []any{map[string]any{"name": "group", "pending": int64(1), "lag": int64(0)}}},
			{err: backendErr},
		}
		_, err = transport.GroupState(ctx, "stream", "group")
		assert.ErrorIs(t, err, backendErr)
		assert.NoError(t, transport.Close())
		assert.True(t, client.closed)
	})
}

func TestNativeTransportRecordFailures(t *testing.T) {
	backendErr := errors.New("backend")
	transport, client := faultTransport(t,
		stubResult{err: backendErr}, stubResult{err: backendErr}, stubResult{err: backendErr},
	)
	delivery := streamqueue.Delivery{ID: "1-0", Body: []byte("body"), Attempts: 1}

	assert.ErrorIs(t, transport.RecordFailure(
		context.Background(), "failures", "jobs", "workers", delivery, testFailureMetadata(),
	), backendErr)
	deadLetterErr := transport.AppendDeadLetter(
		context.Background(), "dead", "jobs", "workers", delivery, testFailureMetadata(),
	)
	assert.ErrorIs(t, deadLetterErr, backendErr)
	resolution := management.ResolveFailure(deadLetterErr)
	assert.Equal(t, management.ClassificationInfrastructure, resolution.Classification)
	assert.Equal(t, management.FailureCodeDeadLetterDestinationUnavailable, resolution.Code)
	_, err := transport.ReadRecords(context.Background(), "records")
	assert.ErrorIs(t, err, backendErr)

	for _, invalid := range []struct {
		stream   string
		delivery streamqueue.Delivery
	}{
		{"", delivery},
		{"failures", streamqueue.Delivery{ID: "", Body: []byte("body"), Attempts: 1}},
		{"failures", streamqueue.Delivery{ID: "1-0", Body: []byte("body"), Attempts: 0}},
		{"failures", streamqueue.Delivery{ID: "1-0", Body: []byte("payload too large"), Attempts: 1}},
	} {
		assert.ErrorIs(t,
			transport.RecordFailure(
				context.Background(), invalid.stream, "jobs", "workers",
				invalid.delivery, testFailureMetadata(),
			),
			streamqueue.ErrInvalidSemanticRequest,
		)
	}
	_, err = transport.ReadRecords(context.Background(), "")
	assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)

	client.results = []stubResult{{streams: map[string][]valkey.XRangeEntry{"records": {{
		ID: "bad", FieldValues: map[string]string{},
	}}}}}
	_, err = transport.ReadRecords(context.Background(), "records")
	assert.Error(t, err)
}

func TestNativeTransportRecordPageFailures(t *testing.T) {
	backendErr := errors.New("backend operator-secret")
	transport, _ := faultTransport(t, stubResult{err: backendErr})

	_, err := transport.ReadRecordPage(
		context.Background(), "", "", 1, management.SortAscending,
	)
	assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
	_, err = transport.ReadRecordPage(
		context.Background(), "records", "", 1, management.SortAscending,
	)
	assert.ErrorIs(t, err, backendErr)
	assert.ErrorIs(t, err, management.ErrManagementUnavailable)
	assert.NotContains(t, err.Error(), "operator-secret")

	transport, _ = faultTransport(t, stubResult{err: backendErr})
	_, _, err = transport.ReadRecord(context.Background(), "records", "1-0")
	assert.ErrorIs(t, err, backendErr)
	assert.ErrorIs(t, err, management.ErrManagementUnavailable)
	assert.NotContains(t, err.Error(), "operator-secret")
	_, _, err = transport.ReadRecord(context.Background(), "", "1-0")
	assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)

	transport, _ = faultTransport(t, stubResult{streams: map[string][]valkey.XRangeEntry{}})
	_, found, err := transport.ReadRecord(context.Background(), "records", "1-0")
	require.NoError(t, err)
	assert.False(t, found)

	transport, _ = faultTransport(t, stubResult{streams: map[string][]valkey.XRangeEntry{
		"records": {{ID: "2-0"}},
	}})
	_, found, err = transport.ReadRecord(context.Background(), "records", "1-0")
	require.NoError(t, err)
	assert.False(t, found)

	transport, _ = faultTransport(t, stubResult{streams: map[string][]valkey.XRangeEntry{
		"records": {{ID: "1-0", FieldValues: map[string]string{}}},
	}})
	_, _, err = transport.ReadRecord(context.Background(), "records", "1-0")
	assert.Error(t, err)
}

func TestValkeyRecordReadErrorPreservesCallerCancellation(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := valkeyRecordReadError("read records", cause)
		assert.ErrorIs(t, err, cause)
		assert.NotErrorIs(t, err, management.ErrManagementUnavailable)
	}
}

func TestNativeTransportMutationFailures(t *testing.T) {
	backendErr := errors.New("backend")
	t.Run("retry", func(t *testing.T) {
		transport, _ := faultTransport(t, stubResult{err: backendErr})
		_, err := transport.RetryRecord(context.Background(), "", "1-0", "jobs", "source", "group", true)
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.RetryRecord(context.Background(), "records", "", "jobs", "source", "group", true)
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.RetryRecord(context.Background(), "records", "1-0", "", "source", "group", true)
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.RetryRecord(context.Background(), "records", "1-0", "jobs", "", "group", true)
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.RetryRecord(context.Background(), "records", "1-0", "jobs", "source", "", true)
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.RetryRecord(context.Background(), "records", "1-0", "jobs", "source", "group", true)
		assert.ErrorIs(t, err, backendErr)
		transport, _ = faultTransport(t, stubResult{text: "invalid"})
		_, err = transport.RetryRecord(context.Background(), "records", "1-0", "jobs", "", "", false)
		assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	})

	t.Run("replay", func(t *testing.T) {
		transport, _ := faultTransport(t, stubResult{err: backendErr})
		for _, request := range []struct {
			records, id, destination string
			policy                   management.ReplayPolicy
		}{
			{"", "1-0", "archive", management.ReplayRejectDuplicate},
			{"dead", "", "archive", management.ReplayRejectDuplicate},
			{"dead", "1-0", "", management.ReplayRejectDuplicate},
			{"dead", "1-0", "archive", management.ReplayPolicy("invalid")},
		} {
			_, err := transport.ReplayRecord(
				context.Background(), request.records, request.id,
				request.destination, request.policy,
			)
			assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		}
		_, err := transport.ReplayRecord(
			context.Background(), "dead", "1-0", "archive",
			management.ReplayRejectDuplicate,
		)
		assert.ErrorIs(t, err, backendErr)
		transport, _ = faultTransport(t, stubResult{text: "invalid"})
		_, err = transport.ReplayRecord(
			context.Background(), "dead", "1-0", "archive",
			management.ReplayRejectDuplicate,
		)
		assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	})

	t.Run("delete", func(t *testing.T) {
		transport, client := faultTransport(t, stubResult{err: backendErr})
		_, err := transport.DeleteRecord(context.Background(), "", "1-0")
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.DeleteRecord(context.Background(), "records", "")
		assert.ErrorIs(t, err, streamqueue.ErrInvalidSemanticRequest)
		_, err = transport.DeleteRecord(context.Background(), "records", "1-0")
		assert.ErrorIs(t, err, backendErr)
		client.results = []stubResult{{integer: 2}}
		_, err = transport.DeleteRecord(context.Background(), "records", "1-0")
		assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
	})

	t.Run("purge", func(t *testing.T) {
		transport, client := faultTransport(t, stubResult{err: backendErr})
		assert.ErrorIs(t, transport.PurgeRecords(context.Background(), ""), streamqueue.ErrInvalidSemanticRequest)
		assert.ErrorIs(t, transport.PurgeRecords(context.Background(), "records"), backendErr)
		client.results = []stubResult{{integer: 2}}
		assert.ErrorIs(t, transport.PurgeRecords(context.Background(), "records"), streamqueue.ErrMalformedDelivery)
	})
}

func TestConvertEntriesRejectsMissingBody(t *testing.T) {
	_, err := convertEntries([]valkey.XRangeEntry{{ID: "1-0"}}, 1, false)
	assert.ErrorIs(t, err, streamqueue.ErrMalformedDelivery)
}

type stubResult struct {
	err     error
	text    string
	integer int64
	value   any
	streams map[string][]valkey.XRangeEntry
}

func (r stubResult) Error() error { return r.err }
func (r stubResult) ToString() (string, error) {
	return r.text, r.err
}
func (r stubResult) ToInt64() (int64, error) {
	return r.integer, r.err
}
func (r stubResult) ToAny() (any, error) { return r.value, r.err }
func (r stubResult) AsXRead() (map[string][]valkey.XRangeEntry, error) {
	return r.streams, r.err
}
func (r stubResult) AsXRange() ([]valkey.XRangeEntry, error) {
	return r.streams["records"], r.err
}

type stubCommandClient struct {
	results []stubResult
	closed  bool
	builder valkey.Builder
}

func (c *stubCommandClient) B() valkey.Builder { return c.builder }
func (c *stubCommandClient) Do(context.Context, valkey.Completed) commandResult {
	result := c.results[0]
	c.results = c.results[1:]
	return result
}
func (c *stubCommandClient) Close() { c.closed = true }

func faultTransport(t *testing.T, results ...stubResult) (*nativeTransport, *stubCommandClient) {
	t.Helper()
	server := miniredis.RunT(t)
	builderClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true,
	})
	require.NoError(t, err)
	t.Cleanup(builderClient.Close)
	client := &stubCommandClient{results: results, builder: builderClient.B()}
	return newNativeTransportForClient(client, 10, 10), client
}
