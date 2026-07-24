package prompts

import "fmt"

// PromptKind identifies the semantic prompt type independently of rendering.
type PromptKind string

const (
	KindText         PromptKind = "text"
	KindMultiline    PromptKind = "multiline"
	KindInteger      PromptKind = "integer"
	KindDecimal      PromptKind = "decimal"
	KindDuration     PromptKind = "duration"
	KindDate         PromptKind = "date"
	KindTime         PromptKind = "time"
	KindPath         PromptKind = "path"
	KindConfirm      PromptKind = "confirm"
	KindSelect       PromptKind = "select"
	KindMultiSelect  PromptKind = "multi_select"
	KindSearchSelect PromptKind = "search_select"
	KindSecret       PromptKind = "secret"
	KindSecretBytes  PromptKind = "secret_bytes"
)

// RetryPolicy bounds invalid submissions. Unlimited retry requires separate
// authority in the interactive execution policy.
type RetryPolicy struct {
	MaxAttempts uint
	Unlimited   bool
}

// CancelBehavior defines the result of an explicit cancel event.
type CancelBehavior uint8

const (
	CancelReturnError CancelBehavior = iota
	CancelUseDefault
	CancelUseFallback
)

// EOFBehavior defines the result of end-of-input or Ctrl-D.
type EOFBehavior uint8

const (
	EOFReturnError EOFBehavior = iota
	EOFUseDefault
	EOFUseFallback
)

// SecretClass controls redaction and terminal echo policy.
type SecretClass uint8

const (
	SecretNone SecretClass = iota
	SecretPassword
	SecretToken
	SecretOther
)

// Descriptor is an immutable value snapshot used by renderers and tests.
type Descriptor struct {
	Kind          PromptKind
	ID            string
	Label         string
	Description   string
	Placeholder   string
	Hint          string
	Help          string
	Retry         RetryPolicy
	Cancel        CancelBehavior
	EndOfInput    EOFBehavior
	Secret        SecretClass
	Headless      HeadlessBehavior
	Accessibility Accessibility
}

func normalizeBehavior(retry RetryPolicy, cancel CancelBehavior, eof EOFBehavior, secret SecretClass) (RetryPolicy, error) {
	if retry.Unlimited && retry.MaxAttempts != 0 {
		return RetryPolicy{}, fmt.Errorf("%w: retry cannot be both bounded and unlimited", ErrInvalidDefinition)
	}
	if !retry.Unlimited && retry.MaxAttempts == 0 {
		retry.MaxAttempts = 3
	}
	if cancel > CancelUseFallback || eof > EOFUseFallback || secret > SecretOther {
		return RetryPolicy{}, fmt.Errorf("%w: invalid execution behavior", ErrInvalidDefinition)
	}

	return retry, nil
}

func invalidBehaviorDefinition(operation string, promptID string, cause error) error {
	return &Error{Kind: ErrorInvalidDefinition, Operation: operation, PromptID: promptID, Cause: cause}
}
