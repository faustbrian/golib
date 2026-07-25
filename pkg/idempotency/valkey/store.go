package valkey

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

const (
	// MaxPrefixBytes bounds the application prefix used in physical keys.
	MaxPrefixBytes = 64
	// MaxRetention is the longest terminal record TTL accepted by the adapter.
	MaxRetention = 365 * 24 * time.Hour
)

type operation string

const (
	operationAcquire   operation = "acquire"
	operationInspect   operation = "inspect"
	operationHeartbeat operation = "heartbeat"
	operationComplete  operation = "complete"
	operationFail      operation = "fail"
	operationRelease   operation = "release"
	operationExpire    operation = "expire"
)

type scriptExecutor interface {
	Exec(context.Context, operation, string, []string) ([]string, error)
	Check(context.Context) error
}

// Check verifies the server version and correctness-critical eviction policy.
func (s *Store) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.executor.Check(ctx)
}

// Options configures physical key isolation, TTL, and token generation.
type Options struct {
	// Prefix isolates physical keys and must not contain braces.
	Prefix string
	// Retention keeps terminal records available before TTL deletion.
	Retention time.Duration
	// OwnerTokens returns a unique token for each ownership attempt.
	OwnerTokens func() (string, error)
}

// Store persists records through atomic single-key Valkey scripts.
type Store struct {
	executor    scriptExecutor
	prefix      string
	retentionMS string
	ownerTokens func() (string, error)
}

func newStore(executor scriptExecutor, options Options) (*Store, error) {
	if executor == nil {
		return nil, configurationError("executor")
	}
	if options.Prefix == "" || len(options.Prefix) > MaxPrefixBytes ||
		strings.ContainsAny(options.Prefix, "{}") {
		return nil, configurationError("prefix")
	}
	if options.Retention <= 0 || options.Retention > MaxRetention {
		return nil, configurationError("retention")
	}
	if options.OwnerTokens == nil {
		return nil, configurationError("owner_tokens")
	}
	return &Store{
		executor:    executor,
		prefix:      options.Prefix,
		retentionMS: milliseconds(options.Retention),
		ownerTokens: options.OwnerTokens,
	}, nil
}

// Acquire atomically elects an owner or returns the retained semantic outcome.
func (s *Store) Acquire(ctx context.Context, request idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.AcquireResult{}, err
	}
	if err := validateLease(request.Lease); err != nil {
		return idempotency.AcquireResult{}, err
	}
	ownerToken, err := s.ownerTokens()
	if err != nil || ownerToken == "" || len(ownerToken) > idempotency.MaxOwnerTokenBytes {
		return idempotency.AcquireResult{}, &idempotency.Error{
			Reason: idempotency.ReasonUnavailable,
			Field:  "owner_token",
			Cause:  err,
		}
	}
	reply, err := s.executor.Exec(ctx, operationAcquire, recordKey(s.prefix, request.Key), []string{
		request.Key.Namespace(),
		request.Key.Tenant(),
		request.Key.Operation(),
		request.Key.Caller(),
		request.Key.Value(),
		ownerToken,
		milliseconds(request.Lease),
		s.retentionMS,
		request.Fingerprint.Version(),
		hex.EncodeToString(request.Fingerprint.Sum()),
	})
	if err != nil {
		return idempotency.AcquireResult{}, err
	}
	if semantic := decodeSemanticReply(reply); semantic != nil {
		return idempotency.AcquireResult{}, semantic
	}
	return decodeAcquireReply(reply)
}

// Inspect returns the authoritative retained record for key.
func (s *Store) Inspect(ctx context.Context, key idempotency.Key) (idempotency.Record, error) {
	return s.executeRecord(ctx, operationInspect, key, nil)
}

// Heartbeat extends a live owner's lease using Valkey server time.
func (s *Store) Heartbeat(ctx context.Context, request idempotency.HeartbeatRequest) (idempotency.Record, error) {
	if err := validateLease(request.Lease); err != nil {
		return idempotency.Record{}, err
	}
	return s.executeRecord(ctx, operationHeartbeat, request.Ownership.Key, []string{
		request.Ownership.OwnerToken,
		strconv.FormatUint(request.Ownership.FencingToken, 10),
		milliseconds(request.Lease),
		s.retentionMS,
	})
}

