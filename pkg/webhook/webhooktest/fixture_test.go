package webhooktest

import (
	"testing"
	"time"

	webhook "github.com/faustbrian/golib/pkg/webhook"
)

func TestFixtureProvidesDeterministicClockNoncesIDsAndPair(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 123).UTC()
	fixture, err := NewFixture(FixtureConfig{
		Algorithm: webhook.SHA256, KeyID: "key", Secret: []byte("secret"),
		Time: now, Tolerance: time.Minute, NoncePrefix: "nonce", IDPrefix: "delivery",
	})
	if err != nil {
		t.Fatalf("NewFixture() error = %v", err)
	}
	if got := fixture.Clock.Now(); !got.Equal(now) {
		t.Fatalf("Clock.Now() = %v", got)
	}
	fixture.Clock.Advance(time.Minute)
	fixture.Clock.Set(now)

	message := webhook.Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com"}
	first, err := fixture.Signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign(first) error = %v", err)
	}
	second, err := fixture.Signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign(second) error = %v", err)
	}
	if first[0].Nonce != "nonce-1" || second[0].Nonce != "nonce-2" {
		t.Fatalf("nonces = %q, %q", first[0].Nonce, second[0].Nonce)
	}
	if _, err := fixture.Verifier.Verify(message, first); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	id1, err1 := fixture.IDGenerator()
	id2, err2 := fixture.IDGenerator()
	if err1 != nil || err2 != nil || id1 != "delivery-1" || id2 != "delivery-2" {
		t.Fatalf("IDs = %q, %q; errors = %v, %v", id1, id2, err1, err2)
	}
}

func TestNewFixtureRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	valid := FixtureConfig{
		Algorithm: webhook.SHA256, KeyID: "key", Secret: []byte("secret"),
		Time: time.Unix(1, 0), Tolerance: time.Minute, NoncePrefix: "nonce", IDPrefix: "id",
	}
	for name, mutate := range map[string]func(*FixtureConfig){
		"time":     func(config *FixtureConfig) { config.Time = time.Time{} },
		"nonce":    func(config *FixtureConfig) { config.NoncePrefix = "" },
		"ID":       func(config *FixtureConfig) { config.IDPrefix = "" },
		"signer":   func(config *FixtureConfig) { config.Secret = nil },
		"verifier": func(config *FixtureConfig) { config.Tolerance = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			if _, err := NewFixture(config); err == nil {
				t.Fatal("NewFixture() accepted invalid configuration")
			}
		})
	}
}
