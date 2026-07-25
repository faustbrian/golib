package prompts_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	prompts "github.com/faustbrian/golib/pkg/prompts"
)

const secretCanary = "secret-canary-7c3e99"

func TestSecretValueRedactsFormattingAndSerialization(t *testing.T) {
	t.Parallel()

	secret := prompts.NewSecretValue(secretCanary)
	if secret.Reveal() != secretCanary {
		t.Fatal("Reveal() did not return the explicit secret")
	}
	representations := make([]string, 0, 8)
	representations = append(representations,
		secret.String(), secret.GoString(),
		fmt.Sprint(secret), fmt.Sprintf("%+v", secret), fmt.Sprintf("%#v", secret),
		secret.LogValue().String(),
	)
	text, err := secret.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	representations = append(representations, string(text))
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	representations = append(representations, string(encoded))
	for _, representation := range representations {
		if strings.Contains(representation, secretCanary) || !strings.Contains(representation, "REDACTED") {
			t.Fatalf("secret representation = %q", representation)
		}
	}
}

func TestSecretPromptSuppressesValidationAndPanicDisclosure(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSecret(prompts.SecretConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
		PostValidate: []prompts.Validator[prompts.SecretValue]{func(context.Context, prompts.SecretValue, prompts.ValidationContext) error {
			return prompts.NewValidationIssue("bad_secret", secretCanary, "token")
		}},
	})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	_, err = prompts.Parse(context.Background(), prompt, secretCanary, nil)
	if !errors.Is(err, prompts.ErrValidationExhausted) || strings.Contains(fmt.Sprintf("%+v", err), secretCanary) {
		t.Fatalf("secret validation error = %v", err)
	}
	var issue *prompts.ValidationIssue
	if !errors.As(err, &issue) || issue.Code() != "secret_validation" || strings.Contains(issue.Message(), secretCanary) {
		t.Fatalf("secret validation issue = %#v", issue)
	}

	panicPrompt, err := prompts.NewSecret(prompts.SecretConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
		Transform: []prompts.Transformer[prompts.SecretValue]{func(context.Context, prompts.SecretValue, prompts.ValidationContext) (prompts.SecretValue, error) {
			panic(secretCanary)
		}},
	})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	_, err = prompts.Parse(context.Background(), panicPrompt, secretCanary, nil)
	if !errors.Is(err, prompts.ErrAdapter) || strings.Contains(fmt.Sprintf("%+v", err), secretCanary) {
		t.Fatalf("secret panic error = %v", err)
	}
}

func TestSecretDefaultsStayOutOfMetadataAndOutput(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSecret(prompts.SecretConfig{
		ID: "password", Label: "Password", Description: "Account password",
		Class: prompts.SecretPassword, Headless: prompts.HeadlessUseFallback,
		Fallback: prompts.Some(prompts.NewSecretValue(secretCanary)),
	})
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	descriptor := prompt.Describe()
	if descriptor.Kind != prompts.KindSecret || descriptor.Secret != prompts.SecretPassword {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if strings.Contains(fmt.Sprintf("%#v", descriptor), secretCanary) {
		t.Fatal("descriptor exposed a secret fallback")
	}
	value, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil || value.Reveal() != secretCanary {
		t.Fatalf("Run() = %v, %v", value, err)
	}
}

func TestSecretDefinitionsRequireClassification(t *testing.T) {
	t.Parallel()

	_, err := prompts.NewSecret(prompts.SecretConfig{ID: "secret", Label: "Secret"})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("unclassified secret error = %v", err)
	}
	_, err = prompts.NewSecret(prompts.SecretConfig{ID: "secret", Label: "Secret", Class: prompts.SecretClass(200)})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid secret class error = %v", err)
	}
}

func TestSecretBytesCopiesRedactsAndDestroysMemory(t *testing.T) {
	t.Parallel()

	source := []byte(secretCanary)
	secret := prompts.NewSecretBytes(source)
	source[0] = 'X'
	if got := string(secret.Reveal()); got != secretCanary {
		t.Fatalf("Reveal() = %q", got)
	}
	revealed := secret.Reveal()
	revealed[0] = 'X'
	if got := string(secret.Reveal()); got != secretCanary {
		t.Fatal("Reveal() exposed internal bytes")
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	text, err := secret.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	for _, representation := range []string{
		secret.String(), secret.GoString(), fmt.Sprint(secret), fmt.Sprintf("%#v", secret),
		secret.LogValue().String(), string(text), string(encoded),
	} {
		if strings.Contains(representation, secretCanary) || !strings.Contains(representation, "REDACTED") {
			t.Fatalf("secret bytes representation = %q", representation)
		}
	}
	if secret.Len() != len(secretCanary) || secret.Destroyed() {
		t.Fatalf("secret state = length %d destroyed %v", secret.Len(), secret.Destroyed())
	}

	var group sync.WaitGroup
	for range 8 {
		group.Go(func() { _ = secret.Reveal() })
	}
	group.Wait()
	secret.Destroy()
	secret.Destroy()
	if !secret.Destroyed() || secret.Len() != 0 || secret.Reveal() != nil {
		t.Fatal("Destroy() did not idempotently clear owned bytes")
	}
}

func TestNilSecretBytesIsSafeAndDestroyed(t *testing.T) {
	t.Parallel()

	var secret *prompts.SecretBytes
	secret.Destroy()
	if secret.Reveal() != nil || secret.Len() != 0 || !secret.Destroyed() {
		t.Fatal("nil secret bytes did not report an empty destroyed value")
	}
	if got := fmt.Sprint(secret); !strings.Contains(got, "REDACTED") {
		t.Fatalf("nil secret formatting = %q", got)
	}
}

func TestSecretBytesPromptReturnsIndependentOwnedResults(t *testing.T) {
	t.Parallel()

	fallback := prompts.NewSecretBytes([]byte(secretCanary))
	prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
		Headless: prompts.HeadlessUseFallback, Fallback: prompts.Some(fallback),
	})
	if err != nil {
		t.Fatalf("NewSecretBytesPrompt() error = %v", err)
	}
	first, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	first.Destroy()
	second, err := prompts.Run(context.Background(), prompt, prompts.Execution{
		Policy: prompts.InteractionPolicy{Mode: prompts.NonInteractiveOnly},
	})
	if err != nil || string(second.Reveal()) != secretCanary {
		t.Fatalf("second Run() = %v, %v", second, err)
	}

	input := []byte("byte-input")
	parsed, err := prompts.ParseBytes(context.Background(), prompt, input, nil)
	if err != nil {
		t.Fatalf("ParseBytes() error = %v", err)
	}
	input[0] = 'X'
	if string(parsed.Reveal()) != "byte-input" {
		t.Fatal("ParseBytes() retained caller memory")
	}
	parsedText, err := prompts.Parse(context.Background(), prompt, "text-input", nil)
	if err != nil || string(parsedText.Reveal()) != "text-input" {
		t.Fatalf("Parse() = %v, %v", parsedText, err)
	}
}

