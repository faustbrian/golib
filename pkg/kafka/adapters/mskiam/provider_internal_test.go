package mskiam

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
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
	afterRetrieve func(int)
}

func (provider *scriptedCredentialsProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	index := min(provider.retrievals, len(provider.results)-1)
	provider.retrievals++
	if provider.afterRetrieve != nil {
		provider.afterRetrieve(provider.retrievals)
	}
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

func signedTestToken(region string, signedAt time.Time) (string, time.Time) {
	expiresAt := signedAt.Add(15 * time.Minute)
	query := url.Values{
		"Action":              {"kafka-cluster:Connect"},
		"User-Agent":          {"aws-msk-iam-sasl-signer-go/test"},
		"X-Amz-Algorithm":     {"AWS4-HMAC-SHA256"},
		"X-Amz-Credential":    {"access-key/" + signedAt.UTC().Format("20060102") + "/" + region + "/kafka-cluster/aws4_request"},
		"X-Amz-Date":          {signedAt.UTC().Format("20060102T150405Z")},
		"X-Amz-Expires":       {strconv.Itoa(15 * 60)},
		"X-Amz-Signature":     {strings.Repeat("a", 64)},
		"X-Amz-SignedHeaders": {"host"},
	}
	signedURL := (&url.URL{
		Scheme:   "https",
		Host:     "kafka." + region + ".amazonaws.com",
		Path:     "/",
		RawQuery: query.Encode(),
	}).String()

	return base64.RawURLEncoding.EncodeToString([]byte(signedURL)), expiresAt
}

func TestTokenRejectsInvalidSignerResults(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	valid, validExpiry := signedTestToken("eu-north-1", now)
	tests := []struct {
		name      string
		token     string
		expiresAt time.Time
		wantError string
	}{
		{name: "empty token", expiresAt: validExpiry, wantError: "kafka/mskiam: signer returned malformed output"},
		{
			name:      "malformed token",
			token:     "not valid!",
			expiresAt: validExpiry,
			wantError: "kafka/mskiam: signer returned malformed output",
		},
		{
			name:      "oversized token",
			token:     strings.Repeat("a", maxTokenBytes+1),
			expiresAt: validExpiry,
			wantError: "kafka/mskiam: signer returned malformed output",
		},
		{
			name:      "nearly expired token",
			token:     valid,
			expiresAt: now.Add(minTokenValidity),
			wantError: "kafka/mskiam: token is expired or expires too soon",
		},
		{
			name:      "unexpected lifetime",
			token:     valid,
			expiresAt: now.Add(maxTokenLifetime + time.Millisecond),
			wantError: "kafka/mskiam: signer returned malformed output",
		},
		{name: "decoded non-URL", token: "YWJj", expiresAt: validExpiry, wantError: "kafka/mskiam: signer returned malformed output"},
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
			if err == nil || err.Error() != test.wantError ||
				!errors.Is(err, ErrInvalidToken) {
				t.Fatalf("token result: %v", err)
			}
		})
	}
}

func TestTokenRejectsMalformedSignedURLShape(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	valid, expiresAt := signedTestToken("eu-north-1", now)
	decoded, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	parsed, err := url.Parse(string(decoded))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	tests := map[string]func(*url.URL){
		"plaintext scheme": func(value *url.URL) { value.Scheme = "http" },
		"wrong host":       func(value *url.URL) { value.Host = "example.com" },
		"wrong path":       func(value *url.URL) { value.Path = "/connect" },
		"fragment":         func(value *url.URL) { value.Fragment = "secret" },
		"wrong action": func(value *url.URL) {
			query := value.Query()
			query.Set("Action", "kafka-cluster:WriteData")
			value.RawQuery = query.Encode()
		},
		"missing signature": func(value *url.URL) {
			query := value.Query()
			query.Del("X-Amz-Signature")
			value.RawQuery = query.Encode()
		},
		"short signature": func(value *url.URL) {
			query := value.Query()
			query.Set("X-Amz-Signature", "a")
			value.RawQuery = query.Encode()
		},
		"non-hex signature": func(value *url.URL) {
			query := value.Query()
			query.Set("X-Amz-Signature", strings.Repeat("g", 64))
			value.RawQuery = query.Encode()
		},
		"credential date mismatch": func(value *url.URL) {
			query := value.Query()
			query.Set(
				"X-Amz-Credential",
				"access-key/20231113/eu-north-1/kafka-cluster/aws4_request",
			)
			value.RawQuery = query.Encode()
		},
		"unexpected query parameter": func(value *url.URL) {
			query := value.Query()
			query.Set("unexpected", "value")
			value.RawQuery = query.Encode()
		},
		"empty security token": func(value *url.URL) {
			query := value.Query()
			query.Set("X-Amz-Security-Token", "")
			value.RawQuery = query.Encode()
		},
		"duplicate credential scope": func(value *url.URL) {
			query := value.Query()
			query.Add("X-Amz-Credential", query.Get("X-Amz-Credential"))
			value.RawQuery = query.Encode()
		},
		"zero query expiry": func(value *url.URL) {
			query := value.Query()
			query.Set("X-Amz-Expires", "0")
			value.RawQuery = query.Encode()
		},
		"excessive query expiry": func(value *url.URL) {
			query := value.Query()
			query.Set("X-Amz-Expires", "1201")
			value.RawQuery = query.Encode()
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			clone := *parsed
			mutate(&clone)
			token := base64.RawURLEncoding.EncodeToString([]byte(clone.String()))
			provider := testProvider(now, generatorFunc(func(
				context.Context,
				string,
				aws.Credentials,
			) (string, int64, error) {
				return token, expiresAt.UnixMilli(), nil
			}))
			if _, tokenErr := provider.Token(context.Background()); tokenErr == nil ||
				tokenErr.Error() != "kafka/mskiam: signer returned malformed output" {
				t.Fatalf("malformed signed URL: %v", tokenErr)
			}
		})
	}
}

