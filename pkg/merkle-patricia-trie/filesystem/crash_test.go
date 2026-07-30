package filesystem

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

const (
	crashHelperPath         = "MPT_FILESYSTEM_CRASH_PATH"
	crashHelperStage        = "MPT_FILESYSTEM_CRASH_STAGE"
	pruneCrashPath          = "MPT_FILESYSTEM_PRUNE_CRASH_PATH"
	pruneCrashStage         = "MPT_FILESYSTEM_PRUNE_CRASH_STAGE"
	retentionCrashPath      = "MPT_FILESYSTEM_RETENTION_CRASH_PATH"
	retentionCrashOperation = "MPT_FILESYSTEM_RETENTION_CRASH_OPERATION"
	crashExitCode           = 86
)

func TestCommitSurvivesProcessTerminationAtPublicationBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		stage       string
		wantUpdated bool
	}{
		{name: "after durable nodes", stage: "nodes", wantUpdated: false},
		{name: "after root rename", stage: "root", wantUpdated: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "trie")
			store, err := Open(ctx, path, DefaultLimits())
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			base, err := mpt.NewRawTrie(mpt.DefaultLimits())
			if err != nil {
				t.Fatalf("NewRawTrie() error = %v", err)
			}
			base, err = base.Update(
				ctx,
				[]byte("base"),
				[]byte("base value long enough to persist"),
			)
			if err != nil {
				t.Fatalf("Update(base) error = %v", err)
			}
			base, err = base.Commit(ctx, store)
			if err != nil {
				t.Fatalf("Commit(base) error = %v", err)
			}
			baseRoot, err := base.Root()
			if err != nil {
				t.Fatalf("Root(base) error = %v", err)
			}
			expected, err := base.Update(
				ctx,
				[]byte("next"),
				[]byte("next value long enough to persist"),
			)
			if err != nil {
				t.Fatalf("Update(next) error = %v", err)
			}
			nextRoot, err := expected.Root()
			if err != nil {
				t.Fatalf("Root(next) error = %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			command := exec.Command(
				os.Args[0],
				"-test.run=^TestCommitCrashHelper$",
			)
			command.Env = append(
				os.Environ(),
				crashHelperPath+"="+path,
				crashHelperStage+"="+test.stage,
			)
			err = command.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) ||
				exitErr.ExitCode() != crashExitCode {
				t.Fatalf("crash helper error = %v", err)
			}

			reopened, err := Open(ctx, path, DefaultLimits())
			if err != nil {
				t.Fatalf("Open(recovery) error = %v", err)
			}
			defer func() {
				if closeErr := reopened.Close(); closeErr != nil {
					t.Errorf("Close(recovery) error = %v", closeErr)
				}
			}()
			wantRoot := baseRoot
			if test.wantUpdated {
				wantRoot = nextRoot
			}
			if got := reopened.Root(); got != wantRoot {
				t.Fatalf("Root(recovery) = %x, want %x", got, wantRoot)
			}
			loaded, err := mpt.LoadRawTrie(
				wantRoot,
				reopened,
				mpt.DefaultLimits(),
			)
			if err != nil {
				t.Fatalf("LoadRawTrie() error = %v", err)
			}
			value, err := loaded.Get(ctx, []byte("base"))
			if err != nil || !bytes.HasPrefix(value, []byte("base value")) {
				t.Fatalf("Get(base) = (%q, %v)", value, err)
			}
			value, err = loaded.Get(ctx, []byte("next"))
			if test.wantUpdated {
				if err != nil || !bytes.HasPrefix(value, []byte("next value")) {
					t.Fatalf("Get(next) = (%q, %v)", value, err)
				}
			} else if !errors.Is(err, mpt.ErrAbsentKey) {
				t.Fatalf("Get(next before publication) error = %v", err)
			}
		})
	}
}

