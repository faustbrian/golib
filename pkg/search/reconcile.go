package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"unicode/utf8"
)

var (
	ErrInvalidReconciler           = errors.New("search: reconciliation dependencies and limits are required")
	ErrInvalidReconciliation       = errors.New("search: invalid reconciliation request")
	ErrReconciliationLimit         = errors.New("search: reconciliation record limit exceeded")
	ErrMalformedReconciliation     = errors.New("search: malformed or non-progressing reconciliation page")
	ErrReconciliationDeletionGuard = errors.New("search: orphan repair requires a durable source deletion guard")
	ErrRepairPartial               = errors.New("search: reconciliation repair was partial or ambiguous")
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
	if !utf8.Valid(source) {
		return ""
	}
	canonical, err := canonicalJSONObject(source)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
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

// ReconciliationDeletion identifies an indexed document that was absent from
// a bounded source snapshot. ObservedIndexVersion is the minimum version that
// a guarded deletion must supersede.
type ReconciliationDeletion struct {
	Tenant, Index, ID    string
	ObservedIndexVersion uint64
}

// ReconciliationDeletionGuard atomically confirms authoritative source
// deletion and reserves a durable tombstone version. The returned version must
// be greater than ObservedIndexVersion, and every later source write for the
// same identity must use a still greater version. Implementations must make a
// repeated reservation safe after an ambiguous or interrupted repair run and
// must be safe for concurrent calls.
type ReconciliationDeletionGuard interface {
	ReserveDeletion(context.Context, ReconciliationDeletion) (uint64, error)
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
	source        ReconciliationReader
	index         ReconciliationReader
	repair        Indexer
	deletionGuard ReconciliationDeletionGuard
	limits        Limits
}

// NewReconciler constructs a reconciler that repairs missing and stale
// documents but fails explicitly before dispatch if repair encounters an
// orphan. Use NewReconcilerWithDeletionGuard to authorize orphan deletion.
func NewReconciler(source, index ReconciliationReader, repair Indexer, limits Limits) (*Reconciler, error) {
	if source == nil || index == nil || repair == nil || limits.Validate() != nil {
		return nil, ErrInvalidReconciler
	}
	return &Reconciler{source: source, index: index, repair: repair, limits: limits}, nil
}

// NewReconcilerWithDeletionGuard constructs a reconciler that may repair
// orphaned index records after the guard durably authorizes each deletion.
func NewReconcilerWithDeletionGuard(source, index ReconciliationReader, repair Indexer, guard ReconciliationDeletionGuard, limits Limits) (*Reconciler, error) {
	if guard == nil {
		return nil, ErrInvalidReconciler
	}
	reconciler, err := NewReconciler(source, index, repair, limits)
	if err != nil {
		return nil, err
	}
	reconciler.deletionGuard = guard
	return reconciler, nil
}

// Run compares bounded, stable ID-ordered snapshots from the source of truth
// and derived index. Same-version content divergence is reported but never
// overwritten because external version semantics cannot safely apply it.
func (r *Reconciler) Run(ctx context.Context, request ReconciliationRequest) (ReconciliationReport, error) {
	maximumRecords := r.limits.MaxPages * r.limits.MaxPageItems
	if request.Tenant == "" || !utf8.ValidString(request.Tenant) {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	if len(request.Tenant) > r.limits.MaxTenantBytes {
		return ReconciliationReport{}, ErrInvalidReconciliation
	}
	if request.Index == "" || !utf8.ValidString(request.Index) {
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
	remainingBytes := r.limits.MaxResultBytes
	source, err := readReconciliation(ctx, r.source, request, true, r.limits, &remainingBytes)
	if err != nil {
		return ReconciliationReport{}, err
	}
	indexRequest := request
	indexRequest.MaxRecords -= len(source)
	indexed, err := readReconciliation(ctx, r.index, indexRequest, false, r.limits, &remainingBytes)
	if err != nil {
		return ReconciliationReport{}, err
	}
	report := ReconciliationReport{SourceRecords: len(source), IndexRecords: len(indexed), Complete: true}
	operations := make([]WriteOperation, 0)
	type guardedDeletion struct {
		position int
		request  ReconciliationDeletion
	}
	guardedDeletions := make([]guardedDeletion, 0)
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
			if request.Repair {
				guardedDeletions = append(guardedDeletions, guardedDeletion{
					position: len(operations),
					request:  ReconciliationDeletion{Tenant: request.Tenant, Index: request.Index, ID: indexed[right].ID, ObservedIndexVersion: indexed[right].Version},
				})
				operations = append(operations, WriteOperation{})
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
	if len(guardedDeletions) != 0 {
		report.Complete = false
		if r.deletionGuard == nil {
			return report, ErrReconciliationDeletionGuard
		}
		for _, deletion := range guardedDeletions {
			if deletion.request.ObservedIndexVersion == math.MaxUint64 {
				return report, ErrReconciliationDeletionGuard
			}
		}
		for _, deletion := range guardedDeletions {
			version, err := r.deletionGuard.ReserveDeletion(ctx, deletion.request)
			if err != nil {
				return report, classifiedReconciliationDeletionGuardError(err)
			}
			if version <= deletion.request.ObservedIndexVersion || version == math.MaxUint64 {
				return report, ErrReconciliationDeletionGuard
			}
			operations[deletion.position] = DeleteDocument(deletion.request.Tenant, deletion.request.Index, deletion.request.ID, version)
		}
	}
	if len(operations) == 0 {
		return report, nil
	}
	report.Complete = false
	for start := 0; start < len(operations); {
		end := min(start+r.limits.MaxBulkItems, len(operations))
		batchOperations := append([]WriteOperation(nil), operations[start:end]...)
		bulk := BulkRequest{Operations: batchOperations, Refresh: RefreshWaitFor}
		if err := bulk.Validate(AllCapabilities(), r.limits); err != nil {
			return report, err
		}
		result, err := r.repair.Bulk(ctx, bulk)
		if err != nil {
			return report, err
		}
		items := result.Items()
		if result.ValidateRequest(bulk) != nil {
			return report, ErrRepairPartial
		}
		for _, item := range items {
			if item.State == OutcomeApplied {
				report.Repaired++
			}
		}
		if result.Partial() {
			return report, ErrRepairPartial
		}
		start = end
	}
	report.Complete = true
	return report, nil
}

func classifiedReconciliationDeletionGuardError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return errors.Join(ErrReconciliationDeletionGuard, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return errors.Join(ErrReconciliationDeletionGuard, context.DeadlineExceeded)
	default:
		return ErrReconciliationDeletionGuard
	}
}

func readReconciliation(ctx context.Context, reader ReconciliationReader, request ReconciliationRequest, requireDocuments bool, limits Limits, sharedRemainingBytes ...*int64) ([]ReconciliationRecord, error) {
	localRemainingBytes := limits.MaxResultBytes
	remainingBytes := &localRemainingBytes
	if len(sharedRemainingBytes) == 1 && sharedRemainingBytes[0] != nil {
		remainingBytes = sharedRemainingBytes[0]
	}
	result := make([]ReconciliationRecord, 0)
	cursor := ""
	pages := 0
	for {
		if pages == limits.MaxPages {
			return nil, ErrReconciliationLimit
		}
		page, err := reader.Read(ctx, request.Tenant, request.Index, cursor, request.PageSize)
		if err != nil {
			return nil, err
		}
		pages++
		if len(page.Cursor) > limits.MaxQueryBytes || page.Done && page.Cursor != "" {
			return nil, ErrMalformedReconciliation
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
			if record.ID == "" || len(record.ID) > limits.MaxIDBytes || !utf8.ValidString(record.ID) || record.Version == 0 ||
				record.Digest == "" || len(record.Digest) > limits.MaxIDBytes || !utf8.ValidString(record.Digest) ||
				requireDocuments && record.Document == nil || !requireDocuments && record.Document != nil {
				return nil, ErrMalformedReconciliation
			}
			if record.Document != nil && (record.Document.Tenant != request.Tenant || record.Document.Index != request.Index || record.Document.ID != record.ID || record.Document.Version != record.Version) {
				return nil, ErrMalformedReconciliation
			}
			if requireDocuments {
				document, err := NewDocument(record.Document.Tenant, record.Document.Index, record.Document.ID, record.Document.Version, record.Document.Source, limits)
				if err != nil || record.Digest != SourceDigest(document.Source) {
					return nil, ErrMalformedReconciliation
				}
				record.Document = &document
			}
			if !reserveReconciliationRecord(record, remainingBytes) {
				return nil, ErrReconciliationLimit
			}
			result = append(result, cloneReconciliationRecord(record))
		}
		if page.Done {
			break
		}
		if len(result) == request.MaxRecords {
			return nil, ErrReconciliationLimit
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

func reserveReconciliationRecord(record ReconciliationRecord, remaining *int64) bool {
	// Reserve conservatively for the retained record, drift/report metadata,
	// and a possible repair operation. Source bytes can be owned once by the
	// snapshot and once by an index repair operation.
	const fixedOverhead int64 = 256
	available := *remaining
	sizes := []int64{fixedOverhead, int64(len(record.ID)), int64(len(record.Digest))}
	if record.Document != nil {
		sizes = append(sizes,
			int64(len(record.Document.Tenant)), int64(len(record.Document.Index)), int64(len(record.Document.ID)),
			int64(len(record.Document.Source)), int64(len(record.Document.Source)),
		)
	}
	for _, size := range sizes {
		if size > available {
			return false
		}
		available -= size
	}
	*remaining = available
	return true
}

func cloneReconciliationRecord(record ReconciliationRecord) ReconciliationRecord {
	if record.Document != nil {
		document := *record.Document
		document.Source = append(json.RawMessage(nil), document.Source...)
		record.Document = &document
	}
	return record
}
