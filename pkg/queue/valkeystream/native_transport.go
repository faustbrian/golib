package valkeystream

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
	valkey "github.com/valkey-io/valkey-go"
)

const (
	streamBodyField               = "body"
	originalIDField               = "original_id"
	deliveryAttemptsField         = "delivery_attempts"
	envelopeVersionField          = "envelope_version"
	classificationField           = "classification"
	failureCodeField              = "failure_code"
	sourceStreamField             = "source_stream"
	consumerGroupField            = "consumer_group"
	replayOriginalDeadLetterField = "replay_original_dead_letter_id"
	replayPriorDeadLetterField    = "replay_prior_dead_letter_id"
	replayGenerationField         = "replay_generation"
	minimumRingScale              = 8
	retryRecordScript             = `
local record = redis.call('XRANGE', KEYS[1], ARGV[1], ARGV[1])
if #record == 0 then return 'not_found' end
local body = nil
local original = nil
local lineageOriginal = nil
local lineagePrior = nil
local lineageGeneration = nil
for index = 1, #record[1][2], 2 do
  if record[1][2][index] == 'body' then body = record[1][2][index + 1] end
  if record[1][2][index] == 'original_id' then original = record[1][2][index + 1] end
  if record[1][2][index] == 'replay_original_dead_letter_id' then
    lineageOriginal = record[1][2][index + 1]
  end
  if record[1][2][index] == 'replay_prior_dead_letter_id' then
    lineagePrior = record[1][2][index + 1]
  end
  if record[1][2][index] == 'replay_generation' then
    lineageGeneration = record[1][2][index + 1]
  end
end
if not body or not original then return 'malformed' end
if ARGV[3] == 'failure' then
  local pending = redis.call('XPENDING', KEYS[3], ARGV[2], original, original, 1)
  if #pending == 0 then return 'stale' end
end
if ARGV[3] == 'dead_letter' then
  local hasAnyLineage = lineageOriginal or lineagePrior or lineageGeneration
  if hasAnyLineage and not (lineageOriginal and lineagePrior and lineageGeneration) then
    return 'malformed'
  end
  local nextOriginal = ARGV[1]
  local nextGeneration = 1
  if hasAnyLineage then
    nextOriginal = lineageOriginal
    nextGeneration = tonumber(lineageGeneration)
    if not nextGeneration or nextGeneration < 1 or nextGeneration >= 4294967295 then
      return 'malformed'
    end
    nextGeneration = nextGeneration + 1
  end
  if string.len(nextOriginal) > 256 then return 'malformed' end
  redis.call(
    'XADD', KEYS[2], 'MAXLEN', '~', ARGV[4], '*',
    'body', body,
    'replay_original_dead_letter_id', nextOriginal,
    'replay_prior_dead_letter_id', ARGV[1],
    'replay_generation', tostring(nextGeneration)
  )
else
  redis.call('XADD', KEYS[2], 'MAXLEN', '~', ARGV[4], '*', 'body', body)
end
if ARGV[3] == 'failure' then redis.call('XACK', KEYS[3], ARGV[2], original) end
redis.call('XDEL', KEYS[1], ARGV[1])
return 'ok'
`
	replayRecordScript = `
local record = redis.call('XRANGE', KEYS[1], ARGV[1], ARGV[1])
if #record == 0 then return 'not_found' end
local body = nil
local lineageOriginal = nil
local lineagePrior = nil
local lineageGeneration = nil
for index = 1, #record[1][2], 2 do
  if record[1][2][index] == 'body' then body = record[1][2][index + 1] end
  if record[1][2][index] == 'replay_original_dead_letter_id' then
    lineageOriginal = record[1][2][index + 1]
  end
  if record[1][2][index] == 'replay_prior_dead_letter_id' then
    lineagePrior = record[1][2][index + 1]
  end
  if record[1][2][index] == 'replay_generation' then
    lineageGeneration = record[1][2][index + 1]
  end
end
if not body then return 'malformed' end
local hasAnyLineage = lineageOriginal or lineagePrior or lineageGeneration
if hasAnyLineage and not (lineageOriginal and lineagePrior and lineageGeneration) then
  return 'malformed'
end
local nextOriginal = ARGV[1]
local nextGeneration = 1
if hasAnyLineage then
  nextOriginal = lineageOriginal
  nextGeneration = tonumber(lineageGeneration)
  if not nextGeneration or nextGeneration < 1 or nextGeneration >= 4294967295 then
    return 'malformed'
  end
  nextGeneration = nextGeneration + 1
end
if string.len(nextOriginal) > 256 then return 'malformed' end
local prior = redis.call('HGET', KEYS[3], ARGV[3])
if prior and ARGV[2] == 'reject_duplicate' then return 'duplicate' end
if prior then redis.call('XDEL', KEYS[2], prior) end
local replayed = redis.call(
  'XADD', KEYS[2], 'MAXLEN', '~', ARGV[4], '*',
  'body', body,
  'replay_original_dead_letter_id', nextOriginal,
  'replay_prior_dead_letter_id', ARGV[1],
  'replay_generation', tostring(nextGeneration)
)
redis.call('HSET', KEYS[3], ARGV[3], replayed)
local now = redis.call('TIME')
local score = tonumber(now[1]) + (tonumber(now[2]) / 1000000)
redis.call('ZADD', KEYS[4], score, ARGV[3])
local excess = redis.call('ZCARD', KEYS[4]) - tonumber(ARGV[4])
if excess > 0 then
  local expired = redis.call('ZRANGE', KEYS[4], 0, excess - 1)
  for _, replayKey in ipairs(expired) do
    redis.call('HDEL', KEYS[3], replayKey)
    redis.call('ZREM', KEYS[4], replayKey)
  end
end
return 'ok'
`
)

