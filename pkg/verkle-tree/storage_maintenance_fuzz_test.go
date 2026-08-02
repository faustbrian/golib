package verkletree

import (
	"bytes"
	"context"
	"testing"
)

func FuzzStorageMaintenanceInventorySequence(f *testing.F) {
	f.Add([]byte{}, []byte{}, false)
	f.Add(make([]byte, NodeIDSize), []byte{1}, false)
	f.Add(
		append(make([]byte, NodeIDSize), bytes.Repeat([]byte{1}, NodeIDSize)...),
		[]byte{1, 0},
		true,
	)
	f.Add(
		append(bytes.Repeat([]byte{2}, NodeIDSize), bytes.Repeat([]byte{1}, NodeIDSize)...),
		[]byte{0, 1},
		false,
	)

	f.Fuzz(func(t *testing.T, encoded []byte, retainedMask []byte, continueAfterPage bool) {
		const maximumIDs = 64
		if len(encoded) > maximumIDs*NodeIDSize {
			return
		}
		ids := make([]NodeID, len(encoded)/NodeIDSize)
		all := make(map[NodeID]struct{}, len(ids))
		kept := make(map[NodeID]struct{}, len(ids))
		wantDeleted := 0
		for index := range ids {
			copy(ids[index][:], encoded[index*NodeIDSize:(index+1)*NodeIDSize])
			all[ids[index]] = struct{}{}
			if index < len(retainedMask) && retainedMask[index]&1 == 1 {
				kept[ids[index]] = struct{}{}
			} else {
				wantDeleted++
			}
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
		limits.MaxInventoryNodes = maximumIDs
		limits.MaxNodeIDsPerPage = maximumIDs
		limits.MaxUnreachableNodes = maximumIDs
		limits.MaxTemporaryBytes = 1 << 20
		deleted, count, err := maintenanceInventory(
			context.Background(), view, limits, 0, all, kept,
		)
		if err != nil {
			return
		}
		if count != len(ids) || len(deleted) != wantDeleted || len(all) != 0 {
			t.Fatalf(
				"accepted counts = (%d, %d, %d), want (%d, %d, 0)",
				count, len(deleted), len(all), len(ids), wantDeleted,
			)
		}
		for index := 1; index < len(deleted); index++ {
			if bytes.Compare(deleted[index-1][:], deleted[index][:]) >= 0 {
				t.Fatalf("accepted non-increasing deletion set at %d", index)
			}
		}
	})
}
