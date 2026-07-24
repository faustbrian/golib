// Package log provides bounded slog attributes compatible with log's
// standard log/slog composition API.
package log

import (
	"log/slog"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

// Attrs returns only non-empty identifier attributes under an explicit
// disclosure policy.
func Attrs(values correlation.Values, policy correlation.DisclosurePolicy) ([]slog.Attr, error) {
	inputs := []struct {
		key   string
		value string
	}{
		{"correlation.id", values.CorrelationID.String()},
		{"request.id", values.RequestID.String()},
		{"causation.id", values.CausationID.String()},
	}
	attributes := make([]slog.Attr, 0, len(inputs))
	for _, input := range inputs {
		if input.value == "" {
			continue
		}
		value, err := correlation.Disclose(input.key, input.value, policy)
		if err != nil {
			return nil, err
		}
		attributes = append(attributes, slog.String(input.key, value))
	}
	return attributes, nil
}
