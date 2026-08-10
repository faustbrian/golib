package audit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCanonicalAndRecordLimitsAcceptExactCeilings(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	record := internalRecord("boundary-record", now)
	encoded, err := CanonicalJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.MaxRecordBytes = len(encoded)
	if parsed, err := ParseCanonicalJSON(encoded, limits); err != nil || parsed.ID() != record.ID() {
		t.Fatalf("exact-limit ParseCanonicalJSON() = %#v, %v", parsed, err)
	}
	limits.MaxRecordBytes--
	if _, err := ParseCanonicalJSON(encoded, limits); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("over-limit ParseCanonicalJSON() error = %v", err)
	}
	if _, err := ParseCanonicalJSON(nil, DefaultLimits()); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty ParseCanonicalJSON() error = %v", err)
	}

	digestLimit := sha256.Size
	exactHex := strings.Repeat("ab", digestLimit)
	if digest, err := decodeDigest(exactHex, digestLimit); err != nil || len(digest) != digestLimit {
		t.Fatalf("exact-limit decodeDigest() = %x, %v", digest, err)
	}
	if _, err := decodeDigest(exactHex+"ab", digestLimit); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("over-limit decodeDigest() error = %v", err)
	}

	buildLimits := DefaultLimits()
	buildLimits.MaxRecordBytes = len(encoded)
	builder, err := NewBuilder(BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return record.ID(), nil }, Limits: buildLimits})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(internalRecordInput(now)); err != nil {
		t.Fatalf("exact-limit Build() error = %v", err)
	}
	buildLimits.MaxRecordBytes--
	builder, _ = NewBuilder(BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return record.ID(), nil }, Limits: buildLimits})
	if _, err := builder.Build(internalRecordInput(now)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("over-limit Build() error = %v", err)
	}
}

func TestValidationDistinguishesEveryIntegrityAndActorBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	limits := DefaultLimits()
	base := internalRecordInput(now)
	valid := []RecordInput{base}
	for _, outcome := range []Outcome{OutcomeSucceeded, OutcomeUnknown} {
		input := base
		input.Outcome = outcome
		valid = append(valid, input)
	}
	for _, kind := range []ActorKind{ActorHuman, ActorService, ActorSystem, ActorAnonymous, ActorUnknown} {
		input := base
		input.Actor = ActorInput{Kind: kind}
		if kind == ActorHuman || kind == ActorService || kind == ActorSystem {
			input.Actor.ID = "actor"
		}
		valid = append(valid, input)
	}
	for index, input := range valid {
		if err := validateInput("record", now, input, limits); err != nil {
			t.Fatalf("valid boundary %d error = %v", index, err)
		}
	}

	complete := IntegrityInput{Algorithm: IntegritySHA256, Partition: "partition", Sequence: 1, Digest: make([]byte, sha256.Size)}
	invalidIntegrity := []IntegrityInput{
		{Algorithm: IntegritySHA256, Sequence: 1, Digest: make([]byte, sha256.Size)},
		{Algorithm: IntegritySHA256, Partition: "partition", Digest: make([]byte, sha256.Size)},
		{Algorithm: IntegritySHA256, Partition: "partition", Sequence: 1},
		{Algorithm: IntegritySHA256, Partition: "partition", Sequence: 2, Digest: make([]byte, sha256.Size)},
	}
	for index, integrity := range invalidIntegrity {
		input := base
		input.Integrity = integrity
		if err := validateInput("record", now, input, limits); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid integrity %d error = %v", index, err)
		}
	}
	input := base
	input.Integrity = complete
	if err := validateInput("record", now, input, limits); err != nil {
		t.Fatalf("complete sequence-one integrity error = %v", err)
	}
	input.Integrity.Sequence = 2
	input.Integrity.PreviousDigest = make([]byte, sha256.Size)
	if err := validateInput("record", now, input, limits); err != nil {
		t.Fatalf("complete sequence-two integrity error = %v", err)
	}

	for _, integrity := range []Integrity{
		{}, {sequence: 1}, {previousDigest: []byte{1}}, {digest: []byte{1}},
	} {
		want := integrity.sequence != 0 || len(integrity.previousDigest) != 0 || len(integrity.digest) != 0
		if integrity.Enabled() != want {
			t.Fatalf("Integrity.Enabled() = %t, want %t for %#v", integrity.Enabled(), want, integrity)
		}
	}
}

