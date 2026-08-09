package opensearch

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

const (
	DefaultMaximumInFlight         = 64
	DefaultCircuitFailureThreshold = 5
	DefaultCircuitOpenDuration     = 30 * time.Second
	maximumInFlight                = 4_096
	maximumQueued                  = 65_536
)

// ResilienceConfig bounds process-local concurrency and overload amplification.
// Zero values select bounded defaults; queuing is disabled unless both queue
// fields are configured. The adapter never performs retries.
type ResilienceConfig struct {
	MaximumInFlight         int
	MaximumQueued           int
	MaximumQueueWait        time.Duration
	CircuitFailureThreshold int
	CircuitOpenDuration     time.Duration
	Clock                   func() time.Time
}

type ResilienceSnapshot struct {
	MaximumInFlight     int
	InFlight            int
	Queued              int
	Admissions          uint64
	Rejections          uint64
	CircuitOpen         bool
	ConsecutiveFailures int
}

type resilienceController struct {
	tokens           chan struct{}
	maximumInFlight  int
	maximumQueued    int
	maximumQueueWait time.Duration
	failureThreshold int
	openDuration     time.Duration
	now              func() time.Time

	mu                  sync.Mutex
	queued              int
	admissions          uint64
	rejections          uint64
	consecutiveFailures int
	openUntil           time.Time
	halfOpenActive      bool
}

func newResilienceController(config ResilienceConfig) (*resilienceController, error) {
	if config.CircuitOpenDuration < 0 {
		return nil, ErrInvalidConfig
	}
	if config.MaximumInFlight == 0 {
		config.MaximumInFlight = DefaultMaximumInFlight
	}
	if config.CircuitFailureThreshold == 0 {
		config.CircuitFailureThreshold = DefaultCircuitFailureThreshold
	}
	if config.CircuitOpenDuration == 0 {
		config.CircuitOpenDuration = DefaultCircuitOpenDuration
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.MaximumInFlight < 1 || config.MaximumInFlight > maximumInFlight {
		return nil, ErrInvalidConfig
	}
	if config.MaximumQueued < 0 || config.MaximumQueued > maximumQueued {
		return nil, ErrInvalidConfig
	}
	if (config.MaximumQueued == 0 && config.MaximumQueueWait != 0) ||
		(config.MaximumQueued > 0 && config.MaximumQueueWait <= 0) ||
		config.CircuitFailureThreshold < 1 {
		return nil, ErrInvalidConfig
	}
	return &resilienceController{
		tokens: make(chan struct{}, config.MaximumInFlight), maximumInFlight: config.MaximumInFlight,
		maximumQueued: config.MaximumQueued, maximumQueueWait: config.MaximumQueueWait,
		failureThreshold: config.CircuitFailureThreshold, openDuration: config.CircuitOpenDuration, now: config.Clock,
	}, nil
}

type resiliencePermit struct {
	controller *resilienceController
	probe      bool
	once       sync.Once
}

func (controller *resilienceController) acquire(ctx context.Context) (*resiliencePermit, error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	probe, err := controller.acquireCircuit()
	if err != nil {
		return nil, err
	}
	select {
	case controller.tokens <- struct{}{}:
		controller.admitted()
		return &resiliencePermit{controller: controller, probe: probe}, nil
	default:
	}
	controller.mu.Lock()
	if controller.maximumQueued == 0 || controller.queued >= controller.maximumQueued {
		controller.rejections++
		if probe {
			controller.halfOpenActive = false
		}
		controller.mu.Unlock()
		return nil, ErrBackpressure
	}
	controller.queued++
	controller.mu.Unlock()
	waitCtx, cancel := context.WithTimeout(ctx, controller.maximumQueueWait)
	defer cancel()
	select {
	case controller.tokens <- struct{}{}:
		controller.mu.Lock()
		controller.queued--
		controller.admissions++
		controller.mu.Unlock()
		return &resiliencePermit{controller: controller, probe: probe}, nil
	case <-waitCtx.Done():
		controller.mu.Lock()
		controller.queued--
		controller.rejections++
		if probe {
			controller.halfOpenActive = false
		}
		controller.mu.Unlock()
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return nil, ErrBackpressure
		}
		return nil, waitCtx.Err()
	}
}

func (controller *resilienceController) acquireCircuit() (bool, error) {
	now := controller.now()
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.openUntil.IsZero() {
		return false, nil
	}
	if now.Before(controller.openUntil) || controller.halfOpenActive {
		controller.rejections++
		return false, ErrCircuitOpen
	}
	controller.halfOpenActive = true
	return true, nil
}

func (controller *resilienceController) admitted() {
	controller.mu.Lock()
	controller.admissions++
	controller.mu.Unlock()
}

func (permit *resiliencePermit) complete(response *http.Response, err error, downstream bool) {
	permit.once.Do(func() {
		<-permit.controller.tokens
		now := permit.controller.now()
		permit.controller.mu.Lock()
		defer permit.controller.mu.Unlock()
		if !downstream {
			if permit.probe {
				permit.controller.halfOpenActive = false
			}
			return
		}
		failed := err != nil || response != nil && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable)
		if failed {
			permit.controller.consecutiveFailures++
			if permit.probe || permit.controller.consecutiveFailures >= permit.controller.failureThreshold {
				permit.controller.openUntil = now.Add(permit.controller.openDuration)
				permit.controller.halfOpenActive = false
			}
			return
		}
		permit.controller.consecutiveFailures = 0
		permit.controller.openUntil = time.Time{}
		permit.controller.halfOpenActive = false
	})
}

func (c *Client) ResilienceSnapshot() ResilienceSnapshot {
	if c == nil || c.transport == nil || c.transport.resilience == nil {
		return ResilienceSnapshot{}
	}
	return c.transport.resilience.snapshot()
}

func (controller *resilienceController) snapshot() ResilienceSnapshot {
	now := controller.now()
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return ResilienceSnapshot{MaximumInFlight: controller.maximumInFlight, InFlight: len(controller.tokens), Queued: controller.queued, Admissions: controller.admissions, Rejections: controller.rejections, CircuitOpen: !controller.openUntil.IsZero() && now.Before(controller.openUntil), ConsecutiveFailures: controller.consecutiveFailures}
}
