package prompts

import (
	"errors"
	"testing"
)

func TestFormSecretLeakIgnoresEmptySecretValues(t *testing.T) {
	t.Parallel()

	byteSecret := NewSecretBytes(nil)
	defer byteSecret.Destroy()
	result := FormResult{
		values: map[string]storedFormValue{
			"text": {value: SecretValue{}},
			"bytes": {value: byteSecret},
		},
	}
	fields, leaked := formSecretLeak(result, errors.New("validation failed"))
	if leaked || fields != nil {
		t.Fatalf("formSecretLeak() = %v, %t", fields, leaked)
	}
}
