package mskiam

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
)

type contendedRotationProvider struct {
	mutex             sync.Mutex
	initialReady      chan struct{}
	initialRelease    chan struct{}
	initialRetrievals int
	refreshRetrievals int
	refreshErr        error
	invalidations     int
	workers           int
	now               time.Time
}

type rotatingCredentialSource struct {
	mutex      sync.Mutex
	retrievals int
	now        time.Time
}

func (source *rotatingCredentialSource) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.retrievals++
	expiresAt := source.now.Add(10 * time.Second)
	accessKey := "initial-generated-access-key"
	if source.retrievals > 1 {
		expiresAt = source.now.Add(5 * time.Minute)
		accessKey = "rotated-generated-access-key"
	}

	return aws.Credentials{
		AccessKeyID:     accessKey,
		SecretAccessKey: "generated-secret-key",
		CanExpire:       true,
		Expires:         expiresAt,
	}, nil
}

func (source *rotatingCredentialSource) count() int {
	source.mutex.Lock()
	defer source.mutex.Unlock()

	return source.retrievals
}

type synchronizedCredentialCache struct {
	cache             *aws.CredentialsCache
	mutex             sync.Mutex
	initialReady      chan struct{}
	initialRelease    chan struct{}
	initialRetrievals int
	invalidations     int
	workers           int
}

func newSynchronizedCredentialCache(
	workers int,
	cache *aws.CredentialsCache,
) *synchronizedCredentialCache {
	return &synchronizedCredentialCache{
		cache:          cache,
		initialReady:   make(chan struct{}),
		initialRelease: make(chan struct{}),
		workers:        workers,
	}
}

func (cache *synchronizedCredentialCache) Retrieve(
	ctx context.Context,
) (aws.Credentials, error) {
	credentials, err := cache.cache.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, err
	}
	cache.mutex.Lock()
	if cache.initialRetrievals < cache.workers {
		cache.initialRetrievals++
		if cache.initialRetrievals == cache.workers {
			close(cache.initialReady)
		}
		cache.mutex.Unlock()
		<-cache.initialRelease

		return credentials, nil
	}
	cache.mutex.Unlock()

	return credentials, nil
}

func (cache *synchronizedCredentialCache) Invalidate() {
	cache.mutex.Lock()
	cache.invalidations++
	cache.mutex.Unlock()
	cache.cache.Invalidate()
}

func (cache *synchronizedCredentialCache) release(t *testing.T) {
	t.Helper()
	select {
	case <-cache.initialReady:
		close(cache.initialRelease)
	case <-time.After(2 * time.Second):
		close(cache.initialRelease)
		t.Fatal("shared cache callers did not retrieve the primed credential")
	}
}

func (cache *synchronizedCredentialCache) invalidationCount() int {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	return cache.invalidations
}

func newContendedRotationProvider(
	workers int,
	now time.Time,
) *contendedRotationProvider {
	return &contendedRotationProvider{
		initialReady:   make(chan struct{}),
		initialRelease: make(chan struct{}),
		workers:        workers,
		now:            now,
	}
}

func (provider *contendedRotationProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	provider.mutex.Lock()
	if provider.initialRetrievals < provider.workers {
		provider.initialRetrievals++
		if provider.initialRetrievals == provider.workers {
			close(provider.initialReady)
		}
		provider.mutex.Unlock()
		<-provider.initialRelease

		return aws.Credentials{
			AccessKeyID:     "initial-access-key",
			SecretAccessKey: "initial-secret-key",
			CanExpire:       true,
			Expires:         provider.now.Add(10 * time.Second),
		}, nil
	}
	provider.refreshRetrievals++
	refreshErr := provider.refreshErr
	provider.mutex.Unlock()
	if refreshErr != nil {
		return aws.Credentials{}, refreshErr
	}

	return aws.Credentials{
		AccessKeyID:     "rotated-access-key",
		SecretAccessKey: "rotated-secret-key",
		CanExpire:       true,
		Expires:         provider.now.Add(5 * time.Minute),
	}, nil
}

func (provider *contendedRotationProvider) Invalidate() {
	provider.mutex.Lock()
	provider.invalidations++
	provider.mutex.Unlock()
}

func (provider *contendedRotationProvider) releaseInitialRetrievals(t *testing.T) {
	t.Helper()
	select {
	case <-provider.initialReady:
		close(provider.initialRelease)
	case <-time.After(2 * time.Second):
		close(provider.initialRelease)
		t.Fatal("concurrent initial credential retrievals did not arrive")
	}
}

