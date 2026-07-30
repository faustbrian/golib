package mpt_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCleanExternalConsumer(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate module source")
	}
	moduleDirectory := filepath.Dir(sourceFile)
	consumerDirectory := t.TempDir()

	goMod := fmt.Sprintf(`module example.test/mpt-consumer

go 1.26.5

require github.com/faustbrian/golib/pkg/merkle-patricia-trie v0.0.0

replace github.com/faustbrian/golib/pkg/merkle-patricia-trie => %s
`, filepath.ToSlash(moduleDirectory))
	writeConsumerFile(t, consumerDirectory, "go.mod", goMod)
	writeConsumerFile(t, consumerDirectory, "consumer_test.go", `package consumer_test

import (
	"context"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/filesystem"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func TestPublicPackages(t *testing.T) {
	limits := mpt.DefaultLimits()
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatal(err)
	}
	trie, err = trie.Update(context.Background(), []byte("key"), []byte("value"))
	if err != nil {
		t.Fatal(err)
	}
	store := memory.New()
	if _, err = trie.Commit(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if filesystem.DefaultLimits().MaxNodeBytes == 0 {
		t.Fatal("filesystem limits are unusable")
	}
}
`)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-mod=mod", "./...")
	command.Dir = consumerDirectory
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("clean consumer: %v\n%s", err, output)
	}
}

func writeConsumerFile(
	t *testing.T,
	directory, name, contents string,
) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(directory, name), []byte(contents), 0o600,
	); err != nil {
		t.Fatalf("write consumer %s: %v", name, err)
	}
}
