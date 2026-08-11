package mskiam_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-msk-iam-sasl-signer-go/signer"
	"github.com/aws/aws-sdk-go-v2/aws"
	mskiam "github.com/faustbrian/golib/pkg/kafka/adapters/mskiam"
)

type sourceCredentials struct {
	accessKey string
	secretKey string
	session   string
}

type generatedExplicitProvider struct {
	credentials sourceCredentials
}

func (provider generatedExplicitProvider) Retrieve(
	context.Context,
) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     provider.credentials.accessKey,
		SecretAccessKey: provider.credentials.secretKey,
		SessionToken:    provider.credentials.session,
	}, nil
}

func generatedSourceCredentials(t *testing.T, source string) sourceCredentials {
	t.Helper()
	digest := sha256.Sum256([]byte(t.Name() + ":" + source))
	encoded := strings.ToUpper(hex.EncodeToString(digest[:]))

	return sourceCredentials{
		accessKey: "AKIA" + encoded[:16],
		secretKey: encoded[16:56],
		session:   "session-" + encoded[56:],
	}
}

func clearDefaultCredentialSources(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	emptyCredentials := filepath.Join(directory, "credentials")
	emptyConfig := filepath.Join(directory, "config")
	for _, path := range []string{emptyCredentials, emptyConfig} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write isolated AWS configuration: %v", err)
		}
	}
	for _, key := range []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_PROFILE",
		"AWS_SHARED_CREDENTIALS_FILE",
		"AWS_CONFIG_FILE",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
		"AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_ROLE_ARN",
		"AWS_ROLE_SESSION_NAME",
		"AWS_ENDPOINT_URL_STS",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", emptyCredentials)
	t.Setenv("AWS_CONFIG_FILE", emptyConfig)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func assertDefaultChainTokenUses(
	t *testing.T,
	want sourceCredentials,
) {
	t.Helper()
	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:       "eu-north-1",
		TokenTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new default-chain provider: %v", err)
	}
	token, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("generate default-chain token: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(token.Token))
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	signedURL, err := url.Parse(string(decoded))
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	credential := signedURL.Query().Get("X-Amz-Credential")
	if !strings.HasPrefix(credential, want.accessKey+"/") {
		t.Fatalf("signed credential does not use selected %s source", t.Name())
	}
	if token.ExpiresAt.After(time.Now().Add(20 * time.Minute)) {
		t.Fatal("token expiry exceeds adapter lifetime bound")
	}
}

