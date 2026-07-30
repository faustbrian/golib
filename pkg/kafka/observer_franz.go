package kafka

import (
	"context"
	"math"
	"net"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type franzObserverHook struct {
	clientID   string
	groupID    string
	dispatcher observerDispatcher
	now        func() time.Time
	before     func()
	after      func()
}

var (
	_ kgo.HookBrokerConnect    = (*franzObserverHook)(nil)
	_ kgo.HookBrokerDisconnect = (*franzObserverHook)(nil)
	_ kgo.HookBrokerE2E        = (*franzObserverHook)(nil)
	_ kgo.HookBrokerThrottle   = (*franzObserverHook)(nil)
	_ kgo.HookGroupManageError = (*franzObserverHook)(nil)
)

func newFranzObserverHook(
	clientID string,
	groupID string,
	dispatcher observerDispatcher,
) *franzObserverHook {
	return &franzObserverHook{
		clientID:   strings.Clone(clientID),
		groupID:    strings.Clone(groupID),
		dispatcher: dispatcher,
		now:        time.Now,
	}
}

func (hook *franzObserverHook) OnBrokerConnect(
	meta kgo.BrokerMetadata,
	duration time.Duration,
	_ net.Conn,
	err error,
) {
	finishedAt := hook.now()
	brokerID, brokerKnown := observedBrokerID(meta.NodeID)
	duration, truncated := observedDuration(duration)
	observation := Observation{
		Kind:        ObservationBrokerConnect,
		StartedAt:   finishedAt.Add(-duration),
		Duration:    duration,
		ClientID:    hook.clientID,
		GroupID:     hook.groupID,
		BrokerID:    brokerID,
		BrokerKnown: brokerKnown,
		Succeeded:   err == nil,
		Truncated:   truncated,
	}
	if err != nil {
		observation.Category = classifyError(err)
	}
	hook.observe(observation)
}

func (hook *franzObserverHook) OnBrokerThrottle(
	meta kgo.BrokerMetadata,
	duration time.Duration,
	throttledAfterResponse bool,
) {
	brokerID, brokerKnown := observedBrokerID(meta.NodeID)
	duration, truncated := observedDuration(duration)
	hook.observe(Observation{
		Kind:                   ObservationBrokerThrottle,
		StartedAt:              hook.now(),
		ClientID:               hook.clientID,
		GroupID:                hook.groupID,
		BrokerID:               brokerID,
		BrokerKnown:            brokerKnown,
		ThrottleDuration:       duration,
		ThrottledAfterResponse: throttledAfterResponse,
		Succeeded:              true,
		Truncated:              truncated,
	})
}

func (hook *franzObserverHook) OnBrokerDisconnect(
	meta kgo.BrokerMetadata,
	_ net.Conn,
) {
	brokerID, brokerKnown := observedBrokerID(meta.NodeID)
	hook.observe(Observation{
		Kind:        ObservationBrokerDisconnect,
		StartedAt:   hook.now(),
		ClientID:    hook.clientID,
		GroupID:     hook.groupID,
		BrokerID:    brokerID,
		BrokerKnown: brokerKnown,
		Succeeded:   true,
	})
}

func (hook *franzObserverHook) OnGroupManageError(err error) {
	hook.observe(Observation{
		Kind:      ObservationConsumeGroupError,
		StartedAt: hook.now(),
		ClientID:  hook.clientID,
		GroupID:   hook.groupID,
		Succeeded: false,
		Category:  classifyError(err),
	})
}

func (hook *franzObserverHook) OnBrokerE2E(
	meta kgo.BrokerMetadata,
	apiKey int16,
	e2e kgo.BrokerE2E,
) {
	finishedAt := hook.now()
	duration, durationTruncated := observedRequestDuration(e2e)
	queueDuration, queueTruncated := observedDuration(e2e.WriteWait)
	requestBytes, requestTruncated := observedBytes(e2e.BytesWritten)
	responseBytes, responseTruncated := observedBytes(e2e.BytesRead)
	brokerID, brokerKnown := observedBrokerID(meta.NodeID)
	observedAPIKey, apiKeyKnown := observedAPIKey(apiKey)
	observation := Observation{
		Kind:          ObservationBrokerRequest,
		StartedAt:     finishedAt.Add(-duration),
		Duration:      duration,
		ClientID:      hook.clientID,
		GroupID:       hook.groupID,
		BrokerID:      brokerID,
		BrokerKnown:   brokerKnown,
		APIKey:        observedAPIKey,
		APIKeyKnown:   apiKeyKnown,
		RequestBytes:  requestBytes,
		ResponseBytes: responseBytes,
		QueueDuration: queueDuration,
		Succeeded:     e2e.Err() == nil,
		Truncated: durationTruncated ||
			queueTruncated ||
			requestTruncated ||
			responseTruncated,
	}
	if err := e2e.Err(); err != nil {
		observation.Category = classifyError(err)
	}
	hook.observe(observation)
}

func (hook *franzObserverHook) observe(observation Observation) {
	if hook.before != nil {
		hook.before()
		defer hook.after()
	}
	hook.dispatcher.observe(context.Background(), observation)
}

func observedBrokerID(nodeID int32) (int32, bool) {
	if nodeID < 0 {
		return 0, false
	}

	return nodeID, true
}

func observedAPIKey(apiKey int16) (int16, bool) {
	if apiKey < 0 {
		return 0, false
	}

	return apiKey, true
}

func observedDuration(duration time.Duration) (time.Duration, bool) {
	if duration < 0 {
		return 0, true
	}

	return duration, false
}

func observedRequestDuration(e2e kgo.BrokerE2E) (time.Duration, bool) {
	var duration time.Duration
	truncated := false
	for _, component := range [...]time.Duration{
		e2e.WriteWait,
		e2e.TimeToWrite,
		e2e.ReadWait,
		e2e.TimeToRead,
	} {
		if component < 0 {
			truncated = true
		} else {
			total := uint64(duration) + uint64(component)
			if total > math.MaxInt64 {
				return time.Duration(math.MaxInt64), true
			}
			duration = time.Duration(total)
		}
	}

	return duration, truncated
}

func observedBytes(bytes int) (int64, bool) {
	if bytes < 0 {
		return 0, true
	}

	return int64(bytes), false
}
