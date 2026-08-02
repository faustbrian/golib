package bulkhead

import (
	"slices"
	"strings"
	"sync"
)

// PartitioningPolicy explicitly names how a registry bounds resource identities.
type PartitioningPolicy interface{ partitioningPolicy() }

// FixedPartitions allows at most Maximum explicitly created partitions. It
// never creates from lookup keys and never evicts automatically.
type FixedPartitions struct {
	Maximum int
}

func (FixedPartitions) partitioningPolicy() { _ = "fixed-partitions" }

// Registry owns an application-scoped bounded set of independent bulkheads.
// It is not global; applications choose and share its lifetime explicitly.
type Registry struct {
	mu         sync.RWMutex
	maximum    int
	partitions map[string]*Bulkhead
}

// NewRegistry validates an explicitly named partition cardinality policy.
func NewRegistry(policy PartitioningPolicy) (*Registry, error) {
	if nilLike(policy) {
		return nil, invalidConfig("PartitioningPolicy", "must not be a typed nil")
	}
	fixed, ok := policy.(FixedPartitions)
	if !ok {
		return nil, invalidConfig("PartitioningPolicy", "unsupported policy")
	}
	if fixed.Maximum <= 0 || fixed.Maximum > MaxPartitions {
		return nil, invalidConfig("FixedPartitions.Maximum", "must be positive and bounded")
	}
	return &Registry{
		maximum:    fixed.Maximum,
		partitions: make(map[string]*Bulkhead),
	}, nil
}

// Create validates and explicitly registers one immutable resource policy.
func (registry *Registry) Create(config Config) (*Bulkhead, error) {
	partition, err := New(config)
	if err != nil {
		return nil, err
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.partitions[config.Resource]; exists {
		return nil, ErrPartitionExists
	}
	if len(registry.partitions) >= registry.maximum {
		return nil, ErrPartitionLimit
	}
	registry.partitions[config.Resource] = partition
	return partition, nil
}

// Lookup returns an explicitly created partition without creating from the key.
func (registry *Registry) Lookup(resource string) (*Bulkhead, error) {
	registry.mu.RLock()
	partition, exists := registry.partitions[resource]
	registry.mu.RUnlock()
	if !exists {
		return nil, ErrPartitionNotFound
	}
	return partition, nil
}

// Remove deletes only a closed, fully drained partition. Configuration changes
// therefore require Close, Drain, Remove, and an explicit Create; retained
// pointers remain closed and cannot split resource capacity.
func (registry *Registry) Remove(resource string) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	partition, exists := registry.partitions[resource]
	if !exists {
		return ErrPartitionNotFound
	}
	if snapshot := partition.Snapshot(); !snapshot.Drained {
		return ErrPartitionBusy
	}
	delete(registry.partitions, resource)
	return nil
}

// Len reports the current bounded partition cardinality.
func (registry *Registry) Len() int {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return len(registry.partitions)
}

// Snapshots returns resource-sorted immutable snapshots.
func (registry *Registry) Snapshots() []Snapshot {
	registry.mu.RLock()
	partitions := make([]*Bulkhead, 0, len(registry.partitions))
	for _, partition := range registry.partitions {
		partitions = append(partitions, partition)
	}
	registry.mu.RUnlock()

	snapshots := make([]Snapshot, 0, len(partitions))
	for _, partition := range partitions {
		snapshots = append(snapshots, partition.Snapshot())
	}
	slices.SortFunc(snapshots, func(left, right Snapshot) int {
		return strings.Compare(left.Resource, right.Resource)
	})
	return snapshots
}
