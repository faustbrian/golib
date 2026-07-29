package treelayout

import (
	"context"
	"reflect"
	"testing"
)

func FuzzLayoutTransitions(f *testing.F) {
	collisionAndDelete := make([]byte, 96)
	collisionAndDelete[1] = 0x10
	collisionAndDelete[2] = 0x20
	collisionAndDelete[33] = 0x10
	collisionAndDelete[34] = 0x30
	collisionAndDelete[64] = 1
	collisionAndDelete[65] = 0x10
	collisionAndDelete[66] = 0x20
	f.Add(collisionAndDelete)
	deepCollision := make([]byte, 64)
	deepCollision[31] = 0x01
	deepCollision[32] = 0
	deepCollision[63] = 0x02
	f.Add(deepCollision)

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 32*32 {
			return
		}

		limits := Limits{
			MaxStems:          32,
			MaxNodes:          1024,
			MaxEdges:          1024,
			MaxTemporaryBytes: 1 << 20,
		}
		layout, err := Build(context.Background(), nil, limits)
		if err != nil {
			t.Fatalf("build empty layout: %v", err)
		}
		model := make(map[Stem]struct{})

		for len(encoded) >= 32 {
			operation := encoded[0]
			var stem Stem
			copy(stem[:], encoded[1:32])
			encoded = encoded[32:]

			_, present := model[stem]
			var changed bool
			if operation&1 == 0 {
				layout, changed, err = layout.Insert(context.Background(), stem)
				if err == nil {
					model[stem] = struct{}{}
				}
			} else {
				layout, changed, err = layout.Delete(context.Background(), stem)
				if err == nil {
					delete(model, stem)
				}
			}
			if err != nil {
				t.Fatalf("apply operation %d to %x: %v", operation, stem, err)
			}
			wantChanged := (operation&1 == 0 && !present) ||
				(operation&1 == 1 && present)
			if changed != wantChanged {
				t.Fatalf(
					"operation %d changed = %t, want %t",
					operation,
					changed,
					wantChanged,
				)
			}
			if layout.StemCount() != len(model) {
				t.Fatalf(
					"stem count = %d, want %d",
					layout.StemCount(),
					len(model),
				)
			}

			stems := make([]Stem, 0, len(model))
			for expected := range model {
				result, lookupErr := layout.Lookup(
					context.Background(),
					expected,
				)
				if lookupErr != nil {
					t.Fatalf("lookup %x: %v", expected, lookupErr)
				}
				if result.Match != MatchPresentStem ||
					result.Existing != expected {
					t.Fatalf("lookup %x = %#v", expected, result)
				}
				stems = append(stems, expected)
			}
			rebuilt, rebuildErr := Build(context.Background(), stems, limits)
			if rebuildErr != nil {
				t.Fatalf("rebuild current state: %v", rebuildErr)
			}
			if !reflect.DeepEqual(layout.stems, rebuilt.stems) ||
				!reflect.DeepEqual(layout.nodes, rebuilt.nodes) ||
				!reflect.DeepEqual(layout.edges, rebuilt.edges) {
				t.Fatalf(
					"incremental layout differs from canonical rebuild:\n"+
						"stems %x / %x\nnodes %#v / %#v\nedges %#v / %#v",
					layout.stems,
					rebuilt.stems,
					layout.nodes,
					rebuilt.nodes,
					layout.edges,
					rebuilt.edges,
				)
			}
		}
	})
}