func (provider *contendedRotationProvider) counts() (int, int, int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()

	return provider.initialRetrievals,
		provider.refreshRetrievals,
		provider.invalidations
}

func TestConcurrentNearExpiryCredentialsRefreshOnce(t *testing.T) {
	t.Parallel()

	const workers = 16
	now := time.Unix(1_700_000_000, 0).UTC()
	credentials := newContendedRotationProvider(workers, now)
	value, signerExpiry := signedTestToken("eu-north-1", now)
	refreshGate := make(chan struct{}, 1)
	refreshGate <- struct{}{}
	provider := &Provider{
		region:      "eu-north-1",
		credentials: credentials,
		timeout:     time.Second,
		generator: generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			return value, signerExpiry.UnixMilli(), nil
		}),
		now:         func() time.Time { return now },
		refreshGate: refreshGate,
	}

	results := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			token, err := provider.Token(context.Background())
			if err == nil && !token.ExpiresAt.Equal(now.Add(5*time.Minute)) {
				err = errors.New("token expiry exceeds rotated credential lifetime")
			}
			results <- err
		}()
	}
	credentials.releaseInitialRetrievals(t)
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent credential refresh did not complete")
	}
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent token generation: %v", err)
		}
	}
	initial, refreshes, invalidations := credentials.counts()
	if initial != workers || refreshes != 1 || invalidations != 1 {
		t.Fatalf(
			"credential transitions = initial:%d refreshes:%d invalidations:%d",
			initial,
			refreshes,
			invalidations,
		)
	}
}

func TestConcurrentFailedCredentialRefreshIsSharedOnce(t *testing.T) {
	t.Parallel()

	const workers = 16
	now := time.Unix(1_700_000_000, 0).UTC()
	canary := generatedFailureCanary(t)
	credentials := newContendedRotationProvider(workers, now)
	credentials.refreshErr = &smithy.GenericAPIError{
		Code:    "ThrottlingException",
		Message: canary,
	}
	value, signerExpiry := signedTestToken("eu-north-1", now)
	refreshGate := make(chan struct{}, 1)
	refreshGate <- struct{}{}
	provider := &Provider{
		region:      "eu-north-1",
		credentials: credentials,
		timeout:     time.Second,
		generator: generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			return value, signerExpiry.UnixMilli(), nil
		}),
		now:         func() time.Time { return now },
		refreshGate: refreshGate,
	}

	results := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			_, err := provider.Token(context.Background())
			results <- err
		}()
	}
	credentials.releaseInitialRetrievals(t)
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("failed credential refresh cohort did not complete")
	}
	close(results)
	for err := range results {
		if !errors.Is(err, ErrCredentialRetrieve) {
			t.Fatalf("failed refresh category = %v", err)
		}
		assertFailureIsRedacted(t, err, canary)
	}
	initial, refreshes, invalidations := credentials.counts()
	if initial != workers || refreshes != 1 || invalidations != 1 {
		t.Fatalf(
			"failed refresh transitions = initial:%d refreshes:%d invalidations:%d",
			initial,
			refreshes,
			invalidations,
		)
	}
}

func TestConcurrentAWSSharedCacheRefreshesUnderlyingProviderOnce(t *testing.T) {
	t.Parallel()

	const workers = 16
	now := time.Now().UTC().Truncate(time.Second)
	source := &rotatingCredentialSource{now: now}
	credentials := newSynchronizedCredentialCache(
		workers,
		aws.NewCredentialsCache(source),
	)
	value, signerExpiry := signedTestToken("eu-north-1", now)
	refreshGate := make(chan struct{}, 1)
	refreshGate <- struct{}{}
	provider := &Provider{
		region:      "eu-north-1",
		credentials: credentials,
		timeout:     time.Second,
		generator: generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			return value, signerExpiry.UnixMilli(), nil
		}),
		now:         func() time.Time { return now },
		refreshGate: refreshGate,
	}

	results := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			_, err := provider.Token(context.Background())
			results <- err
		}()
	}
	credentials.release(t)
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shared cache refresh did not complete")
	}
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("shared-cache token generation: %v", err)
		}
	}
	if source.count() != 2 || credentials.invalidationCount() != 1 {
		t.Fatalf(
			"shared cache transitions = source retrievals:%d invalidations:%d",
			source.count(),
			credentials.invalidationCount(),
		)
	}
}

