package httpclient

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidPolicyProfile indicates an unknown profile or invalid override.
	ErrInvalidPolicyProfile = errors.New("invalid HTTP policy profile")
)

// PolicyProfileID is a stable name and major version for a built-in policy.
type PolicyProfileID string

const (
	PolicyProfileInteractiveV1     PolicyProfileID = "interactive/v1"
	PolicyProfileBatchV1           PolicyProfileID = "batch/v1"
	PolicyProfileStreamingV1       PolicyProfileID = "streaming/v1"
	PolicyProfileWebhookDeliveryV1 PolicyProfileID = "webhook-delivery/v1"
	defaultPolicyProfile                           = PolicyProfileInteractiveV1
	maximumProfileDuration                         = 24 * time.Hour
	maximumProfileBodyBytes        int64           = 1 << 40
)

// PolicyField identifies one inspectable resolved value.
type PolicyField string

const (
	PolicyFieldOperationTimeout            PolicyField = "operation_timeout"
	PolicyFieldRetryMaximumAttempts        PolicyField = "retry_maximum_attempts"
	PolicyFieldRetryMaximumElapsed         PolicyField = "retry_maximum_elapsed"
	PolicyFieldPoolConcurrency             PolicyField = "pool_concurrency"
	PolicyFieldPoolMaximumElapsed          PolicyField = "pool_maximum_elapsed"
	PolicyFieldTransportMaximumConnections PolicyField = "transport_maximum_connections"
	PolicyFieldLimiterMaximumWait          PolicyField = "limiter_maximum_wait"
	PolicyFieldBreakerOpenTimeout          PolicyField = "breaker_open_timeout"
	PolicyFieldCacheMaximumBodyBytes       PolicyField = "cache_maximum_body_bytes"
	PolicyFieldBodyMaximumBytes            PolicyField = "body_maximum_bytes"
	PolicyFieldShutdownTimeout             PolicyField = "shutdown_timeout"
)

// PolicySource identifies the precedence layer that supplied a value.
type PolicySource string

const (
	PolicySourceProfile PolicySource = "profile"
	PolicySourceClient  PolicySource = "client"
	PolicySourceRequest PolicySource = "request"
)

// PolicyValues contains finite defaults resolved for one logical operation.
type PolicyValues struct {
	OperationTimeout            time.Duration
	RetryMaximumAttempts        int
	RetryMaximumElapsed         time.Duration
	PoolConcurrency             int
	PoolMaximumElapsed          time.Duration
	TransportMaximumConnections int
	LimiterMaximumWait          time.Duration
	BreakerOpenTimeout          time.Duration
	CacheMaximumBodyBytes       int64
	BodyMaximumBytes            int64
	ShutdownTimeout             time.Duration
}

// PolicyOverrides uses pointers so an explicit value is distinguishable from
// an omitted field. Every supplied value must be positive and bounded.
type PolicyOverrides struct {
	OperationTimeout            *time.Duration
	RetryMaximumAttempts        *int
	RetryMaximumElapsed         *time.Duration
	PoolConcurrency             *int
	PoolMaximumElapsed          *time.Duration
	TransportMaximumConnections *int
	LimiterMaximumWait          *time.Duration
	BreakerOpenTimeout          *time.Duration
	CacheMaximumBodyBytes       *int64
	BodyMaximumBytes            *int64
	ShutdownTimeout             *time.Duration
}

// ResolvedPolicy is an immutable policy and provenance snapshot.
type ResolvedPolicy struct {
	profile    PolicyProfileID
	version    int
	values     PolicyValues
	provenance map[PolicyField]PolicySource
}

// Profile returns the stable profile identifier.
func (policy ResolvedPolicy) Profile() PolicyProfileID { return policy.profile }

// Version returns the profile schema major version.
func (policy ResolvedPolicy) Version() int { return policy.version }

// Values returns a value copy of all resolved finite limits.
func (policy ResolvedPolicy) Values() PolicyValues { return policy.values }