// Complete records a bounded successful result for a live current owner.
func (s *Store) Complete(ctx context.Context, request idempotency.CompleteRequest) (idempotency.Record, error) {
	metadata, err := encodeMetadata(request.Result, request.Metadata)
	if err != nil {
		return idempotency.Record{}, err
	}
	return s.executeRecord(ctx, operationComplete, request.Ownership.Key, []string{
		request.Ownership.OwnerToken,
		strconv.FormatUint(request.Ownership.FencingToken, 10),
		string(request.Result),
		metadata,
		s.retentionMS,
	})
}

// Fail records a bounded terminal failure for a live current owner.
func (s *Store) Fail(ctx context.Context, request idempotency.FailRequest) (idempotency.Record, error) {
	metadata, err := encodeMetadata(request.Result, request.Metadata)
	if err != nil {
		return idempotency.Record{}, err
	}
	return s.executeRecord(ctx, operationFail, request.Ownership.Key, []string{
		request.Ownership.OwnerToken,
		strconv.FormatUint(request.Ownership.FencingToken, 10),
		string(request.Result),
		metadata,
		s.retentionMS,
	})
}

// Release abandons a live current owner's attempt.
func (s *Store) Release(ctx context.Context, ownership idempotency.Ownership) (idempotency.Record, error) {
	return s.executeRecord(ctx, operationRelease, ownership.Key, []string{
		ownership.OwnerToken,
		strconv.FormatUint(ownership.FencingToken, 10),
		s.retentionMS,
	})
}

// Expire records that an active lease elapsed according to Valkey server time.
func (s *Store) Expire(ctx context.Context, key idempotency.Key) (idempotency.Record, error) {
	return s.executeRecord(ctx, operationExpire, key, []string{s.retentionMS})
}

func (s *Store) executeRecord(ctx context.Context, operation operation, key idempotency.Key, args []string) (idempotency.Record, error) {
	if err := ctx.Err(); err != nil {
		return idempotency.Record{}, err
	}
	reply, err := s.executor.Exec(ctx, operation, recordKey(s.prefix, key), args)
	if err != nil {
		return idempotency.Record{}, err
	}
	if semantic := decodeSemanticReply(reply); semantic != nil {
		return idempotency.Record{}, semantic
	}
	return decodeRecordReply(reply)
}

func decodeRecordReply(reply []string) (idempotency.Record, error) {
	if len(reply) < 3 || reply[0] != "ok" || (len(reply)-1)%2 != 0 {
		return idempotency.Record{}, recordError(errors.New("malformed record reply"))
	}
	fields := make(map[string]string, (len(reply)-1)/2)
	for index := 1; index < len(reply); index += 2 {
		fields[reply[index]] = reply[index+1]
	}
	return decodeRecord(fields)
}

func decodeSemanticReply(reply []string) error {
	if len(reply) != 2 || reply[0] != "error" {
		return nil
	}
	reason := idempotency.Reason(reply[1])
	switch reason {
	case idempotency.ReasonNotFound, idempotency.ReasonStaleOwner,
		idempotency.ReasonLeaseExpired, idempotency.ReasonInvalidTransition:
		return &idempotency.Error{Reason: reason, Field: "record"}
	default:
		return recordError(errors.New("unknown semantic reason"))
	}
}

func encodeMetadata(result []byte, metadata map[string]string) (string, error) {
	if len(result) > idempotency.MaxResultBytes {
		return "", limitError(fieldResult)
	}
	if err := validateMetadata(metadata); err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(metadata)
	return string(encoded), nil
}

func validateLease(lease time.Duration) error {
	if lease <= 0 {
		return &idempotency.Error{Reason: idempotency.ReasonInvalidLease, Field: "lease"}
	}
	if lease > idempotency.MaxLease {
		return &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "lease"}
	}
	return nil
}

func milliseconds(duration time.Duration) string {
	return strconv.FormatInt(duration.Milliseconds(), 10)
}

func configurationError(field string) error {
	return &idempotency.Error{
		Reason: idempotency.ReasonInvalidConfiguration,
		Field:  field,
	}
}