func TestSignedTokenValidationRejectsEachMalformedField(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	valid, expiresAt := signedTestToken("eu-north-1", now)
	decoded, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	parsed, err := url.Parse(string(decoded))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	type malformedCase struct {
		name      string
		mutate    func(*url.URL)
		expiresAt time.Time
	}
	queryMutation := func(mutate func(url.Values)) func(*url.URL) {
		return func(value *url.URL) {
			query := value.Query()
			mutate(query)
			value.RawQuery = query.Encode()
		}
	}
	cases := []malformedCase{
		{name: "userinfo", expiresAt: expiresAt, mutate: func(value *url.URL) {
			value.User = url.User("caller")
		}},
		{name: "invalid raw query", expiresAt: expiresAt, mutate: func(value *url.URL) {
			value.RawQuery += ";invalid"
		}},
		{name: "missing action", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Del("Action")
			query.Set("X-Amz-Security-Token", "session-token")
		})},
		{name: "duplicate action", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Add("Action", "kafka-cluster:Connect")
		})},
		{name: "wrong algorithm", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Algorithm", "AWS4-ECDSA-P256-SHA256")
		})},
		{name: "wrong signed headers", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-SignedHeaders", "host;x-extra")
		})},
		{name: "missing user agent", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Del("User-Agent")
		})},
		{name: "duplicate user agent", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Add("User-Agent", query.Get("User-Agent"))
		})},
		{name: "wrong user agent", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("User-Agent", "different-signer/test")
		})},
		{name: "duplicate signature", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Add("X-Amz-Signature", query.Get("X-Amz-Signature"))
		})},
		{name: "signature before hex range", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Signature", strings.Repeat("/", 64))
		})},
		{name: "signature between ranges", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Signature", strings.Repeat(":", 64))
		})},
		{name: "signature before lowercase hex", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Signature", strings.Repeat("`", 64))
		})},
		{name: "malformed date", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Date", "invalid")
		})},
		{name: "duplicate date", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Add("X-Amz-Date", query.Get("X-Amz-Date"))
		})},
		{name: "malformed expiry", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Expires", "invalid")
		})},
		{name: "duplicate expiry", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Add("X-Amz-Expires", query.Get("X-Amz-Expires"))
		})},
		{name: "zero expiry", expiresAt: now, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Expires", "0")
		})},
		{name: "excessive expiry", expiresAt: now.Add(1201 * time.Second), mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Expires", "1201")
		})},
		{name: "duplicate security token", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query["X-Amz-Security-Token"] = []string{"one", "two"}
		})},
		{name: "too many query keys", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Security-Token", "session")
			query.Set("extra", "value")
		})},
		{name: "empty access key", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Credential", "/20231114/eu-north-1/kafka-cluster/aws4_request")
		})},
		{name: "oversized access key", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Credential", strings.Repeat("a", 129)+"/20231114/eu-north-1/kafka-cluster/aws4_request")
		})},
		{name: "short credential scope", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Credential", "access-key/20231114/eu-north-1/kafka-cluster")
		})},
		{name: "wrong credential region", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Credential", "access-key/20231114/us-east-1/kafka-cluster/aws4_request")
		})},
		{name: "wrong credential service", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Credential", "access-key/20231114/eu-north-1/kafka/aws4_request")
		})},
		{name: "wrong credential terminator", expiresAt: expiresAt, mutate: queryMutation(func(query url.Values) {
			query.Set("X-Amz-Credential", "access-key/20231114/eu-north-1/kafka-cluster/request")
		})},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			clone := *parsed
			test.mutate(&clone)
			token := base64.RawURLEncoding.EncodeToString([]byte(clone.String()))
			if validToken(token, "eu-north-1", test.expiresAt.UnixMilli()) {
				t.Fatal("validToken() accepted malformed signer output")
			}
		})
	}
	if !validToken(valid, "eu-north-1", expiresAt.UnixMilli()) {
		t.Fatal("validToken() rejected canonical signer output")
	}
	maximumExpiry := *parsed
	query := maximumExpiry.Query()
	query.Set("X-Amz-Expires", "1200")
	maximumExpiry.RawQuery = query.Encode()
	maximumExpiryToken := base64.RawURLEncoding.EncodeToString(
		[]byte(maximumExpiry.String()),
	)
	if !validToken(
		maximumExpiryToken,
		"eu-north-1",
		now.Add(maxTokenLifetime).UnixMilli(),
	) {
		t.Fatal("validToken() rejected the maximum token lifetime")
	}
	maximumAccessKey := *parsed
	query = maximumAccessKey.Query()
	query.Set(
		"X-Amz-Credential",
		strings.Repeat("a", 128)+
			"/20231114/eu-north-1/kafka-cluster/aws4_request",
	)
	maximumAccessKey.RawQuery = query.Encode()
	maximumAccessKeyToken := base64.RawURLEncoding.EncodeToString(
		[]byte(maximumAccessKey.String()),
	)
	if !validToken(
		maximumAccessKeyToken,
		"eu-north-1",
		expiresAt.UnixMilli(),
	) {
		t.Fatal("validToken() rejected the maximum access-key length")
	}
	withSecurityToken := *parsed
	query = withSecurityToken.Query()
	query.Set("X-Amz-Security-Token", "session-token")
	withSecurityToken.RawQuery = query.Encode()
	securityToken := base64.RawURLEncoding.EncodeToString(
		[]byte(withSecurityToken.String()),
	)
	if !validToken(securityToken, "eu-north-1", expiresAt.UnixMilli()) {
		t.Fatal("validToken() rejected a canonical session token")
	}
	if validToken(valid, "eu-north-1", expiresAt.Add(time.Second).UnixMilli()) {
		t.Fatal("validToken() accepted mismatched signer expiry metadata")
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
		!errors.Is(err, ErrTokenCanceled) ||
		err.Error() != "kafka/mskiam: token generation canceled" ||
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
	if err == nil || err.Error() != "kafka/mskiam: token generation canceled" ||
		!errors.Is(err, ErrTokenCanceled) ||
		!errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("canceled parent result: calls=%d err=%v", calls.Load(), err)
	}

	_, err = provider.Token(context.Background())
	if !errors.Is(err, ErrTokenGeneration) ||
		!errors.Is(err, ErrTokenTimeout) ||
		err.Error() != "kafka/mskiam: token generation timed out" ||
		!errors.Is(err, context.DeadlineExceeded) ||
		calls.Load() != 1 {
		t.Fatalf("owned deadline result: calls=%d err=%v", calls.Load(), err)
	}
}

func TestTokenRejectsSuccessAfterContextEnds(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	validToken, validExpiry := signedTestToken("eu-north-1", now)
	credentials := aws.Credentials{
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
	}
	t.Run("credential retrieval cancels parent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var generationCalls atomic.Int64
		provider := testProvider(now, generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			generationCalls.Add(1)
			return validToken, validExpiry.UnixMilli(), nil
		}))
		provider.credentials = credentialsProviderFunc(func(
			context.Context,
		) (aws.Credentials, error) {
			cancel()
			return credentials, nil
		})
		if _, err := provider.Token(ctx); !errors.Is(err, ErrTokenCanceled) ||
			!errors.Is(err, context.Canceled) || generationCalls.Load() != 0 {
			t.Fatalf("post-retrieval cancellation: calls=%d err=%v", generationCalls.Load(), err)
		}
	})
	t.Run("credential refresh cancels parent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		var generationCalls atomic.Int64
		expiring := credentials
		expiring.CanExpire = true
		expiring.Expires = now.Add(10 * time.Second)
		refreshed := credentials
		refreshed.CanExpire = true
		refreshed.Expires = now.Add(5 * time.Minute)
		provider := testProvider(now, generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			generationCalls.Add(1)
			return validToken, validExpiry.UnixMilli(), nil
		}))
		provider.credentials = &scriptedCredentialsProvider{
			results: []credentialResult{
				{credentials: expiring},
				{credentials: refreshed},
			},
			afterRetrieve: func(retrievals int) {
				if retrievals == 2 {
					cancel()
				}
			},
		}
		if _, err := provider.Token(ctx); !errors.Is(err, ErrTokenCanceled) ||
			!errors.Is(err, context.Canceled) || generationCalls.Load() != 0 {
			t.Fatalf("post-refresh cancellation: calls=%d err=%v", generationCalls.Load(), err)
		}
	})
	t.Run("signer cancels parent before success", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		provider := testProvider(now, generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			cancel()
			return validToken, validExpiry.UnixMilli(), nil
		}))
		if _, err := provider.Token(ctx); !errors.Is(err, ErrTokenCanceled) ||
			!errors.Is(err, context.Canceled) {
			t.Fatalf("post-signing cancellation: %v", err)
		}
	})
	t.Run("signer outlives owned timeout", func(t *testing.T) {
		provider := testProvider(now, generatorFunc(func(
			ctx context.Context,
			_ string,
			_ aws.Credentials,
		) (string, int64, error) {
			<-ctx.Done()
			return validToken, validExpiry.UnixMilli(), nil
		}))
		if _, err := provider.Token(context.Background()); !errors.Is(err, ErrTokenTimeout) ||
			!errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("post-timeout signer success: %v", err)
		}
	})
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
	oversizedAccessKey := valid
	oversizedAccessKey.AccessKeyID = strings.Repeat("a", 129)
	oversizedSecret := valid
	oversizedSecret.SecretAccessKey = strings.Repeat("s", 257)
	oversizedSessionToken := valid
	oversizedSessionToken.SessionToken = strings.Repeat("t", (16<<10)+1)
	maximumCredentials := aws.Credentials{
		AccessKeyID:     strings.Repeat("a", 128),
		SecretAccessKey: strings.Repeat("s", 256),
		SessionToken:    strings.Repeat("t", 16<<10),
		CanExpire:       true,
		Expires:         validExpiring.Expires,
	}
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
			name: "oversized access key",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return oversizedAccessKey, nil
			}),
			want: ErrInvalidCredentials,
		},
		{
			name: "oversized secret",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return oversizedSecret, nil
			}),
			want: ErrInvalidCredentials,
		},
		{
			name: "oversized session token",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return oversizedSessionToken, nil
			}),
			want: ErrInvalidCredentials,
		},
		{
			name: "maximum credential sizes",
			provider: credentialsProviderFunc(func(
				context.Context,
			) (aws.Credentials, error) {
				return maximumCredentials, nil
			}),
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

			validToken, validExpiry := signedTestToken("eu-north-1", now)
			provider := testProvider(now, generatorFunc(func(
				context.Context,
				string,
				aws.Credentials,
			) (string, int64, error) {
				return validToken, validExpiry.UnixMilli(), nil
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
	validToken, validExpiry := signedTestToken("eu-north-1", startedAt)
	provider := testProvider(startedAt, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return validToken, validExpiry.UnixMilli(), nil
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

	if _, err := provider.Token(context.Background()); err == nil ||
		err.Error() != "kafka/mskiam: token is expired or expires too soon" ||
		!errors.Is(err, ErrTokenExpired) ||
		!errors.Is(err, ErrInvalidToken) {
		t.Fatalf("credential expired during generation: %v", err)
	}
}

func TestTokenReturnsOwnedValue(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	validToken, validExpiry := signedTestToken("eu-north-1", now)
	provider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return validToken, validExpiry.UnixMilli(), nil
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
	if string(second.Token) != validToken {
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
	causeOnly := &ProviderError{
		category: ErrCredentialLoad,
		cause:    context.Canceled,
	}
	if unwrapped := causeOnly.Unwrap(); len(unwrapped) != 2 ||
		unwrapped[0] != ErrCredentialLoad || unwrapped[1] != context.Canceled {
		t.Fatalf("cause-only provider error chain: %#v", unwrapped)
	}
	contextOnly := &ProviderError{
		category:        ErrCredentialLoad,
		contextCategory: ErrTokenCanceled,
	}
	if unwrapped := contextOnly.Unwrap(); len(unwrapped) != 2 ||
		unwrapped[0] != ErrCredentialLoad || unwrapped[1] != ErrTokenCanceled {
		t.Fatalf("context-only provider error chain: %#v", unwrapped)
	}
	sameCategory := &ProviderError{
		category:        ErrTokenCanceled,
		contextCategory: ErrTokenCanceled,
		cause:           context.Canceled,
	}
	if unwrapped := sameCategory.Unwrap(); len(unwrapped) != 2 ||
		unwrapped[0] != ErrTokenCanceled || unwrapped[1] != context.Canceled {
		t.Fatalf("duplicate provider error category: %#v", unwrapped)
	}
}
