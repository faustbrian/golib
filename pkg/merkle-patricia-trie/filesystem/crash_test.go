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
	crashHelperPath  = "MPT_FILESYSTEM_CRASH_PATH"
	crashHelperStage = "MPT_FILESYSTEM_CRASH_STAGE"
	crashExitCode    = 86
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