func TestTokenRejectsSignerClockSkewBeyondTolerance(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	value, expiresAt := signedTestToken(
		"eu-north-1",
		now.Add(-5*time.Minute-time.Second),
	)
	provider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return value, expiresAt.UnixMilli(), nil
	}))

	if _, err := provider.Token(context.Background()); !errors.Is(err, ErrMalformedToken) {
		t.Fatalf("clock-skewed token error = %v, want %v", err, ErrMalformedToken)
	}
}

func TestSignedTokenClockSkewBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, int64(500*time.Millisecond)).UTC()
	signerSecond := now.Truncate(time.Second)
	for _, skew := range []time.Duration{-5 * time.Minute, 5 * time.Minute} {
		value, expiresAt := signedTestToken("eu-north-1", signerSecond.Add(skew))
		if !validToken(value, "eu-north-1", expiresAt.UnixMilli(), now) {
			t.Fatalf("validToken() rejected exact clock skew %s", skew)
		}
	}
	value, expiresAt := signedTestToken(
		"eu-north-1",
		signerSecond.Add(5*time.Minute+time.Second),
	)
	if validToken(value, "eu-north-1", expiresAt.UnixMilli(), now) {
		t.Fatal("validToken() accepted future clock skew beyond tolerance")
	}
}

func generatedFailureCanary(t *testing.T) string {
	t.Helper()
	digest := sha256.Sum256([]byte(t.Name()))

	return fmt.Sprintf("credential-canary-%x", digest[:])
}

func assertFailureIsRedacted(t *testing.T, err error, canary string) {
	t.Helper()
	for _, formatted := range []string{
		err.Error(),
		fmt.Sprintf("%v", err),
		fmt.Sprintf("%+v", err),
		fmt.Sprintf("%#v", err),
	} {
		if strings.Contains(formatted, canary) {
			t.Fatal("credential canary leaked through failure formatting")
		}
	}
}

func TestAWSFailureCategoriesNeverDiscloseGeneratedCanaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	canary := generatedFailureCanary(t)
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "throttling",
			err: &smithy.GenericAPIError{
				Code:    "ThrottlingException",
				Message: canary,
			},
		},
		{
			name: "access denial",
			err: &smithy.GenericAPIError{
				Code:    "AccessDeniedException",
				Message: canary,
			},
		},
		{
			name: "endpoint failure",
			err: &url.Error{
				Op:  "GET",
				URL: "https://" + canary + ".invalid",
				Err: errors.New(canary),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := testProvider(now, generatorFunc(func(
				context.Context,
				string,
				aws.Credentials,
			) (string, int64, error) {
				t.Fatal("signer called after credential failure")

				return "", 0, nil
			}))
			provider.credentials = credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return aws.Credentials{}, test.err
			})
			_, err := provider.Token(context.Background())
			if !errors.Is(err, ErrCredentialRetrieve) {
				t.Fatalf("credential failure category = %v", err)
			}
			assertFailureIsRedacted(t, err, canary)
		})
	}
}

type panicCredentialProvider struct {
	credentials   aws.Credentials
	panicValue    string
	panicRetrieve bool
}

func (provider panicCredentialProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	if provider.panicRetrieve {
		panic(provider.panicValue)
	}

	return provider.credentials, nil
}

func (provider panicCredentialProvider) Invalidate() {
	panic(provider.panicValue)
}

