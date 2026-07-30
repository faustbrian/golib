package mskiam

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestProviderAcceptsExactTokenTimeout(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	provider := testProvider(now, generatorFunc(func(
		context.Context,
		string,
		aws.Credentials,
	) (string, int64, error) {
		return "YWJj", now.Add(15 * time.Minute).UnixMilli(), nil
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
		want := region == "az-a0z9-19"
		got := validRegion(region)
		if got != want {
			t.Fatalf("validRegion(%q) = %t, want %t", region, got, want)
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
}

func TestTokenAcceptsExactEncodedSizeLimit(t *testing.T) {
	t.Parallel()

	if !validToken(strings.Repeat("a", maxTokenBytes)) {
		t.Fatal("validToken() rejected exact encoded size limit")
	}
}
