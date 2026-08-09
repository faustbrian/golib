package postgres

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDerivedStoreConstructorsAcceptDependencies(t *testing.T) {
	t.Parallel()

	pool := &pgxpool.Pool{}
	snapshots, err := NewSnapshotStore(pool, Config{})
	if err != nil || snapshots.database != pool ||
		snapshots.beginner != pool {
		t.Fatalf("NewSnapshotStore() = %#v, %v", snapshots, err)
	}
	checkpoints, err := NewProjectionStore(pool, Config{})
	if err != nil || checkpoints.database != pool ||
		checkpoints.beginner != pool {
		t.Fatalf("NewProjectionStore() = %#v, %v", checkpoints, err)
	}
	tx := &fakeTx{fakeDatabase: &fakeDatabase{}}
	writer, err := NewTxCheckpointWriter(tx, Config{})
	if err != nil || writer.store.database != tx {
		t.Fatalf("NewTxCheckpointWriter() = %#v, %v", writer, err)
	}
	if _, implementsStore := any(writer).(projection.CheckpointStore); implementsStore {
		t.Fatal("transaction writer implements durable checkpoint store")
	}
}

func TestSnapshotStoreLoadsAndDeletes(t *testing.T) {
	t.Parallel()

	snapshot := derivedSnapshot(t, 7, 2, `{"owner":"Ada"}`)
	failure := errors.New("database failure")
	for name, testCase := range map[string]struct {
		rowScan scanFunc
		execErr error
		want    error
	}{
		"load": {
			rowScan: snapshotScan(snapshot),
		},
		"missing": {
			rowScan: scanError(pgx.ErrNoRows),
			want:    eventsourcing.ErrSnapshotNotFound,
		},
		"load failure": {
			rowScan: scanError(failure),
			want:    failure,
		},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDatabase{rowScans: []scanFunc{testCase.rowScan}}
			store := &SnapshotStore{database: db, schema: defaultSchema}
			loaded, err := store.Load(
				context.Background(),
				snapshot.Stream(),
			)
			if testCase.want == nil {
				if err != nil || !loaded.Equal(snapshot) {
					t.Fatalf("Load() = %#v, %v", loaded, err)
				}
			} else if !errors.Is(err, testCase.want) {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}

	for name, execErr := range map[string]error{
		"success": nil,
		"failure": failure,
	} {
		execErr := execErr
		t.Run("delete "+name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDatabase{execErrs: []error{execErr}}
			store := &SnapshotStore{database: db, schema: defaultSchema}
			err := store.Delete(context.Background(), snapshot.Stream())
			if !errors.Is(err, execErr) || db.execCalls != 1 {
				t.Fatalf("Delete() calls/error = %d, %v", db.execCalls, err)
			}
		})
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	store := &SnapshotStore{
		database: &fakeDatabase{},
		schema:   defaultSchema,
	}
	if _, err := store.Load(
		cancelled,
		snapshot.Stream(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load(cancelled) error = %v", err)
	}
	if err := store.Delete(
		cancelled,
		snapshot.Stream(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(cancelled) error = %v", err)
	}
}

func TestSnapshotStoreValidatesEachInputIndependently(t *testing.T) {
	t.Parallel()

	snapshot := derivedSnapshot(t, 1, 1, `{}`)
	validStream := snapshot.Stream()
	validDatabase := &fakeDatabase{}
	validBeginner := &fakeBeginner{}
	var nilContext context.Context

	loadCases := map[string]struct {
		store  *SnapshotStore
		ctx    context.Context
		stream eventsourcing.StreamID
	}{
		"nil store": {
			ctx:    context.Background(),
			stream: validStream,
		},
		"nil database": {
			store:  &SnapshotStore{},
			ctx:    context.Background(),
			stream: validStream,
		},
		"nil context": {
			store:  &SnapshotStore{database: validDatabase},
			ctx:    nilContext,
			stream: validStream,
		},
		"zero stream": {
			store: &SnapshotStore{database: validDatabase},
			ctx:   context.Background(),
		},
	}
	for name, testCase := range loadCases {
		testCase := testCase
		t.Run("load "+name, func(t *testing.T) {
			t.Parallel()

			if _, err := testCase.store.Load(testCase.ctx, testCase.stream); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("Load() error = %v", err)
			}
		})
		t.Run("delete "+name, func(t *testing.T) {
			t.Parallel()

			if err := testCase.store.Delete(testCase.ctx, testCase.stream); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("Delete() error = %v", err)
			}
		})
	}

	saveCases := map[string]struct {
		store    *SnapshotStore
		ctx      context.Context
		snapshot eventsourcing.Snapshot
	}{
		"nil store": {
			ctx:      context.Background(),
			snapshot: snapshot,
		},
		"nil beginner": {
			store:    &SnapshotStore{database: validDatabase},
			ctx:      context.Background(),
			snapshot: snapshot,
		},
		"nil database": {
			store:    &SnapshotStore{beginner: validBeginner},
			ctx:      context.Background(),
			snapshot: snapshot,
		},
		"nil context": {
			store: &SnapshotStore{
				beginner: validBeginner,
				database: validDatabase,
			},
			ctx:      nilContext,
			snapshot: snapshot,
		},
		"zero snapshot": {
			store: &SnapshotStore{
				beginner: validBeginner,
				database: validDatabase,
			},
			ctx: context.Background(),
		},
	}
	for name, testCase := range saveCases {
		testCase := testCase
		t.Run("save "+name, func(t *testing.T) {
			t.Parallel()

			if err := testCase.store.Save(testCase.ctx, testCase.snapshot); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("Save() error = %v", err)
			}
		})
	}
}

