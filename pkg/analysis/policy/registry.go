// Package policy owns rule governance and deterministic inventories.
package policy

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

// Overlap documents another tool covering adjacent or identical semantics.
type Overlap struct {
	Tool               string `json:"tool"`
	CanonicalAuthority string `json:"canonical_authority"`
	CompatibleConfig   string `json:"compatible_configuration,omitempty"`
}

// Entry associates a rule with ownership and promotion evidence.
type Entry struct {
	Rule              shared.Rule `json:"rule"`
	Owner             string      `json:"owner"`
	PromotionEvidence string      `json:"promotion_evidence,omitempty"`
	Overlaps          []Overlap   `json:"overlaps,omitempty"`
}

// Registry is an immutable, validated rule inventory.
type Registry struct {
	entries map[string]Entry
}

// NewRegistry rejects incomplete ownership and silent blocking promotion.
func NewRegistry(entries []Entry) (*Registry, error) {
	registry := &Registry{entries: make(map[string]Entry, len(entries))}
	for _, entry := range entries {
		if err := entry.Rule.Validate(); err != nil {
			return nil, fmt.Errorf("validate rule metadata: %w", err)
		}
		if strings.TrimSpace(entry.Owner) == "" {
			return nil, fmt.Errorf("rule %q requires an owner", entry.Rule.ID)
		}
		if entry.Rule.DefaultStatus == shared.StatusBlocking &&
			strings.TrimSpace(entry.PromotionEvidence) == "" {
			return nil, fmt.Errorf("blocking rule %q requires promotion evidence", entry.Rule.ID)
		}
		overlapTools := make(map[string]struct{}, len(entry.Overlaps))
		for _, overlap := range entry.Overlaps {
			tool := strings.TrimSpace(overlap.Tool)
			if tool == "" ||
				strings.TrimSpace(overlap.CanonicalAuthority) == "" {
				return nil, errors.New("overlap requires tool and canonical authority")
			}
			toolKey := strings.ToLower(tool)
			if _, exists := overlapTools[toolKey]; exists {
				return nil, fmt.Errorf(
					"rule %q duplicates overlap authority %q",
					entry.Rule.ID,
					tool,
				)
			}
			overlapTools[toolKey] = struct{}{}
		}
		if _, exists := registry.entries[entry.Rule.ID]; exists {
			return nil, fmt.Errorf("duplicate rule %q", entry.Rule.ID)
		}
		registry.entries[entry.Rule.ID] = entry
	}

	return registry, nil
}

// IDs returns rule IDs in stable lexical order.
func (registry *Registry) IDs() []string {
	ids := make([]string, 0, len(registry.entries))
	for id := range registry.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	return ids
}

// Entries returns governed rules in stable order without exposing storage.
func (registry *Registry) Entries() []Entry {
	ids := registry.IDs()
	entries := make([]Entry, 0, len(ids))
	for _, id := range ids {
		entry := registry.entries[id]
		entry.Overlaps = append([]Overlap(nil), entry.Overlaps...)
		properties := entry.Rule.Configuration.Properties
		entry.Rule.Configuration.Properties = make(
			map[string]shared.ConfigurationProperty,
			len(properties),
		)
		for name, property := range properties {
			entry.Rule.Configuration.Properties[name] = property
		}
		entries = append(entries, entry)
	}

	return entries
}
