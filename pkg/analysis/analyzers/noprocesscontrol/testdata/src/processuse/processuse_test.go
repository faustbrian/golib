package processuse

import (
	"log"
	"os"
	"testing"
)

func TestProcessFixtures(t *testing.T) {
	t.Run("panic callback", func(t *testing.T) {
		panic("fixture")
	})
	if false {
		log.Fatal("unreachable fixture")
		os.Exit(1)
	}
}