func TestSnapshotStoreOwnsAtomicSaveTransaction(t *testing.T) {
	t.Parallel()

	current := derivedSnapshot(t, 7, 2, `{"owner":"Ada"}`)
	newer := derivedSnapshot(t, 8, 3, `{"owner":"Ada","closed":true}`)
	failure := errors.New("database failure")
	tests := map[string]struct {
		snapshot   eventsourcing.Snapshot
		beginner   *fakeBeginner
		want       error
		wantCommit bool
	}{
		"insert": {
			snapshot: current,
			beginner: snapshotBeginner(
				[]scanFunc{scanValues(true)},
				nil,
				[]pgconn.CommandTag{},
				nil,
			),
			wantCommit: true,
		},
		"maximum aggregate version": {
			snapshot: derivedSnapshot(t, math.MaxInt64, 1, `{}`),
			beginner: snapshotBeginner(
				[]scanFunc{scanValues(true)},
				nil,
				[]pgconn.CommandTag{},
				nil,
			),
			wantCommit: true,
		},
		"begin failure": {
			snapshot: current,
			beginner: &fakeBeginner{err: failure},
			want:     failure,
		},
		"insert failure": {
			snapshot: current,
			beginner: snapshotBeginner(
				[]scanFunc{scanError(failure)},
				nil,
				nil,
				nil,
			),
			want: failure,
		},
		"commit failure": {
			snapshot: current,
			beginner: snapshotBeginner(
				[]scanFunc{scanValues(true)},
				nil,
				nil,
				failure,
			),
			want: failure,
		},
		"load failure": {
			snapshot: current,
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					scanError(failure),
				},
				nil,
				nil,
				nil,
			),
			want: failure,
		},
		"stale": {
			snapshot: derivedSnapshot(t, 6, 2, `{"owner":"Ada"}`),
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				nil,
				nil,
				nil,
			),
			want: eventsourcing.ErrSnapshotStale,
		},
		"idempotent": {
			snapshot: current,
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				nil,
				nil,
				nil,
			),
		},
		"conflict": {
			snapshot: derivedSnapshot(t, 7, 2, `{"owner":"Grace"}`),
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				nil,
				nil,
				nil,
			),
			want: eventsourcing.ErrSnapshotConflict,
		},
		"update": {
			snapshot: newer,
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				nil,
				[]pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
				nil,
			),
			wantCommit: true,
		},
		"aggregate-only update": {
			snapshot: derivedSnapshot(t, 8, 2, `{"owner":"Ada","closed":true}`),
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				nil,
				[]pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
				nil,
			),
			wantCommit: true,
		},
		"schema-only update": {
			snapshot: derivedSnapshot(t, 7, 3, `{"owner":"Ada","closed":true}`),
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				nil,
				[]pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
				nil,
			),
			wantCommit: true,
		},
		"update failure": {
			snapshot: newer,
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				[]error{failure},
				nil,
				nil,
			),
			want: failure,
		},
		"missing update": {
			snapshot: newer,
			beginner: snapshotBeginner(
				[]scanFunc{
					scanError(pgx.ErrNoRows),
					snapshotScan(current),
				},
				nil,
				[]pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")},
				nil,
			),
			want: eventsourcing.ErrSnapshotCorrupt,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &SnapshotStore{
				beginner: testCase.beginner,
				database: &fakeDatabase{},
				schema:   defaultSchema,
			}
			err := store.Save(context.Background(), testCase.snapshot)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Save() error = %v", err)
			}
			if name == "commit failure" &&
				!errors.Is(err, ErrCommitOutcomeUnknown) {
				t.Fatalf("Save(commit failure) category = %v", err)
			}
			if testCase.beginner.tx != nil {
				if testCase.wantCommit &&
					testCase.beginner.tx.commitErr == nil &&
					err != nil {
					t.Fatalf("Save() did not commit: %v", err)
				}
				if testCase.beginner.tx.rollbackCalls != 1 {
					t.Fatalf(
						"rollback calls = %d",
						testCase.beginner.tx.rollbackCalls,
					)
				}
			}
		})
	}
}

