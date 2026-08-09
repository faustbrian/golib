package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
)

var (
	ErrInvalidReconciler       = errors.New("search: reconciliation dependencies and limits are required")
	ErrInvalidReconciliation   = errors.New("search: invalid reconciliation request")
	ErrReconciliationLimit     = errors.New("search: reconciliation record limit exceeded")
	ErrMalformedReconciliation = errors.New("search: malformed or non-progressing reconciliation page")
	ErrRepairPartial           = errors.New("search: reconciliation repair was partial or ambiguous")
)

type ReconciliationRecord struct {
	ID       string
	Version  uint64
	Digest   string
	Document *Document
}

func SourceRecord(document Document) ReconciliationRecord {
	copyDocument := document
	copyDocument.Source = append(json.RawMessage(nil), document.Source...)
	return ReconciliationRecord{ID: document.ID, Version: document.Version, Digest: SourceDigest(document.Source), Document: &copyDocument}
}
func IndexRecord(id string, version uint64, digest string) ReconciliationRecord {
	return ReconciliationRecord{ID: id, Version: version, Digest: digest}
}
func SourceDigest(source json.RawMessage) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

type ReconciliationPage struct {
	Records []ReconciliationRecord
	Cursor  string
	Done    bool
}
type ReconciliationReader interface {
	Read(context.Context, string, string, string, int) (ReconciliationPage, error)
}

type DriftKind string

const (
	DriftMissing   DriftKind = "missing"
	DriftStale     DriftKind = "stale"
	DriftDivergent DriftKind = "divergent"
	DriftOrphaned  DriftKind = "orphaned"
)

type Drift struct {
	ID                          string
	Kind                        DriftKind
	SourceVersion, IndexVersion uint64
}
type ReconciliationRequest struct {
	Tenant, Index        string
	PageSize, MaxRecords int
	Repair               bool
}
type ReconciliationReport struct {
	SourceRecords int
	IndexRecords  int
	Drift         []Drift
	Repaired      int
	Complete      bool
}

type Reconciler struct {
	source ReconciliationReader
	index  ReconciliationReader
	repair Indexer
	limits Limits
}

func NewReconciler(source, index ReconciliationReader, repair Indexer, limits Limits) (*Reconciler, error) {
	if source == nil || index == nil || repair == nil || limits.Validate() != nil {
		return nil, ErrInvalidReconciler
	}
	return &Reconciler{source: source, index: index, repair: repair, limits: limits}, nil
}

// Run compares bounded, stable ID-ordered snapshots from the source of truth
// and derived index. Same-version content divergence is reported but never
// overwritten because external version semantics cannot safely apply it.
func (r *Reconciler) Run(ctx context.Context, request ReconciliationRequest) (ReconciliationReport, error) {
	maximumRecords := r.limits.MaxPages * r.limits.MaxPageItems
	if request.Tenant == "" {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	if len(request.Tenant) > r.limits.MaxTenantBytes {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	if request.Index == "" {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	if len(request.Index) > r.limits.MaxIndexBytes {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	if request.PageSize <= 0 || request.PageSize > r.limits.MaxPageItems {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	if request.MaxRecords <= 0 || request.MaxRecords > maximumRecords {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	source, err := readReconciliation(ctx, r.source, request, true)
	if err != nil {
		return ReconciliationReport{}, err
	}
	indexed, err := readReconciliation(ctx, r.index, request, false)
	if err != nil {
		return ReconciliationReport{}, err
	}
	if len(source) > request.MaxRecords-len(indexed) {
		return ReconciliationReport{}, ErrReconciliationLimit
	}

	report := ReconciliationReport{SourceRecords: len(source), IndexRecords: len(indexed), Complete: true}
	operations := make([]WriteOperation, 0)
	left, right := 0, 0
	for left < len(source) || right < len(indexed) {
		switch {
		case right >= len(indexed) || left < len(source) && source[left].ID < indexed[right].ID:
			report.Drift = append(report.Drift, Drift{ID: source[left].ID, Kind: DriftMissing, SourceVersion: source[left].Version})
			if request.Repair {
				operations = append(operations, IndexDocument(*source[left].Document))
			}
			left++
		case left >= len(source) || indexed[right].ID < source[left].ID:
			report.Drift = append(report.Drift, Drift{ID: indexed[right].ID, Kind: DriftOrphaned, IndexVersion: indexed[right].Version})
			if request.Repair && indexed[right].Version < math.MaxUint64 {
				operations = append(operations, DeleteDocument(request.Tenant, request.Index, indexed[right].ID, indexed[right].Version+1))
			}
			right++
		default:
			sourceRecord, indexRecord := source[left], indexed[right]
			if sourceRecord.Version > indexRecord.Version {
				report.Drift = append(report.Drift, Drift{ID: sourceRecord.ID, Kind: DriftStale, SourceVersion: sourceRecord.Version, IndexVersion: indexRecord.Version})
				if request.Repair {
					operations = append(operations, IndexDocument(*sourceRecord.Document))
				}
			} else if sourceRecord.Version != indexRecord.Version || sourceRecord.Digest != indexRecord.Digest {
				report.Drift = append(report.Drift, Drift{ID: sourceRecord.ID, Kind: DriftDivergent, SourceVersion: sourceRecord.Version, IndexVersion: indexRecord.Version})
			}
			left++
			right++
		}
	}
	if !request.Repair || len(operations) == 0 {
		return report, nil
	}
	bulk := BulkRequest{Operations: operations, Refresh: RefreshWaitFor}
	if err := bulk.Validate(AllCapabilities(), r.limits); err != nil {
		return report, err
	}
	result, err := r.repair.Bulk(ctx, bulk)
	if err != nil {
		return report, err
	}
	for _, item := range result.Items() {
		if item.State == OutcomeApplied {
			report.Repaired++
		}
	}
	if result.Partial() {
		return report, ErrRepairPartial
	}
	return report, nil
}

func readReconciliation(ctx context.Context, reader ReconciliationReader, request ReconciliationRequest, requireDocuments bool) ([]ReconciliationRecord, error) {
	result := make([]ReconciliationRecord, 0)
	cursor := ""
	for {
		page, err := reader.Read(ctx, request.Tenant, request.Index, cursor, request.PageSize)
		if err != nil {
			return nil, err
		}
		if !page.Done && (len(page.Records) == 0 || page.Cursor == "" || page.Cursor == cursor) {
			return nil, ErrMalformedReconciliation
		}
		if len(page.Records) > request.PageSize {
			return nil, ErrMalformedReconciliation
		}
		if len(page.Records) > request.MaxRecords-len(result) {
			return nil, ErrReconciliationLimit
		}
		for _, record := range page.Records {
			if record.ID == "" || record.Version == 0 || record.Digest == "" || requireDocuments && record.Document == nil {
				return nil, ErrMalformedReconciliation
			}
			if record.Document != nil && (record.Document.Tenant != request.Tenant || record.Document.Index != request.Index || record.Document.ID != record.ID || record.Document.Version != record.Version) {
				return nil, ErrMalformedReconciliation
			}
			result = append(result, cloneReconciliationRecord(record))
		}
		if page.Done {
			break
		}
		cursor = page.Cursor
	}
	for index := 1; index < len(result); index++ {
		if result[index-1].ID >= result[index].ID {
			return nil, ErrMalformedReconciliation
		}
	}
	return result, nil
}

func cloneReconciliationRecord(record ReconciliationRecord) ReconciliationRecord {
	if record.Document != nil {
		document := *record.Document
		document.Source = append(json.RawMessage(nil), document.Source...)
		record.Document = &document
	}
	return record
}