func TestMapAndFieldLimitsAcceptExactCeilings(t *testing.T) {
	t.Parallel()

	if err := validateMap("values", map[string]string{"a": "b"}, map[string]string{"c": "d"}, 2, 4, false); err != nil {
		t.Fatalf("exact map ceilings error = %v", err)
	}
	if err := validateMap("values", map[string]string{"a": "b"}, map[string]string{"c": "d"}, 1, 4, false); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("entry ceiling error = %v", err)
	}
	if err := validateMap("values", map[string]string{"a": "b"}, nil, 1, 2, false); err != nil {
		t.Fatalf("exact byte ceiling error = %v", err)
	}
	if err := validateMap("values", map[string]string{"a": "bc"}, nil, 1, 2, false); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("byte ceiling error = %v", err)
	}
	if err := boundedOptional("field", "ab", 2); err != nil {
		t.Fatalf("exact field ceiling error = %v", err)
	}
	if err := boundedOptional("field", "abc", 2); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("field ceiling error = %v", err)
	}
}

func TestRandomIDSetsExactVersionAndVariantBits(t *testing.T) {
	t.Parallel()

	id, err := randomID(strings.NewReader(strings.Repeat("\xff", 16)))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	if raw[6] != 0x4f || raw[8] != 0xbf {
		t.Fatalf("UUID version/variant bytes = %x/%x", raw[6], raw[8])
	}
}

func TestQueryAndRetentionAcceptExactCeilings(t *testing.T) {
	t.Parallel()

	maximum := DefaultLimits().MaxFieldBytes
	exactID := strings.Repeat("t", maximum)
	if scope, err := Tenant(exactID); err != nil || !scope.Valid() {
		t.Fatalf("exact-limit Tenant() = %#v, %v", scope, err)
	}
	if _, err := Tenant(exactID + "x"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("over-limit Tenant() error = %v", err)
	}
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	if cursor, err := NewCursor(now, exactID); err != nil || cursor.RecordID() != exactID {
		t.Fatalf("exact-limit NewCursor() = %#v, %v", cursor, err)
	}
	if _, err := NewCursor(now, exactID+"x"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("over-limit NewCursor() error = %v", err)
	}
	for _, scope := range []TenantScope{TenantScope{kind: tenantExact, id: "tenant"}, NoTenant(), AllTenants()} {
		if !scope.Valid() {
			t.Fatalf("valid scope rejected: %#v", scope)
		}
	}
	for _, scope := range []TenantScope{{kind: tenantExact}, {kind: tenantAbsent, id: "x"}, {kind: tenantAll, id: "x"}} {
		if scope.Valid() {
			t.Fatalf("incoherent scope accepted: %#v", scope)
		}
	}

	plain := "v1\n" + now.Format(canonicalTimeLayout) + "\n" + strings.Repeat("r", maximum)
	parsed, err := ParseCursor(base64.RawURLEncoding.EncodeToString([]byte(plain)))
	if err != nil || parsed.RecordID() != strings.Repeat("r", maximum) {
		t.Fatalf("exact decoded cursor = %#v, %v", parsed, err)
	}
	longestTime := "9999-12-31T23:59:59.999999999+23:59"
	exactEnvelope := "v1\n" + longestTime + "\n" + strings.Repeat("r", maximum)
	parsed, err = ParseCursor(base64.RawURLEncoding.EncodeToString([]byte(exactEnvelope)))
	if err != nil || parsed.RecordID() != strings.Repeat("r", maximum) {
		t.Fatalf("exact-limit cursor envelope = %#v, %v", parsed, err)
	}
	if _, err := ParseCursor(base64.RawURLEncoding.EncodeToString([]byte(exactEnvelope + "x"))); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("over-limit cursor envelope error = %v", err)
	}
	query, err := NewQuery(QueryInput{Tenant: AllTenants(), Outcome: OutcomeUnknown, Limit: MaxQueryRecords})
	if err != nil || query.Limit() != MaxQueryRecords {
		t.Fatalf("exact-limit NewQuery() = %#v, %v", query, err)
	}
	if _, err := NewQuery(QueryInput{Tenant: AllTenants(), Outcome: OutcomeUnknown + 1, Limit: 1}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unknown outcome query error = %v", err)
	}
	for _, plain := range []string{"v2\n" + now.Format(canonicalTimeLayout) + "\nrecord", "v1\n" + now.Format(canonicalTimeLayout)} {
		if _, err := ParseCursor(base64.RawURLEncoding.EncodeToString([]byte(plain))); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("malformed cursor %q error = %v", plain, err)
		}
	}
	if request, err := NewRetentionRequest(RetentionRequestInput{Tenant: AllTenants(), Before: now, Limit: MaxQueryRecords}); err != nil || request.Limit() != MaxQueryRecords {
		t.Fatalf("exact-limit retention request = %#v, %v", request, err)
	}
	candidates := make([]RetentionCandidate, MaxQueryRecords)
	record := internalRecord("retained", now)
	canonical, _ := CanonicalJSON(record)
	digest := sha256.Sum256(canonical)
	for index := range candidates {
		candidates[index], _ = NewRetentionCandidate(record, digest[:])
	}
	if plan, err := NewRetentionPlan(candidates); err != nil || len(plan.Candidates()) != int(MaxQueryRecords) {
		t.Fatalf("exact-limit retention plan length = %d, %v", len(plan.Candidates()), err)
	}
}

