package valkey_test

import (
	"errors"
	"testing"

	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"

	cache "github.com/faustbrian/golib/pkg/cache"
	valkeybackend "github.com/faustbrian/golib/pkg/cache/backend/valkey"
)

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	client := valkeymock.NewClient(gomock.NewController(t))
	tests := map[string]valkeybackend.Config{
		"nil client":   {Clock: cache.SystemClock{}, MaxRecordSize: 128},
		"nil clock":    {Client: client, MaxRecordSize: 128},
		"invalid size": {Client: client, Clock: cache.SystemClock{}},
	}
	for name, config := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := valkeybackend.New(config); !errors.Is(err, cache.ErrInvalidConfig) {
				t.Fatalf("New returned %v, want ErrInvalidConfig", err)
			}
		})
	}
}
