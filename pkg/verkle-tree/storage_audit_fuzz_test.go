package verkletree

import (
	"bytes"
	"context"
	"testing"
)

func FuzzStorageAuditInventorySequence(f *testing.F) {
	f.Add([]byte{}, false)
	f.Add(make([]byte, NodeIDSize), false)
	f.Add(append(make([]byte, NodeIDSize), bytes.Repeat([]byte{1}, NodeIDSize)...), true)
	f.Add(append(bytes.Repeat([]byte{2}, NodeIDSize), bytes.Repeat([]byte{1}, NodeIDSize)...), false)

	f.Fuzz(func(t *testing.T, encoded []byte, continueAfterPage bool) {
		ids := make([]NodeID, len(encoded)/NodeIDSize)
		for index := range ids {
			copy(ids[index][:], encoded[index*NodeIDSize:(index+1)*NodeIDSize])
		}
		calls := 0
		view := &internalAuditSnapshot{
			nodeIDsFn: func(*NodeID, uint32) ([]NodeID, bool, error) {
				calls++
				if calls != 1 {
					return nil, false, nil
				}

				return append([]NodeID(nil), ids...), continueAfterPage, nil
			},
		}
		limits := testInternalStorageAuditLimits()
		limits.MaxInventoryNodes = 64
		limits.MaxNodeIDsPerPage = 64
		limits.MaxUnreachableNodes = 64
		limits.MaxTemporaryBytes = 1 << 20
		unreachable, count, err := auditInventory(
			context.Background(), view, limits, 0, 0, map[NodeID]struct{}{},
		)
		if err != nil {
			return
		}
		if count != len(ids) || len(unreachable) != len(ids) {
			t.Fatalf("accepted counts = (%d, %d), want %d", count, len(unreachable), len(ids))
		}
		for index := 1; index < len(unreachable); index++ {
			if bytes.Compare(unreachable[index-1][:], unreachable[index][:]) >= 0 {
				t.Fatalf("accepted non-increasing inventory at %d", index)
			}
		}
	})
}
