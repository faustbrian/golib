package httpsignature

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVerifierLifecycleKeyRotationAndRevocation(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	oldKey := lifecycleHMACKey(t, "old-key-material-0123456789abcdef")
	newKey := lifecycleHMACKey(t, "new-key-material-0123456789abcdef")
	resolver := &lifecycleAtomicResolver{}
	resolver.Store(lifecycleResolvedKey(now, oldKey))
	verifier := NewVerifier(lifecycleVerificationProfile(t, now, resolver, nil))
	oldMessage := lifecycleSignedMessage(t, now, "rotating-key", "", oldKey)
	newMessage := lifecycleSignedMessage(t, now, "rotating-key", "", newKey)

	if _, err := verifier.Verify(context.Background(), oldMessage.message, "sig", oldMessage.inputs, oldMessage.signatures); err != nil {
		t.Fatalf("Verify(old key before rotation) error = %v", err)
	}

	resolver.Store(lifecycleResolvedKey(now, newKey))
	if _, err := verifier.Verify(context.Background(), oldMessage.message, "sig", oldMessage.inputs, oldMessage.signatures); !lifecycleVerificationFailureIs(err, VerificationCryptographic) {
		t.Fatalf("Verify(old key after rotation) error = %v, want cryptographic failure", err)
	}
	if _, err := verifier.Verify(context.Background(), newMessage.message, "sig", newMessage.inputs, newMessage.signatures); err != nil {
		t.Fatalf("Verify(new key after rotation) error = %v", err)
	}

	revoked := lifecycleResolvedKey(now, newKey)
	revoked.Revoked = true
	resolver.Store(revoked)
	if _, err := verifier.Verify(context.Background(), newMessage.message, "sig", newMessage.inputs, newMessage.signatures); !lifecycleVerificationFailureIs(err, VerificationKey) {
		t.Fatalf("Verify(revoked key) error = %v, want key failure", err)
	}
}

func TestVerifierLifecycleResolverCacheRefreshRace(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	staleKey := lifecycleHMACKey(t, "stale-key-material-0123456789abcde")
	freshKey := lifecycleHMACKey(t, "fresh-key-material-0123456789abcde")
	stale := lifecycleResolvedKey(now, staleKey)
	stale.FreshUntil = now
	fresh := lifecycleResolvedKey(now, freshKey)
	staleStarted := make(chan struct{})
	releaseStale := make(chan struct{})
	resolver := &lifecycleRefreshResolver{firstStarted: staleStarted, releaseFirst: releaseStale}
	resolver.Store(stale)
	verifier := NewVerifier(lifecycleVerificationProfile(t, now, resolver, nil))
	staleMessage := lifecycleSignedMessage(t, now, "rotating-key", "", staleKey)
	freshMessage := lifecycleSignedMessage(t, now, "rotating-key", "", freshKey)
	staleResult := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), staleMessage.message, "sig", staleMessage.inputs, staleMessage.signatures)
		staleResult <- err
	}()

	lifecycleAwaitSignal(t, staleStarted)
	resolver.Store(fresh)
	if _, err := verifier.Verify(context.Background(), freshMessage.message, "sig", freshMessage.inputs, freshMessage.signatures); err != nil {
		close(releaseStale)
		t.Fatalf("Verify(fresh cache snapshot) error = %v", err)
	}
	close(releaseStale)
	if err := lifecycleAwaitError(t, staleResult); !lifecycleVerificationFailureIs(err, VerificationKey) {
		t.Fatalf("Verify(stale cache snapshot) error = %v, want key failure", err)
	}
}

func TestVerifierLifecycleResolverOutageAndUnknownResultFailClosed(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key := lifecycleHMACKey(t, "resolver-key-material-0123456789abc")
	message := lifecycleSignedMessage(t, now, "rotating-key", "", key)
	backendErr := errors.New("resolver backend secret diagnostic")

	for _, test := range []struct {
		name     string
		resolver KeyResolver
	}{
		{
			name: "outage",
			resolver: lifecycleResolverFunc(func(context.Context, string) (ResolvedKey, error) {
				return ResolvedKey{}, backendErr
			}),
		},
		{
			name: "key returned with unknown backend outcome",
			resolver: lifecycleResolverFunc(func(context.Context, string) (ResolvedKey, error) {
				return lifecycleResolvedKey(now, key), backendErr
			}),
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			verifier := NewVerifier(lifecycleVerificationProfile(t, now, test.resolver, nil))
			_, err := verifier.Verify(context.Background(), message.message, "sig", message.inputs, message.signatures)
			if !lifecycleVerificationFailureIs(err, VerificationKeyResolution) || !errors.Is(err, ErrKeyResolutionFailure) {
				t.Fatalf("Verify() error = %v, want sanitized key-resolution failure", err)
			}
			if strings.Contains(err.Error(), "secret") || errors.Is(err, backendErr) {
				t.Fatalf("Verify() exposed resolver diagnostic: %v", err)
			}
		})
	}
}

