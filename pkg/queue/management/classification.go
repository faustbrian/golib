package management

import (
	"context"
	"errors"
	"fmt"
)

// Classification is the stable backend-neutral failure disposition used by
// workers, dead-letter records, and management clients.
type Classification string

const (
	// ClassificationRetryable permits another delivery attempt.
	ClassificationRetryable Classification = "retryable"
	// ClassificationPermanent prevents policy-driven handler retries.
	ClassificationPermanent Classification = "permanent"
	// ClassificationMalformed identifies an undecodable or unsupported input.
	ClassificationMalformed Classification = "malformed"
	// ClassificationCanceled identifies interrupted work that is not terminal
	// unless an explicit backend policy says otherwise.
	ClassificationCanceled Classification = "canceled"
	// ClassificationInfrastructure identifies broker or settlement uncertainty.
	ClassificationInfrastructure Classification = "infrastructure"
)

const (
	// FailureCodeUnsupportedPayloadVersion identifies a syntactically valid
	// payload whose declared schema version cannot be processed.
	FailureCodeUnsupportedPayloadVersion = "unsupported_payload_version"
	// FailureCodeLeaseLost identifies a delivery that can no longer be settled
	// by the current owner.
	FailureCodeLeaseLost = "lease_lost"
	// FailureCodeDeadLetterDestinationUnavailable identifies a failed durable
	// append to the configured terminal destination.
	FailureCodeDeadLetterDestinationUnavailable = "dead_letter_destination_unavailable"
	// FailureCodeAdministrativeQuarantine identifies an explicit operator
	// decision to prevent normal redelivery.
	FailureCodeAdministrativeQuarantine = "administrative_quarantine"
)

// Failure attaches a stable classification and safe code while preserving the
// original cause for errors.Is and errors.As.
type Failure struct {
	Classification Classification
	Code           string
	cause          error
}

// NewFailure classifies cause with a stable, non-sensitive failure code.
func NewFailure(classification Classification, code string, cause error) *Failure {
	return &Failure{Classification: classification, Code: code, cause: cause}
}

// Validate rejects unknown dispositions and unsafe failure codes.
func (e *Failure) Validate() error {
	if e == nil || !e.Classification.valid() {
		return invalid("classification", "is unsupported")
	}
	if invalidIdentity(e.Code) {
		return invalid("code", "is required and must be bounded")
	}

	return nil
}

// Error implements error.
func (e *Failure) Error() string {
	return fmt.Sprintf("%s failure: %s", e.Classification, e.Code)
}

// Unwrap preserves the classified cause for errors.Is and errors.As.
func (e *Failure) Unwrap() error {
	return e.cause
}

// FailureResolution is the stable classification and safe code selected from
// a wrapped or joined failure graph. Code is empty when no classified failure
// supplied one.
type FailureResolution struct {
	Classification Classification
	Code           string
}

// ResolveFailure resolves wrapped and joined failures with deterministic
// safety precedence: infrastructure, canceled, malformed, permanent, then
// retryable. Codes come only from the winning classification; equal-rank codes
// use lexical order so errors.Join argument order cannot change persistence.
func ResolveFailure(err error) FailureResolution {
	resolution := FailureResolution{Classification: ClassificationRetryable}
	stack := []error{err}

	for visited := 0; len(stack) > 0 && visited < 64; visited++ {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == nil {
			continue
		}

		candidate := FailureResolution{Classification: ClassificationRetryable}
		// The bounded graph walk must distinguish one-cause and joined unwraps
		// so every branch participates in the documented precedence.
		switch value := current.(type) { //nolint:errorlint
		case *Failure:
			if value.Validate() == nil {
				candidate.Classification = value.Classification
				candidate.Code = value.Code
			}
		case interface{ Unwrap() []error }:
			stack = append(stack, value.Unwrap()...)
			continue
		case interface{ Unwrap() error }:
			stack = append(stack, value.Unwrap())
			continue
		default:
			switch {
			case errors.Is(current, context.DeadlineExceeded):
				candidate.Classification = ClassificationCanceled
				candidate.Code = "deadline_exceeded"
			case errors.Is(current, context.Canceled):
				candidate.Classification = ClassificationCanceled
				candidate.Code = "context_canceled"
			}
		}

		candidateRank := classificationRank(candidate.Classification)
		resolutionRank := classificationRank(resolution.Classification)
		if candidateRank > resolutionRank ||
			(candidateRank == resolutionRank && candidate.Code != "" &&
				(resolution.Code == "" || candidate.Code < resolution.Code)) {
			resolution = candidate
		}
		if wrapped, ok := current.(interface{ Unwrap() error }); ok {
			stack = append(stack, wrapped.Unwrap())
		}
	}

	return resolution
}

// ClassifyFailure returns the classification selected by ResolveFailure.
func ClassifyFailure(err error) Classification {
	return ResolveFailure(err).Classification
}

func (classification Classification) valid() bool {
	switch classification {
	case ClassificationRetryable,
		ClassificationPermanent,
		ClassificationMalformed,
		ClassificationCanceled,
		ClassificationInfrastructure:
		return true
	default:
		return false
	}
}

func classificationRank(classification Classification) int {
	switch classification {
	case ClassificationInfrastructure:
		return 5
	case ClassificationCanceled:
		return 4
	case ClassificationMalformed:
		return 3
	case ClassificationPermanent:
		return 2
	case ClassificationRetryable:
		return 1
	default:
		return 0
	}
}
