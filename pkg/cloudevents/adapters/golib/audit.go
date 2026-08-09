package golib

import (
	"fmt"
	"strconv"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/faustbrian/golib/pkg/cloudevents"
	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/tenancy"
)

const (
	auditIDExtension      = "auditid"
	auditActionExtension  = "auditaction"
	auditOutcomeExtension = "auditoutcome"
)

// AuditMetadata is the explicitly selected, non-authoritative audit context
// that can be adopted from a trusted CloudEvent. It is not an Audit Record.
type AuditMetadata struct {
	RecordID    string
	Action      string
	Outcome     audit.Outcome
	Correlation correlation.Values
	Tenant      tenancy.TenantID
}

// AddAuditMetadata attaches a deliberately small safe subset of an audit
// record. Actor, subject, changes, policy, integrity, attributes, descriptions,
// and recording time stay owned by the canonical audit record and are reported.
func AddAuditMetadata(
	event cloudevents.Event,
	record audit.Record,
) (cloudevents.Event, Report, error) {
	if record.ID() == "" {
		return cloudevents.Event{}, Report{}, fmt.Errorf("%w: audit record", ErrInvalidAdapterInput)
	}
	contextValue := record.Context()
	additions := map[string]string{
		auditIDExtension:      record.ID(),
		auditActionExtension:  record.Action(),
		auditOutcomeExtension: strconv.FormatUint(uint64(record.Outcome()), 10),
	}
	if contextValue.CorrelationID() != "" {
		additions[correlationIDExtension] = contextValue.CorrelationID()
	}
	if contextValue.CausationID() != "" {
		additions[causationIDExtension] = contextValue.CausationID()
	}
	if value := contextValue.TenantID(); value != "" {
		tenant, err := tenancy.ParseTenantID(value)
		if err != nil {
			return cloudevents.Event{}, Report{}, fmt.Errorf("%w: audit tenant: %w", ErrInvalidAdapterInput, err)
		}
		additions[tenantIDExtension] = tenant.Value()
	}
	converted, err := addStringExtensions(event, additions)
	if err != nil {
		return cloudevents.Event{}, Report{}, err
	}
	return converted, Report{Losses: []Loss{
		{Field: "audit.recorded_at", Reason: "canonical audit ownership"},
		{Field: "audit.actor", Reason: "not flattened into CloudEvents"},
		{Field: "audit.subject", Reason: "not flattened into CloudEvents"},
		{Field: "audit.changes", Reason: "not flattened into CloudEvents"},
		{Field: "audit.policy", Reason: "not flattened into CloudEvents"},
		{Field: "audit.integrity", Reason: "not flattened into CloudEvents"},
		{Field: "audit.attributes", Reason: "not flattened into CloudEvents"},
	}}, nil
}

// ExtractAuditMetadata adopts only the selected fields after an explicit trust
// decision. It never constructs an Audit Record from CloudEvents metadata.
func ExtractAuditMetadata(event cloudevents.Event, trusted bool) (AuditMetadata, error) {
	recordID, hasRecord, err := stringExtension(event, auditIDExtension)
	if err != nil {
		return AuditMetadata{}, err
	}
	action, hasAction, err := stringExtension(event, auditActionExtension)
	if err != nil {
		return AuditMetadata{}, err
	}
	outcomeValue, hasOutcome, err := stringExtension(event, auditOutcomeExtension)
	if err != nil {
		return AuditMetadata{}, err
	}
	if !hasRecord {
		switch {
		case hasAction:
		case hasOutcome:
		default:
			return AuditMetadata{}, nil
		}
	}
	if !trusted {
		return AuditMetadata{}, ErrUntrustedMetadata
	}
	if !hasRecord || !hasAction || !hasOutcome {
		return AuditMetadata{}, fmt.Errorf("%w: incomplete audit metadata", ErrInvalidAdapterInput)
	}
	parsed, err := strconv.ParseUint(outcomeValue, 10, 8)
	if err != nil || audit.Outcome(parsed) < audit.OutcomeSucceeded || audit.Outcome(parsed) > audit.OutcomeUnknown {
		return AuditMetadata{}, fmt.Errorf("%w: audit outcome", ErrInvalidAdapterInput)
	}
	correlationValues, err := ExtractCorrelation(event, true, correlation.Policy{})
	if err != nil {
		return AuditMetadata{}, err
	}
	tenant, err := ExtractTenant(event, true)
	if err != nil {
		return AuditMetadata{}, err
	}
	return AuditMetadata{
		RecordID: recordID, Action: action, Outcome: audit.Outcome(parsed),
		Correlation: correlationValues, Tenant: tenant,
	}, nil
}