type nativeTransport struct {
	client          commandClient
	maxLength       int64
	recordMaxLength int64
	maxPayloadBytes int
}

type commandResult interface {
	Error() error
	ToString() (string, error)
	ToInt64() (int64, error)
	ToAny() (any, error)
	AsXRead() (map[string][]valkey.XRangeEntry, error)
	AsXRange() ([]valkey.XRangeEntry, error)
}

type commandClient interface {
	B() valkey.Builder
	Do(context.Context, valkey.Completed) commandResult
	Close()
}

type nativeCommandClient struct{ valkey.Client }

func (c nativeCommandClient) Do(ctx context.Context, command valkey.Completed) commandResult {
	return c.Client.Do(ctx, command)
}

func nativeClientOptions(opts options) valkey.ClientOption {
	var tlsConfig *tls.Config
	if opts.tlsConfig != nil {
		tlsConfig = opts.tlsConfig.Clone()
	}
	return valkey.ClientOption{
		InitAddress:         []string{opts.address},
		ForceSingleClient:   true,
		Username:            opts.username,
		Password:            opts.password,
		ClientName:          opts.clientName,
		ClientSetInfo:       valkey.DisableClientSetInfo,
		SelectDB:            opts.db,
		TLSConfig:           tlsConfig,
		Dialer:              net.Dialer{Timeout: opts.dialTimeout, KeepAlive: time.Second},
		ConnWriteTimeout:    opts.commandTimeout,
		BlockingPoolMinSize: opts.blockingPoolMinSize,
		BlockingPoolSize:    opts.blockingPoolSize,
		BlockingPoolCleanup: opts.blockingPoolCleanup,
		ReadBufferEachConn:  nativeConnectionBufferSize(),
		WriteBufferEachConn: nativeConnectionBufferSize(),
		RingScaleEachConn:   minimumRingScale,
		DisableCache:        true,
		DisableRetry:        true,
		AlwaysPipelining:    true,
	}
}

func nativeConnectionBufferSize() int {
	return 32 * 1024
}

func newNativeTransport(client valkey.Client, maxLength int64, maxPayloadBytes int) *nativeTransport {
	return newNativeTransportForClient(nativeCommandClient{Client: client}, maxLength, maxPayloadBytes)
}

func newNativeTransportForClient(
	client commandClient, maxLength int64, maxPayloadBytes int,
) *nativeTransport {
	return &nativeTransport{
		client: client, maxLength: maxLength, maxPayloadBytes: maxPayloadBytes,
	}
}

