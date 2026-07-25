package webhook

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestVerifyAndRecordAtomicallyRejectsReplay(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store := &memoryReplayStore{seen: make(map[string]time.Time)}
	verifier, signatures, message := replayFixture(t, now, store)

	const requests = 64
	start := make(chan struct{})
	results := make(chan error, requests)
	var group sync.WaitGroup
	for range requests {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := verifier.VerifyAndRecord(context.Background(), message, signatures, "event-123")
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)

	accepted := 0
	replayed := 0
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrReplay):
			replayed++
		default:
			t.Fatalf("VerifyAndRecord() error = %v", err)
		}
	}
	if accepted != 1 || replayed != requests-1 {
		t.Fatalf("accepted = %d, replayed = %d", accepted, replayed)
	}
}

func TestReplayIdentitySurvivesSecretRotation(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	message := Message{Timestamp: now, Nonce: "nonce", Method: "POST", Path: "/", Host: "example.com"}
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys: []SigningKey{
			{ID: "new", Secret: []byte("new-secret")},
			{ID: "old", Secret: []byte("old-secret")},
		},
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	signatures, err := signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	store := &memoryReplayStore{seen: make(map[string]time.Time)}
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys: []VerificationKey{
			{ID: "new", Secret: []byte("new-secret")},
			{ID: "old", Secret: []byte("old-secret")},
		},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
		ReplayStore: store, ReplayTTL: time.Hour, ReplayNamespace: "tenant",
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	if _, err := verifier.VerifyAndRecord(context.Background(), message, signatures[:1], "event"); err != nil {
		t.Fatalf("first VerifyAndRecord() error = %v", err)
	}
	if _, err := verifier.VerifyAndRecord(context.Background(), message, signatures[1:], "event"); !errors.Is(err, ErrReplay) {
		t.Fatalf("rotated VerifyAndRecord() error = %v, want ErrReplay", err)
	}
}

func TestVerifyAndRecordHashesNamespacedReplayKey(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store := &recordingReplayStore{recorded: true}
	verifier, signatures, message := replayFixture(t, now, store)

	if _, err := verifier.VerifyAndRecord(context.Background(), message, signatures, "sensitive-event-id"); err != nil {
		t.Fatalf("VerifyAndRecord() error = %v", err)
	}
	if store.key == "sensitive-event-id" || len(store.key) != 64 {
		t.Fatalf("replay key = %q, want a 64-character digest", store.key)
	}
	if !store.expiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("expiresAt = %v", store.expiresAt)
	}
}

func TestVerifyAndRecordFailsClosedForMissingIDAndStoreFailure(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store := &recordingReplayStore{err: errors.New("storage offline")}
	verifier, signatures, message := replayFixture(t, now, store)

	if _, err := verifier.VerifyAndRecord(context.Background(), message, signatures, ""); !errors.Is(err, ErrMissingEventID) {
		t.Fatalf("missing event ID error = %v, want ErrMissingEventID", err)
	}
	if _, err := verifier.VerifyAndRecord(context.Background(), message, signatures, "event"); !errors.Is(err, ErrReplayStore) {
		t.Fatalf("store failure error = %v, want ErrReplayStore", err)
	}
}

func replayFixture(t *testing.T, now time.Time, store ReplayStore) (*Verifier, []Signature, Message) {
	t.Helper()

	message := Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com", Body: []byte("body")}
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys:      []SigningKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	signatures, err := signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm:       SHA256,
		Keys:            []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock:           func() time.Time { return now },
		Tolerance:       time.Minute,
		ReplayStore:     store,
		ReplayTTL:       10 * time.Minute,
		ReplayNamespace: "tenant-1",
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	return verifier, signatures, message
}

type memoryReplayStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func (s *memoryReplayStore) CheckAndRecord(_ context.Context, key string, expiresAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.seen[key]; exists {
		return false, nil
	}
	s.seen[key] = expiresAt

	return true, nil
}

type recordingReplayStore struct {
	key       string
	expiresAt time.Time
	recorded  bool
	err       error
}

func (s *recordingReplayStore) CheckAndRecord(_ context.Context, key string, expiresAt time.Time) (bool, error) {
	s.key = key
	s.expiresAt = expiresAt

	return s.recorded, s.err
}
