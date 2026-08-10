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

func TestInternalExportFailureSanitizationCoversEveryBoundary(t *testing.T) {
	t.Parallel()

	query, _ := NewQuery(QueryInput{Tenant: NoTenant(), Limit: 1})
	panicExporter := internalExporterFunc(func(context.Context, Query, func(Record) error) error {
		panic("token=export-secret")
	})
	if err := callObservedExport(panicExporter, context.Background(), query, func(Record) error { return nil }); !errors.Is(err, ErrExportFailed) {
		t.Fatalf("panic exporter error = %v", err)
	}
	for name, consume := range map[string]func(Record) error{
		"panic":    func(Record) error { panic("token=consumer-secret") },
		"canceled": func(Record) error { return context.Canceled },
		"deadline": func(Record) error { return context.DeadlineExceeded },
		"failure":  func(Record) error { return errors.New("token=consumer-secret") },
	} {
		err := consumeObservedSafely(consume, Record{})
		switch name {
		case "canceled":
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s consumer error = %v", name, err)
			}
		case "deadline":
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s consumer error = %v", name, err)
			}
		default:
			if !errors.Is(err, ErrExportConsumerFailed) {
				t.Fatalf("%s consumer error = %v", name, err)
			}
		}
	}
	for input, expected := range map[error]error{
		nil:                                   nil,
		context.Canceled:                      context.Canceled,
		context.DeadlineExceeded:              context.DeadlineExceeded,
		ErrExportConsumerFailed:               ErrExportConsumerFailed,
		ErrInvalidArgument:                    ErrInvalidArgument,
		ErrIntegrityInvalid:                   ErrIntegrityInvalid,
		errors.New("private exporter detail"): ErrExportFailed,
	} {
		if err := safeExportFailure(input); !errors.Is(err, expected) || (expected == nil && err != nil) {
			t.Fatalf("safeExportFailure(%v) = %v, want %v", input, err, expected)
		}
	}
}

func TestInternalRedactionValidationRejectsEveryStateForgery(t *testing.T) {
	t.Parallel()

	base := internalRecord("redaction-state", time.Now())
	noChangeForgery := base
	noChangeForgery.changes = ChangeSet{}

	alreadyRedacted := base
	alreadyRedacted.changes = ChangeSet{redacted: true}
	redactedForgery := alreadyRedacted
	redactedForgery.changes = ChangeSet{noChange: true}

	structured := base
	structured.changes = ChangeSet{before: map[string]string{"state": "before"}}
	structuredForgery := structured
	structuredForgery.changes = ChangeSet{noChange: true, before: map[string]string{"state": "before"}}

	emptyChanges := base
	emptyChanges.changes = ChangeSet{}
	emptyForgery := emptyChanges
	emptyForgery.changes = ChangeSet{noChange: true}

	for name, pair := range map[string][2]Record{
		"no-change":        {base, noChangeForgery},
		"already-redacted": {alreadyRedacted, redactedForgery},
		"structured":       {structured, structuredForgery},
		"empty":            {emptyChanges, emptyForgery},
	} {
		if err := validateRedaction(pair[0], pair[1]); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("%s redaction forgery error = %v", name, err)
		}
	}
	if mapKeysSubset(map[string]string{"injected": "value"}, map[string]string{"safe": "value"}) {
		t.Fatal("injected redaction key accepted")
	}
	input := internalRecordInput(time.Now())
	input.Changes = ChangeSetInput{Redacted: true}
	if err := validateInput("redacted-input", time.Now(), input, DefaultLimits()); err != nil {
		t.Fatalf("explicit redacted input error = %v", err)
	}
}

func TestInternalRedactionValidationChecksEachInvariantIndependently(t *testing.T) {
	t.Parallel()

	original := internalRecord("redaction-invariants", time.Now())
	original.attributes = map[string]string{"safe": "value"}
	original.changes = ChangeSet{
		before: map[string]string{"state": "before"},
		after:  map[string]string{"state": "after"},
	}

	changedAction := original
	changedAction.action = "resource.changed"
	injectedAttribute := original
	injectedAttribute.attributes = map[string]string{"safe": "value", "injected": "value"}
	injectedBefore := original
	injectedBefore.changes.before = map[string]string{"state": "before", "injected": "value"}
	injectedAfter := original
	injectedAfter.changes.after = map[string]string{"state": "after", "injected": "value"}

	for name, candidate := range map[string]Record{
		"non-privacy field": changedAction,
		"attribute key":     injectedAttribute,
		"before key":        injectedBefore,
		"after key":         injectedAfter,
	} {
		if err := validateRedaction(original, candidate); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("%s forgery error = %v", name, err)
		}
	}

	oneSidedChanges := original
	oneSidedChanges.changes.after = nil
	if err := validateRedaction(original, oneSidedChanges); err != nil {
		t.Fatalf("one-sided retained changes error = %v", err)
	}

	alreadyRedacted := original
	alreadyRedacted.changes = ChangeSet{redacted: true}
	redactedFlagRemoved := alreadyRedacted
	redactedFlagRemoved.changes = ChangeSet{}
	if err := validateRedaction(alreadyRedacted, redactedFlagRemoved); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("removed redacted flag error = %v", err)
	}

	emptyOriginal := original
	emptyOriginal.changes = ChangeSet{}
	if err := validateRedaction(emptyOriginal, emptyOriginal); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing fully-redacted marker error = %v", err)
	}
}

func TestInternalSafeKeyFailurePreservesOnlyPublicClassifications(t *testing.T) {
	t.Parallel()

	for input, expected := range map[error]error{
		context.Canceled:               context.Canceled,
		context.DeadlineExceeded:       context.DeadlineExceeded,
		ErrInvalidArgument:             ErrInvalidArgument,
		errors.New("kms token=secret"): ErrKeyUnavailable,
	} {
		if err := safeKeyFailure(input); !errors.Is(err, expected) {
			t.Fatalf("safeKeyFailure(%v) = %v, want %v", input, err, expected)
		}
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
	hmacChain, _ := NewChain(ChainConfig{Algorithm: IntegrityHMACSHA256, Keys: KeyProviderFunc(func(context.Context, KeyRequest) (IntegrityKey, error) { return key, nil })})
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
	hmacChain.keys = KeyProviderFunc(func(context.Context, KeyRequest) (IntegrityKey, error) { return IntegrityKey{}, providerFailure })
	if err := hmacChain.Verify(context.Background(), []Record{keyed}); !errors.Is(err, ErrKeyUnavailable) {
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
