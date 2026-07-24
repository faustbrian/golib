package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maximumReportBytes int64 = 67_108_864

type mutationReport struct {
	Files []struct {
		Mutations []struct {
			Status string `json:"status"`
		} `json:"mutations"`
	} `json:"files"`
	TestEfficacy      float64 `json:"test_efficacy"`
	MutationCoverage  float64 `json:"mutations_coverage"`
	MutantsTotal      int     `json:"mutants_total"`
	MutantsKilled     int     `json:"mutants_killed"`
	MutantsLived      int     `json:"mutants_lived"`
	MutantsNotViable  int     `json:"mutants_not_viable"`
	MutantsNotCovered int     `json:"mutants_not_covered"`
	MutantsTimedOut   int     `json:"mutants_timed_out"`
	MutantsSkipped    int     `json:"mutants_skipped"`
}

type inputFile interface {
	io.Reader
	Close() error
}

var exitProcess = os.Exit
var openInput = openReport

func openReport(path string) (inputFile, error) { return os.Open(path) }

func main() {
	exitProcess(execute(os.Args[1:], os.Stderr, openInput))
}

func execute(
	args []string,
	stderr io.Writer,
	openFile func(string) (inputFile, error),
) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: mutationgate REPORT.json")
		return 2
	}
	file, err := openFile(args[0])
	if err == nil {
		err = check(file)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func check(reader io.Reader) error {
	report, err := decodeReport(reader, maximumReportBytes)
	if err != nil {
		return err
	}
	if report.MutantsTotal <= 0 || report.MutantsKilled <= 0 {
		return errors.New("mutation gate: report contains no tested mutants")
	}
	if report.MutantsLived != 0 || report.MutantsNotCovered != 0 ||
		report.MutantsTimedOut != 0 || report.MutantsSkipped != 0 {
		return fmt.Errorf(
			"mutation gate: lived=%d not-covered=%d timed-out=%d skipped=%d",
			report.MutantsLived,
			report.MutantsNotCovered,
			report.MutantsTimedOut,
			report.MutantsSkipped,
		)
	}
	if report.TestEfficacy != 100 || report.MutationCoverage != 100 {
		return fmt.Errorf(
			"mutation gate: efficacy=%.2f%% coverage=%.2f%%",
			report.TestEfficacy,
			report.MutationCoverage,
		)
	}
	for _, file := range report.Files {
		for _, mutation := range file.Mutations {
			switch mutation.Status {
			case "KILLED", "NOT_VIABLE":
			default:
				return errors.New("mutation gate: unacceptable mutant status")
			}
		}
	}
	return nil
}

func decodeReport(reader io.Reader, maxBytes int64) (mutationReport, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, maxBytes+1))
	var report mutationReport
	if err := decoder.Decode(&report); err != nil {
		return mutationReport{}, fmt.Errorf("mutation gate: decode report: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return mutationReport{}, fmt.Errorf(
			"mutation gate: trailing report data: %w", err,
		)
	}
	return report, nil
}
