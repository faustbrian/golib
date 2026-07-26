package authhttp_test

import (
	"errors"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authhttp"
)

func TestFormatChallengeSortsAndEscapesParameters(t *testing.T) {
	t.Parallel()

	challenge, err := authentication.NewChallenge("Bearer", map[string]string{
		"realm": "a\"b\\c",
		"error": "invalid_token",
	})
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	got, err := authhttp.FormatChallenge(challenge)
	if err != nil {
		t.Fatalf("FormatChallenge() error = %v", err)
	}
	want := `Bearer error="invalid_token", realm="a\"b\\c"`
	if got != want {
		t.Fatalf("FormatChallenge() = %q, want %q", got, want)
	}
}

func TestFormatChallengeRejectsZeroValue(t *testing.T) {
	t.Parallel()

	if _, err := authhttp.FormatChallenge(authentication.Challenge{}); !errors.Is(err, authentication.ErrInvalidChallenge) {
		t.Fatalf("FormatChallenge() error = %v", err)
	}
}

func TestFormatChallengeWithoutParametersUsesOnlyScheme(t *testing.T) {
	t.Parallel()

	challenge, err := authentication.NewChallenge("Basic", nil)
	if err != nil {
		t.Fatalf("NewChallenge() error = %v", err)
	}
	formatted, err := authhttp.FormatChallenge(challenge)
	if err != nil || formatted != "Basic" {
		t.Fatalf("FormatChallenge() = %q, %v", formatted, err)
	}
}
