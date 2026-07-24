package international

import (
	"fmt"
	"sort"
)

// MaxDatasetRecords bounds generic dataset comparison work.
const MaxDatasetRecords = 100_000

// Record is the compatibility-relevant projection of a generated entry.
type Record struct {
	ID          string `json:"id"`
	Status      Status `json:"status"`
	Fingerprint string `json:"fingerprint"`
	AliasOf     string `json:"alias_of"`
}

// DatasetDiff classifies changes that require compatibility review.
type DatasetDiff struct {
	Added           []string `json:"added"`
	Removed         []string `json:"removed"`
	AliasesChanged  []string `json:"aliases_changed"`
	StatusChanged   []string `json:"status_changed"`
	MetadataChanged []string `json:"metadata_changed"`
}

// DiffRecords deterministically classifies changes between two projections.
func DiffRecords(before, after []Record) (DatasetDiff, error) {
	if len(before) > MaxDatasetRecords || len(after) > MaxDatasetRecords {
		return DatasetDiff{}, ErrResourceLimit
	}

	oldRecords, err := indexRecords(before)
	if err != nil {
		return DatasetDiff{}, err
	}
	newRecords, err := indexRecords(after)
	if err != nil {
		return DatasetDiff{}, err
	}

	diff := DatasetDiff{
		Added:           []string{},
		Removed:         []string{},
		AliasesChanged:  []string{},
		StatusChanged:   []string{},
		MetadataChanged: []string{},
	}
	for id, oldRecord := range oldRecords {
		newRecord, exists := newRecords[id]
		if !exists {
			diff.Removed = append(diff.Removed, id)
			continue
		}
		if oldRecord.Status != newRecord.Status {
			diff.StatusChanged = append(diff.StatusChanged, id)
		}
		if oldRecord.AliasOf != newRecord.AliasOf {
			diff.AliasesChanged = append(diff.AliasesChanged, id)
		}
		if oldRecord.Fingerprint != newRecord.Fingerprint {
			diff.MetadataChanged = append(diff.MetadataChanged, id)
		}
	}
	for id := range newRecords {
		if _, exists := oldRecords[id]; !exists {
			diff.Added = append(diff.Added, id)
		}
	}

	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.AliasesChanged)
	sort.Strings(diff.StatusChanged)
	sort.Strings(diff.MetadataChanged)

	return diff, nil
}

func indexRecords(records []Record) (map[string]Record, error) {
	indexed := make(map[string]Record, len(records))
	for _, record := range records {
		if _, exists := indexed[record.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate record", ErrInvalidDataset)
		}
		indexed[record.ID] = record
	}
	return indexed, nil
}