func TestCredentialProviderPanicsAreContainedAndRedacted(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	canary := generatedFailureCanary(t)
	tests := []struct {
		name        string
		credentials panicCredentialProvider
	}{
		{
			name: "retrieval panic",
			credentials: panicCredentialProvider{
				panicValue:    canary,
				panicRetrieve: true,
			},
		},
		{
			name: "invalidation panic",
			credentials: panicCredentialProvider{
				panicValue: canary,
				credentials: aws.Credentials{
					AccessKeyID:     "generated-access-key",
					SecretAccessKey: "generated-secret-key",
					CanExpire:       true,
					Expires:         now.Add(time.Second),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			provider := testProvider(now, generatorFunc(func(
				context.Context,
				string,
				aws.Credentials,
			) (string, int64, error) {
				t.Fatal("signer called after credential provider panic")

				return "", 0, nil
			}))
			provider.credentials = test.credentials
			_, err := provider.Token(context.Background())
			if !errors.Is(err, ErrTokenProviderPanic) {
				t.Fatalf("credential panic category = %v", err)
			}
			assertFailureIsRedacted(t, err, canary)
		})
	}
}

type signalingExpiringProvider struct {
	ready chan struct{}
	once  sync.Once
	now   time.Time
}

type refreshCallerKey struct{}

type cancelingRefreshProvider struct {
	mutex             sync.Mutex
	initialReady      chan struct{}
	initialRelease    chan struct{}
	refreshLeader     chan int
	initialRetrievals int
	refreshes         int
	now               time.Time
}

func newCancelingRefreshProvider(now time.Time) *cancelingRefreshProvider {
	return &cancelingRefreshProvider{
		initialReady:   make(chan struct{}),
		initialRelease: make(chan struct{}),
		refreshLeader:  make(chan int, 1),
		now:            now,
	}
}

func (provider *cancelingRefreshProvider) Retrieve(
	ctx context.Context,
) (aws.Credentials, error) {
	provider.mutex.Lock()
	if provider.initialRetrievals < 2 {
		provider.initialRetrievals++
		if provider.initialRetrievals == 2 {
			close(provider.initialReady)
		}
		provider.mutex.Unlock()
		<-provider.initialRelease

		return aws.Credentials{
			AccessKeyID:     "initial-generated-access-key",
			SecretAccessKey: "generated-secret-key",
			CanExpire:       true,
			Expires:         provider.now.Add(time.Second),
		}, nil
	}
	provider.refreshes++
	refreshNumber := provider.refreshes
	provider.mutex.Unlock()
	if refreshNumber == 1 {
		provider.refreshLeader <- ctx.Value(refreshCallerKey{}).(int)
		<-ctx.Done()

		return aws.Credentials{}, ctx.Err()
	}

	return aws.Credentials{
		AccessKeyID:     "rotated-generated-access-key",
		SecretAccessKey: "generated-secret-key",
		CanExpire:       true,
		Expires:         provider.now.Add(5 * time.Minute),
	}, nil
}

func (provider *cancelingRefreshProvider) Invalidate() {}

func (provider *cancelingRefreshProvider) release(t *testing.T) {
	t.Helper()
	select {
	case <-provider.initialReady:
		close(provider.initialRelease)
	case <-time.After(2 * time.Second):
		close(provider.initialRelease)
		t.Fatal("canceling refresh cohort did not retrieve initial credentials")
	}
}

func (provider *signalingExpiringProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	provider.once.Do(func() { close(provider.ready) })

	return aws.Credentials{
		AccessKeyID:     "generated-access-key",
		SecretAccessKey: "generated-secret-key",
		CanExpire:       true,
		Expires:         provider.now.Add(time.Second),
	}, nil
}

func (provider *signalingExpiringProvider) Invalidate() {}

func TestCredentialRefreshWaitHonorsCancellation(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	credentials := &signalingExpiringProvider{
		ready: make(chan struct{}),
		now:   now,
	}
	provider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		t.Fatal("signer called while credential refresh is blocked")

		return "", 0, nil
	}))
	provider.credentials = credentials
	<-provider.refreshGate
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := provider.Token(ctx)
		result <- err
	}()
	select {
	case <-credentials.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("credential retrieval did not reach refresh wait")
	}
	cancel()
	var err error
	select {
	case err = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("credential refresh did not observe cancellation")
	}
	if !errors.Is(err, ErrTokenCanceled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked refresh cancellation = %v", err)
	}
}

func TestCallerCancellationIsNotSharedWithRefreshCohort(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	credentials := newCancelingRefreshProvider(now)
	value, expiresAt := signedTestToken("eu-north-1", now)
	provider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return value, expiresAt.UnixMilli(), nil
	}))
	provider.timeout = time.Second
	provider.credentials = credentials
	results := make(chan error, 2)
	cancels := make([]context.CancelFunc, 2)
	for caller := range 2 {
		base := context.WithValue(context.Background(), refreshCallerKey{}, caller)
		ctx, cancel := context.WithCancel(base)
		defer cancel()
		cancels[caller] = cancel
		go func() {
			_, err := provider.Token(ctx)
			results <- err
		}()
	}
	credentials.release(t)
	var leader int
	select {
	case leader = <-credentials.refreshLeader:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh cohort did not elect a leader")
	}
	cancels[leader]()
	var successes int
	var cancellations int
	for range 2 {
		select {
		case err := <-results:
			switch {
			case err == nil:
				successes++
			case errors.Is(err, ErrTokenCanceled):
				cancellations++
			default:
				t.Fatalf("refresh cohort result = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("refresh cohort did not complete after leader cancellation")
		}
	}
	if successes != 1 || cancellations != 1 {
		t.Fatalf(
			"refresh cohort outcomes = successes:%d cancellations:%d",
			successes,
			cancellations,
		)
	}
}