func (t *nativeTransport) EnsureGroup(ctx context.Context, stream, group string) error {
	if stream == "" || group == "" {
		return streamqueue.ErrInvalidSemanticRequest
	}
	cmd := t.client.B().XgroupCreate().Key(stream).Group(group).Id("0").Mkstream().Build()
	err := t.client.Do(ctx, cmd).Error()
	if err == nil || valkey.IsValkeyBusyGroup(err) {
		return nil
	}
	return safeerr.Wrap("valkeystream: create consumer group", err)
}

func (t *nativeTransport) Add(ctx context.Context, request streamqueue.AddRequest) (string, error) {
	if err := request.Validate(t.maxPayloadBytes); err != nil {
		return "", err
	}
	cmd := t.client.B().Xadd().Key(request.Stream).Maxlen().Almost().
		Threshold(strconv.FormatInt(request.MaxLength, 10)).Id("*").FieldValue().
		FieldValue(streamBodyField, string(request.Body)).Build()
	id, err := t.client.Do(ctx, cmd).ToString()
	if err != nil {
		return "", safeerr.Wrap("valkeystream: add delivery", err)
	}
	return id, nil
}

func (t *nativeTransport) Read(
	ctx context.Context, request streamqueue.ReadRequest,
) ([]streamqueue.Delivery, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	cmd := t.client.B().Xreadgroup().Group(request.Group, request.Consumer).
		Count(request.Count).Block(request.Block.Milliseconds()).Streams().
		Key(request.Stream).Id(">").Build()
	streams, err := t.client.Do(ctx, cmd).AsXRead()
	if valkey.IsValkeyNil(err) {
		return nil, nil
	}
	if err != nil {
		return nil, safeerr.Wrap("valkeystream: read deliveries", err)
	}
	return convertEntries(streams[request.Stream], 1, false)
}

func (t *nativeTransport) Claim(
	ctx context.Context, request streamqueue.ClaimRequest,
) (streamqueue.ClaimResult, error) {
	if err := request.Validate(); err != nil {
		return streamqueue.ClaimResult{}, err
	}
	cmd := t.client.B().Xautoclaim().Key(request.Stream).Group(request.Group).
		Consumer(request.Consumer).MinIdleTime(strconv.FormatInt(request.MinIdle.Milliseconds(), 10)).
		Start(request.Start).Count(request.Count).Build()
	value, err := t.client.Do(ctx, cmd).ToAny()
	if err != nil {
		return streamqueue.ClaimResult{}, safeerr.Wrap("valkeystream: reclaim deliveries", err)
	}
	result, err := parseClaimResponse(value)
	if err != nil {
		return streamqueue.ClaimResult{}, err
	}
	for index := range result.Deliveries {
		result.Deliveries[index].Attempts, err = t.deliveryAttempts(
			ctx, request.Stream, request.Group, result.Deliveries[index].ID,
		)
		if err != nil {
			return streamqueue.ClaimResult{}, err
		}
	}
	return result, nil
}

func (t *nativeTransport) deliveryAttempts(
	ctx context.Context, stream, group, id string,
) (int64, error) {
	cmd := t.client.B().Xpending().Key(stream).Group(group).Start(id).End(id).Count(1).Build()
	value, err := t.client.Do(ctx, cmd).ToAny()
	if err != nil {
		return 0, safeerr.Wrap("valkeystream: inspect delivery attempts", err)
	}
	return parsePendingAttempts(value)
}

func (t *nativeTransport) Ack(ctx context.Context, request streamqueue.AckRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	cmd := t.client.B().Xack().Key(request.Stream).Group(request.Group).Id(request.ID).Build()
	acknowledged, err := t.client.Do(ctx, cmd).ToInt64()
	if err != nil {
		return safeerr.Wrap("valkeystream: acknowledge delivery", err)
	}
	if acknowledged != 1 {
		return management.NewFailure(
			management.ClassificationInfrastructure,
			management.FailureCodeLeaseLost,
			errors.New("valkey stream delivery is no longer pending"),
		)
	}
	return nil
}

