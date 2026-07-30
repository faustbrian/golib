package idtest

import "testing"

func TestSanitizeLogValueEscapesLineBreaks(t *testing.T) {
	t.Parallel()

	if got := sanitizeLogValue("safe\r\nforged"); got != `safe\r\nforged` {
		t.Fatalf("sanitizeLogValue() = %q", got)
	}
}
