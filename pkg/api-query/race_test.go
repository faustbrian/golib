package apiquery_test

import (
	"context"
	"sync"
	"testing"

	apiquery "github.com/faustbrian/golib/pkg/api-query"
)

func TestSchemaAndPlanAreSafeForConcurrentReaders(t *testing.T) {
	t.Parallel()

	schema := benchmarkSchema(t, 32)
	request := benchmarkRequest(16)
	plan, err := apiquery.Compile(context.Background(), schema, request, apiquery.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := plan.Canonical()
	if err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for iteration := 0; iteration < 100; iteration++ {
				compiled, compileErr := apiquery.Compile(context.Background(), schema,
					request, apiquery.CompileOptions{})
				if compileErr != nil {
					t.Errorf("Compile() error = %v", compileErr)
					return
				}
				got, canonicalErr := compiled.Canonical()
				if canonicalErr != nil || string(got) != string(want) {
					t.Errorf("Canonical() = %q, %v; want %q", got, canonicalErr, want)
					return
				}
				fields := plan.ExecutionFields()
				fields[0] = "mutated"
				if plan.ExecutionFields()[0] == "mutated" {
					t.Error("plan exposed mutable field storage")
					return
				}
			}
		}()
	}
	workers.Wait()
}