func (t *nativeTransport) DeadLetter(ctx context.Context, request streamqueue.DeadLetterRequest) error {
	if err := request.Validate(t.maxPayloadBytes); err != nil {
		return err
	}
	if err := t.AppendDeadLetter(
		ctx, request.Destination, request.Source, request.Group,
		request.Delivery, request.Failure,
	); err != nil {
		return err
	}
	if err := t.Ack(ctx, streamqueue.AckRequest{
		Stream: request.Source, Group: request.Group, ID: request.Delivery.ID,
	}); err != nil {
		return safeerr.Wrap("valkeystream: settle dead letter source", err)
	}
	return nil
}

func (t *nativeTransport) RecordFailure(
	ctx context.Context, destination, source, group string,
	delivery streamqueue.Delivery, failure streamqueue.FailureMetadata,
) error {
	return t.appendRecord(ctx, destination, source, group, delivery, failure, "append failure")
}

func (t *nativeTransport) AppendDeadLetter(
	ctx context.Context, destination, source, group string,
	delivery streamqueue.Delivery, failure streamqueue.FailureMetadata,
) error {
	err := t.appendRecord(
		ctx, destination, source, group, delivery, failure, "append dead letter",
	)
	if err != nil {
		return management.NewFailure(
			management.ClassificationInfrastructure,
			management.FailureCodeDeadLetterDestinationUnavailable,
			err,
		)
	}
	return nil
}

func (t *nativeTransport) appendRecord(
	ctx context.Context, destination, source, group string,
	delivery streamqueue.Delivery, failure streamqueue.FailureMetadata, operation string,
) error {
	if destination == "" || source == "" || group == "" || delivery.ID == "" ||
		delivery.Attempts < 1 || len(delivery.Body) > t.maxPayloadBytes ||
		failure.Validate() != nil {
		return streamqueue.ErrInvalidSemanticRequest
	}
	recordLimit := t.recordMaxLength
	if recordLimit == 0 {
		recordLimit = math.MaxInt64
	}
	cmd := t.client.B().Xadd().Key(destination).Maxlen().Exact().
		Threshold(strconv.FormatInt(recordLimit, 10)).Id("*").FieldValue().
		FieldValue(streamBodyField, string(delivery.Body)).
		FieldValue(originalIDField, delivery.ID).
		FieldValue(deliveryAttemptsField, strconv.FormatInt(delivery.Attempts, 10)).
		FieldValue(envelopeVersionField, strconv.FormatUint(uint64(management.CurrentEnvelopeVersion), 10)).
		FieldValue(classificationField, string(failure.Classification)).
		FieldValue(failureCodeField, failure.Code).
		FieldValue(sourceStreamField, source).
		FieldValue(consumerGroupField, group)
	if delivery.ReplayGeneration > 0 {
		cmd = cmd.FieldValue(
			replayOriginalDeadLetterField, delivery.OriginalDeadLetterID,
		).FieldValue(
			replayPriorDeadLetterField, delivery.PriorDeadLetterID,
		).FieldValue(
			replayGenerationField, strconv.FormatUint(uint64(delivery.ReplayGeneration), 10),
		)
	}
	if err := t.client.Do(ctx, cmd.Build()).Error(); err != nil {
		return safeerr.Wrap("valkeystream: "+operation, err)
	}
	return nil
}

func (t *nativeTransport) ReadRecords(ctx context.Context, stream string) ([]nativeRecord, error) {
	if stream == "" {
		return nil, streamqueue.ErrInvalidSemanticRequest
	}
	readLimit := max(t.maxLength, int64(management.MaxBulkSelection))
	entries, err := t.client.Do(ctx, t.client.B().Xrange().Key(stream).
		Start("-").End("+").Count(readLimit).Build()).AsXRange()
	if err != nil {
		return nil, valkeyRecordReadError("valkeystream: read management records", err)
	}
	return convertNativeRecords(entries)
}

