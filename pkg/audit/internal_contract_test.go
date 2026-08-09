package audit

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

type internalExporterFunc func(context.Context, Query, func(Record) error) error

func (export internalExporterFunc) Export(ctx context.Context, query Query, consume func(Record) error) error {
	return export(ctx, query, consume)
}

func TestRandomRecordIDUsesReaderAndUUIDShape(t *testing.T) {
	t.Parallel()

	id, err := randomID(strings.NewReader("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[14] != '4' || (id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b') {
		t.Fatalf("randomID() = %q", id)
	}
	cause := errors.New("entropy unavailable")
	if _, err := randomID(errorReader{err: cause}); !errors.Is(err, cause) {
		t.Fatalf("randomID(error) = %v", err)
	}
	if _, err := randomID(io.LimitReader(strings.NewReader("short"), 5)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("randomID(short) = %v", err)
	}
}

func TestInternalValidationCoversIntegrityAndActorBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	base := internalRecordInput(now)
	tests := []func(*RecordInput){
		func(value *RecordInput) { value.ReasonCode = strings.Repeat("r", limits.MaxFieldBytes+1) },
		func(value *RecordInput) { value.Context.TenantID = strings.Repeat("t", limits.MaxFieldBytes+1) },
		func(value *RecordInput) { value.Integrity.Digest = make([]byte, limits.MaxIntegrityBytes+1) },
		func(value *RecordInput) {
			value.Integrity = IntegrityInput{Algorithm: IntegrityAlgorithm(99), Partition: "p", Sequence: 1, Digest: make([]byte, sha256.Size)}
		},
		func(value *RecordInput) {
			value.Integrity = IntegrityInput{Algorithm: IntegritySHA256, Partition: "p", Sequence: 1, PreviousDigest: make([]byte, sha256.Size), Digest: make([]byte, sha256.Size)}
		},
		func(value *RecordInput) {
			value.Integrity = IntegrityInput{Algorithm: IntegritySHA256, Partition: "p", Sequence: 2, Digest: make([]byte, sha256.Size)}
		},
		func(value *RecordInput) {
			value.Integrity = IntegrityInput{Algorithm: IntegrityHMACSHA256, Partition: "p", Sequence: 1, Digest: make([]byte, sha256.Size)}
		},
		func(value *RecordInput) {
			value.Integrity = IntegrityInput{Algorithm: IntegritySHA256, Partition: "p", KeyID: "key", Sequence: 1, Digest: make([]byte, sha256.Size)}
		},
		func(value *RecordInput) {
			value.Integrity = IntegrityInput{Partition: strings.Repeat("p", limits.MaxFieldBytes+1)}
		},
		func(value *RecordInput) {
			value.Integrity = IntegrityInput{Algorithm: IntegritySHA256, Partition: strings.Repeat("p", limits.MaxFieldBytes+1), Sequence: 1, Digest: make([]byte, sha256.Size)}
		},
		func(value *RecordInput) { value.Actor.ID = strings.Repeat("a", limits.MaxFieldBytes+1) },
		func(value *RecordInput) {
			value.Actor.AuthenticationMethod = strings.Repeat("a", limits.MaxFieldBytes+1)
		},
	}
	for index, mutate := range tests {
		input := base
		mutate(&input)
		if err := validateInput("record", now, input, limits); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("validation case %d error = %v", index, err)
		}
	}

	validIntegrity := base
	validIntegrity.Integrity = IntegrityInput{Algorithm: IntegrityHMACSHA256, Partition: "p", KeyID: "key", Sequence: 2, PreviousDigest: make([]byte, sha256.Size), Digest: make([]byte, sha256.Size)}
	if err := validateInput("record", now, validIntegrity, limits); err != nil {
		t.Fatal(err)
	}
	value := recordFromInput("record", now, validIntegrity).Integrity()
	if value.Algorithm() != IntegrityHMACSHA256 || value.Partition() != "p" || value.Sequence() != 2 {
		t.Fatalf("integrity accessors = %#v", value)
	}
}

