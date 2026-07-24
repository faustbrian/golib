package config

import "context"

// DefaultSources groups sources by the documented default precedence model.
// Order within each category is preserved.
type DefaultSources struct {
	Defaults          []Source
	DiscoveredBase    []Source
	DiscoveredProfile []Source
	ExplicitFiles     []Source
	Dotenv            []Source
	Environment       []Source
	Overrides         []Source
}

// NewDefaultPlan assigns category priorities and returns an inspectable plan.
func NewDefaultPlan(sources DefaultSources) (Plan, error) {
	resolved := make([]Source, 0,
		len(sources.Defaults)+
			len(sources.DiscoveredBase)+
			len(sources.DiscoveredProfile)+
			len(sources.ExplicitFiles)+
			len(sources.Dotenv)+
			len(sources.Environment)+
			len(sources.Overrides),
	)
	resolved = appendPriority(resolved, sources.Defaults, PriorityDefaults)
	resolved = appendPriority(resolved, sources.DiscoveredBase, PriorityDiscoveredBase)
	resolved = appendPriority(resolved, sources.DiscoveredProfile, PriorityDiscoveredProfile)
	resolved = appendPriority(resolved, sources.ExplicitFiles, PriorityExplicitFiles)
	resolved = appendPriority(resolved, sources.Dotenv, PriorityDotenv)
	resolved = appendPriority(resolved, sources.Environment, PriorityEnvironment)
	resolved = appendPriority(resolved, sources.Overrides, PriorityOverrides)
	return NewPlan(resolved...)
}

type prioritySource struct {
	source Source
	info   SourceInfo
}

func (s prioritySource) Info() SourceInfo { return s.info }

func (s prioritySource) Load(ctx context.Context) (Document, error) {
	return s.source.Load(ctx)
}

func appendPriority(destination, sources []Source, priority int) []Source {
	for _, source := range sources {
		if source == nil || isNilSource(source) {
			destination = append(destination, nil)
			continue
		}
		info := source.Info()
		info.Priority = priority
		destination = append(destination, prioritySource{source: source, info: info})
	}
	return destination
}