func (t *nativeTransport) ReadRecordPage(
	ctx context.Context, stream, cursor string, limit int64,
	direction management.SortDirection,
) ([]nativeRecord, error) {
	if stream == "" || limit <= 0 {
		return nil, streamqueue.ErrInvalidSemanticRequest
	}
	var command valkey.Completed
	if direction == management.SortDescending {
		end := "+"
		if cursor != "" {
			end = "(" + cursor
		}
		command = t.client.B().Xrevrange().Key(stream).End(end).Start("-").
			Count(limit).Build()
	} else {
		start := "-"
		if cursor != "" {
			start = "(" + cursor
		}
		command = t.client.B().Xrange().Key(stream).Start(start).End("+").
			Count(limit).Build()
	}
	entries, err := t.client.Do(ctx, command).AsXRange()
	if err != nil {
		return nil, valkeyRecordReadError("valkeystream: read management record page", err)
	}
	return convertNativeRecords(entries)
}

func (t *nativeTransport) ReadRecord(
	ctx context.Context, stream, id string,
) (nativeRecord, bool, error) {
	if stream == "" || id == "" {
		return nativeRecord{}, false, streamqueue.ErrInvalidSemanticRequest
	}
	entries, err := t.client.Do(ctx, t.client.B().Xrange().Key(stream).
		Start(id).End(id).Count(1).Build()).AsXRange()
	if err != nil {
		return nativeRecord{}, false, valkeyRecordReadError(
			"valkeystream: read management record", err,
		)
	}
	if len(entries) != 1 || entries[0].ID != id {
		return nativeRecord{}, false, nil
	}
	records, err := convertNativeRecords(entries)
	if err != nil {
		return nativeRecord{}, false, err
	}
	return records[0], true, nil
}

func valkeyRecordReadError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return errors.Join(
		management.ErrManagementUnavailable,
		safeerr.Wrap(operation, err),
	)
}

func (t *nativeTransport) RetryRecord(
	ctx context.Context, recordStream, recordID, destination, source, group string,
	failure bool,
) (nativeRetryOutcome, error) {
	if recordStream == "" || recordID == "" || destination == "" ||
		(failure && (source == "" || group == "")) {
		return "", streamqueue.ErrInvalidSemanticRequest
	}
	kind := "dead_letter"
	sourceKey := source
	if failure {
		kind = "failure"
	} else {
		sourceKey = destination
	}
	command := t.client.B().Eval().Script(retryRecordScript).Numkeys(3).
		Key(recordStream, destination, sourceKey).
		Arg(recordID, group, kind, strconv.FormatInt(t.maxLength, 10)).Build()
	value, err := t.client.Do(ctx, command).ToString()
	if err != nil {
		return "", safeerr.Wrap("valkeystream: retry management record", err)
	}
	outcome := nativeRetryOutcome(value)
	if !outcome.valid() {
		return "", malformedResponse("invalid record retry result")
	}
	return outcome, nil
}

func (t *nativeTransport) ReplayRecord(
	ctx context.Context, recordStream, recordID, destination string,
	policy management.ReplayPolicy,
) (nativeReplayOutcome, error) {
	if recordStream == "" || recordID == "" || destination == "" ||
		(policy != management.ReplayRejectDuplicate && policy != management.ReplayReplaceDuplicate) {
		return "", streamqueue.ErrInvalidSemanticRequest
	}
	replayKey := base64.RawURLEncoding.EncodeToString(
		[]byte(recordStream + "\x00" + recordID),
	)
	registry := destination + ":queue:replay-index"
	order := destination + ":queue:replay-order"
	command := t.client.B().Eval().Script(replayRecordScript).Numkeys(4).
		Key(recordStream, destination, registry, order).
		Arg(recordID, string(policy), replayKey, strconv.FormatInt(t.maxLength, 10)).Build()
	value, err := t.client.Do(ctx, command).ToString()
	if err != nil {
		return "", safeerr.Wrap("valkeystream: replay management record", err)
	}
	outcome := nativeReplayOutcome(value)
	if !outcome.valid() {
		return "", malformedResponse("invalid record replay result")
	}
	return outcome, nil
}

