package identifier_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
)

func TestRootBoundaryCoverage(t *testing.T) {
	now := time.Unix(1, 2)
	clock := identifier.ClockFunc(func() time.Time { return now })
	if !clock.Now().Equal(now) {
		t.Fatal("ClockFunc did not delegate")
	}
	if _, err := identifier.Parse[userTag](""); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("empty parse error = %v", err)
	}
	var id identifier.ID[userTag]
	if err := json.Unmarshal([]byte("42"), &id); err == nil {
		t.Fatal("expected JSON type error")
	}
}
