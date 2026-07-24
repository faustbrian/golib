package management

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestClassifyFailureUsesStablePrecedenceAndPreservesCauses(t *testing.T) {
	t.Parallel()

	handlerCause := errors.New("handler rejected job")
	permanent := NewFailure(ClassificationPermanent, "invalid_order", handlerCause)
	if !errors.Is(permanent, handlerCause) {
		t.Fatal("NewFailure() must preserve errors.Is for the handler cause")
	}
	var typed *Failure
	if !errors.As(permanent, &typed) {
		t.Fatal("NewFailure() must preserve errors.As for Failure")
	}
	if typed.Code != "invalid_order" {
		t.Fatalf("Failure.Code = %q, want invalid_order", typed.Code)
	}
	if got := permanent.Error(); got != "permanent failure: invalid_order" {
		t.Fatalf("Failure.Error() = %q", got)
	}
	if strings.Contains(permanent.Error(), handlerCause.Error()) {
		t.Fatal("Failure.Error() disclosed the arbitrary cause")
	}
	withoutCause := NewFailure(ClassificationRetryable, "temporary", nil)
	if got := withoutCause.Error(); got != "retryable failure: temporary" {
		t.Fatalf("Failure.Error() without cause = %q", got)
	}

	tests := map[string]struct {
		err  error
		want Classification
	}{
		"plain errors are retryable": {
			err:  handlerCause,
			want: ClassificationRetryable,
		},
		"deadline is canceled": {
			err:  context.DeadlineExceeded,
			want: ClassificationCanceled,
		},
		"wrapped cancellation is canceled": {
			err:  fmt.Errorf("wrapped: %w", context.Canceled),
			want: ClassificationCanceled,
		},
		"nil is retryable": {
			err:  nil,
			want: ClassificationRetryable,
		},
		"wrapped classification is retained": {
			err:  permanent,
			want: ClassificationPermanent,
		},
		"malformed dominates permanent in a join": {
			err: errors.Join(
				permanent,
				NewFailure(ClassificationMalformed, "unsupported_payload", errors.New("decode")),
			),
			want: ClassificationMalformed,
		},
		"cancellation dominates terminal handler failures": {
			err: errors.Join(
				context.Canceled,
				NewFailure(ClassificationMalformed, "invalid_payload", errors.New("decode")),
			),
			want: ClassificationCanceled,
		},
		"infrastructure dominates every joined failure": {
			err: errors.Join(
				context.Canceled,
				NewFailure(ClassificationInfrastructure, "ack_failed", errors.New("disconnect")),
			),
			want: ClassificationInfrastructure,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := ClassifyFailure(tt.err); got != tt.want {
				t.Fatalf("ClassifyFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOperationalFailureCodesAreStableAndSafe(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		code           string
		classification Classification
	}{
		"unsupported payload version": {
			FailureCodeUnsupportedPayloadVersion, ClassificationMalformed,
		},
		"lease lost": {FailureCodeLeaseLost, ClassificationInfrastructure},
		"dead letter destination": {
			FailureCodeDeadLetterDestinationUnavailable, ClassificationInfrastructure,
		},
		"administrative quarantine": {
			FailureCodeAdministrativeQuarantine, ClassificationPermanent,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			failure := NewFailure(test.classification, test.code, errors.New("private"))
			if err := failure.Validate(); err != nil {
				t.Fatalf("stable failure code is invalid: %v", err)
			}
		})
	}
}

func TestResolveFailureUsesWinningClassificationAndDeterministicCode(t *testing.T) {
	t.Parallel()

	permanent := NewFailure(ClassificationPermanent, "invalid_order", errors.New("payload"))
	infrastructure := NewFailure(
		ClassificationInfrastructure,
		"settlement_failed",
		errors.New("broker endpoint"),
	)
	for _, joined := range []error{
		errors.Join(permanent, infrastructure),
		errors.Join(infrastructure, permanent),
	} {
		resolution := ResolveFailure(joined)
		if resolution.Classification != ClassificationInfrastructure {
			t.Fatalf("ResolveFailure() classification = %q", resolution.Classification)
		}
		if resolution.Code != "settlement_failed" {
			t.Fatalf("ResolveFailure() code = %q", resolution.Code)
		}
	}

	resolution := ResolveFailure(errors.Join(
		NewFailure(ClassificationPermanent, "zeta", nil),
		NewFailure(ClassificationPermanent, "alpha", nil),
	))
	if resolution.Code != "alpha" {
		t.Fatalf("ResolveFailure() equal-rank code = %q", resolution.Code)
	}
}

func TestClassificationRankRejectsUnknownClassification(t *testing.T) {
	t.Parallel()
	if got := classificationRank(Classification("unknown")); got != 0 {
		t.Fatalf("classificationRank(unknown) = %d, want 0", got)
	}
}

func TestFailureValidateRejectsUnsafeClassificationMetadata(t *testing.T) {
	t.Parallel()

	valid := NewFailure(ClassificationPermanent, "invalid_order", errors.New("cause"))
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}

	tests := map[string]struct {
		failure *Failure
		field   string
	}{
		"unknown classification": {
			failure: NewFailure(Classification("fatal"), "invalid_order", errors.New("cause")),
			field:   "classification",
		},
		"empty code": {
			failure: NewFailure(ClassificationPermanent, "", errors.New("cause")),
			field:   "code",
		},
		"oversized code": {
			failure: NewFailure(
				ClassificationPermanent,
				stringOfLength(MaxIdentityBytes+1),
				errors.New("cause"),
			),
			field: "code",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertValidationField(t, tt.failure.Validate(), tt.field)
			if got := ClassifyFailure(tt.failure); got != ClassificationRetryable {
				t.Fatalf("ClassifyFailure(invalid failure) = %q, want %q", got, ClassificationRetryable)
			}
		})
	}
}
