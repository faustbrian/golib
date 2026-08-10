package audit_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

func TestTenantScopesAreExplicit(t *testing.T) {
	t.Parallel()

	if _, err := audit.Tenant(""); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("Tenant(empty) error = %v", err)
	}
	if _, err := audit.Tenant(string([]byte{0xff})); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("Tenant(invalid UTF-8) error = %v", err)
	}
	exact, err := audit.Tenant("tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	id, ok := exact.TenantID()
	if !ok || id != "tenant-1" || exact.Mode() != audit.TenantScopeExact || !exact.Includes("tenant-1") || exact.Includes("") {
		t.Fatalf("exact tenant scope = %#v, %q, %t", exact, id, ok)
	}
	absent := audit.NoTenant()
	if absent.Mode() != audit.TenantScopeAbsent || !absent.Includes("") || absent.Includes("tenant-1") {
		t.Fatalf("absent tenant scope = %#v", absent)
	}
	all := audit.AllTenants()
	if all.Mode() != audit.TenantScopeAll || !all.Includes("") || !all.Includes("tenant-1") {
		t.Fatalf("all tenant scope = %#v", all)
	}
	if (audit.TenantScope{}).Valid() || (audit.TenantScope{}).Includes("") {
		t.Fatal("zero tenant scope was accepted")
	}
}

func TestCursorRoundTripAndBounds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 123, time.FixedZone("offset", 2*60*60))
	cursor, err := audit.NewCursor(now, "record-1")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := audit.ParseCursor(cursor.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.RecordID() != "record-1" || !parsed.RecordedAt().Equal(now.UTC().Truncate(time.Microsecond)) || parsed.String() != cursor.String() {
		t.Fatalf("cursor round trip = %#v", parsed)
	}
	newlineCursor, err := audit.NewCursor(now, "record\nwith\nnewlines")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = audit.ParseCursor(newlineCursor.String())
	if err != nil || parsed.RecordID() != newlineCursor.RecordID() || parsed.String() != newlineCursor.String() {
		t.Fatalf("newline cursor round trip = %#v, %v", parsed, err)
	}
	snapshotCursor, err := audit.NewSnapshotCursor(now, "record-1", 42)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = audit.ParseCursor(snapshotCursor.String())
	if err != nil || parsed.Snapshot() != 42 || parsed.RecordID() != "record-1" || parsed.String() != snapshotCursor.String() {
		t.Fatalf("snapshot cursor round trip = %#v, %v", parsed, err)
	}
	maximumCursor, err := audit.NewSnapshotCursor(
		time.Date(9999, time.December, 31, 23, 59, 59, 999999000, time.UTC),
		strings.Repeat("r", audit.DefaultLimits().MaxFieldBytes), uint64(1<<63-1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := audit.ParseCursor(maximumCursor.String()); err != nil || parsed.String() != maximumCursor.String() {
		t.Fatalf("maximum cursor round trip = %#v, %v", parsed, err)
	}
	if _, err := audit.NewSnapshotCursor(now, "record-1", uint64(1<<63)); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewSnapshotCursor(over PostgreSQL bigint ceiling) error = %v", err)
	}
	for _, input := range []string{"", "%", "not-base64", "e30", strings.Repeat("YQ", 2*audit.DefaultLimits().MaxFieldBytes+1)} {
		if _, err := audit.ParseCursor(input); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("ParseCursor(%q) error = %v", input, err)
		}
	}
	malformedTime := base64.RawURLEncoding.EncodeToString([]byte("v1\nnot-a-time\nrecord-1"))
	if _, err := audit.ParseCursor(malformedTime); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("ParseCursor(malformed time) error = %v", err)
	}
	malformedSnapshotTime := base64.RawURLEncoding.EncodeToString([]byte("v2\nnot-a-time\n42\nrecord-1"))
	if _, err := audit.ParseCursor(malformedSnapshotTime); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("ParseCursor(malformed snapshot time) error = %v", err)
	}
	oversizedID := base64.RawURLEncoding.EncodeToString([]byte("v1\n2026-08-09T12:00:00Z\n" + strings.Repeat("r", audit.DefaultLimits().MaxFieldBytes+1)))
	if _, err := audit.ParseCursor(oversizedID); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("ParseCursor(oversized record ID) error = %v", err)
	}
	if (audit.Cursor{}).String() != "" {
		t.Fatal("zero cursor did not encode as empty")
	}
	if _, err := audit.NewCursor(time.Time{}, "record-1"); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewCursor(zero) error = %v", err)
	}
	if _, err := audit.NewCursor(now, ""); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewCursor(empty ID) error = %v", err)
	}
	if _, err := audit.NewCursor(now, string([]byte{0xff})); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewCursor(invalid UTF-8) error = %v", err)
	}
	if _, err := audit.NewSnapshotCursor(now, "record-1", 0); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewSnapshotCursor(zero snapshot) error = %v", err)
	}
	malformedSnapshot := base64.RawURLEncoding.EncodeToString([]byte("v2\n2026-08-09T12:00:00Z\ninvalid\nrecord-1"))
	if _, err := audit.ParseCursor(malformedSnapshot); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("ParseCursor(malformed snapshot) error = %v", err)
	}
	for _, malformed := range []string{
		"v2\n2026-08-09T12:00:00Z\nrecord-1",
		"v3\n2026-08-09T12:00:00Z\n42\nrecord-1",
	} {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(malformed))
		if _, err := audit.ParseCursor(encoded); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("ParseCursor(%q) error = %v", malformed, err)
		}
	}
}