func TestRedactorPreservesExplicitChangesUntilAllAreRemoved(t *testing.T) {
	t.Parallel()

	record := internalRecord("redaction-boundary", time.Now())
	record.changes = ChangeSet{before: map[string]string{"before": "kept"}, after: map[string]string{"after": "kept"}}
	for _, allowed := range [][]string{{"before"}, {"after"}} {
		redactor, _ := NewRedactor(RedactionRules{AllowedChanges: allowed})
		redacted, err := redactor.Redact(context.Background(), record)
		if err != nil || redacted.changes.noChange || len(redacted.changes.before)+len(redacted.changes.after) != 1 {
			t.Fatalf("partially redacted changes = %#v, %v", redacted.changes, err)
		}
	}
	redactor, _ := NewRedactor(RedactionRules{})
	redacted, err := redactor.Redact(context.Background(), record)
	if err != nil || redacted.changes.noChange || !redacted.changes.redacted {
		t.Fatalf("fully redacted changes = %#v, %v", redacted.changes, err)
	}
}

func TestIntegrityVerificationBoundaryConditions(t *testing.T) {
	t.Parallel()

	var nilChain *Chain
	if err := nilChain.Verify(context.Background(), nil); !errors.Is(err, ErrIntegrityInvalid) {
		t.Fatalf("nil-chain Verify() error = %v", err)
	}
	if err := nilChain.VerifyFromCheckpoint(context.Background(), Checkpoint{}, nil, Checkpoint{}); !errors.Is(err, ErrIntegrityInvalid) {
		t.Fatalf("nil-chain VerifyFromCheckpoint() error = %v", err)
	}

	chain, _ := NewChain(ChainConfig{Algorithm: IntegritySHA256})
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	first, _ := chain.Seal(context.Background(), internalRecord("first", base), ChainLink{Partition: "p", Sequence: 1})
	checkpoint, _ := NewCheckpoint("p", 1, first.integrity.digest)
	if err := chain.VerifyFromCheckpoint(context.Background(), checkpoint, nil, checkpoint); err != nil {
		t.Fatalf("empty verified suffix error = %v", err)
	}
	badPrevious, _ := NewCheckpoint("p", 2, first.integrity.digest)
	if err := chain.VerifyFromCheckpoint(context.Background(), badPrevious, nil, checkpoint); !errors.Is(err, ErrIntegrityInvalid) {
		t.Fatalf("reversed checkpoint range error = %v", err)
	}

	mutations := []func(*Record){
		func(value *Record) { value.integrity.algorithm = IntegrityHMACSHA256 },
		func(value *Record) { value.integrity.sequence = 2 },
		func(value *Record) { value.integrity.previousDigest = []byte{1} },
		func(value *Record) { value.integrity.digest = []byte{1} },
	}
	for index, mutate := range mutations {
		candidate := first
		candidate.integrity.digest = append([]byte(nil), first.integrity.digest...)
		mutate(&candidate)
		if len(candidate.integrity.digest) == sha256.Size {
			candidate.integrity.digest = chain.digest(candidate, nil)
		}
		if err := chain.Verify(context.Background(), []Record{candidate}); !errors.Is(err, ErrIntegrityInvalid) {
			t.Fatalf("first-link mutation %d error = %v", index, err)
		}
	}
	second, _ := chain.Seal(context.Background(), internalRecord("second", base.Add(time.Second)), ChainLink{Partition: "p", Sequence: 2, PreviousDigest: first.integrity.digest})
	second.integrity.partition = "other"
	second.integrity.digest = chain.digest(second, nil)
	if err := chain.Verify(context.Background(), []Record{first, second}); !errors.Is(err, ErrIntegrityInvalid) {
		t.Fatalf("cross-partition link error = %v", err)
	}
}

