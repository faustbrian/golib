// Package metric defines bounded metric SDK configuration, views, and
// cardinality controls.
package metric

import (
	"errors"
	"fmt"
	"math"
	"regexp"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var instrumentNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.\-/*?]{0,254}$`)

// Config applies a hard per-stream cardinality budget and explicit views.
type Config struct {
	CardinalityLimit int
	Views            []ViewConfig
}

// ViewConfig controls one metric stream. AllowedAttributes is an allow-list;
// an empty list records no attributes for the matching stream.
type ViewConfig struct {
	Name              string
	Unit              string
	AllowedAttributes []attribute.Key
	Boundaries        []float64
	NoMinMax          bool
}

// Options validates config and returns standard OpenTelemetry SDK options.
func Options(config Config) ([]sdkmetric.Option, error) {
	if config.CardinalityLimit <= 0 {
		return nil, errors.New("metric cardinality limit must be positive")
	}
	options := []sdkmetric.Option{sdkmetric.WithCardinalityLimit(config.CardinalityLimit)}
	for index, view := range config.Views {
		if err := view.validate(); err != nil {
			return nil, fmt.Errorf("metric view %d: %w", index, err)
		}
		stream := sdkmetric.Stream{
			AttributeFilter: attribute.NewAllowKeysFilter(view.AllowedAttributes...),
		}
		if len(view.Boundaries) > 0 {
			stream.Aggregation = sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: append([]float64(nil), view.Boundaries...),
				NoMinMax:   view.NoMinMax,
			}
		}
		options = append(options, sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: view.Name, Unit: view.Unit},
			stream,
		)))
	}
	return options, nil
}

func (config ViewConfig) validate() error {
	var errs []error
	if !instrumentNamePattern.MatchString(config.Name) {
		errs = append(errs, fmt.Errorf("instrument name %q is invalid", config.Name))
	}
	if len(config.Unit) > 63 {
		errs = append(errs, errors.New("instrument unit exceeds 63 characters"))
	}
	seen := make(map[attribute.Key]struct{}, len(config.AllowedAttributes))
	for _, key := range config.AllowedAttributes {
		if key == "" {
			errs = append(errs, errors.New("allowed attribute key cannot be empty"))
		}
		if _, duplicate := seen[key]; duplicate {
			errs = append(errs, fmt.Errorf("allowed attribute key %q is duplicated", key))
		}
		seen[key] = struct{}{}
	}
	for index, boundary := range config.Boundaries {
		if math.IsNaN(boundary) || math.IsInf(boundary, 0) {
			errs = append(errs, errors.New("histogram boundaries must be finite"))
		}
		if index > 0 && boundary <= config.Boundaries[index-1] {
			errs = append(errs, errors.New("histogram boundaries must be strictly increasing"))
		}
	}
	return errors.Join(errs...)
}