func TestSnapshotStoreRejectsCancellationAndPostgreSQLVersionOverflow(
	t *testing.T,
) {
	t.Parallel()

	store := &SnapshotStore{
		beginner: &fakeBeginner{},
		database: &fakeDatabase{},
		schema:   defaultSchema,
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(
		cancelled,
		derivedSnapshot(t, 1, 1, `{}`),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save(cancelled) error = %v", err)
	}
	overflow := derivedSnapshot(
		t,
		uint64(math.MaxInt64)+1,
		1,
		`{}`,
	)
	if err := store.Save(
		context.Background(),
		overflow,
	); !errors.Is(err, eventsourcing.ErrVersionOverflow) {
		t.Fatalf("Save(overflow) error = %v", err)
	}
}

func TestScanSnapshotRejectsCorruptStoredValues(t *testing.T) {
	t.Parallel()

	valid := derivedSnapshot(t, 7, 2, `{"owner":"Ada"}`)
	validValues := snapshotValues(valid)
	failure := errors.New("scan failure")
	tests := map[string]struct {
		scan scanFunc
		want error
	}{
		"scan": {
			scan: scanError(failure),
			want: failure,
		},
		"aggregate version": {
			scan: scanValues(replaceValue(validValues, 2, int64(0))...),
			want: eventsourcing.ErrSnapshotCorrupt,
		},
		"schema version": {
			scan: scanValues(replaceValue(validValues, 3, int64(0))...),
			want: eventsourcing.ErrSnapshotCorrupt,
		},
		"schema overflow": {
			scan: scanValues(
				replaceValue(validValues, 3, int64(math.MaxUint32)+1)...,
			),
			want: eventsourcing.ErrSnapshotCorrupt,
		},
		"metadata": {
			scan: scanValues(replaceValue(validValues, 5, []byte(`[]`))...),
			want: eventsourcing.ErrSnapshotCorrupt,
		},
		"stream": {
			scan: scanValues(replaceValue(validValues, 0, "")...),
			want: eventsourcing.ErrSnapshotCorrupt,
		},
		"snapshot": {
			scan: scanValues(replaceValue(validValues, 4, []byte{})...),
			want: eventsourcing.ErrSnapshotCorrupt,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := scanSnapshot(fakeRow{scan: testCase.scan})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("scanSnapshot() error = %v", err)
			}
			switch name {
			case "aggregate version", "schema version", "schema overflow":
				if err != eventsourcing.ErrSnapshotCorrupt {
					t.Fatalf("scanSnapshot() classified stored envelope corruption as %v", err)
				}
			}
		})
	}

	maximum := derivedSnapshot(
		t,
		7,
		eventsourcing.SchemaVersion(math.MaxUint32),
		`{"owner":"Ada"}`,
	)
	scanned, err := scanSnapshot(fakeRow{scan: snapshotScan(maximum)})
	if err != nil || !scanned.Equal(maximum) {
		t.Fatalf("scanSnapshot(maximum schema version) = %#v, %v", scanned, err)
	}
}

