package country

import (
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
)

func TestUnknownFutureStatusIsRejected(t *testing.T) {
	t.Parallel()
	if allowed(international.Status(255), ParseOptions{
		AllowHistoric: true, AllowReserved: true, AllowUserAssigned: true,
	}) {
		t.Fatal("unknown future status was accepted")
	}
}

func TestUnmappedInternalNumericHasNoAlpha2(t *testing.T) {
	t.Parallel()
	if _, ok := (Numeric{value: "000"}).Alpha2(); ok {
		t.Fatal("unmapped numeric returned alpha-2")
	}
}
