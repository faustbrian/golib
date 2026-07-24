package httpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// ErrInvalidTraceContext indicates malformed W3C propagation fields.
var ErrInvalidTraceContext = errors.New("invalid W3C trace context")

// W3CTraceContext contains validated Trace Context propagation fields.
type W3CTraceContext struct {
	Traceparent string
	Tracestate  string
}

// WithW3CTraceContext attaches a validated immutable W3C Trace Context v00
// snapshot. Baggage is configured independently through TelemetryOptions.
func WithW3CTraceContext(
	ctx context.Context,
	traceparent string,
	tracestate string,
) (context.Context, error) {
	if ctx == nil || !validTraceparent(traceparent) {
		return nil, ErrInvalidTraceContext
	}
	normalizedState, err := normalizeTracestate(tracestate)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, w3cTraceContextKey{}, W3CTraceContext{
		Traceparent: traceparent,
		Tracestate:  normalizedState,
	}), nil
}

// W3CTraceContextFromContext returns a validated trace-context snapshot.
func W3CTraceContextFromContext(ctx context.Context) (W3CTraceContext, bool) {
	if ctx == nil {
		return W3CTraceContext{}, false
	}
	trace, ok := ctx.Value(w3cTraceContextKey{}).(W3CTraceContext)
	return trace, ok
}

// W3CTraceContextPropagator injects context fields into cloned attempt headers.
type W3CTraceContextPropagator struct{}

// Inject implements TelemetryPropagator.
func (W3CTraceContextPropagator) Inject(ctx context.Context, header http.Header) {
	trace, ok := W3CTraceContextFromContext(ctx)
	if !ok {
		return
	}
	header.Set("Traceparent", trace.Traceparent)
	if trace.Tracestate == "" {
		header.Del("Tracestate")
	} else {
		header.Set("Tracestate", trace.Tracestate)
	}
}

func validTraceparent(value string) bool {
	if len(value) != 55 || value[2] != '-' || value[35] != '-' || value[52] != '-' ||
		value[:2] != "00" {
		return false
	}
	traceID := value[3:35]
	parentID := value[36:52]
	flags := value[53:55]
	return lowerHex(traceID) && lowerHex(parentID) && lowerHex(flags) &&
		!allZero(traceID) && !allZero(parentID)
}

func lowerHex(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func allZero(value string) bool {
	return strings.Trim(value, "0") == ""
}

func normalizeTracestate(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 512 {
		return "", ErrInvalidTraceContext
	}
	members := strings.Split(value, ",")
	if len(members) > 32 {
		return "", ErrInvalidTraceContext
	}
	seen := make(map[string]struct{}, len(members))
	for index, raw := range members {
		member := strings.TrimSpace(raw)
		key, memberValue, found := strings.Cut(member, "=")
		if !found || !validTracestateKey(key) || !validTracestateValue(memberValue) {
			return "", ErrInvalidTraceContext
		}
		if _, duplicate := seen[key]; duplicate {
			return "", ErrInvalidTraceContext
		}
		seen[key] = struct{}{}
		members[index] = key + "=" + memberValue
	}
	return strings.Join(members, ","), nil
}

func validTracestateKey(key string) bool {
	if key == "" || len(key) > 256 {
		return false
	}
	parts := strings.Split(key, "@")
	if len(parts) > 2 {
		return false
	}
	for index, part := range parts {
		if part == "" || !lowerAlpha(part[0]) {
			return false
		}
		maximum := 256
		if len(parts) == 2 && index == 0 {
			maximum = 241
		} else if len(parts) == 2 {
			maximum = 14
		}
		if len(part) > maximum {
			return false
		}
		for _, character := range part[1:] {
			if !lowerAlpha(byte(character)) && (character < '0' || character > '9') &&
				!strings.ContainsRune("_-*/", character) {
				return false
			}
		}
	}
	return true
}

func validTracestateValue(value string) bool {
	if len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e || character == ',' || character == '=' {
			return false
		}
	}
	return true
}

func lowerAlpha(character byte) bool {
	return character >= 'a' && character <= 'z'
}

type w3cTraceContextKey struct{}