func TestInternalCanonicalDecoderAndHelpersRejectEveryMalformedBoundary(t *testing.T) {
	t.Parallel()

	limits := DefaultLimits()
	if _, err := ParseCanonicalJSON([]byte("{}"), Limits{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := ParseCanonicalJSON(make([]byte, limits.MaxRecordBytes+1), limits); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("oversized canonical error = %v", err)
	}
	if _, err := decodeDigest("a", limits.MaxIntegrityBytes); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("odd digest error = %v", err)
	}
	if value, err := decodeDigest("", limits.MaxIntegrityBytes); err != nil || value != nil {
		t.Fatalf("empty digest = %x, %v", value, err)
	}
	if value, err := decodeDigest("00", limits.MaxIntegrityBytes); err != nil || len(value) != 1 {
		t.Fatalf("valid digest = %x, %v", value, err)
	}
	delegated := actorInput(canonicalActor{Kind: ActorHuman, ID: "human", DelegatedBy: &canonicalActor{Kind: ActorService, ID: "service"}})
	if delegated.DelegatedBy == nil || delegated.DelegatedBy.ID != "service" {
		t.Fatalf("actorInput() = %#v", delegated)
	}
}

func TestInternalBuilderDefaultsAndInvalidLimits(t *testing.T) {
	t.Parallel()

	bad := DefaultLimits()
	bad.MaxFieldBytes = 0
	if _, err := NewBuilder(BuilderConfig{Limits: bad}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("NewBuilder(invalid limits) error = %v", err)
	}
	builder, err := NewBuilder(BuilderConfig{})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(internalRecordInput(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(record.ID()) != 36 || record.RecordedAt().IsZero() {
		t.Fatalf("default builder record = %#v", record)
	}
}

func TestInternalRecorderConservativelyHandlesCorruptMode(t *testing.T) {
	t.Parallel()

	cause := NewAppendError(AppendUnknown, errors.New("failed"))
	sink := internalSink{appendErr: cause}
	recorder := &Recorder{
		config: RecorderConfig{
			Sink: sink,
			Redactor: RedactorFunc(func(_ context.Context, record Record) (Record, error) {
				return record, nil
			}),
			Mode: DeliveryMode(99),
		},
		clock: time.Now,
	}
	record := internalRecord("record", time.Now())
	if _, err := recorder.Submit(context.Background(), record); !errors.Is(err, cause) {
		t.Fatalf("corrupt-mode Submit() error = %v", err)
	}
	if _, err := recorder.SubmitBatch(context.Background(), []Record{record}); !errors.Is(err, cause) {
		t.Fatalf("corrupt-mode SubmitBatch() error = %v", err)
	}
}

func TestInternalObserversAndRedactorsValidateNilReceivers(t *testing.T) {
	t.Parallel()

	var redactor *ruleRedactor
	if _, err := redactor.Redact(context.Background(), Record{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil redactor error = %v", err)
	}
	compiled, _ := NewRedactor(RedactionRules{})
	redactor = compiled.(*ruleRedactor)
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := redactor.Redact(nil, Record{}); !errors.Is(err, ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context redactor error = %v", err)
	}

	var exporter *observedExporter
	query, _ := NewQuery(QueryInput{Tenant: NoTenant(), Limit: 1})
	if err := exporter.Export(context.Background(), query, func(Record) error { return nil }); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil observed exporter error = %v", err)
	}
	underlying := internalExporterFunc(func(context.Context, Query, func(Record) error) error { return nil })
	exporter = &observedExporter{exporter: underlying, observer: ObserverFunc(func(context.Context, Observation) {})}
	if err := exporter.Export(context.Background(), query, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil callback observed exporter error = %v", err)
	}
}

func TestInternalQueryAndRetentionRejectIncoherentPrivateState(t *testing.T) {
	t.Parallel()

	badCursor := Cursor{recordedAt: time.Now()}
	if _, err := NewQuery(QueryInput{Tenant: AllTenants(), Limit: 1, After: badCursor}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("incoherent cursor query error = %v", err)
	}
	if bad := (Cursor{recordedAt: time.Now(), recordID: "record"}); bad.String() == "" {
		t.Fatal("nonzero cursor encoded empty")
	}
	tooMany := make([]RetentionCandidate, MaxQueryRecords+1)
	if _, err := NewRetentionPlan(tooMany); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("oversized retention plan error = %v", err)
	}
	if _, err := NewRetentionPlan([]RetentionCandidate{{}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid retention candidate error = %v", err)
	}
}

func TestInternalChainVerificationRejectsEveryLinkDivergence(t *testing.T) {
	t.Parallel()

	chain, _ := NewChain(ChainConfig{Algorithm: IntegritySHA256})
	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	first, err := chain.Seal(context.Background(), internalRecord("record-1", base), ChainLink{Partition: "tenant-1", Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := chain.Seal(context.Background(), internalRecord("record-2", base.Add(time.Second)), ChainLink{Partition: "tenant-1", Sequence: 2, PreviousDigest: first.integrity.digest})
	if err != nil {
		t.Fatal(err)
	}

	mutations := []func(*Record){
		func(value *Record) { value.integrity.algorithm = IntegrityHMACSHA256 },
		func(value *Record) { value.integrity.partition = "tenant-2" },
		func(value *Record) { value.integrity.sequence = 3 },
		func(value *Record) { value.integrity.previousDigest = make([]byte, sha256.Size) },
		func(value *Record) { value.integrity.digest = []byte{1} },
		func(value *Record) { value.integrity.digest[0] ^= 0xff },
	}
	for index, mutate := range mutations {
		candidate := second
		candidate.integrity.previousDigest = append([]byte(nil), second.integrity.previousDigest...)
		candidate.integrity.digest = append([]byte(nil), second.integrity.digest...)
		mutate(&candidate)
		if err := chain.Verify(context.Background(), []Record{first, candidate}); !errors.Is(err, ErrIntegrityInvalid) {
			t.Fatalf("tamper case %d error = %v", index, err)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := chain.Verify(canceled, []Record{first}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Verify() error = %v", err)
	}
	wrongFinal, _ := NewCheckpoint("tenant-1", 2, make([]byte, sha256.Size))
	previous, _ := NewCheckpoint("tenant-1", 1, first.integrity.digest)
	if err := chain.VerifyFromCheckpoint(context.Background(), previous, []Record{second}, wrongFinal); !errors.Is(err, ErrIntegrityInvalid) {
		t.Fatalf("wrong final checkpoint error = %v", err)
	}
	if _, err := MerkleRoot([]Record{first, second, internalRecord("record-3", base.Add(2*time.Second))}); err != nil {
		t.Fatalf("odd MerkleRoot() error = %v", err)
	}

	key := IntegrityKey{ID: "key-1", Bytes: make([]byte, sha256.Size)}
	hmacChain, _ := NewChain(ChainConfig{Algorithm: IntegrityHMACSHA256, Keys: KeyProviderFunc(func(context.Context, string, time.Time) (IntegrityKey, error) { return key, nil })})
	keyed, err := hmacChain.Seal(context.Background(), internalRecord("keyed", base), ChainLink{Partition: "tenant-1", Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	keyed.integrity.keyID = "wrong-key"
	if err := hmacChain.Verify(context.Background(), []Record{keyed}); !errors.Is(err, ErrIntegrityInvalid) {
		t.Fatalf("wrong key ID error = %v", err)
	}
	providerFailure := errors.New("provider failed")
	keyed.integrity.keyID = "key-1"
	hmacChain.keys = KeyProviderFunc(func(context.Context, string, time.Time) (IntegrityKey, error) { return IntegrityKey{}, providerFailure })
	if err := hmacChain.Verify(context.Background(), []Record{keyed}); !errors.Is(err, providerFailure) {
		t.Fatalf("verification provider error = %v", err)
	}
}

func internalRecordInput(now time.Time) RecordInput {
	return RecordInput{
		OccurredAt: now, Action: "account.viewed", Outcome: OutcomeSucceeded,
		Actor:   ActorInput{Kind: ActorSystem, ID: "billing"},
		Subject: SubjectInput{Type: "account", ID: "account-1"},
		Changes: ChangeSetInput{NoChange: true},
	}
}

func internalRecord(id string, now time.Time) Record {
	return recordFromInput(id, now, internalRecordInput(now))
}

type internalSink struct{ appendErr error }

func (sink internalSink) Append(context.Context, Record) (AppendResult, error) {
	return AppendResult{}, sink.appendErr
}

func (sink internalSink) AppendBatch(context.Context, []Record) (BatchResult, error) {
	return BatchResult{}, sink.appendErr
}