func TestSecretBytesInteractiveEntryUsesAndClearsByteEvents(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
	})
	if err != nil {
		t.Fatalf("NewSecretBytesPrompt() error = %v", err)
	}
	input := []byte("secret-👩‍💻")
	event := prompts.PasteBytesEvent(input)
	input[0] = 'X'
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(event, prompts.KeyEvent(prompts.KeyLeft), prompts.RuneEvent('!'),
		prompts.KeyEvent(prompts.KeyEnd), prompts.KeyEvent(prompts.KeyEnter))
	result, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	defer result.Destroy()
	if got := string(result.Reveal()); got != "secret-!👩‍💻" {
		t.Fatalf("Reveal() = %q", got)
	}
	if !event.Bytes.Destroyed() || event.Bytes.Len() != 0 {
		t.Fatal("interactive event retained secret data")
	}
	if strings.Contains(terminal.Output(), secretCanary) || strings.Contains(terminal.Output(), "secret-!") {
		t.Fatalf("terminal output exposed secret: %q", terminal.Output())
	}
}

func TestSecretBytesInteractiveCancellationClearsEditorAndEvents(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
	})
	if err != nil {
		t.Fatalf("NewSecretBytesPrompt() error = %v", err)
	}
	event := prompts.PasteBytesEvent([]byte(secretCanary))
	terminal := prompts.NewVirtualTerminal(80, 24)
	terminal.Push(event, prompts.KeyEvent(prompts.KeyCtrlC))
	result, err := prompts.Run(context.Background(), prompt, interactiveExecution(terminal))
	if result != nil || !errors.Is(err, prompts.ErrCanceled) {
		t.Fatalf("Run() = %v, %v", result, err)
	}
	if !event.Bytes.Destroyed() || event.Bytes.Len() != 0 {
		t.Fatal("canceled byte event retained secret data")
	}
}

func TestSecretBytesPromptRejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()

	_, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{ID: "token", Label: "Token"})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("unclassified secret bytes error = %v", err)
	}
	_, err = prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretClass(200),
	})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("invalid secret bytes class error = %v", err)
	}
	_, err = prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{Class: prompts.SecretToken})
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("missing secret bytes identity error = %v", err)
	}
}

func TestParseBytesValidatesContextTypeAndSafeErrors(t *testing.T) {
	t.Parallel()

	prompt, err := prompts.NewSecretBytesPrompt(prompts.SecretBytesConfig{
		ID: "token", Label: "Token", Class: prompts.SecretToken,
		PostValidate: []prompts.Validator[*prompts.SecretBytes]{func(context.Context, *prompts.SecretBytes, prompts.ValidationContext) error {
			return errors.New(secretCanary)
		}},
	})
	if err != nil {
		t.Fatalf("NewSecretBytesPrompt() error = %v", err)
	}
	_, err = prompts.ParseBytes(context.Background(), prompt, []byte(secretCanary), nil)
	if !errors.Is(err, prompts.ErrValidationExhausted) || strings.Contains(fmt.Sprintf("%+v", err), secretCanary) {
		t.Fatalf("secret bytes validation error = %v", err)
	}
	//lint:ignore SA1012 Nil context behavior is part of the public contract.
	_, err = prompts.ParseBytes(nil, prompt, nil, nil) //nolint:staticcheck // contract test
	if !errors.Is(err, prompts.ErrInvalidDefinition) {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = prompts.ParseBytes(canceled, prompt, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	_, err = prompts.ParseBytes(context.Background(), prompts.Prompt[*prompts.SecretBytes]{}, nil, nil)
	if !errors.Is(err, prompts.ErrUnsupported) {
		t.Fatalf("wrong prompt error = %v", err)
	}
}

var _ slog.LogValuer = prompts.SecretValue{}
