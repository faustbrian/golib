package mskiam

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestProviderAcceptsExactTokenTimeout(t *testing.T) {
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
	provider.timeout = maxTokenTimeout
	if _, err := provider.Token(context.Background()); err != nil {
		t.Fatalf("Token() exact timeout error = %v", err)
	}
}

func TestNilProviderErrorIsSafeAndRedacted(t *testing.T) {
	t.Parallel()

	var providerErr *ProviderError
	if providerErr.Error() != ErrTokenGeneration.Error() ||
		providerErr.GoString() != ErrTokenGeneration.Error() {
		t.Fatalf("nil ProviderError diagnostic = %q", providerErr.Error())
	}
}

func TestRegionCharacterAndLengthBoundaries(t *testing.T) {
	t.Parallel()

	exactLength := "eu-" + strings.Repeat("a", 59) + "-1"
	if len(exactLength) != maxRegionBytes || !validRegion(exactLength) {
		t.Fatalf("validRegion() rejected exact length %d", len(exactLength))
	}
	for _, region := range []string{
		"az-a0z9-19",
		"eu-`-1",
		"eu-{-1",
		"eu-/-1",
		"eu-:-1",
	} {
		if validRegion(region) {
			t.Fatalf("validRegion(%q) accepted a noncanonical region", region)
		}
	}

	if !lowercaseLetters("az") ||
		lowercaseLetters("`a") ||
		lowercaseLetters("z{") {
		t.Fatal("lowercaseLetters() mishandled alphabet boundaries")
	}
	if !decimalDigits("09") ||
		decimalDigits("/0") ||
		decimalDigits("9:") {
		t.Fatal("decimalDigits() mishandled digit boundaries")
	}
	if awsRegionPrefixLength([]string{"eu"}) != 0 {
		t.Fatal("awsRegionPrefixLength() accepted an incomplete region")
	}
}

func TestTokenAcceptsExactEncodedSizeLimit(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	token, expiresAt := signedTestToken("eu-north-1", now)
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode test token: %v", err)
	}
	signedURL, err := url.Parse(string(decoded))
	if err != nil {
		t.Fatalf("parse test token: %v", err)
	}
	query := signedURL.Query()
	decodedLimit := maxTokenBytes * 3 / 4
	query.Set(
		"User-Agent",
		query.Get("User-Agent")+strings.Repeat("a", decodedLimit-len(decoded)),
	)
	signedURL.RawQuery = query.Encode()
	token = base64.RawURLEncoding.EncodeToString([]byte(signedURL.String()))
	if len(token) != maxTokenBytes ||
		!validToken(token, "eu-north-1", expiresAt.UnixMilli(), now) {
		t.Fatalf(
			"validToken() rejected exact encoded size limit: encoded=%d decoded=%d",
			len(token), len(signedURL.String()),
		)
	}
}
