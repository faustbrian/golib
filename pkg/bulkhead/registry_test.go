package bulkhead_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestFixedPartitionsBoundCardinalityAndIsolateCapacity(t *testing.T) {
	registry, err := bulkhead.NewRegistry(bulkhead.FixedPartitions{Maximum: 2})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	inventory, err := registry.Create(bulkhead.Config{
		Resource:  "inventory-db",
		Capacity:  1,
		Admission: bulkhead.RejectImmediately{},
	})
	if err != nil {
		t.Fatalf("Create(inventory) error = %v", err)
	}
	payments, err := registry.Create(bulkhead.Config{
		Resource:  "payments-api",
		Capacity:  1,
		Admission: bulkhead.RejectImmediately{},
	})
	if err != nil {
		t.Fatalf("Create(payments) error = %v", err)
	}
	if found, err := registry.Lookup("payments-api"); err != nil || found != payments {
		t.Fatalf("Lookup(payments) = %p, %v", found, err)
	}
	snapshots := registry.Snapshots()
	if len(snapshots) != 2 || snapshots[0].Resource != "inventory-db" ||
		snapshots[1].Resource != "payments-api" {
		t.Fatalf("Snapshots() = %+v", snapshots)
	}
	if _, err := registry.Create(bulkhead.Config{
		Resource: "shipping-api", Capacity: 1,
	}); !errors.Is(err, bulkhead.ErrPartitionLimit) {
		t.Fatalf("third Create() error = %v, want ErrPartitionLimit", err)
	}
	if _, err := registry.Create(bulkhead.Config{
		Resource: "inventory-db", Capacity: 1,
	}); !errors.Is(err, bulkhead.ErrPartitionExists) {
		t.Fatalf("duplicate Create() error = %v, want ErrPartitionExists", err)
	}

	inventoryPermit, err := inventory.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("inventory Acquire() error = %v", err)
	}
	if _, err := inventory.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrRejected) {
		t.Fatalf("saturated inventory error = %v", err)
	}
	paymentPermit, err := payments.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("isolated payments Acquire() error = %v", err)
	}
	if err := registry.Remove("inventory-db"); !errors.Is(err, bulkhead.ErrPartitionBusy) {
		t.Fatalf("Remove(active) error = %v, want ErrPartitionBusy", err)
	}

	_ = inventory.Close()
	_ = inventoryPermit.Release()
	if err := drainWithin(inventory); err != nil {
		t.Fatalf("inventory Drain() error = %v", err)
	}
	if err := registry.Remove("inventory-db"); err != nil {
		t.Fatalf("Remove(drained) error = %v", err)
	}
	if registry.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", registry.Len())
	}
	if _, err := registry.Lookup("inventory-db"); !errors.Is(err, bulkhead.ErrPartitionNotFound) {
		t.Fatalf("Lookup(removed) error = %v, want ErrPartitionNotFound", err)
	}
	if err := registry.Remove("inventory-db"); !errors.Is(err, bulkhead.ErrPartitionNotFound) {
		t.Fatalf("duplicate Remove() error = %v, want ErrPartitionNotFound", err)
	}
	_ = paymentPermit.Release()
}

func TestRegistryRejectsInvalidPartitionConfiguration(t *testing.T) {
	registry, err := bulkhead.NewRegistry(bulkhead.FixedPartitions{Maximum: 1})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if _, err := registry.Create(bulkhead.Config{}); !errors.Is(err, bulkhead.ErrInvalidConfig) {
		t.Fatalf("Create(invalid) error = %v", err)
	}
}

func TestRegistryAcceptsMaximumPartitionBound(t *testing.T) {
	registry, err := bulkhead.NewRegistry(bulkhead.FixedPartitions{Maximum: bulkhead.MaxPartitions})
	if err != nil {
		t.Fatalf("NewRegistry(MaxPartitions) error = %v", err)
	}
	if got := registry.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}