func TestProjectionStoreStatusAndStateTransitions(t *testing.T) {
	t.Parallel()

	failure := errors.New("database failure")
	checkpoint := pgtype.Int8{Int64: 7, Valid: true}
	for name, testCase := range map[string]struct {
		scan  scanFunc
		want  error
		state projection.RunState
	}{
		"running": {
			scan:  scanValues(projectionStateRunning, checkpoint),
			state: projection.StateRunning,
		},
		"missing": {
			scan:  scanError(pgx.ErrNoRows),
			state: projection.StateRunning,
		},
		"failure": {
			scan: scanError(failure),
			want: failure,
		},
	} {
		testCase := testCase
		t.Run("status "+name, func(t *testing.T) {
			t.Parallel()

			store := &ProjectionStore{
				database: &fakeDatabase{
					rowScans: []scanFunc{testCase.scan},
				},
				schema: defaultSchema,
			}
			status, err := store.Status(context.Background(), "summary")
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Status() error = %v", err)
			}
			if testCase.want == nil && status.State() != testCase.state {
				t.Fatalf("Status() = %#v", status)
			}
		})
	}

	for name, testCase := range map[string]struct {
		state int16
		scan  scanFunc
		want  error
	}{
		"pause": {
			state: projectionStatePaused,
			scan:  scanValues(projectionStatePaused, checkpoint),
		},
		"resume": {
			state: projectionStateRunning,
			scan:  scanValues(projectionStateRunning, checkpoint),
		},
		"failure": {
			state: projectionStatePaused,
			scan:  scanError(failure),
			want:  failure,
		},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &ProjectionStore{
				database: &fakeDatabase{
					rowScans: []scanFunc{testCase.scan},
				},
				schema: defaultSchema,
			}
			var (
				status projection.Status
				err    error
			)
			if testCase.state == projectionStatePaused {
				status, err = store.Pause(context.Background(), "summary")
			} else {
				status, err = store.Resume(context.Background(), "summary")
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("state transition error = %v", err)
			}
			if testCase.want == nil {
				want := projection.StateRunning
				if testCase.state == projectionStatePaused {
					want = projection.StatePaused
				}
				if status.State() != want {
					t.Fatalf("state transition = %#v", status)
				}
			}
		})
	}
}

func TestProjectionStoreOwnsAtomicCheckpointTransaction(t *testing.T) {
	t.Parallel()

	failure := errors.New("database failure")
	tests := map[string]struct {
		beginner *fakeBeginner
		want     error
	}{
		"success": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStateRunning,
						pgtype.Int8{},
					),
				},
				nil,
				[]pgconn.CommandTag{
					pgconn.NewCommandTag("INSERT 0 1"),
					pgconn.NewCommandTag("UPDATE 1"),
				},
				nil,
			),
		},
		"begin": {
			beginner: &fakeBeginner{err: failure},
			want:     failure,
		},
		"stage": {
			beginner: checkpointBeginner(
				nil,
				[]error{failure},
				nil,
				nil,
			),
			want: failure,
		},
		"commit": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStateRunning,
						pgtype.Int8{},
					),
				},
				nil,
				[]pgconn.CommandTag{
					pgconn.NewCommandTag("INSERT 0 1"),
					pgconn.NewCommandTag("UPDATE 1"),
				},
				failure,
			),
			want: failure,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &ProjectionStore{
				beginner: testCase.beginner,
				database: &fakeDatabase{},
				schema:   defaultSchema,
			}
			err := store.Save(context.Background(), "summary", 0, 1)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Save() error = %v", err)
			}
			if name == "commit" &&
				!errors.Is(err, ErrCommitOutcomeUnknown) {
				t.Fatalf("Save(commit failure) category = %v", err)
			}
			if testCase.beginner.tx != nil &&
				testCase.beginner.tx.rollbackCalls != 1 {
				t.Fatalf(
					"rollback calls = %d",
					testCase.beginner.tx.rollbackCalls,
				)
			}
		})
	}

	store := &ProjectionStore{database: &fakeDatabase{}, schema: defaultSchema}
	if err := store.Save(
		context.Background(),
		"summary",
		0,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save(non-pool) error = %v", err)
	}
}