func TestVerifierLifecycleReplayOutageAndUnknownResultFailClosed(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key := lifecycleHMACKey(t, "replay-key-material-0123456789abcde")
	message := lifecycleSignedMessage(t, now, "replay-key", "nonce", key)
	resolver := lifecycleResolverFunc(func(context.Context, string) (ResolvedKey, error) {
		return lifecycleResolvedKey(now, key), nil
	})
	backendErr := errors.New("replay backend secret diagnostic")

	t.Run("outage", func(t *testing.T) {
		t.Parallel()

		replay := lifecycleReplayStoreFunc(func(context.Context, ReplayRecord) error { return backendErr })
		verifier := NewVerifier(lifecycleVerificationProfile(t, now, resolver, replay))
		_, err := verifier.Verify(context.Background(), message.message, "sig", message.inputs, message.signatures)
		assertLifecycleReplayBackendFailure(t, err, backendErr)
	})

	t.Run("commit unknown outcome", func(t *testing.T) {
		t.Parallel()

		committed, err := NewMemoryReplayStore(MemoryReplayConfig{
			Capacity: 1, MaxTTL: time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64,
			Now: func() time.Time { return now },
		})
		if err != nil {
			t.Fatalf("NewMemoryReplayStore() error = %v", err)
		}
		var returnedUnknown atomic.Bool
		replay := lifecycleReplayStoreFunc(func(ctx context.Context, record ReplayRecord) error {
			consumeErr := committed.Consume(ctx, record)
			if consumeErr == nil && returnedUnknown.CompareAndSwap(false, true) {
				return backendErr
			}
			return consumeErr
		})
		verifier := NewVerifier(lifecycleVerificationProfile(t, now, resolver, replay))

		_, firstErr := verifier.Verify(context.Background(), message.message, "sig", message.inputs, message.signatures)
		assertLifecycleReplayBackendFailure(t, firstErr, backendErr)
		_, retryErr := verifier.Verify(context.Background(), message.message, "sig", message.inputs, message.signatures)
		if !lifecycleVerificationFailureIs(retryErr, VerificationReplay) || !errors.Is(retryErr, ErrReplayDetected) {
			t.Fatalf("Verify(retry after unknown commit) error = %v, want replay detection", retryErr)
		}
	})
}

func TestVerifierLifecycleShutdownCancellationStopsResolverAndReplay(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key := lifecycleHMACKey(t, "shutdown-key-material-0123456789abc")
	resolverEntered := make(chan struct{})
	replayEntered := make(chan struct{})
	blockingResolver := lifecycleResolverFunc(func(ctx context.Context, _ string) (ResolvedKey, error) {
		close(resolverEntered)
		<-ctx.Done()
		return ResolvedKey{}, ctx.Err()
	})
	blockingReplay := lifecycleReplayStoreFunc(func(ctx context.Context, _ ReplayRecord) error {
		close(replayEntered)
		<-ctx.Done()
		return ctx.Err()
	})
	readyResolver := lifecycleResolverFunc(func(context.Context, string) (ResolvedKey, error) {
		return lifecycleResolvedKey(now, key), nil
	})
	resolverVerifier := NewVerifier(lifecycleVerificationProfile(t, now, blockingResolver, nil))
	replayVerifier := NewVerifier(lifecycleVerificationProfile(t, now, readyResolver, blockingReplay))
	resolverMessage := lifecycleSignedMessage(t, now, "shutdown-key", "", key)
	replayMessage := lifecycleSignedMessage(t, now, "shutdown-key", "nonce", key)
	shutdown, cancelShutdown := context.WithCancel(context.Background())
	resolverResult := make(chan error, 1)
	replayResult := make(chan error, 1)
	go func() {
		_, err := resolverVerifier.Verify(shutdown, resolverMessage.message, "sig", resolverMessage.inputs, resolverMessage.signatures)
		resolverResult <- err
	}()
	go func() {
		_, err := replayVerifier.Verify(shutdown, replayMessage.message, "sig", replayMessage.inputs, replayMessage.signatures)
		replayResult <- err
	}()

	lifecycleAwaitSignal(t, resolverEntered)
	lifecycleAwaitSignal(t, replayEntered)
	cancelShutdown()
	if err := lifecycleAwaitError(t, resolverResult); !lifecycleVerificationFailureIs(err, VerificationKeyResolution) || !errors.Is(err, context.Canceled) {
		t.Fatalf("resolver Verify() error = %v, want shutdown cancellation", err)
	}
	if err := lifecycleAwaitError(t, replayResult); !lifecycleVerificationFailureIs(err, VerificationReplay) || !errors.Is(err, context.Canceled) {
		t.Fatalf("replay Verify() error = %v, want shutdown cancellation", err)
	}
}

type lifecycleSignedVerification struct {
	message    MessageContext
	inputs     SignatureInputs
	signatures Signatures
}

