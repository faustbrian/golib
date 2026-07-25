package config_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
)

func TestSecretRedactsFormattingAndMarshaling(t *testing.T) {
	t.Parallel()

	secret := config.NewSecret("canary-secret-value")
	outputs := []string{
		fmt.Sprint(secret),
		fmt.Sprintf("%s", secret),
		fmt.Sprintf("%q", secret),
		fmt.Sprintf("%v", secret),
		fmt.Sprintf("%+v", secret),
		fmt.Sprintf("%#v", secret),
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	outputs = append(outputs, string(encoded))

	for _, output := range outputs {
		if strings.Contains(output, "canary-secret-value") {
			t.Fatalf("secret formatting leaked value in %q", output)
		}
		if !strings.Contains(output, config.Redacted) {
			t.Fatalf("secret formatting = %q, want redaction marker", output)
		}
	}
	if got := secret.Reveal(); got != "canary-secret-value" {
		t.Fatalf("Secret.Reveal() = %q", got)
	}
}

func TestSecretDecodesFromText(t *testing.T) {
	t.Parallel()

	type configuration struct {
		Token config.Secret `config:"token"`
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "secret", Priority: 10, Sensitive: true},
		tree: map[string]any{"token": "canary-secret-value"},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[configuration](t.Context(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := snapshot.Value().Token.Reveal(); got != "canary-secret-value" {
		t.Fatalf("Secret.Reveal() = %q", got)
	}
}

func TestSecretDirectFormattingContractsAreRedacted(t *testing.T) {
	t.Parallel()

	secret := config.NewSecret("canary-secret-value")
	if secret.String() != config.Redacted || secret.GoString() != config.Redacted {
		t.Fatalf("direct formatting leaked secret")
	}
	text, err := secret.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if string(text) != config.Redacted || strings.Contains(string(text), "canary") {
		t.Fatalf("MarshalText() = %q", text)
	}
}

func TestSecretComparisonsRemainRedactedWhenFormatted(t *testing.T) {
	t.Parallel()

	secret := config.NewSecret("canary-secret-value")
	same := config.NewSecret("canary-secret-value")
	different := config.NewSecret("different-secret-value")
	if secret != same || secret == different {
		t.Fatal("Secret equality does not compare wrapped values")
	}
	if !reflect.DeepEqual(secret, same) || reflect.DeepEqual(secret, different) {
		t.Fatal("Secret deep equality does not compare wrapped values")
	}
	diagnostic := fmt.Sprintf(
		"equal=%t deep=%t got=%#v want=%#v",
		secret == different,
		reflect.DeepEqual(secret, different),
		secret,
		different,
	)
	if strings.Contains(diagnostic, "canary-secret-value") ||
		strings.Contains(diagnostic, "different-secret-value") {
		t.Fatalf("comparison diagnostic leaked a secret: %q", diagnostic)
	}
}