// Provenance returns the source of a resolved field, or an empty source for an
// unknown field.
func (policy ResolvedPolicy) Provenance(field PolicyField) PolicySource {
	return policy.provenance[field]
}

// ProvenanceSnapshot returns an independently mutable provenance map.
func (policy ResolvedPolicy) ProvenanceSnapshot() map[PolicyField]PolicySource {
	snapshot := make(map[PolicyField]PolicySource, len(policy.provenance))
	for field, source := range policy.provenance {
		snapshot[field] = source
	}
	return snapshot
}

// ResolvePolicy applies deterministic profile, client, then request precedence.
func ResolvePolicy(
	profile PolicyProfileID,
	client PolicyOverrides,
	request PolicyOverrides,
) (ResolvedPolicy, error) {
	if profile == "" {
		profile = defaultPolicyProfile
	}
	values, ok := builtInPolicyValues(profile)
	if !ok {
		return ResolvedPolicy{}, fmt.Errorf("%w: unknown profile", ErrInvalidPolicyProfile)
	}
	provenance := make(map[PolicyField]PolicySource, len(policyFields()))
	for _, field := range policyFields() {
		provenance[field] = PolicySourceProfile
	}
	if err := applyPolicyOverrides(&values, provenance, client, PolicySourceClient); err != nil {
		return ResolvedPolicy{}, err
	}
	if err := applyPolicyOverrides(&values, provenance, request, PolicySourceRequest); err != nil {
		return ResolvedPolicy{}, err
	}
	return ResolvedPolicy{profile: profile, version: 1, values: values, provenance: provenance}, nil
}

// WithPolicyOverrides attaches an immutable per-request override snapshot.
func WithPolicyOverrides(ctx context.Context, overrides PolicyOverrides) (context.Context, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is nil", ErrInvalidPolicyProfile)
	}
	if err := validatePolicyOverrides(overrides); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, policyOverridesContextKey{}, clonePolicyOverrides(overrides)), nil
}

// ResolvedPolicyFromContext returns an immutable operation policy snapshot.
func ResolvedPolicyFromContext(ctx context.Context) (ResolvedPolicy, bool) {
	if ctx == nil {
		return ResolvedPolicy{}, false
	}
	policy, ok := ctx.Value(resolvedPolicyContextKey{}).(ResolvedPolicy)
	if !ok {
		return ResolvedPolicy{}, false
	}
	policy.provenance = policy.ProvenanceSnapshot()
	return policy, true
}

func requestPolicyOverrides(ctx context.Context) PolicyOverrides {
	if ctx == nil {
		return PolicyOverrides{}
	}
	overrides, _ := ctx.Value(policyOverridesContextKey{}).(PolicyOverrides)
	return clonePolicyOverrides(overrides)
}