func TestRecorderDependencyBatchAndObservationBoundaries(t *testing.T) {
	t.Parallel()

	redactor := RedactorFunc(func(_ context.Context, record Record) (Record, error) { return record, nil })
	sink := mutationSink{}
	for _, config := range []RecorderConfig{
		{Sink: sink, Mode: DeliveryFailClosed},
		{Redactor: redactor, Mode: DeliveryFailClosed},
	} {
		if _, err := NewRecorder(config); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("missing dependency NewRecorder(%#v) error = %v", config, err)
		}
	}

	observed := make([]Observation, 0, 4)
	now := time.Date(2026, time.August, 9, 12, 0, 2, 0, time.UTC)
	recorder, err := NewRecorder(RecorderConfig{
		Sink: mutationSink{appendErr: NewAppendError(AppendRejected, ErrBackpressure)}, Redactor: redactor,
		Mode: DeliveryFailClosed, Clock: func() time.Time { return now }, DelayThreshold: time.Second,
		Observer: ObserverFunc(func(_ context.Context, value Observation) { observed = append(observed, value) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	exactlyDelayed := internalRecord("exactly-delayed", now.Add(-time.Second))
	if _, err := recorder.Submit(context.Background(), exactlyDelayed); AppendOutcomeOf(err) != AppendRejected {
		t.Fatalf("rejected Submit() error = %v", err)
	}
	if len(observed) != 3 || observed[0].Kind != ObservationDelayed || observed[1].Kind != ObservationFailed || observed[2].Kind != ObservationRejected {
		t.Fatalf("rejected observations = %#v", observed)
	}

	observed = observed[:0]
	recorder.config.Sink = mutationSink{}
	if _, err := recorder.Submit(context.Background(), internalRecord("not-delayed", now.Add(-time.Second+time.Nanosecond))); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 || observed[0].Kind != ObservationAccepted {
		t.Fatalf("below-threshold observations = %#v", observed)
	}

	records := make([]Record, MaxAppendBatchRecords)
	for index := range records {
		records[index] = internalRecord("batch-record", now)
	}
	if result, err := recorder.SubmitBatch(context.Background(), records); err != nil || len(result.Append.Results) != MaxAppendBatchRecords {
		t.Fatalf("exact-limit SubmitBatch() results = %d, %v", len(result.Append.Results), err)
	}
}

func TestIntegrityAlgorithmSelectsKeyedAndUnkeyedDigests(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	record := internalRecord("algorithm-boundary", now)
	providerCalls := 0
	provider := KeyProviderFunc(func(context.Context, KeyRequest) (IntegrityKey, error) {
		providerCalls++
		return IntegrityKey{ID: "key", Bytes: []byte("0123456789abcdef0123456789abcdef")}, nil
	})
	plain, _ := NewChain(ChainConfig{Algorithm: IntegritySHA256, Keys: provider})
	plainRecord, err := plain.Seal(context.Background(), record, ChainLink{Partition: "p", Sequence: 1})
	if err != nil || providerCalls != 0 || plainRecord.integrity.keyID != "" {
		t.Fatalf("unkeyed Seal() = %#v, calls=%d, %v", plainRecord.integrity, providerCalls, err)
	}
	keyed, _ := NewChain(ChainConfig{Algorithm: IntegrityHMACSHA256, Keys: provider})
	keyedRecord, err := keyed.Seal(context.Background(), record, ChainLink{Partition: "p", Sequence: 1})
	if err != nil || providerCalls != 1 || keyedRecord.integrity.keyID != "key" || strings.EqualFold(hex.EncodeToString(keyedRecord.integrity.digest), hex.EncodeToString(plainRecord.integrity.digest)) {
		t.Fatalf("keyed Seal() = %#v, calls=%d, %v", keyedRecord.integrity, providerCalls, err)
	}
}

func TestMerkleRootMatchesThreeLeafConstruction(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	records := []Record{internalRecord("a", base), internalRecord("b", base), internalRecord("c", base)}
	leaves := make([][sha256.Size]byte, len(records))
	for index, record := range records {
		encoded, _ := CanonicalJSON(record)
		leaves[index] = sha256.Sum256(encoded)
	}
	pair := func(left, right []byte) [sha256.Size]byte {
		payload := append([]byte{1}, left...)
		payload = append(payload, right...)
		return sha256.Sum256(payload)
	}
	left := pair(leaves[0][:], leaves[1][:])
	right := pair(leaves[2][:], leaves[2][:])
	want := pair(left[:], right[:])
	got, err := MerkleRoot(records)
	if err != nil || !strings.EqualFold(hex.EncodeToString(got), hex.EncodeToString(want[:])) {
		t.Fatalf("MerkleRoot(three) = %x, %v, want %x", got, err, want)
	}
}

type mutationSink struct{ appendErr error }

func (sink mutationSink) Append(_ context.Context, record Record) (AppendResult, error) {
	if sink.appendErr != nil {
		return AppendResult{}, sink.appendErr
	}
	return AppendResult{RecordID: record.ID(), Status: AppendAccepted}, nil
}

func (sink mutationSink) AppendBatch(_ context.Context, records []Record) (BatchResult, error) {
	if sink.appendErr != nil {
		return BatchResult{}, sink.appendErr
	}
	results := make([]AppendResult, len(records))
	for index, record := range records {
		results[index] = AppendResult{RecordID: record.ID(), Status: AppendAccepted}
	}
	return BatchResult{Results: results}, nil
}
