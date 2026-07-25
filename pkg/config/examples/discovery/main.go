package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/discover"
	"github.com/faustbrian/golib/pkg/config/filesystem"
)

type settings struct {
	Name string `config:"name,required"`
}

func main() {
	root, err := os.MkdirTemp("", "config-example-")
	must(err)
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			panic(err)
		}
	}()
	start := filepath.Join(root, "services", "worker")
	must(os.MkdirAll(start, 0o700))
	must(os.WriteFile(filepath.Join(root, "app.yaml"), []byte("name: worker\n"), 0o600))

	results, err := discover.Search(context.Background(), discover.Options{
		Root: root, StartDir: start, StopDir: root, Upward: true,
		SearchPlaces: []string{"app.yaml"}, Mode: discover.SearchFirst,
		Permissions: discover.OwnerOnly,
	})
	must(err)
	if len(results) != 1 {
		panic("configuration not found")
	}
	source, err := filesystem.FromDiscovered(
		results[0], filesystem.Options{Name: "discovered"},
	)
	must(err)
	plan, err := config.NewPlan(source)
	must(err)
	snapshot, err := config.Load[settings](context.Background(), plan)
	must(err)
	fmt.Printf("%s from %s\n", snapshot.Value().Name, results[0].SearchPlace)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
