// Command international-dataset-review snapshots and compares governed data.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/faustbrian/golib/pkg/international/internal/datasetreview"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("international-dataset-review", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	snapshotPath := flags.String("snapshot", "", "write the current semantic snapshot")
	beforePath := flags.String("before", "", "previous semantic snapshot")
	afterPath := flags.String("after", "", "updated semantic snapshot")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("positional arguments are not supported")
	}
	if *snapshotPath != "" {
		if *beforePath != "" || *afterPath != "" {
			return errors.New("snapshot and diff modes are mutually exclusive")
		}
		return writeSnapshot(*snapshotPath)
	}
	if *beforePath == "" || *afterPath == "" {
		return errors.New("supply -snapshot or both -before and -after")
	}
	before, err := readSnapshot(*beforePath)
	if err != nil {
		return err
	}
	after, err := readSnapshot(*afterPath)
	if err != nil {
		return err
	}
	report, err := datasetreview.Diff(before, after)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func writeSnapshot(path string) error {
	// #nosec G304 -- the path is an explicit local CLI output argument.
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create dataset snapshot: %w", err)
	}
	if err := datasetreview.Encode(file, datasetreview.Current()); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close dataset snapshot: %w", err)
	}
	return nil
}

func readSnapshot(path string) (datasetreview.Snapshot, error) {
	// #nosec G304 -- the path is an explicit local CLI input argument.
	file, err := os.Open(path)
	if err != nil {
		return datasetreview.Snapshot{}, fmt.Errorf("open dataset snapshot: %w", err)
	}
	defer func() { _ = file.Close() }()
	return datasetreview.Decode(file)
}