func TestStageCheckpointClassifiesStateAndConflicts(t *testing.T) {
	t.Parallel()

	failure := errors.New("database failure")
	tests := map[string]struct {
		rows     []scanFunc
		execErrs []error
		execTags []pgconn.CommandTag
		expected eventsourcing.GlobalPosition
		next     eventsourcing.GlobalPosition
		want     error
	}{
		"success": {
			rows: []scanFunc{
				scanValues(projectionStateRunning, pgtype.Int8{}),
			},
			execTags: []pgconn.CommandTag{
				pgconn.NewCommandTag("INSERT 0 1"),
				pgconn.NewCommandTag("UPDATE 1"),
			},
			next: 1,
		},
		"insert": {
			execErrs: []error{failure},
			next:     1,
			want:     failure,
		},
		"missing": {
			rows: []scanFunc{scanError(pgx.ErrNoRows)},
			next: 1,
			want: projection.ErrCheckpointCorrupt,
		},
		"query": {
			rows: []scanFunc{scanError(failure)},
			next: 1,
			want: failure,
		},
		"paused": {
			rows: []scanFunc{
				scanValues(projectionStatePaused, pgtype.Int8{}),
			},
			next: 1,
			want: projection.ErrProjectionPaused,
		},
		"conflict": {
			rows: []scanFunc{
				scanValues(
					projectionStateRunning,
					pgtype.Int8{Int64: 2, Valid: true},
				),
			},
			expected: 1,
			next:     3,
			want:     projection.ErrCheckpointConflict,
		},
		"update": {
			rows: []scanFunc{
				scanValues(projectionStateRunning, pgtype.Int8{}),
			},
			execErrs: []error{nil, failure},
			next:     1,
			want:     failure,
		},
		"missing update": {
			rows: []scanFunc{
				scanValues(projectionStateRunning, pgtype.Int8{}),
			},
			execTags: []pgconn.CommandTag{
				pgconn.NewCommandTag("INSERT 0 1"),
				pgconn.NewCommandTag("UPDATE 0"),
			},
			next: 1,
			want: projection.ErrCheckpointCorrupt,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := &fakeDatabase{
				rowScans: testCase.rows,
				execErrs: testCase.execErrs,
				execTags: testCase.execTags,
			}
			err := stageCheckpoint(
				context.Background(),
				db,
				defaultSchema,
				"summary",
				testCase.expected,
				testCase.next,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("stageCheckpoint() error = %v", err)
			}
		})
	}
}

