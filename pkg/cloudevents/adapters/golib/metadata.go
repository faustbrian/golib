package golib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/faustbrian/golib/pkg/cloudevents"
	"github.com/faustbrian/golib/pkg/correlation"
	telemetrypropagation "github.com/faustbrian/golib/pkg/telemetry/propagation"
	"github.com/faustbrian/golib/pkg/tenancy"
)

var (
	// ErrInvalidAdapterInput classifies an absent or malformed adapter input.
	ErrInvalidAdapterInput = errors.New("cloudevents golib adapter: invalid input")
	// ErrMetadataCollision reports canonical metadata that conflicts with an
	// existing CloudEvents extension and therefore cannot be overwritten.
	ErrMetadataCollision = errors.New("cloudevents golib adapter: metadata collision")
	// ErrUntrustedMetadata reports an attempt to adopt inbound identity metadata
	// without an explicit trust decision.
	ErrUntrustedMetadata = errors.New("cloudevents golib adapter: metadata is untrusted")
)

const (
	correlationIDExtension = "correlationid"
	requestIDExtension     = "requestid"
	causationIDExtension   = "causationid"
	tenantIDExtension      = "tenantid"
)

// Loss describes one value that a selected target cannot represent.
type Loss struct {
	Field  string
	Reason string
}

// Report makes every conversion loss explicit. An empty Losses slice means
// the selected operation retained all of the metadata it owned.
type Report struct {
	Losses []Loss
}

// AddCorrelation returns a new event with Golib correlation identifiers. It
// never overwrites an existing, different extension value.
func AddCorrelation(event cloudevents.Event, values correlation.Values) (cloudevents.Event, error) {
	additions := map[string]string{}
	if values.CorrelationID != "" {
		additions[correlationIDExtension] = values.CorrelationID.String()
	}
	if values.RequestID != "" {
		additions[requestIDExtension] = values.RequestID.String()
	}
	if values.CausationID != "" {
		additions[causationIDExtension] = values.CausationID.String()
	}
	return addStringExtensions(event, additions)
}

// ExtractCorrelation parses correlation extensions only after the caller
// explicitly marks the inbound metadata trusted.
func ExtractCorrelation(
	event cloudevents.Event,
	trusted bool,
	policy correlation.Policy,
) (correlation.Values, error) {
	correlationValue, hasCorrelation, err := stringExtension(event, correlationIDExtension)
	if err != nil {
		return correlation.Values{}, err
	}
	requestValue, hasRequest, err := stringExtension(event, requestIDExtension)
	if err != nil {
		return correlation.Values{}, err
	}
	causationValue, hasCausation, err := stringExtension(event, causationIDExtension)
	if err != nil {
		return correlation.Values{}, err
	}
	if !hasCorrelation && !hasRequest && !hasCausation {
		return correlation.Values{}, nil
	}
	if !trusted {
		return correlation.Values{}, ErrUntrustedMetadata
	}

	var values correlation.Values
	if hasCorrelation {
		values.CorrelationID, err = correlation.ParseCorrelationID(correlationValue, policy)
		if err != nil {
			return correlation.Values{}, fmt.Errorf("%w: correlationid: %w", ErrInvalidAdapterInput, err)
		}
	}
	if hasRequest {
		values.RequestID, err = correlation.ParseRequestID(requestValue, policy)
		if err != nil {
			return correlation.Values{}, fmt.Errorf("%w: requestid: %w", ErrInvalidAdapterInput, err)
		}
	}
	if hasCausation {
		values.CausationID, err = correlation.ParseCausationID(causationValue, policy)
		if err != nil {
			return correlation.Values{}, fmt.Errorf("%w: causationid: %w", ErrInvalidAdapterInput, err)
		}
	}
	return values, nil
}

// AddTenant returns a new event with one validated tenant routing extension.
// The tenant remains routing data and is not authentication or authorization.
func AddTenant(event cloudevents.Event, tenant tenancy.TenantID) (cloudevents.Event, error) {
	if !tenant.Valid() {
		return cloudevents.Event{}, fmt.Errorf("%w: tenant", ErrInvalidAdapterInput)
	}
	return addStringExtensions(event, map[string]string{tenantIDExtension: tenant.Value()})
}

// ExtractTenant adopts the tenant extension only after an explicit trust
// decision. Applications must still authenticate and authorize the tenant.
func ExtractTenant(event cloudevents.Event, trusted bool) (tenancy.TenantID, error) {
	value, present, err := stringExtension(event, tenantIDExtension)
	if err != nil {
		return tenancy.TenantID{}, err
	}
	if !present {
		return tenancy.TenantID{}, fmt.Errorf(
			"%w: tenantid: %w",
			ErrInvalidAdapterInput,
			tenancy.ErrTenantMetadataMissing,
		)
	}
	if !trusted {
		return tenancy.TenantID{}, ErrUntrustedMetadata
	}
	tenant, err := tenancy.ParseTenantID(value)
	if err != nil {
		return tenancy.TenantID{}, fmt.Errorf("%w: tenantid: %w", ErrInvalidAdapterInput, err)
	}
	return tenant, nil
}

