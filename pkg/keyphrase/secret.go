package keyphrase

import (
	"fmt"
	"io"
	"log/slog"
)

// Secret is a caller-owned byte slice whose fmt representation is always
// redacted. Convert it explicitly to []byte or string only at a reviewed
// integration boundary.
type Secret []byte

// Clear overwrites the current backing array as a best-effort measure. Go and
// the operating system may retain compiler, runtime, string, or page copies.
func (s Secret) Clear() {
	clear(s)
}

// Format prevents accidental disclosure through logging and debugging verbs.
func (Secret) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "<redacted>")
}

// LogValue prevents disclosure through the standard structured logger.
func (Secret) LogValue() slog.Value {
	return slog.StringValue("<redacted>")
}

// MarshalText prevents disclosure through standard text and JSON encoders.
func (Secret) MarshalText() ([]byte, error) {
	return []byte("<redacted>"), nil
}