func TestProjectionStoreResetsExpectedPausedCheckpoint(t *testing.T) {
	t.Parallel()

	failure := errors.New("database failure")
	tests := map[string]struct {
		beginner *fakeBeginner
		expected eventsourcing.GlobalPosition
		want     error
	}{
		"success": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStatePaused,
						pgtype.Int8{Int64: 7, Valid: true},
					),
				},
				nil,
				[]pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
				nil,
			),
			expected: 7,
		},
		"begin": {
			beginner: &fakeBeginner{err: failure},
			want:     failure,
		},
		"missing": {
			beginner: checkpointBeginner(
				[]scanFunc{scanError(pgx.ErrNoRows)},
				nil,
				nil,
				nil,
			),
			want: projection.ErrProjectionRunning,
		},
		"query": {
			beginner: checkpointBeginner(
				[]scanFunc{scanError(failure)},
				nil,
				nil,
				nil,
			),
			want: failure,
		},
		"running": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStateRunning,
						pgtype.Int8{},
					),
				},
				nil,
				nil,
				nil,
			),
			want: projection.ErrProjectionRunning,
		},
		"conflict": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStatePaused,
						pgtype.Int8{Int64: 7, Valid: true},
					),
				},
				nil,
				nil,
				nil,
			),
			expected: 6,
			want:     projection.ErrCheckpointConflict,
		},
		"update": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStatePaused,
						pgtype.Int8{},
					),
				},
				[]error{failure},
				nil,
				nil,
			),
			want: failure,
		},
		"missing update": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStatePaused,
						pgtype.Int8{},
					),
				},
				nil,
				[]pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 0")},
				nil,
			),
			want: projection.ErrCheckpointCorrupt,
		},
		"commit": {
			beginner: checkpointBeginner(
				[]scanFunc{
					scanValues(
						projectionStatePaused,
						pgtype.Int8{},
					),
				},
				nil,
				[]pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")},
				failure,
			),
			want: failure,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &ProjectionStore{
				beginner: testCase.beginner,
				database: &fakeDatabase{},
				schema:   defaultSchema,
			}
			status, err := store.ResetCheckpoint(
				context.Background(),
				"summary",
				testCase.expected,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("ResetCheckpoint() error = %v", err)
			}
			if name == "commit" &&
				!errors.Is(err, ErrCommitOutcomeUnknown) {
				t.Fatalf("ResetCheckpoint(commit failure) category = %v", err)
			}
			if testCase.want == nil {
				if checkpoint, exists := status.Checkpoint(); exists ||
					checkpoint != 0 ||
					status.State() != projection.StatePaused {
					t.Fatalf("ResetCheckpoint() = %#v", status)
				}
			}
		})
	}

	store := &ProjectionStore{database: &fakeDatabase{}, schema: defaultSchema}
	if _, err := store.ResetCheckpoint(
		context.Background(),
		"summary",
		0,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ResetCheckpoint(non-pool) error = %v", err)
	}
}

func TestProjectionStatusValidationAndCheckpointInput(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		state      int16
		checkpoint pgtype.Int8
		want       error
	}{
		"running": {
			state: projectionStateRunning,
		},
		"paused": {
			state: projectionStatePaused,
		},
		"checkpoint": {
			state: projectionStateRunning,
			checkpoint: pgtype.Int8{
				Int64: 7,
				Valid: true,
			},
		},
		"state": {
			state: 99,
			want:  projection.ErrCheckpointCorrupt,
		},
		"zero checkpoint": {
			state: projectionStateRunning,
			checkpoint: pgtype.Int8{
				Valid: true,
			},
			want: projection.ErrCheckpointCorrupt,
		},
		"negative checkpoint": {
			state: projectionStateRunning,
			checkpoint: pgtype.Int8{
				Int64: -1,
				Valid: true,
			},
			want: projection.ErrCheckpointCorrupt,
		},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newProjectionStatus(
				testCase.state,
				testCase.checkpoint,
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("newProjectionStatus() error = %v", err)
			}
		})
	}

	store := &ProjectionStore{database: &fakeDatabase{}, schema: defaultSchema}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Status(
		cancelled,
		"summary",
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Status(cancelled) error = %v", err)
	}
	if err := store.Save(
		context.Background(),
		"summary",
		1,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save(non-monotonic) error = %v", err)
	}
	unsupported := eventsourcing.GlobalPosition(uint64(math.MaxInt64) + 1)
	if err := store.Save(
		context.Background(),
		"summary",
		0,
		unsupported,
	); !errors.Is(err, eventsourcing.ErrVersionOverflow) {
		t.Fatalf("Save(overflow) error = %v", err)
	}
	maximum := eventsourcing.GlobalPosition(math.MaxInt64)
	if err := validateCheckpoint(
		store,
		context.Background(),
		"summary",
		maximum-1,
		maximum,
	); err != nil {
		t.Fatalf("validateCheckpoint(maximum) error = %v", err)
	}
	if validProjectionName("") || !validProjectionName("summary") {
		t.Fatal("projection name validation is inconsistent")
	}
	var nilContext context.Context
	if _, err := store.Pause(
		nilContext,
		"summary",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Pause(nil context) error = %v", err)
	}
	if _, err := store.Resume(
		context.Background(),
		"",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Resume(invalid name) error = %v", err)
	}
	if _, err := store.ResetCheckpoint(
		nilContext,
		"summary",
		0,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ResetCheckpoint(nil context) error = %v", err)
	}
}

