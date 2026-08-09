package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	// ErrOverloaded identifies retryable 429 or 503 admission failure.
	ErrOverloaded = errors.New("search/opensearch: cluster is overloaded")
	// ErrClusterBlocked identifies a cluster-level write or metadata block.
	ErrClusterBlocked = errors.New("search/opensearch: cluster is blocked")
	// ErrRejected identifies a deterministic request rejection.
	ErrRejected        = errors.New("search/opensearch: request was rejected")
	ErrVersionConflict = errors.New("search/opensearch: external version conflict")
	ErrMappingRejected = errors.New("search/opensearch: document mapping was rejected")
	ErrPITExpired      = errors.New("search/opensearch: point in time expired")
	ErrBackpressure    = errors.New("search/opensearch: local capacity exhausted")
	ErrCircuitOpen     = errors.New("search/opensearch: circuit is open")
)

// Operation identifies the stable adapter operation in diagnostics.
type Operation string

const (
	// OperationInfo reads cluster identity and supported version.
	OperationInfo         Operation = "info"
	OperationSearch       Operation = "search"
	OperationCreatePIT    Operation = "create_pit"
	OperationDeletePIT    Operation = "delete_pit"
	OperationBulk         Operation = "bulk"
	OperationWrite        Operation = "write"
	OperationLifecycle    Operation = "lifecycle"
	OperationCreateIndex  Operation = "create_index"
	OperationReindex      Operation = "reindex"
	OperationVerifyIndex  Operation = "verify_index"
	OperationResolveAlias Operation = "resolve_alias"
	OperationSwapAlias    Operation = "swap_alias"
	OperationDeleteIndex  Operation = "delete_index"
	OperationHealth       Operation = "health"
	OperationCapacity     Operation = "capacity"
	OperationTemplate     Operation = "template"
)

// FailureCategory is a stable operational classification independent of
// OpenSearch's human-readable reason strings.
type FailureCategory string

const (
	FailureCancelled       FailureCategory = "cancelled"
	FailureTransport       FailureCategory = "transport"
	FailureOverloaded      FailureCategory = "overloaded"
	FailureClusterBlocked  FailureCategory = "cluster_blocked"
	FailureRejected        FailureCategory = "rejected"
	FailureMalformed       FailureCategory = "malformed_response"
	FailureVersionConflict FailureCategory = "version_conflict"
	FailureMappingRejected FailureCategory = "mapping_rejected"
	FailurePITExpired      FailureCategory = "pit_expired"
	FailureBackpressure    FailureCategory = "backpressure"
	FailureCircuitOpen     FailureCategory = "circuit_open"
)

// Failure is a secret-safe actionable adapter error. Code is retained only
// when OpenSearch returns a bounded identifier; reason text is never retained.
type Failure struct {
	Operation    Operation
	Category     FailureCategory
	Status       int
	Code         string
	Retryable    bool
	OutcomeKnown bool
	cause        error
}

func unknownTransportFailure(operation Operation, cause error) *Failure {
	failure := transportFailure(operation, cause)
	failure.OutcomeKnown = false
	return failure
}

func (failure *Failure) Error() string {
	message := fmt.Sprintf("search/opensearch: %s failed (%s", failure.Operation, failure.Category)
	if failure.Status != 0 {
		message += fmt.Sprintf(", HTTP %d", failure.Status)
	}
	if failure.Code != "" {
		message += ", code " + failure.Code
	}

	return message + ")"
}

func (failure *Failure) Unwrap() error { return failure.cause }

func cancelledFailure(operation Operation, cause error) *Failure {
	return &Failure{
		Operation: operation, Category: FailureCancelled,
		OutcomeKnown: true, cause: cause,
	}
}

func transportFailure(operation Operation, cause error) *Failure {
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cancelledFailure(operation, cause)
	}
	if errors.Is(cause, ErrBackpressure) {
		return &Failure{Operation: operation, Category: FailureBackpressure, Retryable: true, OutcomeKnown: false, cause: ErrBackpressure}
	}
	if errors.Is(cause, ErrCircuitOpen) {
		return &Failure{Operation: operation, Category: FailureCircuitOpen, Retryable: true, OutcomeKnown: false, cause: ErrCircuitOpen}
	}

	return &Failure{
		Operation: operation, Category: FailureTransport,
		Retryable: true, OutcomeKnown: true, cause: ErrTransport,
	}
}

func responseFailure(operation Operation, status int, body []byte) *Failure {
	code := responseErrorCode(body)
	category, retryable, sentinel := FailureRejected, false, ErrRejected
	switch {
	case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
		category, retryable, sentinel = FailureOverloaded, true, ErrOverloaded
	case code == "cluster_block_exception":
		category, sentinel = FailureClusterBlocked, ErrClusterBlocked
	case status == http.StatusConflict || code == "version_conflict_engine_exception":
		category, sentinel = FailureVersionConflict, ErrVersionConflict
	case code == "mapper_parsing_exception" || code == "strict_dynamic_mapping_exception":
		category, sentinel = FailureMappingRejected, ErrMappingRejected
	case operation == OperationSearch && status == http.StatusNotFound &&
		(code == "resource_not_found_exception" || responseHasErrorCode(body, "search_context_missing_exception")):
		category, sentinel = FailurePITExpired, ErrPITExpired
	}

	return &Failure{
		Operation: operation, Category: category, Status: status, Code: code,
		Retryable: retryable, OutcomeKnown: true,
		cause: errors.Join(ErrUnexpectedStatus, sentinel),
	}
}

func responseHasErrorCode(body []byte, expected string) bool {
	var payload struct {
		Error struct {
			Type      string `json:"type"`
			RootCause []struct {
				Type string `json:"type"`
			} `json:"root_cause"`
			FailedShards []struct {
				Reason struct {
					Type string `json:"type"`
				} `json:"reason"`
			} `json:"failed_shards"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil || expected == "" {
		return false
	}
	if payload.Error.Type == expected {
		return true
	}
	for _, cause := range payload.Error.RootCause {
		if cause.Type == expected {
			return true
		}
	}
	for _, shard := range payload.Error.FailedShards {
		if shard.Reason.Type == expected {
			return true
		}
	}
	return false
}

func malformedFailure(operation Operation, cause error) *Failure {
	return &Failure{
		Operation: operation, Category: FailureMalformed,
		OutcomeKnown: true, cause: cause,
	}
}

func responseErrorCode(body []byte) string {
	var payload struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "unknown"
	}
	if len(payload.Error) == 0 {
		return "unknown"
	}
	var object struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload.Error, &object) != nil || !safeErrorCode(object.Type) {
		return "unknown"
	}

	return object.Type
}

func safeErrorCode(code string) bool {
	if code == "" || len(code) > 128 {
		return false
	}
	for _, character := range code {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '_' {
			return false
		}
	}

	return !strings.Contains(code, "__")
}