func (t *nativeTransport) DeleteRecord(
	ctx context.Context, stream, id string,
) (bool, error) {
	if stream == "" || id == "" {
		return false, streamqueue.ErrInvalidSemanticRequest
	}
	deleted, err := t.client.Do(ctx, t.client.B().Xdel().Key(stream).Id(id).Build()).ToInt64()
	if err != nil {
		return false, safeerr.Wrap("valkeystream: delete management record", err)
	}
	if deleted < 0 || deleted > 1 {
		return false, malformedResponse("invalid record deletion result")
	}
	return deleted == 1, nil
}

func (t *nativeTransport) PurgeRecords(ctx context.Context, stream string) error {
	if stream == "" {
		return streamqueue.ErrInvalidSemanticRequest
	}
	deleted, err := t.client.Do(ctx, t.client.B().Del().Key(stream).Build()).ToInt64()
	if err != nil {
		return safeerr.Wrap("valkeystream: purge management records", err)
	}
	if deleted < 0 || deleted > 1 {
		return malformedResponse("invalid record purge result")
	}
	return nil
}

func (t *nativeTransport) GroupState(
	ctx context.Context, stream, group string,
) (streamqueue.GroupState, error) {
	if stream == "" || group == "" {
		return streamqueue.GroupState{}, streamqueue.ErrInvalidSemanticRequest
	}
	value, err := t.client.Do(ctx, t.client.B().XinfoGroups().Key(stream).Build()).ToAny()
	if err != nil {
		return streamqueue.GroupState{}, safeerr.Wrap("valkeystream: inspect consumer groups", err)
	}
	state, err := parseGroupState(value, group)
	if err != nil || state.Pending == 0 {
		return state, err
	}
	pending := t.client.B().Xpending().Key(stream).Group(group).
		Start("-").End("+").Count(1).Build()
	value, err = t.client.Do(ctx, pending).ToAny()
	if err != nil {
		return streamqueue.GroupState{}, safeerr.Wrap("valkeystream: inspect oldest pending delivery", err)
	}
	state.OldestPendingID, err = parseOldestPendingID(value)
	return state, err
}

func (t *nativeTransport) Close() error {
	t.client.Close()
	return nil
}