func TestQueryRequiresBoundedCoherentFilters(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	valid, err := audit.NewQuery(audit.QueryInput{
		Tenant: audit.AllTenants(), From: base, Through: base.Add(time.Hour),
		ActorID: "actor-1", SubjectType: "invoice", SubjectID: "invoice-1",
		Action: "invoice.viewed", CorrelationID: "correlation-1",
		Outcome: audit.OutcomeDenied, Limit: audit.MaxQueryRecords,
	})
	if err != nil || !valid.Valid() || valid.Limit() != audit.MaxQueryRecords {
		t.Fatalf("NewQuery(valid) = %#v, %v", valid, err)
	}
	cases := []audit.QueryInput{
		{Tenant: audit.TenantScope{}, Limit: 1},
		{Tenant: audit.AllTenants(), Limit: 0},
		{Tenant: audit.AllTenants(), Limit: audit.MaxQueryRecords + 1},
		{Tenant: audit.AllTenants(), From: base.Add(time.Hour), Through: base, Limit: 1},
		{Tenant: audit.AllTenants(), Outcome: audit.Outcome(255), Limit: 1},
		{Tenant: audit.AllTenants(), ActorID: strings.Repeat("a", audit.DefaultLimits().MaxFieldBytes+1), Limit: 1},
		{Tenant: audit.AllTenants(), From: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 1},
		{Tenant: audit.AllTenants(), Through: time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 1},
		{Tenant: audit.AllTenants(), From: time.Date(0, 1, 1, 0, 0, 0, 0, time.FixedZone("east", 14*60*60)), Limit: 1},
		{Tenant: audit.AllTenants(), Through: time.Date(9999, 12, 31, 23, 0, 0, 0, time.FixedZone("west", -14*60*60)), Limit: 1},
	}
	for _, input := range cases {
		if _, err := audit.NewQuery(input); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewQuery(%#v) error = %v", input, err)
		}
	}
}

