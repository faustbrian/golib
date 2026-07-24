package reference_test

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestReferenceRewritersRejectWideValuesBeforeCopyingChildren(t *testing.T) {
	members := []jsonvalue.Member{
		{Name: "openapi", Value: mustString(t, "3.2.0")},
		{Name: "paths", Value: mustObject(t, nil)},
	}
	for index := range 4096 {
		members = append(members, jsonvalue.Member{
			Name: "x-wide-" + strconv.Itoa(index), Value: jsonvalue.Null(),
		})
	}
	base := reference.Resource{Root: mustObject(t, members)}
	schemas := make([]jsonvalue.Member, 4096)
	for index := range schemas {
		schemas[index] = jsonvalue.Member{
			Name: "Schema" + strconv.Itoa(index), Value: jsonvalue.Null(),
		}
	}
	componentBase := reference.Resource{Root: mustObject(t, []jsonvalue.Member{
		{Name: "openapi", Value: mustString(t, "3.2.0")},
		{Name: "paths", Value: mustObject(t, nil)},
		{Name: "components", Value: mustObject(t, []jsonvalue.Member{
			{Name: "schemas", Value: mustObject(t, schemas)},
		})},
	})}
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "bundle", run: func() error {
			options := reference.DefaultBundleOptions()
			options.MaxNodes = 1
			_, err := reference.BundleComponents(
				context.Background(), base, nil, options,
			)
			return err
		}},
		{name: "bundle existing components", run: func() error {
			options := reference.DefaultBundleOptions()
			options.MaxComponents = 1
			_, err := reference.BundleComponents(
				context.Background(), componentBase, nil, options,
			)
			return err
		}},
		{name: "dereference", run: func() error {
			options := reference.DefaultDereferenceOptions()
			options.MaxNodes = 1
			_, err := reference.DereferenceObjects(
				context.Background(), base, nil, options,
			)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const repetitions = 16
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			for range repetitions {
				if err := test.run(); !errors.Is(err, reference.ErrLimitExceeded) {
					t.Fatalf("wide rewrite error = %v", err)
				}
			}
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions
			if allocated > 64<<10 {
				t.Fatalf("wide rejected rewrite allocated %d bytes per operation", allocated)
			}
		})
	}
}