func providerTokenAccessKey(t *testing.T, provider *mskiam.Provider) string {
	t.Helper()
	token, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("generate provider token: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(token.Token))
	if err != nil {
		t.Fatalf("decode provider token: %v", err)
	}
	signedURL, err := url.Parse(string(decoded))
	if err != nil {
		t.Fatalf("parse provider token: %v", err)
	}

	return strings.SplitN(
		signedURL.Query().Get("X-Amz-Credential"),
		"/",
		2,
	)[0]
}

func TestDefaultCredentialChainSourcePaths(t *testing.T) {
	t.Run("environment", func(t *testing.T) {
		clearDefaultCredentialSources(t)
		credentials := generatedSourceCredentials(t, "environment")
		t.Setenv("AWS_ACCESS_KEY_ID", credentials.accessKey)
		t.Setenv("AWS_SECRET_ACCESS_KEY", credentials.secretKey)
		t.Setenv("AWS_SESSION_TOKEN", credentials.session)

		assertDefaultChainTokenUses(t, credentials)
	})

	t.Run("shared profile", func(t *testing.T) {
		clearDefaultCredentialSources(t)
		credentials := generatedSourceCredentials(t, "profile")
		directory := t.TempDir()
		credentialsPath := filepath.Join(directory, "credentials")
		contents := fmt.Appendf(
			nil,
			"[workload]\naws_access_key_id=%s\naws_secret_access_key=%s\naws_session_token=%s\n",
			credentials.accessKey,
			credentials.secretKey,
			credentials.session,
		)
		if err := os.WriteFile(credentialsPath, contents, 0o600); err != nil {
			t.Fatalf("write generated profile: %v", err)
		}
		t.Setenv("AWS_PROFILE", "workload")
		t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)

		assertDefaultChainTokenUses(t, credentials)
	})

	t.Run("ECS task role", func(t *testing.T) {
		clearDefaultCredentialSources(t)
		credentials := generatedSourceCredentials(t, "ecs")
		server := httptest.NewServer(http.HandlerFunc(func(
			writer http.ResponseWriter,
			_ *http.Request,
		) {
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(map[string]string{
				"AccessKeyId":     credentials.accessKey,
				"SecretAccessKey": credentials.secretKey,
				"Token":           credentials.session,
				"Expiration":      time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			}); err != nil {
				t.Errorf("encode ECS credential response: %v", err)
			}
		}))
		defer server.Close()
		t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", server.URL)

		assertDefaultChainTokenUses(t, credentials)
	})

	t.Run("EKS pod identity", func(t *testing.T) {
		clearDefaultCredentialSources(t)
		credentials := generatedSourceCredentials(t, "pod-identity")
		initialCredentials := generatedSourceCredentials(t, "initial-pod-identity")
		authorization := "Bearer " + generatedSourceCredentials(t, "authorization").session
		rotatedAuthorization := "Bearer " + generatedSourceCredentials(t, "rotated-authorization").session
		tokenPath := filepath.Join(t.TempDir(), "pod-identity-token")
		if err := os.WriteFile(tokenPath, []byte(authorization), 0o600); err != nil {
			t.Fatalf("write generated pod identity token: %v", err)
		}
		var requests atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(
			writer http.ResponseWriter,
			request *http.Request,
		) {
			requestNumber := requests.Add(1)
			wantAuthorization := rotatedAuthorization
			responseCredentials := credentials
			expiration := time.Now().Add(time.Hour)
			if requestNumber == 1 {
				wantAuthorization = authorization
				responseCredentials = initialCredentials
				expiration = time.Now().Add(10 * time.Second)
			}
			if request.Header.Get("Authorization") != wantAuthorization {
				t.Error("pod identity authorization token was not forwarded")
			}
			writer.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(writer).Encode(map[string]string{
				"AccessKeyId":     responseCredentials.accessKey,
				"SecretAccessKey": responseCredentials.secretKey,
				"Token":           responseCredentials.session,
				"Expiration":      expiration.UTC().Format(time.RFC3339),
			}); err != nil {
				t.Errorf("encode pod identity credential response: %v", err)
			}
			if requestNumber == 1 {
				if err := os.WriteFile(
					tokenPath,
					[]byte(rotatedAuthorization),
					0o600,
				); err != nil {
					t.Errorf("rotate generated pod identity token: %v", err)
				}
			}
		}))
		defer server.Close()
		t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", server.URL)
		t.Setenv("AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE", tokenPath)

		assertDefaultChainTokenUses(t, credentials)
		if requests.Load() != 2 {
			t.Fatalf("pod identity credential requests = %d, want 2", requests.Load())
		}
	})

	t.Run("EKS web identity", func(t *testing.T) {
		clearDefaultCredentialSources(t)
		credentials := generatedSourceCredentials(t, "web-identity")
		identityToken := generatedSourceCredentials(t, "identity-token").session
		tokenPath := filepath.Join(t.TempDir(), "web-identity-token")
		if err := os.WriteFile(tokenPath, []byte(identityToken), 0o600); err != nil {
			t.Fatalf("write generated web identity token: %v", err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(
			writer http.ResponseWriter,
			request *http.Request,
		) {
			if err := request.ParseForm(); err != nil {
				t.Errorf("parse STS request: %v", err)
			}
			if request.Form.Get("Action") != "AssumeRoleWithWebIdentity" ||
				request.Form.Get("WebIdentityToken") != identityToken {
				t.Error("unexpected STS web identity request")
			}
			writer.Header().Set("Content-Type", "text/xml")
			_, _ = fmt.Fprintf(writer, `<AssumeRoleWithWebIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <AssumeRoleWithWebIdentityResult><Credentials>
    <AccessKeyId>%s</AccessKeyId><SecretAccessKey>%s</SecretAccessKey>
    <SessionToken>%s</SessionToken><Expiration>%s</Expiration>
  </Credentials></AssumeRoleWithWebIdentityResult>
  <ResponseMetadata><RequestId>generated-request</RequestId></ResponseMetadata>
</AssumeRoleWithWebIdentityResponse>`, credentials.accessKey, credentials.secretKey,
				credentials.session, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		}))
		defer server.Close()
		t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", tokenPath)
		t.Setenv("AWS_ROLE_ARN", "arn:aws:iam::123456789012:role/generated-test")
		t.Setenv("AWS_ROLE_SESSION_NAME", "generated-session")
		t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)

		assertDefaultChainTokenUses(t, credentials)
	})
}