func TestRetentionContractsAreImmutableAndBounded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 999, time.FixedZone("offset", 2*60*60))
	event, err := audit.NewRetentionEvent(audit.RetentionEventInput{
		ID: "hold-1", RecordID: "security-record", ReasonCode: "legal_case",
		Kind: audit.RetentionHold, OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.ID() != "hold-1" || event.RecordID() != "security-record" || event.ReasonCode() != "legal_case" ||
		event.Kind() != audit.RetentionHold || !event.OccurredAt().Equal(now.UTC().Truncate(time.Microsecond)) || event.OccurredAt().Location() != time.UTC {
		t.Fatalf("retention event = %#v", event)
	}
	for _, input := range []audit.RetentionEventInput{
		{},
		{ID: "id", RecordID: "record", ReasonCode: "reason", Kind: audit.RetentionEventKind(99), OccurredAt: now},
		{ID: "id", RecordID: "record", ReasonCode: "reason", Kind: audit.RetentionHold},
		{ID: "id", RecordID: "record", ReasonCode: "reason", Kind: audit.RetentionHold, OccurredAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "id", RecordID: "record", ReasonCode: "reason", Kind: audit.RetentionHold, OccurredAt: time.Date(0, 1, 1, 0, 0, 0, 0, time.FixedZone("east", 14*60*60))},
	} {
		if _, err := audit.NewRetentionEvent(input); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewRetentionEvent(%#v) error = %v", input, err)
		}
	}

	request, err := audit.NewRetentionRequest(audit.RetentionRequestInput{Tenant: audit.AllTenants(), Before: now, Limit: 2})
	if err != nil || !request.Valid() || request.Tenant().Mode() != audit.TenantScopeAll ||
		!request.Before().Equal(now.UTC().Truncate(time.Microsecond)) || request.Limit() != 2 {
		t.Fatalf("NewRetentionRequest() = %#v, %v", request, err)
	}
	for _, input := range []audit.RetentionRequestInput{
		{Tenant: audit.TenantScope{}, Before: now, Limit: 1},
		{Tenant: audit.AllTenants(), Limit: 1},
		{Tenant: audit.AllTenants(), Before: now},
		{Tenant: audit.AllTenants(), Before: now, Limit: audit.MaxQueryRecords + 1},
		{Tenant: audit.AllTenants(), Before: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), Limit: 1},
		{Tenant: audit.AllTenants(), Before: time.Date(9999, 12, 31, 23, 0, 0, 0, time.FixedZone("west", -14*60*60)), Limit: 1},
	} {
		if _, err := audit.NewRetentionRequest(input); !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("NewRetentionRequest(%#v) error = %v", input, err)
		}
	}

	record := mustSecurityRecord(t)
	canonical, err := audit.CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	candidate, err := audit.NewRetentionCandidate(record, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	digest[0] ^= 0xff
	if candidate.Record().ID() != record.ID() || candidate.Digest()[0] == digest[0] {
		t.Fatal("retention candidate retained caller-owned digest")
	}
	plan, err := audit.NewRetentionPlan([]audit.RetentionCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	copy := plan.Candidates()
	copy[0], _ = audit.NewRetentionCandidate(record, digest[:])
	if plan.Candidates()[0].Digest()[0] != candidate.Digest()[0] {
		t.Fatal("retention plan exposed mutable candidates")
	}
	secondRecord := deliveryRecord(t, "a-retention-record")
	secondCanonical, _ := audit.CanonicalJSON(secondRecord)
	secondDigest := sha256.Sum256(secondCanonical)
	secondCandidate, err := audit.NewRetentionCandidate(secondRecord, secondDigest[:])
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := audit.NewRetentionPlan([]audit.RetentionCandidate{candidate, secondCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if got := ordered.Candidates(); got[0].Record().ID() != "a-retention-record" || got[1].Record().ID() != record.ID() {
		t.Fatalf("retention plan lock order = %q, %q", got[0].Record().ID(), got[1].Record().ID())
	}
	if _, err := audit.NewRetentionCandidate(audit.Record{}, make([]byte, sha256.Size)); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewRetentionCandidate(zero record) error = %v", err)
	}
	if _, err := audit.NewRetentionCandidate(record, []byte{1}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewRetentionCandidate(short digest) error = %v", err)
	}
	if _, err := audit.NewRetentionCandidate(record, make([]byte, sha256.Size)); !errors.Is(err, audit.ErrIntegrityInvalid) {
		t.Fatalf("NewRetentionCandidate(mismatched digest) error = %v", err)
	}
}
