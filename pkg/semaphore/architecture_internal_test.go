package semaphore

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProductionOwnsNoHiddenGoroutineTimerOrFinalizer(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Clean(entry.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		aliases := make(map[string]string)
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Base(path)
			if imported.Name != nil {
				name = imported.Name.Name
			}
			aliases[name] = path
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.GoStmt:
				t.Errorf("%s owns a goroutine at token position %v", entry.Name(), node.Go)
			case *ast.CallExpr:
				selector, ok := node.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				path := aliases[identifier.Name]
				if path == "time" && (selector.Sel.Name == "After" || selector.Sel.Name == "AfterFunc" ||
					selector.Sel.Name == "NewTicker" || selector.Sel.Name == "NewTimer" || selector.Sel.Name == "Sleep" ||
					selector.Sel.Name == "Tick") {
					t.Errorf("%s owns a timer through time.%s", entry.Name(), selector.Sel.Name)
				}
				if path == "context" && (selector.Sel.Name == "AfterFunc" || selector.Sel.Name == "WithDeadline" ||
					selector.Sel.Name == "WithDeadlineCause" || selector.Sel.Name == "WithTimeout" ||
					selector.Sel.Name == "WithTimeoutCause") {
					t.Errorf("%s owns a timer through context.%s", entry.Name(), selector.Sel.Name)
				}
				if path == "runtime" && (selector.Sel.Name == "AddCleanup" || selector.Sel.Name == "SetFinalizer") {
					t.Errorf("%s owns lifecycle cleanup through runtime.%s", entry.Name(), selector.Sel.Name)
				}
			}
			return true
		})
	}
}

func TestTerminalWaitersAreDetachedFromQueue(t *testing.T) {
	t.Parallel()

	for _, terminal := range []string{"cancel", "close", "grant"} {
		t.Run(terminal, func(t *testing.T) {
			t.Parallel()
			sem, err := New(Config{Capacity: 1, MaxWaiters: 1})
			if err != nil {
				t.Fatal(err)
			}
			held, err := sem.Acquire(testContextInternal(t), 1)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan acquireResultInternal, 1)
			go func() {
				permit, acquireErr := sem.Acquire(ctx, 1)
				result <- acquireResultInternal{permit: permit, err: acquireErr}
			}()
			waitForSnapshotInternal(t, sem, func(snapshot Snapshot) bool { return snapshot.Waiters == 1 })

			sem.mu.Lock()
			node := sem.waiterHead
			sem.mu.Unlock()
			if node == nil {
				t.Fatal("queued waiter missing")
			}

			switch terminal {
			case "cancel":
				cancel()
			case "close":
				if err := sem.Close(); err != nil {
					t.Fatal(err)
				}
			case "grant":
				if err := held.Release(); err != nil {
					t.Fatal(err)
				}
			}
			outcome := receiveInternal(t, result)
			if outcome.permit != nil {
				if err := outcome.permit.Release(); err != nil {
					t.Fatal(err)
				}
			}
			if terminal != "grant" {
				if err := held.Release(); err != nil {
					t.Fatal(err)
				}
			}

			sem.mu.Lock()
			defer sem.mu.Unlock()
			if sem.waiterCount != 0 || sem.waiterHead != nil || sem.waiterTail != nil {
				t.Fatalf("terminal queue state: count=%d head=%p tail=%p", sem.waiterCount, sem.waiterHead, sem.waiterTail)
			}
			if node.previous != nil || node.next != nil {
				t.Fatalf("terminal waiter retained queue links: previous=%p next=%p", node.previous, node.next)
			}
		})
	}
}

type acquireResultInternal struct {
	permit *Permit
	err    error
}

func testContextInternal(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func waitForSnapshotInternal(t *testing.T, sem *Semaphore, ready func(Snapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !ready(sem.Snapshot()) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for state: %+v", sem.Snapshot())
		}
		runtime.Gosched()
	}
}

func receiveInternal(t *testing.T, result <-chan acquireResultInternal) acquireResultInternal {
	t.Helper()
	select {
	case outcome := <-result:
		return outcome
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for acquisition result")
		return acquireResultInternal{}
	}
}
