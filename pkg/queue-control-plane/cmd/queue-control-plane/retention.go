package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
)

const (
	minimumRetention               = time.Hour
	maximumRetention               = 10 * 365 * 24 * time.Hour
	maximumRetentionPlans          = 1_000
	maximumRetentionBatches uint32 = 100
)

var (
	ErrInvalidRetentionDocument = errors.New("queue-control-plane: invalid retention document")
	ErrInvalidRetentionPlan     = errors.New("queue-control-plane: invalid retention plan")
	ErrRetentionIncomplete      = errors.New("queue-control-plane: retention incomplete")
)

type retentionPolicy struct {
	TenantID   string
	RetainFor  time.Duration
	BatchSize  uint32
	MaxBatches uint32
	LegalHold  bool
}

type retentionPolicyDocument struct {
	TenantID         string `json:"id"`
	RetentionSeconds int64  `json:"retention_seconds"`
	BatchSize        uint32 `json:"batch_size"`
	MaxBatches       uint32 `json:"max_batches"`
	LegalHold        bool   `json:"legal_hold"`
}

type retentionDocument struct {
	Tenants []retentionPolicyDocument `json:"tenants"`
}

type retentionAudit interface {
	VerifyTenant(context.Context, string, uint32) (controlpostgres.VerificationReport, error)
	RetainBefore(context.Context, string, time.Time, uint32) (controlpostgres.RetentionResult, error)
	RetainCommandsBefore(
		context.Context,
		string,
		time.Time,
		uint32,
	) (controlpostgres.CommandRetentionResult, error)
}

func loadRetentionPolicies(reader io.Reader, maxBytes int64) ([]retentionPolicy, error) {
	if missingDependency(reader) || maxBytes < 1 {
		return nil, ErrInvalidRetentionDocument
	}
	encoded, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || int64(len(encoded)) > maxBytes {
		return nil, ErrInvalidRetentionDocument
	}
	var document retentionDocument
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrInvalidRetentionDocument
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidRetentionDocument
	}
	if len(document.Tenants) == 0 || len(document.Tenants) > maximumRetentionPlans {
		return nil, ErrInvalidRetentionDocument
	}

	policies := make([]retentionPolicy, 0, len(document.Tenants))
	seen := make(map[string]struct{}, len(document.Tenants))
	for _, entry := range document.Tenants {
		policy := retentionPolicy{
			TenantID:   entry.TenantID,
			RetainFor:  time.Duration(entry.RetentionSeconds) * time.Second,
			BatchSize:  entry.BatchSize,
			MaxBatches: entry.MaxBatches,
			LegalHold:  entry.LegalHold,
		}
		if !validRetentionPolicy(policy) {
			return nil, ErrInvalidRetentionDocument
		}
		if _, duplicate := seen[policy.TenantID]; duplicate {
			return nil, ErrInvalidRetentionDocument
		}
		seen[policy.TenantID] = struct{}{}
		policies = append(policies, policy)
	}

	return policies, nil
}

func validRetentionPolicy(policy retentionPolicy) bool {
	return policy.TenantID == strings.TrimSpace(policy.TenantID) &&
		policy.TenantID != "" && len(policy.TenantID) <= controlplane.MaxIdentityBytes &&
		policy.RetainFor >= minimumRetention && policy.RetainFor <= maximumRetention &&
		policy.BatchSize > 0 && policy.BatchSize <= controlpostgres.MaxAuditBatch &&
		policy.MaxBatches > 0 && policy.MaxBatches <= maximumRetentionBatches
}

func applyRetention(
	ctx context.Context,
	audit retentionAudit,
	policies []retentionPolicy,
	now func() time.Time,
) error {
	if audit == nil || missingDependency(audit) || len(policies) == 0 || now == nil {
		return ErrInvalidRetentionPlan
	}

	var failures []error
	for _, policy := range policies {
		if !validRetentionPolicy(policy) {
			failures = append(failures, ErrInvalidRetentionPlan)
			continue
		}
		if policy.LegalHold {
			continue
		}
		if _, err := audit.VerifyTenant(ctx, policy.TenantID, policy.BatchSize); err != nil {
			failures = append(failures, err)
			continue
		}

		cutoff := now().UTC().Add(-policy.RetainFor)
		fullBatch := false
		auditFailed := false
		for range policy.MaxBatches {
			result, err := audit.RetainBefore(ctx, policy.TenantID, cutoff, policy.BatchSize)
			if err != nil {
				failures = append(failures, err)
				auditFailed = true
				fullBatch = false
				break
			}
			fullBatch = result.Deleted == policy.BatchSize
			if !fullBatch {
				break
			}
		}
		if _, err := audit.VerifyTenant(ctx, policy.TenantID, policy.BatchSize); err != nil {
			failures = append(failures, err)
			continue
		}
		if auditFailed || fullBatch {
			if fullBatch {
				failures = append(failures, ErrRetentionIncomplete)
			}
			continue
		}

		fullBatch = false
		for range policy.MaxBatches {
			result, err := audit.RetainCommandsBefore(
				ctx,
				policy.TenantID,
				cutoff,
				policy.BatchSize,
			)
			if err != nil {
				failures = append(failures, err)
				fullBatch = false
				break
			}
			fullBatch = result.Deleted == policy.BatchSize
			if !fullBatch {
				break
			}
		}
		if fullBatch {
			failures = append(failures, ErrRetentionIncomplete)
		}
	}

	return errors.Join(failures...)
}
