package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
)

func TestStoreAcceptsExactConfigurationLimits(t *testing.T) {
	store, err := newStore(&faultDatabase{}, StoreConfig{
		MaxClaimBatch:    maximumStoreBatch,
		MaxAdminBatch:    maximumStoreBatch,
		MaxLeaseDuration: maximumLeaseDuration,
	})
	if err != nil {
		t.Fatalf("exact store limits: %v", err)
	}
	if store.maxClaimBatch != maximumStoreBatch || store.maxAdminBatch != maximumStoreBatch ||
		store.maxLeaseDuration != maximumLeaseDuration {
		t.Fatalf("store limits = %d/%d/%s", store.maxClaimBatch, store.maxAdminBatch, store.maxLeaseDuration)
	}
	if store.clock().IsZero() {
		t.Fatal("default clock returned the zero time")
	}
}

func TestStoreOperationsAcceptExactRequestLimits(t *testing.T) {
	queryFailure := errors.New("query reached")
	identifier := strings.Repeat("x", maxIdentifierBytes)
	store, err := newStore(&faultDatabase{queryErr: queryFailure}, StoreConfig{
		MaxClaimBatch:       maximumStoreBatch,
		MaxAdminBatch:       maximumStoreBatch,
		MaxLeaseDuration:    maximumLeaseDuration,
		LeaseTokenGenerator: func() (string, error) { return identifier, nil },
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.Inspect(context.Background(), InspectRequest{Limit: maximumStoreBatch}); !errors.Is(err, queryFailure) {
		t.Fatalf("exact inspect limit error = %v", err)
	}
	if _, err := store.Claim(context.Background(), ClaimRequest{
		Owner: identifier, Limit: maximumStoreBatch, LeaseDuration: maximumLeaseDuration,
	}); !errors.Is(err, queryFailure) {
		t.Fatalf("exact claim limits error = %v", err)
	}

	rowFailure := errors.New("query row reached")
	extensionStore, err := newStore(&faultDatabase{row: faultRow{err: rowFailure}}, StoreConfig{
		MaxLeaseDuration: maximumLeaseDuration,
	})
	if err != nil {
		t.Fatalf("create extension store: %v", err)
	}
	if _, err := extensionStore.ExtendLease(context.Background(), LeaseRef{
		ID: identifier, Token: identifier,
	}, maximumLeaseDuration); !errors.Is(err, rowFailure) {
		t.Fatalf("exact lease duration error = %v", err)
	}
	if err := extensionStore.Retry(context.Background(), LeaseRef{
		ID: identifier, Token: identifier,
	}, maximumRetryDelay, nil); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("exact retry delay error = %v", err)
	}

	overflow := identifier + "x"
	if _, err := store.Claim(context.Background(), ClaimRequest{
		Owner: overflow, Limit: 1, LeaseDuration: time.Second,
	}); !errors.Is(err, ErrValueOutsideBounds) {
		t.Fatalf("overflow claim owner error = %v", err)
	}
	tokenStore, err := newStore(&faultDatabase{}, StoreConfig{
		LeaseTokenGenerator: func() (string, error) { return overflow, nil },
	})
	if err != nil {
		t.Fatalf("create overflow-token store: %v", err)
	}
	if _, err := tokenStore.Claim(context.Background(), ClaimRequest{
		Owner: "owner", Limit: 1, LeaseDuration: time.Second,
	}); !errors.Is(err, ErrValueOutsideBounds) {
		t.Fatalf("overflow claim token error = %v", err)
	}
}

func TestReplayAcceptsExactBoundsBeforePersistence(t *testing.T) {
	beginFailure := errors.New("begin reached")
	identifier := strings.Repeat("x", maxIdentifierBytes)
	ids := make([]string, maximumStoreBatch)
	for index := range ids {
		ids[index] = identifier[:maxIdentifierBytes-8] + fmt.Sprintf("%08d", index)
	}
	store, err := newStore(&faultDatabase{beginErr: beginFailure}, StoreConfig{
		MaxAdminBatch:       maximumStoreBatch,
		LeaseTokenGenerator: func() (string, error) { return identifier, nil },
		ReplayAuthorizer:    allowReplayForTest,
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	for _, availableAt := range []time.Time{
		time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC),
	} {
		_, err := store.Replay(context.Background(), ReplayRequest{
			IDs: ids, RequestedBy: identifier, Reason: strings.Repeat("r", maxReplayReasonBytes),
			AvailableAt: availableAt,
		})
		if !errors.Is(err, beginFailure) {
			t.Fatalf("exact replay bounds at %s error = %v", availableAt, err)
		}
	}
}

func TestReplayRejectsEveryOverflowBoundary(t *testing.T) {
	exact := strings.Repeat("x", maxIdentifierBytes)
	overflow := exact + "x"
	base := ReplayRequest{IDs: []string{"id"}, RequestedBy: "operator", Reason: "reason"}
	store, err := newStore(&faultDatabase{}, StoreConfig{
		LeaseTokenGenerator: func() (string, error) { return "replay", nil },
		ReplayAuthorizer:    allowReplayForTest,
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	for name, request := range map[string]ReplayRequest{
		"requester": {IDs: base.IDs, RequestedBy: overflow, Reason: base.Reason},
		"reason":    {IDs: base.IDs, RequestedBy: base.RequestedBy, Reason: strings.Repeat("r", maxReplayReasonBytes+1)},
		"message ID": {IDs: []string{overflow}, RequestedBy: base.RequestedBy,
			Reason: base.Reason},
	} {
		if _, err := store.Replay(context.Background(), request); !errors.Is(err, ErrValueOutsideBounds) {
			t.Fatalf("overflow %s error = %v", name, err)
		}
	}
	replayIDStore, err := newStore(&faultDatabase{}, StoreConfig{
		LeaseTokenGenerator: func() (string, error) { return overflow, nil },
		ReplayAuthorizer:    allowReplayForTest,
	})
	if err != nil {
		t.Fatalf("create replay-ID store: %v", err)
	}
	if _, err := replayIDStore.Replay(context.Background(), base); !errors.Is(err, ErrValueOutsideBounds) {
		t.Fatalf("overflow replay ID error = %v", err)
	}
}

func TestMaintenanceAcceptsExactBatchLimit(t *testing.T) {
	store, err := newStore(&faultDatabase{rows: &faultRows{}, tx: &faultTx{rows: &faultRows{}}}, StoreConfig{
		MaxAdminBatch: maximumStoreBatch,
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, err := store.PruneDelivered(context.Background(), time.Now(), maximumStoreBatch); err != nil {
		t.Fatalf("exact prune limit: %v", err)
	}
	if _, err := store.ArchiveAndPruneDelivered(
		context.Background(), time.Now(), maximumStoreBatch,
		ArchiveFunc(func(context.Context, []DeliveredMessage) error { return nil }),
	); err != nil {
		t.Fatalf("exact archive limit: %v", err)
	}
}

func TestObserverClampsNegativeDuration(t *testing.T) {
	now := time.Now()
	clockCalls := 0
	observer := &storeObserver{}
	store, err := newStore(&faultDatabase{queryErr: errors.New("failure")}, StoreConfig{
		Observer: observer,
		Clock: func() time.Time {
			clockCalls++
			if clockCalls == 1 {
				return now
			}
			return now.Add(-time.Nanosecond)
		},
	})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	_, _ = store.PruneDelivered(context.Background(), now, 1)
	if len(observer.events) != 1 || observer.events[0].Duration != 0 {
		t.Fatalf("observer events = %#v", observer.events)
	}
}

func TestLeaseReferenceAcceptsExactIdentifierBounds(t *testing.T) {
	exact := strings.Repeat("x", maxIdentifierBytes)
	if err := validateLeaseRef(LeaseRef{ID: exact, Token: exact}); err != nil {
		t.Fatalf("exact lease reference: %v", err)
	}
	for name, lease := range map[string]LeaseRef{
		"id":    {ID: exact + "x"},
		"token": {Token: exact + "x"},
	} {
		if err := validateLeaseRef(lease); !errors.Is(err, ErrValueOutsideBounds) {
			t.Fatalf("%s overflow error = %v", name, err)
		}
	}
}

func TestWriterAcceptsExactLimitsAndBuildsExactBatchShape(t *testing.T) {
	writer, err := NewWriter(WriterConfig{MaxBatchSize: maximumInsertBatch})
	if err != nil {
		t.Fatalf("exact writer batch limit: %v", err)
	}
	if writer.maxBatchSize != maximumInsertBatch {
		t.Fatalf("writer batch limit = %d", writer.maxBatchSize)
	}
	if _, err := NewWriter(WriterConfig{MaxBatchSize: maximumInsertBatch + 1}); !errors.Is(err, ErrInvalidBatchLimit) {
		t.Fatalf("overflow writer batch limit error = %v", err)
	}

	now := time.Now()
	envelopes := []outbox.Envelope{
		{ID: "one", Topic: "topic", PayloadVersion: 1, AvailableAt: now, CreatedAt: now},
		{ID: "two", Topic: "topic", PayloadVersion: 1, AvailableAt: now, CreatedAt: now},
	}
	query, arguments := writer.insertQuery(envelopes)
	wantValues := " VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)," +
		"($11,$12,$13,$14,$15,$16,$17,$18,$19,$20)"
	if !strings.HasSuffix(query, wantValues) || len(arguments) != 20 {
		t.Fatalf("query/arguments = %q/%d", query, len(arguments))
	}
}

func TestWriterSchemaAcceptsExactStorageBounds(t *testing.T) {
	exactIdentifier := strings.Repeat("x", maxIdentifierBytes)
	exactMetadata := strings.Repeat("x", maxEncodedMetadataBytes-len(`{"k":""}`))
	for name, envelope := range map[string]outbox.Envelope{
		"id":              {ID: exactIdentifier},
		"topic":           {Topic: exactIdentifier},
		"ordering key":    {OrderingKey: exactIdentifier},
		"idempotency key": {IdempotencyKey: exactIdentifier},
		"payload":         {Payload: make([]byte, maxPayloadBytes)},
		"metadata":        {Metadata: map[string]string{"k": exactMetadata}},
	} {
		if err := validateEnvelopeForSchema(envelope); err != nil {
			t.Fatalf("exact %s bound: %v", name, err)
		}
	}
	for name, envelope := range map[string]outbox.Envelope{
		"id":              {ID: exactIdentifier + "x"},
		"topic":           {Topic: exactIdentifier + "x"},
		"ordering key":    {OrderingKey: exactIdentifier + "x"},
		"idempotency key": {IdempotencyKey: exactIdentifier + "x"},
		"payload":         {Payload: make([]byte, maxPayloadBytes+1)},
		"metadata":        {Metadata: map[string]string{"k": exactMetadata + "x"}},
	} {
		if err := validateEnvelopeForSchema(envelope); !errors.Is(err, ErrValueOutsideBounds) {
			t.Fatalf("overflow %s error = %v", name, err)
		}
	}
}

func TestWriterAcceptsExactConfiguredBatch(t *testing.T) {
	writer, err := NewWriter(WriterConfig{MaxBatchSize: maximumInsertBatch})
	if err != nil {
		t.Fatal(err)
	}
	envelope := outbox.Envelope{
		ID: "message", Topic: "topic", PayloadVersion: 1,
		AvailableAt: time.Now(), CreatedAt: time.Now(),
	}
	envelopes := make([]outbox.Envelope, maximumInsertBatch)
	for index := range envelopes {
		envelopes[index] = envelope
	}
	tx := &faultTx{}
	if err := writer.InsertBatch(context.Background(), tx, envelopes); err != nil {
		t.Fatalf("exact configured batch: %v", err)
	}
}
