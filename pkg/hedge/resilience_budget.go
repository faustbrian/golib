package hedge

import (
	"sync"

	"github.com/faustbrian/golib/pkg/resilience"
)

type sharedWorkPermit struct {
	permit resilience.Permit
	once   sync.Once
}

func wrapResiliencePermit(permit resilience.Permit) Permit {
	if permit == nil {
		return nil
	}
	return &sharedWorkPermit{permit: permit}
}

func (permit *sharedWorkPermit) Release() {
	if permit == nil {
		return
	}
	permit.once.Do(func() { _ = permit.permit.Complete() })
}