func lifecycleSignedMessage(t *testing.T, now time.Time, keyID, nonce string, key HMACKey) lifecycleSignedVerification {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, "https://example.com/lifecycle", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	parameters := []Parameter{
		{Name: "created", Value: now.Unix()},
		{Name: "keyid", Value: keyID},
		{Name: "alg", Value: string(HMACSHA256)},
	}
	if nonce != "" {
		parameters = append(parameters, Parameter{Name: "nonce", Value: nonce})
	}
	input := SignatureInput{
		Label:      "sig",
		Components: []ComponentIdentifier{{Name: "@method"}},
		Parameters: parameters,
	}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	value, err := Sign(context.Background(), HMACSHA256, key, []byte(base), nil)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	return lifecycleSignedVerification{
		message:    MessageContext{Request: request},
		inputs:     SignatureInputs{entries: []SignatureInput{input}},
		signatures: Signatures{entries: []SignatureValue{{Label: "sig", Value: value}}},
	}
}

func lifecycleVerificationProfile(
	t *testing.T,
	now time.Time,
	resolver KeyResolver,
	replay ReplayStore,
) *VerificationProfile {
	t.Helper()

	noncePolicy := ParameterForbidden
	replayTimeout := time.Duration(0)
	if replay != nil {
		noncePolicy = ParameterRequired
		replayTimeout = time.Second
	}
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created:            ParameterRequired,
		Expires:            ParameterForbidden,
		AlgorithmParameter: ParameterRequired,
		Nonce:              noncePolicy,
		Tag:                ParameterForbidden,
		MaxAge:             time.Minute,
		ResolveTimeout:     time.Second,
		ReplayTimeout:      replayTimeout,
		Now:                func() time.Time { return now },
		Resolver:           resolver,
		Replay:             replay,
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	return profile
}

func lifecycleResolvedKey(now time.Time, key HMACKey) ResolvedKey {
	return ResolvedKey{
		Algorithm:  HMACSHA256,
		Key:        key,
		NotBefore:  now.Add(-time.Minute),
		NotAfter:   now.Add(time.Minute),
		FreshUntil: now.Add(time.Minute),
	}
}

func lifecycleHMACKey(t *testing.T, material string) HMACKey {
	t.Helper()

	key, err := NewHMACKey([]byte(material))
	if err != nil {
		t.Fatalf("NewHMACKey() error = %v", err)
	}
	return key
}

type lifecycleResolvedSnapshot struct {
	key ResolvedKey
}

type lifecycleAtomicResolver struct {
	current atomic.Pointer[lifecycleResolvedSnapshot]
}

func (resolver *lifecycleAtomicResolver) Store(key ResolvedKey) {
	resolver.current.Store(&lifecycleResolvedSnapshot{key: key})
}

func (resolver *lifecycleAtomicResolver) Resolve(ctx context.Context, keyID string) (ResolvedKey, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedKey{}, err
	}
	if keyID != "rotating-key" {
		return ResolvedKey{}, ErrKeyNotFound
	}
	snapshot := resolver.current.Load()
	if snapshot == nil {
		return ResolvedKey{}, ErrKeyNotFound
	}
	return snapshot.key, nil
}

type lifecycleRefreshResolver struct {
	current      atomic.Pointer[lifecycleResolvedSnapshot]
	first        atomic.Bool
	firstStarted chan<- struct{}
	releaseFirst <-chan struct{}
}

func (resolver *lifecycleRefreshResolver) Store(key ResolvedKey) {
	resolver.current.Store(&lifecycleResolvedSnapshot{key: key})
}

func (resolver *lifecycleRefreshResolver) Resolve(ctx context.Context, keyID string) (ResolvedKey, error) {
	if keyID != "rotating-key" {
		return ResolvedKey{}, ErrKeyNotFound
	}
	snapshot := resolver.current.Load()
	if snapshot == nil {
		return ResolvedKey{}, ErrKeyNotFound
	}
	if resolver.first.CompareAndSwap(false, true) {
		close(resolver.firstStarted)
		select {
		case <-resolver.releaseFirst:
		case <-ctx.Done():
			return ResolvedKey{}, ctx.Err()
		}
	}
	return snapshot.key, nil
}

type lifecycleResolverFunc func(context.Context, string) (ResolvedKey, error)

func (resolve lifecycleResolverFunc) Resolve(ctx context.Context, keyID string) (ResolvedKey, error) {
	return resolve(ctx, keyID)
}

type lifecycleReplayStoreFunc func(context.Context, ReplayRecord) error

func (consume lifecycleReplayStoreFunc) Consume(ctx context.Context, record ReplayRecord) error {
	return consume(ctx, record)
}

func assertLifecycleReplayBackendFailure(t *testing.T, err, backendErr error) {
	t.Helper()

	if !lifecycleVerificationFailureIs(err, VerificationReplay) || !errors.Is(err, ErrReplayBackendFailure) {
		t.Fatalf("Verify() error = %v, want sanitized replay-backend failure", err)
	}
	if strings.Contains(err.Error(), "secret") || errors.Is(err, backendErr) {
		t.Fatalf("Verify() exposed replay diagnostic: %v", err)
	}
}

func lifecycleAwaitError(t *testing.T, result <-chan error) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("verification did not stop after cancellation")
		return nil
	}
}

func lifecycleAwaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("verification did not reach the expected lifecycle boundary")
	}
}

func lifecycleVerificationFailureIs(err error, want VerificationFailure) bool {
	var verificationError *VerificationError
	return errors.As(err, &verificationError) && verificationError.Failure == want
}