func TestWorkloadReplacementLoadsCurrentEnvironmentIdentity(t *testing.T) {
	clearDefaultCredentialSources(t)
	initial := generatedSourceCredentials(t, "initial-workload")
	rotated := generatedSourceCredentials(t, "replacement-workload")
	t.Setenv("AWS_ACCESS_KEY_ID", initial.accessKey)
	t.Setenv("AWS_SECRET_ACCESS_KEY", initial.secretKey)
	t.Setenv("AWS_SESSION_TOKEN", initial.session)
	initialProvider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:       "eu-north-1",
		TokenTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new initial workload provider: %v", err)
	}
	t.Setenv("AWS_ACCESS_KEY_ID", rotated.accessKey)
	t.Setenv("AWS_SECRET_ACCESS_KEY", rotated.secretKey)
	t.Setenv("AWS_SESSION_TOKEN", rotated.session)
	replacementProvider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:       "eu-north-1",
		TokenTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new replacement workload provider: %v", err)
	}

	if got := providerTokenAccessKey(t, initialProvider); got != initial.accessKey {
		t.Fatal("initial workload identity changed after environment rotation")
	}
	if got := providerTokenAccessKey(t, replacementProvider); got != rotated.accessKey {
		t.Fatal("replacement workload did not load the rotated identity")
	}
}

func TestExplicitProviderUsesGeneratedIdentity(t *testing.T) {
	credentials := generatedSourceCredentials(t, "explicit-provider")
	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region: "eu-north-1",
		CredentialsProvider: generatedExplicitProvider{
			credentials: credentials,
		},
		TokenTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new explicit provider: %v", err)
	}
	if got := providerTokenAccessKey(t, provider); got != credentials.accessKey {
		t.Fatal("token did not use the explicit generated identity")
	}
}

func TestProviderRejectsProcessWideSignerDebugSettingWithoutMutation(
	t *testing.T,
) {
	credentials := generatedSourceCredentials(t, "signer-global")
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		requests.Add(1)
		writer.Header().Set("Content-Type", "text/xml")
		_, _ = fmt.Fprint(writer, `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult><Arn>generated-arn</Arn><UserId>generated-user</UserId><Account>generated-account</Account></GetCallerIdentityResult>
  <ResponseMetadata><RequestId>generated-request</RequestId></ResponseMetadata>
</GetCallerIdentityResponse>`)
	}))
	defer server.Close()
	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)
	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region: "eu-north-1",
		CredentialsProvider: generatedExplicitProvider{
			credentials: credentials,
		},
		TokenTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new explicit provider: %v", err)
	}
	debugSetting := signer.AwsDebugCreds
	signer.AwsDebugCreds = true
	defer func() { signer.AwsDebugCreds = debugSetting }()
	logOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(logOutput)

	if _, err := provider.Token(context.Background()); !errors.Is(err, mskiam.ErrTokenGeneration) {
		t.Fatalf("debug signer setting error category = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("debug signer setting triggered an external identity request")
	}
	if !signer.AwsDebugCreds {
		t.Fatal("provider mutated the process-wide signer debug setting")
	}
}