// InjectTraceContext uses Golib's caller-owned telemetry propagation policy and
// returns a new event. Baggage has no selected CloudEvents extension and is
// reported rather than silently flattened.
func InjectTraceContext(
	ctx context.Context,
	event cloudevents.Event,
	policy *telemetrypropagation.Policy,
) (cloudevents.Event, Report, error) {
	if ctx == nil || policy == nil {
		return cloudevents.Event{}, Report{}, fmt.Errorf("%w: telemetry policy", ErrInvalidAdapterInput)
	}
	carrier := metadataCarrier{}
	policy.Inject(ctx, carrier)
	additions := map[string]string{}
	for _, name := range []string{"traceparent", "tracestate"} {
		if value := carrier.Get(name); value != "" {
			additions[name] = value
		}
	}
	converted, err := addStringExtensions(event, additions)
	if err != nil {
		return cloudevents.Event{}, Report{}, err
	}
	report := Report{}
	if carrier.Get("baggage") != "" {
		report.Losses = []Loss{{Field: "baggage", Reason: "no selected CloudEvents extension"}}
	}
	return converted, report, nil
}

// ExtractTraceContext delegates inbound trust handling to Golib's telemetry
// propagation policy. It does not modify the event or perform I/O.
func ExtractTraceContext(
	ctx context.Context,
	event cloudevents.Event,
	policy *telemetrypropagation.Policy,
	trusted bool,
) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if policy == nil {
		return ctx
	}
	carrier := metadataCarrier{}
	for _, name := range []string{"traceparent", "tracestate"} {
		value, present, _ := stringExtension(event, name)
		if present {
			carrier.Set(name, value)
		}
	}
	if trusted {
		return policy.ExtractTrusted(ctx, carrier)
	}
	return policy.Extract(ctx, carrier)
}

func addStringExtensions(
	event cloudevents.Event,
	additions map[string]string,
) (cloudevents.Event, error) {
	if err := event.Validate(); err != nil {
		return cloudevents.Event{}, fmt.Errorf("%w: event: %w", ErrInvalidAdapterInput, err)
	}
	extensions := event.Extensions()
	for name, value := range additions {
		attribute, err := cloudevents.NewStringAttribute(value)
		if err != nil {
			return cloudevents.Event{}, fmt.Errorf("%w: extension %s: %w", ErrInvalidAdapterInput, name, err)
		}
		if existing, present := extensions[name]; present {
			if !attributesEqual(existing, attribute) {
				return cloudevents.Event{}, fmt.Errorf("%w: %s", ErrMetadataCollision, name)
			}
		} else {
			extensions[name] = attribute
		}
	}
	return rebuildEvent(event, extensions)
}

func stringExtension(event cloudevents.Event, name string) (string, bool, error) {
	attribute, present := event.Extension(name)
	if !present {
		return "", false, nil
	}
	if attribute.Kind() != cloudevents.AttributeString || attribute.String() == "" {
		return "", false, fmt.Errorf("%w: extension %s", ErrInvalidAdapterInput, name)
	}
	return attribute.String(), true, nil
}

func attributesEqual(left, right cloudevents.Attribute) bool {
	return left.Kind() == right.Kind() && left.String() == right.String() && bytes.Equal(left.Bytes(), right.Bytes())
}

func rebuildEvent(
	event cloudevents.Event,
	extensions map[string]cloudevents.Attribute,
) (cloudevents.Event, error) {
	dataContentType, _ := event.DataContentType()
	dataSchema, _ := event.DataSchema()
	subject, _ := event.Subject()
	timeValue, hasTime := event.Time()
	var occurredAt *time.Time
	if hasTime {
		occurredAt = &timeValue
	}
	return cloudevents.NewEvent(cloudevents.Attributes{
		ID: event.ID(), Source: event.Source(), Type: event.Type(),
		DataContentType: dataContentType, DataSchema: dataSchema, Subject: subject,
		Time: occurredAt, Extensions: extensions,
	}, event.Data())
}

type metadataCarrier map[string]string

func (carrier metadataCarrier) Get(key string) string { return carrier[key] }

func (carrier metadataCarrier) Set(key, value string) { carrier[key] = value }

func (carrier metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(carrier))
	for key := range carrier {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