func TestCommitCrashHelper(t *testing.T) {
	path := os.Getenv(crashHelperPath)
	stage := os.Getenv(crashHelperStage)
	if path == "" || stage == "" {
		return
	}
	ctx := context.Background()
	store, err := Open(ctx, path, DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	target := nodesDurable
	if stage == "root" {
		target = rootRenamed
	}
	store.checkpoint = func(actual commitStage) {
		if actual == target {
			os.Exit(crashExitCode)
		}
	}
	trie, err := mpt.LoadRawTrie(store.Root(), store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	trie, err = trie.Update(
		ctx,
		[]byte("next"),
		[]byte("next value long enough to persist"),
	)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := trie.Commit(ctx, store); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	t.Fatal("commit completed without reaching crash checkpoint")
}

func TestPruneSurvivesProcessTerminationAtCommitBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name            string
		stage           string
		wantOldRootNode bool
	}{
		{
			name:            "before prune commit",
			stage:           "pending",
			wantOldRootNode: true,
		},
		{
			name:            "after prune commit",
			stage:           "committed",
			wantOldRootNode: false,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "trie")
			store, err := Open(ctx, path, DefaultLimits())
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
			if err != nil {
				t.Fatalf("NewRawTrie() error = %v", err)
			}
			trie, err = trie.Update(
				ctx,
				[]byte("old"),
				[]byte("old value long enough to persist"),
			)
			if err != nil {
				t.Fatalf("Update(old) error = %v", err)
			}
			trie, err = trie.Commit(ctx, store)
			if err != nil {
				t.Fatalf("Commit(old) error = %v", err)
			}
			oldRoot, err := trie.Root()
			if err != nil {
				t.Fatalf("Root(old) error = %v", err)
			}
			trie, err = trie.Delete(ctx, []byte("old"))
			if err != nil {
				t.Fatalf("Delete(old) error = %v", err)
			}
			trie, err = trie.Update(
				ctx,
				[]byte("current"),
				[]byte("current value long enough to persist"),
			)
			if err != nil {
				t.Fatalf("Update(current) error = %v", err)
			}
			trie, err = trie.Commit(ctx, store)
			if err != nil {
				t.Fatalf("Commit(current) error = %v", err)
			}
			currentRoot, err := trie.Root()
			if err != nil {
				t.Fatalf("Root(current) error = %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			command := exec.Command(
				os.Args[0],
				"-test.run=^TestPruneCrashHelper$",
			)
			command.Env = append(
				os.Environ(),
				pruneCrashPath+"="+path,
				pruneCrashStage+"="+test.stage,
			)
			err = command.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) ||
				exitErr.ExitCode() != crashExitCode {
				t.Fatalf("prune crash helper error = %v", err)
			}

			reopened, err := Open(ctx, path, DefaultLimits())
			if err != nil {
				t.Fatalf("Open(recovery) error = %v", err)
			}
			defer func() {
				if closeErr := reopened.Close(); closeErr != nil {
					t.Errorf("Close(recovery) error = %v", closeErr)
				}
			}()
			if reopened.Root() != currentRoot {
				t.Fatalf(
					"Root(recovery) = %x, want %x",
					reopened.Root(),
					currentRoot,
				)
			}
			current, err := mpt.LoadRawTrie(
				currentRoot,
				reopened,
				mpt.DefaultLimits(),
			)
			if err != nil {
				t.Fatalf("LoadRawTrie(current) error = %v", err)
			}
			if _, err := current.Get(
				ctx,
				[]byte("current"),
			); err != nil {
				t.Fatalf("Get(current) error = %v", err)
			}
			_, err = reopened.GetNode(ctx, oldRoot)
			if test.wantOldRootNode && err != nil {
				t.Fatalf("GetNode(restored old root) error = %v", err)
			}
			if !test.wantOldRootNode &&
				!errors.Is(err, mpt.ErrMissingNode) {
				t.Fatalf("GetNode(pruned old root) error = %v", err)
			}
			for _, name := range []string{
				prunePendingDirectory,
				pruneCommittedDirectory,
			} {
				if _, err := os.Lstat(
					filepath.Join(path, name),
				); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("recovered prune artifact %q error = %v", name, err)
				}
			}
		})
	}
}

