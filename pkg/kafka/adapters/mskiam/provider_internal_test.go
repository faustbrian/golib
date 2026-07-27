package mskiam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

var errSecretProvider = errors.New("secret provider diagnostic")

type generatorFunc func(
	context.Context,
	string,
	aws.Credentials,
) (string, int64, error)

func (generate generatorFunc) generate(
	ctx context.Context,
	region string,
	credentials aws.Credentials,
) (string, int64, error) {
	return generate(ctx, region, credentials)
}

type inertCredentialsProvider struct{}

func (inertCredentialsProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	}, nil
}

type credentialsProviderFunc func(context.Context) (aws.Credentials, error)

func (provider credentialsProviderFunc) Retrieve(
	ctx context.Context,
) (aws.Credentials, error) {
	return provider(ctx)
}

type credentialResult struct {
	credentials aws.Credentials
	err         error
}

type scriptedCredentialsProvider struct {
	results       []credentialResult
	retrievals    int
	invalidations int
}

func (provider *scriptedCredentialsProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	index := min(provider.retrievals, len(provider.results)-1)
	provider.retrievals++
	result := provider.results[index]

	return result.credentials, result.err
}

func (provider *scriptedCredentialsProvider) Invalidate() {
	provider.invalidations++
}

func testProvider(now time.Time, generate tokenGenerator) *Provider {
	return &Provider{
		region:      "eu-north-1",
		credentials: inertCredentialsProvider{},
		timeout:     minTokenTimeout,
		generator:   generate,
		now: func() time.Time {
			return now
		},
	}
}

func TestTokenRejectsInvalidSignerResults(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name      string
		token     string
		expiresAt time.Time
	}{
		{name: "empty token", expiresAt: now.Add(15 * time.Minute)},
		{
			name:      "malformed token",
			token:     "not valid!",
			expiresAt: now.Add(15 * time.Minute),
		},
		{
			name:      "oversized token",
			token:     strings.Repeat("a", maxTokenBytes+1),
			expiresAt: now.Add(15 * time.Minute),
		},
		{
			name:      "nearly expired token",
			token:     "YWJj",
			expiresAt: now.Add(minTokenValidity),
		},
		{
			name:      "unexpected lifetime",
			token:     "YWJj",
			expiresAt: now.Add(maxTokenLifetime + time.Millisecond),
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
				return test.token, test.expiresAt.UnixMilli(), nil
			}))
			_, err := provider.Token(context.Background())
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("token result: %v", err)
			}
		})
	}
}

func TestTokenRedactsFailuresAndContainsPanics(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	failureProvider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return "", 0, errSecretProvider
	}))
	_, err := failureProvider.Token(context.Background())
	if !errors.Is(err, ErrTokenGeneration) ||
		errors.Is(err, errSecretProvider) ||
		strings.Contains(err.Error(), "secret") ||
		strings.Contains(strings.TrimSpace(err.(*ProviderError).GoString()), "secret") {
		t.Fatalf("unredacted provider failure: %v", err)
	}
	if len(err.(*ProviderError).Unwrap()) != 1 {
		t.Fatal("provider failure retained an arbitrary cause")
	}

	secretCancellation := fmt.Errorf(
		"secret cancellation diagnostic: %w",
		context.Canceled,
	)
	canceledProvider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return "", 0, secretCancellation
	}))
	_, err = canceledProvider.Token(context.Background())
	if !errors.Is(err, ErrTokenGeneration) ||
		!errors.Is(err, context.Canceled) ||
		errors.Is(err, secretCancellation) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe cancellation classification: %v", err)
	}

	panicProvider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		panic("secret panic payload")
	}))
	_, err = panicProvider.Token(context.Background())
	if !errors.Is(err, ErrTokenProviderPanic) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("uncontained provider panic: %v", err)
	}
}

func TestTokenHonorsParentAndOwnedDeadlines(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	var calls atomic.Int64
	provider := testProvider(now, generatorFunc(func(
		ctx context.Context,
		_ string,
		_ aws.Credentials,
	) (string, int64, error) {
		calls.Add(1)
		<-ctx.Done()

		return "", 0, ctx.Err()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := provider.Token(ctx)
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("canceled parent result: calls=%d err=%v", calls.Load(), err)
	}

	_, err = provider.Token(context.Background())
	if !errors.Is(err, ErrTokenGeneration) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		calls.Load() != 1 {
		t.Fatalf("owned deadline result: calls=%d err=%v", calls.Load(), err)
	}
}

func TestTokenRejectsInvalidReceiverState(t *testing.T) {
	t.Parallel()

	var nilProvider *Provider
	if _, err := nilProvider.Token(context.Background()); !errors.Is(
		err,
		ErrInvalidConfig,
	) {
		t.Fatalf("nil provider: %v", err)
	}
	if _, err := (&Provider{}).Token(context.Background()); !errors.Is(
		err,
		ErrInvalidConfig,
	) {
		t.Fatalf("zero provider: %v", err)
	}
	missingCredentials := testProvider(
		time.Now(),
		generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			return "", 0, nil
		}),
	)
	missingCredentials.credentials = nil
	if _, err := missingCredentials.Token(
		context.Background(),
	); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing credentials: %v", err)
	}
	var nilContext context.Context
	if _, err := testProvider(
		time.Now(),
		generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			return "YWJj", time.Now().Add(time.Minute).UnixMilli(), nil
		}),
	).Token(nilContext); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context: %v", err)
	}
}

