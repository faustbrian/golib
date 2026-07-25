package main

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
)

func TestLoadRetentionPoliciesAcceptsBoundedLegalHoldPlan(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[` +
		`{"id":"tenant-1","retention_seconds":86400,"batch_size":2,"max_batches":3,"legal_hold":false},` +
		`{"id":"tenant-2","retention_seconds":172800,"batch_size":10,"max_batches":1,"legal_hold":true}` +
		`]}`
	policies, err := loadRetentionPolicies(strings.NewReader(document), 1024)
	want := []retentionPolicy{
		{TenantID: "tenant-1", RetainFor: 24 * time.Hour, BatchSize: 2, MaxBatches: 3},
		{TenantID: "tenant-2", RetainFor: 48 * time.Hour, BatchSize: 10, MaxBatches: 1, LegalHold: true},
	}
	if err != nil || !reflect.DeepEqual(policies, want) {
		t.Fatalf("loadRetentionPolicies() = (%+v, %v), want %+v", policies, err, want)
	}
}

func TestLoadRetentionPoliciesRejectsUnsafeDocuments(t *testing.T) {
	t.Parallel()
	var nilReader *strings.Reader
	for _, input := range []struct {
		reader io.Reader
		limit  int64
	}{
		{reader: nilReader, limit: 1024},
		{reader: strings.NewReader(`{}`)},
	} {
		policies, err := loadRetentionPolicies(input.reader, input.limit)
		if policies != nil || !errors.Is(err, ErrInvalidRetentionDocument) {
			t.Fatalf("loadRetentionPolicies(invalid input) = (%+v, %v)", policies, err)
		}
	}

	for name, input := range map[string]struct {
		document string
		limit    int64
	}{
		"missing":          {document: `{}`, limit: 1024},
		"empty":            {document: `{"tenants":[]}`, limit: 1024},
		"unknown":          {document: `{"tenants":[],"unknown":true}`, limit: 1024},
		"trailing":         {document: `{"tenants":[]}{}`, limit: 1024},
		"oversized":        {document: `{"tenants":[]}`, limit: 4},
		"blank tenant":     {document: `{"tenants":[` + retentionPolicyJSON(" ") + `]}`, limit: 1024},
		"duplicate tenant": {document: `{"tenants":[` + retentionPolicyJSON("tenant-1") + `,` + retentionPolicyJSON("tenant-1") + `]}`, limit: 2048},
		"short retention":  {document: retentionDocumentFor(3599, 10, 2), limit: 1024},
		"long retention":   {document: retentionDocumentFor(315360001, 10, 2), limit: 1024},
		"zero batch":       {document: retentionDocumentFor(86400, 0, 2), limit: 1024},
		"large batch":      {document: retentionDocumentFor(86400, 1001, 2), limit: 1024},
		"zero batches":     {document: retentionDocumentFor(86400, 10, 0), limit: 1024},
		"large batches":    {document: retentionDocumentFor(86400, 10, 101), limit: 1024},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			policies, err := loadRetentionPolicies(strings.NewReader(input.document), input.limit)
			if policies != nil || !errors.Is(err, ErrInvalidRetentionDocument) {
				t.Fatalf("loadRetentionPolicies() = (%+v, %v)", policies, err)
			}
		})
	}
}

func TestApplyRetentionVerifiesAndBoundsEveryTenant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	audit := &retentionAuditStub{retained: map[string][]uint32{
		"tenant-1": {2, 1},
	}}
	err := applyRetention(context.Background(), audit, []retentionPolicy{
		{TenantID: "tenant-1", RetainFor: 24 * time.Hour, BatchSize: 2, MaxBatches: 3},
		{TenantID: "tenant-2", RetainFor: 48 * time.Hour, BatchSize: 10, MaxBatches: 1, LegalHold: true},
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("applyRetention() error = %v", err)
	}
	if !reflect.DeepEqual(audit.verified, []string{"tenant-1", "tenant-1"}) {
		t.Fatalf("verified tenants = %v", audit.verified)
	}
	wantCutoff := now.Add(-24 * time.Hour)
	if !reflect.DeepEqual(audit.cutoffs, []time.Time{wantCutoff, wantCutoff}) ||
		!reflect.DeepEqual(audit.batches, []uint32{2, 2}) {
		t.Fatalf("retention calls = (%v, %v)", audit.cutoffs, audit.batches)
	}
	if !reflect.DeepEqual(audit.commandCutoffs, []time.Time{wantCutoff}) ||
		!reflect.DeepEqual(audit.commandBatches, []uint32{2}) {
		t.Fatalf("command retention calls = (%v, %v)", audit.commandCutoffs, audit.commandBatches)
	}
}

func TestApplyRetentionReportsIncompleteAndBackendFailures(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("audit failed")
	policy := []retentionPolicy{{TenantID: "tenant-1", RetainFor: time.Hour, BatchSize: 1, MaxBatches: 1}}
	for name, audit := range map[string]*retentionAuditStub{
		"incomplete":         {retained: map[string][]uint32{"tenant-1": {1}}},
		"verify":             {verifyErr: stageErr},
		"retain":             {retainErr: stageErr},
		"post verify":        {verifyErrors: []error{nil, stageErr}},
		"command":            {commandErr: stageErr},
		"command incomplete": {commandRetained: map[string][]uint32{"tenant-1": {1}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := applyRetention(context.Background(), audit, policy, time.Now)
			want := stageErr
			if name == "incomplete" || name == "command incomplete" {
				want = ErrRetentionIncomplete
			}
			if !errors.Is(err, want) {
				t.Fatalf("applyRetention() error = %v, want %v", err, want)
			}
		})
	}
	failedAudit := &retentionAuditStub{retainErr: stageErr}
	_ = applyRetention(context.Background(), failedAudit, policy, time.Now)
	if len(failedAudit.commandCutoffs) != 0 {
		t.Fatalf("failed audit retention reached command cleanup: %v", failedAudit.commandCutoffs)
	}
	if err := applyRetention(context.Background(), &retentionAuditStub{}, nil, time.Now); !errors.Is(err, ErrInvalidRetentionPlan) {
		t.Fatalf("applyRetention(empty) error = %v", err)
	}
	if err := applyRetention(context.Background(), nil, policy, time.Now); !errors.Is(err, ErrInvalidRetentionPlan) {
		t.Fatalf("applyRetention(nil audit) error = %v", err)
	}
	if err := applyRetention(context.Background(), &retentionAuditStub{}, policy, nil); !errors.Is(err, ErrInvalidRetentionPlan) {
		t.Fatalf("applyRetention(nil clock) error = %v", err)
	}
	invalid := []retentionPolicy{{TenantID: "", RetainFor: time.Hour, BatchSize: 1, MaxBatches: 1}}
	if err := applyRetention(context.Background(), &retentionAuditStub{}, invalid, time.Now); !errors.Is(err, ErrInvalidRetentionPlan) {
		t.Fatalf("applyRetention(invalid policy) error = %v", err)
	}
}

func retentionPolicyJSON(tenant string) string {
	return `{"id":"` + tenant + `","retention_seconds":86400,"batch_size":10,"max_batches":2,"legal_hold":false}`
}

func retentionDocumentFor(seconds int64, batch, batches uint32) string {
	return `{"tenants":[{"id":"tenant-1","retention_seconds":` +
		strconv.FormatInt(seconds, 10) + `,"batch_size":` + strconv.FormatUint(uint64(batch), 10) +
		`,"max_batches":` + strconv.FormatUint(uint64(batches), 10) + `,"legal_hold":false}]}`
}

type retentionAuditStub struct {
	verified        []string
	cutoffs         []time.Time
	batches         []uint32
	retained        map[string][]uint32
	verifyErr       error
	verifyErrors    []error
	retainErr       error
	commandCutoffs  []time.Time
	commandBatches  []uint32
	commandRetained map[string][]uint32
	commandErr      error
}

func (audit *retentionAuditStub) RetainCommandsBefore(
	_ context.Context,
	tenant string,
	cutoff time.Time,
	batch uint32,
) (controlpostgres.CommandRetentionResult, error) {
	audit.commandCutoffs = append(audit.commandCutoffs, cutoff)
	audit.commandBatches = append(audit.commandBatches, batch)
	deleted := uint32(0)
	if values := audit.commandRetained[tenant]; len(values) > 0 {
		deleted = values[0]
		audit.commandRetained[tenant] = values[1:]
	}

	return controlpostgres.CommandRetentionResult{Deleted: deleted}, audit.commandErr
}

func (audit *retentionAuditStub) VerifyTenant(
	_ context.Context,
	tenant string,
	_ uint32,
) (controlpostgres.VerificationReport, error) {
	audit.verified = append(audit.verified, tenant)
	if len(audit.verifyErrors) > 0 {
		err := audit.verifyErrors[0]
		audit.verifyErrors = audit.verifyErrors[1:]

		return controlpostgres.VerificationReport{}, err
	}

	return controlpostgres.VerificationReport{}, audit.verifyErr
}

func (audit *retentionAuditStub) RetainBefore(
	_ context.Context,
	tenant string,
	cutoff time.Time,
	batch uint32,
) (controlpostgres.RetentionResult, error) {
	audit.cutoffs = append(audit.cutoffs, cutoff)
	audit.batches = append(audit.batches, batch)
	deleted := uint32(0)
	if values := audit.retained[tenant]; len(values) > 0 {
		deleted = values[0]
		audit.retained[tenant] = values[1:]
	}

	return controlpostgres.RetentionResult{Deleted: deleted}, audit.retainErr
}