func TestTransactionCheckpointWriterStagesAndPropagatesFailures(t *testing.T) {
	t.Parallel()

	successDB := &fakeDatabase{
		rowScans: []scanFunc{
			scanValues(projectionStateRunning, pgtype.Int8{}),
		},
		execTags: []pgconn.CommandTag{
			pgconn.NewCommandTag("INSERT 0 1"),
			pgconn.NewCommandTag("UPDATE 1"),
		},
	}
	writer := newTxCheckpointWriter(
		&ProjectionStore{
			database: successDB,
			schema:   defaultSchema,
		},
	)
	if err := writer.Stage(
		context.Background(),
		"summary",
		0,
		1,
	); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}

	failure := errors.New("database failure")
	writer.store.database = &fakeDatabase{execErrs: []error{failure}}
	if err := writer.Stage(
		context.Background(),
		"summary",
		0,
		1,
	); !errors.Is(err, failure) {
		t.Fatalf("Stage(failure) error = %v", err)
	}
	if err := writer.Stage(
		context.Background(),
		"summary",
		1,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Stage(invalid) error = %v", err)
	}
	if err := writer.Stage(
		context.Background(),
		"",
		0,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Stage(invalid name) error = %v", err)
	}
	emptyWriter := &TxCheckpointWriter{}
	if err := emptyWriter.Stage(
		context.Background(),
		"summary",
		0,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Stage(empty writer) error = %v", err)
	}

	waiting := newTxCheckpointWriter(&ProjectionStore{
		database: &fakeDatabase{},
		schema:   defaultSchema,
	})
	<-waiting.operation
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waiting.Stage(
		cancelled,
		"summary",
		0,
		1,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stage(waiting) error = %v", err)
	}
	waiting.operation.release()
}

func snapshotBeginner(
	rows []scanFunc,
	execErrs []error,
	execTags []pgconn.CommandTag,
	commitErr error,
) *fakeBeginner {
	return &fakeBeginner{
		tx: &fakeTx{
			fakeDatabase: &fakeDatabase{
				rowScans: rows,
				execErrs: execErrs,
				execTags: execTags,
			},
			commitErr: commitErr,
		},
	}
}

func checkpointBeginner(
	rows []scanFunc,
	execErrs []error,
	execTags []pgconn.CommandTag,
	commitErr error,
) *fakeBeginner {
	return snapshotBeginner(rows, execErrs, execTags, commitErr)
}

func derivedSnapshot(
	t testing.TB,
	aggregateVersion uint64,
	schemaVersion eventsourcing.SchemaVersion,
	state string,
) eventsourcing.Snapshot {
	t.Helper()

	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           testStream(t),
		AggregateVersion: aggregateVersion,
		SchemaVersion:    schemaVersion,
		State:            []byte(state),
		Metadata:         map[string]string{"codec": "json"},
		CreatedAt: time.Date(
			2026,
			time.July,
			25,
			16,
			0,
			0,
			123456789,
			time.UTC,
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}

func snapshotScan(snapshot eventsourcing.Snapshot) scanFunc {
	return scanValues(snapshotValues(snapshot)...)
}

func snapshotValues(snapshot eventsourcing.Snapshot) []any {
	return []any{
		snapshot.Stream().AggregateType(),
		snapshot.Stream().AggregateID(),
		int64(snapshot.AggregateVersion()),
		int64(snapshot.SchemaVersion()),
		snapshot.State(),
		encodeMetadata(snapshot.Metadata()),
		snapshot.CreatedAt(),
	}
}

func replaceValue(values []any, index int, replacement any) []any {
	replaced := append([]any(nil), values...)
	replaced[index] = replacement

	return replaced
}

func scanError(err error) scanFunc {
	return func([]any) error {
		return err
	}
}