func TestTokenValidatesAndRefreshesCredentialResults(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	valid := aws.Credentials{
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	}
	expiring := valid
	expiring.CanExpire = true
	expiring.Expires = now.Add(10 * time.Second)
	validExpiring := valid
	validExpiring.CanExpire = true
	validExpiring.Expires = now.Add(5 * time.Minute)
	invalid := aws.Credentials{AccessKeyID: "access-key"}
	invalidExpiry := valid
	invalidExpiry.CanExpire = true
	tests := []struct {
		name     string
		provider aws.CredentialsProvider
		want     error
		hidden   error
	}{
		{
			name: "initial retrieval failure",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return aws.Credentials{}, errSecretProvider
			}),
			want:   ErrCredentialRetrieve,
			hidden: errSecretProvider,
		},
		{
			name: "invalid initial credentials",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return invalid, nil
			}),
			want: ErrInvalidCredentials,
		},
		{
			name: "missing credential expiry",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return invalidExpiry, nil
			}),
			want: ErrInvalidCredentials,
		},
		{
			name: "expiring credentials without invalidation",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return expiring, nil
			}),
			want: ErrExpiringCredentials,
		},
		{
			name: "refresh retrieval failure",
			provider: &scriptedCredentialsProvider{results: []credentialResult{
				{credentials: expiring},
				{err: errSecretProvider},
			}},
			want:   ErrCredentialRetrieve,
			hidden: errSecretProvider,
		},
		{
			name: "invalid refreshed credentials",
			provider: &scriptedCredentialsProvider{results: []credentialResult{
				{credentials: expiring},
				{credentials: invalid},
			}},
			want: ErrInvalidCredentials,
		},
		{
			name: "still expiring refreshed credentials",
			provider: &scriptedCredentialsProvider{results: []credentialResult{
				{credentials: expiring},
				{credentials: expiring},
			}},
			want: ErrExpiringCredentials,
		},
		{
			name: "valid refreshed credentials",
			provider: &scriptedCredentialsProvider{results: []credentialResult{
				{credentials: expiring},
				{credentials: validExpiring},
			}},
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
				return "YWJj", now.Add(15 * time.Minute).UnixMilli(), nil
			}))
			provider.credentials = test.provider
			token, err := provider.Token(context.Background())
			if !errors.Is(err, test.want) ||
				(test.hidden != nil && errors.Is(err, test.hidden)) ||
				(err != nil && strings.Contains(err.Error(), "secret")) {
				t.Fatalf("credential result: token=%#v err=%v", token, err)
			}
			if test.want == nil &&
				!token.ExpiresAt.Equal(validExpiring.Expires) {
				t.Fatalf("effective expiry = %s", token.ExpiresAt)
			}
		})
	}
}

func TestTokenRejectsCredentialThatExpiresDuringGeneration(t *testing.T) {
	t.Parallel()

	startedAt := time.Unix(1_700_000_000, 0)
	provider := testProvider(startedAt, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return "YWJj", startedAt.Add(15 * time.Minute).UnixMilli(), nil
	}))
	provider.credentials = credentialsProviderFunc(func(
		context.Context,
	) (aws.Credentials, error) {
		return aws.Credentials{
			AccessKeyID:     "access-key",
			SecretAccessKey: "secret-key",
			CanExpire:       true,
			Expires:         startedAt.Add(34 * time.Second),
		}, nil
	})
	nowCalls := 0
	provider.now = func() time.Time {
		nowCalls++
		if nowCalls == 1 {
			return startedAt
		}

		return startedAt.Add(5 * time.Second)
	}

	if _, err := provider.Token(context.Background()); !errors.Is(
		err,
		ErrInvalidToken,
	) {
		t.Fatalf("credential expired during generation: %v", err)
	}
}

func TestTokenReturnsOwnedValue(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	provider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return "YWJj", now.Add(15 * time.Minute).UnixMilli(), nil
	}))
	first, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	first.Token[0] = 'X'
	second, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if string(second.Token) != "YWJj" {
		t.Fatalf("token result was aliased: %q", second.Token)
	}
}

func TestProviderErrorZeroValueIsSafeAndRedacted(t *testing.T) {
	t.Parallel()

	err := new(ProviderError)
	if err.Error() != ErrTokenGeneration.Error() ||
		err.GoString() != ErrTokenGeneration.Error() ||
		len(err.Unwrap()) != 1 ||
		!errors.Is(err, ErrTokenGeneration) {
		t.Fatalf("unsafe zero provider error: %#v", err)
	}
	categoryOnly := &ProviderError{category: ErrCredentialLoad}
	if len(categoryOnly.Unwrap()) != 1 ||
		!errors.Is(categoryOnly, ErrCredentialLoad) {
		t.Fatalf("unsafe category-only provider error: %#v", categoryOnly)
	}
}
