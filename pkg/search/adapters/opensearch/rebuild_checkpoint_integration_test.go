//go:build integration

package opensearch_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maximumRebuildCheckpointBytes = 16 << 10

type durableRebuildCheckpoint struct {
	SnapshotID     string `json:"snapshot_id"`
	SourceCursor   string `json:"source_cursor"`
	SourceComplete bool   `json:"source_complete"`
	OutboxCursor   int    `json:"outbox_cursor"`
}

func loadDurableRebuildCheckpoint(path, snapshotID string) (durableRebuildCheckpoint, error) {
	file, err := os.Open(path)
	if err != nil {
		return durableRebuildCheckpoint{}, err
	}
	defer func() { _ = file.Close() }()
	encoded, err := io.ReadAll(io.LimitReader(file, maximumRebuildCheckpointBytes+1))
	if err != nil || len(encoded) > maximumRebuildCheckpointBytes {
		return durableRebuildCheckpoint{}, errors.New("rebuild checkpoint exceeds its byte limit")
	}
	var checkpoint durableRebuildCheckpoint
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&checkpoint) != nil || decoder.Decode(&struct{}{}) != io.EOF || checkpoint.SnapshotID != snapshotID || checkpoint.OutboxCursor < 0 {
		return durableRebuildCheckpoint{}, errors.New("rebuild checkpoint is malformed or belongs to another snapshot")
	}
	return checkpoint, nil
}

func saveDurableRebuildCheckpoint(path string, checkpoint durableRebuildCheckpoint) error {
	if checkpoint.SnapshotID == "" || checkpoint.OutboxCursor < 0 {
		return errors.New("rebuild checkpoint is invalid")
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil || len(encoded) > maximumRebuildCheckpointBytes {
		return errors.New("rebuild checkpoint encoding exceeds its byte limit")
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".rebuild-checkpoint-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return err
	}
	committed = true
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer func() { _ = directoryHandle.Close() }()
	return directoryHandle.Sync()
}
