package management

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDesiredRecordValidatesRevisionedLifecycleState(t *testing.T) {
	t.Parallel()

	valid := desiredRecord(1, DesiredPaused)
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	tests := map[string]func(*DesiredRecord){
		"target kind": func(value *DesiredRecord) { value.Target.Kind = TargetFailure },
		"target name": func(value *DesiredRecord) { value.Target.Name = "" },
		"state":       func(value *DesiredRecord) { value.State = DesiredState("lost") },
		"revision":    func(value *DesiredRecord) { value.Revision = 0 },
		"changed at":  func(value *DesiredRecord) { value.ChangedAt = time.Time{} },
		"command ID":  func(value *DesiredRecord) { value.CommandID = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := valid
			mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatalf("Validate(%s) error = nil", name)
			}
		})
	}
	for _, state := range []DesiredState{
		DesiredActive, DesiredPaused, DesiredDraining, DesiredTerminating,
	} {
		value := desiredRecord(1, state)
		if err := value.Validate(); err != nil {
			t.Fatalf("Validate(%q) error = %v", state, err)
		}
	}
}

func TestDesiredStateReconcilerAppliesMonotonicTargetScopedRevisions(t *testing.T) {
	t.Parallel()

	queueTarget := Target{Kind: TargetQueue, Name: "critical"}
	groupTarget := Target{Kind: TargetWorkerGroup, Name: "payments"}
	reader := &desiredReaderStub{records: map[Target]DesiredRecord{
		queueTarget: desiredRecordFor(queueTarget, 1, DesiredPaused),
		groupTarget: desiredRecordFor(groupTarget, 4, DesiredDraining),
	}}
	applier := &desiredApplierStub{}
	reconciler, err := NewDesiredStateReconciler(DesiredStateReconcilerConfig{
		Reader: reader, Applier: applier, Targets: []Target{queueTarget, groupTarget},
	})
	if err != nil {
		t.Fatalf("NewDesiredStateReconciler() error = %v", err)
	}
	ctx := context.WithValue(context.Background(), desiredContextKey{}, "forwarded")
	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile(first) error = %v", err)
	}
	if !reflect.DeepEqual(applier.records, []DesiredRecord{
		reader.records[queueTarget], reader.records[groupTarget],
	}) {
		t.Fatalf("applied records = %+v", applier.records)
	}
	if reader.ctx.Value(desiredContextKey{}) != "forwarded" ||
		applier.ctx.Value(desiredContextKey{}) != "forwarded" {
		t.Fatal("reconciliation context was not forwarded")
	}
	if err := reconciler.Reconcile(ctx); err != nil || len(applier.records) != 2 {
		t.Fatalf("Reconcile(unchanged) = %v, applied=%d", err, len(applier.records))
	}
	reader.records[queueTarget] = desiredRecordFor(queueTarget, 2, DesiredActive)
	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile(updated) error = %v", err)
	}
	if len(applier.records) != 3 || applier.records[2].State != DesiredActive {
		t.Fatalf("updated records = %+v", applier.records)
	}
}

func TestDesiredStateReconcilerFailsClosedWithoutLosingRetryability(t *testing.T) {
	t.Parallel()

	target := Target{Kind: TargetQueue, Name: "critical"}
	readerErr := errors.New("reader unavailable")
	applyErr := errors.New("apply failed")
	reader := &desiredReaderStub{err: readerErr}
	applier := &desiredApplierStub{}
	reconciler, err := NewDesiredStateReconciler(DesiredStateReconcilerConfig{
		Reader: reader, Applier: applier, Targets: []Target{target},
	})
	if err != nil {
		t.Fatalf("NewDesiredStateReconciler() error = %v", err)
	}
	if err := reconciler.Reconcile(context.Background()); !errors.Is(err, readerErr) {
		t.Fatalf("reader error = %v", err)
	}

	reader.err = nil
	reader.records = map[Target]DesiredRecord{target: desiredRecordFor(target, 2, DesiredPaused)}
	applier.err = applyErr
	if err := reconciler.Reconcile(context.Background()); !errors.Is(err, applyErr) {
		t.Fatalf("apply error = %v", err)
	}
	applier.err = nil
	if err := reconciler.Reconcile(context.Background()); err != nil || len(applier.records) != 1 {
		t.Fatalf("retry = %v, records=%+v", err, applier.records)
	}

	reader.records[target] = desiredRecordFor(target, 1, DesiredActive)
	if err := reconciler.Reconcile(context.Background()); !errors.Is(err, ErrDesiredStateRegression) {
		t.Fatalf("regression error = %v", err)
	}
	conflict := desiredRecordFor(target, 2, DesiredActive)
	reader.records[target] = conflict
	if err := reconciler.Reconcile(context.Background()); !errors.Is(err, ErrDesiredStateConflict) {
		t.Fatalf("same-revision conflict error = %v", err)
	}
	reader.records[target] = desiredRecordFor(
		Target{Kind: TargetQueue, Name: "other"}, 3, DesiredActive,
	)
	if err := reconciler.Reconcile(context.Background()); !errors.Is(err, ErrInvalidDesiredStateOutput) {
		t.Fatalf("mismatched target error = %v", err)
	}
}