func builtInPolicyValues(profile PolicyProfileID) (PolicyValues, bool) {
	switch profile {
	case PolicyProfileInteractiveV1:
		return PolicyValues{
			OperationTimeout: 30 * time.Second, RetryMaximumAttempts: 3,
			RetryMaximumElapsed: 10 * time.Second, PoolConcurrency: 4,
			PoolMaximumElapsed: 5 * time.Minute, TransportMaximumConnections: 100,
			LimiterMaximumWait: 2 * time.Second, BreakerOpenTimeout: 30 * time.Second,
			CacheMaximumBodyBytes: 8 << 20, BodyMaximumBytes: 8 << 20,
			ShutdownTimeout: 10 * time.Second,
		}, true
	case PolicyProfileBatchV1:
		return PolicyValues{
			OperationTimeout: 5 * time.Minute, RetryMaximumAttempts: 5,
			RetryMaximumElapsed: time.Minute, PoolConcurrency: 16,
			PoolMaximumElapsed: 30 * time.Minute, TransportMaximumConnections: 100,
			LimiterMaximumWait: 30 * time.Second, BreakerOpenTimeout: time.Minute,
			CacheMaximumBodyBytes: 16 << 20, BodyMaximumBytes: 64 << 20,
			ShutdownTimeout: 30 * time.Second,
		}, true
	case PolicyProfileStreamingV1:
		return PolicyValues{
			OperationTimeout: 30 * time.Minute, RetryMaximumAttempts: 3,
			RetryMaximumElapsed: 30 * time.Second, PoolConcurrency: 4,
			PoolMaximumElapsed: time.Hour, TransportMaximumConnections: 32,
			LimiterMaximumWait: 30 * time.Second, BreakerOpenTimeout: time.Minute,
			CacheMaximumBodyBytes: 8 << 20, BodyMaximumBytes: 1 << 30,
			ShutdownTimeout: time.Minute,
		}, true
	case PolicyProfileWebhookDeliveryV1:
		return PolicyValues{
			OperationTimeout: 15 * time.Second, RetryMaximumAttempts: 5,
			RetryMaximumElapsed: 30 * time.Second, PoolConcurrency: 32,
			PoolMaximumElapsed: 10 * time.Minute, TransportMaximumConnections: 256,
			LimiterMaximumWait: 5 * time.Second, BreakerOpenTimeout: 30 * time.Second,
			CacheMaximumBodyBytes: 1 << 20, BodyMaximumBytes: 1 << 20,
			ShutdownTimeout: 30 * time.Second,
		}, true
	default:
		return PolicyValues{}, false
	}
}

func applyPolicyOverrides(
	values *PolicyValues,
	provenance map[PolicyField]PolicySource,
	overrides PolicyOverrides,
	source PolicySource,
) error {
	if err := validatePolicyOverrides(overrides); err != nil {
		return err
	}
	applyDurationOverride(&values.OperationTimeout, overrides.OperationTimeout, provenance, PolicyFieldOperationTimeout, source)
	applyIntOverride(&values.RetryMaximumAttempts, overrides.RetryMaximumAttempts, provenance, PolicyFieldRetryMaximumAttempts, source)
	applyDurationOverride(&values.RetryMaximumElapsed, overrides.RetryMaximumElapsed, provenance, PolicyFieldRetryMaximumElapsed, source)
	applyIntOverride(&values.PoolConcurrency, overrides.PoolConcurrency, provenance, PolicyFieldPoolConcurrency, source)
	applyDurationOverride(&values.PoolMaximumElapsed, overrides.PoolMaximumElapsed, provenance, PolicyFieldPoolMaximumElapsed, source)
	applyIntOverride(&values.TransportMaximumConnections, overrides.TransportMaximumConnections, provenance, PolicyFieldTransportMaximumConnections, source)
	applyDurationOverride(&values.LimiterMaximumWait, overrides.LimiterMaximumWait, provenance, PolicyFieldLimiterMaximumWait, source)
	applyDurationOverride(&values.BreakerOpenTimeout, overrides.BreakerOpenTimeout, provenance, PolicyFieldBreakerOpenTimeout, source)
	applyBytesOverride(&values.CacheMaximumBodyBytes, overrides.CacheMaximumBodyBytes, provenance, PolicyFieldCacheMaximumBodyBytes, source)
	applyBytesOverride(&values.BodyMaximumBytes, overrides.BodyMaximumBytes, provenance, PolicyFieldBodyMaximumBytes, source)
	applyDurationOverride(&values.ShutdownTimeout, overrides.ShutdownTimeout, provenance, PolicyFieldShutdownTimeout, source)
	return nil
}

