// Package datasetreview creates bounded semantic snapshots of generated data.
package datasetreview

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

const (
	// SchemaVersion identifies the deterministic snapshot representation.
	SchemaVersion = 1
	// MaxSnapshotBytes bounds review artifact decoding and diagnostics.
	MaxSnapshotBytes = 2 << 20
)

// Snapshot is the compatibility-relevant projection of every generated table.
type Snapshot struct {
	Schema      int                    `json:"schema"`
	Country     []international.Record `json:"country"`
	Subdivision []international.Record `json:"subdivision"`
	Currency    []international.Record `json:"currency"`
}

// Report classifies semantic changes independently for each generated dataset.
type Report struct {
	Country     international.DatasetDiff `json:"country"`
	Subdivision international.DatasetDiff `json:"subdivision"`
	Currency    international.DatasetDiff `json:"currency"`
}

// Current returns a fresh snapshot of the compiled generated tables.
func Current() Snapshot {
	return Snapshot{
		Schema:      SchemaVersion,
		Country:     country.DatasetRecords(),
		Subdivision: subdivision.DatasetRecords(),
		Currency:    currency.DatasetRecords(),
	}
}

// Encode writes a validated snapshot as deterministic indented JSON.
func Encode(writer io.Writer, snapshot Snapshot) error {
	if writer == nil {
		return fmt.Errorf("%w: snapshot writer is required", international.ErrInvalidDataset)
	}
	if err := validate(snapshot); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("encode dataset snapshot: %w", err)
	}
	return nil
}

// Decode reads one bounded, strict snapshot document.
func Decode(reader io.Reader) (Snapshot, error) {
	if reader == nil {
		return Snapshot{}, fmt.Errorf("%w: snapshot reader is required", international.ErrInvalidDataset)
	}
	payload, err := io.ReadAll(io.LimitReader(reader, MaxSnapshotBytes+1))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read dataset snapshot: %w", err)
	}
	if len(payload) > MaxSnapshotBytes {
		return Snapshot{}, international.ErrResourceLimit
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("%w: decode snapshot", international.ErrInvalidDataset)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Snapshot{}, fmt.Errorf("%w: trailing snapshot data", international.ErrInvalidDataset)
	}
	if err := validate(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// Diff validates and classifies changes between two semantic snapshots.
func Diff(before, after Snapshot) (Report, error) {
	if err := validate(before); err != nil {
		return Report{}, err
	}
	if err := validate(after); err != nil {
		return Report{}, err
	}
	countryDiff, err := international.DiffRecords(before.Country, after.Country)
	subdivisionDiff, subdivisionErr := international.DiffRecords(before.Subdivision, after.Subdivision)
	currencyDiff, currencyErr := international.DiffRecords(before.Currency, after.Currency)
	return Report{Country: countryDiff, Subdivision: subdivisionDiff, Currency: currencyDiff},
		errors.Join(err, subdivisionErr, currencyErr)
}

func validate(snapshot Snapshot) error {
	if snapshot.Schema != SchemaVersion {
		return fmt.Errorf("%w: unsupported snapshot schema", international.ErrInvalidDataset)
	}
	for _, dataset := range []struct {
		name    string
		records []international.Record
	}{
		{name: "country", records: snapshot.Country},
		{name: "subdivision", records: snapshot.Subdivision},
		{name: "currency", records: snapshot.Currency},
	} {
		if err := validateRecords(dataset.records); err != nil {
			return fmt.Errorf("%w: invalid %s records", err, dataset.name)
		}
	}
	return nil
}

func validateRecords(records []international.Record) error {
	if len(records) == 0 || len(records) > international.MaxDatasetRecords {
		return international.ErrInvalidDataset
	}
	previous := ""
	for _, record := range records {
		fingerprint, err := hex.DecodeString(record.Fingerprint)
		if record.ID == "" || record.ID <= previous || record.Status > international.StatusHistoric ||
			err != nil || len(fingerprint) != sha256Bytes {
			return international.ErrInvalidDataset
		}
		previous = record.ID
	}
	return nil
}

const sha256Bytes = 32