func TestDesiredStateReconcilerTreatsMissingStateAsNoChange(t *testing.T) {
	t.Parallel()

	target := Target{Kind: TargetWorker, Name: "worker-1"}
	reader := &desiredReaderStub{missing: true}
	applier := &desiredApplierStub{}
	reconciler, err := NewDesiredStateReconciler(DesiredStateReconcilerConfig{
		Reader: reader, Applier: applier, Targets: []Target{target},
	})
	if err != nil {
		t.Fatalf("NewDesiredStateReconciler() error = %v", err)
	}
	if err := reconciler.Reconcile(context.Background()); err != nil || len(applier.records) != 0 {
		t.Fatalf("Reconcile(missing) = %v, records=%+v", err, applier.records)
	}
}

func TestDesiredStateReconcilerRejectsUnsafeConfigurationAndContext(t *testing.T) {
	t.Parallel()

	reader := &desiredReaderStub{}
	applier := &desiredApplierStub{}
	var nilReader *desiredReaderStub
	var nilApplier *desiredApplierStub
	tooMany := make([]Target, MaxDesiredStateTargets+1)
	for index := range tooMany {
		tooMany[index] = Target{Kind: TargetQueue, Name: "queue"}
	}
	tests := []DesiredStateReconcilerConfig{
		{},
		{Reader: nilReader, Applier: applier, Targets: []Target{{Kind: TargetQueue, Name: "queue"}}},
		{Reader: reader, Applier: nilApplier, Targets: []Target{{Kind: TargetQueue, Name: "queue"}}},
		{Reader: reader, Applier: applier},
		{Reader: reader, Applier: applier, Targets: tooMany},
		{Reader: reader, Applier: applier, Targets: []Target{{Kind: TargetFailure, Name: "failure"}}},
		{Reader: reader, Applier: applier, Targets: []Target{{Kind: TargetQueue, Name: "same"}, {Kind: TargetQueue, Name: "same"}}},
	}
	for _, config := range tests {
		reconciler, err := NewDesiredStateReconciler(config)
		if reconciler != nil || !errors.Is(err, ErrInvalidDesiredStateConfiguration) {
			t.Fatalf("NewDesiredStateReconciler(%+v) = (%v, %v)", config, reconciler, err)
		}
	}
	reconciler, err := NewDesiredStateReconciler(DesiredStateReconcilerConfig{
		Reader: reader, Applier: applier,
		Targets: []Target{{Kind: TargetQueue, Name: "queue"}},
	})
	if err != nil {
		t.Fatalf("NewDesiredStateReconciler() error = %v", err)
	}
	//lint:ignore SA1012 Public boundary must reject a nil context safely.
	//nolint:staticcheck // Public boundary must reject a nil context safely.
	if err := reconciler.Reconcile(nil); !errors.Is(err, ErrInvalidDesiredStateContext) {
		t.Fatalf("Reconcile(nil) error = %v", err)
	}
}

func desiredRecord(revision uint64, state DesiredState) DesiredRecord {
	return desiredRecordFor(Target{Kind: TargetQueue, Name: "critical"}, revision, state)
}

func desiredRecordFor(target Target, revision uint64, state DesiredState) DesiredRecord {
	return DesiredRecord{
		Target: target, State: state, Revision: revision,
		ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		CommandID: "command-1",
	}
}

type desiredContextKey struct{}

type desiredReaderStub struct {
	records map[Target]DesiredRecord
	err     error
	missing bool
	ctx     context.Context
}

func (s *desiredReaderStub) GetDesiredState(
	ctx context.Context,
	target Target,
) (DesiredRecord, error) {
	s.ctx = ctx
	if s.err != nil {
		return DesiredRecord{}, s.err
	}
	if s.missing {
		return DesiredRecord{}, ErrDesiredStateNotFound
	}
	return s.records[target], nil
}

type desiredApplierStub struct {
	records []DesiredRecord
	err     error
	ctx     context.Context
}

func (s *desiredApplierStub) ApplyDesiredState(
	ctx context.Context,
	record DesiredRecord,
) error {
	s.ctx = ctx
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, record)
	return nil
}
