package keyphrase_test

import (
	"fmt"
	"strings"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
)

func TestSecretFormattingIsAlwaysRedacted(t *testing.T) {
	t.Parallel()

	secret := keyphrase.Secret([]byte("sensitive-value"))
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%x", "%q"} {
		formatted := fmt.Sprintf(format, secret)
		if strings.Contains(formatted, "sensitive") || strings.Contains(formatted, "73656e736974697665") {
			t.Fatalf("format %q disclosed the secret", format)
		}
		if !strings.Contains(formatted, "redacted") {
			t.Fatalf("format %q was not explicitly redacted: %q", format, formatted)
		}
	}
	secret.Clear()
	for _, value := range secret {
		if value != 0 {
			t.Fatal("Clear() left secret bytes behind")
		}
	}
}