func convertEntries(
	entries []valkey.XRangeEntry, attempts int64, reclaimed bool,
) ([]streamqueue.Delivery, error) {
	deliveries := make([]streamqueue.Delivery, 0, len(entries))
	for _, entry := range entries {
		body, ok := entry.FieldValues[streamBodyField]
		if !ok {
			return nil, fmt.Errorf("%w: delivery body is missing", streamqueue.ErrMalformedDelivery)
		}
		delivery := streamqueue.Delivery{
			ID: entry.ID, Body: []byte(body), Attempts: attempts, Reclaimed: reclaimed,
		}
		if err := applyReplayLineage(&delivery, entry.FieldValues); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func parseClaimResponse(value any) (streamqueue.ClaimResult, error) {
	response, ok := value.([]any)
	if !ok || len(response) < 2 {
		return streamqueue.ClaimResult{}, malformedResponse("invalid reclaim response")
	}
	next, ok := response[0].(string)
	if !ok || next == "" {
		return streamqueue.ClaimResult{}, malformedResponse("invalid reclaim cursor")
	}
	entries, ok := response[1].([]any)
	if !ok {
		return streamqueue.ClaimResult{}, malformedResponse("invalid reclaim entries")
	}
	deliveries := make([]streamqueue.Delivery, 0, len(entries))
	for _, rawEntry := range entries {
		entry, ok := rawEntry.([]any)
		if !ok || len(entry) != 2 {
			return streamqueue.ClaimResult{}, malformedResponse("invalid reclaim entry")
		}
		id, ok := entry[0].(string)
		if !ok || id == "" {
			return streamqueue.ClaimResult{}, malformedResponse("invalid reclaim identifier")
		}
		fields, ok := alternatingFields(entry[1])
		if !ok {
			return streamqueue.ClaimResult{}, malformedResponse("invalid reclaim fields")
		}
		body, ok := fields[streamBodyField]
		if !ok {
			return streamqueue.ClaimResult{}, malformedResponse("delivery body is missing")
		}
		delivery := streamqueue.Delivery{
			ID: id, Body: []byte(body), Attempts: 1, Reclaimed: true,
		}
		if err := applyReplayLineage(&delivery, fields); err != nil {
			return streamqueue.ClaimResult{}, err
		}
		deliveries = append(deliveries, delivery)
	}
	return streamqueue.ClaimResult{Next: next, Deliveries: deliveries}, nil
}

func applyReplayLineage(delivery *streamqueue.Delivery, fields map[string]string) error {
	original, prior, generation, err := parseReplayLineage(fields)
	if err != nil {
		return malformedResponse("invalid replay lineage")
	}
	delivery.OriginalDeadLetterID = original
	delivery.PriorDeadLetterID = prior
	delivery.ReplayGeneration = generation
	return nil
}

func parseReplayLineage(fields map[string]string) (string, string, uint32, error) {
	original, originalOK := fields[replayOriginalDeadLetterField]
	prior, priorOK := fields[replayPriorDeadLetterField]
	generationText, generationOK := fields[replayGenerationField]
	if !originalOK && !priorOK && !generationOK {
		return "", "", 0, nil
	}
	if !originalOK || !priorOK || !generationOK || strings.TrimSpace(original) == "" ||
		strings.TrimSpace(prior) == "" || len(original) > management.MaxIdentityBytes ||
		len(prior) > management.MaxIdentityBytes {
		return "", "", 0, errors.New("incomplete replay lineage")
	}
	generation, err := strconv.ParseUint(generationText, 10, 32)
	if err != nil || generation == 0 {
		return "", "", 0, errors.New("invalid replay generation")
	}
	return original, prior, uint32(generation), nil
}

func parsePendingAttempts(value any) (int64, error) {
	response, ok := value.([]any)
	if !ok || len(response) != 1 {
		return 0, malformedResponse("missing pending delivery")
	}
	pending, ok := response[0].([]any)
	if !ok || len(pending) != 4 {
		return 0, malformedResponse("invalid pending delivery")
	}
	attempts, ok := pending[3].(int64)
	if !ok || attempts < 1 {
		return 0, malformedResponse("invalid delivery attempts")
	}
	return attempts, nil
}

func parseOldestPendingID(value any) (string, error) {
	response, ok := value.([]any)
	if !ok || len(response) != 1 {
		return "", malformedResponse("missing oldest pending delivery")
	}
	pending, ok := response[0].([]any)
	if !ok || len(pending) != 4 {
		return "", malformedResponse("invalid oldest pending delivery")
	}
	id, ok := pending[0].(string)
	if !ok || id == "" {
		return "", malformedResponse("invalid oldest pending identifier")
	}
	return id, nil
}

func parseGroupState(value any, group string) (streamqueue.GroupState, error) {
	groups, ok := value.([]any)
	if !ok {
		return streamqueue.GroupState{}, malformedResponse("invalid group response")
	}
	for _, rawGroup := range groups {
		fields, ok := rawGroup.(map[string]any)
		if !ok {
			return streamqueue.GroupState{}, malformedResponse("invalid group fields")
		}
		name, ok := fields["name"].(string)
		if !ok {
			return streamqueue.GroupState{}, malformedResponse("invalid group name")
		}
		if name != group {
			continue
		}
		pending, pendingOK := fields["pending"].(int64)
		lag, lagOK := fields["lag"].(int64)
		if !pendingOK || !lagOK || pending < 0 || lag < -1 {
			return streamqueue.GroupState{}, malformedResponse("invalid group counters")
		}
		return streamqueue.GroupState{Pending: pending, Lag: lag}, nil
	}
	return streamqueue.GroupState{}, errors.New("valkeystream: consumer group does not exist")
}

func alternatingFields(value any) (map[string]string, bool) {
	values, ok := value.([]any)
	if !ok || len(values)%2 != 0 {
		return nil, false
	}
	fields := make(map[string]string, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		field, fieldOK := values[index].(string)
		value, valueOK := values[index+1].(string)
		if !fieldOK || !valueOK || field == "" {
			return nil, false
		}
		fields[field] = value
	}
	return fields, true
}

func malformedResponse(detail string) error {
	return fmt.Errorf("%w: %s", streamqueue.ErrMalformedDelivery, detail)
}