func validatePolicyOverrides(overrides PolicyOverrides) error {
	for field, value := range map[PolicyField]*time.Duration{
		PolicyFieldOperationTimeout:    overrides.OperationTimeout,
		PolicyFieldRetryMaximumElapsed: overrides.RetryMaximumElapsed,
		PolicyFieldPoolMaximumElapsed:  overrides.PoolMaximumElapsed,
		PolicyFieldLimiterMaximumWait:  overrides.LimiterMaximumWait,
		PolicyFieldBreakerOpenTimeout:  overrides.BreakerOpenTimeout,
		PolicyFieldShutdownTimeout:     overrides.ShutdownTimeout,
	} {
		if value != nil && (*value <= 0 || *value > maximumProfileDuration) {
			return fmt.Errorf("%w: %s is out of bounds", ErrInvalidPolicyProfile, field)
		}
	}
	for field, value := range map[PolicyField]*int{
		PolicyFieldRetryMaximumAttempts:        overrides.RetryMaximumAttempts,
		PolicyFieldPoolConcurrency:             overrides.PoolConcurrency,
		PolicyFieldTransportMaximumConnections: overrides.TransportMaximumConnections,
	} {
		maximum := maximumPoolPending
		switch field {
		case PolicyFieldRetryMaximumAttempts:
			maximum = maximumRetryAttempts
		case PolicyFieldPoolConcurrency:
			maximum = maximumPoolConcurrency
		}
		if value != nil && (*value <= 0 || *value > maximum) {
			return fmt.Errorf("%w: %s is out of bounds", ErrInvalidPolicyProfile, field)
		}
	}
	for field, value := range map[PolicyField]*int64{
		PolicyFieldCacheMaximumBodyBytes: overrides.CacheMaximumBodyBytes,
		PolicyFieldBodyMaximumBytes:      overrides.BodyMaximumBytes,
	} {
		if value != nil && (*value <= 0 || *value > maximumProfileBodyBytes) {
			return fmt.Errorf("%w: %s is out of bounds", ErrInvalidPolicyProfile, field)
		}
	}
	return nil
}

func policyFields() []PolicyField {
	return []PolicyField{
		PolicyFieldOperationTimeout, PolicyFieldRetryMaximumAttempts,
		PolicyFieldRetryMaximumElapsed, PolicyFieldPoolConcurrency,
		PolicyFieldPoolMaximumElapsed, PolicyFieldTransportMaximumConnections,
		PolicyFieldLimiterMaximumWait, PolicyFieldBreakerOpenTimeout,
		PolicyFieldCacheMaximumBodyBytes, PolicyFieldBodyMaximumBytes,
		PolicyFieldShutdownTimeout,
	}
}

func applyDurationOverride(target *time.Duration, value *time.Duration, provenance map[PolicyField]PolicySource, field PolicyField, source PolicySource) {
	if value != nil {
		*target = *value
		provenance[field] = source
	}
}

func applyIntOverride(target *int, value *int, provenance map[PolicyField]PolicySource, field PolicyField, source PolicySource) {
	if value != nil {
		*target = *value
		provenance[field] = source
	}
}

func applyBytesOverride(target *int64, value *int64, provenance map[PolicyField]PolicySource, field PolicyField, source PolicySource) {
	if value != nil {
		*target = *value
		provenance[field] = source
	}
}

func clonePolicyOverrides(overrides PolicyOverrides) PolicyOverrides {
	return PolicyOverrides{
		OperationTimeout:            cloneDuration(overrides.OperationTimeout),
		RetryMaximumAttempts:        cloneInt(overrides.RetryMaximumAttempts),
		RetryMaximumElapsed:         cloneDuration(overrides.RetryMaximumElapsed),
		PoolConcurrency:             cloneInt(overrides.PoolConcurrency),
		PoolMaximumElapsed:          cloneDuration(overrides.PoolMaximumElapsed),
		TransportMaximumConnections: cloneInt(overrides.TransportMaximumConnections),
		LimiterMaximumWait:          cloneDuration(overrides.LimiterMaximumWait),
		BreakerOpenTimeout:          cloneDuration(overrides.BreakerOpenTimeout),
		CacheMaximumBodyBytes:       cloneInt64(overrides.CacheMaximumBodyBytes),
		BodyMaximumBytes:            cloneInt64(overrides.BodyMaximumBytes),
		ShutdownTimeout:             cloneDuration(overrides.ShutdownTimeout),
	}
}

func cloneDuration(value *time.Duration) *time.Duration {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

type policyOverridesContextKey struct{}
type resolvedPolicyContextKey struct{}