func TestPruneCrashHelper(t *testing.T) {
	path := os.Getenv(pruneCrashPath)
	stage := os.Getenv(pruneCrashStage)
	if path == "" || stage == "" {
		return
	}
	ctx := context.Background()
	store, err := Open(ctx, path, DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	target := pruneNodesStaged
	if stage == "committed" {
		target = pruneCommitted
	}
	store.pruneCheckpoint = func(actual pruneStage) {
		if actual == target {
			os.Exit(crashExitCode)
		}
	}
	if _, err := store.Prune(
		ctx,
		mpt.DefaultReachabilityLimits(),
	); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	t.Fatal("prune completed without reaching crash checkpoint")
}

func TestRetentionSurvivesProcessTerminationAtPublicationBoundaries(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"retain", "release"} {
		operation := operation
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "trie")
			store, err := Open(ctx, path, DefaultLimits())
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
			if err != nil {
				t.Fatalf("NewRawTrie() error = %v", err)
			}
			trie, err = trie.Update(
				ctx,
				[]byte("retained"),
				[]byte("retained value long enough to persist"),
			)
			if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			trie, err = trie.Commit(ctx, store)
			if err != nil {
				t.Fatalf("Commit() error = %v", err)
			}
			root, err := trie.Root()
			if err != nil {
				t.Fatalf("Root() error = %v", err)
			}
			if operation == "release" {
				if _, err := store.RetainRoot(
					ctx,
					root,
					mpt.DefaultReachabilityLimits(),
				); err != nil {
					t.Fatalf("RetainRoot() error = %v", err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			command := exec.Command(
				os.Args[0],
				"-test.run=^TestRetentionCrashHelper$",
			)
			command.Env = append(
				os.Environ(),
				retentionCrashPath+"="+path,
				retentionCrashOperation+"="+operation,
			)
			err = command.Run()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) ||
				exitErr.ExitCode() != crashExitCode {
				t.Fatalf("retention crash helper error = %v", err)
			}

			reopened, err := Open(ctx, path, DefaultLimits())
			if err != nil {
				t.Fatalf("Open(recovery) error = %v", err)
			}
			defer func() {
				if closeErr := reopened.Close(); closeErr != nil {
					t.Errorf("Close(recovery) error = %v", closeErr)
				}
			}()
			retentions, err := reopened.Retentions(
				ctx,
				DefaultLimits().MaxRetentions,
			)
			if err != nil {
				t.Fatalf("Retentions() error = %v", err)
			}
			want := 1
			if operation == "release" {
				want = 0
			}
			if len(retentions) != want {
				t.Fatalf("Retentions() count = %d, want %d", len(retentions), want)
			}
			if len(retentions) == 1 && retentions[0].Root() != root {
				t.Fatalf("Retentions()[0].Root() = %x, want %x", retentions[0].Root(), root)
			}
		})
	}
}

func TestRetentionCrashHelper(t *testing.T) {
	path := os.Getenv(retentionCrashPath)
	operation := os.Getenv(retentionCrashOperation)
	if path == "" || operation == "" {
		return
	}
	ctx := context.Background()
	store, err := Open(ctx, path, DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	target := retentionPublished
	if operation == "release" {
		target = retentionReleased
	}
	store.retentionCheckpoint = func(actual retentionStage) {
		if actual == target {
			os.Exit(crashExitCode)
		}
	}
	if operation == "release" {
		retentions, err := store.Retentions(
			ctx,
			DefaultLimits().MaxRetentions,
		)
		if err != nil {
			t.Fatalf("Retentions() error = %v", err)
		}
		if len(retentions) != 1 {
			t.Fatalf("Retentions() count = %d, want 1", len(retentions))
		}
		if err := retentions[0].Release(ctx); err != nil {
			t.Fatalf("Release() error = %v", err)
		}
	} else {
		if _, err := store.RetainRoot(
			ctx,
			store.Root(),
			mpt.DefaultReachabilityLimits(),
		); err != nil {
			t.Fatalf("RetainRoot() error = %v", err)
		}
	}
	t.Fatal("retention operation completed without reaching crash checkpoint")
}
